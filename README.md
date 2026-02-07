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

### `cbox completion [bash|zsh|fish]`

Generates shell completion scripts. See [Shell Completion](#shell-completion) for installation instructions.

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

## Workflow (`cbox flow`)

Workflow orchestration for task-driven development. Creates an issue, spins up a sandbox, and provides the inner Claude with task context — the inner Claude drives the work, the flow system handles issue tracking.

### Setup

```bash
# Add workflow config to .cbox.yml (defaults use gh CLI)
cbox flow init
```

### Usage

```bash
# Create issue + sandbox + task context
cbox flow start "Add user authentication"

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

The `workflow` section of `.cbox.yml` controls issue tracking commands:

```yaml
workflow:
  branch: "{{.Slug}}"
  issue:
    create: 'gh issue create --title "{{.Title}}" --body "{{.Description}}" | grep -o ''[0-9]*$'''
    view: 'gh issue view {{.IssueID}}'
    set_status: 'gh issue edit {{.IssueID}} --add-label "{{.Status}}"'
    comment: 'gh issue comment {{.IssueID}} --body "{{.Body}}"'
  pr:
    create: 'gh pr create --title "{{.Title}}" --body "{{.Description}}"'
    merge: 'gh pr merge {{.PRURL}} --merge'
  prompts:
    yolo: "custom prompt for --yolo mode (optional)"
```

All commands are Go templates. Replace with your issue tracker's CLI as needed.

## Docker resources

Per sandbox, cbox creates:

- 1 container: `cbox-<project>-<branch>-claude`
- 1 bridge network: `cbox-<project>-<branch>`
- 1 image: `cbox-<project>:claude`
- 1 git worktree directory
- 1 MCP server process (if commands or host_commands are configured)

All are cleaned up by `cbox clean`.
