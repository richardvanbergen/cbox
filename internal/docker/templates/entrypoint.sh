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

# Set up Chrome bridge socket relays via socat if mappings are provided
if [ -n "$CHROME_BRIDGE_MAPPINGS" ]; then
    BRIDGE_DIR="/tmp/claude-mcp-browser-bridge-${USER}"
    mkdir -p "$BRIDGE_DIR"
    chown claude:claude "$BRIDGE_DIR"
    chmod 700 "$BRIDGE_DIR"

    # Parse JSON mappings without jq: extract socket_name and tcp_port pairs
    # Input format: [{"socket_name":"86155.sock","tcp_port":49321},...]
    # Use process substitution instead of a pipeline so socat processes are
    # children of the main shell and survive the exec into gosu.
    while IFS= read -r entry; do
        SOCK_NAME=$(echo "$entry" | sed -n 's/.*"socket_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
        TCP_PORT=$(echo "$entry" | sed -n 's/.*"tcp_port"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p')

        if [ -n "$SOCK_NAME" ] && [ -n "$TCP_PORT" ]; then
            SOCK_PATH="$BRIDGE_DIR/$SOCK_NAME"
            # Remove stale socket file if it exists
            rm -f "$SOCK_PATH"
            socat UNIX-LISTEN:"$SOCK_PATH",fork,mode=0600,user=claude \
                  TCP:host.docker.internal:"$TCP_PORT" &
        fi
    done < <(echo "$CHROME_BRIDGE_MAPPINGS" | tr '{}' '\n')

    # Brief wait for socat processes to create socket files
    sleep 0.2
fi

exec gosu claude "$@"
