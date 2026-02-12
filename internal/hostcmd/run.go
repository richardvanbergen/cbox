package hostcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// proxyOutput is the JSON written to stdout for the parent process to read.
type proxyOutput struct {
	Port int `json:"port"`
}

// RunProxyCommand starts the MCP server, prints the port as JSON, and blocks until signaled.
// timeoutSeconds sets the per-command timeout; 0 uses the default (120s).
func RunProxyCommand(worktreePath string, commands []string, namedCommands map[string]string, reportDir string, flow *FlowConfig, timeoutSeconds int) error {
	srv := NewServer(worktreePath, commands, namedCommands, timeoutSeconds)
	if reportDir != "" {
		srv.SetReportDir(reportDir)
	}
	if flow != nil {
		srv.SetFlow(flow)
	}

	port, err := srv.Start()
	if err != nil {
		return fmt.Errorf("starting MCP server: %w", err)
	}

	data, err := json.Marshal(proxyOutput{Port: port})
	if err != nil {
		srv.Stop()
		return fmt.Errorf("marshaling output: %w", err)
	}

	// Print port to stdout for the parent process to read
	fmt.Println(string(data))

	// Block until SIGTERM or SIGINT
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	srv.Stop()
	return nil
}
