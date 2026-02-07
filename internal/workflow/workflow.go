package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/richvanbergen/cbox/internal/config"
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

// FlowStart begins a new workflow: creates issue, sandbox, runs research.
func FlowStart(projectDir, title, description string, autoMode bool) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	wf := cfg.Workflow
	if wf == nil {
		return fmt.Errorf("no workflow config — run 'cbox flow init' first")
	}

	// Generate branch name
	slug := slugify(title)
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
		desc := description
		if desc == "" {
			desc = title
		}
		issueID, err = runShellCommand(wf.Issue.Create, map[string]string{
			"Title":       title,
			"Description": desc,
		})
		if err != nil {
			return fmt.Errorf("creating issue: %w", err)
		}
		fmt.Printf("Created issue #%s\n", issueID)
	}

	// Create flow state
	state := &FlowState{
		Branch:      branch,
		Title:       title,
		Description: description,
		Phase:       "research",
		IssueID:     issueID,
		AutoMode:    autoMode,
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

	// Update issue status
	if issueID != "" && wf.Issue != nil && wf.Issue.SetStatus != "" {
		runShellCommand(wf.Issue.SetStatus, map[string]string{
			"IssueID": issueID,
			"Status":  "in-progress",
		})
	}

	// Run research prompt
	fmt.Println("Running research phase...")
	var customResearch string
	if wf.Prompts != nil {
		customResearch = wf.Prompts.Research
	}
	prompt, err := renderPrompt(defaultResearchPrompt, customResearch, map[string]string{
		"Title":       title,
		"Description": description,
	})
	if err != nil {
		return fmt.Errorf("rendering research prompt: %w", err)
	}

	if err := sandbox.ChatPrompt(projectDir, branch, prompt); err != nil {
		return fmt.Errorf("research phase failed: %w", err)
	}

	// Read plan from reports
	reports, err := hostcmd.LoadReports(repDir)
	if err != nil {
		fmt.Printf("Warning: could not read reports: %v\n", err)
	}

	var planReport *hostcmd.Report
	for i := range reports {
		if reports[i].Type == "plan" {
			planReport = &reports[i]
		}
	}

	// Update state to plan_review
	state.Phase = "plan_review"
	if err := SaveFlowState(projectDir, state); err != nil {
		return fmt.Errorf("saving flow state: %w", err)
	}

	if planReport != nil {
		fmt.Printf("\n--- Plan: %s ---\n%s\n---\n\n", planReport.Title, planReport.Body)
	} else {
		fmt.Println("\nResearch complete. No plan report received — check the sandbox for details.")
	}

	if autoMode {
		fmt.Println("Auto mode: continuing to execution...")
		if err := flowExecute(projectDir, cfg, state, repDir); err != nil {
			return err
		}
		fmt.Println("Auto mode: creating PR...")
		return FlowPR(projectDir, branch)
	}

	fmt.Printf("Review the plan, then run: cbox flow continue %s\n", branch)
	return nil
}

// FlowContinue resumes after plan review: runs execution phase.
func FlowContinue(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	state, err := LoadFlowState(projectDir, branch)
	if err != nil {
		return err
	}

	if state.Phase != "plan_review" {
		return fmt.Errorf("flow is in %q phase, expected plan_review", state.Phase)
	}

	repDir := reportDir(projectDir, branch)
	return flowExecute(projectDir, cfg, state, repDir)
}

// flowExecute runs the execution phase of the workflow.
func flowExecute(projectDir string, cfg *config.Config, state *FlowState, repDir string) error {
	wf := cfg.Workflow

	// Read the plan from reports
	reports, err := hostcmd.LoadReports(repDir)
	if err != nil {
		return fmt.Errorf("reading reports: %w", err)
	}

	var plan string
	for _, r := range reports {
		if r.Type == "plan" {
			plan = r.Body
		}
	}

	if plan == "" {
		fmt.Println("Warning: no plan found in reports, proceeding with title/description only")
	}

	// Update state
	state.Phase = "executing"
	if err := SaveFlowState(projectDir, state); err != nil {
		return fmt.Errorf("saving flow state: %w", err)
	}

	// Run execution prompt
	var customExecute string
	if wf != nil && wf.Prompts != nil {
		customExecute = wf.Prompts.Execute
	}
	prompt, err := renderPrompt(defaultExecutePrompt, customExecute, map[string]string{
		"Title":       state.Title,
		"Description": state.Description,
		"Plan":        plan,
	})
	if err != nil {
		return fmt.Errorf("rendering execute prompt: %w", err)
	}

	fmt.Println("Running execution phase...")
	if err := sandbox.ChatPrompt(projectDir, state.Branch, prompt); err != nil {
		return fmt.Errorf("execution phase failed: %w", err)
	}

	// Read done report
	reports, err = hostcmd.LoadReports(repDir)
	if err != nil {
		fmt.Printf("Warning: could not read reports: %v\n", err)
	}

	var doneReport *hostcmd.Report
	for i := range reports {
		if reports[i].Type == "done" {
			doneReport = &reports[i]
		}
	}

	// Update state
	state.Phase = "pr_review"
	if err := SaveFlowState(projectDir, state); err != nil {
		return fmt.Errorf("saving flow state: %w", err)
	}

	if doneReport != nil {
		fmt.Printf("\n--- Done: %s ---\n%s\n---\n\n", doneReport.Title, doneReport.Body)
	} else {
		fmt.Println("\nExecution complete. No done report received — check the sandbox for details.")
	}

	// Comment on issue
	if state.IssueID != "" && wf != nil && wf.Issue != nil && wf.Issue.Comment != "" {
		body := "Execution complete."
		if doneReport != nil {
			body = fmt.Sprintf("**%s**\n\n%s", doneReport.Title, doneReport.Body)
		}
		runShellCommand(wf.Issue.Comment, map[string]string{
			"IssueID": state.IssueID,
			"Body":    body,
		})
	}

	fmt.Printf("Ready for PR. Run: cbox flow pr %s\n", state.Branch)
	return nil
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

	if state.Phase != "pr_review" {
		return fmt.Errorf("flow is in %q phase, expected pr_review", state.Phase)
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

	// Update issue status
	if state.IssueID != "" && wf != nil && wf.Issue != nil && wf.Issue.SetStatus != "" {
		runShellCommand(wf.Issue.SetStatus, map[string]string{
			"IssueID": state.IssueID,
			"Status":  "done",
		})
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

	// Update issue status to cancelled
	if state.IssueID != "" && wf != nil && wf.Issue != nil && wf.Issue.SetStatus != "" {
		runShellCommand(wf.Issue.SetStatus, map[string]string{
			"IssueID": state.IssueID,
			"Status":  "cancelled",
		})
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
