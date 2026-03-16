// Package portfwd implements the wildcard subdomain port-forwarding proxy.
//
// Subdomain format: <port>--<username>.jupyter.example.com
//
// Flow:
//  1. Ingress routes *.jupyter.example.com to this service
//  2. Handler parses Host header to extract port and username
//  3. Validates auth (JupyterHub cookie or Bearer token header)
//  4. Looks up user server pod IP via JupyterHub API
//  5. Reverse-proxies HTTP(S)/WebSocket to http://<pod-ip>:<port><path>
package portfwd

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/groundsada/jhub-ssh/internal/hubclient"
)

const (
	// minPort is the lowest port number we will forward to (inclusive).
	// Ports 0–1023 are privileged and always blocked.
	minPort = 1024

	// maxPort is the highest allowed port.
	maxPort = 65535
)

// ParsedSubdomain holds the result of parsing a wildcard subdomain.
type ParsedSubdomain struct {
	Port     int
	Username string
}

// ParseSubdomain parses the leftmost label(s) of a hostname into a port and username.
//
// Expected format: "<port>--<username>.base.domain"
//   - "8888--alice.jupyter.example.com"  → {Port:8888, Username:"alice"}
//   - "3000--bob.jupyter.example.com"    → {Port:3000, Username:"bob"}
//
// Returns ErrInvalidSubdomain if the format does not match.
func ParseSubdomain(host string) (*ParsedSubdomain, error) {
	// Strip port suffix if present (e.g. host:80)
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Take only the leftmost label
	firstLabel := strings.SplitN(host, ".", 2)[0]

	// Must contain "--" separator
	parts := strings.SplitN(firstLabel, "--", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidSubdomain
	}

	portStr, username := parts[0], parts[1]
	if portStr == "" || username == "" {
		return nil, ErrInvalidSubdomain
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, ErrInvalidSubdomain
	}

	if port < minPort || port > maxPort {
		return nil, fmt.Errorf("%w: port %d is out of allowed range [%d, %d]",
			ErrPortForbidden, port, minPort, maxPort)
	}

	return &ParsedSubdomain{Port: port, Username: username}, nil
}

// Sentinel errors
var (
	ErrInvalidSubdomain = errors.New("invalid subdomain format (expected <port>--<username>)")
	ErrPortForbidden    = errors.New("port forbidden")
)

// Handler handles subdomain-based port forwarding.
type Handler struct {
	Hub    *hubclient.Client
	Log    *log.Logger

	// HubLoginURL is the URL to redirect unauthenticated users to.
	// Default: "<hub-base>/hub/login"
	HubLoginURL string
}

// New creates a Handler.
func New(hub *hubclient.Client, logger *log.Logger) *Handler {
	if logger == nil {
		logger = log.Default()
	}
	return &Handler{Hub: hub, Log: logger}
}

// ServeHTTP handles a port-forwarding request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sub, err := ParseSubdomain(r.Host)
	if err != nil {
		if errors.Is(err, ErrPortForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		// Not a port-forwarding subdomain — treat as 404
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Authenticate
	token := h.extractAuth(r)
	if token == "" {
		h.redirectToLogin(w, r)
		return
	}

	tokenInfo, err := h.Hub.ValidateToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, hubclient.ErrInvalidToken) {
			h.redirectToLogin(w, r)
			return
		}
		h.Log.Printf("portfwd: ValidateToken: %v", err)
		http.Error(w, "token validation failed", http.StatusInternalServerError)
		return
	}

	// Authorization: token owner must match username (or be admin)
	if tokenInfo.Name != sub.Username {
		ownerInfo, err := h.Hub.GetUser(r.Context(), tokenInfo.Name)
		if err != nil || !ownerInfo.Admin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	// Look up user server pod IP
	userInfo, err := h.Hub.GetUser(r.Context(), sub.Username)
	if err != nil {
		if errors.Is(err, hubclient.ErrUserNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		h.Log.Printf("portfwd: GetUser(%q): %v", sub.Username, err)
		http.Error(w, "hub API error", http.StatusBadGateway)
		return
	}

	podIP, err := h.Hub.DefaultServerPodIP(userInfo)
	if err != nil {
		if errors.Is(err, hubclient.ErrServerNotReady) {
			startURL := fmt.Sprintf("%s/hub/user/%s", h.Hub.BaseURL, sub.Username)
			http.Error(w,
				fmt.Sprintf("Server not running. Start it at %s", startURL),
				http.StatusServiceUnavailable,
			)
			return
		}
		h.Log.Printf("portfwd: pod IP lookup for %q: %v", sub.Username, err)
		http.Error(w, "could not locate server", http.StatusBadGateway)
		return
	}

	targetURL := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", podIP, sub.Port),
	}

	h.Log.Printf("portfwd: %s → %s%s", r.Host, targetURL.Host, r.URL.Path)
	proxy := newReverseProxy(targetURL)
	proxy.ServeHTTP(w, r)
}

// newReverseProxy creates an httputil.ReverseProxy that forwards to target.
func newReverseProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Override director to set correct Host header and strip auth params
	// that belong to JupyterHub and should not be forwarded to user services.
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host

		// Remove JupyterHub session cookies — the user service should not
		// be able to impersonate the hub session.
		strippedCookies := filterCookies(req.Cookies(), func(name string) bool {
			return !strings.HasPrefix(name, "jupyterhub-")
		})
		if len(strippedCookies) == 0 {
			req.Header.Del("Cookie")
		} else {
			parts := make([]string, 0, len(strippedCookies))
			for _, c := range strippedCookies {
				parts = append(parts, c.Name+"="+c.Value)
			}
			req.Header.Set("Cookie", strings.Join(parts, "; "))
		}

		// Strip the jhub_token query param (used for token-based auth)
		q := req.URL.Query()
		q.Del("jhub_token")
		req.URL.RawQuery = q.Encode()
	}

	// Modify response to clean up CORS headers from forwarded services
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Remove CORS headers — the browser's CORS policy should apply to
		// the subdomain, not be overridden by the inner service.
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Credentials")
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, fmt.Sprintf("proxy error: %v", err), http.StatusBadGateway)
	}

	// Set transport timeouts suitable for long-lived WebSocket connections
	proxy.Transport = &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
	}

	return proxy
}

// extractAuth reads the JupyterHub token from:
//  1. Authorization: token <t> header
//  2. Authorization: Bearer <t> header
//  3. ?jhub_token=<t> query param
//  4. jupyterhub-session-id cookie (validated via hub API)
func (h *Handler) extractAuth(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if t, ok := strings.CutPrefix(auth, "token "); ok {
		return t
	}
	if t, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return t
	}
	if t := r.URL.Query().Get("jhub_token"); t != "" {
		return t
	}
	// Try JupyterHub session cookie
	for _, c := range r.Cookies() {
		if c.Name == "jupyterhub-session-id" || strings.HasPrefix(c.Name, "jupyterhub-user-") {
			return c.Value
		}
	}
	return ""
}

// redirectToLogin redirects the user to the JupyterHub login page.
func (h *Handler) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	loginURL := h.HubLoginURL
	if loginURL == "" && h.Hub != nil {
		loginURL = h.Hub.BaseURL + "/hub/login"
	}
	next := r.URL.String()
	http.Redirect(w, r, loginURL+"?next="+url.QueryEscape(next), http.StatusFound)
}

// filterCookies returns cookies for which keep(name) returns true.
func filterCookies(cookies []*http.Cookie, keep func(string) bool) []*http.Cookie {
	out := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		if keep(c.Name) {
			out = append(out, c)
		}
	}
	return out
}

// Mux registers the handler. Port forwarding is triggered by subdomain routing
// at the ingress level — all requests on *.jupyter.example.com land here.
func (h *Handler) Mux(mux *http.ServeMux) {
	mux.Handle("/", h)
}
