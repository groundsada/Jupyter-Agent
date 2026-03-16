# jupyterhub_config.py — example configuration for JupyterHub SSH extension
#
# Copy relevant sections into your existing jupyterhub_config.py.
# Replace all placeholder values (jupyter.example.com, etc.)

# ── 1. Subdomain mode ─────────────────────────────────────────────────────────
# Set your base domain. Users' servers will be at username.jupyter.example.com
# and port forwarding will work at 8888--username.jupyter.example.com.
c.JupyterHub.subdomain_host = "https://jupyter.example.com"

# Share the session cookie with all subdomains so the port-forwarding proxy
# can validate the user's session without a redirect round-trip.
c.JupyterHub.cookie_options = {"domain": ".jupyter.example.com"}

# ── 2. SSH spawner hook ───────────────────────────────────────────────────────
from jupyterhub_ssh.hooks import make_ssh_pre_spawn_hook

c.KubeSpawner.pre_spawn_hook = make_ssh_pre_spawn_hook(
    sidecar_image="ghcr.io/groundsada/jhub-ssh-sidecar:latest",
    namespace="jupyterhub",
    ssh_port=2222,
    # If KubeSpawner already mounts a PVC as the user's home volume,
    # set its volume name here so the sidecar can share it.
    # If None, an emptyDir shared volume is used (data is not persisted).
    home_pvc_volume_name=None,  # e.g. "home" or "claim-{username}"
)

# ── 3. Extra API handlers (VS Code connect + SSH key endpoint) ────────────────
from jupyterhub_ssh.apihandlers import get_extra_handlers

c.JupyterHub.extra_handlers = get_extra_handlers(
    hub_base_url="https://jupyter.example.com",
    ssh_host="jupyter.example.com",
)

# ── 4. Services: register the SSH gateway as a managed service (optional) ─────
# If you want JupyterHub to manage the gateway process lifecycle,
# uncomment the block below. Otherwise, deploy the gateway independently
# (recommended for Kubernetes — use gateway-deployment.yaml).
#
# import os
# c.JupyterHub.services = [
#     {
#         "name": "ssh-gateway",
#         "command": ["/usr/local/bin/jhub-ssh", "serve",
#                     "--addr=:8022",
#                     "--hub=https://jupyter.example.com"],
#         "environment": {
#             "JUPYTERHUB_API_TOKEN": os.environ["JHUB_SSH_GATEWAY_TOKEN"],
#         },
#         "oauth_no_confirm": True,
#     }
# ]
