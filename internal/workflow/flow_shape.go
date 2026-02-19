package workflow

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/sandbox"
)

// FlowShape enters or resumes the shaping phase: sets the phase, creates a
// plan scaffold if needed, and launches an interactive chat with the shaping
// prompt.
func FlowShape(projectDir, branch string) error {
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

	// Check if the PR has been merged remotely — block re-entry on a done task
	if task.PRNumber != "" {
		wf := cfg.Workflow
		if wf != nil && wf.PR != nil && wf.PR.View != "" {
			prStatus, _ := fetchTaskPRStatus(wf, task)
			if prStatus != nil && strings.ToUpper(prStatus.State) == "MERGED" {
				task.Phase = PhaseDone
				SaveTask(wtPath, task)
				return fmt.Errorf("PR has been merged — task is done")
			}
		}
	}

	alreadyShaping := task.Phase == PhaseShaping

	// Validate phase
	if !alreadyShaping {
		if task.Phase == PhaseNew {
			// Normal forward transition
		} else if task.Phase == PhaseDone {
			return fmt.Errorf("task is done — cannot re-enter shaping")
		} else if phaseIndex(task.Phase) > phaseIndex(PhaseShaping) {
			// Re-entering shaping from ready or later — confirm with user
			fmt.Printf("Task is in phase %q. Re-enter shaping? [y/N] ", task.Phase)
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(answer)) != "y" {
				return nil
			}
		}

		// Advance to shaping
		if err := task.SetPhase(wtPath, PhaseShaping, cfg.Workflow); err != nil {
			return fmt.Errorf("setting phase to shaping: %w", err)
		}
	}

	// Create plan scaffold if it doesn't exist
	planPath := filepath.Join(wtPath, ".cbox", "plan.md")
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		if err := createPlanScaffold(planPath, task.Title, task.Description); err != nil {
			return fmt.Errorf("creating plan scaffold: %w", err)
		}
		task.Plan = ".cbox/plan.md"
		if err := SaveTask(wtPath, task); err != nil {
			return fmt.Errorf("saving task with plan path: %w", err)
		}
	}

	// Build initial prompt or resume
	resume := alreadyShaping
	var initialPrompt string
	if !resume {
		initialPrompt = buildShapingPrompt(task)
	}

	chrome := cfg.Browser
	return sandbox.Chat(projectDir, branch, chrome, initialPrompt, resume)
}

// createPlanScaffold writes a plan.md template with the task context.
func createPlanScaffold(path, title, description string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	content := fmt.Sprintf(`# Task: %s

## Context
<!-- What you learned from researching the codebase -->

## Approach
<!-- The plan for how to implement this -->

## Acceptance Criteria
- [ ] (define testable criteria here)

## Notes
<!-- Anything else relevant — edge cases, open questions, etc. -->
`, title)

	return os.WriteFile(path, []byte(content), 0644)
}

const shapingPromptTemplate = `You are in SHAPING MODE for a cbox flow task.

Task: $Title
Description: $Description

Your goal is to produce a clear, actionable plan at /workspace/.cbox/plan.md.

Rules:
- Research the codebase to understand the current state.
- Ask the user clarifying questions about scope, requirements, and edge cases.
- Write an early draft of the plan and iterate on it throughout the session.
- The plan must include an "## Acceptance Criteria" section with clear, testable items.
- Do NOT write implementation code. Pseudocode and architecture sketches are fine.
- The plan should be detailed enough that a separate session can implement it without re-deriving the approach.

If /workspace/.cbox/plan.md already exists, read it and continue from where it left off.

When the plan is complete and the user confirms:
1. Write the final plan to /workspace/.cbox/plan.md
2. Call the cbox_flow_ready MCP tool to advance the task to the ready phase

IMPORTANT: Do NOT commit or git-add any files in .cbox/ (task.json, plan.md, etc.).
These files are local workflow state managed by the cbox system and are in .gitignore.
Never use "git add -f" to bypass .gitignore for these files.`

// buildShapingPrompt expands the shaping template with task data.
func buildShapingPrompt(task *Task) string {
	return expandVars(shapingPromptTemplate, map[string]string{
		"Title":       task.Title,
		"Description": task.Description,
	})
}
