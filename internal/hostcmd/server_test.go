package hostcmd

import (
	"testing"
)

func TestTranslateCwd(t *testing.T) {
	s := NewServer([]string{"git"}, "/host/path/to/worktree")

	tests := []struct {
		name       string
		input      string
		want       string
		wantErr    bool
	}{
		{"workspace root", "/workspace", "/host/path/to/worktree", false},
		{"workspace subdir", "/workspace/src", "/host/path/to/worktree/src", false},
		{"workspace nested", "/workspace/src/pkg/foo", "/host/path/to/worktree/src/pkg/foo", false},
		{"non-workspace path", "/home/claude", "/host/path/to/worktree", false},
		{"workspace with trailing slash", "/workspace/", "/host/path/to/worktree", false},
		{"path traversal", "/workspace/../etc/passwd", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.translateCwd(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("translateCwd(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWhitelist(t *testing.T) {
	s := NewServer([]string{"git", "gh"}, "/tmp/worktree")

	if !s.commands["git"] {
		t.Error("git should be allowed")
	}
	if !s.commands["gh"] {
		t.Error("gh should be allowed")
	}
	if s.commands["rm"] {
		t.Error("rm should not be allowed")
	}
	if s.commands[""] {
		t.Error("empty string should not be allowed")
	}
}
