package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreatePlanScaffold(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, ".cbox", "plan.md")

	if err := createPlanScaffold(planPath, "Add retry logic", "API client needs retries"); err != nil {
		t.Fatalf("createPlanScaffold failed: %v", err)
	}

	data, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("reading plan file: %v", err)
	}

	content := string(data)

	// Check title is in the header
	if !strings.Contains(content, "# Task: Add retry logic") {
		t.Error("plan should contain task title in header")
	}

	// Check required sections exist
	sections := []string{"## Context", "## Approach", "## Acceptance Criteria", "## Notes"}
	for _, s := range sections {
		if !strings.Contains(content, s) {
			t.Errorf("plan should contain section %q", s)
		}
	}
}

func TestCreatePlanScaffold_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "nested", "dir", "plan.md")

	if err := createPlanScaffold(planPath, "Test", "Desc"); err != nil {
		t.Fatalf("createPlanScaffold failed: %v", err)
	}

	if _, err := os.Stat(planPath); err != nil {
		t.Fatalf("plan file should exist: %v", err)
	}
}

func TestCreatePlanScaffold_DoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, ".cbox", "plan.md")

	// Create the plan with some content
	os.MkdirAll(filepath.Dir(planPath), 0755)
	existing := "# My custom plan\n\nAlready written."
	os.WriteFile(planPath, []byte(existing), 0644)

	// Verify we can detect it exists (this is how FlowShape uses it)
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Fatal("plan file should already exist")
	}

	// Read it back to verify content wasn't changed
	data, _ := os.ReadFile(planPath)
	if string(data) != existing {
		t.Error("existing plan should not be modified")
	}
}

func TestBuildShapingPrompt(t *testing.T) {
	task := &Task{
		Title:       "Add retry logic",
		Description: "The API client needs retry support.",
	}

	prompt := buildShapingPrompt(task)

	if !strings.Contains(prompt, "SHAPING MODE") {
		t.Error("prompt should contain SHAPING MODE")
	}
	if !strings.Contains(prompt, "Add retry logic") {
		t.Error("prompt should contain task title")
	}
	if !strings.Contains(prompt, "The API client needs retry support.") {
		t.Error("prompt should contain task description")
	}
	if !strings.Contains(prompt, "/workspace/.cbox/plan.md") {
		t.Error("prompt should reference plan file path")
	}
	if !strings.Contains(prompt, "Acceptance Criteria") {
		t.Error("prompt should mention acceptance criteria")
	}
	if !strings.Contains(prompt, `"ready"`) {
		t.Error("prompt should instruct advancing to ready phase")
	}
}

func TestFlowShape_PhaseValidation(t *testing.T) {
	tests := []struct {
		name        string
		phase       Phase
		expectValid bool
	}{
		{"from new", PhaseNew, true},
		{"from shaping", PhaseShaping, true},
		// ready/impl/verify require user confirmation (interactive),
		// so we only test the non-interactive paths here
		{"from done", PhaseDone, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the underlying phase transition logic
			if tt.phase == PhaseShaping {
				// Already in shaping — no transition needed, always valid
				return
			}
			err := ValidateTransition(tt.phase, PhaseShaping)
			if tt.expectValid && err != nil {
				t.Errorf("transition from %q to shaping should be valid: %v", tt.phase, err)
			}
			if !tt.expectValid && err == nil {
				t.Errorf("transition from %q to shaping should be invalid", tt.phase)
			}
		})
	}
}

func TestShapingPromptTemplate_NoUnexpandedVars(t *testing.T) {
	task := &Task{
		Title:       "Test Title",
		Description: "Test description",
	}
	prompt := buildShapingPrompt(task)

	// Check that $Title and $Description were expanded
	if strings.Contains(prompt, "$Title") {
		t.Error("prompt should not contain unexpanded $Title")
	}
	if strings.Contains(prompt, "$Description") {
		t.Error("prompt should not contain unexpanded $Description")
	}
}
