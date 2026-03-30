package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNameDefaultsToClaude(t *testing.T) {
	if got := ParseName(""); got != Claude {
		t.Fatalf("ParseName(\"\") = %q, want %q", got, Claude)
	}
}

func TestMergeWorkspaceClaudeMD_AppendsGeneratedInstructions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("Project instructions"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	merged := mergeWorkspaceClaudeMD(dir, "CBox instructions")
	if !strings.Contains(merged, "Project instructions") {
		t.Fatalf("merged instructions missing project content: %q", merged)
	}
	if !strings.Contains(merged, "CBox instructions") {
		t.Fatalf("merged instructions missing generated content: %q", merged)
	}
	if !strings.Contains(merged, cboxInstructionsStart) || !strings.Contains(merged, cboxInstructionsEnd) {
		t.Fatalf("merged instructions missing cbox markers: %q", merged)
	}
}

func TestBuildCursorMCPConfig_IncludesCboxHost(t *testing.T) {
	dir := filepath.Join("testdata-does-not-exist")
	cfg := buildCursorMCPConfig(dir, 4321)
	if !strings.Contains(cfg, `"cbox-host"`) {
		t.Fatalf("expected cbox-host server in config: %s", cfg)
	}
	if !strings.Contains(cfg, `4321`) {
		t.Fatalf("expected MCP port in config: %s", cfg)
	}
}

func TestCursorInjectInstructions_WritesClaudeMD(t *testing.T) {
	dir := t.TempDir()
	spec := RuntimeSpec{
		WorktreePath: dir,
		HostCommands: []string{"git"},
		Commands: map[string]string{
			"test": "go test ./...",
		},
	}

	if err := (CursorBackend{}).InjectInstructions("", spec); err != nil {
		t.Fatalf("InjectInstructions: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "CBox Container Environment") {
		t.Fatalf("expected generated cbox instructions in CLAUDE.md: %q", text)
	}
	if !strings.Contains(text, "cbox_test") {
		t.Fatalf("expected configured command in CLAUDE.md: %q", text)
	}
}

func TestCursorInjectInstructions_PreservesExistingClaudeMD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("Project instructions"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	spec := RuntimeSpec{
		WorktreePath: dir,
		HostCommands: []string{"git"},
	}
	if err := (CursorBackend{}).InjectInstructions("", spec); err != nil {
		t.Fatalf("InjectInstructions: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "Project instructions") {
		t.Fatalf("expected existing CLAUDE.md content to be preserved: %q", text)
	}
	if !strings.Contains(text, cboxInstructionsStart) {
		t.Fatalf("expected cbox marker in CLAUDE.md: %q", text)
	}
}

func TestCursorInjectInstructions_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	spec := RuntimeSpec{
		WorktreePath: dir,
		HostCommands: []string{"git"},
	}

	backend := CursorBackend{}
	if err := backend.InjectInstructions("", spec); err != nil {
		t.Fatalf("first InjectInstructions: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile first: %v", err)
	}

	if err := backend.InjectInstructions("", spec); err != nil {
		t.Fatalf("second InjectInstructions: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("ReadFile second: %v", err)
	}

	if string(first) != string(second) {
		t.Fatalf("expected repeated injection to be stable\nfirst:\n%s\n\nsecond:\n%s", string(first), string(second))
	}
	if strings.Count(string(second), cboxInstructionsStart) != 1 {
		t.Fatalf("expected exactly one managed cbox section: %q", string(second))
	}
}
