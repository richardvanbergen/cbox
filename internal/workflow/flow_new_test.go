package workflow

import (
	"os/exec"
	"strings"
	"testing"
)

func TestParseTitleDescription(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTitle string
		wantDesc  string
	}{
		{
			name:      "standard format",
			input:     "TITLE: Add retry logic\nDESCRIPTION: The API client needs retry support.",
			wantTitle: "Add retry logic",
			wantDesc:  "The API client needs retry support.",
		},
		{
			name:      "multiline description",
			input:     "TITLE: Fix auth bug\nDESCRIPTION: The login flow breaks when\nthe session expires during a request.",
			wantTitle: "Fix auth bug",
			wantDesc:  "The login flow breaks when\nthe session expires during a request.",
		},
		{
			name:      "extra whitespace",
			input:     "  TITLE:  My Title  \n  DESCRIPTION:  Some description  ",
			wantTitle: "My Title",
			wantDesc:  "Some description",
		},
		{
			name:      "title only",
			input:     "TITLE: Just a title",
			wantTitle: "Just a title",
			wantDesc:  "",
		},
		{
			name:      "no title marker",
			input:     "Some random text",
			wantTitle: "",
			wantDesc:  "",
		},
		{
			name:      "empty input",
			input:     "",
			wantTitle: "",
			wantDesc:  "",
		},
		{
			name:      "leading text before markers",
			input:     "Here is the result:\nTITLE: My Title\nDESCRIPTION: My description",
			wantTitle: "My Title",
			wantDesc:  "My description",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, desc := parseTitleDescription(tt.input)
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
			if desc != tt.wantDesc {
				t.Errorf("desc = %q, want %q", desc, tt.wantDesc)
			}
		})
	}
}

func TestResolveBranchConflict_NoConflict(t *testing.T) {
	dir := initTempGitRepo(t)

	branch, slug := resolveBranchConflict(dir, "new-feature", "new-feature")
	if branch != "new-feature" {
		t.Errorf("branch = %q, want %q", branch, "new-feature")
	}
	if slug != "new-feature" {
		t.Errorf("slug = %q, want %q", slug, "new-feature")
	}
}

func TestResolveBranchConflict_WithConflict(t *testing.T) {
	dir := initTempGitRepo(t)

	cmd := exec.Command("git", "branch", "existing-branch")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("creating test branch: %v", err)
	}

	branch, slug := resolveBranchConflict(dir, "existing-branch", "existing-branch")
	if branch != "existing-branch-2" {
		t.Errorf("branch = %q, want %q", branch, "existing-branch-2")
	}
	if slug != "existing-branch-2" {
		t.Errorf("slug = %q, want %q", slug, "existing-branch-2")
	}
}

func TestResolveBranchConflict_MultipleConflicts(t *testing.T) {
	dir := initTempGitRepo(t)

	for _, name := range []string{"my-branch", "my-branch-2", "my-branch-3"} {
		cmd := exec.Command("git", "branch", name)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("creating test branch %s: %v", name, err)
		}
	}

	branch, slug := resolveBranchConflict(dir, "my-branch", "my-branch")
	if branch != "my-branch-4" {
		t.Errorf("branch = %q, want %q", branch, "my-branch-4")
	}
	if slug != "my-branch-4" {
		t.Errorf("slug = %q, want %q", slug, "my-branch-4")
	}
}

func TestBranchExists(t *testing.T) {
	dir := initTempGitRepo(t)

	if branchExists(dir, "nonexistent") {
		t.Error("branchExists should return false for nonexistent branch")
	}

	cmd := exec.Command("git", "branch", "test-exists")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("creating test branch: %v", err)
	}

	if !branchExists(dir, "test-exists") {
		t.Error("branchExists should return true for existing branch")
	}
}

func TestPolishTask_Fallback(t *testing.T) {
	// Test the fallback path (LLM unavailable) directly
	title := fallbackSummarize("Add retry logic to the API client for transient errors")
	if title == "" {
		t.Error("fallbackSummarize should produce a non-empty title")
	}
	if len(title) > 70 {
		t.Errorf("title length %d exceeds 70 chars", len(title))
	}
}

func TestTaskEditTemplate_AllComments(t *testing.T) {
	for _, line := range strings.Split(taskEditTemplate, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && trimmed[0] != '#' {
			t.Errorf("non-comment instruction line: %q", line)
		}
	}
}

// initTempGitRepo creates a temporary directory with an initialized git repo
// and an initial commit.
func initTempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}

	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", args[0], err, out)
		}
	}

	return dir
}
