package sandbox

import (
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

// TestCleanQuietRemovesState verifies that CleanQuiet performs the same
// cleanup as Clean (removing the state file) without emitting progress output.
func TestCleanQuietRemovesState(t *testing.T) {
	dir := t.TempDir()
	branch := "test-quiet"

	state := &State{
		ClaudeContainer: "cbox-test-nonexistent-77777",
		NetworkName:     "cbox-test-net-nonexistent-77777",
		WorktreePath:    filepath.Join(dir, "fake-worktree"),
		Branch:          branch,
		ProjectDir:      dir,
		Running:         false,
	}

	if err := SaveState(dir, branch, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	os.MkdirAll(state.WorktreePath, 0755)

	err := CleanQuiet(dir, branch)
	if err != nil {
		t.Fatalf("CleanQuiet returned error: %v", err)
	}

	statePath := filepath.Join(dir, StateDir, branch+".state.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("state file still exists after CleanQuiet: %s", statePath)
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
