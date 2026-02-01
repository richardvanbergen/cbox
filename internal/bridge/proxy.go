package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// ProxyMapping maps a socket file to the TCP port the proxy is listening on.
type ProxyMapping struct {
	SocketName string `json:"socket_name"`
	TCPPort    int    `json:"tcp_port"`
}

// proxyState holds the listeners and wait group for a running proxy.
type proxyState struct {
	listeners []net.Listener
	wg        sync.WaitGroup
	done      chan struct{}
}

var activeProxy *proxyState

// DiscoverSockets returns the names of all *.sock files in dir.
func DiscoverSockets(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.sock"))
	if err != nil {
		return nil, fmt.Errorf("globbing sockets: %w", err)
	}
	var names []string
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	return names, nil
}

// StartProxy discovers Unix sockets in socketDir, opens a TCP listener for each,
// and bidirectionally copies between TCP connections and the Unix socket.
// Returns the mappings and any error. The proxy runs in the background until StopProxy is called.
func StartProxy(socketDir string) ([]ProxyMapping, error) {
	sockets, err := DiscoverSockets(socketDir)
	if err != nil {
		return nil, err
	}
	if len(sockets) == 0 {
		return nil, nil
	}

	state := &proxyState{
		done: make(chan struct{}),
	}

	var mappings []ProxyMapping

	for _, sockName := range sockets {
		sockPath := filepath.Join(socketDir, sockName)

		// Verify the socket is connectable
		testConn, err := net.Dial("unix", sockPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: socket %s not connectable, skipping: %v\n", sockName, err)
			continue
		}
		testConn.Close()

		ln, err := net.Listen("tcp", "0.0.0.0:0")
		if err != nil {
			return nil, fmt.Errorf("listening TCP for %s: %w", sockName, err)
		}

		port := ln.Addr().(*net.TCPAddr).Port
		state.listeners = append(state.listeners, ln)
		mappings = append(mappings, ProxyMapping{
			SocketName: sockName,
			TCPPort:    port,
		})

		state.wg.Add(1)
		go func(ln net.Listener, sockPath string) {
			defer state.wg.Done()
			for {
				tcpConn, err := ln.Accept()
				if err != nil {
					select {
					case <-state.done:
						return
					default:
						fmt.Fprintf(os.Stderr, "bridge accept error: %v\n", err)
						return
					}
				}

				go relay(tcpConn, sockPath)
			}
		}(ln, sockPath)
	}

	activeProxy = state
	return mappings, nil
}

// StopProxy shuts down the running proxy.
func StopProxy() {
	if activeProxy == nil {
		return
	}

	close(activeProxy.done)
	for _, ln := range activeProxy.listeners {
		ln.Close()
	}
	activeProxy.wg.Wait()
	activeProxy = nil
}

// relay connects to a Unix socket and bidirectionally copies data with the TCP connection.
func relay(tcpConn net.Conn, sockPath string) {
	defer tcpConn.Close()

	unixConn, err := net.Dial("unix", sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bridge: failed to connect to %s: %v\n", sockPath, err)
		return
	}
	defer unixConn.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(unixConn, tcpConn)
		// Signal the other direction to stop
		if c, ok := unixConn.(*net.UnixConn); ok {
			c.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(tcpConn, unixConn)
		// Signal the other direction to stop
		if c, ok := tcpConn.(*net.TCPConn); ok {
			c.CloseWrite()
		}
	}()

	wg.Wait()
}

// MarshalMappings returns the JSON encoding of the mappings.
func MarshalMappings(mappings []ProxyMapping) (string, error) {
	data, err := json.Marshal(mappings)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RunProxyCommand is the implementation of the _bridge-proxy hidden command.
// It starts the proxy, prints mappings as JSON to stdout, then blocks until interrupted.
func RunProxyCommand(socketDir string) error {
	mappings, err := StartProxy(socketDir)
	if err != nil {
		return err
	}

	data, err := json.Marshal(mappings)
	if err != nil {
		return err
	}

	// Print mappings to stdout for the parent process to read
	fmt.Println(string(data))

	if activeProxy == nil {
		return nil
	}

	// Block until proxy is stopped
	<-activeProxy.done
	return nil
}
