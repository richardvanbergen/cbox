package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/charmbracelet/lipgloss/v2"
)

var (
	progressPrefix = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	successPrefix  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	warningPrefix  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	errorPrefix    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))

	toolHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	toolBorder = lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("4")).
			PaddingLeft(1)
	toolInput = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	cmdBorder = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// Render writes all blocks to w in order, with a blank line between blocks.
func Render(w io.Writer, blocks []Block) {
	for i, b := range blocks {
		if i > 0 {
			fmt.Fprintln(w)
		}
		RenderBlock(w, b)
	}
}

// RenderBlock writes a single block to w.
func RenderBlock(w io.Writer, b Block) {
	switch v := b.(type) {
	case TextBlock:
		fmt.Fprintln(w, v.Text)
	case ToolUseBlock:
		header := toolHeader.Render(v.Name) + " " + v.ID
		var body string
		if len(v.Input) > 0 {
			var indented bytes.Buffer
			if json.Indent(&indented, v.Input, "", "  ") == nil {
				body = toolInput.Render(indented.String())
			}
		}
		var content string
		if body != "" {
			content = header + "\n" + body
		} else {
			content = header
		}
		fmt.Fprintln(w, toolBorder.Render(content))
	case ProgressBlock:
		fmt.Fprintln(w, progressPrefix.Render("›")+" "+v.Message)
	case SuccessBlock:
		fmt.Fprintln(w, successPrefix.Render("✓")+" "+v.Message)
	case WarningBlock:
		fmt.Fprintln(w, warningPrefix.Render("!")+" "+v.Message)
	case ErrorBlock:
		fmt.Fprintln(w, errorPrefix.Render("✗")+" "+v.Message)
	}
}

// Progress writes a styled progress message to stdout.
func Progress(format string, args ...any) {
	RenderBlock(os.Stdout, ProgressBlock{Message: fmt.Sprintf(format, args...)})
}

// Success writes a styled success message to stdout.
func Success(format string, args ...any) {
	RenderBlock(os.Stdout, SuccessBlock{Message: fmt.Sprintf(format, args...)})
}

// Warning writes a styled warning message to stdout.
func Warning(format string, args ...any) {
	RenderBlock(os.Stdout, WarningBlock{Message: fmt.Sprintf(format, args...)})
}

// Error writes a styled error message to stdout.
func Error(format string, args ...any) {
	RenderBlock(os.Stdout, ErrorBlock{Message: fmt.Sprintf(format, args...)})
}

// Text writes a styled text message to stdout.
func Text(format string, args ...any) {
	RenderBlock(os.Stdout, TextBlock{Text: fmt.Sprintf(format, args...)})
}

// CommandWriter wraps an io.Writer and prepends a dim "│ " border to each
// line of output. It is used to visually frame third-party command output
// (e.g. docker run) so it's easy to distinguish from cbox messages.
//
// For commands with interactive terminal output (e.g. docker build), connect
// cmd.Stdout/cmd.Stderr directly to os.Stdout/os.Stderr to preserve TTY.
type CommandWriter struct {
	w    io.Writer
	buf  []byte
	once sync.Once
}

// NewCommandWriter returns a CommandWriter that writes bordered lines to w.
func NewCommandWriter(w io.Writer) *CommandWriter {
	return &CommandWriter{w: w}
}

func (cw *CommandWriter) Write(p []byte) (int, error) {
	cw.once.Do(func() {
		fmt.Fprintln(cw.w)
	})

	cw.buf = append(cw.buf, p...)
	for {
		idx := bytes.IndexByte(cw.buf, '\n')
		if idx < 0 {
			break
		}
		line := cw.buf[:idx]
		cw.buf = cw.buf[idx+1:]
		prefix := cmdBorder.Render("│") + " "
		fmt.Fprintln(cw.w, prefix+string(line))
	}
	return len(p), nil
}

// Close flushes any remaining buffered content.
func (cw *CommandWriter) Close() {
	if len(cw.buf) > 0 {
		prefix := cmdBorder.Render("│") + " "
		fmt.Fprintln(cw.w, prefix+string(cw.buf))
		cw.buf = nil
	}
}

