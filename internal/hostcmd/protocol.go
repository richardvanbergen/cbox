package hostcmd

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Frame types for the wire protocol.
const (
	FrameStdin    byte = 0 // client -> server
	FrameStdout   byte = 1 // server -> client
	FrameStderr   byte = 2 // server -> client
	FrameExitCode byte = 3 // server -> client (4-byte int32, terminal)
	FrameSignal   byte = 4 // client -> server (signal number)
	FrameStdinEOF byte = 5 // client -> server (0-length)
)

// HandshakeRequest is sent by the client as a JSON line to initiate a command.
type HandshakeRequest struct {
	Cmd  string   `json:"cmd"`
	Args []string `json:"args"`
	Cwd  string   `json:"cwd"`
}

// HandshakeResponse is sent by the server as a JSON line after receiving the request.
type HandshakeResponse struct {
	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
}

// WriteFrame writes a single frame to w: [1-byte type][4-byte big-endian length][data].
func WriteFrame(w io.Writer, frameType byte, data []byte) error {
	header := [5]byte{frameType}
	binary.BigEndian.PutUint32(header[1:], uint32(len(data)))
	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("writing frame header: %w", err)
	}
	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("writing frame data: %w", err)
		}
	}
	return nil
}

// ReadFrame reads a single frame from r, returning the type and data.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	frameType := header[0]
	length := binary.BigEndian.Uint32(header[1:])
	if length == 0 {
		return frameType, nil, nil
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, fmt.Errorf("reading frame data: %w", err)
	}
	return frameType, data, nil
}
