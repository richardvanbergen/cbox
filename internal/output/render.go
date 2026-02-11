package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

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
		fmt.Fprintln(w, progressPrefix.Render("...")+" "+v.Message)
	case SuccessBlock:
		fmt.Fprintln(w, successPrefix.Render("✓")+" "+v.Message)
	case WarningBlock:
		fmt.Fprintln(w, warningPrefix.Render("!")+" "+v.Message)
	case ErrorBlock:
		fmt.Fprintln(w, errorPrefix.Render("✗")+" "+v.Message)
	}
}
