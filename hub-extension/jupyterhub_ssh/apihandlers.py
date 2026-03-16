"""
JupyterHub API extension handlers for SSH key retrieval.

Adds two endpoints to the JupyterHub hub API:

  GET /hub/api/users/{name}/ssh-key
      Returns the user's SSH private key (once, on first call after key generation).
      Requires: authenticated as that user, or admin.

  GET /hub/api/users/{name}/vscode-connect
      Returns connection info for VS Code Remote SSH.
      Requires: authenticated as that user, or admin.

Register in jupyterhub_config.py:

    from jupyterhub_ssh.apihandlers import load_jupyter_server_extension
    c.JupyterHub.extra_handlers = [
        (r"/api/users/([^/]+)/ssh-key", jupyterhub_ssh.apihandlers.SSHKeyHandler),
        (r"/api/users/([^/]+)/vscode-connect", jupyterhub_ssh.apihandlers.VSCodeConnectHandler),
    ]
"""

import logging
import json

log = logging.getLogger(__name__)

try:
    from jupyterhub.apihandlers.base import APIHandler
    from jupyterhub.scopes import needs_scope
    from tornado import web
    _HAS_JUPYTERHUB = True
except ImportError:
    # Allow importing this module in test environments without JupyterHub installed.
    APIHandler = object
    _HAS_JUPYTERHUB = False


class SSHKeyHandler(APIHandler):
    """
    GET /hub/api/users/{name}/ssh-key

    Returns the SSH private key for the user on first call after key generation.
    The key is stored temporarily on the spawner object and cleared after retrieval.

    Response (200):
        {
            "username": "alice",
            "public_key": "ssh-ed25519 AAAA...",
            "private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\n..."  # or null
        }
    """

    async def get(self, username):
        # Must be authenticated as the user themselves, or an admin
        current_user = await self.get_current_user()
        if current_user is None:
            raise web.HTTPError(403)
        if current_user.name != username and not current_user.admin:
            raise web.HTTPError(403)

        # Look up the user
        user = self.find_user(username)
        if user is None:
            raise web.HTTPError(404, f"No such user: {username}")

        # Get the key store
        key_store = self._get_key_store()
        public_key = await key_store.get_public_key(username)
        if public_key is None:
            raise web.HTTPError(404, "No SSH key registered for this user. Start your server first.")

        # Retrieve and clear the one-time private key from the spawner
        private_key = None
        spawner = user.spawner
        if spawner is not None and hasattr(spawner, "_jhub_ssh_private_key"):
            private_key = spawner._jhub_ssh_private_key
            spawner._jhub_ssh_private_key = None  # Clear after retrieval

        self.set_header("Content-Type", "application/json")
        self.finish(json.dumps({
            "username": username,
            "public_key": public_key,
            "private_key": private_key,
        }))

    def _get_key_store(self):
        # Key store is attached to the application by the extension loader
        return self.settings.get("jhub_ssh_key_store")


class VSCodeConnectHandler(APIHandler):
    """
    GET /hub/api/users/{name}/vscode-connect

    Returns connection info for VS Code Remote SSH.

    Response (200):
        {
            "username": "alice",
            "ssh_host": "alice.jupyter.example.com",
            "ssh_user": "jovyan",
            "vscode_uri": "vscode://ms-vscode-remote.remote-ssh/open?hostName=...",
            "setup_cmd": "jhub-ssh config-ssh --hub https://jupyter.example.com --token <token>"
        }
    """

    async def get(self, username):
        current_user = await self.get_current_user()
        if current_user is None:
            raise web.HTTPError(403)
        if current_user.name != username and not current_user.admin:
            raise web.HTTPError(403)

        user = self.find_user(username)
        if user is None:
            raise web.HTTPError(404, f"No such user: {username}")

        hub_base_url = self.settings.get("jhub_ssh_hub_base_url", "")
        # SSH host: for path-based JupyterHub, users ssh to the hub host;
        # for subdomain-based, could be <username>.jupyter.example.com
        ssh_host = self.settings.get("jhub_ssh_host", self.request.host)

        # Generate a short-lived token scoped to server access only
        token = user.new_api_token(
            note="vscode-connect (short-lived)",
            expires_in=3600,  # 1 hour
            roles=[],
            scopes=[f"access:servers!user={username}"],
        )

        # URI handled by the "JupyterHub Remote" VS Code extension (groundsada.jhub-vscode).
        # VS Code will prompt to install the extension if it isn't present yet.
        from urllib.parse import urlencode, quote
        vscode_uri = (
            "vscode://groundsada.jhub-vscode/connect?"
            + urlencode({
                "hub": hub_base_url,
                "token": token,
                "user": username,
                "folder": "/home/jovyan",
            })
        )

        self.set_header("Content-Type", "application/json")
        self.finish(json.dumps({
            "username": username,
            "ssh_host": ssh_host,
            "ssh_user": "jovyan",
            "vscode_uri": vscode_uri,
            "setup_cmd": setup_cmd,
        }))


def get_extra_handlers(key_store=None, hub_base_url="", ssh_host=""):
    """
    Return (pattern, handler, kwargs) tuples for c.JupyterHub.extra_handlers.

    Usage in jupyterhub_config.py:

        from jupyterhub_ssh.apihandlers import get_extra_handlers
        c.JupyterHub.extra_handlers = get_extra_handlers(
            hub_base_url="https://jupyter.example.com",
            ssh_host="jupyter.example.com",
        )
    """
    settings = {
        "jhub_ssh_key_store": key_store,
        "jhub_ssh_hub_base_url": hub_base_url,
        "jhub_ssh_host": ssh_host,
    }
    return [
        (r"/api/users/([^/]+)/ssh-key", SSHKeyHandler, settings),
        (r"/api/users/([^/]+)/vscode-connect", VSCodeConnectHandler, settings),
    ]
