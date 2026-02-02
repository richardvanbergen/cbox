# cbox

Sandboxed development environments for Claude Code. Each sandbox runs two containers sharing a workspace volume — Claude never touches your production image.

## Architecture

```
Claude Container              App Container
(debian + Claude CLI)         (your Dockerfile)

  cbox-run  ──docker exec──>  bun run index.ts
  cbox-test ──docker exec──>  bun test

  /workspace (mount)          /workspace (mount)
  docker.sock (mount)         ports exposed
```

- **Claude container** — Fixed Debian image with Claude Code CLI and Docker client. `cbox chat` connects here. Has generated wrapper scripts (`cbox-run`, `cbox-test`, etc.) that execute commands in the app container.
- **App container** — Your Dockerfile built unmodified, started with `sleep infinity`. Named commands from `.cbox.yml` run inside it.
- **Workspace** — A git worktree mounted into both containers. Each branch gets its own isolated worktree.

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

## Quick start

```bash
cd your-project

# Create config
cbox init
# Edit .cbox.yml to add commands, ports, etc.

# Start sandbox on a new branch
cbox up --branch feat-my-feature

# Start Claude
cbox chat

# Run a one-shot prompt
cbox chat -p "refactor the auth module"

# Shell into the app container
cbox exec

# Run a command in the app container
cbox exec npm test

# Stop containers (keeps worktree)
cbox down

# Full cleanup (removes worktree and branch)
cbox clean
```

## Configuration

Create `.cbox.yml` in your project root:

```yaml
dockerfile: ./Dockerfile
target: release              # optional multi-stage build target
commands:
  run: "bun run index.ts"
  test: "bun test"
  build: "bun build ./index.ts --outdir ./dist"
env:
  - ANTHROPIC_API_KEY        # read from host environment
ports:
  - "3000:3000"
host_commands:
  - git
  - gh
```

### Fields

| Field | Description |
|---|---|
| `dockerfile` | Path to your Dockerfile |
| `target` | Multi-stage build target (optional) |
| `commands` | Named commands that become `cbox-<name>` scripts in the Claude container |
| `env` | Environment variable names to pass from host |
| `env_file` | Path to an env file |
| `ports` | Port mappings for the app container |
| `host_commands` | Commands Claude can run on the host via MCP (e.g. `git`, `gh`) |

## Commands

### `cbox init`

Creates a default `.cbox.yml` in the current directory.

### `cbox up --branch <name>`

Creates a git worktree, builds both images, creates a Docker network, and starts both containers. Idempotent — re-running replaces existing containers.

### `cbox down`

Stops both containers and removes the network. Preserves the worktree so you can `cbox up` again.

### `cbox chat`

Launches Claude Code interactively in the Claude container.

### `cbox chat -p "<prompt>"`

Runs a one-shot Claude prompt in the Claude container (headless, JSON output).

### `cbox exec [command...]`

Runs a command in the app container. With no arguments, opens an interactive shell (prefers bash, falls back to sh).

### `cbox shell`

Opens a bash shell in the Claude container. Useful for debugging the Claude container itself.

### `cbox clean`

Stops containers, removes the network, deletes the worktree, and removes the branch.

## How named commands work

Each entry in `commands` becomes a script at `/home/claude/bin/cbox-<name>` inside the Claude container. When Claude (or you) runs `cbox-run`, it executes:

```bash
docker exec -i <app-container> sh -c 'bun run index.ts'
```

This keeps the Claude container decoupled from your app's runtime — it only needs the Docker CLI to dispatch commands.

## Host commands

Claude inside the container doesn't have access to host tools like `git` or `gh`. The `host_commands` config whitelists commands that Claude can run on the host machine via an MCP server.

When `host_commands` is set, `cbox up` starts an MCP server on the host that exposes a `run_command` tool. Claude Code in the container connects to it automatically via `.mcp.json`. A system-level `CLAUDE.md` is also injected to tell Claude to use the MCP tool instead of running these commands directly.

```yaml
host_commands:
  - git
  - gh
```

With this config, Claude can run `git status`, `gh pr create`, etc. on the host. Commands not in the whitelist are rejected — Claude will tell the user to add the command to `host_commands` in `.cbox.yml`.

Path arguments containing `/workspace/...` are automatically translated to the host worktree path, and paths outside the worktree are rejected.

## Docker resources

Per sandbox, cbox creates:

- 2 containers: `cbox-<project>-<branch>-claude` and `cbox-<project>-<branch>-app`
- 1 bridge network: `cbox-<project>-<branch>`
- 2 images: `cbox-<project>:claude` and `cbox-<project>:app`
- 1 git worktree directory

All are cleaned up by `cbox clean`.
