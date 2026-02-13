package serve

import (
	"net"
	"strconv"
	"testing"
)

func TestAllocatePort_Fixed(t *testing.T) {
	port, err := AllocatePort(8080)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 8080 {
		t.Fatalf("expected 8080, got %d", port)
	}
}

func TestAllocatePort_Random(t *testing.T) {
	port, err := AllocatePort(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port <= 0 {
		t.Fatalf("expected positive port, got %d", port)
	}

	// Verify the port is actually available
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("allocated port %d is not available: %v", port, err)
	}
	ln.Close()
}
