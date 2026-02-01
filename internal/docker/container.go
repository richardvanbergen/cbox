package docker

import (
	"bytes"
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
func RunClaudeContainer(name, image, network, worktreePath, appContainerName string, envVars []string, envFile string, bridgeMappings []bridge.ProxyMapping, hostCmdPort int) error {
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

	// Pass host command proxy address if configured
	if hostCmdPort > 0 {
		args = append(args, "-e", fmt.Sprintf("CBOX_HOST_CMD_ADDR=host.docker.internal:%d", hostCmdPort))
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

// InstallHostCommands installs the host command client binary and creates symlinks
// for each whitelisted command in the Claude container.
func InstallHostCommands(claudeContainer string, commands []string) error {
	// Detect container architecture
	arch, err := DetectContainerArch(claudeContainer)
	if err != nil {
		return fmt.Errorf("detecting container arch: %w", err)
	}

	// Get the correct client binary
	clientBinary, err := HostCmdClientBinary(arch)
	if err != nil {
		return err
	}

	// Write the client binary into the container
	clientPath := "/home/claude/bin/.cbox-host-cmd-client"
	writeCmd := fmt.Sprintf("cat > %s && chmod +x %s", clientPath, clientPath)
	cmd := exec.Command("docker", "exec", "-i", claudeContainer, "sh", "-c", writeCmd)
	cmd.Stdin = bytes.NewReader(clientBinary)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("writing client binary: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Create symlinks for each command
	for _, name := range commands {
		linkPath := "/home/claude/bin/" + name
		// Remove existing file/link first, then create symlink
		symlinkCmd := fmt.Sprintf("rm -f %s && ln -s %s %s", linkPath, clientPath, linkPath)
		cmd := exec.Command("docker", "exec", claudeContainer, "sh", "-c", symlinkCmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("creating symlink for %q: %s: %w", name, strings.TrimSpace(string(out)), err)
		}
	}

	return nil
}

// DetectContainerArch detects the CPU architecture of a running container.
func DetectContainerArch(container string) (string, error) {
	cmd := exec.Command("docker", "exec", container, "uname", "-m")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("uname -m: %w", err)
	}
	arch := strings.TrimSpace(string(out))
	switch arch {
	case "x86_64":
		return "amd64", nil
	case "aarch64":
		return "arm64", nil
	default:
		return arch, nil
	}
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
