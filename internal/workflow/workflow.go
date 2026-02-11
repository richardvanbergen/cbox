package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/docker"
	"github.com/richvanbergen/cbox/internal/hostcmd"
	"github.com/richvanbergen/cbox/internal/output"
	"github.com/richvanbergen/cbox/internal/sandbox"
)

// resolveOpenCommand determines the open command to run based on the flag state.
// If openFlag is false, no open command should run (returns "").
// If openFlag is true and openCmd is non-empty, it returns openCmd.
// If openFlag is true and openCmd is empty (or whitespace-only), it falls back to configOpen.
func resolveOpenCommand(openFlag bool, openCmd, configOpen string) string {
	if !openFlag {
		return ""
	}
	cmd := strings.TrimSpace(openCmd)
	if cmd == "" {
		cmd = configOpen
	}
	return cmd
}

// reportDir returns the report directory path for a branch.
func reportDir(projectDir, branch string) string {
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	return filepath.Join(projectDir, ".cbox", "reports", safeBranch)
}

// FlowInit writes default workflow config into .cbox.toml.
func FlowInit(projectDir string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return fmt.Errorf("could not load %s — run 'cbox init' first: %w", config.ConfigFile, err)
	}

	if cfg.Workflow != nil {
		return fmt.Errorf("workflow config already exists in %s", config.ConfigFile)
	}

	cfg.Workflow = config.DefaultWorkflowConfig()
	if err := cfg.Save(projectDir); err != nil {
		return err
	}

	output.Success("Added workflow config to %s", config.ConfigFile)
	output.Text("Defaults use 'gh' CLI. Edit the workflow section to use a different issue tracker.")
	return nil
}

// FlowStart begins a new workflow: creates issue, sandbox, writes task file, and sets up context.
// If openFlag is true, the open command runs after the sandbox is ready. openCmd overrides the
// config default; when openCmd is empty the value from cfg.Open is used.
func FlowStart(projectDir, description string, yolo bool, openFlag bool, openCmd string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	wf := cfg.Workflow
	if wf == nil {
		return fmt.Errorf("no workflow config — run 'cbox flow init' first")
	}

	// Generate branch name from description
	slug := slugify(description)
	branchTmpl := "$Slug"
	if wf.Branch != "" {
		branchTmpl = wf.Branch
	}
	branch := expandVars(branchTmpl, map[string]string{"Slug": slug})

	// Create issue if configured
	title := summarize(description)
	var issueID string
	if wf.Issue != nil && wf.Issue.Create != "" {
		output.Progress("Creating issue...")
		issueID, err = runShellCommand(wf.Issue.Create, map[string]string{
			"Title":       title,
			"Description": description,
		})
		if err != nil {
			return fmt.Errorf("creating issue: %w", err)
		}
		output.Success("Created issue #%s", issueID)
	}

	// Create flow state
	state := &FlowState{
		Branch:      branch,
		Title:       title,
		Description: description,
		Phase:       "started",
		IssueID:     issueID,
		AutoMode:    yolo,
		CreatedAt:   time.Now(),
	}
	if err := SaveFlowState(projectDir, state); err != nil {
		return fmt.Errorf("saving flow state: %w", err)
	}

	// Start sandbox with report dir
	repDir := reportDir(projectDir, branch)
	output.Progress("Starting sandbox...")
	if err := sandbox.UpWithOptions(projectDir, branch, sandbox.UpOptions{
		ReportDir:  repDir,
		FlowBranch: branch,
	}); err != nil {
		return fmt.Errorf("starting sandbox: %w", err)
	}

	// Get worktree path from sandbox state
	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("loading sandbox state: %w", err)
	}

	// Fetch issue content and write task file
	tf := &TaskFile{
		Task: TaskInfo{
			Title:       description,
			Description: description,
		},
	}

	if issueID != "" && wf.Issue != nil && wf.Issue.View != "" {
		output.Progress("Fetching issue content...")
		issueContent, err := runShellCommand(wf.Issue.View, map[string]string{
			"IssueID": issueID,
		})
		if err != nil {
			output.Warning("Could not fetch issue content: %v", err)
		} else {
			issueInfo, parseErr := parseIssueJSON(issueContent)
			if parseErr != nil {
				// Fall back for custom non-JSON view commands
				tf.Issue = &IssueInfo{
					ID:   issueID,
					Body: issueContent,
				}
			} else {
				issueInfo.ID = issueID
				tf.Issue = issueInfo
				if issueInfo.Title != "" {
					tf.Task.Title = issueInfo.Title
				}
			}
		}
	}

	if tf.Issue == nil && issueID != "" {
		tf.Issue = &IssueInfo{ID: issueID}
	}

	if err := writeStructuredTaskFile(sandboxState.WorktreePath, tf); err != nil {
		return fmt.Errorf("writing task file: %w", err)
	}

	// Append workflow instructions to container's CLAUDE.md
	taskInstruction := `
## CBox Flow — Task Assignment

You are working inside a cbox flow. Read ` + "`/workspace/.cbox-task`" + ` for your task details.

### Your responsibilities
- Read the codebase, implement changes, write tests, and commit your work
- When done, call the ` + "`cbox_report`" + ` MCP tool with type "done" to report results
- When the user agrees the work is ready, use the ` + "`cbox_flow_pr`" + ` MCP tool to create a PR

### What you must NOT do
The cbox flow system manages all issue tracking on your behalf.
Do NOT use gh or any tool to:
- Create, update, close, or comment on issues
- Create PRs directly (use the ` + "`cbox_flow_pr`" + ` tool instead)
- Push branches (` + "`cbox_flow_pr`" + ` handles pushing)`

	if err := docker.AppendClaudeMD(sandboxState.ClaudeContainer, taskInstruction); err != nil {
		output.Warning("Could not append task instruction to CLAUDE.md: %v", err)
	}

	// Update issue status
	if issueID != "" && wf.Issue != nil && wf.Issue.SetStatus != "" {
		runShellCommand(wf.Issue.SetStatus, map[string]string{
			"IssueID": issueID,
			"Status":  "in-progress",
		})
	}

	// Run open command if --open flag was explicitly provided
	if open := resolveOpenCommand(openFlag, openCmd, cfg.Open); open != "" {
		if _, err := runShellCommand(open, map[string]string{"Dir": sandboxState.WorktreePath}); err != nil {
			output.Warning("Open command failed: %v", err)
		}
	}

	if !yolo {
		output.Success("Sandbox ready. Run 'cbox flow chat %s' to begin.", branch)
		return nil
	}

	// Yolo mode: run headless prompt then create PR
	taskContent := description
	if tf.Issue != nil && tf.Issue.Body != "" {
		taskContent = tf.Issue.Body
	}

	var customYolo string
	if wf.Prompts != nil {
		customYolo = wf.Prompts.Yolo
	}
	prompt := renderPrompt(defaultYoloPrompt, customYolo, map[string]string{
		"TaskContent": taskContent,
	})

	output.Progress("Running in yolo mode...")
	if err := sandbox.ChatPrompt(projectDir, branch, prompt); err != nil {
		return fmt.Errorf("yolo execution failed: %w", err)
	}

	output.Progress("Creating PR...")
	return FlowPR(projectDir, branch)
}

// FlowChat refreshes the task file from the issue and opens an interactive chat.
// If openFlag is true, the open command runs before chat. openCmd overrides the
// config default; when openCmd is empty the value from cfg.Open is used.
func FlowChat(projectDir, branch string, openFlag bool, openCmd string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	state, err := LoadFlowState(projectDir, branch)
	if err != nil {
		return err
	}

	wf := cfg.Workflow

	// Get worktree path from sandbox state
	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("loading sandbox state: %w", err)
	}

	// Refresh task file from issue
	tf := &TaskFile{
		Task: TaskInfo{
			Title:       state.Title,
			Description: state.Description,
		},
	}

	if state.IssueID != "" && wf != nil && wf.Issue != nil && wf.Issue.View != "" {
		output.Progress("Refreshing task from issue...")
		issueContent, err := runShellCommand(wf.Issue.View, map[string]string{
			"IssueID": state.IssueID,
		})
		if err != nil {
			output.Warning("Could not fetch issue content: %v", err)
			tf.Issue = &IssueInfo{ID: state.IssueID}
		} else {
			issueInfo, parseErr := parseIssueJSON(issueContent)
			if parseErr != nil {
				tf.Issue = &IssueInfo{
					ID:   state.IssueID,
					Body: issueContent,
				}
			} else {
				issueInfo.ID = state.IssueID
				tf.Issue = issueInfo
			}
		}
	}

	if state.PRURL != "" || state.PRNumber != "" {
		tf.PR = &PRInfo{
			Number: state.PRNumber,
			URL:    state.PRURL,
		}
	}

	if err := writeStructuredTaskFile(sandboxState.WorktreePath, tf); err != nil {
		output.Warning("Could not update task file: %v", err)
	}

	// Run open command only if --open flag was explicitly provided
	if open := resolveOpenCommand(openFlag, openCmd, cfg.Open); open != "" {
		if _, err := runShellCommand(open, map[string]string{"Dir": sandboxState.WorktreePath}); err != nil {
			output.Warning("Open command failed: %v", err)
		}
	}

	// Check if browser is enabled
	var chrome bool
	if cfg.Browser {
		chrome = true
	}

	resume := state.Chatted

	if !state.Chatted {
		state.Chatted = true
		if err := SaveFlowState(projectDir, state); err != nil {
			return fmt.Errorf("saving flow state: %w", err)
		}
	}

	var initialPrompt string
	if !resume {
		initialPrompt = `Do these two things before anything else:

1. Read /workspace/.cbox-task and summarize the task.
2. Check your environment: what runtimes, tools, and commands are available? Try running the project's build and test commands via your MCP tools. If anything is missing or broken, warn me clearly about what's not working and what needs to be fixed before we can start.

After reporting both, wait for my instructions.`
	}

	return sandbox.Chat(projectDir, branch, chrome, initialPrompt, resume)
}

// FlowPR creates a pull request for the flow.
func FlowPR(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	state, err := LoadFlowState(projectDir, branch)
	if err != nil {
		return err
	}

	if state.Phase == "done" || state.Phase == "abandoned" {
		return fmt.Errorf("flow is in %q phase — cannot create PR", state.Phase)
	}

	wf := cfg.Workflow
	if wf == nil || wf.PR == nil || wf.PR.Create == "" {
		return fmt.Errorf("no PR create command configured")
	}

	// Load sandbox state to get worktree path — git/gh commands must
	// run there so they see the correct branch.
	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("loading sandbox state: %w", err)
	}
	wtPath := sandboxState.WorktreePath

	// Build PR description from reports
	repDir := reportDir(projectDir, branch)
	reports, _ := hostcmd.LoadReports(repDir)

	var description string
	for _, r := range reports {
		if r.Type == "done" {
			description = r.Body
		}
	}
	if description == "" {
		description = state.Description
	}
	if description == "" {
		description = state.Title
	}

	// Push the branch first
	output.Progress("Pushing branch...")
	if _, err := runShellCommandInDir("git push -u origin $Branch", map[string]string{
		"Branch": branch,
	}, wtPath); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	output.Progress("Creating PR...")
	prOutput, err := runShellCommandInDir(wf.PR.Create, map[string]string{
		"Title":       state.Title,
		"Description": description,
	}, wtPath)
	if err != nil {
		return fmt.Errorf("creating PR: %w", err)
	}

	prURL, prNumber, parseErr := parsePROutput(prOutput)
	if parseErr != nil {
		output.Warning("Could not parse PR number: %v", parseErr)
		prURL = prOutput
	}

	state.PRURL = prURL
	state.PRNumber = prNumber
	if err := SaveFlowState(projectDir, state); err != nil {
		return fmt.Errorf("saving flow state: %w", err)
	}

	// Update task file with PR info
	existing, _ := loadTaskFile(wtPath)
	if existing != nil {
		existing.PR = &PRInfo{
			Number: prNumber,
			URL:    prURL,
		}
		if err := writeStructuredTaskFile(wtPath, existing); err != nil {
			output.Warning("Could not update task file with PR info: %v", err)
		}
	}

	// Update issue status and comment with PR link
	if state.IssueID != "" && wf.Issue != nil {
		if wf.Issue.SetStatus != "" {
			runShellCommand(wf.Issue.SetStatus, map[string]string{
				"IssueID": state.IssueID,
				"Status":  "review",
			})
		}
		if wf.Issue.Comment != "" {
			runShellCommand(wf.Issue.Comment, map[string]string{
				"IssueID": state.IssueID,
				"Body":    fmt.Sprintf("PR created: %s", prURL),
			})
		}
	}

	output.Success("PR created: %s", prURL)
	output.Text("To merge: cbox flow merge %s", branch)
	return nil
}

// FlowMerge merges the PR and cleans up.
func FlowMerge(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	state, err := LoadFlowState(projectDir, branch)
	if err != nil {
		return err
	}

	wf := cfg.Workflow

	// Merge PR
	if state.PRURL != "" && wf != nil && wf.PR != nil && wf.PR.Merge != "" {
		prNumber := state.PRNumber
		if prNumber == "" {
			// Fallback: extract from URL for old state files
			_, extracted, _ := parsePROutput(state.PRURL)
			prNumber = extracted
		}

		output.Progress("Merging PR...")
		if _, err := runShellCommand(wf.PR.Merge, map[string]string{
			"PRURL":    state.PRURL,
			"PRNumber": prNumber,
		}); err != nil {
			return fmt.Errorf("merging PR: %w", err)
		}
	} else {
		output.Warning("No PR merge command configured — merge manually.")
	}

	// Update and close issue
	if state.IssueID != "" && wf != nil && wf.Issue != nil {
		if wf.Issue.SetStatus != "" {
			runShellCommand(wf.Issue.SetStatus, map[string]string{
				"IssueID": state.IssueID,
				"Status":  "done",
			})
		}
		if wf.Issue.Close != "" {
			runShellCommand(wf.Issue.Close, map[string]string{
				"IssueID": state.IssueID,
			})
		}
	}

	// Clean up sandbox
	output.Progress("Cleaning up sandbox...")
	if err := sandbox.Clean(projectDir, branch); err != nil {
		output.Warning("Sandbox cleanup failed: %v", err)
	}

	// Remove flow state and reports
	RemoveFlowState(projectDir, branch)
	repDir := reportDir(projectDir, branch)
	os.RemoveAll(repDir)

	state.Phase = "done"
	output.Success("Flow complete.")
	return nil
}

// FlowStatus shows the status of active flows.
func FlowStatus(projectDir, branch string) error {
	if branch != "" {
		state, err := LoadFlowState(projectDir, branch)
		if err != nil {
			return err
		}
		printFlowState(projectDir, state)
		return nil
	}

	states, err := ListFlowStates(projectDir)
	if err != nil {
		return err
	}

	if len(states) == 0 {
		output.Text("No active flows.")
		return nil
	}

	for _, s := range states {
		output.Text("%-30s %-15s %s", s.Branch, s.Phase, s.Title)
	}
	return nil
}

func printFlowState(projectDir string, s *FlowState) {
	output.Text("Branch:      %s", s.Branch)
	output.Text("Title:       %s", s.Title)
	if s.Description != "" {
		output.Text("Description: %s", s.Description)
	}
	output.Text("Phase:       %s", s.Phase)
	if s.IssueID != "" {
		output.Text("Issue:       #%s", s.IssueID)
	}
	if s.PRURL != "" {
		output.Text("PR:          %s", s.PRURL)
	}
	output.Text("Auto mode:   %v", s.AutoMode)
	output.Text("Created:     %s", s.CreatedAt.Format(time.RFC3339))
	output.Text("Updated:     %s", s.UpdatedAt.Format(time.RFC3339))

	// Show latest report summary
	repDir := reportDir(projectDir, s.Branch)
	reports, err := hostcmd.LoadReports(repDir)
	if err == nil && len(reports) > 0 {
		latest := reports[len(reports)-1]
		output.Text("Latest report (%s): %s", latest.Type, latest.Title)
	}
}

// FlowAbandon cancels a flow and cleans up.
func FlowAbandon(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	state, err := LoadFlowState(projectDir, branch)
	if err != nil {
		return err
	}

	wf := cfg.Workflow

	// Close and label the issue
	if state.IssueID != "" && wf != nil && wf.Issue != nil {
		if wf.Issue.SetStatus != "" {
			runShellCommand(wf.Issue.SetStatus, map[string]string{
				"IssueID": state.IssueID,
				"Status":  "cancelled",
			})
		}
		if wf.Issue.Close != "" {
			runShellCommand(wf.Issue.Close, map[string]string{
				"IssueID": state.IssueID,
			})
		}
	}

	// Clean up sandbox
	output.Progress("Cleaning up sandbox...")
	if err := sandbox.Clean(projectDir, branch); err != nil {
		output.Warning("Sandbox cleanup failed: %v", err)
	}

	// Remove flow state and reports
	RemoveFlowState(projectDir, branch)
	repDir := reportDir(projectDir, branch)
	os.RemoveAll(repDir)

	output.Success("Flow '%s' abandoned.", state.Title)
	return nil
}
