package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestBuildImplementationPrompt_Basic(t *testing.T) {
	task := &Task{
		Title: "Add retry logic",
	}

	prompt := buildImplementationPrompt(task, false)

	if !strings.Contains(prompt, "IMPLEMENTATION MODE") {
		t.Error("prompt should contain IMPLEMENTATION MODE")
	}
	if !strings.Contains(prompt, "Add retry logic") {
		t.Error("prompt should contain task title")
	}
	if !strings.Contains(prompt, "/workspace/.cbox/plan.md") {
		t.Error("prompt should reference plan file")
	}
	if strings.Contains(prompt, "YOLO") {
		t.Error("prompt should NOT contain YOLO when yolo=false")
	}
	if strings.Contains(prompt, "verification failures") {
		t.Error("prompt should NOT contain failures when none exist")
	}
}

func TestBuildImplementationPrompt_Yolo(t *testing.T) {
	task := &Task{
		Title: "Add retry logic",
	}

	prompt := buildImplementationPrompt(task, true)

	if !strings.Contains(prompt, "YOLO mode") {
		t.Error("prompt should contain YOLO mode when yolo=true")
	}
	if !strings.Contains(prompt, "work autonomously") {
		t.Error("prompt should contain autonomous instruction")
	}
}

func TestBuildImplementationPrompt_WithVerifyFailures(t *testing.T) {
	task := &Task{
		Title: "Add retry logic",
		VerifyFailures: []VerifyFailure{
			{Reason: "Tests are failing", Timestamp: time.Date(2025, 2, 18, 10, 0, 0, 0, time.UTC)},
			{Reason: "Missing error handling", Timestamp: time.Date(2025, 2, 18, 11, 0, 0, 0, time.UTC)},
		},
	}

	prompt := buildImplementationPrompt(task, false)

	if !strings.Contains(prompt, "verification failures") {
		t.Error("prompt should contain verification failures section")
	}
	if !strings.Contains(prompt, "Tests are failing") {
		t.Error("prompt should contain first failure reason")
	}
	if !strings.Contains(prompt, "Missing error handling") {
		t.Error("prompt should contain second failure reason")
	}
}

func TestBuildImplementationPrompt_YoloWithFailures(t *testing.T) {
	task := &Task{
		Title: "Fix bug",
		VerifyFailures: []VerifyFailure{
			{Reason: "Bug still present", Timestamp: time.Now()},
		},
	}

	prompt := buildImplementationPrompt(task, true)

	if !strings.Contains(prompt, "YOLO mode") {
		t.Error("yolo section should be present")
	}
	if !strings.Contains(prompt, "verification failures") {
		t.Error("failures section should be present")
	}
}

func TestBuildImplementationPrompt_NoUnexpandedVars(t *testing.T) {
	task := &Task{
		Title: "Test Title",
	}
	prompt := buildImplementationPrompt(task, false)

	if strings.Contains(prompt, "$Title") {
		t.Error("prompt should not contain unexpanded $Title")
	}
}

func TestFormatVerifyFailures_Empty(t *testing.T) {
	result := formatVerifyFailures(nil)
	if result != "" {
		t.Errorf("expected empty string for nil failures, got %q", result)
	}

	result = formatVerifyFailures([]VerifyFailure{})
	if result != "" {
		t.Errorf("expected empty string for empty failures, got %q", result)
	}
}

func TestFormatVerifyFailures_WithEntries(t *testing.T) {
	failures := []VerifyFailure{
		{Reason: "Tests fail", Timestamp: time.Date(2025, 2, 18, 10, 0, 0, 0, time.UTC)},
	}
	result := formatVerifyFailures(failures)

	if !strings.Contains(result, "Tests fail") {
		t.Error("should contain failure reason")
	}
	if !strings.Contains(result, "2025-02-18") {
		t.Error("should contain timestamp")
	}
}

func TestPlanExists(t *testing.T) {
	dir := t.TempDir()

	if planExists(dir) {
		t.Error("planExists should return false when no plan file")
	}

	// Create plan scaffold
	planPath := dir + "/.cbox/plan.md"
	if err := createPlanScaffold(planPath, "Test", "Desc"); err != nil {
		t.Fatalf("createPlanScaffold failed: %v", err)
	}

	if !planExists(dir) {
		t.Error("planExists should return true after creating plan")
	}
}

func TestAdvanceTaskToVerification(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Description")
	task.Phase = PhaseImplementation
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	advanceTaskToVerification(dir, nil)

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}
	if loaded.Phase != PhaseVerification {
		t.Errorf("phase = %q, want %q", loaded.Phase, PhaseVerification)
	}
}

func TestAdvanceTaskToVerification_SkipsWhenNotImplementation(t *testing.T) {
	dir := t.TempDir()

	task := NewTask("test", "test", "Test", "Description")
	task.Phase = PhaseShaping
	if err := SaveTask(dir, task); err != nil {
		t.Fatalf("SaveTask failed: %v", err)
	}

	advanceTaskToVerification(dir, nil)

	loaded, err := LoadTask(dir)
	if err != nil {
		t.Fatalf("LoadTask failed: %v", err)
	}
	if loaded.Phase != PhaseShaping {
		t.Errorf("phase should still be shaping, got %q", loaded.Phase)
	}
}

func TestAdvanceTaskToVerification_NoTaskFile(t *testing.T) {
	dir := t.TempDir()
	// Should not panic when no task.json exists
	advanceTaskToVerification(dir, nil)
}

func TestFlowRun_PhaseValidation(t *testing.T) {
	// Test the phase transition paths that FlowRun uses.
	// FlowRun enforces: ready → implementation (normal) or
	// shaping → ready → implementation (fallback with plan).
	// The state machine allows any forward transition, but FlowRun
	// adds business logic (e.g., shaping requires plan.md to exist).

	// Ready → implementation: always valid
	if err := ValidateTransition(PhaseReady, PhaseImplementation); err != nil {
		t.Errorf("ready→implementation should be valid: %v", err)
	}

	// Shaping → ready → implementation: valid two-step advance
	if err := ValidateTransition(PhaseShaping, PhaseReady); err != nil {
		t.Errorf("shaping→ready should be valid: %v", err)
	}
	if err := ValidateTransition(PhaseReady, PhaseImplementation); err != nil {
		t.Errorf("ready→implementation should be valid: %v", err)
	}

	// Implementation → verification: valid (PR auto-advance)
	if err := ValidateTransition(PhaseImplementation, PhaseVerification); err != nil {
		t.Errorf("implementation→verification should be valid: %v", err)
	}
}
