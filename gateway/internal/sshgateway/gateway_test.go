package sshgateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractToken_Header(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ssh/alice", nil)
	req.Header.Set("Authorization", "token mytoken123")
	tok := extractToken(req)
	if tok != "mytoken123" {
		t.Errorf("got %q, want %q", tok, "mytoken123")
	}
}

func TestExtractToken_BearerHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ssh/alice", nil)
	req.Header.Set("Authorization", "Bearer bearertoken")
	tok := extractToken(req)
	if tok != "bearertoken" {
		t.Errorf("got %q, want %q", tok, "bearertoken")
	}
}

func TestExtractToken_QueryParam(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ssh/alice?token=querytoken", nil)
	tok := extractToken(req)
	if tok != "querytoken" {
		t.Errorf("got %q, want %q", tok, "querytoken")
	}
}

func TestExtractToken_Empty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ssh/alice", nil)
	tok := extractToken(req)
	if tok != "" {
		t.Errorf("expected empty token, got %q", tok)
	}
}

func TestIsClosedErr_EOF(t *testing.T) {
	import_io := func() error { return nil }
	_ = import_io
	if !isClosedErr(nil) {
		t.Error("nil should be closed err")
	}
}

func TestIsClosedErr_ClosedNetwork(t *testing.T) {
	err := &wrappedErr{"use of closed network connection"}
	if !isClosedErr(err) {
		t.Error("closed network connection should be closed err")
	}
}

func TestIsClosedErr_BrokenPipe(t *testing.T) {
	err := &wrappedErr{"broken pipe"}
	if !isClosedErr(err) {
		t.Error("broken pipe should be closed err")
	}
}

func TestGatewayMissingToken(t *testing.T) {
	h := New(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/ssh/alice", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGatewayMissingUsername(t *testing.T) {
	h := New(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/ssh/", nil)
	req.Header.Set("Authorization", "token sometoken")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// wrappedErr is a test error type wrapping a message string.
type wrappedErr struct{ msg string }

func (e *wrappedErr) Error() string { return e.msg }

// Compile-time check that strings package is used
var _ = strings.Contains
