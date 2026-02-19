package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/output"
	"github.com/richvanbergen/cbox/internal/sandbox"
)

// FlowRun enters the implementation phase: validates that shaping is complete,
// sets the phase, and launches chat with the implementation prompt.
// If yolo is true, runs non-interactively and creates a PR when done.
func FlowRun(projectDir, branch string, yolo bool) error {
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

	alreadyImplementing := task.Phase == PhaseImplementation

	if !alreadyImplementing {
		// Accept "ready" (normal) or "shaping" with plan (fallback)
		switch task.Phase {
		case PhaseReady:
			// Normal path — shaping is done
		case PhaseShaping:
			// Fallback: if plan.md exists, auto-advance through ready
			planPath := filepath.Join(wtPath, ".cbox", "plan.md")
			if _, err := os.Stat(planPath); os.IsNotExist(err) {
				return fmt.Errorf("task is in shaping phase and no plan exists — run 'cbox flow shape %s' first", branch)
			}
			// Advance shaping → ready → implementation
			if err := task.SetPhase(wtPath, PhaseReady, cfg.Workflow); err != nil {
				return fmt.Errorf("advancing to ready: %w", err)
			}
		default:
			return fmt.Errorf("cannot start implementation from phase %q — task must be in 'ready' phase", task.Phase)
		}

		// Advance to implementation
		if err := task.SetPhase(wtPath, PhaseImplementation, cfg.Workflow); err != nil {
			return fmt.Errorf("setting phase to implementation: %w", err)
		}
	}

	// Build implementation prompt
	prompt := buildImplementationPrompt(task, yolo)

	if yolo {
		// Non-interactive: run headless, then create PR
		output.Progress("Running in yolo mode")
		if err := sandbox.ChatPrompt(projectDir, branch, prompt); err != nil {
			return fmt.Errorf("yolo execution failed: %w", err)
		}

		output.Progress("Creating PR")
		return FlowPR(projectDir, branch)
	}

	// Interactive: resume if already implementing and conversation history exists
	resume := alreadyImplementing
	if resume {
		hasHistory, err := sandbox.HasConversationHistory(projectDir, branch)
		if err != nil {
			output.Warning("Could not check conversation history: %v", err)
			hasHistory = false
		}
		if !hasHistory {
			output.Warning("No conversation history found — starting fresh session")
			resume = false
		}
	}

	var initialPrompt string
	if !resume {
		initialPrompt = prompt
	}

	chrome := cfg.Browser
	return sandbox.Chat(projectDir, branch, chrome, initialPrompt, resume)
}

// FlowOpen runs the configured open command for the task's worktree.
// This is a convenience command that works at any phase.
func FlowOpen(projectDir, branch, openCmd string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("loading sandbox state: %w", err)
	}

	open := resolveOpenCommand(true, openCmd, cfg.Open)
	if open == "" {
		return fmt.Errorf("no open command configured — add 'open' to cbox.toml or pass a command with --open")
	}

	_, err = runShellCommand(open, map[string]string{"Dir": sandboxState.WorktreePath})
	return err
}

const implementationPromptTemplate = `You are in IMPLEMENTATION MODE for a cbox flow task.

Task: $Title
Plan: /workspace/.cbox/plan.md

Read the plan file for full details, including acceptance criteria.

- Implement the feature according to the plan.
- Write tests and ensure all acceptance criteria are satisfied.
- When complete, call the cbox_report MCP tool with type "done", then use cbox_flow_pr to create a PR.
- Do not deviate from the plan without discussing with the user first.
- If the plan is unclear or incomplete on a point, ask rather than guess.
- IMPORTANT: Do NOT commit or git-add any files in .cbox/ (task.json, plan.md, etc.). These are local workflow state in .gitignore. Never use "git add -f" to bypass this.`

const yoloModeSuffix = `

You are in YOLO mode — work autonomously. Use your best judgment for minor
decisions that aren't covered by the plan. Only stop for truly ambiguous or
high-risk choices.`

// buildImplementationPrompt constructs the implementation prompt with
// optional yolo mode and verify failure context.
func buildImplementationPrompt(task *Task, yolo bool) string {
	prompt := expandVars(implementationPromptTemplate, map[string]string{
		"Title": task.Title,
	})

	if yolo {
		prompt += yoloModeSuffix
	}

	if len(task.VerifyFailures) > 0 {
		prompt += "\n\nPrevious verification failures (address these):"
		for _, vf := range task.VerifyFailures {
			prompt += fmt.Sprintf("\n- [%s] %s", vf.Timestamp.Format("2006-01-02T15:04:05Z"), vf.Reason)
		}
	}

	return prompt
}

// planExists checks if a plan file exists in the worktree.
func planExists(wtPath string) bool {
	planPath := filepath.Join(wtPath, ".cbox", "plan.md")
	info, err := os.Stat(planPath)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

