package workflow

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/output"
	"github.com/richvanbergen/cbox/internal/sandbox"
	"github.com/richvanbergen/cbox/internal/worktree"
)

// FlowNewYolo orchestrates the full pipeline non-interactively:
// create task → generate plan → implement → create PR.
func FlowNewYolo(projectDir, roughDesc string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	wf := cfg.Workflow
	if wf == nil {
		return fmt.Errorf("no workflow config — run 'cbox flow init' first")
	}

	// Step 1: Polish the rough description (auto-accept, no confirm loop)
	title, description := polishTask(roughDesc)
	output.Success("Task: %s", title)

	// Step 2: Slugify → branch name
	slug := slugify(title)
	branchTmpl := "$Slug"
	if wf.Branch != "" {
		branchTmpl = wf.Branch
	}
	branch := expandVars(branchTmpl, map[string]string{"Slug": slug})
	branch, slug = resolveBranchConflict(projectDir, branch, slug)

	// Check for existing task
	wtPath := worktree.WorktreePath(projectDir, branch)
	if TaskExists(wtPath) {
		return fmt.Errorf("task already exists for branch %q — use 'cbox flow run --yolo %s' to resume", branch, branch)
	}

	// Step 3: Start sandbox
	repDir := reportDir(projectDir, branch)
	if err := output.Spin("Starting sandbox", func() error {
		return sandbox.UpWithOptions(projectDir, branch, sandbox.UpOptions{
			ReportDir:  repDir,
			FlowBranch: branch,
		})
	}); err != nil {
		return fmt.Errorf("starting sandbox: %w", err)
	}

	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("loading sandbox state: %w", err)
	}
	wtPath = sandboxState.WorktreePath

	// Step 4: Create task
	task := NewTask(slug, branch, title, description)
	task.Container = sandboxState.ClaudeContainer
	if err := SaveTask(wtPath, task); err != nil {
		return fmt.Errorf("writing task file: %w", err)
	}
	output.Success("Task created on branch '%s'", branch)

	// Step 5: Advance to shaping and create plan scaffold
	if err := task.SetPhase(wtPath, PhaseShaping, wf); err != nil {
		return fmt.Errorf("advancing to shaping: %w", err)
	}

	planPath := filepath.Join(wtPath, ".cbox", "plan.md")
	if err := createPlanScaffold(planPath, title, description); err != nil {
		return fmt.Errorf("creating plan scaffold: %w", err)
	}
	task.Plan = ".cbox/plan.md"
	if err := SaveTask(wtPath, task); err != nil {
		return fmt.Errorf("saving task with plan path: %w", err)
	}

	// Step 6: Generate plan via ChatPrompt
	output.Progress("Generating plan")
	prompt := buildYoloShapingPrompt(task)
	if err := sandbox.ChatPrompt(projectDir, branch, prompt); err != nil {
		return fmt.Errorf("plan generation failed: %w", err)
	}

	// Step 7: Verify plan was generated
	task, err = LoadTask(wtPath)
	if err != nil {
		return fmt.Errorf("reloading task after plan generation: %w", err)
	}
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		return fmt.Errorf("plan generation did not produce a plan file — debug with 'cbox flow shape %s'", branch)
	}
	if task.Phase != PhaseReady {
		// Auto-advance if Claude didn't update the phase
		if err := task.SetPhase(wtPath, PhaseReady, wf); err != nil {
			return fmt.Errorf("advancing to ready: %w", err)
		}
	}
	output.Success("Plan ready")

	// Step 8: Run implementation (yolo mode — creates PR and advances to verification)
	output.Progress("Running implementation (yolo mode)")
	return FlowRun(projectDir, branch, true)
}

const yoloShapingPromptTemplate = `You are in YOLO SHAPING MODE for a cbox flow task.

Task: $Title
Description: $Description

Your goal is to produce a clear, actionable plan at /workspace/.cbox/plan.md.

Rules:
- Research the codebase thoroughly to understand the current state.
- Write a complete, detailed plan with:
  - Context section (what you learned from the codebase)
  - Approach section (step-by-step implementation plan)
  - Acceptance Criteria section (clear, testable items)
- The plan must be detailed enough for implementation without re-deriving the approach.
- Do NOT write implementation code. Pseudocode and architecture sketches are fine.
- Use your best judgment — do not ask questions, make reasonable decisions.
- When the plan is complete, update /workspace/.cbox/task.json — change "phase" to "ready".
- IMPORTANT: Do NOT commit or git-add any files in .cbox/.`

// buildYoloShapingPrompt expands the yolo shaping template with task data.
func buildYoloShapingPrompt(task *Task) string {
	return expandVars(yoloShapingPromptTemplate, map[string]string{
		"Title":       task.Title,
		"Description": task.Description,
	})
}
