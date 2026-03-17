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
//  1. KubeSpawner's state.dns_name — the pod's in-cluster DNS name (preferred).
//  2. Parse the internal URL if it starts with http:// and contains an IP.
func (c *Client) DefaultServerPodIP(user *UserInfo) (string, error) {
	server, ok := user.Servers[""]
	if !ok || !server.Ready {
		return "", ErrServerNotReady
	}

	// 1. Prefer the KubeSpawner dns_name from server state.
	if dns := server.State.DNSName; dns != "" {
		return dns, nil
	}

	// 2. Fall back: try to parse an IP from an internal server URL.
	ip, err := extractIPFromServerURL(server.URL)
	if err != nil {
		return "", fmt.Errorf("could not locate pod address (state.dns_name empty, URL parse failed: %w)", err)
	}
	return ip, nil
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
