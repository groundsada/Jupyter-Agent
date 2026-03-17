"""
JupyterHub API extension handlers for SSH key retrieval.

Adds two endpoints to the JupyterHub hub API:

  GET /hub/api/users/{name}/ssh-key
      Returns the user's SSH public key (and one-time private key after generation).
      Requires: authenticated as that user, or admin.

  GET /hub/api/users/{name}/vscode-connect
      Returns a vscode:// URI for one-click VS Code Remote SSH.
      Requires: authenticated as that user, or admin.

Register in jupyterhub_config.py:

    from jupyterhub_ssh.apihandlers import get_extra_handlers
    from jupyterhub_ssh.keys import KubernetesSecretKeyStore

    key_store = KubernetesSecretKeyStore(namespace="mynamespace")
    c.JupyterHub.extra_handlers = get_extra_handlers(
        key_store=key_store,
        hub_base_url="https://jupyter.example.com",
    )
"""

import logging
import json
from urllib.parse import urlencode

log = logging.getLogger(__name__)

try:
    from jupyterhub.apihandlers.base import APIHandler
    from tornado import web
    _HAS_JUPYTERHUB = True
except ImportError:
    APIHandler = object
    _HAS_JUPYTERHUB = False


class SSHKeyHandler(APIHandler):
    """
    GET /hub/api/users/{name}/ssh-key

    Returns the SSH public key and (once) the private key after generation.
    """

    def initialize(self, key_store=None, hub_base_url="", ssh_host=""):
        self._key_store = key_store
        self._hub_base_url = hub_base_url
        self._ssh_host = ssh_host

    async def get(self, username):
        current_user = await self.get_current_user()
        if current_user is None:
            raise web.HTTPError(403)
        if current_user.name != username and not current_user.admin:
            raise web.HTTPError(403)

        user = self.find_user(username)
        if user is None:
            raise web.HTTPError(404, f"No such user: {username}")

        public_key = await self._key_store.get_public_key(username)
        if public_key is None:
            raise web.HTTPError(404, "No SSH key registered for this user. Start your server first.")

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


class VSCodeConnectHandler(APIHandler):
    """
    GET /hub/api/users/{name}/vscode-connect

    Returns a vscode:// URI for one-click VS Code Remote SSH.
    """

    def initialize(self, key_store=None, hub_base_url="", ssh_host=""):
        self._key_store = key_store
        self._hub_base_url = hub_base_url
        self._ssh_host = ssh_host

    async def get(self, username):
        current_user = await self.get_current_user()
        if current_user is None:
            raise web.HTTPError(403)
        if current_user.name != username and not current_user.admin:
            raise web.HTTPError(403)

        user = self.find_user(username)
        if user is None:
            raise web.HTTPError(404, f"No such user: {username}")

        hub_base_url = self._hub_base_url or self.request.protocol + "://" + self.request.host

        token = user.new_api_token(
            note="vscode-connect (short-lived)",
            expires_in=3600,
            roles=[],
            scopes=[f"access:servers!user={username}"],
        )

        vscode_uri = (
            "vscode://groundsada.jhub-vscode/connect?"
            + urlencode({
                "hub": hub_base_url,
                "token": token,
                "user": username,
                "folder": "/home/jovyan",
            })
        )

        ssh_host = self._ssh_host or self.request.host

        self.set_header("Content-Type", "application/json")
        self.finish(json.dumps({
            "username": username,
            "ssh_host": ssh_host,
            "ssh_user": "jovyan",
            "vscode_uri": vscode_uri,
        }))


def get_extra_handlers(key_store=None, hub_base_url="", ssh_host=""):
    """
    Return (pattern, handler, kwargs) tuples for c.JupyterHub.extra_handlers.
    """
    kwargs = {
        "key_store": key_store,
        "hub_base_url": hub_base_url,
        "ssh_host": ssh_host,
    }
    return [
        (r"/api/users/([^/]+)/ssh-key", SSHKeyHandler, kwargs),
        (r"/api/users/([^/]+)/vscode-connect", VSCodeConnectHandler, kwargs),
    ]
