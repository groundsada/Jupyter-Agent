"""
JupyterHub spawner hook — injects SSH sidecar + authorized keys at spawn time.

Usage in jupyterhub_config.py:

    from jupyterhub_ssh.hooks import make_ssh_pre_spawn_hook

    c.KubeSpawner.pre_spawn_hook = make_ssh_pre_spawn_hook(
        sidecar_image="ghcr.io/groundsada/jhub-ssh-sidecar:latest",
        namespace="jupyterhub",
    )
"""

import logging
import sys
import os

log = logging.getLogger(__name__)

# Allow importing sidecar_spec from the sibling sidecar/ directory when
# running tests without installing the full package.
_SIDECAR_SPEC_PATH = os.path.join(
    os.path.dirname(__file__), "..", "..", "sidecar"
)


def _import_sidecar_spec():
    if _SIDECAR_SPEC_PATH not in sys.path:
        sys.path.insert(0, _SIDECAR_SPEC_PATH)
    from sidecar_spec import ssh_sidecar_container, ssh_shared_volume, ssh_shared_volume_mount
    return ssh_sidecar_container, ssh_shared_volume, ssh_shared_volume_mount


def make_ssh_pre_spawn_hook(
    sidecar_image: str = "ghcr.io/groundsada/jhub-ssh-sidecar:latest",
    namespace: str = "jupyterhub",
    ssh_port: int = 2222,
    key_store=None,
    home_pvc_volume_name: str = None,
):
    """
    Factory that returns a pre_spawn_hook coroutine for KubeSpawner.

    Args:
        sidecar_image:       Docker image for the SSH sidecar container.
        namespace:           Kubernetes namespace (for KubernetesSecretKeyStore).
        ssh_port:            Port sshd listens on inside the pod.
        key_store:           SSHKeyStore instance. Defaults to KubernetesSecretKeyStore.
        home_pvc_volume_name: If the user pod already mounts a PVC named this as
                             the home volume, the sidecar will mount the same PVC.
                             If None, an emptyDir shared volume is used.
    """
    if key_store is None:
        from jupyterhub_ssh.keys import KubernetesSecretKeyStore
        key_store = KubernetesSecretKeyStore(namespace=namespace)

    ssh_sidecar_container, ssh_shared_volume, ssh_shared_volume_mount = _import_sidecar_spec()

    async def pre_spawn_hook(spawner):
        username = spawner.user.name
        log.info("jhub-ssh: pre_spawn_hook for user %s", username)

        # ── 1. Get or create SSH public key for this user ─────────────────────
        try:
            private_key, public_key = await key_store.get_or_create_keypair(username)
            if private_key:
                log.info(
                    "jhub-ssh: Generated new SSH key pair for %s. "
                    "Private key available once via /api/users/%s/ssh-key",
                    username, username,
                )
                # Stash private key temporarily so the API handler can return it once.
                # It is never persisted beyond this spawn lifecycle.
                spawner._jhub_ssh_private_key = private_key
            else:
                log.debug("jhub-ssh: Existing SSH key found for %s", username)
                spawner._jhub_ssh_private_key = None
        except Exception:
            log.exception("jhub-ssh: Failed to get/create SSH key for %s — skipping sidecar", username)
            return

        # ── 2. Build sidecar container spec ───────────────────────────────────
        sidecar = ssh_sidecar_container(
            authorized_key=public_key,
            image=sidecar_image,
            ssh_port=ssh_port,
        )

        # ── 3. Attach sidecar to spawner ──────────────────────────────────────
        existing = list(getattr(spawner, "extra_containers", None) or [])
        # Avoid double-adding if hook is called twice (e.g., named servers)
        if not any(c.get("name") == "ssh-sidecar" for c in existing):
            existing.append(sidecar)
        spawner.extra_containers = existing

        # ── 4. Ensure shared home volume exists ───────────────────────────────
        extra_volumes = list(getattr(spawner, "extra_volumes", None) or [])
        extra_volume_mounts = list(getattr(spawner, "extra_volume_mounts", None) or [])

        shared_vol = ssh_shared_volume(existing_home_volume_name=home_pvc_volume_name)
        if shared_vol and not any(v.get("name") == "home" for v in extra_volumes):
            extra_volumes.append(shared_vol)

        # Add the home mount to the notebook container too (if not already there)
        if not any(m.get("name") == "home" for m in extra_volume_mounts):
            extra_volume_mounts.append(ssh_shared_volume_mount())

        spawner.extra_volumes = extra_volumes
        spawner.extra_volume_mounts = extra_volume_mounts

        # ── 5. Expose SSH port info via environment ────────────────────────────
        env = dict(getattr(spawner, "environment", None) or {})
        env["JHUB_SSH_PORT"] = str(ssh_port)
        spawner.environment = env

        log.info(
            "jhub-ssh: Attached SSH sidecar (port %d) to %s's pod",
            ssh_port, username,
        )

    return pre_spawn_hook
