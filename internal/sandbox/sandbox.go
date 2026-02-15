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
	"github.com/richvanbergen/cbox/internal/output"
	"github.com/richvanbergen/cbox/internal/serve"
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
	output.Progress("Preparing worktree for branch '%s'", branch)
	wtPath, err := worktree.Create(projectDir, branch)
	if err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}
	output.Success("Worktree ready at %s", wtPath)

	// Copy configured files into the new worktree
	if len(cfg.CopyFiles) > 0 {
		output.Progress("Copying files to worktree")
		if err := worktree.CopyFiles(projectDir, wtPath, cfg.CopyFiles); err != nil {
			return fmt.Errorf("copying files to worktree: %w", err)
		}
	}

	// 2. Start serve process and Traefik proxy if [serve] is configured.
	//    This runs early so a broken serve command fails fast before we spend
	//    time building images and creating containers.
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	var servePID, servePort int
	var serveURL string
	if cfg.Serve != nil && cfg.Serve.Command != "" {
		output.Progress("Starting serve process")
		servePID, servePort, err = startServeProcess(cfg.Serve.Command, cfg.Serve.Port, wtPath)
		if err != nil {
			return fmt.Errorf("starting serve process: %w", err)
		}
		output.Text("  Serve process listening on port %d (log: .cbox/serve.log)", servePort)

		proxyPort := cfg.Serve.ProxyPort
		if proxyPort <= 0 {
			proxyPort = 80
		}
		output.Progress("Ensuring Traefik proxy is running")
		if err := serve.EnsureTraefik(projectDir, projectName, proxyPort); err != nil {
			stopProcess(servePID)
			return fmt.Errorf("starting traefik: %w", err)
		}
		if err := serve.AddRoute(projectDir, safeBranch, projectName, servePort); err != nil {
			stopProcess(servePID)
			return fmt.Errorf("adding traefik route: %w", err)
		}
		if proxyPort == 80 {
			serveURL = fmt.Sprintf("http://%s.%s.dev.localhost", safeBranch, projectName)
		} else {
			serveURL = fmt.Sprintf("http://%s.%s.dev.localhost:%d", safeBranch, projectName, proxyPort)
		}
		output.Success("Serve URL: %s", serveURL)
	}

	// 3. Build Claude image
	claudeImage := docker.ImageName(projectName, "claude")
	output.Progress("Building Claude image %s", claudeImage)
	buildOpts := docker.BuildOptions{NoCache: opts.Rebuild}
	if cfg.Dockerfile != "" {
		buildOpts.ProjectDockerfile = filepath.Join(projectDir, cfg.Dockerfile)
	}
	if err := docker.BuildClaudeImage(claudeImage, buildOpts); err != nil {
		return fmt.Errorf("building claude image: %w", err)
	}

	// 4. Create Docker network
	networkName := docker.NetworkName(projectName, branch)
	output.Progress("Creating network %s", networkName)
	if err := docker.CreateNetwork(networkName); err != nil {
		return fmt.Errorf("creating network: %w", err)
	}

	// 5. Stop/remove existing Claude container
	claudeContainerName := docker.ContainerName(projectName, branch, "claude")
	docker.StopAndRemove(claudeContainerName)

	// 6. Resolve env file path
	envFile := ""
	if cfg.EnvFile != "" {
		envFile = filepath.Join(projectDir, cfg.EnvFile)
	}

	// 7. Start Chrome bridge proxy if browser is enabled and bridge sockets exist on the host
	var bridgePID int
	var bridgeMappings []bridge.ProxyMapping
	if cfg.Browser {
		currentUser := os.Getenv("USER")
		chromeBridgePath := "/tmp/claude-mcp-browser-bridge-" + currentUser
		if _, err := os.Stat(chromeBridgePath); err == nil {
			output.Progress("Starting Chrome bridge proxy")
			bridgePID, bridgeMappings, err = startBridgeProxy(chromeBridgePath)
			if err != nil {
				output.Warning("Chrome bridge proxy failed: %v", err)
			} else if len(bridgeMappings) > 0 {
				for _, m := range bridgeMappings {
					output.Text("  %s → TCP port %d", m.SocketName, m.TCPPort)
				}
			}
		}
	}

	// 8. Start MCP proxy if host_commands or commands are configured
	var mcpPID, mcpPort int
	if len(cfg.HostCommands) > 0 || len(cfg.Commands) > 0 {
		output.Progress("Starting MCP host command server")
		mcpPID, mcpPort, err = startMCPProxy(projectDir, wtPath, cfg.HostCommands, cfg.Commands, opts.ReportDir, opts.FlowBranch)
		if err != nil {
			output.Warning("MCP host command server failed: %v", err)
		} else {
			output.Text("  MCP server listening on port %d", mcpPort)
		}
	}

	// 9. Start Claude container
	output.Progress("Starting Claude container %s", claudeContainerName)
	if err := docker.RunClaudeContainer(claudeContainerName, claudeImage, networkName, wtPath, cfg.Env, envFile, bridgeMappings, cfg.Ports); err != nil {
		return fmt.Errorf("starting claude container: %w", err)
	}

	// 10. Inject system CLAUDE.md into Claude container
	output.Progress("Injecting system CLAUDE.md")
	if err := docker.InjectClaudeMD(claudeContainerName, cfg.HostCommands, cfg.Commands, cfg.Ports); err != nil {
		output.Warning("Could not inject CLAUDE.md: %v", err)
	}

	// 11. Inject MCP config into Claude container if MCP proxy is running
	if mcpPort > 0 {
		output.Progress("Injecting MCP config into Claude container")
		if err := docker.InjectMCPConfig(claudeContainerName, mcpPort); err != nil {
			output.Warning("Could not inject MCP config: %v", err)
		}
	}

	// 12. Save state
	state := &State{
		ClaudeContainer: claudeContainerName,
		NetworkName:     networkName,
		WorktreePath:    wtPath,
		Branch:          branch,
		ClaudeImage:     claudeImage,
		ProjectDir:      projectDir,
		Running:         true,
		Ports:           cfg.Ports,
		BridgeProxyPID:  bridgePID,
		BridgeMappings:  bridgeMappings,
		MCPProxyPID:     mcpPID,
		MCPProxyPort:    mcpPort,
		ServePID:        servePID,
		ServePort:       servePort,
		ServeURL:        serveURL,
	}
	if err := SaveState(projectDir, branch, state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	output.Success("Sandbox is running! Use 'cbox chat %s' to start Claude.", branch)
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
		output.Progress("Stopping Chrome bridge proxy")
		stopBridgeProxy(state.BridgeProxyPID)
	}

	// Stop MCP proxy if running
	if state.MCPProxyPID > 0 {
		output.Progress("Stopping MCP host command server")
		stopProcess(state.MCPProxyPID)
	}

	// Stop serve process and clean up Traefik route
	stopServe(state, projectDir)

	output.Progress("Stopping container %s", state.ClaudeContainer)
	if err := docker.StopAndRemove(state.ClaudeContainer); err != nil {
		output.Warning("Could not remove container: %v", err)
	}

	output.Progress("Removing network %s", state.NetworkName)
	docker.RemoveNetwork(state.NetworkName)

	// Mark as not running but preserve state so `clean` can still find the worktree
	state.Running = false
	state.Ports = nil
	state.BridgeProxyPID = 0
	state.BridgeMappings = nil
	state.MCPProxyPID = 0
	state.MCPProxyPort = 0
	state.ServePID = 0
	state.ServePort = 0
	state.ServeURL = ""
	if err := SaveState(projectDir, branch, state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	output.Success("Container stopped. Worktree preserved at %s", state.WorktreePath)
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

	output.Text("Branch:           %s", state.Branch)
	output.Text("Worktree:         %s", state.WorktreePath)
	output.Text("Claude container: %s", state.ClaudeContainer)
	output.Text("Network:          %s", state.NetworkName)
	if len(state.Ports) > 0 {
		output.Text("Ports:            %s", strings.Join(state.Ports, ", "))
	}
	if state.ServeURL != "" {
		output.Text("Serve URL:        %s", state.ServeURL)
	}
	return nil
}

// Serve starts the serve process and Traefik route for an existing sandbox.
func Serve(projectDir, branch string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	if state.ServePID > 0 {
		return fmt.Errorf("serve process already running (PID %d, URL %s)", state.ServePID, state.ServeURL)
	}

	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	if cfg.Serve == nil || cfg.Serve.Command == "" {
		return fmt.Errorf("no [serve] section configured in %s", config.ConfigFile)
	}

	projectName := filepath.Base(projectDir)
	safeBranch := strings.ReplaceAll(branch, "/", "-")

	output.Progress("Starting serve process")
	servePID, servePort, err := startServeProcess(cfg.Serve.Command, cfg.Serve.Port, state.WorktreePath)
	if err != nil {
		return fmt.Errorf("starting serve process: %w", err)
	}
	output.Text("  Serve process listening on port %d (log: .cbox/serve.log)", servePort)

	proxyPort := cfg.Serve.ProxyPort
	if proxyPort <= 0 {
		proxyPort = 80
	}

	output.Progress("Ensuring Traefik proxy is running")
	if err := serve.EnsureTraefik(projectDir, projectName, proxyPort); err != nil {
		return fmt.Errorf("starting traefik: %w", err)
	}

	if err := serve.AddRoute(projectDir, safeBranch, projectName, servePort); err != nil {
		return fmt.Errorf("adding traefik route: %w", err)
	}

	var serveURL string
	if proxyPort == 80 {
		serveURL = fmt.Sprintf("http://%s.%s.dev.localhost", safeBranch, projectName)
	} else {
		serveURL = fmt.Sprintf("http://%s.%s.dev.localhost:%d", safeBranch, projectName, proxyPort)
	}

	state.ServePID = servePID
	state.ServePort = servePort
	state.ServeURL = serveURL
	if err := SaveState(projectDir, branch, state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	output.Success("Serve URL: %s", serveURL)
	return nil
}

// ServeLogPath returns the path to the serve log file for a sandbox.
func ServeLogPath(projectDir, branch string) (string, error) {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(state.WorktreePath), ".cbox", "serve.log"), nil
}

// ServeStop stops the serve process and removes the Traefik route for a sandbox.
func ServeStop(projectDir, branch string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	if state.ServePID == 0 && state.ServeURL == "" {
		return fmt.Errorf("no serve process running for branch %q", branch)
	}

	stopServe(state, projectDir)

	state.ServePID = 0
	state.ServePort = 0
	state.ServeURL = ""
	if err := SaveState(projectDir, branch, state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	output.Success("Serve process stopped.")
	return nil
}

// Clean stops the container, removes the network, worktree, and branch.
func Clean(projectDir, branch string) error {
	return cleanImpl(projectDir, branch, false)
}

// CleanQuiet is like Clean but suppresses progress output.
// Use this when Clean is called inside an output.Spin wrapper.
func CleanQuiet(projectDir, branch string) error {
	return cleanImpl(projectDir, branch, true)
}

func cleanImpl(projectDir, branch string, quiet bool) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	progress := output.Progress
	warning := output.Warning
	success := output.Success
	if quiet {
		noop := func(string, ...any) {}
		progress = noop
		warning = noop
		success = noop
	}

	// Stop bridge proxy if running
	if state.BridgeProxyPID > 0 {
		progress("Stopping Chrome bridge proxy")
		stopBridgeProxy(state.BridgeProxyPID)
	}

	// Stop MCP proxy if running
	if state.MCPProxyPID > 0 {
		progress("Stopping MCP host command server")
		stopProcess(state.MCPProxyPID)
	}

	// Stop serve process and clean up Traefik route
	stopServe(state, projectDir)

	// Always attempt to stop and remove the container. The Running flag in
	// the state file can be stale (e.g. after a crash or if Down was called
	// but the container was restarted). StopAndRemove is safe to call even
	// when the container is already gone.
	progress("Stopping container %s", state.ClaudeContainer)
	if err := docker.StopAndRemove(state.ClaudeContainer); err != nil {
		warning("Could not remove container: %v", err)
	}

	// Remove network (safe to call even if already removed)
	progress("Removing network %s", state.NetworkName)
	docker.RemoveNetwork(state.NetworkName)

	// Remove worktree
	progress("Removing worktree at %s", state.WorktreePath)
	if err := worktree.Remove(state.ProjectDir, state.WorktreePath); err != nil {
		warning("Could not remove worktree: %v", err)
	}

	// Delete branch (may already be gone after worktree remove)
	worktree.DeleteBranch(state.ProjectDir, state.Branch)

	// Remove state
	RemoveState(projectDir, branch)

	success("Sandbox cleaned up.")
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

// startServeProcess launches `cbox _serve-runner` as a background process.
// It reads the JSON output from the process's stdout and returns its PID and port.
func startServeProcess(command string, fixedPort int, dir string) (int, int, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return 0, 0, fmt.Errorf("finding executable: %w", err)
	}

	args := []string{"_serve-runner", "--command", command, "--port", fmt.Sprintf("%d", fixedPort), "--dir", dir}

	// Write serve output to a log file so it doesn't flood the terminal.
	logDir := filepath.Join(filepath.Dir(dir), ".cbox")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "serve.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, 0, fmt.Errorf("creating serve log: %w", err)
	}

	cmd := exec.Command(selfPath, args...)
	cmd.Stderr = logFile

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, 0, fmt.Errorf("creating stdout pipe: %w", err)
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, 0, fmt.Errorf("starting serve process: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := stdout.Read(buf)
	if err != nil {
		cmd.Process.Kill()
		return 0, 0, fmt.Errorf("reading serve process output: %w", err)
	}

	line := strings.TrimSpace(string(buf[:n]))
	var result struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal([]byte(line), &result); err != nil {
		cmd.Process.Kill()
		return 0, 0, fmt.Errorf("parsing serve process output: %w", err)
	}

	return cmd.Process.Pid, result.Port, nil
}

// stopServe stops the serve process and cleans up the Traefik route.
// If no routes remain, the Traefik container is stopped.
func stopServe(state *State, projectDir string) {
	if state.ServePID > 0 {
		output.Progress("Stopping serve process")
		stopProcess(state.ServePID)
	}

	if state.ServeURL != "" {
		safeBranch := strings.ReplaceAll(state.Branch, "/", "-")
		projectName := filepath.Base(state.ProjectDir)

		output.Progress("Removing Traefik route")
		serve.RemoveRoute(projectDir, safeBranch)

		hasRoutes, _ := serve.HasRoutes(projectDir)
		if !hasRoutes {
			output.Progress("No routes remaining, stopping Traefik proxy")
			serve.StopTraefik(projectName)
		}
	}
}
