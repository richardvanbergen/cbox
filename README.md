# cbox

Sandboxed development environments for Claude Code. Each sandbox runs a Claude container with your workspace mounted, and exposes project commands as MCP tools that run on the host.

## Architecture

```
Host Machine                    Claude Container
                                (debian + Claude CLI)
  MCP Server
    cbox_build ──sh -c──>       calls via MCP tool
    cbox_test  ──sh -c──>       calls via MCP tool
    run_command (git, gh)       calls via MCP tool

                                /workspace (mount)
                                docker.sock (mount)
```

- **Claude container** — Fixed Debian image with Claude Code CLI. `cbox chat` connects here.
- **MCP server** — Runs on the host, exposes named commands (`cbox_build`, `cbox_test`, etc.) and whitelisted host commands (`git`, `gh`) as MCP tools. Claude calls these tools from inside the container.
- **Workspace** — A git worktree mounted into the container. Each branch gets its own isolated worktree.

## Install

```bash
go install github.com/richvanbergen/cbox/cmd/cbox@latest
```

Or build from source:

```bash
git clone https://github.com/richvanbergen/cbox.git
cd cbox
go build -o bin/cbox ./cmd/cbox
```

Requires Docker.

## Shell Completion

cbox supports shell completion for bash, zsh, and fish. Completions include commands, flags, git branches, and config-defined commands.

### Bash

```bash
# Load for current session
source <(cbox completion bash)

# Install permanently (Linux)
cbox completion bash > /etc/bash_completion.d/cbox

# Install permanently (macOS with Homebrew)
cbox completion bash > $(brew --prefix)/etc/bash_completion.d/cbox
```

### Zsh

```bash
# Enable completion (add to ~/.zshrc if not already enabled)
autoload -U compinit; compinit

# Install completion
cbox completion zsh > "${fpath[1]}/_cbox"
```

### Fish

```fish
# Load for current session
cbox completion fish | source

# Install permanently
cbox completion fish > ~/.config/fish/completions/cbox.fish
```

## Quick start

```bash
cd your-project

# Create config
cbox init
# Edit cbox.toml to set your build/test commands

# Start sandbox on a new branch
cbox up feat-my-feature

# Start Claude
cbox chat feat-my-feature

# Run a one-shot prompt
cbox chat feat-my-feature -p "refactor the auth module"

# Shell into the Claude container (for debugging)
cbox shell feat-my-feature

# Stop container (keeps worktree)
cbox down feat-my-feature

# Full cleanup (removes worktree and branch)
cbox clean feat-my-feature
```

## Configuration

Create `cbox.toml` in your project root (`cbox init` generates a starter config):

```toml
env = ["ANTHROPIC_API_KEY"]  # read from host environment
host_commands = ["git", "gh"]

[commands]
build = "npm run build"
test = "npm test"
```

### Fields

| Field | Description |
|---|---|
| `commands` | Named commands exposed as `cbox_<name>` MCP tools (run on the host via `sh -c`) |
| `env` | Environment variable names to pass from host into the Claude container |
| `env_file` | Path to an env file |
| `browser` | Enable Chrome bridge for browser-aware Claude sessions |
| `host_commands` | Commands Claude can run on the host via the `run_command` MCP tool (e.g. `git`, `gh`) |
| `copy_files` | Files or directories to copy from the main project into each new worktree |
| `ports` | Ports to expose from the container to the host (Docker `-p` syntax) |
| `dockerfile` | Path to custom Dockerfile (see `cbox eject`) |
| `open` | Shell command to run before chat (use `$Dir` for worktree path, e.g., `code $Dir`) |
| `editor` | Editor command for editing flow descriptions (e.g., `vim`, `code --wait`) |
| `serve` | Serve process config — see [Serve](#serve-cbox-serve) |

## Commands

### `cbox init`

Creates a default `cbox.toml` in the current directory with placeholder `build` and `test` commands, and `git`/`gh` as default host commands.

### `cbox up <branch>`

Creates a git worktree, builds the Claude image, creates a Docker network, and starts the Claude container. If `commands` or `host_commands` are configured, starts an MCP server on the host. Idempotent — re-running replaces the existing container.

### `cbox down <branch>`

Stops the container, MCP server, and removes the network. Preserves the worktree so you can `cbox up` again.

### `cbox chat <branch>`

Launches Claude Code interactively in the Claude container.

### `cbox chat <branch> -p "<prompt>"`

Runs a one-shot Claude prompt in the Claude container (headless, JSON output).

**Flags:**
- `--open [command]` — Run a command before starting chat (uses `open` config if no command specified; use `$Dir` for worktree path)

### `cbox shell <branch>`

Opens a bash shell in the Claude container. Useful for debugging.

### `cbox open <branch>`

Runs the `open` command configured in `cbox.toml` for the specified sandbox without starting a chat session. Useful for opening your editor or browser to the worktree.

**Flags:**
- `--open <command>` — Override the config and run a custom command (use `$Dir` for worktree path)

### `cbox run <command>`

Runs a named command from the `commands` section of `cbox.toml` directly on the host machine (not via MCP). Useful for local development workflows.

**Example:**
```bash
# With this config:
# [commands]
# test = "go test ./..."

cbox run test  # runs 'go test ./...' on the host
```

### `cbox eject`

Copies the embedded Dockerfile into your project as `Dockerfile.cbox` and updates `cbox.toml` to reference it. Use this when you need to customize the container image (e.g., to install runtimes like Node.js or Python).

After ejecting, you can freely edit `Dockerfile.cbox`. Existing sandboxes need rebuilding with `cbox up --rebuild <branch>`.

### `cbox list`

Lists all tracked sandboxes and their status.

### `cbox info <branch>`

Shows details about a specific sandbox (container name, network, worktree path).

### `cbox clean <branch>`

Stops the container, removes the network, deletes the worktree, and removes the branch.

### `cbox completion [bash|zsh|fish]`

Generates shell completion scripts. See [Shell Completion](#shell-completion) for installation instructions.

## How named commands work

Each entry in `commands` becomes a dedicated MCP tool named `cbox_<name>`. When Claude calls the tool, the MCP server on the host runs `sh -c '<expression>'` in the worktree directory.

For example, with this config:

```toml
[commands]
test = "npm test"
build = "npm run build"
```

Claude sees two MCP tools: `cbox_test` and `cbox_build`. Calling `cbox_test` runs `sh -c 'npm test'` on the host in the worktree directory.

## Host commands

Claude inside the container doesn't have access to host tools like `git` or `gh`. The `host_commands` config whitelists commands that Claude can run on the host machine via the `run_command` MCP tool.

When `host_commands` or `commands` are configured, `cbox up` starts an MCP server on the host. Claude Code in the container connects to it automatically via `.mcp.json`. A system-level `CLAUDE.md` is also injected to tell Claude how to use the available tools.

```toml
host_commands = ["git", "gh"]
```

With this config, Claude can run `git status`, `gh pr create`, etc. on the host via the `run_command` tool. Commands not in the whitelist are rejected.

Path arguments containing `/workspace/...` are automatically translated to the host worktree path, and paths outside the worktree are rejected.

## File Copying

When `cbox up` creates a new worktree, you can automatically copy files or directories from your main project directory into it using the `copy_files` configuration:

```toml
copy_files = [".env", "node_modules", "vendor"]
```

This is useful for:
- **Environment files** (`.env`, `.env.local`) that shouldn't be in git
- **Dependencies** (`node_modules`, `vendor`) to avoid reinstalling on every branch
- **Build artifacts** or cached files that are .gitignored

### How it works

The worktree itself is **mounted** into the container via Docker volume at `/workspace`, so changes are immediately visible on both the host and container—no copying between host and container occurs.

However, when creating a new worktree with `git worktree add`, git only checks out tracked files. The `copy_files` setting lets you supplement each new worktree with additional files from your main project:

1. **Worktree creation** — git creates the worktree with tracked files
2. **File copying** — cbox copies each pattern from the main project to the worktree
3. **Container mount** — the worktree is bind-mounted to `/workspace` in the container

### Behavior

- Patterns are relative to the project root
- Missing files are **silently skipped** (so optional entries like `.env` don't cause errors)
- Both files and directories are supported
- For directories, the entire tree is recursively copied
- File permissions are preserved

### Example

```toml
copy_files = [
    ".env",              # environment secrets
    "node_modules",      # pre-installed dependencies
    ".next",             # Next.js build cache
]
```

With this config, each new worktree will have these files copied from your main project directory, even though they're not in git.

## Custom Dockerfiles

By default, cbox uses a minimal Debian-based image with Claude CLI. If you need additional tools (Node.js, Python, Go, etc.) or system packages in the container, you can customize the Dockerfile:

```bash
# Eject the embedded Dockerfile
cbox eject
```

This creates `Dockerfile.cbox` in your project and updates `cbox.toml`:

```toml
dockerfile = "Dockerfile.cbox"
```

**Example customization:**

```dockerfile
# Add Node.js runtime
FROM node:20-bookworm-slim AS node-base

FROM debian:bookworm-slim
COPY --from=node-base /usr/local/bin/node /usr/local/bin/
COPY --from=node-base /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm

# Rest of the original Dockerfile...
```

After editing, rebuild existing sandboxes:

```bash
cbox up --rebuild <branch>
```

## Port Exposure

By default, cbox containers have no ports mapped to the host. The `ports` config field maps container ports to the host using Docker's standard `-p` syntax:

```toml
ports = ["3000", "8080:80", "127.0.0.1:3000:3000"]
```

| Format | Meaning |
|---|---|
| `"3000"` | Expose container port 3000 on the same host port |
| `"8080:80"` | Map host port 8080 to container port 80 |
| `"127.0.0.1:3000:3000"` | Bind to a specific host interface |

Changing ports requires restarting the sandbox (`cbox down` + `cbox up`).

### Docker-in-Docker

The cbox container has the host Docker socket mounted and the Docker CLI installed, so Claude can run containers inside the sandbox. The `ports` field makes services started this way accessible from your host machine.

**Without eject** — Claude can use `docker run` directly inside the container. Expose the port so you can reach it from the host:

```toml
ports = ["3000"]

[commands]
build = "go build -o app ./cmd/server"
```

Claude can then run the built binary via Docker:

```bash
docker run --rm -v /workspace:/workspace -w /workspace golang:1.23 ./app
```

The app listening on port 3000 inside the container is reachable at `localhost:3000` on your machine.

**With eject** — If your app needs runtimes baked into the container, eject the Dockerfile and install them. The `ports` field works the same way:

```bash
cbox eject
```

Add your runtime to `Dockerfile.cbox`:

```dockerfile
FROM golang:1.23-bookworm AS go-base

FROM debian:bookworm-slim
COPY --from=go-base /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

# Rest of the original Dockerfile...
```

```toml
dockerfile = "Dockerfile.cbox"
ports = ["3000"]

[commands]
build = "go build -o app ./cmd/server"
run = "./app"
```

Now Claude can build and run the app directly inside the container, and port 3000 is accessible on the host. Rebuild after ejecting:

```bash
cbox up --rebuild <branch>
```

## Auto-open Command

The `open` config field lets you automatically run a command when starting a chat session, useful for opening your editor or browser:

```toml
open = "code $Dir"  # Open VS Code in the worktree directory
```

The special variable `$Dir` is replaced with the worktree path at runtime.

**Usage:**

```bash
# Uses the configured open command automatically
cbox chat my-branch --open

# Or override with a custom command
cbox chat my-branch --open "cursor $Dir"

# Run open command without starting chat
cbox open my-branch
```

**Common examples:**

```toml
open = "code $Dir"                           # VS Code
open = "cursor $Dir"                         # Cursor
open = "open -a 'IntelliJ IDEA' $Dir"       # IntelliJ (macOS)
open = "tmux new-session -s cbox -c $Dir"   # tmux session
```

## Serve (`cbox serve`)

When running multiple worktrees of the same app, port conflicts are inevitable (e.g., two branches both trying to bind to port 3000). The `[serve]` config section solves this by automatically allocating random ports and routing traffic through a shared Traefik reverse proxy using hostname-based routing.

### Setup

Add a `[serve]` section to `cbox.toml`:

```toml
[serve]
command = "npm start --port $Port"
```

### How it works

1. `cbox up` (or `cbox serve start`) allocates a random port and substitutes `$Port` in the command
2. A shared Traefik reverse proxy container routes `http://<branch>.<project>.dev.localhost` to the allocated port
3. `cbox down` (or `cbox serve stop`) removes the route; Traefik stops automatically when no routes remain

```
Browser → feature-auth.myapp.dev.localhost:80
            ↓
         Traefik container (shared across branches)
            ↓
         App process on host (random port)
```

The `.dev.localhost` domain resolves to `127.0.0.1` per RFC 6761 — no `/etc/hosts` or DNS configuration needed.

### Port variables

| Variable | Description |
|---|---|
| `$Port` | Primary port — used for Traefik routing |
| `$Port2`, `$Port3`, ... | Additional auto-allocated ports for services that need their own ports |

Use extra port variables when your app has auxiliary services that bind to fixed ports (e.g., dev tools):

```toml
# Allocate a random port for the app AND for TanStack devtools
command = "DEVTOOLS_PORT=$Port2 npm run dev --host 0.0.0.0 --port $Port"
```

If a service needs a specific fixed port, just hardcode it:

```toml
command = "DEVTOOLS_PORT=42069 npm run dev --host 0.0.0.0 --port $Port"
```

### Config fields

```toml
[serve]
command = "npm start --port $Port"  # required: shell command to run
# port = 3000                       # optional: force a fixed primary port (skip random allocation)
# proxy_port = 80                   # optional: override the Traefik listen port
```

### Important: bind to 0.0.0.0

Your app must listen on `0.0.0.0`, not `127.0.0.1`, for Traefik (running in Docker) to reach it. Most dev servers default to localhost, so you'll typically need `--host 0.0.0.0`:

```toml
command = "npm run dev --host 0.0.0.0 --port $Port"
```

### Commands

```bash
# Start serve for an existing sandbox
cbox serve start <branch>

# Stop serve (removes Traefik route)
cbox serve stop <branch>
```

Serve also starts/stops automatically with `cbox up` and `cbox down`.

## Workflow (`cbox flow`)

Workflow orchestration for task-driven development. Creates an issue, spins up a sandbox, and provides the inner Claude with task context — the inner Claude drives the work, the flow system handles issue tracking.

### Setup

```bash
# Add workflow config to cbox.toml (defaults use gh CLI)
cbox flow init
```

### Usage

```bash
# Create issue + sandbox + task context (opens editor for description if not provided)
cbox flow start
# Or provide description inline
cbox flow start -d "Add user authentication"

# Open interactive chat (refreshes task from issue)
cbox flow chat add-user-authentication

# Create PR when done
cbox flow pr add-user-authentication

# Merge and clean up
cbox flow merge add-user-authentication

# Or abandon the flow
cbox flow abandon add-user-authentication

# Check status
cbox flow status
cbox flow status add-user-authentication
```

### Yolo mode

Run fully autonomous — creates issue, starts sandbox, sends a headless prompt, and opens a PR:

```bash
cbox flow start --yolo "Add user authentication"
```

### How it works

1. **`flow start`** — Slugifies the description into a branch name, creates a GitHub issue, starts a sandbox with the `cbox_report` MCP tool enabled, fetches issue content via `gh issue view`, writes a `.cbox-task` file into the worktree, and appends a task pointer to the container's `CLAUDE.md`.

2. **`flow chat`** — Refreshes `.cbox-task` from the latest issue content, then opens an interactive Claude session. The inner Claude reads `/workspace/.cbox-task` for task details and calls `cbox_report` when done.

3. **`flow pr`** / **`flow merge`** — Creates a PR (using done report as description if available), then merges and cleans up.

### Workflow config

The `workflow` section of `cbox.toml` controls issue tracking commands.

You can also configure an `editor` at the top level for editing flow descriptions:

```toml
editor = "vim"  # or "code --wait", "nano", etc.
```

If not configured, defaults to `$EDITOR` environment variable, then falls back to a temporary file approach.

Workflow commands:

```toml
[workflow]
branch = "$Slug"

[workflow.issue]
create = "gh issue create --title \"$Title\" --body \"$Description\" | grep -o '[0-9]*$'"
view = "gh issue view \"$IssueID\" --json number,title,body,labels,state,url"
close = "gh issue close \"$IssueID\""
set_status = "gh issue edit \"$IssueID\" --add-label \"$Status\""
comment = "gh issue comment \"$IssueID\" --body \"$Body\""

[workflow.pr]
create = "gh pr create --title \"$Title\" --body \"$Description\""
view = "gh pr view \"$PRNumber\" --json number,state,title,url,mergedAt"
merge = "gh pr merge \"$PRNumber\" --merge"

[workflow.prompts]
yolo = "custom prompt for --yolo mode (optional)"
```

All commands use shell variable substitution (`$VarName`). Values are passed as environment variables so they're safe from shell metacharacter injection. Replace with your issue tracker's CLI as needed.

## Docker resources

Per sandbox, cbox creates:

- 1 container: `cbox-<project>-<branch>-claude`
- 1 bridge network: `cbox-<project>-<branch>`
- 1 image: `cbox-<project>:claude`
- 1 git worktree directory
- 1 MCP server process (if commands or host_commands are configured)

All are cleaned up by `cbox clean`.
