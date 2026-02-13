package serve

import (
	"fmt"
	"net"
)

// AllocatePort returns a free TCP port. If fixedPort > 0, it is returned as-is.
// Otherwise, the OS assigns a random available port.
func AllocatePort(fixedPort int) (int, error) {
	if fixedPort > 0 {
		return fixedPort, nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocating port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}
