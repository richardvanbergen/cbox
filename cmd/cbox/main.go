package main

import (
	"fmt"
	"os"

	"github.com/richvanbergen/cbox/internal/bridge"
	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/sandbox"
	"github.com/richvanbergen/cbox/internal/worktree"
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
	var branch string

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Create worktree, build images, and start sandboxed container",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()
			if branch == "" {
				state, err := sandbox.LoadState(dir)
				if err == nil {
					branch = state.Branch
				} else {
					current, err := worktree.CurrentBranch(dir)
					if err != nil {
						return fmt.Errorf("could not detect branch: %w", err)
					}
					branch = current
				}
			}
			return sandbox.Up(dir, branch)
		},
	}

	cmd.Flags().StringVar(&branch, "branch", "", "Branch name for the worktree")
	return cmd
}

func downCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the sandboxed container (keeps worktree)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Down(projectDir())
		},
	}
}

func chatCmd() *cobra.Command {
	var prompt string

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start Claude Code in the sandbox (interactive or one-shot with -p)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()

			var chrome bool
			if cfg, err := config.Load(dir); err == nil {
				chrome = cfg.Browser
			}

			if prompt != "" {
				return sandbox.ChatPrompt(dir, prompt)
			}
			return sandbox.Chat(dir, chrome)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "Run a one-shot prompt instead of interactive mode")
	return cmd
}

func execCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec [command...]",
		Short: "Run a command in the app container (no args for interactive shell)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Exec(projectDir(), args)
		},
		DisableFlagParsing: true,
	}
}

func shellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Open a shell in the Claude container (for debugging)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Shell(projectDir())
		},
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all git worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := projectDir()
			active := sandbox.ActiveBranch(dir)
			out, err := worktree.List(dir, active)
			if err != nil {
				return err
			}
			fmt.Println(out)
			return nil
		},
	}
}

func infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show current sandbox status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Info(projectDir())
		},
	}
}

func cleanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clean",
		Short: "Stop container, remove worktree and branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Clean(projectDir())
		},
	}
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
