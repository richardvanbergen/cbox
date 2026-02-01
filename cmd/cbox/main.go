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
	root.AddCommand(attachCmd())
	root.AddCommand(runCmd())
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

func attachCmd() *cobra.Command {
	var chat bool

	cmd := &cobra.Command{
		Use:   "attach",
		Short: "Attach to the running container (shell by default, --chat for Claude)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if chat {
				return sandbox.Chat(projectDir())
			}
			return sandbox.Attach(projectDir())
		},
	}

	cmd.Flags().BoolVar(&chat, "chat", false, "Start Claude instead of a shell")
	return cmd
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [prompt]",
		Short: "Run a one-shot Claude prompt in the sandbox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sandbox.Run(projectDir(), args[0])
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
