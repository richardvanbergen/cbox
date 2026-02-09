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
	"github.com/richvanbergen/cbox/internal/sandbox"
	"github.com/richvanbergen/cbox/internal/workflow"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "cbox",
		Short: "Sandboxed development environments for Claude Code",
	}

	root.AddCommand(initCmd())
	root.AddCommand(upCmd())
	root.AddCommand(downCmd())
	root.AddCommand(chatCmd())
	root.AddCommand(shellCmd())
	root.AddCommand(listCmd())
	root.AddCommand(infoCmd())
	root.AddCommand(cleanCmd())
	root.AddCommand(runCmd())
	root.AddCommand(ejectCmd())
	root.AddCommand(completionCmd())
	root.AddCommand(flowCmd())
	root.AddCommand(bridgeProxyCmd())
	root.AddCommand(mcpProxyCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func projectDir() string {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return dir
}

// branchCompletion returns a completion function that suggests git branches.
func branchCompletion() func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		out, err := exec.Command("git", "branch", "--format=%(refname:short)").Output()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}

		branches := strings.Split(strings.TrimSpace(string(out)), "\n")
		var completions []string
		for _, b := range branches {
			if b != "" && strings.HasPrefix(b, toComplete) {
				completions = append(completions, b)
			}
		}
		return completions, cobra.ShellCompDirectiveNoFileComp
	}
}

// configCommandCompletion returns a completion function that suggests commands from .cbox.toml.
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

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a .cbox.toml config in the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()

			if _, err := os.Stat(config.ConfigFile); err == nil {
				return fmt.Errorf("%s already exists", config.ConfigFile)
			}

			cfg := config.DefaultConfig()
			if err := cfg.Save(dir); err != nil {
				return err
			}

			fmt.Printf("Created %s\n", config.ConfigFile)
			fmt.Println("Edit the file to configure your commands, env vars, and host commands.")
			return nil
		},
	}
}

func upCmd() *cobra.Command {
	var rebuild bool

	cmd := &cobra.Command{
		Use:               "up <branch>",
		Short:             "Create worktree and start sandboxed Claude container",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: branchCompletion(),
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
		ValidArgsFunction: branchCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Down(projectDir(), args[0])
		},
	}
}

// runOpenCommand resolves and runs the open command (flag overrides config).
// Errors are warned about but don't block execution.
func runOpenCommand(cfg *config.Config, flagValue, projectDir, branch string) {
	openCmd := flagValue
	if openCmd == "" && cfg != nil {
		openCmd = cfg.Open
	}
	if openCmd == "" {
		return
	}

	state, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load sandbox state for open command: %v\n", err)
		return
	}

	c := exec.Command("sh", "-c", openCmd)
	c.Env = append(os.Environ(), "Dir="+state.WorktreePath)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: open command failed: %v\n", err)
	}
}

func chatCmd() *cobra.Command {
	var prompt string
	var openCmd string

	cmd := &cobra.Command{
		Use:               "chat <branch>",
		Short:             "Start Claude Code in the sandbox (interactive or one-shot with -p)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: branchCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()
			branch := args[0]

			var chrome bool
			cfg, _ := config.Load(dir)
			if cfg != nil {
				chrome = cfg.Browser
			}

			runOpenCommand(cfg, openCmd, dir, branch)

			if prompt != "" {
				return sandbox.ChatPrompt(dir, branch, prompt)
			}
			return sandbox.Chat(dir, branch, chrome, "", false)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Run a one-shot prompt instead of interactive mode")
	cmd.Flags().StringVar(&openCmd, "open", "", "Run a command before chat (use $Dir for worktree path)")
	return cmd
}

func shellCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "shell <branch>",
		Short:             "Open a shell in the Claude container (for debugging)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: branchCompletion(),
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
				fmt.Println("No active sandboxes.")
				return nil
			}

			for _, s := range states {
				status := "unknown"
				if running, _ := docker.IsRunning(s.ClaudeContainer); running {
					status = "running"
				} else {
					status = "stopped"
				}
				fmt.Printf("%-30s %s\n", s.Branch, status)
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
		ValidArgsFunction: branchCompletion(),
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
		ValidArgsFunction: branchCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Clean(projectDir(), args[0])
		},
	}
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <command>",
		Short: "Run a named command from .cbox.toml",
		Long: `Run a named command defined in the commands section of .cbox.toml.
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

			fmt.Printf("Created %s and updated %s.\n", filename, config.ConfigFile)
			fmt.Println("Edit Dockerfile.cbox to customize the container image.")
			fmt.Println("Rebuild existing branches with: cbox up --rebuild <branch>")
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
	cmd.AddCommand(flowChatCmd())
	cmd.AddCommand(flowPRCmd())
	cmd.AddCommand(flowMergeCmd())
	cmd.AddCommand(flowAbandonCmd())

	return cmd
}

func flowInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Add default workflow config to .cbox.toml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.FlowInit(projectDir())
		},
	}
}

func flowStartCmd() *cobra.Command {
	var yolo bool

	cmd := &cobra.Command{
		Use:   "start <description>",
		Short: "Begin a new workflow: create issue and sandbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.FlowStart(projectDir(), args[0], yolo)
		},
	}

	cmd.Flags().BoolVar(&yolo, "yolo", false, "Run all phases automatically (research, execute, PR)")
	return cmd
}

func flowStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [branch]",
		Short: "Show status of active flows",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch := ""
			if len(args) > 0 {
				branch = args[0]
			}
			return workflow.FlowStatus(projectDir(), branch)
		},
	}
}

func flowChatCmd() *cobra.Command {
	var openCmd string

	cmd := &cobra.Command{
		Use:               "chat <branch>",
		Short:             "Refresh task context and open interactive chat",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: branchCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.FlowChat(projectDir(), args[0], openCmd)
		},
	}

	cmd.Flags().StringVar(&openCmd, "open", "", "Run a command before chat (use $Dir for worktree path)")
	return cmd
}

func flowPRCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "pr <branch>",
		Short:             "Create a pull request for the flow",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: branchCompletion(),
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
		ValidArgsFunction: branchCompletion(),
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
		ValidArgsFunction: branchCompletion(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.FlowAbandon(projectDir(), args[0])
		},
	}
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
