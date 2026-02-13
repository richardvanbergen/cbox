package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"path/filepath"

	"github.com/richvanbergen/cbox/internal/bridge"
	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/docker"
	"github.com/richvanbergen/cbox/internal/hostcmd"
	"github.com/richvanbergen/cbox/internal/output"
	"github.com/richvanbergen/cbox/internal/sandbox"
	"github.com/richvanbergen/cbox/internal/serve"
	"github.com/richvanbergen/cbox/internal/workflow"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:           "cbox",
		Short:         "Sandboxed development environments for Claude Code",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(helloCmd())
	root.AddCommand(initCmd())
	root.AddCommand(upCmd())
	root.AddCommand(downCmd())
	root.AddCommand(chatCmd())
	root.AddCommand(openCmd())
	root.AddCommand(shellCmd())
	root.AddCommand(listCmd())
	root.AddCommand(infoCmd())
	root.AddCommand(cleanCmd())
	root.AddCommand(serveCmd())
	root.AddCommand(runCmd())
	root.AddCommand(ejectCmd())
	root.AddCommand(completionCmd())
	root.AddCommand(flowCmd())
	root.AddCommand(bridgeProxyCmd())
	root.AddCommand(mcpProxyCmd())
	root.AddCommand(serveRunnerCmd())
	root.AddCommand(testOutputCmd())

	if err := root.Execute(); err != nil {
		output.Error("%v", err)
		os.Exit(1)
	}
}

func projectDir() string {
	dir, err := os.Getwd()
	if err != nil {
		output.Error("%v", err)
		os.Exit(1)
	}
	return dir
}

// sandboxCompletion returns a completion function that suggests existing cbox sandboxes.
func sandboxCompletion() func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		dir, err := os.Getwd()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		states, err := sandbox.ListStates(dir)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var completions []string
		for _, s := range states {
			if s.Branch != "" && strings.HasPrefix(s.Branch, toComplete) {
				completions = append(completions, s.Branch)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// flowCompletion returns a completion function that suggests existing flow branches.
func flowCompletion() func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		dir, err := os.Getwd()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		states, err := workflow.ListFlowStates(dir)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var completions []string
		for _, s := range states {
			if s.Branch != "" && strings.HasPrefix(s.Branch, toComplete) {
				completions = append(completions, s.Branch)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// configCommandCompletion returns a completion function that suggests commands from cbox.toml.
func configCommandCompletion() func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		dir, err := os.Getwd()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		cfg, err := config.Load(dir)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		var completions []string
		for name := range cfg.Commands {
			if strings.HasPrefix(name, toComplete) {
				completions = append(completions, name)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

func helloCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hello",
		Short: "Say hello",
		Run: func(cmd *cobra.Command, args []string) {
			output.Success("Hello from cbox!")
		},
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a cbox.toml config in the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()

			if _, err := os.Stat(config.ConfigFile); err == nil {
				return fmt.Errorf("%s already exists", config.ConfigFile)
			}
			if _, err := os.Stat(config.LegacyConfigFile); err == nil {
				return fmt.Errorf("%s already exists (rename to %s to use the new name)", config.LegacyConfigFile, config.ConfigFile)
			}

			cfg := config.DefaultConfig()
			if err := cfg.Save(dir); err != nil {
				return err
			}

			output.Success("Created %s", config.ConfigFile)
			output.Text("Edit the file to configure your commands, env vars, and host commands.")
			return nil
		},
	}
}

func upCmd() *cobra.Command {
	var rebuild bool

	cmd := &cobra.Command{
		Use:   "up <branch>",
		Short: "Create worktree and start sandboxed Claude container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Up(projectDir(), args[0], rebuild)
		},
	}

	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "Force a clean image rebuild (--no-cache)")
	return cmd
}

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "down <branch>",
		Short:             "Stop the sandboxed container (keeps worktree)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sandboxCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Down(projectDir(), args[0])
		},
	}
}

// runOpenCommand resolves and runs the open command.
// The command only runs if openFlag is true (i.e. --open was explicitly passed).
// When openFlag is true, flagValue is used; if empty, falls back to cfg.Open.
// Errors are warned about but don't block execution.
func runOpenCommand(cfg *config.Config, openFlag bool, flagValue, projectDir, branch string) {
	if !openFlag {
		return
	}
	openCmd := strings.TrimSpace(flagValue)
	if openCmd == "" && cfg != nil {
		openCmd = cfg.Open
	}
	if openCmd == "" {
		return
	}

	state, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		output.Warning("Could not load sandbox state for open command: %v", err)
		return
	}

	c := exec.Command("sh", "-c", openCmd)
	c.Env = append(os.Environ(), "Dir="+state.WorktreePath)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		output.Warning("Open command failed: %v", err)
	}
}

func openCmd() *cobra.Command {
	var openCmdFlag string

	cmd := &cobra.Command{
		Use:               "open <branch>",
		Short:             "Run the open command for a sandbox (without starting a chat)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sandboxCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()
			branch := args[0]

			cfg, _ := config.Load(dir)

			openExpr := openCmdFlag
			if openExpr == "" && cfg != nil {
				openExpr = cfg.Open
			}
			if openExpr == "" {
				return fmt.Errorf("no open command configured — set open in %s or pass --open", config.ConfigFile)
			}

			runOpenCommand(cfg, true, openCmdFlag, dir, branch)
			return nil
		},
	}

	cmd.Flags().StringVar(&openCmdFlag, "open", "", "Command to run (overrides config; use $Dir for worktree path)")
	return cmd
}

func chatCmd() *cobra.Command {
	var prompt string
	var openCmd string

	cmd := &cobra.Command{
		Use:               "chat <branch>",
		Short:             "Start Claude Code in the sandbox (interactive or one-shot with -p)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sandboxCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()
			branch := args[0]

			var chrome bool
			cfg, _ := config.Load(dir)
			if cfg != nil {
				chrome = cfg.Browser
			}

			openFlag := cmd.Flags().Changed("open")
			runOpenCommand(cfg, openFlag, openCmd, dir, branch)

			if prompt != "" {
				return sandbox.ChatPrompt(dir, branch, prompt)
			}
			return sandbox.Chat(dir, branch, chrome, "", false)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Run a one-shot prompt instead of interactive mode")
	cmd.Flags().StringVar(&openCmd, "open", "", "Run a command before chat (use $Dir for worktree path); omit value to use config default")
	cmd.Flags().Lookup("open").NoOptDefVal = " "
	return cmd
}

func shellCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "shell <branch>",
		Short:             "Open a shell in the Claude container (for debugging)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sandboxCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Shell(projectDir(), args[0])
		},
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tracked sandboxes",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()
			states, err := sandbox.ListStates(dir)
			if err != nil {
				return err
			}

			if len(states) == 0 {
				output.Text("No active sandboxes.")
				return nil
			}

			for _, s := range states {
				status := "unknown"
				if running, _ := docker.IsRunning(s.ClaudeContainer); running {
					status = "running"
				} else {
					status = "stopped"
				}
				output.Text("%-30s %s", s.Branch, status)
			}
			return nil
		},
	}
}

func infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "info <branch>",
		Short:             "Show current sandbox status",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sandboxCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Info(projectDir(), args[0])
		},
	}
}

func cleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "clean <branch>",
		Short:             "Stop container, remove worktree and branch",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sandboxCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Clean(projectDir(), args[0])
		},
	}
}

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Manage the serve process for a sandbox",
	}

	cmd.AddCommand(serveStartCmd())
	cmd.AddCommand(serveStopCmd())
	cmd.AddCommand(serveLogsCmd())

	return cmd
}

func serveStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "start <branch>",
		Short:             "Start the serve process and Traefik route",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sandboxCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Serve(projectDir(), args[0])
		},
	}
}

func serveStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "stop <branch>",
		Short:             "Stop the serve process and remove Traefik route",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sandboxCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.ServeStop(projectDir(), args[0])
		},
	}
}

func serveLogsCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:               "logs <branch>",
		Short:             "Show serve process output",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: sandboxCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			logPath, err := sandbox.ServeLogPath(projectDir(), args[0])
			if err != nil {
				return err
			}
			tailArgs := []string{"-n", "+1"}
			if follow {
				tailArgs = append(tailArgs, "-f")
			}
			tailArgs = append(tailArgs, logPath)
			c := exec.Command("tail", tailArgs...)
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	return cmd
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <command>",
		Short: "Run a named command from cbox.toml",
		Long: `Run a named command defined in the commands section of cbox.toml.
For example, if your config has:

  [commands]
  build = "go build ./..."
  test = "go test ./..."

Then 'cbox run build' will execute 'go build ./...' via sh -c.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: configCommandCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()
			cfg, err := config.Load(dir)
			if err != nil {
				return err
			}

			name := args[0]
			expr, ok := cfg.Commands[name]
			if !ok {
				available := make([]string, 0, len(cfg.Commands))
				for k := range cfg.Commands {
					available = append(available, k)
				}
				if len(available) == 0 {
					return fmt.Errorf("no commands defined in %s", config.ConfigFile)
				}
				return fmt.Errorf("unknown command %q (available: %s)", name, strings.Join(available, ", "))
			}

			c := exec.Command("sh", "-c", expr)
			c.Dir = dir
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}

func ejectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "eject",
		Short: "Copy the embedded Dockerfile into the project for customization",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()

			cfg, err := config.Load(dir)
			if err != nil {
				return fmt.Errorf("could not load %s — run 'cbox init' first: %w", config.ConfigFile, err)
			}

			if cfg.Dockerfile != "" {
				return fmt.Errorf("already ejected: %s references dockerfile %q", config.ConfigFile, cfg.Dockerfile)
			}

			data, err := docker.EmbeddedDockerfile()
			if err != nil {
				return fmt.Errorf("reading embedded Dockerfile: %w", err)
			}

			const filename = "Dockerfile.cbox"
			header := "# Ejected from cbox. Edit freely.\n" +
				"# Existing branches need rebuilding: cbox up --rebuild <branch>\n" +
				"# The entrypoint.sh remains managed by cbox and is injected at build time.\n\n"

			outPath := filepath.Join(dir, filename)
			if err := os.WriteFile(outPath, []byte(header+string(data)), 0644); err != nil {
				return fmt.Errorf("writing %s: %w", filename, err)
			}

			cfg.Dockerfile = filename
			if err := cfg.Save(dir); err != nil {
				return fmt.Errorf("updating %s: %w", config.ConfigFile, err)
			}

			output.Success("Created %s and updated %s.", filename, config.ConfigFile)
			output.Text("Edit Dockerfile.cbox to customize the container image.")
			output.Text("Rebuild existing branches with: cbox up --rebuild <branch>")
			return nil
		},
	}
}

func completionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion script",
		Long: `Generate a shell completion script for cbox.

To load completions:

Bash:
  $ source <(cbox completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ cbox completion bash > /etc/bash_completion.d/cbox
  # macOS:
  $ cbox completion bash > $(brew --prefix)/etc/bash_completion.d/cbox

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ cbox completion zsh > "${fpath[1]}/_cbox"

  # You may need to start a new shell for this setup to take effect.

Fish:
  $ cbox completion fish | source

  # To load completions for each session, execute once:
  $ cbox completion fish > ~/.config/fish/completions/cbox.fish
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			}
			return nil
		},
	}
	return cmd
}

func flowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flow",
		Short: "Workflow orchestration for automated development flows",
	}

	cmd.AddCommand(flowInitCmd())
	cmd.AddCommand(flowStartCmd())
	cmd.AddCommand(flowStatusCmd())
	cmd.AddCommand(flowCleanCmd())
	cmd.AddCommand(flowChatCmd())
	cmd.AddCommand(flowPRCmd())
	cmd.AddCommand(flowMergeCmd())
	cmd.AddCommand(flowAbandonCmd())

	return cmd
}

func flowInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Add default workflow config to cbox.toml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.FlowInit(projectDir())
		},
	}
}

func flowStartCmd() *cobra.Command {
	var description string
	var yolo bool
	var openCmd string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Begin a new workflow: create issue and sandbox",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if description == "" {
				cfg, _ := config.Load(projectDir())
				var editorCfg string
				if cfg != nil {
					editorCfg = cfg.Editor
				}
				var err error
				description, err = workflow.EditDescription(editorCfg)
				if err != nil {
					return err
				}
			}
			openFlag := cmd.Flags().Changed("open")
			return workflow.FlowStart(projectDir(), description, yolo, openFlag, openCmd)
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "Flow description (opens editor if omitted)")
	cmd.Flags().BoolVar(&yolo, "yolo", false, "Run all phases automatically (research, execute, PR)")
	cmd.Flags().StringVar(&openCmd, "open", "", "Run a command before chat (use $Dir for worktree path); omit value to use config default")
	cmd.Flags().Lookup("open").NoOptDefVal = " "
	return cmd
}

func flowStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "status [branch]",
		Short:             "Show status of active flows",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: flowCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch := ""
			if len(args) > 0 {
				branch = args[0]
			}
			return workflow.FlowStatus(projectDir(), branch)
		},
	}
}

func flowCleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Remove local resources for merged flows",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.FlowClean(projectDir())
		},
	}
}

func flowChatCmd() *cobra.Command {
	var openCmd string

	cmd := &cobra.Command{
		Use:               "chat <branch>",
		Short:             "Refresh task context and open interactive chat",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: flowCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			openFlag := cmd.Flags().Changed("open")
			return workflow.FlowChat(projectDir(), args[0], openFlag, openCmd)
		},
	}

	cmd.Flags().StringVar(&openCmd, "open", "", "Run a command before chat (use $Dir for worktree path); omit value to use config default")
	cmd.Flags().Lookup("open").NoOptDefVal = " "
	return cmd
}

func flowPRCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "pr <branch>",
		Short:             "Create a pull request for the flow",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: flowCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.FlowPR(projectDir(), args[0])
		},
	}
}

func flowMergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "merge <branch>",
		Short:             "Merge the PR and clean up",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: flowCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.FlowMerge(projectDir(), args[0])
		},
	}
}

func flowAbandonCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "abandon <branch>",
		Short:             "Cancel the flow and clean up",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: flowCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.FlowAbandon(projectDir(), args[0])
		},
	}
}

func testOutputCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_test-output",
		Short:  "Internal: render sample structured output blocks",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			blocks := []output.Block{
				output.ProgressBlock{Message: "Creating worktree for branch feature/auth..."},
				output.SuccessBlock{Message: "Worktree created at /tmp/cbox/feature-auth"},
				output.ProgressBlock{Message: "Building container image..."},
				output.SuccessBlock{Message: "Container image built"},
				output.ProgressBlock{Message: "Starting sandbox container..."},
				output.WarningBlock{Message: "Port 8080 is already in use, using 8081"},
				output.SuccessBlock{Message: "Sandbox running (container: cbox-feature-auth)"},
				output.ProgressBlock{Message: "Running Claude prompt..."},
				output.TextBlock{Text: "I'll help you implement the authentication module. Let me start by reading the existing code."},
				output.ToolUseBlock{
					ID:    "toolu_01ABC",
					Name:  "Read",
					Input: json.RawMessage(`{"file_path":"/workspace/internal/auth/auth.go"}`),
				},
				output.TextBlock{Text: "Now I'll create the login handler with session management."},
				output.ToolUseBlock{
					ID:    "toolu_02DEF",
					Name:  "Write",
					Input: json.RawMessage(`{"file_path":"/workspace/internal/auth/login.go","content":"package auth\n..."}`),
				},
				output.SuccessBlock{Message: "Claude prompt completed"},
				output.ErrorBlock{Message: "Failed to push branch: remote rejected"},
			}
			output.Render(os.Stdout, blocks)
			return nil
		},
	}
}

func serveRunnerCmd() *cobra.Command {
	var command string
	var port int
	var dir string

	cmd := &cobra.Command{
		Use:    "_serve-runner",
		Short:  "Internal: run a serve process with PORT injection",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return serve.RunServeCommand(command, port, dir)
		},
	}

	cmd.Flags().StringVar(&command, "command", "", "Shell command to run")
	cmd.MarkFlagRequired("command")
	cmd.Flags().IntVar(&port, "port", 0, "Fixed port (0 = auto-allocate)")
	cmd.Flags().StringVar(&dir, "dir", "", "Working directory")
	return cmd
}

func mcpProxyCmd() *cobra.Command {
	var worktreePath string
	var commandsJSON string
	var reportDir string
	var flowProjectDir string
	var flowBranch string

	cmd := &cobra.Command{
		Use:    "_mcp-proxy [host-commands...]",
		Short:  "Internal: MCP server for host and project commands",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var namedCommands map[string]string
			if commandsJSON != "" {
				if err := json.Unmarshal([]byte(commandsJSON), &namedCommands); err != nil {
					return fmt.Errorf("parsing --commands JSON: %w", err)
				}
			}
			var flow *hostcmd.FlowConfig
			if flowProjectDir != "" && flowBranch != "" {
				flow = &hostcmd.FlowConfig{
					ProjectDir: flowProjectDir,
					Branch:     flowBranch,
				}
			}
			return hostcmd.RunProxyCommand(worktreePath, args, namedCommands, reportDir, flow)
		},
	}

	cmd.Flags().StringVar(&worktreePath, "worktree", "", "Host worktree path for path translation")
	cmd.MarkFlagRequired("worktree")
	cmd.Flags().StringVar(&commandsJSON, "commands", "", "JSON map of named project commands")
	cmd.Flags().StringVar(&reportDir, "report-dir", "", "Directory for cbox_report tool output")
	cmd.Flags().StringVar(&flowProjectDir, "flow-project-dir", "", "Project dir for flow commands")
	cmd.Flags().StringVar(&flowBranch, "flow-branch", "", "Branch name for flow commands")
	return cmd
}

func bridgeProxyCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_bridge-proxy [socket-dir]",
		Short:  "Internal: TCP proxy for Chrome bridge sockets",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return bridge.RunProxyCommand(args[0])
		},
	}
}
