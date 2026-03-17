"""
SSH key management for JupyterHub users.

Each user gets one RSA or Ed25519 key pair generated on first use.
The public key is stored in JupyterHub's database via the auth_state mechanism
or, when that is not available, in a Kubernetes Secret.

The private key is returned to the user once (for jhub-ssh config-ssh) and
never stored server-side.
"""

import asyncio
import base64
import logging
import os
import subprocess
import tempfile
from typing import Optional, Tuple

log = logging.getLogger(__name__)


def generate_ed25519_keypair() -> Tuple[str, str]:
    """
    Generate an Ed25519 SSH key pair.

    Returns:
        (private_key_pem, public_key_openssh) as strings.

    Uses the `ssh-keygen` binary (available in all standard environments).
    Falls back to `cryptography` library if available.
    """
    try:
        return _generate_with_ssh_keygen()
    except (FileNotFoundError, subprocess.CalledProcessError):
        return _generate_with_cryptography()


def _generate_with_ssh_keygen() -> Tuple[str, str]:
    with tempfile.TemporaryDirectory() as tmpdir:
        key_path = os.path.join(tmpdir, "id_ed25519")
        subprocess.run(
            ["ssh-keygen", "-t", "ed25519", "-f", key_path, "-N", "", "-q", "-C", "jupyterhub"],
            check=True,
            capture_output=True,
        )
        with open(key_path) as f:
            private_key = f.read()
        with open(key_path + ".pub") as f:
            public_key = f.read().strip()
    return private_key, public_key


def _generate_with_cryptography() -> Tuple[str, str]:
    try:
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        from cryptography.hazmat.primitives.serialization import (
            Encoding, PrivateFormat, PublicFormat, NoEncryption,
        )
    except ImportError:
        raise RuntimeError(
            "Neither ssh-keygen nor the 'cryptography' package is available. "
            "Install one of them to use jupyterhub-ssh key management."
        )

    private_key = Ed25519PrivateKey.generate()
    private_pem = private_key.private_bytes(Encoding.PEM, PrivateFormat.OpenSSH, NoEncryption()).decode()

    public_key_bytes = private_key.public_key().public_bytes(Encoding.Raw, PublicFormat.Raw)
    b64 = base64.b64encode(
        b"\x00\x00\x00\x0bssh-ed25519" +
        len(public_key_bytes).to_bytes(4, "big") +
        public_key_bytes
    ).decode()
    public_openssh = f"ssh-ed25519 {b64} jupyterhub"

    return private_pem, public_openssh


class SSHKeyStore:
    """
    Stores per-user SSH public keys.

    Storage backend is pluggable. Default: in-memory dict (for testing).
    Production backends: KubernetesSecretKeyStore, AuthStateKeyStore.
    """

    async def get_public_key(self, username: str) -> Optional[str]:
        raise NotImplementedError

    async def set_public_key(self, username: str, public_key: str) -> None:
        raise NotImplementedError

    async def get_or_create_keypair(self, username: str) -> Tuple[Optional[str], str]:
        """
        Return (private_key, public_key).
        private_key is only returned on first creation; None on subsequent calls.
        """
        existing = await self.get_public_key(username)
        if existing:
            return None, existing
        private_key, public_key = generate_ed25519_keypair()
        await self.set_public_key(username, public_key)
        return private_key, public_key


class InMemoryKeyStore(SSHKeyStore):
    """In-memory key store for testing."""

    def __init__(self):
        self._store: dict = {}

    async def get_public_key(self, username: str) -> Optional[str]:
        return self._store.get(username)

    async def set_public_key(self, username: str, public_key: str) -> None:
        self._store[username] = public_key


class KubernetesSecretKeyStore(SSHKeyStore):
    """
    Stores public keys as Kubernetes Secrets in the hub's namespace.

    Secret name: jhub-ssh-pubkey-<username>
    Secret key:  authorized_keys

    Requires the hub ServiceAccount to have:
      - get/create/update on secrets in its namespace
    """

    def __init__(self, namespace: str = "jupyterhub"):
        self.namespace = namespace
        self._client = None

    def _get_client(self):
        if self._client is None:
            try:
                from kubernetes_asyncio import client, config
                config.load_incluster_config()
                self._client = client.CoreV1Api()
            except Exception as e:
                raise RuntimeError(f"Failed to init Kubernetes client: {e}") from e
        return self._client

    def _secret_name(self, username: str) -> str:
        # Kubernetes names must be lowercase alphanumeric or '-'
        import re
        safe = username.lower()
        safe = re.sub(r'[^a-z0-9]+', '-', safe)
        safe = safe.strip('-')
        return f"jhub-ssh-pubkey-{safe}"

    async def get_public_key(self, username: str) -> Optional[str]:
        from kubernetes_asyncio import client as k8s_client
        v1 = self._get_client()
        try:
            secret = await v1.read_namespaced_secret(
                name=self._secret_name(username),
                namespace=self.namespace,
            )
            data = secret.data or {}
            if "authorized_keys" in data:
                return base64.b64decode(data["authorized_keys"]).decode()
        except k8s_client.ApiException as e:
            if e.status == 404:
                return None
            raise
        return None

    async def set_public_key(self, username: str, public_key: str) -> None:
        from kubernetes_asyncio import client as k8s_client
        v1 = self._get_client()
        secret_name = self._secret_name(username)
        body = k8s_client.V1Secret(
            metadata=k8s_client.V1ObjectMeta(name=secret_name, namespace=self.namespace),
            data={"authorized_keys": base64.b64encode(public_key.encode()).decode()},
        )
        try:
            await v1.create_namespaced_secret(namespace=self.namespace, body=body)
        except k8s_client.ApiException as e:
            if e.status == 409:
                # Already exists — update it
                await v1.replace_namespaced_secret(
                    name=secret_name, namespace=self.namespace, body=body
                )
            else:
                raise
