"""
Kubernetes sidecar container spec for the JupyterHub SSH sidecar.

This module is imported by the spawner hook (jupyterhub-ext/jupyterhub_ssh/hooks.py).
It returns the dict structures that KubeSpawner accepts for:
  - extra_containers
  - extra_volumes
  - extra_volume_mounts
"""

from typing import Optional


def ssh_sidecar_container(
    authorized_key: Optional[str],
    image: str = "ghcr.io/groundsada/jhub-ssh-sidecar:latest",
    ssh_port: int = 2222,
    cpu_limit: str = "100m",
    memory_limit: str = "64Mi",
    cpu_request: str = "10m",
    memory_request: str = "16Mi",
) -> dict:
    """
    Return a Kubernetes container spec dict for the SSH sidecar.

    Args:
        authorized_key: The user's SSH public key string (e.g. "ssh-ed25519 AAAA...").
                        Passed as JHUB_SSH_AUTHORIZED_KEY env var.
        image:          Sidecar image reference.
        ssh_port:       Port sshd listens on inside the pod (default 2222).
        *_limit/request: Resource constraints.

    Returns:
        A dict matching the Kubernetes container spec schema, usable directly
        as an element of KubeSpawner.extra_containers.
    """
    env = []
    if authorized_key:
        env.append({
            "name": "JHUB_SSH_AUTHORIZED_KEY",
            "value": authorized_key,
        })

    return {
        "name": "ssh-sidecar",
        "image": image,
        "ports": [
            {
                "name": "ssh",
                "containerPort": ssh_port,
                "protocol": "TCP",
            }
        ],
        "env": env,
        "resources": {
            "limits": {"cpu": cpu_limit, "memory": memory_limit},
            "requests": {"cpu": cpu_request, "memory": memory_request},
        },
        "volumeMounts": [
            {
                # Shared home dir — notebook container also mounts this.
                # The sidecar reads/writes /home/jovyan just like the notebook.
                "name": "home",
                "mountPath": "/home/jovyan",
            }
        ],
        "readinessProbe": {
            "tcpSocket": {"port": ssh_port},
            "initialDelaySeconds": 3,
            "periodSeconds": 10,
            "failureThreshold": 3,
        },
        "livenessProbe": {
            "tcpSocket": {"port": ssh_port},
            "initialDelaySeconds": 10,
            "periodSeconds": 30,
            "failureThreshold": 3,
        },
        # sshd needs root to bind + setuid; drops to jovyan for sessions.
        # If your cluster policy forbids root sidecars, use the Go sidecar
        # variant (Task 2 alt) which runs fully non-root.
        "securityContext": {
            "runAsUser": 0,
            "allowPrivilegeEscalation": False,
            "capabilities": {
                # sshd needs SETUID/SETGID to drop privileges to the session user.
                "add": ["SETUID", "SETGID"],
                "drop": ["ALL"],
            },
        },
    }


def ssh_shared_volume(existing_home_volume_name: Optional[str] = None) -> Optional[dict]:
    """
    Return an extra volume spec if the notebook container does not already
    provide a 'home' volume.

    If KubeSpawner already mounts a PVC as 'home', pass its name here and
    this returns None (no extra volume needed — sidecar just mounts it too).

    Args:
        existing_home_volume_name: Name of the existing PVC volume, if any.

    Returns:
        A Kubernetes volume dict, or None if not needed.
    """
    if existing_home_volume_name:
        # The PVC volume already exists on the pod; sidecar will mount it.
        return None
    # No persistent home — use an emptyDir shared between containers.
    return {
        "name": "home",
        "emptyDir": {},
    }


def ssh_shared_volume_mount(mount_path: str = "/home/jovyan") -> dict:
    """Return the volumeMount dict for the notebook container's home dir."""
    return {
        "name": "home",
        "mountPath": mount_path,
    }
