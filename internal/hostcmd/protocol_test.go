package hostcmd

import (
	"bytes"
	"io"
	"testing"
)

func TestWriteAndReadFrame(t *testing.T) {
	tests := []struct {
		name      string
		frameType byte
		data      []byte
	}{
		{"stdout frame", FrameStdout, []byte("hello world")},
		{"stderr frame", FrameStderr, []byte("error message")},
		{"stdin frame", FrameStdin, []byte("input data")},
		{"empty stdin EOF", FrameStdinEOF, nil},
		{"exit code", FrameExitCode, []byte{0, 0, 0, 0}},
		{"signal", FrameSignal, []byte{0, 0, 0, 2}},
		{"large payload", FrameStdout, bytes.Repeat([]byte("x"), 65536)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			if err := WriteFrame(&buf, tt.frameType, tt.data); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}

			gotType, gotData, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}

			if gotType != tt.frameType {
				t.Errorf("frame type = %d, want %d", gotType, tt.frameType)
			}
			if !bytes.Equal(gotData, tt.data) {
				t.Errorf("data length = %d, want %d", len(gotData), len(tt.data))
			}
		})
	}
}

func TestReadFrameEOF(t *testing.T) {
	var buf bytes.Buffer
	_, _, err := ReadFrame(&buf)
	if err != io.EOF && err != io.ErrUnexpectedEOF {
		t.Errorf("expected EOF-like error, got %v", err)
	}
}

func TestMultipleFrames(t *testing.T) {
	var buf bytes.Buffer

	frames := []struct {
		typ  byte
		data []byte
	}{
		{FrameStdout, []byte("line 1\n")},
		{FrameStderr, []byte("warning\n")},
		{FrameStdout, []byte("line 2\n")},
		{FrameExitCode, []byte{0, 0, 0, 0}},
	}

	for _, f := range frames {
		if err := WriteFrame(&buf, f.typ, f.data); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}

	for i, want := range frames {
		gotType, gotData, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame[%d]: %v", i, err)
		}
		if gotType != want.typ {
			t.Errorf("frame[%d] type = %d, want %d", i, gotType, want.typ)
		}
		if !bytes.Equal(gotData, want.data) {
			t.Errorf("frame[%d] data mismatch", i)
		}
	}
}
