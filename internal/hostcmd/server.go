package hostcmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const commandTimeout = 120 * time.Second

// Server is an MCP server that exposes a run_command tool for whitelisted commands
// and dedicated tools for named project commands.
type Server struct {
	worktreePath  string
	allowedCmds   map[string]bool
	namedCommands map[string]string
	listener      net.Listener
	httpServer    *http.Server
}

// NewServer creates a new MCP host command server.
func NewServer(worktreePath string, commands []string, namedCommands map[string]string) *Server {
	allowed := make(map[string]bool, len(commands))
	for _, c := range commands {
		allowed[c] = true
	}
	return &Server{
		worktreePath:  worktreePath,
		allowedCmds:   allowed,
		namedCommands: namedCommands,
	}
}

// Start listens on a random port and serves the MCP protocol. Returns the port.
func (s *Server) Start() (int, error) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return 0, fmt.Errorf("listening: %w", err)
	}
	s.listener = ln

	mcpServer := server.NewMCPServer(
		"cbox-host",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	if len(s.allowedCmds) > 0 {
		mcpServer.AddTool(s.toolDefinition(), s.handleRunCommand)
	}

	// Register each named command as a dedicated tool
	for name, expr := range s.namedCommands {
		mcpServer.AddTool(s.namedToolDefinition(name, expr), s.makeNamedCommandHandler(expr))
	}

	httpTransport := server.NewStreamableHTTPServer(mcpServer, server.WithStateLess(true))

	mux := http.NewServeMux()
	mux.Handle("/mcp", httpTransport)

	s.httpServer = &http.Server{Handler: mux}

	go s.httpServer.Serve(ln)

	return ln.Addr().(*net.TCPAddr).Port, nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}
}

func (s *Server) toolDefinition() mcp.Tool {
	names := make([]string, 0, len(s.allowedCmds))
	for name := range s.allowedCmds {
		names = append(names, name)
	}

	desc := fmt.Sprintf(
		"Run a command on the host machine. Allowed commands: %s. "+
			"Use this tool instead of running these commands directly in the container.",
		strings.Join(names, ", "),
	)

	return mcp.NewTool(
		"run_command",
		mcp.WithDescription(desc),
		mcp.WithString("command",
			mcp.Description("The command to run (must be in the whitelist)"),
			mcp.Required(),
		),
		mcp.WithArray("args",
			mcp.Description("Arguments to pass to the command"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithString("cwd",
			mcp.Description("Working directory (relative to /workspace, defaults to /workspace)"),
		),
	)
}

func (s *Server) handleRunCommand(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	command, err := request.RequireString("command")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: command"), nil
	}

	if !s.allowedCmds[command] {
		return mcp.NewToolResultError(fmt.Sprintf("command %q is not in the whitelist", command)), nil
	}

	var args []string
	if rawArgs, ok := request.GetArguments()["args"]; ok {
		if argSlice, ok := rawArgs.([]any); ok {
			for _, a := range argSlice {
				if str, ok := a.(string); ok {
					args = append(args, str)
				}
			}
		}
	}

	cwd := s.worktreePath
	if cwdArg := request.GetString("cwd", ""); cwdArg != "" {
		cwd = s.translatePath(cwdArg)
	}

	// Validate cwd is within worktree
	absWorktree, _ := filepath.Abs(s.worktreePath)
	absCwd, _ := filepath.Abs(cwd)
	if !strings.HasPrefix(absCwd, absWorktree) {
		return mcp.NewToolResultError("working directory must be within the workspace"), nil
	}

	execCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, command, args...)
	cmd.Dir = cwd

	output, err := cmd.CombinedOutput()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if execCtx.Err() == context.DeadlineExceeded {
			return mcp.NewToolResultError("command timed out after 120 seconds"), nil
		} else {
			return mcp.NewToolResultError(fmt.Sprintf("failed to execute command: %v", err)), nil
		}
	}

	result := fmt.Sprintf("exit_code: %d\n%s", exitCode, string(output))
	if exitCode != 0 {
		return mcp.NewToolResultError(result), nil
	}
	return mcp.NewToolResultText(result), nil
}

// namedToolDefinition creates an MCP tool definition for a named project command.
func (s *Server) namedToolDefinition(name, expr string) mcp.Tool {
	desc := fmt.Sprintf("Run the project's %s command: %s", name, expr)
	return mcp.NewTool(
		"cbox_"+name,
		mcp.WithDescription(desc),
	)
}

// makeNamedCommandHandler returns an MCP handler that runs the given shell expression.
func (s *Server) makeNamedCommandHandler(expr string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		execCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()

		cmd := exec.CommandContext(execCtx, "sh", "-c", expr)
		cmd.Dir = s.worktreePath

		output, err := cmd.CombinedOutput()

		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else if execCtx.Err() == context.DeadlineExceeded {
				return mcp.NewToolResultError("command timed out after 120 seconds"), nil
			} else {
				return mcp.NewToolResultError(fmt.Sprintf("failed to execute command: %v", err)), nil
			}
		}

		result := fmt.Sprintf("exit_code: %d\n%s", exitCode, string(output))
		if exitCode != 0 {
			return mcp.NewToolResultError(result), nil
		}
		return mcp.NewToolResultText(result), nil
	}
}

// translatePath converts /workspace/... paths to the host worktree path.
func (s *Server) translatePath(p string) string {
	if strings.HasPrefix(p, "/workspace") {
		return filepath.Join(s.worktreePath, strings.TrimPrefix(p, "/workspace"))
	}
	// Treat relative paths as relative to worktree
	if !filepath.IsAbs(p) {
		return filepath.Join(s.worktreePath, p)
	}
	return p
}
