package workflow

import (
	"fmt"

	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/sandbox"
)

// FlowReady marks the shaping phase as complete and advances to ready.
// Validates that the task is in shaping phase and a plan exists.
func FlowReady(projectDir, branch string) error {
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

	if task.Phase != PhaseShaping {
		return fmt.Errorf("task is in phase %q — must be in shaping to mark ready", task.Phase)
	}

	if !planExists(wtPath) {
		return fmt.Errorf("no plan found — write a plan before marking ready")
	}

	return task.SetPhase(wtPath, PhaseReady, cfg.Workflow)
}
