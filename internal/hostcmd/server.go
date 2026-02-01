package hostcmd

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// Server is a TCP server that proxies whitelisted commands from a container to the host.
type Server struct {
	commands    map[string]bool
	worktreePath string
	listener    net.Listener
	wg          sync.WaitGroup
	done        chan struct{}
}

// NewServer creates a new host command proxy server.
func NewServer(commands []string, worktreePath string) *Server {
	allowed := make(map[string]bool, len(commands))
	for _, c := range commands {
		allowed[c] = true
	}
	return &Server{
		commands:    allowed,
		worktreePath: worktreePath,
		done:        make(chan struct{}),
	}
}

// Start begins listening on a random TCP port. Returns the port number.
func (s *Server) Start() (int, error) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return 0, fmt.Errorf("listening: %w", err)
	}
	s.listener = ln
	port := ln.Addr().(*net.TCPAddr).Port

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-s.done:
					return
				default:
					fmt.Fprintf(os.Stderr, "hostcmd accept error: %v\n", err)
					return
				}
			}
			go s.handleConn(conn)
		}
	}()

	return port, nil
}

// Stop shuts down the server and waits for active connections to finish.
func (s *Server) Stop() {
	close(s.done)
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
}

// translateCwd converts a container path (/workspace/...) to the host worktree path.
// Returns an error if the path would escape the worktree.
func (s *Server) translateCwd(containerCwd string) (string, error) {
	const containerPrefix = "/workspace"
	if !strings.HasPrefix(containerCwd, containerPrefix) {
		return s.worktreePath, nil
	}

	rel := strings.TrimPrefix(containerCwd, containerPrefix)
	rel = strings.TrimPrefix(rel, "/")

	if rel == "" {
		return s.worktreePath, nil
	}

	hostPath := filepath.Join(s.worktreePath, rel)

	// Ensure the resolved path is within the worktree
	resolved, err := filepath.Abs(hostPath)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}
	absWorktree, err := filepath.Abs(s.worktreePath)
	if err != nil {
		return "", fmt.Errorf("resolving worktree: %w", err)
	}
	if !strings.HasPrefix(resolved, absWorktree) {
		return "", fmt.Errorf("path %q escapes worktree", containerCwd)
	}

	return hostPath, nil
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Read handshake (JSON line)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "hostcmd: reading handshake: %v\n", err)
		return
	}

	var req HandshakeRequest
	if err := json.Unmarshal(line, &req); err != nil {
		writeHandshakeError(conn, "invalid handshake JSON")
		return
	}

	// Validate command against whitelist
	if !s.commands[req.Cmd] {
		writeHandshakeError(conn, fmt.Sprintf("command not allowed: %s", req.Cmd))
		return
	}

	// Translate cwd
	hostCwd, err := s.translateCwd(req.Cwd)
	if err != nil {
		writeHandshakeError(conn, err.Error())
		return
	}

	// Send OK response
	resp := HandshakeResponse{OK: true}
	respData, _ := json.Marshal(resp)
	respData = append(respData, '\n')
	if _, err := conn.Write(respData); err != nil {
		return
	}

	// Spawn the command
	cmd := exec.Command(req.Cmd, req.Args...)
	cmd.Dir = hostCwd

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		sendExitCode(conn, 1)
		return
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		sendExitCode(conn, 1)
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		sendExitCode(conn, 1)
		return
	}

	if err := cmd.Start(); err != nil {
		sendExitCode(conn, 127)
		return
	}

	var ioWg sync.WaitGroup

	// Forward stdout -> client
	ioWg.Add(1)
	go func() {
		defer ioWg.Done()
		pipeToFrames(conn, stdoutPipe, FrameStdout)
	}()

	// Forward stderr -> client
	ioWg.Add(1)
	go func() {
		defer ioWg.Done()
		pipeToFrames(conn, stderrPipe, FrameStderr)
	}()

	// Read frames from client (stdin data, signals, EOF)
	ioWg.Add(1)
	go func() {
		defer ioWg.Done()
		for {
			frameType, data, err := ReadFrame(reader)
			if err != nil {
				stdinPipe.Close()
				return
			}
			switch frameType {
			case FrameStdin:
				stdinPipe.Write(data)
			case FrameStdinEOF:
				stdinPipe.Close()
			case FrameSignal:
				if len(data) >= 4 && cmd.Process != nil {
					sig := syscall.Signal(binary.BigEndian.Uint32(data))
					cmd.Process.Signal(sig)
				}
			}
		}
	}()

	// Wait for command to finish
	err = cmd.Wait()
	ioWg.Wait()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	sendExitCode(conn, exitCode)
}

func writeHandshakeError(conn net.Conn, msg string) {
	resp := HandshakeResponse{Error: msg}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	conn.Write(data)
}

func sendExitCode(conn net.Conn, code int) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, uint32(int32(code)))
	WriteFrame(conn, FrameExitCode, data)
}

func pipeToFrames(conn net.Conn, r io.Reader, frameType byte) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if writeErr := WriteFrame(conn, frameType, buf[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
