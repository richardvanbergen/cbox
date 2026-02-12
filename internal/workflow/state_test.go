package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveFlowStateSetsVersion(t *testing.T) {
	dir := t.TempDir()

	state := &FlowState{
		Branch:    "test-branch",
		Title:     "Test",
		Phase:     "started",
		CreatedAt: time.Now(),
	}

	if err := SaveFlowState(dir, state); err != nil {
		t.Fatalf("SaveFlowState: %v", err)
	}

	if state.Version != FlowStateVersion {
		t.Errorf("state.Version = %d, want %d", state.Version, FlowStateVersion)
	}

	loaded, err := LoadFlowState(dir, "test-branch")
	if err != nil {
		t.Fatalf("LoadFlowState: %v", err)
	}

	if loaded.Version != FlowStateVersion {
		t.Errorf("loaded.Version = %d, want %d", loaded.Version, FlowStateVersion)
	}
}

func TestLoadFlowStateLegacyNoVersion(t *testing.T) {
	dir := t.TempDir()

	// Write a flow state file without the version field.
	stateJSON := `{
  "branch": "old-branch",
  "title": "Old Task",
  "phase": "started",
  "auto_mode": false,
  "chatted": false,
  "created_at": "2025-01-01T00:00:00Z",
  "updated_at": "2025-01-01T00:00:00Z"
}`
	stateDir := filepath.Join(dir, ".cbox")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "flow-old-branch.json")
	if err := os.WriteFile(path, []byte(stateJSON), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFlowState(dir, "old-branch")
	if err != nil {
		t.Fatalf("LoadFlowState: %v", err)
	}

	if loaded.Version != 0 {
		t.Errorf("loaded.Version = %d, want 0 for legacy file", loaded.Version)
	}
	if loaded.Title != "Old Task" {
		t.Errorf("loaded.Title = %q, want %q", loaded.Title, "Old Task")
	}
}

func TestSaveFlowStateVersionInJSON(t *testing.T) {
	dir := t.TempDir()

	state := &FlowState{
		Branch:    "json-check",
		Title:     "Check",
		Phase:     "started",
		CreatedAt: time.Now(),
	}

	if err := SaveFlowState(dir, state); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, ".cbox", "flow-json-check.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if _, ok := raw["version"]; !ok {
		t.Error("expected 'version' key in serialized flow state JSON")
	}
}

func TestSaveFlowStateUpdatesTimestamp(t *testing.T) {
	dir := t.TempDir()

	before := time.Now().Add(-time.Second)
	state := &FlowState{
		Branch:    "ts-check",
		Title:     "Timestamp",
		Phase:     "started",
		CreatedAt: before,
	}

	if err := SaveFlowState(dir, state); err != nil {
		t.Fatal(err)
	}

	if !state.UpdatedAt.After(before) {
		t.Errorf("UpdatedAt should be after creation time")
	}
}
