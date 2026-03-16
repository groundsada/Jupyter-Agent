#!/bin/sh
# SSH Sidecar entrypoint
# Generates host keys, writes authorized_keys, starts sshd.
set -e

SSH_DIR=/tmp/ssh
HOME_SSH=/home/jovyan/.ssh

# ── 1. Generate ephemeral host keys ──────────────────────────────────────────
mkdir -p "$SSH_DIR"
if [ ! -f "$SSH_DIR/ssh_host_ed25519_key" ]; then
    ssh-keygen -t ed25519 -f "$SSH_DIR/ssh_host_ed25519_key" -N "" -q
fi
if [ ! -f "$SSH_DIR/ssh_host_rsa_key" ]; then
    ssh-keygen -t rsa -b 4096 -f "$SSH_DIR/ssh_host_rsa_key" -N "" -q
fi
chmod 600 "$SSH_DIR"/ssh_host_*_key

# ── 2. Write authorized_keys ─────────────────────────────────────────────────
# JHUB_SSH_AUTHORIZED_KEY env var is set by the spawner hook (Task 3).
# It contains the user's SSH public key (one line, base64 encoded pubkey).
mkdir -p "$HOME_SSH"
chmod 700 "$HOME_SSH"

if [ -n "$JHUB_SSH_AUTHORIZED_KEY" ]; then
    printf '%s\n' "$JHUB_SSH_AUTHORIZED_KEY" > "$HOME_SSH/authorized_keys"
elif [ -f "$HOME_SSH/authorized_keys" ]; then
    # Already exists (e.g., mounted from a Secret) — leave it alone
    :
else
    echo "[sidecar] WARNING: No authorized key provided. SSH auth will fail." >&2
    touch "$HOME_SSH/authorized_keys"
fi
chmod 600 "$HOME_SSH/authorized_keys"

echo "[sidecar] authorized_keys contains $(wc -l < "$HOME_SSH/authorized_keys") key(s)"

# ── 3. Start sshd in foreground ──────────────────────────────────────────────
echo "[sidecar] Starting sshd on port 2222"
exec /usr/sbin/sshd -D -e -f /etc/ssh/sshd_config
