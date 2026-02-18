package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richvanbergen/cbox/internal/config"
)

func TestValidPhase(t *testing.T) {
	valid := []Phase{PhaseNew, PhaseShaping, PhaseReady, PhaseImplementation, PhaseVerification, PhaseDone}
	for _, p := range valid {
		if !ValidPhase(p) {
			t.Errorf("ValidPhase(%q) = false, want true", p)
		}
	}

	invalid := []Phase{"", "unknown", "Started", "NEW"}
	for _, p := range invalid {
		if ValidPhase(p) {
			t.Errorf("ValidPhase(%q) = true, want false", p)
		}
	}
}

func TestPhaseIndex(t *testing.T) {
	tests := []struct {
		phase Phase
		want  int
	}{
		{PhaseNew, 0},
		{PhaseShaping, 1},
		{PhaseReady, 2},
		{PhaseImplementation, 3},
		{PhaseVerification, 4},
		{PhaseDone, 5},
		{"unknown", -1},
	}

	for _, tt := range tests {
		got := phaseIndex(tt.phase)
		if got != tt.want {
			t.Errorf("phaseIndex(%q) = %d, want %d", tt.phase, got, tt.want)
		}
	}
}

func TestValidateTransition_Forward(t *testing.T) {
	// All forward transitions should be valid
	for i := 0; i < len(phaseOrder)-1; i++ {
		for j := i + 1; j < len(phaseOrder); j++ {
			from := phaseOrder[i]
			to := phaseOrder[j]
			if err := ValidateTransition(from, to); err != nil {
				t.Errorf("ValidateTransition(%q, %q) = %v, want nil", from, to, err)
			}
		}
	}
}

func TestValidateTransition_SamePhase(t *testing.T) {
	for _, p := range phaseOrder {
		err := ValidateTransition(p, p)
		if err == nil {
			t.Errorf("ValidateTransition(%q, %q) = nil, want error", p, p)
		}
	}
}

func TestValidateTransition_VerifyFail(t *testing.T) {
	err := ValidateTransition(PhaseVerification, PhaseImplementation)
	if err != nil {
		t.Errorf("verify fail transition should be allowed: %v", err)
	}
}

func TestValidateTransition_ReenterShaping(t *testing.T) {
	// From ready or later (except done), going back to shaping should be allowed
	allowed := []Phase{PhaseReady, PhaseImplementation, PhaseVerification}
	for _, from := range allowed {
		if err := ValidateTransition(from, PhaseShaping); err != nil {
			t.Errorf("re-enter shaping from %q should be allowed: %v", from, err)
		}
	}
}

func TestValidateTransition_ReenterShaping_FromDone(t *testing.T) {
	err := ValidateTransition(PhaseDone, PhaseShaping)
	if err == nil {
		t.Error("re-enter shaping from done should NOT be allowed")
	}
}

func TestValidateTransition_InvalidBackward(t *testing.T) {
	tests := []struct {
		from, to Phase
	}{
		{PhaseShaping, PhaseNew},
		{PhaseReady, PhaseNew},
		{PhaseImplementation, PhaseNew},
		{PhaseImplementation, PhaseReady},
		{PhaseDone, PhaseVerification},
		{PhaseDone, PhaseImplementation},
		{PhaseDone, PhaseNew},
	}

	for _, tt := range tests {
		err := ValidateTransition(tt.from, tt.to)
		if err == nil {
			t.Errorf("ValidateTransition(%q, %q) = nil, want error", tt.from, tt.to)
		}
	}
}

func TestValidateTransition_InvalidPhase(t *testing.T) {
	err := ValidateTransition("invalid", PhaseNew)
	if err == nil {
		t.Error("expected error for invalid source phase")
	}

	err = ValidateTransition(PhaseNew, "invalid")
	if err == nil {
		t.Error("expected error for invalid target phase")
	}
}

func TestSaveAndLoadTask(t *testing.T) {
	dir := t.TempDir()

	task := &Task{
		Version:     1,
		Slug:        "add-retry-logic",
		Branch:      "add-retry-logic",
		Title:       "Add retry logic to API client",
		Description: "The API client currently fails on transient errors...",
		Phase:       PhaseNew,
		CreatedAt:   time.Now().Truncate(time.Second),
	}

	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Verify file exists
	path := TaskPath(dir)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("task file not created: %v", err)
	}

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}

	if loaded.Version != 1 {
		t.Errorf("Version = %d, want 1", loaded.Version)
	}
	if loaded.Slug != "add-retry-logic" {
		t.Errorf("Slug = %q, want %q", loaded.Slug, "add-retry-logic")
	}
	if loaded.Branch != "add-retry-logic" {
		t.Errorf("Branch = %q, want %q", loaded.Branch, "add-retry-logic")
	}
	if loaded.Title != "Add retry logic to API client" {
		t.Errorf("Title = %q, want %q", loaded.Title, "Add retry logic to API client")
	}
	if loaded.Description != "The API client currently fails on transient errors..." {
		t.Errorf("Description = %q, want expected value", loaded.Description)
	}
	if loaded.Phase != PhaseNew {
		t.Errorf("Phase = %q, want %q", loaded.Phase, PhaseNew)
	}
}

func TestSaveTask_WithVerifyFailures(t *testing.T) {
	dir := t.TempDir()

	task := &Task{
		Version: 1,
		Slug:    "test",
		Branch:  "test",
		Title:   "Test task",
		Phase:   PhaseImplementation,
		VerifyFailures: []VerifyFailure{
			{Reason: "Tests fail", Timestamp: time.Now().Truncate(time.Second)},
			{Reason: "Missing docs", Timestamp: time.Now().Truncate(time.Second)},
		},
		CreatedAt: time.Now().Truncate(time.Second),
	}

	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}

	if len(loaded.VerifyFailures) != 2 {
		t.Fatalf("VerifyFailures length = %d, want 2", len(loaded.VerifyFailures))
	}
	if loaded.VerifyFailures[0].Reason != "Tests fail" {
		t.Errorf("VerifyFailures[0].Reason = %q, want %q", loaded.VerifyFailures[0].Reason, "Tests fail")
	}
	if loaded.VerifyFailures[1].Reason != "Missing docs" {
		t.Errorf("VerifyFailures[1].Reason = %q, want %q", loaded.VerifyFailures[1].Reason, "Missing docs")
	}
}

func TestSaveTask_WithOptionalFields(t *testing.T) {
	dir := t.TempDir()

	task := &Task{
		Version:   1,
		Slug:      "test",
		Branch:    "test",
		Title:     "Test task",
		Phase:     PhaseShaping,
		Container: "cbox-test",
		Plan:      ".cbox/plan.md",
		MemoryRef: "42",
		CreatedAt: time.Now().Truncate(time.Second),
	}

	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}

	if loaded.Container != "cbox-test" {
		t.Errorf("Container = %q, want %q", loaded.Container, "cbox-test")
	}
	if loaded.Plan != ".cbox/plan.md" {
		t.Errorf("Plan = %q, want %q", loaded.Plan, ".cbox/plan.md")
	}
	if loaded.MemoryRef != "42" {
		t.Errorf("MemoryRef = %q, want %q", loaded.MemoryRef, "42")
	}
}

func TestSaveTask_OmitsEmptyOptionalFields(t *testing.T) {
	dir := t.TempDir()

	task := &Task{
		Version:   1,
		Slug:      "test",
		Branch:    "test",
		Title:     "Test task",
		Phase:     PhaseNew,
		CreatedAt: time.Now(),
	}

	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	data, err := os.ReadFile(TaskPath(dir))
	if err != nil {
		t.Fatalf("reading task file: %v", err)
	}
	content := string(data)

	// Optional fields with omitempty should not appear in JSON
	if strings.Contains(content, "container") {
		t.Error("empty container should be omitted from JSON")
	}
	if strings.Contains(content, "plan") {
		t.Error("empty plan should be omitted from JSON")
	}
	if strings.Contains(content, "memory_ref") {
		t.Error("empty memory_ref should be omitted from JSON")
	}
	if strings.Contains(content, "verify_failures") {
		t.Error("nil verify_failures should be omitted from JSON")
	}
}

func TestTaskExists(t *testing.T) {
	dir := t.TempDir()

	if TaskExists(dir) {
		t.Error("TaskExists should return false for empty dir")
	}

	task := NewTask("test", "test", "Test", "Description")
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	if !TaskExists(dir) {
		t.Error("TaskExists should return true after saving task")
	}
}

func TestLoadTask_NoFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadTask(dir)
	if err == nil {
		t.Error("expected error when task file doesn't exist")
	}
}

func TestLoadTask_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := TaskPath(dir)
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte("not json"), 0644)

	_, err := LoadTask(dir)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTaskPath(t *testing.T) {
	path := TaskPath("/home/user/project--branch")
	want := "/home/user/project--branch/.cbox/task.json"
	if path != want {
		t.Errorf("TaskPath = %q, want %q", path, want)
	}
}

func TestNewTask(t *testing.T) {
	task := NewTask("my-slug", "my-slug", "My Title", "My description")

	if task.Version != 1 {
		t.Errorf("Version = %d, want 1", task.Version)
	}
	if task.Slug != "my-slug" {
		t.Errorf("Slug = %q, want %q", task.Slug, "my-slug")
	}
	if task.Branch != "my-slug" {
		t.Errorf("Branch = %q, want %q", task.Branch, "my-slug")
	}
	if task.Title != "My Title" {
		t.Errorf("Title = %q, want %q", task.Title, "My Title")
	}
	if task.Description != "My description" {
		t.Errorf("Description = %q, want %q", task.Description, "My description")
	}
	if task.Phase != PhaseNew {
		t.Errorf("Phase = %q, want %q", task.Phase, PhaseNew)
	}
	if task.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestSetPhase_ValidForwardTransition(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Description")
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Transition new → shaping (no workflow config, memory sync silently skipped)
	if err := task.SetPhase(dir, PhaseShaping, nil); err != nil {
		t.Fatalf("SetPhase new→shaping failed: %v", err)
	}

	if task.Phase != PhaseShaping {
		t.Errorf("Phase = %q, want %q", task.Phase, PhaseShaping)
	}

	// Verify it was persisted
	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}
	if loaded.Phase != PhaseShaping {
		t.Errorf("persisted Phase = %q, want %q", loaded.Phase, PhaseShaping)
	}
}

func TestSetPhase_FullForwardCycle(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Description")
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	phases := []Phase{PhaseShaping, PhaseReady, PhaseImplementation, PhaseVerification, PhaseDone}
	for _, p := range phases {
		if err := task.SetPhase(dir, p, nil); err != nil {
			t.Fatalf("SetPhase → %q failed: %v", p, err)
		}
		if task.Phase != p {
			t.Errorf("Phase = %q, want %q", task.Phase, p)
		}
	}
}

func TestSetPhase_InvalidTransition(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Description")
	task.Phase = PhaseReady
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Cannot go backward to new
	err := task.SetPhase(dir, PhaseNew, nil)
	if err == nil {
		t.Error("expected error for backward transition to new")
	}

	// Phase should not have changed
	if task.Phase != PhaseReady {
		t.Errorf("Phase should still be %q, got %q", PhaseReady, task.Phase)
	}
}

func TestSetPhase_VerifyFailBackward(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Description")
	task.Phase = PhaseVerification
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	if err := task.SetPhase(dir, PhaseImplementation, nil); err != nil {
		t.Fatalf("verify fail backward transition should be allowed: %v", err)
	}

	if task.Phase != PhaseImplementation {
		t.Errorf("Phase = %q, want %q", task.Phase, PhaseImplementation)
	}
}

func TestSetPhase_ReenterShaping(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Description")
	task.Phase = PhaseReady
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	if err := task.SetPhase(dir, PhaseShaping, nil); err != nil {
		t.Fatalf("re-enter shaping should be allowed: %v", err)
	}

	if task.Phase != PhaseShaping {
		t.Errorf("Phase = %q, want %q", task.Phase, PhaseShaping)
	}
}

func TestSetPhase_WithMemorySyncCreate(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test task", "Description")
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Use echo to simulate issue creation
	wf := &config.WorkflowConfig{
		Issue: &config.WorkflowIssueConfig{
			Create: `echo 123`,
		},
	}

	if err := task.SetPhase(dir, PhaseShaping, wf); err != nil {
		t.Fatalf("SetPhase failed: %v", err)
	}

	// Memory ref should have been set
	if task.MemoryRef != "123" {
		t.Errorf("MemoryRef = %q, want %q", task.MemoryRef, "123")
	}

	// Verify persisted
	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}
	if loaded.MemoryRef != "123" {
		t.Errorf("persisted MemoryRef = %q, want %q", loaded.MemoryRef, "123")
	}
}

func TestSetPhase_WithMemorySyncUpdate(t *testing.T) {
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "status.txt")

	task := NewTask("test", "test", "Test task", "Description")
	task.Phase = PhaseShaping
	task.MemoryRef = "42"
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	wf := &config.WorkflowConfig{
		Issue: &config.WorkflowIssueConfig{
			SetStatus: `echo "$Status" > ` + statusFile,
		},
	}

	if err := task.SetPhase(dir, PhaseReady, wf); err != nil {
		t.Fatalf("SetPhase failed: %v", err)
	}

	// Check the status was written
	data, err := os.ReadFile(statusFile)
	if err != nil {
		t.Fatalf("reading status file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "ready" {
		t.Errorf("status = %q, want %q", got, "ready")
	}
}

func TestSetPhase_NoWorkflowConfig(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Description")
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// nil workflow config — memory sync should silently skip
	if err := task.SetPhase(dir, PhaseShaping, nil); err != nil {
		t.Fatalf("SetPhase with nil wf should succeed: %v", err)
	}
	if task.MemoryRef != "" {
		t.Errorf("MemoryRef should be empty, got %q", task.MemoryRef)
	}
}

func TestSetPhase_NoIssueConfig(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Description")
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	// Workflow config without issue section — memory sync should silently skip
	wf := &config.WorkflowConfig{}
	if err := task.SetPhase(dir, PhaseShaping, wf); err != nil {
		t.Fatalf("SetPhase with no issue config should succeed: %v", err)
	}
	if task.MemoryRef != "" {
		t.Errorf("MemoryRef should be empty, got %q", task.MemoryRef)
	}
}

func TestSyncMemory_CreateFailure(t *testing.T) {
	task := &Task{
		Title:       "Test",
		Description: "Desc",
		Phase:       PhaseShaping,
	}

	wf := &config.WorkflowConfig{
		Issue: &config.WorkflowIssueConfig{
			Create: `exit 1`,
		},
	}

	updated := syncMemory(task, wf)
	if updated {
		t.Error("syncMemory should return false when create fails")
	}
	if task.MemoryRef != "" {
		t.Errorf("MemoryRef should be empty after create failure, got %q", task.MemoryRef)
	}
}

func TestSyncMemory_SkipsWhenNoConfig(t *testing.T) {
	task := &Task{Phase: PhaseShaping}

	if syncMemory(task, nil) {
		t.Error("should return false for nil config")
	}
	if syncMemory(task, &config.WorkflowConfig{}) {
		t.Error("should return false for nil issue config")
	}
}
