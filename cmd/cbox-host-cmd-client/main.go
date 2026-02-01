package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/richvanbergen/cbox/internal/hostcmd"
)

func main() {
	cmdName := filepath.Base(os.Args[0])
	args := os.Args[1:]

	addr := os.Getenv("CBOX_HOST_CMD_ADDR")
	if addr == "" {
		fmt.Fprintf(os.Stderr, "cbox-host-cmd-client: CBOX_HOST_CMD_ADDR not set\n")
		os.Exit(1)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cbox-host-cmd-client: %v\n", err)
		os.Exit(1)
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cbox-host-cmd-client: connect: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Send handshake
	req := hostcmd.HandshakeRequest{
		Cmd:  cmdName,
		Args: args,
		Cwd:  cwd,
	}
	reqData, _ := json.Marshal(req)
	reqData = append(reqData, '\n')
	if _, err := conn.Write(reqData); err != nil {
		fmt.Fprintf(os.Stderr, "cbox-host-cmd-client: handshake write: %v\n", err)
		os.Exit(1)
	}

	// Read handshake response
	reader := bufio.NewReader(conn)
	respLine, err := reader.ReadBytes('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "cbox-host-cmd-client: handshake read: %v\n", err)
		os.Exit(1)
	}

	var resp hostcmd.HandshakeResponse
	if err := json.Unmarshal(respLine, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "cbox-host-cmd-client: handshake parse: %v\n", err)
		os.Exit(1)
	}
	if resp.Error != "" {
		fmt.Fprintf(os.Stderr, "cbox-host-cmd-client: %s\n", resp.Error)
		os.Exit(1)
	}

	// Trap signals and forward as signal frames
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for sig := range sigCh {
			s, ok := sig.(syscall.Signal)
			if !ok {
				continue
			}
			data := make([]byte, 4)
			binary.BigEndian.PutUint32(data, uint32(s))
			hostcmd.WriteFrame(conn, hostcmd.FrameSignal, data)
		}
	}()

	var wg sync.WaitGroup

	// Forward local stdin to server
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if writeErr := hostcmd.WriteFrame(conn, hostcmd.FrameStdin, buf[:n]); writeErr != nil {
					return
				}
			}
			if err != nil {
				hostcmd.WriteFrame(conn, hostcmd.FrameStdinEOF, nil)
				return
			}
		}
	}()

	// Read frames from server
	exitCode := 1
	for {
		frameType, data, err := hostcmd.ReadFrame(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(os.Stderr, "cbox-host-cmd-client: read frame: %v\n", err)
			break
		}
		switch frameType {
		case hostcmd.FrameStdout:
			os.Stdout.Write(data)
		case hostcmd.FrameStderr:
			os.Stderr.Write(data)
		case hostcmd.FrameExitCode:
			if len(data) >= 4 {
				exitCode = int(int32(binary.BigEndian.Uint32(data)))
			}
			// Terminal frame - done
			os.Exit(exitCode)
		}
	}

	os.Exit(exitCode)
}
