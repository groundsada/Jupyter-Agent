# SSH Sidecar for JupyterHub user pods
# Runs OpenSSH on port 2222 as UID 1000 (jovyan), shares /home/jovyan with
# the notebook container via a shared volume.
#
# Image size target: < 20MB

FROM alpine:3.20

# Install OpenSSH server and sftp-server
RUN apk add --no-cache openssh-server openssh-sftp-server \
    && rm -rf /var/cache/apk/*

# Create the jovyan user matching the default JupyterHub notebook UID/GID
RUN addgroup -g 1000 jovyan \
    && adduser -u 1000 -G jovyan -h /home/jovyan -s /bin/sh -D jovyan

# Install sshd_config
COPY sshd_config /etc/ssh/sshd_config

# Install entrypoint
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# /tmp/ssh: host keys (ephemeral, writable by UID 1000)
# /home/jovyan: shared with notebook container via volume mount
RUN mkdir -p /tmp/ssh /home/jovyan/.ssh \
    && chown -R jovyan:jovyan /tmp/ssh /home/jovyan

# sshd needs to run as root to bind and setuid, but drops privileges to jovyan
# for the actual session. The entrypoint runs as root.
USER root

EXPOSE 2222

ENTRYPOINT ["/entrypoint.sh"]
