# JupyterHub SSH + Port-Forwarding + VS Code Extension — Architecture

## Overview

This document describes the architecture for extending JupyterHub with three features
currently unique to Coder:

1. **SSH access** to user server pods over HTTPS (no raw TCP needed)
2. **Wildcard subdomain port forwarding** (`<port>--<user>.jupyter.example.com`)
3. **VS Code remote development** via local VS Code connecting to user pods

---

## Goals and Non-Goals

### Goals
- Users can SSH into their JupyterHub server from any machine with only HTTPS outbound access
- Users can expose arbitrary ports (≥1024) in their server pod via browser-accessible subdomains
- Local VS Code opens directly in the user's pod workspace via one click
- Everything deploys as an overlay on an existing JupyterHub Kubernetes installation
- No changes to JupyterHub core; all integration is via published extension points

### Non-Goals
- Replacing JupyterHub's existing notebook proxy or auth system
- Supporting non-Kubernetes spawners in v1 (LocalProcessSpawner, DockerSpawner)
- Building a WireGuard/Tailnet overlay (simpler TCP-over-WebSocket is sufficient)

---

## Component Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                        User's Laptop                                │
│                                                                     │
│  ssh alice@jupyter.example.com   Browser: 8888--alice.jupyter...   │
│  (ProxyCommand: jhub-ssh)        VS Code "Open in VS Code" button  │
└────────────┬──────────────────────────┬────────────────────────────┘
             │ HTTPS/WSS                │ HTTPS
             ▼                          ▼
┌────────────────────────────────────────────────────────────────────┐
│              Kubernetes Cluster — Wildcard Ingress                 │
│         *.jupyter.example.com  →  ingress-nginx (TLS terminated)  │
│                                                                    │
│  Path /ssh/*  ──────────────────► SSH Gateway Service  (Task 4)   │
│  Subdomain *.jupyter.example.com ► Port-Fwd Proxy      (Task 5)   │
│  jupyter.example.com  ──────────► JupyterHub Hub        (existing) │
└──────────┬─────────────────────────────┬──────────────────────────┘
           │                             │
           │ TCP to pod:2222             │ HTTP to pod:<port>
           ▼                             ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     User Server Pod                                 │
│                                                                     │
│  ┌──────────────────────┐   ┌──────────────────────────────────┐   │
│  │  Notebook Server     │   │  SSH Sidecar (Task 2)            │   │
│  │  (jupyter-server)    │   │  - OpenSSH or gliderlabs/ssh     │   │
│  │  port 8888           │   │  - port 2222                     │   │
│  │                      │   │  - authorized_keys from hub API  │   │
│  │  any other port...   │   │  - shared home volume            │   │
│  └──────────────────────┘   └──────────────────────────────────┘   │
│                                                                     │
│  Shared PVC: /home/jovyan  (mounted by both containers)            │
└─────────────────────────────────────────────────────────────────────┘

JupyterHub Hub:
  - Spawner hook (Task 3): injects sidecar + SSH keys at spawn time
  - API extension (Task 7): /api/users/{name}/vscode-connect
```

---

## Feature 1: SSH Access over HTTPS

### Flow

```
1. First-time setup (once per developer machine):
   jhub-ssh config-ssh --hub https://jupyter.example.com --token <api-token>
   → Writes to ~/.ssh/config:
       Host *.jupyter.example.com
         ProxyCommand jhub-ssh proxy-connect --hub https://jupyter.example.com %r

2. SSH session:
   ssh alice@jupyter.example.com
     │
     ├─ SSH client invokes ProxyCommand: jhub-ssh proxy-connect alice
     │
     ├─ jhub-ssh opens WebSocket to wss://jupyter.example.com/ssh/alice
     │   (with Authorization: token <api-token> header)
     │
     ├─ SSH Gateway (Task 4):
     │   a. Validates token via JupyterHub API GET /api/users/alice
     │   b. Checks token owner == alice (or is admin)
     │   c. Looks up alice's server pod IP from JupyterHub API
     │   d. Dials TCP to <pod-ip>:2222
     │   e. Bidirectionally copies WebSocket ↔ TCP
     │
     └─ SSH Sidecar (Task 2) on pod:2222:
         a. Receives SSH connection
         b. Validates client public key against authorized_keys
         c. Opens shell / SFTP / port-forward session
```

### Components

#### SSH Sidecar (Task 2)
- **Image**: Alpine-based, ~15MB, runs `sshd` or embedded Go SSH server
- **Port**: 2222 (non-privileged, runs as UID 1000)
- **Authorized keys**: Read from `/home/jovyan/.ssh/authorized_keys` (populated by spawner hook)
- **Volume**: Shares `/home/jovyan` with notebook container via `emptyDir` or same PVC
- **Capabilities**: Interactive shell, SFTP, TCP port forwarding (for tunneling notebook ports locally)

#### Spawner Hook (Task 3)
Registered as `c.KubeSpawner.pre_spawn_hook`:
```python
async def ssh_pre_spawn_hook(spawner):
    # 1. Get or generate user SSH public key from hub API
    pub_key = await get_or_create_user_ssh_pubkey(spawner.user.name)
    # 2. Write to user's authorized_keys via a Kubernetes init container
    #    or store as a Secret and mount it
    spawner.extra_containers = [ssh_sidecar_spec(pub_key)]
    spawner.extra_volumes = [shared_home_volume()]
    spawner.extra_volume_mounts = [shared_home_mount()]
```

#### SSH Gateway (Task 4)
- **Language**: Go (single binary, easy to containerize)
- **Protocol**: Raw TCP-over-WebSocket (RFC 6455 binary frames)
- **Auth**: Validates JupyterHub API token on WebSocket upgrade
- **Routing**: Calls `GET /hub/api/users/{name}` to get pod IP, then dials `:2222`
- **Endpoint**: `wss://jupyter.example.com/ssh/{username}`
- **CLI**: `jhub-ssh` binary with two subcommands:
  - `config-ssh`: writes `~/.ssh/config` ProxyCommand block
  - `proxy-connect`: stdio bridge (reads from stdin, writes to stdout) — used as ProxyCommand

---

## Feature 2: Wildcard Subdomain Port Forwarding

### URL Format

```
<port>--<username>.jupyter.example.com[/<path>]

Examples:
  8888--alice.jupyter.example.com          → alice's pod port 8888 (JupyterLab)
  8080--alice.jupyter.example.com/api/v1   → alice's pod port 8080, path /api/v1
  3000--bob.jupyter.example.com            → bob's pod port 3000 (dev server)
```

### Flow

```
Browser → https://8888--alice.jupyter.example.com
  │
  ├─ Ingress: wildcard *.jupyter.example.com → Port-Fwd Proxy (Task 5)
  │
  ├─ Port-Fwd Proxy:
  │   a. Parse Host header: extract port=8888, username=alice
  │   b. Validate port ≥ 1024
  │   c. Check auth:
  │       - Read JupyterHub session cookie from request
  │       - POST /hub/api/authorizations/token/<cookie-value>
  │       - If invalid → redirect to /hub/login?next=<original-url>
  │   d. Confirm authenticated user == alice (or is admin)
  │   e. GET /hub/api/users/alice → pod IP + server status
  │   f. If server not running → 503 "Server not started"
  │   g. httputil.ReverseProxy → http://<pod-ip>:8888/<path>
  │
  └─ User pod port 8888 responds
```

### Subdomain Parsing Rules

| Input subdomain part | Rule |
|---|---|
| `8888` | Port number — must be integer 1024–65535 |
| `alice` | JupyterHub username |
| Port < 1024 | Rejected — 403 Forbidden |
| Unknown username | 404 Not Found |
| Server not running | 503 with "Start your server" link |

### Auth Cookie Flow (for browsers)

The proxy sits on `*.jupyter.example.com`. JupyterHub sets its session cookie on
`jupyter.example.com` (the apex domain). To share the cookie with subdomains the
hub must set `cookie_options = {"domain": ".jupyter.example.com"}` in `jupyterhub_config.py`.

For API clients (curl, scripts), pass a JupyterHub API token in:
- Header: `Authorization: token <jhub-token>`
- Query param: `?jhub_token=<token>` (proxy reads, validates, strips before forwarding)

---

## Feature 3: VS Code Remote Development

### Flow

```
1. User clicks "Open in VS Code" in JupyterLab (Task 7 extension):
   │
   ├─ JupyterLab extension calls GET /hub/api/users/alice/vscode-connect
   │   Response: {
   │     "ssh_host": "alice.jupyter.example.com",
   │     "ssh_user": "alice",
   │     "vscode_uri": "vscode://ms-vscode-remote.remote-ssh/open?hostName=alice.jupyter.example.com&user=alice",
   │     "setup_cmd": "jhub-ssh config-ssh --hub https://jupyter.example.com --token <token>"
   │   }
   │
   ├─ If ~/.ssh/config not yet configured → show setup_cmd in modal
   │
   └─ Browser navigates to vscode:// URI
       VS Code Remote SSH opens SSH connection to alice.jupyter.example.com
       ProxyCommand: jhub-ssh proxy-connect alice
       → SSH session in pod via SSH Gateway
```

### VS Code SSH Config Entry (written by `jhub-ssh config-ssh`)

```
# BEGIN JUPYTERHUB
Host *.jupyter.example.com
  User jovyan
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
  ProxyCommand jhub-ssh proxy-connect --hub https://jupyter.example.com --token <stored-token> %r
# END JUPYTERHUB
```

The token is stored in the system keychain (or `~/.config/jhub-ssh/token`) and read
at ProxyCommand execution time — it is not embedded in `~/.ssh/config`.

---

## Security Model

### Token Scoping
- **SSH Gateway tokens**: Scoped to `read:users:servers` + `access:servers!user=<name>`
  (JupyterHub token scopes). A user's token cannot access another user's server.
- **Admin tokens**: Can connect to any user's server (for operators).
- **VS Code connect tokens**: Short-lived (1 hour), single-scope tokens issued per session.

### Port Access Controls
- Port < 1024: always blocked at the proxy layer
- Port 22: blocked (SSH is accessed via the gateway, not port forwarding)
- Port 2222: the SSH sidecar port; blocked from external access (only gateway dials it in-cluster)

### Network Policies
```yaml
# User pods: only allow ingress from SSH gateway and port-fwd proxy
kind: NetworkPolicy
spec:
  podSelector: {matchLabels: {component: jupyter}}
  ingress:
    - from: [{podSelector: {matchLabels: {app: jhub-ssh-gateway}}}]
      ports: [{port: 2222}]
    - from: [{podSelector: {matchLabels: {app: jhub-portfwd-proxy}}}]
      ports: [{port: 1024, endPort: 65535, protocol: TCP}]
```

### Key Management
- SSH host keys: generated once per sidecar pod lifecycle (ephemeral; `StrictHostKeyChecking no` on client)
- User authorized keys: stored as a Kubernetes Secret named `jhub-ssh-pubkey-<username>`,
  mounted read-only into sidecar at `/home/jovyan/.ssh/authorized_keys`
- JupyterHub never sees the user's private key

---

## Technology Choices

| Component | Choice | Rationale |
|---|---|---|
| SSH Gateway | Go + `golang.org/x/crypto/ssh` | Single static binary, easy CLI, matches Coder's approach |
| SSH Sidecar | Alpine + OpenSSH | Mature, well-understood, minimal config |
| Port-Fwd Proxy | Go + `net/http/httputil` | Same binary as gateway, efficient reverse proxy |
| JupyterHub hook | Python | Must integrate with JupyterHub Python API |
| JupyterLab extension | TypeScript + React | Standard JupyterLab extension stack |
| Ingress | ingress-nginx | Most common in JupyterHub Kubernetes deployments |
| TLS | cert-manager + Let's Encrypt DNS-01 | Wildcard cert requires DNS challenge |

---

## Repository Layout

```
jupyter-ssh/
├── ARCHITECTURE.md          ← this file
├── gateway/                 ← Go: SSH gateway + port-fwd proxy + jhub-ssh CLI (Tasks 4, 5)
│   ├── cmd/jhub-ssh/        ← CLI entrypoint
│   ├── internal/
│   │   ├── sshgateway/      ← WebSocket↔TCP SSH relay
│   │   ├── portfwd/         ← subdomain parser + reverse proxy
│   │   └── hubclient/       ← JupyterHub API client
│   ├── Dockerfile
│   └── go.mod
├── sidecar/                 ← SSH sidecar container (Task 2)
│   ├── Dockerfile
│   └── sshd_config
├── jupyterhub-ext/          ← Python: spawner hook + hub API extension (Tasks 3, 7)
│   ├── jupyterhub_ssh/
│   │   ├── hooks.py         ← pre_spawn_hook
│   │   ├── apihandlers.py   ← /api/users/{name}/vscode-connect
│   │   └── keys.py          ← SSH key management
│   └── setup.py
├── labextension/            ← TypeScript: JupyterLab "Open in VS Code" button (Task 7)
│   ├── src/
│   └── package.json
├── helm/
│   └── jupyterhub-ssh/      ← Helm chart overlay (Task 9)
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
├── tests/
│   └── integration/         ← End-to-end tests (Task 8)
└── docs/
    ├── INSTALL.md
    ├── SECURITY.md
    └── COMPARISON.md        ← Feature parity vs Coder
```

---

## Deployment Topology

```
Kubernetes Namespace: jupyterhub

Deployments:
  jupyterhub          (existing)
  jhub-ssh-gateway    (new) — SSH gateway + port-fwd proxy in one pod

DaemonSet: (none needed — gateway is centralized)

Services:
  hub                 ClusterIP  :8081   (existing)
  proxy-public        LoadBalancer :80/:443  (existing, or ingress)
  jhub-ssh-gateway    ClusterIP  :8022 (WebSocket), :8080 (port-fwd)

Ingress (ingress-nginx):
  jupyter.example.com             → proxy-public (existing JupyterHub)
  jupyter.example.com/ssh/*       → jhub-ssh-gateway:8022
  *.jupyter.example.com           → jhub-ssh-gateway:8080

cert-manager Certificate:
  dnsNames: ["jupyter.example.com", "*.jupyter.example.com"]
```

---

## Coder Feature Parity

| Feature | Coder | This project |
|---|---|---|
| SSH to workspace | `coder ssh <workspace>` via WireGuard | `ssh user@jupyter.example.com` via WebSocket gateway |
| SSH ProxyCommand | `coder config-ssh` | `jhub-ssh config-ssh` |
| Port forwarding (browser) | `<port>--<ws>.apps.coder.com` | `<port>--<user>.jupyter.example.com` |
| Port forwarding (CLI) | `coder port-forward` | `ssh -L <port>:localhost:<port> ...` (via SSH tunnel) |
| VS Code button | Custom VS Code extension | Standard Remote-SSH extension + config-ssh |
| Overlay network | WireGuard/Tailnet | TCP-over-WebSocket (simpler, sufficient for k8s) |
| Multi-region proxies | Workspace proxies (Enterprise) | Not in scope v1 |
| JetBrains Gateway | Supported | Not in scope v1 |
