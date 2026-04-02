---
name: cbox-init
description: Set up or repair a project's cbox.toml. If none exists, generates one by reading the project. If one exists, checks it has everything needed for the cbox-plan/cbox-swarm flow and fixes what's missing.
---

You are a cbox configuration assistant. Your job is to ensure the project has a correct `cbox.toml` that supports the full `/cbox-plan` → `/cbox-swarm` workflow.

## Step 1: Check for Existing Config

Check if `cbox.toml` exists in the project root.

- **If it exists**: Go to Step 3 (Audit).
- **If it doesn't exist**: Go to Step 2 (Generate).

---

## Step 2: Generate from Scratch

Read the project to infer the correct configuration. Do not ask the user — infer everything from the codebase.

### 2a. Detect Package Manager & Commands

Look for lockfiles and config files to determine the stack:

| Found | Package manager | Build | Test | Dev server |
|-------|----------------|-------|------|------------|
| `bun.lockb` | bun | `bun run build` | `bun run test` | `bun run dev` |
| `pnpm-lock.yaml` | pnpm | `pnpm build` | `pnpm test` | `pnpm dev` |
| `yarn.lock` | yarn | `yarn build` | `yarn test` | `yarn dev` |
| `package-lock.json` | npm | `npm run build` | `npm test` | `npm run dev` |
| `go.mod` | go | `go build ./...` | `go test ./...` | — |
| `Cargo.toml` | cargo | `cargo build` | `cargo test` | — |

For monorepos (multiple `package.json` files, `apps/` or `packages/` directories): use workspace-aware commands, e.g. `bun --filter='./apps/*' run build`.

### 2b. Detect E2E Test Runner

Check for:
- `playwright.config.*` → Playwright: `bunx playwright test $Args` (or via package.json script)
- `cypress.config.*` → Cypress: `bunx cypress run $Args`
- No E2E runner found → omit `test-e2e` from commands

For Playwright in a monorepo, find the app that owns the config and prefix with `cd apps/<name> &&`.

Check `package.json` scripts for a `test:e2e` or similar script — prefer using that over invoking the runner directly.

### 2c. Detect Serve Setup

Look for:
- `docker-compose*.yml` or `docker-compose*.yaml` → use `docker compose -f <file> up -d` as `[serve] up`
- A dev server command from 2a → use as `[serve] command` with `--port $Port` or `--host 0.0.0.0 --port $Port`
- Database setup scripts → use as `[serve] setup`

If the project has both a server and a database, it likely needs `[serve]`.

### 2d. Detect Environment Variables

Check for a `.env` or `.env.example` file. If present, set `copy_files = [".env"]` and note any required vars.

Always include `ANTHROPIC_API_KEY` in `env`.

### 2e. Detect E2E Base URL Pattern

If a `[serve]` section is needed and E2E tests exist, the test command needs `PLAYWRIGHT_BASE_URL` (or equivalent) set to `http://localhost:$Port`. Check the existing test config to confirm the env var name.

### 2f. Write cbox.toml

Write the generated config:

```toml
env = ["ANTHROPIC_API_KEY"]
host_commands = ["git", "gh"]
copy_files = [".env"]        # only if .env exists
command_timeout = 600        # increase if E2E suite takes longer

[commands]
build    = "<detected build command>"
test     = "<detected test command>"
test-e2e = "<env var>=http://localhost:$Port <detected e2e command> $Args"
# setup  = "<install command>"  # include if setup is non-trivial

[serve]                      # only if a server is needed
up      = "<docker compose up or equivalent>"
setup   = "<install + build command>"
clean   = "<teardown/db-drop command if applicable>"
command = "<dev server command> --host 0.0.0.0 --port $Port"
```

Show the generated config to the user and explain each decision. Ask for confirmation before writing.

---

## Step 3: Audit Existing Config

Read the existing `cbox.toml` and check each requirement:

### Required fields checklist

| Field | Requirement | How to fix if missing |
|-------|-------------|----------------------|
| `host_commands` | Must include `"git"` and `"gh"` | Add them |
| `command_timeout` | Should be ≥ 600 for E2E projects, ≥ 120 otherwise | Add or increase |
| `env` | Should include `"ANTHROPIC_API_KEY"` | Add it |
| `[commands] build` | Must exist | Detect and add |
| `[commands] test` | Must exist | Detect and add |
| `[commands] test-e2e` | Must exist if project has E2E tests | Detect and add |
| `[commands] test-e2e` contains `$Args` | Required for per-task filtering | Append `$Args` to command |
| `[serve]` section | Required if project has a dev server or database | Detect and add |
| `[serve] command` contains `$Port` | Required for E2E base URL substitution | Add `--port $Port` |

### Reporting

Show the user a summary:

```
cbox.toml audit:

✓ host_commands includes git and gh
✓ command_timeout = 3600
✗ test-e2e missing $Args — E2E filtering will not work
✗ command_timeout should be at least 600 for E2E projects

Proposed fixes:
- test-e2e: append $Args to command
- (no other changes needed)
```

Ask for confirmation, then apply fixes.

---

## Step 4: Confirm

After writing or updating the config, confirm:

```
cbox.toml is ready. This project supports:
- /cbox-plan  — feature planning and GitHub issue creation
- /cbox-swarm — automated implementation with per-task E2E verification
```

If anything could not be inferred (e.g. no E2E runner found, unclear serve setup), tell the user what to add manually and why it's needed.
