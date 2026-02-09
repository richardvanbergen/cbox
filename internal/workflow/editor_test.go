package workflow

import "testing"

func TestStripComments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "removes comment lines",
			input: "hello\n# this is a comment\nworld",
			want:  "hello\nworld",
		},
		{
			name:  "removes indented comment lines",
			input: "hello\n  # indented comment\nworld",
			want:  "hello\nworld",
		},
		{
			name:  "preserves non-comment lines",
			input: "line one\nline two\nline three",
			want:  "line one\nline two\nline three",
		},
		{
			name:  "trims surrounding whitespace",
			input: "\n\nhello\n\n",
			want:  "hello",
		},
		{
			name:  "strips editor template",
			input: "my description\n# Enter your flow description above.\n# Lines starting with '#' will be ignored.\n# An empty description aborts the flow.\n",
			want:  "my description",
		},
		{
			name:  "empty after stripping comments",
			input: "# just a comment\n# another comment",
			want:  "",
		},
		{
			name:  "whitespace only after stripping",
			input: "  \n\n  # comment\n  \n",
			want:  "",
		},
		{
			name:  "multiline description preserved",
			input: "line one\nline two\n# comment\nline three",
			want:  "line one\nline two\nline three",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripComments(tt.input)
			if got != tt.want {
				t.Errorf("stripComments() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveEditor(t *testing.T) {
	// Clear all editor env vars for a clean test.
	t.Setenv("CBOX_EDITOR", "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	t.Run("returns empty when nothing set", func(t *testing.T) {
		got := resolveEditor("")
		if got != "" {
			t.Errorf("resolveEditor() = %q, want empty", got)
		}
	})

	t.Run("CBOX_EDITOR takes priority", func(t *testing.T) {
		t.Setenv("CBOX_EDITOR", "cbox-ed")
		t.Setenv("VISUAL", "vis")
		t.Setenv("EDITOR", "ed")
		got := resolveEditor("config-ed")
		if got != "cbox-ed" {
			t.Errorf("resolveEditor() = %q, want %q", got, "cbox-ed")
		}
	})

	t.Run("config editor used when no CBOX_EDITOR", func(t *testing.T) {
		t.Setenv("CBOX_EDITOR", "")
		t.Setenv("VISUAL", "vis")
		t.Setenv("EDITOR", "ed")
		got := resolveEditor("config-ed")
		if got != "config-ed" {
			t.Errorf("resolveEditor() = %q, want %q", got, "config-ed")
		}
	})

	t.Run("VISUAL used when no config", func(t *testing.T) {
		t.Setenv("CBOX_EDITOR", "")
		t.Setenv("VISUAL", "vis")
		t.Setenv("EDITOR", "ed")
		got := resolveEditor("")
		if got != "vis" {
			t.Errorf("resolveEditor() = %q, want %q", got, "vis")
		}
	})

	t.Run("EDITOR used as last resort", func(t *testing.T) {
		t.Setenv("CBOX_EDITOR", "")
		t.Setenv("VISUAL", "")
		t.Setenv("EDITOR", "ed")
		got := resolveEditor("")
		if got != "ed" {
			t.Errorf("resolveEditor() = %q, want %q", got, "ed")
		}
	})
}
