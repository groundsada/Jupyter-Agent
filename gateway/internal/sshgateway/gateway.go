// Package sshgateway implements the SSH-over-WebSocket relay.
//
// Flow:
//  1. Client (jhub-ssh proxy-connect) opens WSS connection to /ssh/{username}
//     with header  Authorization: token <jhub-token>
//  2. Gateway validates the token via JupyterHub API
//  3. Gateway looks up the user's pod IP via JupyterHub API
//  4. Gateway dials TCP to <pod-ip>:2222 (SSH sidecar)
//  5. Gateway copies frames bidirectionally: WS ↔ TCP
//
// The WebSocket carries raw SSH protocol bytes as binary frames.
// This is the same approach as many WebSSH implementations and avoids
// the need for a WireGuard overlay network.
package sshgateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/groundsada/jhub-ssh/internal/hubclient"
	"golang.org/x/net/websocket"
)

const (
	// defaultSSHPort is the port the SSH sidecar listens on inside the pod.
	defaultSSHPort = "2222"

	// dialTimeout is how long we wait to connect to the SSH sidecar.
	dialTimeout = 10 * time.Second
)

// Handler handles WebSocket upgrade requests for SSH tunneling.
type Handler struct {
	Hub     *hubclient.Client
	SSHPort string // port the sidecar listens on (default "2222")
	Log     *log.Logger
}

// New creates a Handler with defaults.
func New(hub *hubclient.Client, logger *log.Logger) *Handler {
	if logger == nil {
		logger = log.Default()
	}
	return &Handler{
		Hub:     hub,
		SSHPort: defaultSSHPort,
		Log:     logger,
	}
}

// ServeHTTP handles GET /ssh/{username} — upgrades to WebSocket then relays.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract username from URL path: /ssh/{username}
	username := strings.TrimPrefix(r.URL.Path, "/ssh/")
	username = strings.TrimSuffix(username, "/")
	if username == "" {
		http.Error(w, "missing username", http.StatusBadRequest)
		return
	}

	// Validate token
	token := extractToken(r)
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	tokenInfo, err := h.Hub.ValidateToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, hubclient.ErrInvalidToken) {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		h.Log.Printf("ssh-gateway: ValidateToken error: %v", err)
		http.Error(w, "token validation failed", http.StatusInternalServerError)
		return
	}

	// Authorization: token owner must match target username (or be admin)
	if tokenInfo.Name != username {
		// Check if the token owner is an admin
		ownerInfo, err := h.Hub.GetUser(r.Context(), tokenInfo.Name)
		if err != nil || !ownerInfo.Admin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	// Look up user server pod IP
	userInfo, err := h.Hub.GetUser(r.Context(), username)
	if err != nil {
		if errors.Is(err, hubclient.ErrUserNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		h.Log.Printf("ssh-gateway: GetUser(%q) error: %v", username, err)
		http.Error(w, "hub API error", http.StatusBadGateway)
		return
	}

	podIP, err := h.Hub.DefaultServerPodIP(userInfo)
	if err != nil {
		if errors.Is(err, hubclient.ErrServerNotReady) {
			http.Error(w, "server not running — start it from JupyterHub first", http.StatusServiceUnavailable)
			return
		}
		h.Log.Printf("ssh-gateway: pod IP lookup failed for %q: %v", username, err)
		http.Error(w, "could not locate server", http.StatusBadGateway)
		return
	}

	sshAddr := net.JoinHostPort(podIP, h.SSHPort)
	h.Log.Printf("ssh-gateway: relaying %q → %s", username, sshAddr)

	// Upgrade to WebSocket and relay
	websocket.Handler(func(ws *websocket.Conn) {
		ws.PayloadType = websocket.BinaryFrame
		h.relay(ws, sshAddr, username)
	}).ServeHTTP(w, r)
}

// relay connects to the SSH sidecar and copies bytes between WebSocket and TCP.
func (h *Handler) relay(ws *websocket.Conn, sshAddr, username string) {
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	sshConn, err := (&net.Dialer{}).DialContext(ctx, "tcp", sshAddr)
	cancel()
	if err != nil {
		h.Log.Printf("ssh-gateway: dial %s for %q: %v", sshAddr, username, err)
		ws.Close()
		return
	}
	defer sshConn.Close()
	defer ws.Close()

	done := make(chan struct{}, 2)

	// WebSocket → SSH sidecar
	go func() {
		_, err := io.Copy(sshConn, ws)
		if err != nil && !isClosedErr(err) {
			h.Log.Printf("ssh-gateway: ws→ssh copy (%s): %v", username, err)
		}
		sshConn.(*net.TCPConn).CloseWrite()
		done <- struct{}{}
	}()

	// SSH sidecar → WebSocket
	go func() {
		_, err := io.Copy(ws, sshConn)
		if err != nil && !isClosedErr(err) {
			h.Log.Printf("ssh-gateway: ssh→ws copy (%s): %v", username, err)
		}
		ws.Close()
		done <- struct{}{}
	}()

	<-done
	<-done
	h.Log.Printf("ssh-gateway: session closed for %q", username)
}

// extractToken reads the Bearer token from Authorization header or ?token= query param.
func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth != "" {
		if t, ok := strings.CutPrefix(auth, "token "); ok {
			return t
		}
		if t, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return t
		}
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}

// isClosedErr reports whether err is a benign connection-closed error.
func isClosedErr(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "use of closed network connection") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection reset by peer")
}

// Mux registers the gateway handler onto an http.ServeMux.
func (h *Handler) Mux(mux *http.ServeMux) {
	mux.Handle("/ssh/", h)
}

// ServerPodIPFunc is a function that returns the pod IP for a given username.
// Used in tests to replace the real hub lookup.
type ServerPodIPFunc func(ctx context.Context, username string) (string, error)

// HandlerWithIPFunc creates a Handler that uses ipFunc instead of the hub API.
// For testing only.
func HandlerWithIPFunc(ipFunc ServerPodIPFunc, logger *log.Logger) *testHandler {
	return &testHandler{ipFunc: ipFunc, log: logger}
}

type testHandler struct {
	ipFunc ServerPodIPFunc
	log    *log.Logger
}

func (h *testHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "test handler — not for production")
}
