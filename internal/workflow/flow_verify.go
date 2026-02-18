package workflow

import (
	"fmt"
	"time"

	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/output"
	"github.com/richvanbergen/cbox/internal/sandbox"
)

// FlowVerifyPass marks the task as verified and advances to done.
// Accepts tasks in any phase except "done" — the user is the final authority.
func FlowVerifyPass(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("loading sandbox state: %w", err)
	}
	wtPath := sandboxState.WorktreePath

	task, err := LoadTask(wtPath)
	if err != nil {
		return fmt.Errorf("loading task: %w", err)
	}

	if task.Phase == PhaseDone {
		return fmt.Errorf("task is already done")
	}

	// Jump directly to done — skip intermediate phases
	task.Phase = PhaseDone
	if err := SaveTask(wtPath, task); err != nil {
		return fmt.Errorf("saving task: %w", err)
	}
	syncMemory(task, cfg.Workflow)

	output.Success("Task verified. Run 'cbox flow merge %s' to merge the PR.", branch)
	return nil
}

// FlowVerifyFail records a verification failure and sends the task back
// to implementation. The reason is required.
// Accepts tasks in any phase except "new" and "done".
func FlowVerifyFail(projectDir, branch, reason string) error {
	if reason == "" {
		return fmt.Errorf("reason is required — use --reason to explain what needs fixing")
	}

	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("loading sandbox state: %w", err)
	}
	wtPath := sandboxState.WorktreePath

	task, err := LoadTask(wtPath)
	if err != nil {
		return fmt.Errorf("loading task: %w", err)
	}

	if task.Phase == PhaseDone {
		return fmt.Errorf("task is already done — cannot fail verification")
	}
	if task.Phase == PhaseNew {
		return fmt.Errorf("task has not started yet — nothing to verify")
	}

	// Record the failure
	task.VerifyFailures = append(task.VerifyFailures, VerifyFailure{
		Reason:    reason,
		Timestamp: time.Now(),
	})

	// Jump directly to implementation
	task.Phase = PhaseImplementation
	if err := SaveTask(wtPath, task); err != nil {
		return fmt.Errorf("saving task: %w", err)
	}
	syncMemory(task, cfg.Workflow)

	output.Warning("Verification failed: %s", reason)
	output.Text("Task moved back to implementation. Run 'cbox flow run %s' to address the issues.", branch)
	return nil
}

// checkMergeGate checks if a task.json exists and enforces the verification
// gate. Returns nil if merge is allowed, error if blocked.
// If no task.json exists (old-style flow), merge is always allowed.
func checkMergeGate(wtPath string) error {
	task, err := LoadTask(wtPath)
	if err != nil {
		// No task.json — old-style flow, allow merge
		return nil
	}

	if task.Phase != PhaseDone {
		return fmt.Errorf("task is in phase %q — run 'cbox flow verify pass' before merging", task.Phase)
	}
	return nil
}
