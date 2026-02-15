package workflow

import (
	"os"
	"path/filepath"
	"strings"
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

func TestFindMergedFlows(t *testing.T) {
	wf := &config.WorkflowConfig{
		PR: &config.WorkflowPRConfig{
			View: `echo "{\"number\":$PRNumber,\"state\":\"MERGED\",\"title\":\"t\",\"url\":\"u\",\"mergedAt\":\"2025-01-01\"}"`,
		},
	}

	states := []*FlowState{
		{Branch: "merged-branch", Title: "Merged PR", PRNumber: "1", Phase: "started"},
		{Branch: "no-pr-branch", Title: "No PR", PRNumber: "", Phase: "started"},
	}

	merged := findMergedFlows(wf, states)

	if len(merged) != 1 {
		t.Fatalf("expected 1 merged flow, got %d", len(merged))
	}
	if merged[0].Branch != "merged-branch" {
		t.Errorf("expected merged branch 'merged-branch', got %q", merged[0].Branch)
	}
}

func TestFindMergedFlows_NonemergedPR(t *testing.T) {
	wf := &config.WorkflowConfig{
		PR: &config.WorkflowPRConfig{
			View: `echo '{"number":1,"state":"OPEN","title":"t","url":"u","mergedAt":""}'`,
		},
	}

	states := []*FlowState{
		{Branch: "open-branch", Title: "Open PR", PRNumber: "1", Phase: "started"},
	}

	merged := findMergedFlows(wf, states)

	if len(merged) != 0 {
		t.Fatalf("expected 0 merged flows, got %d", len(merged))
	}
}

func TestFindMergedFlows_Empty(t *testing.T) {
	wf := &config.WorkflowConfig{
		PR: &config.WorkflowPRConfig{
			View: `echo '{}'`,
		},
	}

	merged := findMergedFlows(wf, nil)
	if len(merged) != 0 {
		t.Fatalf("expected 0 merged flows for nil states, got %d", len(merged))
	}
}

func TestFindMergedFlows_MixedStates(t *testing.T) {
	// Use a view command that returns MERGED for PRNumber=1 and OPEN for PRNumber=2
	wf := &config.WorkflowConfig{
		PR: &config.WorkflowPRConfig{
			View: `if [ "$PRNumber" = "1" ]; then echo '{"number":1,"state":"MERGED","title":"t","url":"u","mergedAt":"2025-01-01"}'; else echo '{"number":2,"state":"OPEN","title":"t","url":"u","mergedAt":""}'; fi`,
		},
	}

	states := []*FlowState{
		{Branch: "merged-one", Title: "Merged", PRNumber: "1", Phase: "started"},
		{Branch: "open-one", Title: "Open", PRNumber: "2", Phase: "started"},
		{Branch: "no-pr", Title: "No PR", PRNumber: "", Phase: "started"},
	}

	merged := findMergedFlows(wf, states)

	if len(merged) != 1 {
		t.Fatalf("expected 1 merged flow, got %d", len(merged))
	}
	if merged[0].Branch != "merged-one" {
		t.Errorf("expected merged branch 'merged-one', got %q", merged[0].Branch)
	}
}

// setupFlowCleanDir creates a temp directory with a cbox.toml config and
// flow state files for testing flowClean.
func setupFlowCleanDir(t *testing.T, viewCmd string, states []*FlowState) string {
	t.Helper()
	dir := t.TempDir()

	// Write cbox.toml — use TOML multi-line literal string (''') so that
	// both single and double quotes inside the view command are preserved.
	toml := "[workflow]\n[workflow.pr]\nview = '''" + viewCmd + "'''\n"
	if err := os.WriteFile(filepath.Join(dir, config.ConfigFile), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	// Write flow state files
	for _, s := range states {
		if err := SaveFlowState(dir, s); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestFlowClean_NoActiveFlows(t *testing.T) {
	dir := t.TempDir()
	toml := `[workflow]
[workflow.pr]
view = "echo test"
`
	if err := os.WriteFile(filepath.Join(dir, config.ConfigFile), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	err := flowClean(dir, strings.NewReader("y\n"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFlowClean_NoWorkflowConfig(t *testing.T) {
	dir := t.TempDir()
	toml := "[commands]\nbuild = \"echo build\"\n"
	if err := os.WriteFile(filepath.Join(dir, config.ConfigFile), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	err := flowClean(dir, strings.NewReader("y\n"))
	if err == nil {
		t.Error("expected error when no workflow config")
	}
}

func TestFlowClean_UserDeclinesConfirmation(t *testing.T) {
	dir := setupFlowCleanDir(t,
		`echo '{"number":1,"state":"MERGED","title":"t","url":"u","mergedAt":"2025-01-01"}'`,
		[]*FlowState{
			{Branch: "test-branch", Title: "Test", PRNumber: "1", Phase: "started"},
		},
	)

	// User answers "n" — flow state should NOT be removed
	err := flowClean(dir, strings.NewReader("n\n"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Flow state should still exist
	_, err = LoadFlowState(dir, "test-branch")
	if err != nil {
		t.Errorf("flow state should still exist after declining: %v", err)
	}
}

func TestFlowClean_UserConfirmsRemoval(t *testing.T) {
	dir := setupFlowCleanDir(t,
		`echo '{"number":1,"state":"MERGED","title":"t","url":"u","mergedAt":"2025-01-01"}'`,
		[]*FlowState{
			{Branch: "test-branch", Title: "Test", PRNumber: "1", Phase: "started"},
		},
	)

	// User answers "y" — flow state should be removed
	// sandbox.Clean will fail (no sandbox state file), but FlowClean warns and continues
	err := flowClean(dir, strings.NewReader("y\n"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Flow state should be removed
	_, err = LoadFlowState(dir, "test-branch")
	if err == nil {
		t.Error("flow state should have been removed after confirming")
	}
}

func TestFlowClean_UserConfirmsYes(t *testing.T) {
	dir := setupFlowCleanDir(t,
		`echo '{"number":1,"state":"MERGED","title":"t","url":"u","mergedAt":"2025-01-01"}'`,
		[]*FlowState{
			{Branch: "test-branch", Title: "Test", PRNumber: "1", Phase: "started"},
		},
	)

	// "yes" should also work
	err := flowClean(dir, strings.NewReader("yes\n"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	_, err = LoadFlowState(dir, "test-branch")
	if err == nil {
		t.Error("flow state should have been removed after confirming 'yes'")
	}
}

func TestFlowClean_NoMergedFlows(t *testing.T) {
	dir := setupFlowCleanDir(t,
		`echo '{"number":1,"state":"OPEN","title":"t","url":"u","mergedAt":""}'`,
		[]*FlowState{
			{Branch: "open-branch", Title: "Open", PRNumber: "1", Phase: "started"},
		},
	)

	err := flowClean(dir, strings.NewReader("y\n"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Flow state should still exist since nothing was merged
	_, err = LoadFlowState(dir, "open-branch")
	if err != nil {
		t.Errorf("flow state should still exist: %v", err)
	}
}

func TestFlowClean_RemovesReportsDir(t *testing.T) {
	dir := setupFlowCleanDir(t,
		`echo '{"number":1,"state":"MERGED","title":"t","url":"u","mergedAt":"2025-01-01"}'`,
		[]*FlowState{
			{Branch: "test-branch", Title: "Test", PRNumber: "1", Phase: "started"},
		},
	)

	// Create a reports directory
	repDir := reportDir(dir, "test-branch")
	if err := os.MkdirAll(repDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repDir, "001-done.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	err := flowClean(dir, strings.NewReader("y\n"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Reports directory should be removed
	if _, err := os.Stat(repDir); !os.IsNotExist(err) {
		t.Error("reports directory should have been removed")
	}
}

func TestFlowClean_EmptyInput(t *testing.T) {
	dir := setupFlowCleanDir(t,
		`echo '{"number":1,"state":"MERGED","title":"t","url":"u","mergedAt":"2025-01-01"}'`,
		[]*FlowState{
			{Branch: "test-branch", Title: "Test", PRNumber: "1", Phase: "started"},
		},
	)

	// Empty input (EOF) should not clean up
	err := flowClean(dir, strings.NewReader(""))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	_, err = LoadFlowState(dir, "test-branch")
	if err != nil {
		t.Errorf("flow state should still exist after EOF: %v", err)
	}
}

func TestFlowMerge_RejectsWithoutPR(t *testing.T) {
	dir := t.TempDir()

	// Write a minimal cbox.toml
	toml := "[workflow]\n[workflow.pr]\nmerge = \"echo merged\"\n"
	if err := os.WriteFile(filepath.Join(dir, config.ConfigFile), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	// Save flow state without a PR URL
	state := &FlowState{
		Branch: "no-pr-branch",
		Title:  "Test without PR",
		Phase:  "started",
	}
	if err := SaveFlowState(dir, state); err != nil {
		t.Fatal(err)
	}

	err := FlowMerge(dir, "no-pr-branch")
	if err == nil {
		t.Fatal("expected error when merging without a PR, got nil")
	}
	if !strings.Contains(err.Error(), "no PR has been created") {
		t.Errorf("error should mention missing PR, got: %v", err)
	}
	if !strings.Contains(err.Error(), "cbox flow pr") {
		t.Errorf("error should suggest running cbox flow pr, got: %v", err)
	}

	// Flow state should still exist — nothing should be cleaned up
	_, err = LoadFlowState(dir, "no-pr-branch")
	if err != nil {
		t.Errorf("flow state should still exist after rejected merge: %v", err)
	}
}

func TestFlowMerge_ProceedsWithPR(t *testing.T) {
	dir := t.TempDir()

	// Write a cbox.toml with a merge command that succeeds
	toml := "[workflow]\n[workflow.pr]\nmerge = \"echo merged\"\n"
	if err := os.WriteFile(filepath.Join(dir, config.ConfigFile), []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	// Save flow state WITH a PR URL
	state := &FlowState{
		Branch:   "has-pr-branch",
		Title:    "Test with PR",
		Phase:    "started",
		PRURL:    "https://github.com/test/repo/pull/1",
		PRNumber: "1",
	}
	if err := SaveFlowState(dir, state); err != nil {
		t.Fatal(err)
	}

	// FlowMerge should NOT return the "no PR" error
	err := FlowMerge(dir, "has-pr-branch")
	// It may fail for other reasons (sandbox cleanup, etc.) but should NOT
	// fail with the "no PR has been created" error.
	if err != nil && strings.Contains(err.Error(), "no PR has been created") {
		t.Errorf("should not reject merge when PR exists, got: %v", err)
	}
}
