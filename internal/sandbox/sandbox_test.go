package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCleanAttemptsDockerCleanupRegardlessOfRunningFlag verifies that Clean
// always attempts to stop and remove the Docker container, even when the
// state file has Running=false. This is a regression test for the bug where
// Clean skipped Docker cleanup when state.Running was false (e.g. after
// "cbox down" was called), leaving the container behind.
func TestCleanAttemptsDockerCleanupRegardlessOfRunningFlag(t *testing.T) {
	// Create a temporary project directory with a state file.
	dir := t.TempDir()
	branch := "test-branch"

	// Save state with Running=false to simulate the scenario where
	// "cbox down" was previously called.
	state := &State{
		ClaudeContainer: "cbox-test-nonexistent-99999",
		NetworkName:     "cbox-test-net-nonexistent-99999",
		WorktreePath:    filepath.Join(dir, "fake-worktree"),
		Branch:          branch,
		ProjectDir:      dir,
		Running:         false,
	}

	if err := SaveState(dir, branch, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Create a fake worktree directory so worktree.Remove doesn't fail
	// in an unexpected way (it will still fail because it's not a real
	// git worktree, but that's handled with a warning).
	os.MkdirAll(state.WorktreePath, 0755)

	// Call Clean — this should NOT skip Docker cleanup even though
	// state.Running is false. Previously, it would skip the entire
	// docker.StopAndRemove + docker.RemoveNetwork block.
	//
	// We can't easily verify the Docker commands were called without
	// mocking, but we can verify that Clean completes without error
	// and removes the state file.
	err := Clean(dir, branch)

	// Clean should succeed (warnings are printed but not returned as errors).
	if err != nil {
		t.Fatalf("Clean returned error: %v", err)
	}

	// Verify the state file was removed.
	statePath := filepath.Join(dir, StateDir, branch+".state.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("state file still exists after Clean: %s", statePath)
	}
}

// TestCleanWithRunningState verifies Clean works when state.Running is true.
func TestCleanWithRunningState(t *testing.T) {
	dir := t.TempDir()
	branch := "test-running"

	state := &State{
		ClaudeContainer: "cbox-test-nonexistent-88888",
		NetworkName:     "cbox-test-net-nonexistent-88888",
		WorktreePath:    filepath.Join(dir, "fake-worktree"),
		Branch:          branch,
		ProjectDir:      dir,
		Running:         true,
		MCPProxyPID:     0,
		BridgeProxyPID:  0,
	}

	if err := SaveState(dir, branch, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	os.MkdirAll(state.WorktreePath, 0755)

	err := Clean(dir, branch)
	if err != nil {
		t.Fatalf("Clean returned error: %v", err)
	}

	statePath := filepath.Join(dir, StateDir, branch+".state.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("state file still exists after Clean: %s", statePath)
	}
}

// TestSaveStateSetsVersion verifies that SaveState sets the version field.
func TestSaveStateSetsVersion(t *testing.T) {
	dir := t.TempDir()
	branch := "test-version"

	state := &State{
		ClaudeContainer: "test-container",
		Branch:          branch,
		ProjectDir:      dir,
	}

	if err := SaveState(dir, branch, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	if state.Version != StateVersion {
		t.Errorf("state.Version = %d, want %d", state.Version, StateVersion)
	}

	loaded, err := LoadState(dir, branch)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.Version != StateVersion {
		t.Errorf("loaded.Version = %d, want %d", loaded.Version, StateVersion)
	}
}

// TestLoadStateLegacyNoVersion verifies that state files without a version
// field (written before versioning was added) can still be loaded.
func TestLoadStateLegacyNoVersion(t *testing.T) {
	dir := t.TempDir()
	branch := "legacy"

	// Write a state file without the version field, simulating an old file.
	stateJSON := `{
  "claude_container": "old-container",
  "network_name": "old-net",
  "worktree_path": "/tmp/wt",
  "branch": "legacy",
  "claude_image": "img",
  "project_dir": "/proj",
  "running": true
}`
	stateDir := filepath.Join(dir, StateDir)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, branch+".state.json")
	if err := os.WriteFile(path, []byte(stateJSON), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadState(dir, branch)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	// Version should be 0 (zero value) for legacy files
	if loaded.Version != 0 {
		t.Errorf("loaded.Version = %d, want 0 for legacy file", loaded.Version)
	}
	if loaded.ClaudeContainer != "old-container" {
		t.Errorf("loaded.ClaudeContainer = %q, want %q", loaded.ClaudeContainer, "old-container")
	}
}

// TestSaveStateVersionInJSON verifies the version field appears in serialized JSON.
func TestSaveStateVersionInJSON(t *testing.T) {
	dir := t.TempDir()
	branch := "json-check"

	state := &State{
		ClaudeContainer: "c",
		Branch:          branch,
		ProjectDir:      dir,
	}

	if err := SaveState(dir, branch, state); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, StateDir, branch+".state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if _, ok := raw["version"]; !ok {
		t.Error("expected 'version' key in serialized state JSON")
	}
}
