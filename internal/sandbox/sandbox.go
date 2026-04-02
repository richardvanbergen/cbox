package sandbox

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/richvanbergen/cbox/internal/backend"
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
	NoWorktree bool   // If true, run in the current directory without creating a worktree
}

// Up creates a worktree, builds the runtime image, creates a network, and starts the backend container.
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
	rtBackend, err := backend.Get(backend.ParseName(cfg.Backend))
	if err != nil {
		return err
	}

	projectName := filepath.Base(projectDir)

	// Capture the current branch as the source before any worktree operations.
	sourceBranch, _ := worktree.CurrentBranch(projectDir)

	// Track resources for rollback on failure. The worktree is intentionally
	// excluded — it's cheap to keep and useful for debugging failed starts.
	var cleanup rollback

	// 1. Create or reuse worktree (skipped in no-worktree mode or when the
	//    requested branch is already checked out in projectDir).
	var wtPath string
	var worktreePath string // saved in state
	if opts.NoWorktree || branch == sourceBranch {
		wtPath = projectDir
		worktreePath = projectDir
		output.Progress("Starting sandbox for branch '%s' (no worktree)", branch)
	} else {
		output.Progress("Preparing worktree for branch '%s'", branch)
		var err error
		wtPath, err = worktree.Create(projectDir, branch)
		if err != nil {
			return fmt.Errorf("creating worktree: %w", err)
		}
		worktreePath = wtPath
		output.Success("Worktree ready at %s", wtPath)

		// Copy configured files into the new worktree
		if len(cfg.CopyFiles) > 0 {
			output.Progress("Copying files to worktree")
			if err := worktree.CopyFiles(projectDir, wtPath, cfg.CopyFiles); err != nil {
				return fmt.Errorf("copying files to worktree: %w", err)
			}
		}
	}

	safeBranch := strings.ReplaceAll(branch, "/", "-")

	// Set up git mounts so the worktree link resolves inside the container.
	// A worktree's .git file contains a gitdir reference using an absolute
	// host path that doesn't exist in the container. We mount the project's
	// .git directory at /repo/.git and provide a rewritten .git file that
	// points there instead. Skipped in no-worktree mode (no .git file to rewrite).
	var gitMounts *docker.GitMountConfig
	if !opts.NoWorktree {
		wtName, gitErr := worktree.GitWorktreeName(wtPath)
		if gitErr == nil {
			gitDir := filepath.Join(projectDir, ".cbox", "git")
			os.MkdirAll(gitDir, 0755)
			containerGitFile := filepath.Join(gitDir, safeBranch+".gitfile")
			gitContent := fmt.Sprintf("gitdir: /repo/.git/worktrees/%s\n", wtName)
			if writeErr := os.WriteFile(containerGitFile, []byte(gitContent), 0644); writeErr == nil {
				gitMounts = &docker.GitMountConfig{
					ProjectGitDir:    filepath.Join(projectDir, ".git"),
					ContainerGitFile: containerGitFile,
				}
			}
		}
	}

	// 2. Create Docker network early so it's available for $Network in serve commands.
	networkName := docker.NetworkName(projectName, branch)
	output.Progress("Creating network %s", networkName)
	if err := docker.CreateNetwork(networkName); err != nil {
		return fmt.Errorf("creating network: %w", err)
	}
	cleanup.addNetwork(networkName)

	// 3. Start serve process and Traefik proxy if [serve] is configured.
	//    This runs early so a broken serve command fails fast before we spend
	//    time building images and creating containers.
	var servePID, servePort int
	var serveURL string
	if cfg.Serve != nil && cfg.Serve.Command != "" {
		// Run [serve] lifecycle commands before starting the serve process.
		if cfg.Serve.Up != "" {
			output.Progress("Running serve up command")
			if err := runServeLifecycleCommand(cfg.Serve.Up, wtPath, networkName, safeBranch); err != nil {
				cleanup.run()
				return fmt.Errorf("serve up command failed: %w", err)
			}
		}
		if cfg.Serve.Setup != "" {
			output.Progress("Running serve setup command")
			if err := runServeLifecycleCommand(cfg.Serve.Setup, wtPath, networkName, safeBranch); err != nil {
				cleanup.run()
				return fmt.Errorf("serve setup command failed: %w", err)
			}
		}

		output.Progress("Starting serve process")
		servePID, servePort, err = startServeProcess(cfg.Serve.Command, cfg.Serve.Port, wtPath, networkName, safeBranch)
		if err != nil {
			cleanup.run()
			return fmt.Errorf("starting serve process: %w", err)
		}
		cleanup.addProcess(servePID)
		output.Text("  Serve process listening on port %d (log: .cbox/serve.log)", servePort)

		proxyPort := cfg.Serve.ProxyPort
		if proxyPort <= 0 {
			proxyPort = 80
		}
		output.Progress("Ensuring Traefik proxy is running")
		if err := serve.EnsureTraefik(projectDir, projectName, proxyPort); err != nil {
			cleanup.run()
			return fmt.Errorf("starting traefik: %w", err)
		}

		// When container-based routing is configured, connect both Traefik and
		// the app container to the branch network so they can communicate.
		var containerHost string
		if cfg.Serve.Container != "" {
			traefikName := serve.TraefikContainerName(projectName)
			docker.NetworkConnect(networkName, traefikName)
			// Derive the devcontainer name: <worktree-basename>_devcontainer-<service>-1
			wtBase := filepath.Base(wtPath)
			containerHost = wtBase + "_devcontainer-" + cfg.Serve.Container + "-1"
			docker.NetworkConnect(networkName, containerHost)
		}

		if err := serve.AddRoute(projectDir, safeBranch, projectName, servePort, containerHost); err != nil {
			cleanup.run()
			return fmt.Errorf("adding traefik route: %w", err)
		}
		cleanup.addTraefikRoute(projectDir, safeBranch)
		if proxyPort == 80 {
			serveURL = fmt.Sprintf("http://%s.%s.dev.localhost", safeBranch, projectName)
		} else {
			serveURL = fmt.Sprintf("http://%s.%s.dev.localhost:%d", safeBranch, projectName, proxyPort)
		}
		output.Success("Serve URL: %s", serveURL)
	}

	// 4. Build runtime image
	output.Progress("Building %s image", rtBackend.DisplayName())
	buildOpts := docker.BuildOptions{NoCache: opts.Rebuild}
	if cfg.Dockerfile != "" {
		buildOpts.ProjectDockerfile = filepath.Join(projectDir, cfg.Dockerfile)
	}
	runtimeImage, err := rtBackend.BuildImage(projectName, buildOpts)
	if err != nil {
		cleanup.run()
		return fmt.Errorf("building %s image: %w", rtBackend.Name(), err)
	}
	output.Success("Built %s image %s", rtBackend.DisplayName(), runtimeImage)

	// 5. Stop/remove existing backend container
	runtimeContainerName := rtBackend.ContainerName(projectName, branch)
	docker.StopAndRemove(runtimeContainerName)

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
			} else {
				cleanup.addProcess(bridgePID)
				if len(bridgeMappings) > 0 {
					for _, m := range bridgeMappings {
						output.Text("  %s → TCP port %d", m.SocketName, m.TCPPort)
					}
				}
			}
		}
	}

	// 8. Start MCP proxy if host_commands or commands are configured
	var mcpPID, mcpPort int
	if len(cfg.HostCommands) > 0 || len(cfg.Commands) > 0 {
		output.Progress("Starting MCP host command server")
		mcpPID, mcpPort, err = startMCPProxy(projectDir, wtPath, branch, cfg.HostCommands, cfg.Commands, opts.ReportDir, servePort, time.Duration(cfg.CommandTimeout)*time.Second)
		if err != nil {
			output.Warning("MCP host command server failed: %v", err)
		} else {
			cleanup.addProcess(mcpPID)
			output.Text("  MCP server listening on port %d", mcpPort)
		}
	}

	runtimeSpec := backend.RuntimeSpec{
		ProjectDir:     projectDir,
		ProjectName:    projectName,
		Branch:         branch,
		WorktreePath:   wtPath,
		NetworkName:    networkName,
		GitMounts:      gitMounts,
		EnvVars:        cfg.Env,
		EnvFile:        envFile,
		BridgeMappings: bridgeMappings,
		Ports:          cfg.Ports,
		HostCommands:   cfg.HostCommands,
		Commands:       cfg.Commands,
		MCPPort:        mcpPort,
	}
	// 9. Start runtime container
	output.Progress("Starting %s container %s", rtBackend.DisplayName(), runtimeContainerName)
	runtimeContainerName, err = rtBackend.RunContainer(runtimeSpec, runtimeImage)
	if err != nil {
		cleanup.run()
		return fmt.Errorf("starting %s container: %w", rtBackend.Name(), err)
	}
	cleanup.addContainer(runtimeContainerName)

	// 10. Inject backend instructions when required after startup.
	output.Progress("Injecting %s instructions", rtBackend.DisplayName())
	if err := rtBackend.InjectInstructions(runtimeContainerName, runtimeSpec); err != nil {
		output.Warning("Could not inject backend instructions: %v", err)
	}

	// 11. Register MCP config inside the runtime when needed
	if mcpPort > 0 {
		output.Progress("Registering MCP config for %s", rtBackend.DisplayName())
		if err := rtBackend.RegisterMCP(runtimeContainerName, mcpPort); err != nil {
			output.Warning("Could not inject MCP config: %v", err)
		}
	}

	// 12. Save state — all resources created successfully, disarm rollback
	state := &State{
		Backend:          string(rtBackend.Name()),
		RuntimeContainer: runtimeContainerName,
		NetworkName:      networkName,
		WorktreePath:     worktreePath,
		Branch:           branch,
		SourceBranch:     sourceBranch,
		RuntimeImage:     runtimeImage,
		ProjectDir:       projectDir,
		Running:          true,
		Ports:            cfg.Ports,
		BridgeProxyPID:   bridgePID,
		BridgeMappings:   bridgeMappings,
		MCPProxyPID:      mcpPID,
		MCPProxyPort:     mcpPort,
		ServePID:         servePID,
		ServePort:        servePort,
		ServeURL:         serveURL,
	}
	if err := SaveState(projectDir, branch, state); err != nil {
		cleanup.run()
		return fmt.Errorf("saving state: %w", err)
	}
	cleanup.disarm()

	output.Success("Sandbox is running! Use 'cbox chat %s' to start %s.", branch, rtBackend.DisplayName())
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

	output.Progress("Stopping container %s", state.RuntimeContainer)
	if err := docker.StopAndRemove(state.RuntimeContainer); err != nil {
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

// Chat launches the configured backend interactively in the runtime container.
func Chat(projectDir, branch string, chrome bool, initialPrompt string, resume bool) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}
	rtBackend, err := backend.Get(backend.ParseName(state.Backend))
	if err != nil {
		return err
	}
	return rtBackend.Chat(state.RuntimeContainer, backend.ChatOptions{
		Chrome:        chrome,
		InitialPrompt: initialPrompt,
		Resume:        resume,
	})
}

// ChatPrompt runs a one-shot backend prompt in the runtime container.
func ChatPrompt(projectDir, branch, prompt, outputFormat string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}
	rtBackend, err := backend.Get(backend.ParseName(state.Backend))
	if err != nil {
		return err
	}
	return rtBackend.ChatPrompt(state.RuntimeContainer, prompt, outputFormat)
}

// HasConversationHistory checks if the backend has any conversation history for the sandbox on the given branch.
func HasConversationHistory(projectDir, branch string) (bool, error) {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return false, err
	}
	rtBackend, err := backend.Get(backend.ParseName(state.Backend))
	if err != nil {
		return false, err
	}
	return rtBackend.HasConversationHistory(state.RuntimeContainer)
}

// Shell opens an interactive shell in the runtime container.
func Shell(projectDir, branch string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}
	rtBackend, err := backend.Get(backend.ParseName(state.Backend))
	if err != nil {
		return err
	}
	return rtBackend.Shell(state.RuntimeContainer)
}

// Info prints the current sandbox state.
func Info(projectDir, branch string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	output.Text("Branch:           %s", state.Branch)
	output.Text("Backend:          %s", state.Backend)
	output.Text("Worktree:         %s", state.WorktreePath)
	output.Text("Runtime container: %s", state.RuntimeContainer)
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

	networkName := docker.NetworkName(projectName, branch)
	docker.CreateNetwork(networkName)

	// Run [serve] lifecycle commands before starting the serve process.
	if cfg.Serve.Up != "" {
		output.Progress("Running serve up command")
		if err := runServeLifecycleCommand(cfg.Serve.Up, state.WorktreePath, networkName, safeBranch); err != nil {
			return fmt.Errorf("serve up command failed: %w", err)
		}
	}
	if cfg.Serve.Setup != "" {
		output.Progress("Running serve setup command")
		if err := runServeLifecycleCommand(cfg.Serve.Setup, state.WorktreePath, networkName, safeBranch); err != nil {
			return fmt.Errorf("serve setup command failed: %w", err)
		}
	}

	output.Progress("Starting serve process")
	servePID, servePort, err := startServeProcess(cfg.Serve.Command, cfg.Serve.Port, state.WorktreePath, networkName, safeBranch)
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

	var containerHost string
	if cfg.Serve.Container != "" {
		traefikName := serve.TraefikContainerName(projectName)
		docker.NetworkConnect(networkName, traefikName)
		wtBase := filepath.Base(state.WorktreePath)
		containerHost = wtBase + "_devcontainer-" + cfg.Serve.Container + "-1"
		docker.NetworkConnect(networkName, containerHost)
	}

	if err := serve.AddRoute(projectDir, safeBranch, projectName, servePort, containerHost); err != nil {
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

// ServeClean runs the [serve] clean lifecycle command for a sandbox.
// This is used to tear down resources created by the serve setup (e.g. a branch database).
func ServeClean(projectDir, branch string) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	if cfg.Serve == nil || cfg.Serve.Clean == "" {
		return fmt.Errorf("no [serve] clean command configured in %s", config.ConfigFile)
	}

	safeBranch := strings.ReplaceAll(branch, "/", "-")
	networkName := docker.NetworkName(filepath.Base(projectDir), branch)

	output.Progress("Running serve clean command")
	if err := runServeLifecycleCommand(cfg.Serve.Clean, state.WorktreePath, networkName, safeBranch); err != nil {
		return fmt.Errorf("serve clean command failed: %w", err)
	}
	output.Success("Serve clean complete.")
	return nil
}

// CleanOptions configures optional behavior for sandbox cleanup.
type CleanOptions struct {
	Quiet      bool // Suppress progress output
	KeepBranch bool // Preserve the local git branch after removing the worktree
	Force      bool // Delete branch even if it has unpushed commits
}

// Clean stops the container, removes the network, worktree, and branch.
func Clean(projectDir, branch string) error {
	return CleanWithOptions(projectDir, branch, CleanOptions{})
}

// CleanQuiet is like Clean but suppresses progress output.
// Use this when Clean is called inside an output.Spin wrapper.
func CleanQuiet(projectDir, branch string) error {
	return CleanWithOptions(projectDir, branch, CleanOptions{Quiet: true})
}

// CleanWithOptions stops the container and removes resources with configurable behavior.
func CleanWithOptions(projectDir, branch string, opts CleanOptions) error {
	state, err := LoadState(projectDir, branch)
	if err != nil {
		return err
	}

	progress := output.Progress
	warning := output.Warning
	success := output.Success
	if opts.Quiet {
		noop := func(string, ...any) {}
		progress = noop
		warning = noop
		success = noop
	}

	// Check for unpushed commits before doing anything destructive.
	// Skipped when no worktree was used (we won't delete the branch).
	if state.WorktreePath != "" && state.WorktreePath != state.ProjectDir && !opts.KeepBranch && !opts.Force {
		if unpushed, err := worktree.HasUnpushedCommits(state.ProjectDir, state.Branch); err == nil && unpushed {
			return fmt.Errorf("branch '%s' has unpushed commits — use --keep-branch to preserve it or --force to delete anyway", state.Branch)
		}
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

	// Run [serve] clean lifecycle command if configured (e.g. drop branch database)
	cfg, cfgErr := config.Load(projectDir)
	if cfgErr == nil && cfg.Serve != nil && cfg.Serve.Clean != "" {
		safeBranch := strings.ReplaceAll(branch, "/", "-")
		networkName := docker.NetworkName(filepath.Base(projectDir), branch)
		progress("Running serve clean command")
		if err := runServeLifecycleCommand(cfg.Serve.Clean, state.WorktreePath, networkName, safeBranch); err != nil {
			warning("Serve clean command failed: %v", err)
		}
	}

	// Always attempt to stop and remove the container. The Running flag in
	// the state file can be stale (e.g. after a crash or if Down was called
	// but the container was restarted). StopAndRemove is safe to call even
	// when the container is already gone.
	progress("Stopping container %s", state.RuntimeContainer)
	if err := docker.StopAndRemove(state.RuntimeContainer); err != nil {
		warning("Could not remove container: %v", err)
	}

	// Remove network (safe to call even if already removed)
	progress("Removing network %s", state.NetworkName)
	docker.RemoveNetwork(state.NetworkName)

	// Remove worktree and branch (skipped when sandbox was started without a worktree)
	if state.WorktreePath != "" && state.WorktreePath != state.ProjectDir {
		progress("Removing worktree at %s", state.WorktreePath)
		if err := worktree.Remove(state.ProjectDir, state.WorktreePath); err != nil {
			warning("Could not remove worktree: %v", err)
		}

		if !opts.KeepBranch {
			worktree.DeleteBranch(state.ProjectDir, state.Branch)
		}
	}

	RemoveState(projectDir, branch)
	if state.WorktreePath != "" && state.WorktreePath != state.ProjectDir && opts.KeepBranch {
		success("Sandbox cleaned up. Branch '%s' preserved.", state.Branch)
	} else {
		success("Sandbox cleaned up.")
	}
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
func startMCPProxy(projectDir, worktreePath, branch string, hostCommands []string, namedCommands map[string]string, reportDir string, servePort int, commandTimeout time.Duration) (int, int, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return 0, 0, fmt.Errorf("finding executable: %w", err)
	}

	args := []string{"_mcp-proxy", "--worktree", worktreePath}

	// Store logs in the project .cbox dir, keyed by branch, so they're
	// outside the worktree volume mount.
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	logDir := filepath.Join(projectDir, ".cbox", "logs", safeBranch)
	args = append(args, "--log-dir", logDir)

	// Pass named commands as JSON via --commands flag, substituting $Port
	if len(namedCommands) > 0 {
		resolved := make(map[string]string, len(namedCommands))
		for name, expr := range namedCommands {
			resolved[name] = strings.ReplaceAll(expr, "$Port", fmt.Sprintf("%d", servePort))
		}
		cmdJSON, err := json.Marshal(resolved)
		if err != nil {
			return 0, 0, fmt.Errorf("marshaling commands: %w", err)
		}
		args = append(args, "--commands", string(cmdJSON))
	}

	// Pass report dir if set
	if reportDir != "" {
		args = append(args, "--report-dir", reportDir)
	}

	// Pass command timeout if set
	if commandTimeout > 0 {
		args = append(args, "--command-timeout", commandTimeout.String())
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
func startServeProcess(command string, fixedPort int, dir string, network string, branch string) (int, int, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return 0, 0, fmt.Errorf("finding executable: %w", err)
	}

	args := []string{"_serve-runner", "--command", command, "--port", fmt.Sprintf("%d", fixedPort), "--dir", dir}
	if network != "" {
		args = append(args, "--network", network)
	}
	if branch != "" {
		args = append(args, "--branch", branch)
	}

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

	var result struct {
		Port int `json:"port"`
	}
	scanner := bufio.NewScanner(stdout)
	found := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if err := json.Unmarshal([]byte(line), &result); err == nil && result.Port > 0 {
			found = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		cmd.Process.Kill()
		return 0, 0, fmt.Errorf("reading serve process output: %w", err)
	}
	if !found {
		cmd.Process.Kill()
		return 0, 0, fmt.Errorf("parsing serve process output: no JSON port line found")
	}

	return cmd.Process.Pid, result.Port, nil
}

// runServeLifecycleCommand runs a shell command synchronously before the serve
// process starts. It substitutes $Network so commands can reference the Docker
// network. Output goes to the serve log file.
func runServeLifecycleCommand(command, dir, network, branch string) error {
	expanded := strings.ReplaceAll(command, "$Network", network)
	expanded = strings.ReplaceAll(expanded, "$Branch", branch)
	cmd := exec.Command("sh", "-c", expanded)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// rollback tracks resources created during UpWithOptions so they can be
// cleaned up if a later step fails. The worktree is intentionally not
// tracked — it's preserved for debugging and reuse on the next attempt.
type rollback struct {
	disarmed      bool
	networks      []string
	containers    []string
	pids          []int
	traefikRoutes []struct{ projectDir, safeBranch string }
}

func (r *rollback) addNetwork(name string)    { r.networks = append(r.networks, name) }
func (r *rollback) addContainer(name string)   { r.containers = append(r.containers, name) }
func (r *rollback) addProcess(pid int)         { r.pids = append(r.pids, pid) }
func (r *rollback) addTraefikRoute(projectDir, safeBranch string) {
	r.traefikRoutes = append(r.traefikRoutes, struct{ projectDir, safeBranch string }{projectDir, safeBranch})
}

// disarm prevents rollback from running — call after all resources are
// successfully created and state is saved.
func (r *rollback) disarm() { r.disarmed = true }

// run tears down all tracked resources in reverse order.
func (r *rollback) run() {
	if r.disarmed {
		return
	}
	output.Warning("Cleaning up resources after failed startup...")
	for _, pid := range r.pids {
		stopProcess(pid)
	}
	for _, route := range r.traefikRoutes {
		serve.RemoveRoute(route.projectDir, route.safeBranch)
	}
	for _, name := range r.containers {
		docker.StopAndRemove(name)
	}
	for _, name := range r.networks {
		docker.RemoveNetwork(name)
	}
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
