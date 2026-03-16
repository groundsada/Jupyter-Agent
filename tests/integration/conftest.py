"""
Integration test fixtures for jhub-ssh.

Tests here run against a mock JupyterHub API and a real sshd server.
Full cluster tests (against a live kind cluster) are tagged @pytest.mark.k8s
and skipped by default.

Run unit/mock tests:
    pytest tests/integration/

Run with real cluster:
    JHUB_SSH_E2E=1 JHUB_HUB_URL=https://... JHUB_ADMIN_TOKEN=... pytest tests/integration/ -m k8s
"""

import asyncio
import os
import socket
import subprocess
import sys
import tempfile
import threading
from typing import Generator, Optional
from unittest.mock import MagicMock, AsyncMock, patch

import pytest

# Allow imports from project packages
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "hub-extension"))
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "sidecar"))

pytest_plugins = ("anyio",)


# ── Fixtures ──────────────────────────────────────────────────────────────────

@pytest.fixture
def hub_url() -> str:
    return os.environ.get("JHUB_HUB_URL", "https://jupyter.example.com")


@pytest.fixture
def admin_token() -> str:
    return os.environ.get("JHUB_ADMIN_TOKEN", "fake-admin-token")


@pytest.fixture
def mock_hub_client():
    """A mock hubclient that returns fake user data."""
    from unittest.mock import MagicMock, AsyncMock

    client = MagicMock()
    client.BaseURL = "https://jupyter.example.com"

    # Default: alice has a running server at 10.0.1.42
    alice_server = {
        "": MagicMock(
            ready=True,
            url="http://10.0.1.42:8888/user/alice/",
        )
    }
    alice = MagicMock(
        name_="alice",
        admin=False,
        servers=alice_server,
    )
    alice.name = "alice"

    async def validate_token(ctx, token):
        if token == "valid-alice-token":
            info = MagicMock()
            info.name = "alice"
            info.scopes = ["access:servers!user=alice"]
            return info
        from jupyterhub_ssh.keys import InMemoryKeyStore  # noqa
        raise Exception("invalid token")

    async def get_user(ctx, username):
        if username == "alice":
            return alice
        raise Exception("user not found")

    client.validate_token = validate_token
    client.get_user = get_user

    return client


@pytest.fixture
def key_store():
    """A fresh InMemoryKeyStore for each test."""
    from jupyterhub_ssh.keys import InMemoryKeyStore
    return InMemoryKeyStore()
