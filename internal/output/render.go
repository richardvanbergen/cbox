package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Render writes all blocks to w in order.
func Render(w io.Writer, blocks []Block) {
	for _, b := range blocks {
		RenderBlock(w, b)
	}
}

// RenderBlock writes a single block to w.
func RenderBlock(w io.Writer, b Block) {
	switch v := b.(type) {
	case TextBlock:
		fmt.Fprintln(w, v.Text)
	case ToolUseBlock:
		fmt.Fprintf(w, "[tool_use] %s (id: %s)\n", v.Name, v.ID)
		if len(v.Input) > 0 {
			var indented bytes.Buffer
			if json.Indent(&indented, v.Input, "  ", "  ") == nil {
				for _, line := range strings.Split(indented.String(), "\n") {
					fmt.Fprintf(w, "  %s\n", line)
				}
			}
		}
	case ProgressBlock:
		fmt.Fprintf(w, "... %s\n", v.Message)
	case SuccessBlock:
		fmt.Fprintf(w, "  ✓ %s\n", v.Message)
	case WarningBlock:
		fmt.Fprintf(w, "  ! %s\n", v.Message)
	case ErrorBlock:
		fmt.Fprintf(w, "  ✗ %s\n", v.Message)
	}
}
