package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderTextBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, TextBlock{Text: "hello world"})
	got := buf.String()
	if got != "hello world\n" {
		t.Errorf("TextBlock: got %q, want %q", got, "hello world\n")
	}
}

func TestRenderToolUseBlock(t *testing.T) {
	var buf bytes.Buffer
	input := json.RawMessage(`{"path":"/tmp"}`)
	RenderBlock(&buf, ToolUseBlock{ID: "abc123", Name: "Read", Input: input})
	got := buf.String()
	if !strings.HasPrefix(got, "[tool_use] Read (id: abc123)\n") {
		t.Errorf("ToolUseBlock header: got %q", got)
	}
	if !strings.Contains(got, `"path"`) {
		t.Errorf("ToolUseBlock input: expected JSON input in output, got %q", got)
	}
}

func TestRenderToolUseBlockNoInput(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, ToolUseBlock{ID: "x", Name: "Bash", Input: nil})
	got := buf.String()
	want := "[tool_use] Bash (id: x)\n"
	if got != want {
		t.Errorf("ToolUseBlock no input: got %q, want %q", got, want)
	}
}

func TestRenderProgressBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, ProgressBlock{Message: "creating sandbox"})
	got := buf.String()
	if got != "... creating sandbox\n" {
		t.Errorf("ProgressBlock: got %q", got)
	}
}

func TestRenderSuccessBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, SuccessBlock{Message: "done"})
	got := buf.String()
	if got != "  ✓ done\n" {
		t.Errorf("SuccessBlock: got %q", got)
	}
}

func TestRenderWarningBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, WarningBlock{Message: "disk low"})
	got := buf.String()
	if got != "  ! disk low\n" {
		t.Errorf("WarningBlock: got %q", got)
	}
}

func TestRenderErrorBlock(t *testing.T) {
	var buf bytes.Buffer
	RenderBlock(&buf, ErrorBlock{Message: "failed"})
	got := buf.String()
	if got != "  ✗ failed\n" {
		t.Errorf("ErrorBlock: got %q", got)
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
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "... step 1" {
		t.Errorf("line 0: got %q", lines[0])
	}
	if lines[1] != "step 2" {
		t.Errorf("line 1: got %q", lines[1])
	}
	if lines[2] != "  ✓ step 3" {
		t.Errorf("line 2: got %q", lines[2])
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

	// Render and check output
	var buf bytes.Buffer
	Render(&buf, blocks)
	out := buf.String()
	if !strings.Contains(out, "Hello from Claude") {
		t.Errorf("missing text block content in output")
	}
	if !strings.Contains(out, "[tool_use] Bash (id: tu_1)") {
		t.Errorf("missing tool_use block in output")
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
