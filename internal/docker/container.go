package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/richvanbergen/cbox/internal/bridge"
	"github.com/richvanbergen/cbox/internal/output"
)

// ContainerName returns a deterministic container name with a role suffix.
func ContainerName(project, branch, role string) string {
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	return "cbox-" + project + "-" + safeBranch + "-" + role
}

// NetworkName returns a deterministic network name.
func NetworkName(project, branch string) string {
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	return "cbox-" + project + "-" + safeBranch
}

// CreateNetwork creates a Docker bridge network.
func CreateNetwork(name string) error {
	cmd := exec.Command("docker", "network", "create", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore if network already exists
		if strings.Contains(string(out), "already exists") {
			return nil
		}
		return fmt.Errorf("docker network create: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// RemoveNetwork removes a Docker network.
func RemoveNetwork(name string) error {
	cmd := exec.Command("docker", "network", "rm", name)
	cmd.Run() // ignore error if network doesn't exist
	return nil
}

// RunClaudeContainer starts the Claude container with docker socket, workspace mount, and shared network.
func RunClaudeContainer(name, image, network, worktreePath string, envVars []string, envFile string, bridgeMappings []bridge.ProxyMapping, ports []string) error {
	currentUser := os.Getenv("USER")

	args := []string{
		"run", "-d",
		"--name", name,
		"--network", network,
		"-v", worktreePath + ":/workspace",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
	}

	for _, p := range ports {
		args = append(args, "-p", p)
	}

	// Extract Claude Code OAuth credentials from macOS Keychain and pass to container
	credCmd := exec.Command("security", "find-generic-password", "-s", "Claude Code-credentials", "-w")
	if credOut, err := credCmd.Output(); err == nil {
		args = append(args, "-e", "CLAUDE_CODE_CREDENTIALS="+strings.TrimSpace(string(credOut)))
	}

	// Pass Chrome bridge mappings and USER so the entrypoint can set up socat relays
	if len(bridgeMappings) > 0 {
		mappingsJSON, err := bridge.MarshalMappings(bridgeMappings)
		if err == nil {
			args = append(args, "-e", "CHROME_BRIDGE_MAPPINGS="+mappingsJSON)
			args = append(args, "-e", "USER="+currentUser)
		}
	}

	for _, env := range envVars {
		val := os.Getenv(env)
		if val != "" {
			args = append(args, "-e", env+"="+val)
		}
	}

	if envFile != "" {
		if _, err := os.Stat(envFile); err == nil {
			args = append(args, "--env-file", envFile)
		}
	}

	args = append(args, image)

	cmd := exec.Command("docker", args...)
	cw := output.NewCommandWriter(os.Stdout)
	cmd.Stdout = cw
	cmd.Stderr = cw
	runErr := cmd.Run()
	cw.Close()
	if runErr != nil {
		return fmt.Errorf("docker run (claude): %w", runErr)
	}
	return nil
}

// terminalEnvArgs returns docker exec -e flags for host terminal environment
// variables. These allow applications inside the container to detect the
// terminal emulator and enable features like enhanced keyboard protocols
// (e.g. kitty keyboard protocol for Shift+Enter) and inline image display.
func terminalEnvArgs() []string {
	vars := []string{
		"COLORTERM",
		"TERM_PROGRAM",
		"TERM_PROGRAM_VERSION",
		"LC_TERMINAL",
		"LC_TERMINAL_VERSION",
		"KITTY_WINDOW_ID",
		"KITTY_PID",
		"ITERM_SESSION_ID",
		"WT_SESSION",
		"WT_PROFILE_ID",
		"TERMINAL_EMULATOR",
		"WEZTERM_PANE",
		"KONSOLE_VERSION",
		"VTE_VERSION",
	}
	var args []string
	for _, v := range vars {
		if val := os.Getenv(v); val != "" {
			args = append(args, "-e", v+"="+val)
		}
	}
	return args
}

// Shell execs into a running container with an interactive shell.
func Shell(name string) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}

	args := []string{"docker", "exec", "-it"}
	args = append(args, terminalEnvArgs()...)
	args = append(args, "-u", "claude", name, "bash")
	return syscall.Exec(dockerPath, args, os.Environ())
}

// Chat execs into the Claude container and launches Claude Code interactively.
// If resume is true, passes --continue to resume the last conversation.
// Otherwise, if initialPrompt is provided, it is sent as the first message.
func Chat(name string, chrome bool, initialPrompt string, resume bool) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}

	args := []string{"docker", "exec", "-it"}
	args = append(args, terminalEnvArgs()...)
	args = append(args, "-u", "claude", name, "claude", "--dangerously-skip-permissions")
	if chrome {
		args = append(args, "--chrome")
	}
	if resume {
		args = append(args, "--continue")
	} else if initialPrompt != "" {
		args = append(args, initialPrompt)
	}
	return syscall.Exec(dockerPath, args, os.Environ())
}

// ChatPrompt runs Claude in headless mode with a prompt inside the Claude container.
func ChatPrompt(name, prompt string) error {
	cmd := exec.Command("docker", "exec", "-u", "claude", name,
		"claude", "--dangerously-skip-permissions",
		"-p", prompt,
		"--output-format", "json",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// InjectClaudeMD writes a system-level CLAUDE.md into the Claude container at
// ~/.claude/CLAUDE.md so Claude Code understands the container environment.
func InjectClaudeMD(claudeContainer string, hostCommands []string, namedCommands map[string]string, ports []string, extras ...string) error {
	var sections []string

	// Base environment section
	sections = append(sections, `# CBox Container Environment

You are running inside a CBox sandbox — a Docker container purpose-built for
isolated development. You do NOT have direct access to the host machine.

## What you have

- /workspace is a mounted git worktree from the host
- Docker CLI is available (the host Docker socket is mounted)
- bash, curl, git (local only — see below), ca-certificates, socat
- Your MCP tools (see below) are your primary way to interact with the project

## What you do NOT have

- No language runtimes (no node, bun, python, go, cargo, etc.)
- No package managers beyond apt (no npm, pip, brew, etc.)
- No direct internet access beyond Docker networking
- No direct access to the host filesystem, git, or CLI tools
- Do NOT run apt-get install — the container is ephemeral and changes are lost on rebuild`)

	// Host commands section
	if len(hostCommands) > 0 {
		hostSection := fmt.Sprintf(`## Host Commands (MCP)

You have a "cbox-host" MCP server that runs commands on the HOST machine.
Whitelisted commands: %s

IMPORTANT:
- You MUST use the run_command MCP tool for these — do not run them directly
- Direct execution will fail or produce wrong results (wrong filesystem, wrong git repo)
- The run_command tool executes in the host worktree, not inside this container`, strings.Join(hostCommands, ", "))

		// Add gh-specific tips if gh is in the whitelist
		for _, cmd := range hostCommands {
			if cmd == "gh" {
				hostSection += `

### gh CLI tips
- ALWAYS use --json with gh issue view and gh pr view to avoid deprecated API errors
  Example: gh issue view 123 --json title,body,labels,state
- The default (non-JSON) output triggers a sunsetted Projects Classic API and will fail`
				break
			}
		}

		sections = append(sections, hostSection)
	}

	// Named commands section
	if len(namedCommands) > 0 {
		var lines []string
		for name, expr := range namedCommands {
			lines = append(lines, fmt.Sprintf("- cbox_%s: `%s`", name, expr))
		}
		sections = append(sections, fmt.Sprintf(`## Project Commands (MCP)

These MCP tools run on the host and are your primary way to build, test, and run the project:
%s

Use these instead of trying to run build/test commands directly in the container.`, strings.Join(lines, "\n")))
	}

	// Exposed ports section
	if len(ports) > 0 {
		var portLines []string
		for _, p := range ports {
			portLines = append(portLines, fmt.Sprintf("- `%s`", p))
		}
		sections = append(sections, fmt.Sprintf(`## Exposed Ports

The following ports are mapped from this container to the host:
%s

These ports were configured via the `+"`ports`"+` field in cbox.toml.`, strings.Join(portLines, "\n")))
	}

	// Self-healing section
	sections = append(sections, `## When something is missing

If you need a tool, runtime, or command that is not available, DO NOT try to install
it inside the container. Instead, choose one of the strategies below.

Present these options to the user and let them decide which approach they prefer.

### Quick: run it via Docker

The Docker socket is mounted, so you can run any tool via a Docker image right now
without reconfiguring anything:
`+"```bash"+`
# Run a command using a runtime image — /workspace is shared with the host
docker run --rm -v /workspace:/workspace -w /workspace node:20 npm install
docker run --rm -v /workspace:/workspace -w /workspace golang:1.23 go test ./...
docker run --rm -v /workspace:/workspace -w /workspace python:3.12 python script.py
`+"```"+`
This is immediate but ephemeral — installed packages don't persist between runs.
For services (databases, redis, etc.), use docker run -d to keep them running.

### Permanent: configure cbox

These changes go in cbox.toml and persist across sessions. After any change,
the user must rebuild: `+"`cbox up <branch> --rebuild`"+`

**Add a host command** — expose a tool already installed on the host machine:
`+"```toml"+`
host_commands = ["git", "gh", "bun"]
`+"```"+`

**Add or update project commands** — define build/test/run as MCP tools:
`+"```toml"+`
[commands]
build = "go build ./..."
test = "go test ./..."
run = "go run ./cmd/myapp"
`+"```"+`

**Use a custom Dockerfile** — bake runtimes or system packages into the container:
`+"```toml"+`
dockerfile = ".cbox.Dockerfile"
`+"```"+`
The user creates a Dockerfile that installs what's needed (e.g. node, python, etc.)
and references it in cbox.toml. This makes the tools available directly in the container.`)

	// Extra sections (e.g. task assignment from workflow)
	for _, e := range extras {
		sections = append(sections, e)
	}

	claudeMD := strings.Join(sections, "\n\n") + "\n"

	writeCmd := "mkdir -p /home/claude/.claude && cat > /home/claude/.claude/CLAUDE.md && chown -R claude:claude /home/claude/.claude"
	cmd := exec.Command("docker", "exec", "-i", claudeContainer, "sh", "-c", writeCmd)
	cmd.Stdin = strings.NewReader(claudeMD)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("writing CLAUDE.md: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// AppendClaudeMD appends text to the CLAUDE.md file inside the Claude container.
func AppendClaudeMD(claudeContainer, text string) error {
	cmd := exec.Command("docker", "exec", claudeContainer,
		"sh", "-c", `printf '\n%s\n' "$0" >> /home/claude/.claude/CLAUDE.md`, text)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("appending to CLAUDE.md: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// InjectMCPConfig writes a .mcp.json file into the Claude container so Claude Code
// can connect to the host MCP server.
func InjectMCPConfig(claudeContainer string, mcpPort int) error {
	mcpConfig := fmt.Sprintf(`{
  "mcpServers": {
    "cbox-host": {
      "type": "http",
      "url": "http://host.docker.internal:%d/mcp"
    }
  }
}
`, mcpPort)

	writeCmd := "cat > /workspace/.mcp.json && chown claude:claude /workspace/.mcp.json"
	cmd := exec.Command("docker", "exec", "-i", claudeContainer, "sh", "-c", writeCmd)
	cmd.Stdin = strings.NewReader(mcpConfig)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("writing .mcp.json: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// IsRunning checks if a container is currently running.
func IsRunning(name string) (bool, error) {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

// StopAndRemove stops and removes a container.
// It returns nil if the container was successfully removed or did not exist.
func StopAndRemove(name string) error {
	stop := exec.Command("docker", "stop", name)
	stop.Run() // ignore error — container may already be stopped

	rm := exec.Command("docker", "rm", name)
	out, err := rm.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		// Not an error if the container doesn't exist
		if strings.Contains(outStr, "No such container") ||
			strings.Contains(outStr, "no such container") {
			return nil
		}
		return fmt.Errorf("docker rm: %s: %w", outStr, err)
	}
	return nil
}

// GenerateEnvFile writes a temporary env file from the host environment for the given var names.
func GenerateEnvFile(dir string, envVars []string) (string, error) {
	var lines []string
	for _, env := range envVars {
		val := os.Getenv(env)
		if val != "" {
			lines = append(lines, env+"="+val)
		}
	}

	if len(lines) == 0 {
		return "", nil
	}

	path := filepath.Join(dir, ".cbox.env")
	err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
	if err != nil {
		return "", fmt.Errorf("writing env file: %w", err)
	}
	return path, nil
}
