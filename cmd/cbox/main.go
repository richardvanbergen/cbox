package main

import (
	"fmt"
	"os"

	"github.com/richvanbergen/cbox/internal/bridge"
	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/docker"
	"github.com/richvanbergen/cbox/internal/hostcmd"
	"github.com/richvanbergen/cbox/internal/sandbox"
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
	root.AddCommand(execCmd())
	root.AddCommand(shellCmd())
	root.AddCommand(listCmd())
	root.AddCommand(infoCmd())
	root.AddCommand(cleanCmd())
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

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a .cbox.yml config in the current project",
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
			fmt.Println("Edit the file to configure your Dockerfile path, target, env vars, and ports.")
			return nil
		},
	}
}

func upCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up <branch>",
		Short: "Create worktree, build images, and start sandboxed container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Up(projectDir(), args[0])
		},
	}
}

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down <branch>",
		Short: "Stop the sandboxed container (keeps worktree)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Down(projectDir(), args[0])
		},
	}
}

func chatCmd() *cobra.Command {
	var prompt string

	cmd := &cobra.Command{
		Use:   "chat <branch>",
		Short: "Start Claude Code in the sandbox (interactive or one-shot with -p)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()
			branch := args[0]

			var chrome bool
			if cfg, err := config.Load(dir); err == nil {
				chrome = cfg.Browser
			}

			if prompt != "" {
				return sandbox.ChatPrompt(dir, branch, prompt)
			}
			return sandbox.Chat(dir, branch, chrome)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Run a one-shot prompt instead of interactive mode")
	return cmd
}

func execCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec <branch> [command...]",
		Short: "Run a command in the app container (no command args for interactive shell)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("requires at least 1 arg: the branch name")
			}
			return sandbox.Exec(projectDir(), args[0], args[1:])
		},
		DisableFlagParsing: true,
	}
}

func shellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell <branch>",
		Short: "Open a shell in the Claude container (for debugging)",
		Args:  cobra.ExactArgs(1),
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
		Use:   "info <branch>",
		Short: "Show current sandbox status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Info(projectDir(), args[0])
		},
	}
}

func cleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clean <branch>",
		Short: "Stop container, remove worktree and branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Clean(projectDir(), args[0])
		},
	}
}

func mcpProxyCmd() *cobra.Command {
	var worktreePath string

	cmd := &cobra.Command{
		Use:    "_mcp-proxy [commands...]",
		Short:  "Internal: MCP server for host commands",
		Hidden: true,
		Args:   cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return hostcmd.RunProxyCommand(worktreePath, args)
		},
	}

	cmd.Flags().StringVar(&worktreePath, "worktree", "", "Host worktree path for path translation")
	cmd.MarkFlagRequired("worktree")
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
