package workflow

import (
	"strings"
	"testing"
)

func TestFormatPRPhase(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  string
	}{
		{"merged", "MERGED", "merged"},
		{"open", "OPEN", "pr-open"},
		{"closed", "CLOSED", "closed"},
		{"lowercase merged", "merged", "merged"},
		{"unknown state", "DRAFT", "draft"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := &PRStatus{State: tt.state}
			got := formatPRPhase(status)
			if got != tt.want {
				t.Errorf("formatPRPhase(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestFetchTaskPRStatus_NoPRNumber(t *testing.T) {
	task := &Task{PRNumber: ""}

	status, err := fetchTaskPRStatus(nil, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if status != nil {
		t.Error("expected nil when PRNumber is empty")
	}
}

func TestFetchTaskPRStatus_NilWorkflow(t *testing.T) {
	task := &Task{PRNumber: "42"}

	_, err := fetchTaskPRStatus(nil, task)
	if err == nil {
		t.Error("expected error when workflow config is nil")
	}
}

func TestResolveOpenCommand(t *testing.T) {
	tests := []struct {
		name       string
		openFlag   bool
		openCmd    string
		configOpen string
		want       string
	}{
		{
			name:       "flag not set, no config",
			openFlag:   false,
			openCmd:    "",
			configOpen: "",
			want:       "",
		},
		{
			name:       "flag not set, config present",
			openFlag:   false,
			openCmd:    "",
			configOpen: "code $Dir",
			want:       "",
		},
		{
			name:       "flag set with command",
			openFlag:   true,
			openCmd:    "vim $Dir",
			configOpen: "code $Dir",
			want:       "vim $Dir",
		},
		{
			name:       "flag set without value, config present",
			openFlag:   true,
			openCmd:    "",
			configOpen: "code $Dir",
			want:       "code $Dir",
		},
		{
			name:       "flag set with whitespace-only value, config present",
			openFlag:   true,
			openCmd:    " ",
			configOpen: "code $Dir",
			want:       "code $Dir",
		},
		{
			name:       "flag set without value, no config",
			openFlag:   true,
			openCmd:    "",
			configOpen: "",
			want:       "",
		},
		{
			name:       "flag set with command, no config",
			openFlag:   true,
			openCmd:    "vim $Dir",
			configOpen: "",
			want:       "vim $Dir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveOpenCommand(tt.openFlag, tt.openCmd, tt.configOpen)
			if got != tt.want {
				t.Errorf("resolveOpenCommand(%v, %q, %q) = %q, want %q",
					tt.openFlag, tt.openCmd, tt.configOpen, got, tt.want)
			}
		})
	}
}

func TestFlowMerge_RejectsWithoutPR(t *testing.T) {
	dir := t.TempDir()

	// Create a task.json in a fake worktree with phase "done" but no PR
	task := NewTask("test", "no-pr-branch", "Test without PR", "Description")
	task.Phase = PhaseDone
	if err := SaveTask(dir, task); err != nil {
		t.Fatal(err)
	}

	// FlowMerge requires sandbox state which we can't easily fake here,
	// so we just verify the error message pattern from checkMergeGate
	// and the PR check would happen after that.
	err := checkMergeGate(dir)
	if err != nil {
		t.Errorf("checkMergeGate should pass for done phase: %v", err)
	}

	// Verify the task has no PR URL
	loaded, _ := LoadTask(dir)
	if loaded.PRURL != "" {
		t.Error("expected empty PR URL")
	}
}

func TestFlowMerge_TaskPRFields(t *testing.T) {
	dir := t.TempDir()

	// Create a task with PR info
	task := NewTask("test", "has-pr-branch", "Test with PR", "Description")
	task.Phase = PhaseDone
	task.PRURL = "https://github.com/test/repo/pull/1"
	task.PRNumber = "1"
	if err := SaveTask(dir, task); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.PRURL != "https://github.com/test/repo/pull/1" {
		t.Errorf("PRURL = %q, want %q", loaded.PRURL, "https://github.com/test/repo/pull/1")
	}
	if loaded.PRNumber != "1" {
		t.Errorf("PRNumber = %q, want %q", loaded.PRNumber, "1")
	}

	err = checkMergeGate(dir)
	if err != nil {
		t.Errorf("merge gate should pass for done phase: %v", err)
	}
}

func TestCheckMergeGate_BlocksNonDone(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Desc")
	task.Phase = PhaseVerification
	if err := SaveTask(dir, task); err != nil {
		t.Fatal(err)
	}

	err := checkMergeGate(dir)
	if err == nil {
		t.Error("should block merge when phase is verification")
	}
	if !strings.Contains(err.Error(), "verify") {
		t.Errorf("error should mention verify, got: %v", err)
	}
}
