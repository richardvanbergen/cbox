package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/richvanbergen/cbox/internal/bridge"
	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/docker"
	"github.com/richvanbergen/cbox/internal/worktree"
)

// Up creates a worktree, builds images, creates a network, and starts two containers.
func Up(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	projectName := filepath.Base(projectDir)

	// 1. Create or reuse worktree
	fmt.Printf("Preparing worktree for branch '%s'...\n", branch)
	wtPath, err := worktree.Create(projectDir, branch)
	if err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}
	fmt.Printf("Worktree ready at %s\n", wtPath)

	// 2. Build app image (user's Dockerfile)
	appImage := docker.ImageName(projectName, "app")
	fmt.Printf("Building app image %s...\n", appImage)
	dockerfile := filepath.Join(projectDir, cfg.Dockerfile)
	if err := docker.BuildBaseImage(projectDir, dockerfile, cfg.Target, appImage); err != nil {
		return fmt.Errorf("building app image: %w", err)
	}

	// 3. Build Claude image
	claudeImage := docker.ImageName(projectName, "claude")
	fmt.Printf("Building Claude image %s...\n", claudeImage)
	if err := docker.BuildClaudeImage(claudeImage); err != nil {
		return fmt.Errorf("building claude image: %w", err)
	}

	// 4. Create Docker network
	networkName := docker.NetworkName(projectName, branch)
	fmt.Printf("Creating network %s...\n", networkName)
	if err := docker.CreateNetwork(networkName); err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	// 5. Stop/remove existing containers
	appContainerName := docker.ContainerName(projectName, branch, "app")
	claudeContainerName := docker.ContainerName(projectName, branch, "claude")
	docker.StopAndRemove(appContainerName)
	docker.StopAndRemove(claudeContainerName)

	// 6. Resolve env file path
	envFile := ""
	if cfg.EnvFile != "" {
		envFile = filepath.Join(projectDir, cfg.EnvFile)
	}

	// 7. Start App container
	fmt.Printf("Starting app container %s...\n", appContainerName)
	if err := docker.RunAppContainer(appContainerName, appImage, networkName, wtPath, cfg.Env, envFile, cfg.Ports); err != nil {
		return fmt.Errorf("starting app container: %w", err)
	}

	// 7.5 Start Chrome bridge proxy if browser is enabled and bridge sockets exist on the host
	var bridgePID int
	var bridgeMappings []bridge.ProxyMapping
	if cfg.Browser {
		currentUser := os.Getenv("USER")
		chromeBridgePath := "/tmp/claude-mcp-browser-bridge-" + currentUser
		if _, err := os.Stat(chromeBridgePath); err == nil {
			fmt.Println("Starting Chrome bridge proxy...")
			bridgePID, bridgeMappings, err = startBridgeProxy(chromeBridgePath)
			if err != nil {
				fmt.Printf("Warning: Chrome bridge proxy failed: %v\n", err)
			} else if len(bridgeMappings) > 0 {
				for _, m := range bridgeMappings {
					fmt.Printf("  %s → TCP port %d\n", m.SocketName, m.TCPPort)
				}
			}
		}
	}

	// 8. Start MCP host command proxy if configured
	var mcpPID, mcpPort int
	if len(cfg.HostCommands) > 0 {
		fmt.Println("Starting MCP host command server...")
		mcpPID, mcpPort, err = startMCPProxy(wtPath, cfg.HostCommands)
		if err != nil {
			fmt.Printf("Warning: MCP host command server failed: %v\n", err)
		} else {
			fmt.Printf("  MCP server listening on port %d\n", mcpPort)
		}
	}

	// 9. Start Claude container
	fmt.Printf("Starting Claude container %s...\n", claudeContainerName)
	if err := docker.RunClaudeContainer(claudeContainerName, claudeImage, networkName, wtPath, appContainerName, cfg.Env, envFile, bridgeMappings); err != nil {
		return fmt.Errorf("starting claude container: %w", err)
	}

	// 9.5 Inject system CLAUDE.md into Claude container
	fmt.Println("Injecting system CLAUDE.md...")
	if err := docker.InjectClaudeMD(claudeContainerName, cfg.HostCommands); err != nil {
		fmt.Printf("Warning: could not inject CLAUDE.md: %v\n", err)
	}

	// 9.6 Inject MCP config into Claude container if MCP proxy is running
	if mcpPort > 0 {
		fmt.Println("Injecting MCP config into Claude container...")
		if err := docker.InjectMCPConfig(claudeContainerName, mcpPort); err != nil {
			fmt.Printf("Warning: could not inject MCP config: %v\n", err)
		}
	}

	// 10. Install named command scripts
	if len(cfg.Commands) > 0 {
		fmt.Printf("Installing %d command(s)...\n", len(cfg.Commands))
		if err := docker.InstallCommands(claudeContainerName, cfg.Commands); err != nil {
			return fmt.Errorf("installing commands: %w", err)
		}
	}

	// 11. Save state
	state := &State{
		ClaudeContainer: claudeContainerName,
		AppContainer:    appContainerName,
		NetworkName:     networkName,
		WorktreePath:    wtPath,
		Branch:          branch,
		ClaudeImage:     claudeImage,
		AppImage:        appImage,
		ProjectDir:      projectDir,
		BridgeProxyPID:  bridgePID,
		BridgeMappings:  bridgeMappings,
		MCPProxyPID:     mcpPID,
		MCPProxyPort:    mcpPort,
	}
	if err := SaveState(projectDir, branch, state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Printf("\nSandbox is running! Use 'cbox chat %s' to start Claude.\n", branch)
	return nil
}

// Down stops both containers and removes the network.
func Down(projectDir, branch string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	// Stop bridge proxy if running
	if state.BridgeProxyPID > 0 {
		fmt.Println("Stopping Chrome bridge proxy...")
		stopBridgeProxy(state.BridgeProxyPID)
	}

	// Stop MCP proxy if running
	if state.MCPProxyPID > 0 {
		fmt.Println("Stopping MCP host command server...")
		stopProcess(state.MCPProxyPID)
	}

	fmt.Printf("Stopping containers...\n")
	docker.StopAndRemove(state.ClaudeContainer)
	docker.StopAndRemove(state.AppContainer)

	fmt.Printf("Removing network %s...\n", state.NetworkName)
	docker.RemoveNetwork(state.NetworkName)

	if err := RemoveState(projectDir, branch); err != nil {
		return fmt.Errorf("removing state: %w", err)
	}

	fmt.Printf("Containers stopped. Worktree preserved at %s\n", state.WorktreePath)
	return nil
}

// Chat launches Claude Code interactively in the Claude container.
func Chat(projectDir, branch string, chrome bool) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	return docker.Chat(state.ClaudeContainer, chrome)
}

// ChatPrompt runs a one-shot Claude prompt in the Claude container.
func ChatPrompt(projectDir, branch, prompt string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	return docker.ChatPrompt(state.ClaudeContainer, prompt)
}

// Exec runs a command in the app container. No command means interactive shell.
func Exec(projectDir, branch string, command []string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	return docker.ExecApp(state.AppContainer, command)
}

// Shell opens an interactive shell in the Claude container.
func Shell(projectDir, branch string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	return docker.Shell(state.ClaudeContainer)
}

// Info prints the current sandbox state.
func Info(projectDir, branch string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	fmt.Printf("Branch:           %s\n", state.Branch)
	fmt.Printf("Worktree:         %s\n", state.WorktreePath)
	fmt.Printf("Claude container: %s\n", state.ClaudeContainer)
	fmt.Printf("App container:    %s\n", state.AppContainer)
	fmt.Printf("Network:          %s\n", state.NetworkName)
	return nil
}

// Clean stops both containers, removes the network, worktree, and branch.
func Clean(projectDir, branch string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	// Stop bridge proxy if running
	if state.BridgeProxyPID > 0 {
		fmt.Println("Stopping Chrome bridge proxy...")
		stopBridgeProxy(state.BridgeProxyPID)
	}

	// Stop MCP proxy if running
	if state.MCPProxyPID > 0 {
		fmt.Println("Stopping MCP host command server...")
		stopProcess(state.MCPProxyPID)
	}

	// Stop containers
	fmt.Printf("Stopping containers...\n")
	docker.StopAndRemove(state.ClaudeContainer)
	docker.StopAndRemove(state.AppContainer)

	// Remove network
	fmt.Printf("Removing network %s...\n", state.NetworkName)
	docker.RemoveNetwork(state.NetworkName)

	// Remove worktree
	fmt.Printf("Removing worktree at %s...\n", state.WorktreePath)
	if err := worktree.Remove(state.ProjectDir, state.WorktreePath); err != nil {
		fmt.Printf("Warning: could not remove worktree: %v\n", err)
	}

	// Delete branch
	fmt.Printf("Deleting branch %s...\n", state.Branch)
	if err := worktree.DeleteBranch(state.ProjectDir, state.Branch); err != nil {
		fmt.Printf("Warning: could not delete branch: %v\n", err)
	}

	// Remove state
	RemoveState(projectDir, branch)

	fmt.Println("Sandbox cleaned up.")
	return nil
}

// startBridgeProxy launches `cbox _bridge-proxy` as a background process.
// It reads the JSON mappings from the process's stdout and returns its PID.
func startBridgeProxy(socketDir string) (int, []bridge.ProxyMapping, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return 0, nil, fmt.Errorf("finding executable: %w", err)
	}

	cmd := exec.Command(selfPath, "_bridge-proxy", socketDir)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Start as a new process group so it outlives this process
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("starting bridge proxy: %w", err)
	}

	// Read the first line (JSON mappings)
	buf := make([]byte, 4096)
	n, err := stdout.Read(buf)
	if err != nil {
		cmd.Process.Kill()
		return 0, nil, fmt.Errorf("reading bridge proxy output: %w", err)
	}

	line := strings.TrimSpace(string(buf[:n]))
	var mappings []bridge.ProxyMapping
	if err := json.Unmarshal([]byte(line), &mappings); err != nil {
		cmd.Process.Kill()
		return 0, nil, fmt.Errorf("parsing bridge proxy output: %w", err)
	}

	return cmd.Process.Pid, mappings, nil
}

// stopBridgeProxy sends SIGTERM to the bridge proxy process.
func stopBridgeProxy(pid int) {
	stopProcess(pid)
}

// stopProcess sends SIGTERM to a process and waits for it to exit.
func stopProcess(pid int) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	proc.Signal(syscall.SIGTERM)
	proc.Wait()
}

// startMCPProxy launches `cbox _mcp-proxy` as a background process.
// It reads the JSON output from the process's stdout and returns its PID and port.
func startMCPProxy(worktreePath string, commands []string) (int, int, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return 0, 0, fmt.Errorf("finding executable: %w", err)
	}

	args := []string{"_mcp-proxy", "--worktree", worktreePath}
	args = append(args, commands...)

	cmd := exec.Command(selfPath, args...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, 0, fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Start as a new process group so it outlives this process
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, 0, fmt.Errorf("starting MCP proxy: %w", err)
	}

	// Read the first line (JSON with port)
	buf := make([]byte, 4096)
	n, err := stdout.Read(buf)
	if err != nil {
		cmd.Process.Kill()
		return 0, 0, fmt.Errorf("reading MCP proxy output: %w", err)
	}

	line := strings.TrimSpace(string(buf[:n]))
	var output struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal([]byte(line), &output); err != nil {
		cmd.Process.Kill()
		return 0, 0, fmt.Errorf("parsing MCP proxy output: %w", err)
	}

	return cmd.Process.Pid, output.Port, nil
}
