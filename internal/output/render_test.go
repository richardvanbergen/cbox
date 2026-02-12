package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRenderTextBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, TextBlock{Text: "hello world"})
	got := buf.String()
	if !strings.Contains(got, "hello world") {
		t.Errorf("TextBlock: got %q, want it to contain %q", got, "hello world")
	}
}

func TestRenderToolUseBlock(t *testing.T) {
	var buf bytes.Buffer
	input := json.RawMessage(`{"path":"/tmp"}`)
	RenderBlock(&buf, ToolUseBlock{ID: "abc123", Name: "Read", Input: input})
	got := buf.String()
	if !strings.Contains(got, "Read") {
		t.Errorf("ToolUseBlock: expected tool name 'Read' in output, got %q", got)
	}
	if !strings.Contains(got, "abc123") {
		t.Errorf("ToolUseBlock: expected ID 'abc123' in output, got %q", got)
	}
	if !strings.Contains(got, `"path"`) {
		t.Errorf("ToolUseBlock: expected JSON key 'path' in output, got %q", got)
	}
}

func TestRenderToolUseBlockNoInput(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, ToolUseBlock{ID: "x", Name: "Bash", Input: nil})
	got := buf.String()
	if !strings.Contains(got, "Bash") {
		t.Errorf("ToolUseBlock no input: expected tool name 'Bash', got %q", got)
	}
	if !strings.Contains(got, "x") {
		t.Errorf("ToolUseBlock no input: expected ID 'x', got %q", got)
	}
}

func TestRenderProgressBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, ProgressBlock{Message: "creating sandbox"})
	got := buf.String()
	if !strings.Contains(got, "›") {
		t.Errorf("ProgressBlock: expected '›' prefix, got %q", got)
	}
	if !strings.Contains(got, "creating sandbox") {
		t.Errorf("ProgressBlock: expected message, got %q", got)
	}
}

func TestRenderSuccessBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, SuccessBlock{Message: "done"})
	got := buf.String()
	if !strings.Contains(got, "✓") {
		t.Errorf("SuccessBlock: expected '✓' prefix, got %q", got)
	}
	if !strings.Contains(got, "done") {
		t.Errorf("SuccessBlock: expected message, got %q", got)
	}
}

func TestRenderWarningBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, WarningBlock{Message: "disk low"})
	got := buf.String()
	if !strings.Contains(got, "!") {
		t.Errorf("WarningBlock: expected '!' prefix, got %q", got)
	}
	if !strings.Contains(got, "disk low") {
		t.Errorf("WarningBlock: expected message, got %q", got)
	}
}

func TestRenderErrorBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, ErrorBlock{Message: "failed"})
	got := buf.String()
	if !strings.Contains(got, "✗") {
		t.Errorf("ErrorBlock: expected '✗' prefix, got %q", got)
	}
	if !strings.Contains(got, "failed") {
		t.Errorf("ErrorBlock: expected message, got %q", got)
	}
}

func TestRenderBatchOrder(t *testing.T) {
	var buf bytes.Buffer
	blocks := []Block{
		ProgressBlock{Message: "step 1"},
		TextBlock{Text: "step 2"},
		SuccessBlock{Message: "step 3"},
	}
	Render(&buf, blocks)
	got := buf.String()

	pos1 := strings.Index(got, "step 1")
	pos2 := strings.Index(got, "step 2")
	pos3 := strings.Index(got, "step 3")

	if pos1 == -1 || pos2 == -1 || pos3 == -1 {
		t.Fatalf("missing content: got %q", got)
	}
	if pos1 >= pos2 || pos2 >= pos3 {
		t.Errorf("blocks rendered out of order: positions %d, %d, %d", pos1, pos2, pos3)
	}
}

func TestParseClaudeBlocksRoundtrip(t *testing.T) {
	input := `[
		{"type":"text","text":"Hello from Claude"},
		{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"ls"}},
		{"type":"unknown_future","text":"something new"}
	]`
	blocks, err := ParseClaudeBlocks([]byte(input))
	if err != nil {
		t.Fatalf("ParseClaudeBlocks: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}

	// Verify types
	if blocks[0].BlockType() != "text" {
		t.Errorf("block 0: got type %q", blocks[0].BlockType())
	}
	if blocks[1].BlockType() != "tool_use" {
		t.Errorf("block 1: got type %q", blocks[1].BlockType())
	}
	// Unknown type falls through to text
	if blocks[2].BlockType() != "text" {
		t.Errorf("block 2: got type %q, want text (fallback)", blocks[2].BlockType())
	}

	// Render and check output contains semantic content
	var buf bytes.Buffer
	Render(&buf, blocks)
	out := buf.String()
	if !strings.Contains(out, "Hello from Claude") {
		t.Errorf("missing text block content in output")
	}
	if !strings.Contains(out, "Bash") {
		t.Errorf("missing tool name in output")
	}
	if !strings.Contains(out, "tu_1") {
		t.Errorf("missing tool ID in output")
	}
	if !strings.Contains(out, "[unknown_future]") {
		t.Errorf("missing fallback block in output")
	}
}

func TestParseClaudeBlocksInvalidJSON(t *testing.T) {
	_, err := ParseClaudeBlocks([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestConvenienceFunctions(t *testing.T) {
	tests := []struct {
		name   string
		call   func()
		prefix string
		msg    string
	}{
		{"Progress", func() { Progress("step %d", 1) }, "›", "step 1"},
		{"Success", func() { Success("done %s", "ok") }, "✓", "done ok"},
		{"Warning", func() { Warning("low %s", "disk") }, "!", "low disk"},
		{"Error", func() { Error("fail %d", 42) }, "✗", "fail 42"},
		{"Text", func() { Text("info %s", "val") }, "", "info val"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, w, _ := os.Pipe()
			old := os.Stdout
			os.Stdout = w

			tt.call()

			w.Close()
			os.Stdout = old

			var buf bytes.Buffer
			buf.ReadFrom(r)
			got := buf.String()

			if tt.prefix != "" && !strings.Contains(got, tt.prefix) {
				t.Errorf("expected prefix %q in output %q", tt.prefix, got)
			}
			if !strings.Contains(got, tt.msg) {
				t.Errorf("expected message %q in output %q", tt.msg, got)
			}
		})
	}
}

func TestCommandWriter(t *testing.T) {
	var buf bytes.Buffer
	cw := NewCommandWriter(&buf)
	cw.Write([]byte("line one\nline two\n"))
	cw.Close()
	got := buf.String()

	if !strings.Contains(got, "│") {
		t.Errorf("expected '│' border in output, got %q", got)
	}
	if !strings.Contains(got, "line one") {
		t.Errorf("expected 'line one' in output, got %q", got)
	}
	if !strings.Contains(got, "line two") {
		t.Errorf("expected 'line two' in output, got %q", got)
	}

	// Verify each content line is prefixed with the border
	lines := strings.Split(strings.TrimSpace(got), "\n")
	// First line is blank (visual separator), remaining lines have content
	contentLines := 0
	for _, line := range lines {
		if strings.Contains(line, "│") {
			contentLines++
		}
	}
	if contentLines != 2 {
		t.Errorf("expected 2 bordered lines, got %d in %q", contentLines, got)
	}
}

func TestCommandWriterPartialLines(t *testing.T) {
	var buf bytes.Buffer
	cw := NewCommandWriter(&buf)
	cw.Write([]byte("partial"))
	cw.Write([]byte(" line\ncomplete\n"))
	cw.Close()
	got := buf.String()

	if !strings.Contains(got, "partial line") {
		t.Errorf("expected buffered 'partial line' in output, got %q", got)
	}
	if !strings.Contains(got, "complete") {
		t.Errorf("expected 'complete' in output, got %q", got)
	}
}

func TestCommandWriterFlushOnClose(t *testing.T) {
	var buf bytes.Buffer
	cw := NewCommandWriter(&buf)
	cw.Write([]byte("no newline"))
	cw.Close()
	got := buf.String()

	if !strings.Contains(got, "│") {
		t.Errorf("expected '│' border in output, got %q", got)
	}
	if !strings.Contains(got, "no newline") {
		t.Errorf("expected 'no newline' in output, got %q", got)
	}
}

func TestPassthroughWriter(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPassthroughWriter(&buf)
	pw.Write([]byte("line one\nline two\n"))
	pw.Close()
	got := buf.String()

	if strings.Contains(got, "│") {
		t.Errorf("PassthroughWriter should not add border, got %q", got)
	}
	if !strings.Contains(got, "line one") {
		t.Errorf("expected 'line one' in output, got %q", got)
	}
	if !strings.Contains(got, "line two") {
		t.Errorf("expected 'line two' in output, got %q", got)
	}
}

func TestPassthroughWriterPreservesControlChars(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPassthroughWriter(&buf)
	// Simulate docker build output with carriage return for in-place updates
	pw.Write([]byte("#5 [1/3] RUN something\r"))
	pw.Write([]byte("#5 CACHED\n"))
	pw.Close()
	got := buf.String()

	if strings.Contains(got, "│") {
		t.Errorf("PassthroughWriter should not add border, got %q", got)
	}
	if !strings.Contains(got, "\r") {
		t.Errorf("expected carriage return to be preserved, got %q", got)
	}
	if !strings.Contains(got, "CACHED") {
		t.Errorf("expected 'CACHED' in output, got %q", got)
	}
}

func TestPassthroughWriterLeadingBlankLine(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPassthroughWriter(&buf)
	pw.Write([]byte("hello"))
	pw.Close()
	got := buf.String()

	if !strings.HasPrefix(got, "\n") {
		t.Errorf("expected leading blank line separator, got %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("expected 'hello' in output, got %q", got)
	}
}
