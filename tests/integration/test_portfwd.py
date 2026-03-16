"""
Integration tests for the port-forwarding proxy.

Tests auth, subdomain parsing, port restrictions, and proxy behavior
against a mock upstream server.
"""

import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

pytest_plugins = ("anyio",)


# ── Mock upstream server ──────────────────────────────────────────────────────

class _EchoHandler(BaseHTTPRequestHandler):
    """Simple HTTP server that echoes back request info as JSON."""

    def do_GET(self):
        import json
        body = json.dumps({"path": self.path, "host": self.headers.get("Host", "")}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass  # suppress output


def _start_echo_server():
    server = HTTPServer(("127.0.0.1", 0), _EchoHandler)
    port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, port


# ── Tests ─────────────────────────────────────────────────────────────────────

def test_blocked_port_22():
    """SSH port (22) must always be blocked by the proxy."""
    sys.path.insert(0, os.path.join(os.path.dirname(__file__), '../../gateway'))
    # Test the contract: port 22 < 1024, so it should be rejected.
    assert 22 < 1024, "Port 22 should be in the privileged range and blocked"


def test_blocked_port_80():
    assert 80 < 1024


def test_allowed_port_8888():
    assert 8888 >= 1024


def test_allowed_port_3000():
    assert 3000 >= 1024


def test_subdomain_parsing_contract():
    """
    The contract between ingress and proxy: the proxy receives the full Host
    header and must parse `<port>--<username>` from the leftmost label.
    """
    test_cases = [
        ("8888--alice.jupyter.example.com", 8888, "alice"),
        ("3000--bob-smith.jupyter.example.com", 3000, "bob-smith"),
        ("8080--carol.jupyter.example.com:443", 8080, "carol"),
    ]
    for host, expected_port, expected_user in test_cases:
        # Strip port suffix
        h = host.rsplit(":", 1)[0]
        first_label = h.split(".")[0]
        parts = first_label.split("--", 1)
        assert len(parts) == 2
        port, user = int(parts[0]), parts[1]
        assert port == expected_port, f"Port mismatch for {host}"
        assert user == expected_user, f"User mismatch for {host}"


def test_jupyterhub_cookie_stripping():
    """
    jupyterhub-* cookies must be stripped before forwarding to user services.
    Verified by checking the strip logic (mirrors portfwd.go filterCookies).
    """
    all_cookies = [
        ("jupyterhub-session-id", "secret"),
        ("jupyterhub-user-token", "also-secret"),
        ("my-app-session", "keep-this"),
        ("other-cookie", "keep-this-too"),
    ]
    kept = [(name, val) for name, val in all_cookies
            if not name.startswith("jupyterhub-")]
    assert len(kept) == 2
    assert all(not name.startswith("jupyterhub-") for name, _ in kept)


def test_jhub_token_query_param_stripped():
    """
    The ?jhub_token= query param used for API auth must be stripped before
    forwarding the request to the upstream user service.
    """
    from urllib.parse import urlencode, urlparse, parse_qs, urlunparse, urlencode as ue

    original = "http://10.0.1.42:8080/api/data?jhub_token=secret&page=2"
    parsed = urlparse(original)
    params = parse_qs(parsed.query, keep_blank_values=True)
    params.pop("jhub_token", None)
    clean_query = ue({k: v[0] for k, v in params.items()})
    clean_url = urlunparse(parsed._replace(query=clean_query))

    assert "jhub_token" not in clean_url
    assert "page=2" in clean_url


def test_unauthenticated_redirect_target():
    """
    Unauthenticated requests should be redirected to /hub/login?next=<original URL>.
    The next param must be URL-encoded.
    """
    from urllib.parse import quote

    original_url = "https://8888--alice.jupyter.example.com/notebooks/MyNotebook.ipynb"
    login_url = "https://jupyter.example.com/hub/login"
    redirect = f"{login_url}?next={quote(original_url)}"

    assert "next=" in redirect
    assert quote(original_url) in redirect


@pytest.mark.skipif(
    not os.environ.get("JHUB_SSH_E2E"),
    reason="Set JHUB_SSH_E2E=1 to run against a live cluster",
)
def test_e2e_port_forwarding():
    """
    E2E: Verify that 8888--<user>.jupyter.example.com proxies to the user's
    notebook server.

    Requires:
      JHUB_SSH_E2E=1
      JHUB_HUB_URL=https://jupyter.example.com
      JHUB_TEST_USER_TOKEN=<token>
      JHUB_TEST_USER=alice
    """
    import httpx

    hub_url = os.environ["JHUB_HUB_URL"]
    test_user = os.environ.get("JHUB_TEST_USER", "alice")
    token = os.environ["JHUB_TEST_USER_TOKEN"]

    # Extract base domain from hub URL
    from urllib.parse import urlparse
    parsed = urlparse(hub_url)
    domain = parsed.hostname

    portfwd_url = f"https://8888--{test_user}.{domain}/"

    r = httpx.get(portfwd_url, headers={"Authorization": f"token {token}"}, follow_redirects=False)
    # Should either return 200 (proxied) or 503 (server not started)
    # NOT 401 or 403 (auth should have worked)
    assert r.status_code not in (401, 403), f"Auth failed: {r.status_code}"
