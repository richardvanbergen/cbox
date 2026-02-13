package workflow

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// FlowInit writes default workflow config into cbox.toml.
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
		if err := output.Spin("Creating issue", func() error {
			var createErr error
			issueID, createErr = runShellCommand(wf.Issue.Create, map[string]string{
				"Title":       title,
				"Description": description,
			})
			return createErr
		}); err != nil {
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
	if err := output.Spin("Starting sandbox", func() error {
		return sandbox.UpWithOptions(projectDir, branch, sandbox.UpOptions{
			ReportDir:  repDir,
			FlowBranch: branch,
		})
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
		var issueContent string
		fetchErr := output.Spin("Fetching issue content", func() error {
			var e error
			issueContent, e = runShellCommand(wf.Issue.View, map[string]string{
				"IssueID": issueID,
			})
			return e
		})
		if fetchErr != nil {
			output.Warning("Could not fetch issue content: %v", fetchErr)
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

	output.Progress("Running in yolo mode")
	if err := sandbox.ChatPrompt(projectDir, branch, prompt); err != nil {
		return fmt.Errorf("yolo execution failed: %w", err)
	}

	output.Progress("Creating PR")
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
		var issueContent string
		fetchErr := output.Spin("Refreshing task from issue", func() error {
			var e error
			issueContent, e = runShellCommand(wf.Issue.View, map[string]string{
				"IssueID": state.IssueID,
			})
			return e
		})
		if fetchErr != nil {
			output.Warning("Could not fetch issue content: %v", fetchErr)
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
	if err := output.Spin("Pushing branch", func() error {
		_, pushErr := runShellCommandInDir("git push -u origin $Branch", map[string]string{
			"Branch": branch,
		}, wtPath)
		return pushErr
	}); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	var prOutput string
	if err := output.Spin("Creating PR", func() error {
		var prErr error
		prOutput, prErr = runShellCommandInDir(wf.PR.Create, map[string]string{
			"Title":       state.Title,
			"Description": description,
		}, wtPath)
		return prErr
	}); err != nil {
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

		if err := output.Spin("Merging PR", func() error {
			_, mergeErr := runShellCommand(wf.PR.Merge, map[string]string{
				"PRURL":    state.PRURL,
				"PRNumber": prNumber,
			})
			return mergeErr
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
	if err := output.Spin("Cleaning up sandbox", func() error {
		return sandbox.CleanQuiet(projectDir, branch)
	}); err != nil {
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

// fetchPRStatus fetches the current PR status from the provider.
// Returns an error if the view command is not configured or the fetch fails.
func fetchPRStatus(wf *config.WorkflowConfig, state *FlowState) (*PRStatus, error) {
	if state.PRNumber == "" {
		return nil, nil
	}
	if wf == nil || wf.PR == nil || wf.PR.View == "" {
		return nil, fmt.Errorf("no pr.view command configured — add [workflow.pr] view to %s", config.ConfigFile)
	}

	prOutput, err := runShellCommand(wf.PR.View, map[string]string{
		"PRNumber": state.PRNumber,
		"PRURL":    state.PRURL,
	})
	if err != nil {
		return nil, fmt.Errorf("fetching PR status: %w", err)
	}

	status, err := parsePRJSON(prOutput)
	if err != nil {
		return nil, fmt.Errorf("parsing PR status: %w", err)
	}

	return status, nil
}

// FlowStatus shows the status of active flows.
func FlowStatus(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	wf := cfg.Workflow
	if wf == nil || wf.PR == nil || wf.PR.View == "" {
		return fmt.Errorf("no pr.view command configured — add [workflow.pr] view to %s", config.ConfigFile)
	}

	if branch != "" {
		state, err := LoadFlowState(projectDir, branch)
		if err != nil {
			return err
		}
		printFlowState(projectDir, wf, state)
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

	// Determine which flows need a PR status fetch
	type flowLine struct {
		state    *FlowState
		needsPR  bool
	}
	flowLines := make([]flowLine, len(states))
	anyNeedsPR := false
	for i, s := range states {
		needsPR := s.PRNumber != ""
		flowLines[i] = flowLine{state: s, needsPR: needsPR}
		if needsPR {
			anyNeedsPR = true
		}
	}

	// If no flows need PR status, just print them directly
	if !anyNeedsPR {
		for _, fl := range flowLines {
			output.Text("%-30s %-15s %s", fl.state.Branch, fl.state.Phase, fl.state.Title)
		}
		return nil
	}

	// Show all flows with spinners, fetch PR status concurrently
	spinner := output.NewLineSpinner(len(flowLines))
	for i, fl := range flowLines {
		if fl.needsPR {
			spinner.SetLine(i, fmt.Sprintf("%-30s %%s  %s", fl.state.Branch, fl.state.Title))
		} else {
			spinner.SetLine(i, fmt.Sprintf("%-30s %%s  %s", fl.state.Branch, fl.state.Title))
			spinner.Resolve(i, fmt.Sprintf("%-15s", fl.state.Phase))
		}
	}

	// Fetch PR statuses concurrently
	var wg sync.WaitGroup
	for i, fl := range flowLines {
		if !fl.needsPR {
			continue
		}
		wg.Add(1)
		go func(idx int, s *FlowState) {
			defer wg.Done()
			phase := s.Phase
			prStatus, err := fetchPRStatus(wf, s)
			if err == nil && prStatus != nil {
				phase = formatPRPhase(prStatus)
			}
			spinner.Resolve(idx, fmt.Sprintf("%-15s", phase))
		}(i, fl.state)
	}

	// Run spinner in a goroutine, wait for all fetches, then wait for spinner
	go func() {
		wg.Wait()
	}()
	spinner.Run()

	return nil
}

// FlowClean removes local resources (worktrees, containers) for flows whose PRs
// have been merged. It fetches PR status for all active flows, identifies the
// merged ones, shows the user what will be removed, and prompts for confirmation.
func FlowClean(projectDir string) error {
	return flowClean(projectDir, os.Stdin)
}

func flowClean(projectDir string, confirmReader io.Reader) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	wf := cfg.Workflow
	if wf == nil || wf.PR == nil || wf.PR.View == "" {
		return fmt.Errorf("no pr.view command configured — add [workflow.pr] view to %s", config.ConfigFile)
	}

	states, err := ListFlowStates(projectDir)
	if err != nil {
		return err
	}

	if len(states) == 0 {
		output.Text("No active flows.")
		return nil
	}

	// Fetch PR status concurrently and identify merged flows
	merged := findMergedFlows(wf, states)

	if len(merged) == 0 {
		output.Text("No merged flows to clean up.")
		return nil
	}

	// Show what will be removed
	fmt.Println()
	output.Text("The following merged flows will be cleaned up:")
	for _, s := range merged {
		output.Text("  - %s (%s)", s.Branch, s.Title)
	}
	fmt.Println()

	// Prompt for confirmation
	fmt.Print("Remove these flows? [y/N] ")
	scanner := bufio.NewScanner(confirmReader)
	if !scanner.Scan() {
		return nil
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "y" && answer != "yes" {
		output.Text("Aborted.")
		return nil
	}

	// Clean up each merged flow
	for _, s := range merged {
		branchName := s.Branch
		if err := output.Spin(fmt.Sprintf("Cleaning up %s", branchName), func() error {
			return sandbox.CleanQuiet(projectDir, branchName)
		}); err != nil {
			output.Warning("Sandbox cleanup failed for %s: %v", branchName, err)
		}

		RemoveFlowState(projectDir, branchName)
		repDir := reportDir(projectDir, branchName)
		os.RemoveAll(repDir)
	}

	output.Success("Done. Cleaned up %d merged flow(s).", len(merged))
	return nil
}

// findMergedFlows fetches PR status for all flows concurrently and returns
// those whose PRs are in the MERGED state.
func findMergedFlows(wf *config.WorkflowConfig, states []*FlowState) []*FlowState {
	type flowResult struct {
		state  *FlowState
		merged bool
	}
	results := make([]flowResult, len(states))
	var mu sync.Mutex
	var wg sync.WaitGroup

	spinner := output.NewLineSpinner(len(states))
	for i, s := range states {
		results[i] = flowResult{state: s}
		spinner.SetLine(i, fmt.Sprintf("%-30s %%s  %s", s.Branch, s.Title))

		if s.PRNumber == "" {
			spinner.Resolve(i, fmt.Sprintf("%-15s", s.Phase))
			continue
		}

		wg.Add(1)
		go func(idx int, s *FlowState) {
			defer wg.Done()
			prStatus, err := fetchPRStatus(wf, s)
			merged := err == nil && prStatus != nil && strings.ToUpper(prStatus.State) == "MERGED"
			mu.Lock()
			results[idx].merged = merged
			mu.Unlock()

			phase := s.Phase
			if err == nil && prStatus != nil {
				phase = formatPRPhase(prStatus)
			}
			spinner.Resolve(idx, fmt.Sprintf("%-15s", phase))
		}(i, s)
	}

	go func() {
		wg.Wait()
	}()
	spinner.Run()

	var merged []*FlowState
	for _, r := range results {
		if r.merged {
			merged = append(merged, r.state)
		}
	}
	return merged
}

// formatPRPhase returns a display string for the PR status.
func formatPRPhase(prStatus *PRStatus) string {
	switch strings.ToUpper(prStatus.State) {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	case "OPEN":
		return "pr-open"
	default:
		return strings.ToLower(prStatus.State)
	}
}

func printFlowState(projectDir string, wf *config.WorkflowConfig, s *FlowState) {
	output.Text("Branch:      %s", s.Branch)
	output.Text("Title:       %s", s.Title)
	if s.Description != "" {
		output.Text("Description: %s", s.Description)
	}

	// Use a spinner if we need to fetch PR status
	var fetchedPR *PRStatus
	if s.PRNumber != "" {
		spinner := output.NewLineSpinner(1)
		spinner.SetLine(0, "Phase:       %s")
		go func() {
			phase := s.Phase
			if prStatus, err := fetchPRStatus(wf, s); err == nil && prStatus != nil {
				fetchedPR = prStatus
				phase = formatPRPhase(prStatus)
			}
			spinner.Resolve(0, phase)
		}()
		spinner.Run()
	} else {
		output.Text("Phase:       %s", s.Phase)
	}

	if s.IssueID != "" {
		output.Text("Issue:       #%s", s.IssueID)
	}
	if s.PRURL != "" {
		output.Text("PR:          %s", s.PRURL)
	}

	// Show merge/close timestamps when available
	if fetchedPR != nil {
		if fetchedPR.MergedAt != "" {
			output.Text("Merged at:   %s", fetchedPR.MergedAt)
		}
		if fetchedPR.ClosedAt != "" {
			output.Text("Closed at:   %s", fetchedPR.ClosedAt)
		}
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
	if err := output.Spin("Cleaning up sandbox", func() error {
		return sandbox.CleanQuiet(projectDir, branch)
	}); err != nil {
		output.Warning("Sandbox cleanup failed: %v", err)
	}

	// Remove flow state and reports
	RemoveFlowState(projectDir, branch)
	repDir := reportDir(projectDir, branch)
	os.RemoveAll(repDir)

	output.Success("Flow '%s' abandoned.", state.Title)
	return nil
}
