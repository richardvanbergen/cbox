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
	"github.com/richvanbergen/cbox/internal/sandbox"
)

// reportDir returns the report directory path for a branch.
func reportDir(projectDir, branch string) string {
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	return filepath.Join(projectDir, ".cbox", "reports", safeBranch)
}

// FlowInit writes default workflow config into .cbox.yml.
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

	fmt.Printf("Added workflow config to %s\n", config.ConfigFile)
	fmt.Println("Defaults use 'gh' CLI. Edit the workflow section to use a different issue tracker.")
	return nil
}

// FlowStart begins a new workflow: creates issue, sandbox, writes task file, and sets up context.
func FlowStart(projectDir, description string, yolo bool) error {
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
	branchTmpl := "{{.Slug}}"
	if wf.Branch != "" {
		branchTmpl = wf.Branch
	}
	branch, err := renderTemplate(branchTmpl, map[string]string{"Slug": slug})
	if err != nil {
		return fmt.Errorf("rendering branch template: %w", err)
	}

	// Create issue if configured
	var issueID string
	if wf.Issue != nil && wf.Issue.Create != "" {
		fmt.Println("Creating issue...")
		issueID, err = runShellCommand(wf.Issue.Create, map[string]string{
			"Title":       description,
			"Description": description,
		})
		if err != nil {
			return fmt.Errorf("creating issue: %w", err)
		}
		fmt.Printf("Created issue #%s\n", issueID)
	}

	// Create flow state
	state := &FlowState{
		Branch:      branch,
		Title:       description,
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
	fmt.Println("Starting sandbox...")
	if err := sandbox.UpWithOptions(projectDir, branch, sandbox.UpOptions{
		ReportDir: repDir,
	}); err != nil {
		return fmt.Errorf("starting sandbox: %w", err)
	}

	// Get worktree path from sandbox state
	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("loading sandbox state: %w", err)
	}

	// Fetch issue content and write task file
	var issueContent string
	if issueID != "" && wf.Issue != nil && wf.Issue.View != "" {
		fmt.Println("Fetching issue content...")
		issueContent, err = runShellCommand(wf.Issue.View, map[string]string{
			"IssueID": issueID,
		})
		if err != nil {
			fmt.Printf("Warning: could not fetch issue content: %v\n", err)
		}
	}

	if err := writeTaskFile(sandboxState.WorktreePath, issueID, issueContent); err != nil {
		return fmt.Errorf("writing task file: %w", err)
	}

	// Append task pointer to container's CLAUDE.md
	taskInstruction := `
## Task Assignment

You have been assigned a task. Read ` + "`/workspace/.cbox-task`" + ` for the full issue details.
When you are done, call the ` + "`cbox_report`" + ` MCP tool with type "done" to report your results.`

	if err := docker.AppendClaudeMD(sandboxState.ClaudeContainer, taskInstruction); err != nil {
		fmt.Printf("Warning: could not append task instruction to CLAUDE.md: %v\n", err)
	}

	// Update issue status
	if issueID != "" && wf.Issue != nil && wf.Issue.SetStatus != "" {
		runShellCommand(wf.Issue.SetStatus, map[string]string{
			"IssueID": issueID,
			"Status":  "in-progress",
		})
	}

	if !yolo {
		fmt.Printf("\nSandbox ready. Run 'cbox flow chat %s' to begin.\n", branch)
		return nil
	}

	// Yolo mode: run headless prompt then create PR
	taskContent := description
	if issueContent != "" {
		taskContent = issueContent
	}

	var customYolo string
	if wf.Prompts != nil {
		customYolo = wf.Prompts.Yolo
	}
	prompt, err := renderPrompt(defaultYoloPrompt, customYolo, map[string]string{
		"TaskContent": taskContent,
	})
	if err != nil {
		return fmt.Errorf("rendering yolo prompt: %w", err)
	}

	fmt.Println("Running in yolo mode...")
	if err := sandbox.ChatPrompt(projectDir, branch, prompt); err != nil {
		return fmt.Errorf("yolo execution failed: %w", err)
	}

	fmt.Println("Creating PR...")
	return FlowPR(projectDir, branch)
}

// FlowChat refreshes the task file from the issue and opens an interactive chat.
func FlowChat(projectDir, branch string) error {
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
	if state.IssueID != "" && wf != nil && wf.Issue != nil && wf.Issue.View != "" {
		fmt.Println("Refreshing task from issue...")
		issueContent, err := runShellCommand(wf.Issue.View, map[string]string{
			"IssueID": state.IssueID,
		})
		if err != nil {
			fmt.Printf("Warning: could not fetch issue content: %v\n", err)
		} else {
			if err := writeTaskFile(sandboxState.WorktreePath, state.IssueID, issueContent); err != nil {
				fmt.Printf("Warning: could not update task file: %v\n", err)
			}
		}
	}

	// Check if browser is enabled
	var chrome bool
	if cfg.Browser {
		chrome = true
	}

	return sandbox.Chat(projectDir, branch, chrome)
}

// writeTaskFile writes a .cbox-task file to the worktree.
func writeTaskFile(worktreePath, issueID, issueContent string) error {
	var content string
	if issueID != "" {
		content = fmt.Sprintf("Issue: #%s\n\n%s\n", issueID, issueContent)
	} else {
		content = issueContent + "\n"
	}

	path := filepath.Join(worktreePath, ".cbox-task")
	return os.WriteFile(path, []byte(content), 0644)
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
	fmt.Println("Pushing branch...")
	if _, err := runShellCommand("git push -u origin {{.Branch}}", map[string]string{
		"Branch": branch,
	}); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	fmt.Println("Creating PR...")
	prURL, err := runShellCommand(wf.PR.Create, map[string]string{
		"Title":       state.Title,
		"Description": description,
	})
	if err != nil {
		return fmt.Errorf("creating PR: %w", err)
	}

	state.PRURL = prURL
	if err := SaveFlowState(projectDir, state); err != nil {
		return fmt.Errorf("saving flow state: %w", err)
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

	fmt.Printf("PR created: %s\n", prURL)
	fmt.Printf("To merge: cbox flow merge %s\n", branch)
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
		fmt.Println("Merging PR...")
		if _, err := runShellCommand(wf.PR.Merge, map[string]string{
			"PRURL": state.PRURL,
		}); err != nil {
			return fmt.Errorf("merging PR: %w", err)
		}
	} else {
		fmt.Println("No PR merge command configured — merge manually.")
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
	fmt.Println("Cleaning up sandbox...")
	if err := sandbox.Clean(projectDir, branch); err != nil {
		fmt.Printf("Warning: sandbox cleanup failed: %v\n", err)
	}

	// Remove flow state and reports
	RemoveFlowState(projectDir, branch)
	repDir := reportDir(projectDir, branch)
	os.RemoveAll(repDir)

	state.Phase = "done"
	fmt.Println("Flow complete.")
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
		fmt.Println("No active flows.")
		return nil
	}

	for _, s := range states {
		fmt.Printf("%-30s %-15s %s\n", s.Branch, s.Phase, s.Title)
	}
	return nil
}

func printFlowState(projectDir string, s *FlowState) {
	fmt.Printf("Branch:      %s\n", s.Branch)
	fmt.Printf("Title:       %s\n", s.Title)
	if s.Description != "" {
		fmt.Printf("Description: %s\n", s.Description)
	}
	fmt.Printf("Phase:       %s\n", s.Phase)
	if s.IssueID != "" {
		fmt.Printf("Issue:       #%s\n", s.IssueID)
	}
	if s.PRURL != "" {
		fmt.Printf("PR:          %s\n", s.PRURL)
	}
	fmt.Printf("Auto mode:   %v\n", s.AutoMode)
	fmt.Printf("Created:     %s\n", s.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Updated:     %s\n", s.UpdatedAt.Format(time.RFC3339))

	// Show latest report summary
	repDir := reportDir(projectDir, s.Branch)
	reports, err := hostcmd.LoadReports(repDir)
	if err == nil && len(reports) > 0 {
		latest := reports[len(reports)-1]
		fmt.Printf("\nLatest report (%s): %s\n", latest.Type, latest.Title)
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
	fmt.Println("Cleaning up sandbox...")
	if err := sandbox.Clean(projectDir, branch); err != nil {
		fmt.Printf("Warning: sandbox cleanup failed: %v\n", err)
	}

	// Remove flow state and reports
	RemoveFlowState(projectDir, branch)
	repDir := reportDir(projectDir, branch)
	os.RemoveAll(repDir)

	fmt.Printf("Flow '%s' abandoned.\n", state.Title)
	return nil
}
