package hostcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// ProxyInfo is printed as JSON to stdout for the parent process to read.
type ProxyInfo struct {
	Port int `json:"port"`
}

// RunProxyCommand starts the host command proxy server, prints port info to stdout,
// and blocks until SIGTERM. This is the implementation of the _host-cmd-proxy hidden command.
func RunProxyCommand(commands []string, worktreePath string) error {
	server := NewServer(commands, worktreePath)

	port, err := server.Start()
	if err != nil {
		return err
	}

	info := ProxyInfo{Port: port}
	data, err := json.Marshal(info)
	if err != nil {
		server.Stop()
		return err
	}

	fmt.Println(string(data))

	// Block until interrupted
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	server.Stop()
	return nil
}
