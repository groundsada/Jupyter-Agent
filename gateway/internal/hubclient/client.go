// Package hubclient provides a minimal JupyterHub API client used by the
// SSH gateway and port-forwarding proxy to authenticate tokens and look up
// user server addresses.
package hubclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a JupyterHub API client.
type Client struct {
	BaseURL    string // e.g. "https://jupyter.example.com"
	AdminToken string // Hub service token with admin read access
	HTTPClient *http.Client
}

// New creates a Client with sensible defaults.
func New(baseURL, adminToken string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		AdminToken: adminToken,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// UserInfo contains the subset of JupyterHub user data we need.
type UserInfo struct {
	Name    string            `json:"name"`
	Admin   bool              `json:"admin"`
	Servers map[string]Server `json:"servers"`
}

// ServerState holds KubeSpawner-specific state embedded in a server record.
type ServerState struct {
	PodName   string `json:"pod_name"`
	Namespace string `json:"namespace"`
	DNSName   string `json:"dns_name"` // e.g. "jupyter-alice.ns.svc.cluster.local"
}

// Server represents a single-user server entry from the JupyterHub API.
type Server struct {
	Name    string      `json:"name"`
	Ready   bool        `json:"ready"`
	// URL is the hub-relative URL path, e.g. "/user/alice/"
	URL     string      `json:"url"`
	State   ServerState `json:"state"`
	// UserOptions contains spawner-specific data; we use it to extract pod IP.
	UserOptions map[string]interface{} `json:"user_options"`
}

// TokenInfo is returned by /hub/api/authorizations/token/<token>.
type TokenInfo struct {
	Name   string   `json:"name"`   // username or service name
	Token  string   `json:"token"`
	Scopes []string `json:"scopes"`
	Kind   string   `json:"kind"`  // "user" or "service"
	Admin  bool     `json:"admin"` // true when the token has admin rights
}

// GetUser returns information about a JupyterHub user.
// Authenticates the request with the admin token.
func (c *Client) GetUser(ctx context.Context, username string) (*UserInfo, error) {
	url := fmt.Sprintf("%s/hub/api/users/%s?include_stopped_servers=false", c.BaseURL, username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+c.AdminToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrUserNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, body)
	}

	var info UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode user info: %w", err)
	}
	return &info, nil
}

// ValidateToken checks a JupyterHub API token and returns the owning user's name.
// Uses /hub/api/authorizations/token/<token>.
func (c *Client) ValidateToken(ctx context.Context, token string) (*TokenInfo, error) {
	url := fmt.Sprintf("%s/hub/api/authorizations/token/%s", c.BaseURL, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+c.AdminToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrInvalidToken
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrInvalidToken
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, body)
	}

	var info TokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode token info: %w", err)
	}
	return &info, nil
}

// DefaultServerPodIP returns the hostname (or IP) to reach the user's SSH sidecar.
//
// Strategy (in order):
//  1. Query the hub proxy routing table (/hub/api/proxy) — the target is the
//     internal http://<pod-ip>:<port>/user/…  URL that KubeSpawner registers.
//  2. KubeSpawner's state.dns_name (works when a per-user headless Service exists).
//  3. Parse the internal URL from server.URL if it happens to contain an IP.
func (c *Client) DefaultServerPodIP(ctx context.Context, user *UserInfo) (string, error) {
	server, ok := user.Servers[""]
	if !ok || !server.Ready {
		return "", ErrServerNotReady
	}

	// 1. Query the JupyterHub proxy routing table.
	// The proxy maps "/user/<name>/" → "http://<pod-ip>:<port>/user/<name>/".
	if ip, err := c.podIPFromProxy(ctx, user.Name); err == nil {
		return ip, nil
	}

	// 2. Try KubeSpawner's state.dns_name (requires per-user headless Service).
	if dns := server.State.DNSName; dns != "" {
		return dns, nil
	}

	// 3. Last resort: parse an IP from the server URL itself.
	ip, err := extractIPFromServerURL(server.URL)
	if err != nil {
		return "", fmt.Errorf("could not locate pod address: %w", err)
	}
	return ip, nil
}

// podIPFromProxy queries /hub/api/proxy and returns the pod IP for the user's
// default server by looking up the "/user/<name>/" route target.
func (c *Client) podIPFromProxy(ctx context.Context, username string) (string, error) {
	url := fmt.Sprintf("%s/hub/api/proxy", c.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+c.AdminToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	// Response is a map of path → {target: "http://<pod-ip>:<port>/user/…"}
	var routes map[string]struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
		return "", fmt.Errorf("decode proxy routes: %w", err)
	}

	routeKey := "/user/" + username + "/"
	entry, ok := routes[routeKey]
	if !ok || entry.Target == "" {
		return "", fmt.Errorf("no proxy route for %q", routeKey)
	}
	return extractIPFromServerURL(entry.Target)
}

// extractIPFromServerURL parses "http://10.0.1.42:8888/user/alice/" → "10.0.1.42".
func extractIPFromServerURL(rawURL string) (string, error) {
	// KubeSpawner uses the internal URL format http://<pod-ip>:<port>/...
	// We just need the host part.
	after, found := strings.CutPrefix(rawURL, "http://")
	if !found {
		after, found = strings.CutPrefix(rawURL, "https://")
		if !found {
			return "", fmt.Errorf("unexpected scheme in %q", rawURL)
		}
	}
	hostPort := strings.SplitN(after, "/", 2)[0]
	host := strings.SplitN(hostPort, ":", 2)[0]
	if host == "" {
		return "", fmt.Errorf("empty host in %q", rawURL)
	}
	return host, nil
}

// Sentinel errors
var (
	ErrUserNotFound  = fmt.Errorf("user not found")
	ErrInvalidToken  = fmt.Errorf("invalid or expired token")
	ErrServerNotReady = fmt.Errorf("user server not ready")
)
