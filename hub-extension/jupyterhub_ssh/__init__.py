"""
jupyterhub-ssh — JupyterHub extension for SSH access and VS Code integration.

Provides:
  - pre_spawn_hook: injects SSH sidecar + authorized keys into user pods
  - SSH key management: generate/store/retrieve per-user SSH public keys
  - Hub API extension: /api/users/{name}/vscode-connect endpoint
"""

__version__ = "0.1.0"
