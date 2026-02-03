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

## Quick start

```bash
cd your-project

# Create config
cbox init
# Edit .cbox.yml to set your build/test commands

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

Create `.cbox.yml` in your project root (`cbox init` generates a starter config):

```yaml
commands:
  build: "npm run build"
  test: "npm test"
env:
  - ANTHROPIC_API_KEY        # read from host environment
host_commands:
  - git
  - gh
```

### Fields

| Field | Description |
|---|---|
| `commands` | Named commands exposed as `cbox_<name>` MCP tools (run on the host via `sh -c`) |
| `env` | Environment variable names to pass from host into the Claude container |
| `env_file` | Path to an env file |
| `browser` | Enable Chrome bridge for browser-aware Claude sessions |
| `host_commands` | Commands Claude can run on the host via the `run_command` MCP tool (e.g. `git`, `gh`) |

## Commands

### `cbox init`

Creates a default `.cbox.yml` in the current directory with placeholder `build` and `test` commands, and `git`/`gh` as default host commands.

### `cbox up <branch>`

Creates a git worktree, builds the Claude image, creates a Docker network, and starts the Claude container. If `commands` or `host_commands` are configured, starts an MCP server on the host. Idempotent — re-running replaces the existing container.

### `cbox down <branch>`

Stops the container, MCP server, and removes the network. Preserves the worktree so you can `cbox up` again.

### `cbox chat <branch>`

Launches Claude Code interactively in the Claude container.

### `cbox chat <branch> -p "<prompt>"`

Runs a one-shot Claude prompt in the Claude container (headless, JSON output).

### `cbox shell <branch>`

Opens a bash shell in the Claude container. Useful for debugging.

### `cbox list`

Lists all tracked sandboxes and their status.

### `cbox info <branch>`

Shows details about a specific sandbox (container name, network, worktree path).

### `cbox clean <branch>`

Stops the container, removes the network, deletes the worktree, and removes the branch.

## How named commands work

Each entry in `commands` becomes a dedicated MCP tool named `cbox_<name>`. When Claude calls the tool, the MCP server on the host runs `sh -c '<expression>'` in the worktree directory.

For example, with this config:

```yaml
commands:
  test: "npm test"
  build: "npm run build"
```

Claude sees two MCP tools: `cbox_test` and `cbox_build`. Calling `cbox_test` runs `sh -c 'npm test'` on the host in the worktree directory.

## Host commands

Claude inside the container doesn't have access to host tools like `git` or `gh`. The `host_commands` config whitelists commands that Claude can run on the host machine via the `run_command` MCP tool.

When `host_commands` or `commands` are configured, `cbox up` starts an MCP server on the host. Claude Code in the container connects to it automatically via `.mcp.json`. A system-level `CLAUDE.md` is also injected to tell Claude how to use the available tools.

```yaml
host_commands:
  - git
  - gh
```

With this config, Claude can run `git status`, `gh pr create`, etc. on the host via the `run_command` tool. Commands not in the whitelist are rejected.

Path arguments containing `/workspace/...` are automatically translated to the host worktree path, and paths outside the worktree are rejected.

## Docker resources

Per sandbox, cbox creates:

- 1 container: `cbox-<project>-<branch>-claude`
- 1 bridge network: `cbox-<project>-<branch>`
- 1 image: `cbox-<project>:claude`
- 1 git worktree directory
- 1 MCP server process (if commands or host_commands are configured)

All are cleaned up by `cbox clean`.
