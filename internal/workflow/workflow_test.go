package workflow

import (
	"testing"

	"github.com/richvanbergen/cbox/internal/config"
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

func TestFetchPRStatus_NoPRNumber(t *testing.T) {
	wf := &config.WorkflowConfig{
		PR: &config.WorkflowPRConfig{
			View: `echo '{"number":1,"state":"OPEN","title":"t","url":"u","mergedAt":""}'`,
		},
	}
	state := &FlowState{PRNumber: ""}

	status, err := fetchPRStatus(wf, state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if status != nil {
		t.Error("expected nil when PRNumber is empty")
	}
}

func TestFetchPRStatus_NilWorkflow(t *testing.T) {
	state := &FlowState{PRNumber: "42"}

	_, err := fetchPRStatus(nil, state)
	if err == nil {
		t.Error("expected error when workflow config is nil")
	}
}

func TestFetchPRStatus_NoViewCommand(t *testing.T) {
	wf := &config.WorkflowConfig{
		PR: &config.WorkflowPRConfig{},
	}
	state := &FlowState{PRNumber: "42"}

	_, err := fetchPRStatus(wf, state)
	if err == nil {
		t.Error("expected error when view command is empty")
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
