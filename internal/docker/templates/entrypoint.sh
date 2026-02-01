#!/bin/bash
set -e

# Match the docker socket's GID so the claude user can access it
if [ -S /var/run/docker.sock ]; then
    SOCK_GID=$(stat -c '%g' /var/run/docker.sock)
    if ! getent group "$SOCK_GID" > /dev/null 2>&1; then
        groupadd -g "$SOCK_GID" dockersock
    fi
    SOCK_GROUP=$(getent group "$SOCK_GID" | cut -d: -f1)
    usermod -aG "$SOCK_GROUP" claude
fi

# Write Claude Code credentials from host keychain into container
if [ -n "$CLAUDE_CODE_CREDENTIALS" ]; then
    echo "$CLAUDE_CODE_CREDENTIALS" > /home/claude/.claude/.credentials.json
    chown claude:claude /home/claude/.claude/.credentials.json
    chmod 600 /home/claude/.claude/.credentials.json
fi

exec gosu claude "$@"
