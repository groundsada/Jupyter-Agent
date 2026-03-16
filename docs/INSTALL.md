# Installation Guide

## Prerequisites

- Kubernetes cluster with JupyterHub already installed via the [Zero to JupyterHub Helm chart](https://z2jh.jupyter.org)
- `kubectl` and `helm` configured for your cluster
- A domain name with DNS you control (e.g. `jupyter.example.com`)
- `cert-manager` v1.x installed (for wildcard TLS)
- ingress-nginx controller installed

---

## Step 1: DNS Setup

Add a wildcard A record pointing to your ingress load balancer IP:

```
*.jupyter.example.com  A  <INGRESS_LB_IP>
jupyter.example.com    A  <INGRESS_LB_IP>
```

Find your ingress IP:
```bash
kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

---

## Step 2: cert-manager Wildcard Certificate

Edit `k8s/cert-manager.yaml`:
1. Replace `<YOUR_EMAIL>` with your Let's Encrypt email
2. Uncomment and configure your DNS provider section (Route53, Cloudflare, etc.)

Apply:
```bash
kubectl apply -f k8s/cert-manager.yaml
```

Wait for the certificate to be issued:
```bash
kubectl get certificate -n jupyterhub jupyter-wildcard-tls -w
# STATUS: Ready = True
```

---

## Step 3: Build and Push Docker Images

```bash
# SSH sidecar
docker build -t ghcr.io/groundsada/jhub-ssh-sidecar:0.1.0 ./sidecar/
docker push ghcr.io/groundsada/jhub-ssh-sidecar:0.1.0

# SSH gateway + port-forwarding proxy
docker build -t ghcr.io/groundsada/jhub-ssh-gateway:0.1.0 ./gateway/
docker push ghcr.io/groundsada/jhub-ssh-gateway:0.1.0
```

---

## Step 4: Install the jupyterhub-ssh Python Package in Your Hub Image

Add to your JupyterHub hub image Dockerfile:
```dockerfile
RUN pip install /path/to/jupyterhub-ext/
```

Or install from PyPI (once published):
```dockerfile
RUN pip install jupyterhub-ssh==0.1.0
```

---

## Step 5: Create a JupyterHub Service Token for the Gateway

```bash
# Create a token with admin read access
kubectl exec -n jupyterhub deploy/hub -- jupyterhub token --user admin --note "jhub-ssh-gateway"
```

Store it as a Kubernetes secret:
```bash
kubectl create secret generic jhub-ssh-gateway-token \
  --namespace jupyterhub \
  --from-literal=token=<TOKEN>
```

---

## Step 6: Install the Helm Chart

```bash
helm install jupyterhub-ssh ./helm/jupyterhub-ssh/ \
  --namespace jupyterhub \
  --set hub.baseUrl=https://jupyter.example.com \
  --set domain.base=jupyter.example.com \
  --set hub.adminTokenSecretRef.name=jhub-ssh-gateway-token \
  --set hub.adminTokenSecretRef.key=token \
  --set gateway.image.repository=ghcr.io/groundsada/jhub-ssh-gateway \
  --set sidecar.image.repository=ghcr.io/groundsada/jhub-ssh-sidecar
```

---

## Step 7: Configure JupyterHub

Add to your `jupyterhub_config.py` (or your JupyterHub Helm values `hub.extraConfig`):

```python
from jupyterhub_ssh.hooks import make_ssh_pre_spawn_hook
from jupyterhub_ssh.apihandlers import get_extra_handlers

c.JupyterHub.subdomain_host = "https://jupyter.example.com"
c.JupyterHub.cookie_options = {"domain": ".jupyter.example.com"}

c.KubeSpawner.pre_spawn_hook = make_ssh_pre_spawn_hook(
    sidecar_image="ghcr.io/groundsada/jhub-ssh-sidecar:0.1.0",
    namespace="jupyterhub",
)

c.JupyterHub.extra_handlers = get_extra_handlers(
    hub_base_url="https://jupyter.example.com",
    ssh_host="jupyter.example.com",
)
```

Restart the hub:
```bash
kubectl rollout restart deploy/hub -n jupyterhub
```

---

## Step 8: Set Up Local SSH Access

On each developer's machine, install the `jhub-ssh` CLI:
```bash
# Download from GitHub releases
curl -Lo jhub-ssh https://github.com/groundsada/jupyter-ssh/releases/latest/download/jhub-ssh-$(uname -s)-$(uname -m)
chmod +x jhub-ssh && sudo mv jhub-ssh /usr/local/bin/

# Configure SSH
jhub-ssh config-ssh \
  --hub https://jupyter.example.com \
  --token <YOUR_JUPYTERHUB_TOKEN>
```

Test SSH access:
```bash
ssh alice@jupyter.example.com
```

---

## Step 9: VS Code Integration

1. Install the [Remote - SSH](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-ssh) extension in VS Code
2. Click **"Open in VS Code"** in JupyterLab (top bar button)
3. On first click, a modal shows the `jhub-ssh config-ssh` setup command — run it if not already done
4. VS Code opens and connects to your server pod

---

## Verify Installation

```bash
# Check gateway pod is running
kubectl get pod -n jupyterhub -l app=jhub-ssh-gateway

# Check ingress
kubectl get ingress -n jupyterhub

# Check certificate
kubectl get certificate -n jupyterhub

# Test port forwarding (replace with your username)
curl -H "Authorization: token <YOUR_TOKEN>" https://8888--alice.jupyter.example.com/api

# Test SSH
ssh alice@jupyter.example.com whoami
```
