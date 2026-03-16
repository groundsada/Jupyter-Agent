package portfwd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── ParseSubdomain ────────────────────────────────────────────────────────────

func TestParseSubdomain_Valid(t *testing.T) {
	cases := []struct {
		host     string
		wantPort int
		wantUser string
	}{
		{"8888--alice.jupyter.example.com", 8888, "alice"},
		{"3000--bob.jupyter.example.com", 3000, "bob"},
		{"8080--carol-smith.jupyter.example.com", 8080, "carol-smith"},
		{"65535--x.example.com", 65535, "x"},
		{"1024--user.example.com", 1024, "user"},
		// Port specified in host header (nginx passes it)
		{"8888--alice.jupyter.example.com:443", 8888, "alice"},
	}

	for _, tc := range cases {
		sub, err := ParseSubdomain(tc.host)
		if err != nil {
			t.Errorf("ParseSubdomain(%q): unexpected error: %v", tc.host, err)
			continue
		}
		if sub.Port != tc.wantPort {
			t.Errorf("ParseSubdomain(%q).Port = %d, want %d", tc.host, sub.Port, tc.wantPort)
		}
		if sub.Username != tc.wantUser {
			t.Errorf("ParseSubdomain(%q).Username = %q, want %q", tc.host, sub.Username, tc.wantUser)
		}
	}
}

func TestParseSubdomain_Invalid(t *testing.T) {
	cases := []string{
		"alice.jupyter.example.com",     // no port--user separator
		"jupyter.example.com",           // plain domain
		"",                              // empty
		"--alice.example.com",           // empty port
		"8888--.example.com",            // empty username
		"notaport--alice.example.com",   // non-numeric port
	}

	for _, host := range cases {
		_, err := ParseSubdomain(host)
		if err == nil {
			t.Errorf("ParseSubdomain(%q): expected error, got nil", host)
		}
		if err != nil && !errors.Is(err, ErrInvalidSubdomain) && !errors.Is(err, ErrPortForbidden) {
			t.Errorf("ParseSubdomain(%q): unexpected error type: %v", host, err)
		}
	}
}

func TestParseSubdomain_PrivilegedPortBlocked(t *testing.T) {
	cases := []struct {
		host string
		port int
	}{
		{"22--alice.example.com", 22},
		{"80--bob.example.com", 80},
		{"443--carol.example.com", 443},
		{"1023--dave.example.com", 1023},
		{"0--eve.example.com", 0},
	}

	for _, tc := range cases {
		_, err := ParseSubdomain(tc.host)
		if !errors.Is(err, ErrPortForbidden) {
			t.Errorf("ParseSubdomain(%q): expected ErrPortForbidden, got %v", tc.host, err)
		}
	}
}

func TestParseSubdomain_PortOutOfRange(t *testing.T) {
	_, err := ParseSubdomain("65536--alice.example.com")
	if !errors.Is(err, ErrPortForbidden) {
		t.Errorf("expected ErrPortForbidden for port 65536, got %v", err)
	}
}

// ── filterCookies ─────────────────────────────────────────────────────────────

func TestFilterCookies_StripsJupyterHubCookies(t *testing.T) {
	cookies := []*http.Cookie{
		{Name: "jupyterhub-session-id", Value: "abc"},
		{Name: "jupyterhub-user-token", Value: "def"},
		{Name: "my-app-cookie", Value: "ghi"},
		{Name: "other", Value: "jkl"},
	}

	kept := filterCookies(cookies, func(name string) bool {
		return !isJupyterHubCookie(name)
	})

	if len(kept) != 2 {
		t.Errorf("expected 2 kept cookies, got %d", len(kept))
	}
	for _, c := range kept {
		if isJupyterHubCookie(c.Name) {
			t.Errorf("jupyterhub cookie %q was not stripped", c.Name)
		}
	}
}

func isJupyterHubCookie(name string) bool {
	return len(name) >= 11 && name[:11] == "jupyterhub-"
}

// ── extractAuth ───────────────────────────────────────────────────────────────

func TestExtractAuth_TokenHeader(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "token mytoken")
	if got := h.extractAuth(req); got != "mytoken" {
		t.Errorf("got %q, want %q", got, "mytoken")
	}
}

func TestExtractAuth_BearerHeader(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer bearertoken")
	if got := h.extractAuth(req); got != "bearertoken" {
		t.Errorf("got %q, want %q", got, "bearertoken")
	}
}

func TestExtractAuth_QueryParam(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/?jhub_token=qtoken", nil)
	if got := h.extractAuth(req); got != "qtoken" {
		t.Errorf("got %q, want %q", got, "qtoken")
	}
}

func TestExtractAuth_Cookie(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "jupyterhub-session-id", Value: "cookietoken"})
	if got := h.extractAuth(req); got != "cookietoken" {
		t.Errorf("got %q, want %q", got, "cookietoken")
	}
}

func TestExtractAuth_Empty(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := h.extractAuth(req); got != "" {
		t.Errorf("expected empty token, got %q", got)
	}
}

// ── Handler routing ───────────────────────────────────────────────────────────

func TestHandler_PrivilegedPortReturns403(t *testing.T) {
	h := New(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "80--alice.example.com"
	req.Header.Set("Authorization", "token tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestHandler_UnauthenticatedRedirectsToLogin(t *testing.T) {
	h := New(nil, nil)
	h.HubLoginURL = "https://jupyter.example.com/hub/login"
	req := httptest.NewRequest(http.MethodGet, "/mypath", nil)
	req.Host = "8888--alice.example.com"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc == "" {
		t.Error("expected Location header in redirect response")
	}
}

func TestHandler_InvalidSubdomainReturns404(t *testing.T) {
	h := New(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "alice.example.com" // no port--user format
	req.Header.Set("Authorization", "token tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
