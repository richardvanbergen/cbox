package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/richvanbergen/cbox/internal/bridge"
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

// RunAppContainer starts the app container with sleep infinity, shared network, workspace mount, and port mappings.
func RunAppContainer(name, image, network, worktreePath string, envVars []string, envFile string, ports []string) error {
	args := []string{
		"run", "-d",
		"--name", name,
		"--network", network,
		"--entrypoint", "",
		"-v", worktreePath + ":/workspace",
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

	for _, p := range ports {
		args = append(args, "-p", p)
	}

	args = append(args, image, "sleep", "infinity")

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker run (app): %w", err)
	}
	return nil
}

// RunClaudeContainer starts the Claude container with docker socket, workspace mount, and shared network.
func RunClaudeContainer(name, image, network, worktreePath, appContainerName string, envVars []string, envFile string, bridgeMappings []bridge.ProxyMapping) error {
	currentUser := os.Getenv("USER")

	args := []string{
		"run", "-d",
		"--name", name,
		"--network", network,
		"-v", worktreePath + ":/workspace",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-e", "CBOX_APP_CONTAINER=" + appContainerName,
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker run (claude): %w", err)
	}
	return nil
}

// InstallCommands writes wrapper scripts into the Claude container for each named command.
// Each script executes the corresponding command in the app container via docker exec.
func InstallCommands(claudeContainer string, commands map[string]string) error {
	for name, command := range commands {
		scriptPath := "/home/claude/bin/cbox-" + name
		script := "#!/bin/bash\nexec docker exec -i \"$CBOX_APP_CONTAINER\" sh -c '" + command + "'\n"

		// Pipe script content via stdin to avoid shell escaping issues
		writeCmd := fmt.Sprintf("cat > %s && chmod +x %s", scriptPath, scriptPath)
		cmd := exec.Command("docker", "exec", "-i", claudeContainer, "sh", "-c", writeCmd)
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("installing command %q: %s: %w", name, strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

// Shell execs into a running container with an interactive shell.
func Shell(name string) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}

	args := []string{"docker", "exec", "-it", "-u", "claude", name, "bash"}
	return syscall.Exec(dockerPath, args, os.Environ())
}

// Chat execs into the Claude container and launches Claude Code interactively.
func Chat(name string, chrome bool) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}

	args := []string{"docker", "exec", "-it", "-u", "claude", name, "claude", "--dangerously-skip-permissions"}
	if chrome {
		args = append(args, "--chrome")
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

// ExecApp runs a command in the app container interactively.
// If no command is given, it opens a shell.
func ExecApp(name string, command []string) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}

	args := []string{"docker", "exec", "-it", name}
	if len(command) == 0 {
		args = append(args, "sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh")
	} else {
		args = append(args, command...)
	}
	return syscall.Exec(dockerPath, args, os.Environ())
}

// InjectClaudeMD writes a system-level CLAUDE.md into the Claude container at
// ~/.claude/CLAUDE.md so Claude Code understands the container environment.
func InjectClaudeMD(claudeContainer string, hostCommands []string) error {
	var hostCmdSection string
	if len(hostCommands) > 0 {
		hostCmdSection = fmt.Sprintf(`

## Host Commands (MCP)

You have access to a "cbox-host" MCP server that can run commands on the host machine.
The following commands are whitelisted: %s

When you need to run any of these commands (e.g. git, gh), you MUST use the run_command
tool from the cbox-host MCP server instead of running them directly. Direct execution
will fail or produce incorrect results because you are inside a container.

Before running a command, check that it is in the whitelist above. If the user asks you
to run a command that is not whitelisted, inform them that the command is not available
and they need to add it to the host_commands list in their .cbox.yml configuration.`, strings.Join(hostCommands, ", "))
	}

	claudeMD := fmt.Sprintf(`You are currently executing inside a cbox container environment.

This means you do NOT have direct access to the host machine's filesystem, git
repositories, or CLI tools. Everything you run executes inside a Docker container.

Key things to know:
- The /workspace directory is a mounted volume from the host
- You do not have direct internet access beyond what Docker networking provides
- Most host CLI tools (git, gh, etc.) are not available inside this container
- The app container is accessible via the cbox-run/cbox-test wrapper commands%s
`, hostCmdSection)

	writeCmd := "mkdir -p /home/claude/.claude && cat > /home/claude/.claude/CLAUDE.md && chown -R claude:claude /home/claude/.claude"
	cmd := exec.Command("docker", "exec", "-i", claudeContainer, "sh", "-c", writeCmd)
	cmd.Stdin = strings.NewReader(claudeMD)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("writing CLAUDE.md: %s: %w", strings.TrimSpace(string(out)), err)
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
func StopAndRemove(name string) error {
	stop := exec.Command("docker", "stop", name)
	stop.Run()

	rm := exec.Command("docker", "rm", name)
	out, err := rm.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm: %s: %w", strings.TrimSpace(string(out)), err)
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
