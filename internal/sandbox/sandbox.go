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

// UpOptions configures optional behavior for sandbox creation.
type UpOptions struct {
	Rebuild    bool
	ReportDir  string // If set, enables the cbox_report MCP tool
	FlowBranch string // If set, enables flow MCP tools (cbox_flow_pr, etc.)
}

// Up creates a worktree, builds the Claude image, creates a network, and starts the Claude container.
// If rebuild is true, the image is built with --no-cache.
func Up(projectDir, branch string, rebuild bool) error {
	return UpWithOptions(projectDir, branch, UpOptions{Rebuild: rebuild})
}

// UpWithOptions creates a sandbox with additional options.
func UpWithOptions(projectDir, branch string, opts UpOptions) error {
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

	// Copy configured files into the new worktree
	if len(cfg.CopyFiles) > 0 {
		fmt.Println("Copying files to worktree...")
		if err := worktree.CopyFiles(projectDir, wtPath, cfg.CopyFiles); err != nil {
			return fmt.Errorf("copying files to worktree: %w", err)
		}
	}

	// 2. Build Claude image
	claudeImage := docker.ImageName(projectName, "claude")
	fmt.Printf("Building Claude image %s...\n", claudeImage)
	buildOpts := docker.BuildOptions{NoCache: opts.Rebuild}
	if cfg.Dockerfile != "" {
		buildOpts.ProjectDockerfile = filepath.Join(projectDir, cfg.Dockerfile)
	}
	if err := docker.BuildClaudeImage(claudeImage, buildOpts); err != nil {
		return fmt.Errorf("building claude image: %w", err)
	}

	// 3. Create Docker network
	networkName := docker.NetworkName(projectName, branch)
	fmt.Printf("Creating network %s...\n", networkName)
	if err := docker.CreateNetwork(networkName); err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	// 4. Stop/remove existing Claude container
	claudeContainerName := docker.ContainerName(projectName, branch, "claude")
	docker.StopAndRemove(claudeContainerName)

	// 5. Resolve env file path
	envFile := ""
	if cfg.EnvFile != "" {
		envFile = filepath.Join(projectDir, cfg.EnvFile)
	}

	// 6. Start Chrome bridge proxy if browser is enabled and bridge sockets exist on the host
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

	// 7. Start MCP proxy if host_commands or commands are configured
	var mcpPID, mcpPort int
	if len(cfg.HostCommands) > 0 || len(cfg.Commands) > 0 {
		fmt.Println("Starting MCP host command server...")
		mcpPID, mcpPort, err = startMCPProxy(projectDir, wtPath, cfg.HostCommands, cfg.Commands, opts.ReportDir, opts.FlowBranch)
		if err != nil {
			fmt.Printf("Warning: MCP host command server failed: %v\n", err)
		} else {
			fmt.Printf("  MCP server listening on port %d\n", mcpPort)
		}
	}

	// 8. Start Claude container
	fmt.Printf("Starting Claude container %s...\n", claudeContainerName)
	if err := docker.RunClaudeContainer(claudeContainerName, claudeImage, networkName, wtPath, cfg.Env, envFile, bridgeMappings); err != nil {
		return fmt.Errorf("starting claude container: %w", err)
	}

	// 9. Inject system CLAUDE.md into Claude container
	fmt.Println("Injecting system CLAUDE.md...")
	if err := docker.InjectClaudeMD(claudeContainerName, cfg.HostCommands, cfg.Commands); err != nil {
		fmt.Printf("Warning: could not inject CLAUDE.md: %v\n", err)
	}

	// 10. Inject MCP config into Claude container if MCP proxy is running
	if mcpPort > 0 {
		fmt.Println("Injecting MCP config into Claude container...")
		if err := docker.InjectMCPConfig(claudeContainerName, mcpPort); err != nil {
			fmt.Printf("Warning: could not inject MCP config: %v\n", err)
		}
	}

	// 11. Save state
	state := &State{
		ClaudeContainer: claudeContainerName,
		NetworkName:     networkName,
		WorktreePath:    wtPath,
		Branch:          branch,
		ClaudeImage:     claudeImage,
		ProjectDir:      projectDir,
		Running:         true,
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

// Down stops the container and removes the network.
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

	fmt.Printf("Stopping container %s...\n", state.ClaudeContainer)
	if err := docker.StopAndRemove(state.ClaudeContainer); err != nil {
		fmt.Printf("Warning: could not remove container: %v\n", err)
	}

	fmt.Printf("Removing network %s...\n", state.NetworkName)
	docker.RemoveNetwork(state.NetworkName)

	// Mark as not running but preserve state so `clean` can still find the worktree
	state.Running = false
	state.BridgeProxyPID = 0
	state.BridgeMappings = nil
	state.MCPProxyPID = 0
	state.MCPProxyPort = 0
	if err := SaveState(projectDir, branch, state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	fmt.Printf("Container stopped. Worktree preserved at %s\n", state.WorktreePath)
	return nil
}

// Chat launches Claude Code interactively in the Claude container.
// If resume is true, passes --continue to resume the last conversation.
// Otherwise, if initialPrompt is provided, it is sent as the first message.
func Chat(projectDir, branch string, chrome bool, initialPrompt string, resume bool) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	return docker.Chat(state.ClaudeContainer, chrome, initialPrompt, resume)
}

// ChatPrompt runs a one-shot Claude prompt in the Claude container.
func ChatPrompt(projectDir, branch, prompt string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	return docker.ChatPrompt(state.ClaudeContainer, prompt)
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
	fmt.Printf("Network:          %s\n", state.NetworkName)
	return nil
}

// Clean stops the container, removes the network, worktree, and branch.
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

	// Always attempt to stop and remove the container. The Running flag in
	// the state file can be stale (e.g. after a crash or if Down was called
	// but the container was restarted). StopAndRemove is safe to call even
	// when the container is already gone.
	fmt.Printf("Stopping container %s...\n", state.ClaudeContainer)
	if err := docker.StopAndRemove(state.ClaudeContainer); err != nil {
		fmt.Printf("Warning: could not remove container: %v\n", err)
	}

	// Remove network (safe to call even if already removed)
	fmt.Printf("Removing network %s...\n", state.NetworkName)
	docker.RemoveNetwork(state.NetworkName)

	// Remove worktree
	fmt.Printf("Removing worktree at %s...\n", state.WorktreePath)
	if err := worktree.Remove(state.ProjectDir, state.WorktreePath); err != nil {
		fmt.Printf("Warning: could not remove worktree: %v\n", err)
	}

	// Delete branch (may already be gone after worktree remove)
	worktree.DeleteBranch(state.ProjectDir, state.Branch)

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
func startMCPProxy(projectDir, worktreePath string, hostCommands []string, namedCommands map[string]string, reportDir, flowBranch string) (int, int, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return 0, 0, fmt.Errorf("finding executable: %w", err)
	}

	args := []string{"_mcp-proxy", "--worktree", worktreePath}

	// Pass named commands as JSON via --commands flag
	if len(namedCommands) > 0 {
		cmdJSON, err := json.Marshal(namedCommands)
		if err != nil {
			return 0, 0, fmt.Errorf("marshaling commands: %w", err)
		}
		args = append(args, "--commands", string(cmdJSON))
	}

	// Pass report dir if set
	if reportDir != "" {
		args = append(args, "--report-dir", reportDir)
	}

	// Pass flow context if set
	if flowBranch != "" {
		args = append(args, "--flow-project-dir", projectDir, "--flow-branch", flowBranch)
	}

	// Host commands are passed as positional args
	args = append(args, hostCommands...)

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
