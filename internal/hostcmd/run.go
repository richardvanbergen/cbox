package hostcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// proxyOutput is the JSON written to stdout for the parent process to read.
type proxyOutput struct {
	Port int `json:"port"`
}

// RunProxyCommand starts the MCP server, prints the port as JSON, and blocks until signaled.
// commandTimeout of 0 uses the default (120s).
func RunProxyCommand(worktreePath string, commands []string, namedCommands map[string]string, reportDir, logDir string, commandTimeout time.Duration) error {
	srv := NewServer(worktreePath, commands, namedCommands)
	if reportDir != "" {
		srv.SetReportDir(reportDir)
	}
	if logDir != "" {
		srv.SetLogDir(logDir)
	}
	if commandTimeout > 0 {
		srv.SetCommandTimeout(commandTimeout)
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
