package hostcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const commandTimeout = 120 * time.Second

// Report represents a single report from the inner Claude.
type Report struct {
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// FlowConfig holds flow-mode context for registering flow MCP tools.
type FlowConfig struct {
	ProjectDir string
	Branch     string
}

// Server is an MCP server that exposes a run_command tool for whitelisted commands
// and dedicated tools for named project commands.
type Server struct {
	worktreePath  string
	allowedCmds   map[string]bool
	namedCommands map[string]string
	reportDir     string
	flow          *FlowConfig
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

// SetReportDir enables the cbox_report tool and sets where reports are stored.
func (s *Server) SetReportDir(dir string) {
	s.reportDir = dir
}

// SetFlow enables flow-mode MCP tools (e.g. cbox_flow_pr).
func (s *Server) SetFlow(fc *FlowConfig) {
	s.flow = fc
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

	// Register report tool if report dir is set
	if s.reportDir != "" {
		mcpServer.AddTool(s.reportToolDefinition(), s.handleReport)
	}

	// Register flow tools if in flow mode
	if s.flow != nil {
		mcpServer.AddTool(s.flowPRToolDefinition(), s.handleFlowPR)
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

func (s *Server) reportToolDefinition() mcp.Tool {
	return mcp.NewTool(
		"cbox_report",
		mcp.WithDescription("Report progress or results back to the workflow orchestrator. "+
			"Use this to submit your plan, status updates, or completion summary."),
		mcp.WithString("type",
			mcp.Description("Report type: plan, status, or done"),
			mcp.Required(),
			mcp.Enum("plan", "status", "done"),
		),
		mcp.WithString("title",
			mcp.Description("Short summary"),
			mcp.Required(),
		),
		mcp.WithString("body",
			mcp.Description("Detailed content (plan, progress update, or completion summary)"),
			mcp.Required(),
		),
	)
}

func (s *Server) handleReport(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	reportType, err := request.RequireString("type")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: type"), nil
	}

	title, err := request.RequireString("title")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: title"), nil
	}

	body, err := request.RequireString("body")
	if err != nil {
		return mcp.NewToolResultError("missing required parameter: body"), nil
	}

	if err := os.MkdirAll(s.reportDir, 0755); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("creating report dir: %v", err)), nil
	}

	// Determine next sequence number
	seq := s.nextReportSequence()

	report := Report{
		Type:      reportType,
		Title:     title,
		Body:      body,
		CreatedAt: time.Now(),
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshaling report: %v", err)), nil
	}

	filename := fmt.Sprintf("%03d-%s.json", seq, reportType)
	path := filepath.Join(s.reportDir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("writing report: %v", err)), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Report saved as %s", filename)), nil
}

func (s *Server) flowPRToolDefinition() mcp.Tool {
	return mcp.NewTool(
		"cbox_flow_pr",
		mcp.WithDescription("Create a pull request for the current flow. "+
			"This pushes the branch and creates a PR using the project's configured PR command. "+
			"Only call this when you and the user agree the work is ready for review."),
	)
}

func (s *Server) handleFlowPR(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Import cycle prevention: we shell out to `cbox flow pr` instead of calling workflow.FlowPR directly
	selfPath, err := os.Executable()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("finding cbox executable: %v", err)), nil
	}

	cmd := exec.CommandContext(ctx, selfPath, "flow", "pr", s.flow.Branch)
	cmd.Dir = s.flow.ProjectDir
	output, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(output))

	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("flow pr failed:\n%s", result)), nil
	}
	return mcp.NewToolResultText(result), nil
}

func (s *Server) nextReportSequence() int {
	entries, err := os.ReadDir(s.reportDir)
	if err != nil {
		return 1
	}

	max := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) >= 3 {
			var n int
			if _, err := fmt.Sscanf(name, "%03d-", &n); err == nil && n > max {
				max = n
			}
		}
	}
	return max + 1
}

// LoadReports reads all reports from a report directory, sorted by filename.
func LoadReports(reportDir string) ([]Report, error) {
	entries, err := os.ReadDir(reportDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading report dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var reports []Report
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(reportDir, name))
		if err != nil {
			continue
		}
		var r Report
		if err := json.Unmarshal(data, &r); err != nil {
			continue
		}
		reports = append(reports, r)
	}
	return reports, nil
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
