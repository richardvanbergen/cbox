# Multi-branch state management for cbox

## Rationale

cbox is designed to give Claude Code autonomous, sandboxed environments. The next step is an orchestration layer — a host-side process (or Claude instance) that spins up multiple cboxes and coordinates work across them. For example: one cbox working on auth, another on the API layer, a third running tests — all concurrently.

The current architecture blocks this. There is a single `.cbox.state.json` file per project that tracks one active branch. Every command (`chat`, `exec`, `down`) reads from that file to find its target containers. Running `cbox up` a second time overwrites the state, orphaning the previous branch's containers. There is no way to address a specific sandbox without it being the currently selected context.

To support concurrent orchestration, we need:
1. **Independent state per branch** — so multiple sandboxes can run simultaneously without stepping on each other.
2. **Explicit branch targeting on every command** — so an orchestrator can direct actions to specific sandboxes without a shared mutable "active" context.
3. **No "active branch" concept** — because implicit state is a footgun when multiple actors are issuing commands concurrently.

## Summary

Move from a single `.cbox.state.json` file to a `.cbox/` directory containing per-branch state files. Remove the "active branch" concept entirely — every command takes branch as a positional argument. The filesystem (presence of state files) serves as the registry.

## State file layout

```
project/
  .cbox/
    feature-auth.state.json
    hotfix-login.state.json
  .cbox.yml
```

Branch names are normalized the same way as container names: `/` → `-`. So branch `feature/auth` becomes `.cbox/feature-auth.state.json`.

## CLI changes — branch as positional arg

Every command that operates on a sandbox takes `<branch>` as a required positional arg (not a flag):

| Before | After |
|--------|-------|
| `cbox up --branch feature-a` | `cbox up feature-a` |
| `cbox down` | `cbox down feature-a` |
| `cbox chat` | `cbox chat feature-a` |
| `cbox chat -p "fix bug"` | `cbox chat feature-a -p "fix bug"` |
| `cbox exec npm test` | `cbox exec feature-a npm test` |
| `cbox shell` | `cbox shell feature-a` |
| `cbox info` | `cbox info feature-a` |
| `cbox clean` | `cbox clean feature-a` |
| `cbox list` | `cbox list` (no change) |

Commands that don't change: `init`, `list`, `_mcp-proxy`, `_bridge-proxy`.

## Files to modify

### 1. `internal/sandbox/state.go`
- Change `StateFile` constant to `StateDir = ".cbox"`
- `LoadState(projectDir, branch)` — reads `.cbox/<safe-branch>.state.json`
- `SaveState(projectDir, branch, state)` — creates `.cbox/` dir if needed, writes `.cbox/<safe-branch>.state.json`
- `RemoveState(projectDir, branch)` — deletes specific branch state file
- Add `ListStates(projectDir) ([]*State, error)` — globs `.cbox/*.state.json`, returns all states
- Add helper `stateFilePath(projectDir, branch) string` for the path logic
- Use same branch normalization as docker (`/` → `-`)

### 2. `internal/sandbox/sandbox.go`
- `Up(projectDir, branch)` — no signature change needed; calls `SaveState(projectDir, branch, state)` instead of `SaveState(projectDir, state)`
- `Down(projectDir, branch string)` — add branch param, calls `LoadState(projectDir, branch)`
- `Chat(projectDir, branch string, chrome bool)` — add branch param
- `ChatPrompt(projectDir, branch, prompt string)` — add branch param
- `Exec(projectDir, branch string, command []string)` — add branch param
- `Shell(projectDir, branch string)` — add branch param
- `Info(projectDir, branch string)` — add branch param
- `Clean(projectDir, branch string)` — add branch param
- `ActiveBranch()` — remove entirely (no active branch concept)

### 3. `cmd/cbox/main.go`
- `upCmd()`: Change from `--branch` flag to `Args: cobra.ExactArgs(1)`, use `args[0]` as branch
- `downCmd()`: Add `Args: cobra.ExactArgs(1)`, pass `args[0]` to `Down()`
- `chatCmd()`: Add `Args: cobra.ExactArgs(1)`, pass `args[0]` to `Chat()`/`ChatPrompt()`
- `execCmd()`: First arg is branch, rest is command. Since `DisableFlagParsing` is true, `args[0]` is branch, `args[1:]` is the command
- `shellCmd()`: Add `Args: cobra.ExactArgs(1)`, pass `args[0]`
- `infoCmd()`: Add `Args: cobra.ExactArgs(1)`, pass `args[0]`
- `cleanCmd()`: Add `Args: cobra.ExactArgs(1)`, pass `args[0]`
- `listCmd()`: Rewrite — call `ListStates()` to get all tracked sandboxes, display branch + status info for each

### 4. `internal/worktree/worktree.go`
- `List()` — remove the `activeBranch` param and `*` marking logic, since `list` is now based on state files not worktrees

### 5. `.gitignore`
- Add `.cbox/` (and remove `.cbox.state.json` if it was listed)

## Migration note

The old `.cbox.state.json` file will simply stop being recognized. Users doing `cbox up <branch>` after this change will create the new `.cbox/` directory. Old state files can be manually deleted. No automatic migration needed — `cbox up` recreates everything.

## Verification

1. `cbox up feature-a` — creates `.cbox/feature-a.state.json`, starts containers
2. `cbox up feature-b` — creates `.cbox/feature-b.state.json`, starts separate containers (both running)
3. `cbox list` — shows both feature-a and feature-b as tracked
4. `cbox chat feature-a` — connects to feature-a's claude container
5. `cbox chat feature-b` — connects to feature-b's claude container
6. `cbox down feature-a` — stops only feature-a, removes its state file
7. `cbox list` — shows only feature-b
8. `cbox clean feature-b` — full cleanup of feature-b
9. `cbox list` — empty
10. Build: `go build -o bin/cbox ./cmd/cbox`
