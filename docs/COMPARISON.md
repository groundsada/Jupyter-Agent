# Feature Parity: JupyterHub SSH vs Coder

| Feature | Coder | This project (JupyterHub SSH) | Notes |
|---|---|---|---|
| **SSH to workspace** | `coder ssh <workspace>` | `ssh alice@jupyter.example.com` | Both work with only HTTPS outbound |
| **SSH ProxyCommand** | `coder config-ssh` | `jhub-ssh config-ssh` | Writes identical block to `~/.ssh/config` |
| **SSH protocol** | WireGuard/Tailnet overlay | TCP-over-WebSocket | Simpler; no UDP needed |
| **Port forwarding (browser)** | `<port>--<agent>--<ws>.apps.coder.com` | `<port>--<user>.jupyter.example.com` | Both use wildcard ingress |
| **Port forwarding (CLI)** | `coder port-forward` | `ssh -L <port>:localhost:<port> alice@...` | SSH TCP forwarding covers this |
| **WebSocket proxying** | Yes | Yes | Both preserve WS upgrade headers |
| **SFTP** | Via SSH | Via SSH (OpenSSH `sftp-server`) | |
| **VS Code connect** | Custom VS Code extension | Standard Remote-SSH + `jhub-ssh config-ssh` | No custom extension needed |
| **"Open in VS Code" button** | JupyterLab/Web button | JupyterLab top bar button | Same UX pattern |
| **VS Code URI scheme** | `vscode://coder.coder-remote/open` | `vscode://ms-vscode-remote.remote-ssh/open` | Standard Remote-SSH protocol |
| **JetBrains Gateway** | Supported (Enterprise) | Not in v1 scope | Could add via same SSH path |
| **Cursor editor** | Supported | Possible (same SSH config) | Any Remote-SSH compatible editor works |
| **Auth: token** | Coder API token | JupyterHub API token | |
| **Auth: cookie** | Session cookie | JupyterHub session cookie (shared via `.domain`) | |
| **Port restrictions** | Port ≥ 1024 | Port ≥ 1024 | Same security policy |
| **Multi-user isolation** | RBAC + token scoping | JupyterHub token scoping + NetworkPolicy | |
| **Overlay network** | WireGuard (Tailnet) | None (in-cluster TCP is sufficient) | |
| **Multi-region proxies** | Workspace proxies (Enterprise) | Not in v1 scope | |
| **Kubernetes native** | Yes | Yes | Both use KubeSpawner |
| **Helm chart** | Yes | Yes (`helm/jupyterhub-ssh/`) | |
| **Wildcard TLS** | Yes (cert-manager) | Yes (cert-manager) | |
| **Audit logging** | Yes (Enterprise) | Not in v1 scope | |
| **GPU / resource quotas** | Via workspace templates | Via KubeSpawner profiles | |

## Architecture Differences

### Networking
Coder uses a WireGuard overlay (Tailnet/DERP) which works even when pods cannot
accept inbound TCP connections. This project uses direct in-cluster TCP from the
gateway pod to the user pod's SSH sidecar — simpler but requires network policy
to be set up correctly.

### SSH Daemon
Coder uses a custom Go SSH server (`gliderlabs/ssh`) inside the workspace agent,
giving it fine-grained control over sessions. This project uses standard OpenSSH
(`sshd`) in a sidecar container, which is more conservative but battle-tested and
supports all standard SSH features (agent forwarding, X11, SFTP, etc.).

### Token Routing
Coder encodes signed JWTs in subdomain requests (app tokens). This project uses
JupyterHub's native session cookie / API token, validated against the hub's API.
This avoids a custom JWT infrastructure but adds a round-trip to the hub API per
unauthenticated request.
