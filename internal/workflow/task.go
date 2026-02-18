package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/output"
)

// Phase represents a state in the flow state machine.
type Phase string

const (
	PhaseNew            Phase = "new"
	PhaseShaping        Phase = "shaping"
	PhaseReady          Phase = "ready"
	PhaseImplementation Phase = "implementation"
	PhaseVerification   Phase = "verification"
	PhaseDone           Phase = "done"
)

// phaseOrder defines the forward transition order.
var phaseOrder = []Phase{
	PhaseNew,
	PhaseShaping,
	PhaseReady,
	PhaseImplementation,
	PhaseVerification,
	PhaseDone,
}

// ValidPhase returns true if the given phase is recognized.
func ValidPhase(p Phase) bool {
	for _, valid := range phaseOrder {
		if p == valid {
			return true
		}
	}
	return false
}

// phaseIndex returns the index of a phase in the order, or -1.
func phaseIndex(p Phase) int {
	for i, phase := range phaseOrder {
		if p == phase {
			return i
		}
	}
	return -1
}

// VerifyFailure records a verification failure with reason and timestamp.
type VerifyFailure struct {
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

// Task is the single source of truth for task state.
// Stored in .cbox/task.json in the worktree root.
type Task struct {
	Version        int             `json:"version"`
	Slug           string          `json:"slug"`
	Branch         string          `json:"branch"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Phase          Phase           `json:"phase"`
	Container      string          `json:"container,omitempty"`
	Plan           string          `json:"plan,omitempty"`
	MemoryRef      string          `json:"memory_ref,omitempty"`
	PRURL          string          `json:"pr_url,omitempty"`
	PRNumber       string          `json:"pr_number,omitempty"`
	VerifyFailures []VerifyFailure `json:"verify_failures,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

const stateDir = ".cbox"

const taskJSONFile = "task.json"

// NewTask creates a new task with sensible defaults.
func NewTask(slug, branch, title, description string) *Task {
	now := time.Now()
	return &Task{
		Version:     1,
		Slug:        slug,
		Branch:      branch,
		Title:       title,
		Description: description,
		Phase:       PhaseNew,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// TaskPath returns the full path to .cbox/task.json in the given directory.
func TaskPath(dir string) string {
	return filepath.Join(dir, stateDir, taskJSONFile)
}

// LoadTask reads and parses .cbox/task.json from the given directory.
func LoadTask(dir string) (*Task, error) {
	path := TaskPath(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no task file found: %w", err)
	}

	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parsing task file: %w", err)
	}
	return &t, nil
}

// SaveTask writes the task to .cbox/task.json in the given directory.
func SaveTask(dir string, t *Task) error {
	taskDirPath := filepath.Join(dir, stateDir)
	if err := os.MkdirAll(taskDirPath, 0755); err != nil {
		return fmt.Errorf("creating task dir: %w", err)
	}

	t.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling task: %w", err)
	}

	return os.WriteFile(TaskPath(dir), data, 0644)
}

// TaskExists returns true if .cbox/task.json exists in the given directory.
func TaskExists(dir string) bool {
	_, err := os.Stat(TaskPath(dir))
	return err == nil
}

// ValidateTransition checks if a phase transition is allowed.
//
// Allowed transitions:
//   - Any forward transition (higher phase index)
//   - verification → implementation (verify fail)
//   - ready/implementation/verification → shaping (re-enter shaping)
func ValidateTransition(from, to Phase) error {
	if !ValidPhase(from) {
		return fmt.Errorf("invalid current phase: %q", from)
	}
	if !ValidPhase(to) {
		return fmt.Errorf("invalid target phase: %q", to)
	}
	if from == to {
		return fmt.Errorf("already in phase %q", from)
	}

	fromIdx := phaseIndex(from)
	toIdx := phaseIndex(to)

	// Forward transitions are always valid
	if toIdx > fromIdx {
		return nil
	}

	// Allowed backward transitions:

	// 1. verification → implementation (verify fail)
	if from == PhaseVerification && to == PhaseImplementation {
		return nil
	}

	// 2. Re-enter shaping from ready or later (except done)
	if to == PhaseShaping && from != PhaseDone && fromIdx > phaseIndex(PhaseShaping) {
		return nil
	}

	return fmt.Errorf("cannot transition from %q to %q", from, to)
}

// SetPhase transitions the task to a new phase, validates the transition,
// saves the task, and triggers memory sync.
func (t *Task) SetPhase(dir string, to Phase, wf *config.WorkflowConfig) error {
	if err := ValidateTransition(t.Phase, to); err != nil {
		return err
	}

	t.Phase = to
	if err := SaveTask(dir, t); err != nil {
		return err
	}

	// Fire memory sync — may update MemoryRef on first sync
	if updated := syncMemory(t, wf); updated {
		if err := SaveTask(dir, t); err != nil {
			return err
		}
	}

	return nil
}

// syncMemory pushes task state to the configured external system.
// Returns true if the task was modified (e.g. MemoryRef was set).
// Silently skips if no [workflow.issue] is configured.
func syncMemory(t *Task, wf *config.WorkflowConfig) bool {
	if wf == nil || wf.Issue == nil {
		return false
	}

	// First sync: create issue
	if t.MemoryRef == "" && wf.Issue.Create != "" {
		issueID, err := runShellCommand(wf.Issue.Create, map[string]string{
			"Title":       t.Title,
			"Description": t.Description,
		})
		if err == nil {
			issueID = strings.TrimSpace(issueID)
			if issueID != "" {
				t.MemoryRef = issueID
				return true
			}
		}
		return false
	}

	// Subsequent syncs: update status and comment
	if t.MemoryRef != "" {
		vars := map[string]string{
			"IssueID": t.MemoryRef,
			"Status":  string(t.Phase),
		}
		if wf.Issue.SetStatus != "" {
			runShellCommand(wf.Issue.SetStatus, vars)
		}
		if wf.Issue.Comment != "" {
			vars["Body"] = fmt.Sprintf("Phase changed to: %s", t.Phase)
			runShellCommand(wf.Issue.Comment, vars)
		}
	}
	return false
}

// PrintTaskStatus displays the current task state.
func PrintTaskStatus(t *Task) {
	output.Text("Slug:        %s", t.Slug)
	output.Text("Branch:      %s", t.Branch)
	output.Text("Title:       %s", t.Title)
	if t.Description != "" {
		output.Text("Description: %s", t.Description)
	}
	output.Text("Phase:       %s", t.Phase)
	if t.Plan != "" {
		output.Text("Plan:        %s", t.Plan)
	}
	if t.MemoryRef != "" {
		output.Text("Issue:       #%s", t.MemoryRef)
	}
	if t.PRURL != "" {
		output.Text("PR:          %s", t.PRURL)
	}
	if len(t.VerifyFailures) > 0 {
		output.Text("Verify failures: %d", len(t.VerifyFailures))
		for _, vf := range t.VerifyFailures {
			output.Text("  - [%s] %s", vf.Timestamp.Format(time.RFC3339), vf.Reason)
		}
	}
	output.Text("Created:     %s", t.CreatedAt.Format(time.RFC3339))
	output.Text("Updated:     %s", t.UpdatedAt.Format(time.RFC3339))
}
