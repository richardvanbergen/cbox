package workflow

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/richvanbergen/cbox/internal/config"
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

// FlowPR creates a pull request for the flow.
func FlowPR(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	wf := cfg.Workflow
	if wf == nil || wf.PR == nil || wf.PR.Create == "" {
		return fmt.Errorf("no PR create command configured")
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
		return fmt.Errorf("task is already done — cannot create PR")
	}

	// Build PR description from reports, falling back to task description
	description := task.Description
	repDir := reportDir(projectDir, branch)
	reports, _ := hostcmd.LoadReports(repDir)
	for _, r := range reports {
		if r.Type == "done" {
			description = r.Body
		}
	}

	title := task.Title
	if title == "" {
		title = branch
	}
	if description == "" {
		description = title
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

	var prURL, prNumber string
	prExisted := false
	if err := output.Spin("Creating PR", func() error {
		prOut, prErr := runShellCommandInDir(wf.PR.Create, map[string]string{
			"Title":       title,
			"Description": description,
		}, wtPath)
		if prErr != nil {
			// Check if PR already exists — gh includes the URL in the error
			errMsg := prErr.Error()
			if strings.Contains(errMsg, "already exists") {
				url, num, parseErr := parsePROutput(errMsg)
				if parseErr == nil {
					prURL = url
					prNumber = num
					prExisted = true
					return nil
				}
			}
			return prErr
		}
		url, num, parseErr := parsePROutput(prOut)
		if parseErr != nil {
			prURL = prOut
		} else {
			prURL = url
			prNumber = num
		}
		return nil
	}); err != nil {
		return fmt.Errorf("creating PR: %w", err)
	}

	// Save PR info and advance to verification in one save
	task.PRURL = prURL
	task.PRNumber = prNumber
	shouldAdvance := task.Phase != PhaseVerification && task.Phase != PhaseDone
	if shouldAdvance {
		task.Phase = PhaseVerification
	}
	if err := SaveTask(wtPath, task); err != nil {
		output.Warning("Could not save task: %v", err)
	}

	// Fire memory sync separately (best-effort)
	if shouldAdvance {
		syncMemory(task, wf)
	}

	if prExisted {
		output.Success("PR already exists: %s", prURL)
	} else {
		output.Success("PR created: %s", prURL)
	}
	if task.Phase == PhaseVerification {
		output.Text("Next: review the PR, then run 'cbox flow verify pass %s' or 'cbox flow verify fail %s --reason \"...\"'", branch, branch)
	}
	return nil
}

// FlowMerge merges the PR and cleans up.
func FlowMerge(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	wf := cfg.Workflow

	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("no flow found for branch %q", branch)
	}

	task, err := LoadTask(sandboxState.WorktreePath)
	if err != nil {
		return fmt.Errorf("loading task: %w", err)
	}

	// Enforce verification gate
	if err := checkMergeGate(sandboxState.WorktreePath); err != nil {
		return err
	}

	// Try to detect PR if not stored in task
	if task.PRURL == "" {
		prStatus, lookupErr := lookupBranchPR(wf, sandboxState.WorktreePath, branch)
		if lookupErr != nil || prStatus == nil {
			return fmt.Errorf("no PR found for branch %q — run 'cbox flow pr %s' first", branch, branch)
		}
		task.PRURL = prStatus.URL
		task.PRNumber = prStatus.Number
		if err := SaveTask(sandboxState.WorktreePath, task); err != nil {
			output.Warning("Could not save detected PR info: %v", err)
		}
	}

	// Merge PR
	if wf != nil && wf.PR != nil && wf.PR.Merge != "" {
		prNumber := task.PRNumber
		if prNumber == "" {
			_, extracted, _ := parsePROutput(task.PRURL)
			prNumber = extracted
		}

		if err := output.Spin("Merging PR", func() error {
			_, mergeErr := runShellCommand(wf.PR.Merge, map[string]string{
				"PRURL":    task.PRURL,
				"PRNumber": prNumber,
			})
			return mergeErr
		}); err != nil {
			return fmt.Errorf("merging PR: %w", err)
		}
	} else {
		output.Warning("No PR merge command configured — merge manually.")
	}

	// Close issue via memory sync
	if task.MemoryRef != "" && wf != nil && wf.Issue != nil {
		if wf.Issue.SetStatus != "" {
			runShellCommand(wf.Issue.SetStatus, map[string]string{
				"IssueID": task.MemoryRef,
				"Status":  "done",
			})
		}
		if wf.Issue.Close != "" {
			runShellCommand(wf.Issue.Close, map[string]string{
				"IssueID": task.MemoryRef,
			})
		}
	}

	// Clean up sandbox
	if err := output.Spin("Cleaning up sandbox", func() error {
		return sandbox.CleanQuiet(projectDir, branch)
	}); err != nil {
		output.Warning("Sandbox cleanup failed: %v", err)
	}

	// Remove reports
	repDir := reportDir(projectDir, branch)
	os.RemoveAll(repDir)

	output.Success("Flow complete.")
	return nil
}

// fetchTaskPRStatus fetches the current PR status for a task.
func fetchTaskPRStatus(wf *config.WorkflowConfig, task *Task) (*PRStatus, error) {
	if task.PRNumber == "" {
		return nil, nil
	}
	if wf == nil || wf.PR == nil || wf.PR.View == "" {
		return nil, fmt.Errorf("no pr.view command configured — add [workflow.pr] view to %s", config.ConfigFile)
	}

	prOutput, err := runShellCommand(wf.PR.View, map[string]string{
		"PRNumber": task.PRNumber,
		"PRURL":    task.PRURL,
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

// lookupBranchPR tries to discover PR info for a branch by running the
// view command from the worktree directory (where tools like gh can
// auto-detect the PR from the current branch).
func lookupBranchPR(wf *config.WorkflowConfig, wtPath, branch string) (*PRStatus, error) {
	if wf == nil || wf.PR == nil || wf.PR.View == "" {
		return nil, fmt.Errorf("no pr.view command configured")
	}

	// Pass the branch name as PRNumber — gh accepts branch names too
	prOutput, err := runShellCommandInDir(wf.PR.View, map[string]string{
		"PRNumber": branch,
		"PRURL":    "",
	}, wtPath)
	if err != nil {
		return nil, fmt.Errorf("looking up PR for branch %q: %w", branch, err)
	}

	status, err := parsePRJSON(prOutput)
	if err != nil {
		return nil, fmt.Errorf("parsing PR info: %w", err)
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

	if branch != "" {
		sandboxState, err := sandbox.LoadState(projectDir, branch)
		if err != nil {
			return fmt.Errorf("no flow found for branch %q", branch)
		}
		task, err := LoadTask(sandboxState.WorktreePath)
		if err != nil {
			return fmt.Errorf("no flow found for branch %q", branch)
		}
		PrintTaskStatus(task)
		if sandboxState.ServeURL != "" {
			output.Text("Serve URL:   %s", sandboxState.ServeURL)
		}
		return nil
	}

	// List all flows via sandbox states
	sandboxStates, err := sandbox.ListStates(projectDir)
	if err != nil {
		return err
	}

	type statusLine struct {
		task     *Task
		needsPR  bool
		serveURL string
	}

	var lines []statusLine
	for _, ss := range sandboxStates {
		task, taskErr := LoadTask(ss.WorktreePath)
		if taskErr != nil {
			continue // sandbox without task.json — not a managed flow
		}
		needsPR := task.PRNumber != "" && wf != nil && wf.PR != nil && wf.PR.View != ""
		lines = append(lines, statusLine{task: task, needsPR: needsPR, serveURL: ss.ServeURL})
	}

	if len(lines) == 0 {
		output.Text("No active flows.")
		return nil
	}

	// Check if any need PR status fetch
	anyNeedsPR := false
	for _, l := range lines {
		if l.needsPR {
			anyNeedsPR = true
			break
		}
	}

	// If no flows need PR status, just print them directly
	if !anyNeedsPR {
		for _, l := range lines {
			line := fmt.Sprintf("%-30s %-15s %s", l.task.Branch, l.task.Phase, l.task.Title)
			if l.serveURL != "" {
				line += fmt.Sprintf("  %s", l.serveURL)
			}
			output.Text("%s", line)
		}
		return nil
	}

	// Show all flows with spinners, fetch PR status concurrently
	spinner := output.NewLineSpinner(len(lines))
	for i, l := range lines {
		suffix := l.task.Title
		if l.serveURL != "" {
			suffix += fmt.Sprintf("  %s", l.serveURL)
		}
		spinner.SetLine(i, fmt.Sprintf("%-30s %%s  %s", l.task.Branch, suffix))
		if !l.needsPR {
			spinner.Resolve(i, fmt.Sprintf("%-15s", l.task.Phase))
		}
	}

	var wg sync.WaitGroup
	for i, l := range lines {
		if !l.needsPR {
			continue
		}
		wg.Add(1)
		go func(idx int, t *Task) {
			defer wg.Done()
			phase := string(t.Phase)
			prStatus, err := fetchTaskPRStatus(wf, t)
			if err == nil && prStatus != nil {
				phase = formatPRPhase(prStatus)
			}
			spinner.Resolve(idx, fmt.Sprintf("%-15s", phase))
		}(i, l.task)
	}

	go func() {
		wg.Wait()
	}()
	spinner.Run()

	return nil
}

// FlowClean removes local resources (worktrees, containers) for flows whose PRs
// have been merged.
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

	sandboxStates, err := sandbox.ListStates(projectDir)
	if err != nil {
		return err
	}

	// Collect tasks with PRs
	var flows []flowEntry
	for _, ss := range sandboxStates {
		task, taskErr := LoadTask(ss.WorktreePath)
		if taskErr != nil {
			continue
		}
		flows = append(flows, flowEntry{task: task, branch: ss.Branch})
	}

	if len(flows) == 0 {
		output.Text("No active flows.")
		return nil
	}

	// Find merged flows
	merged := findMergedTasks(wf, flows)

	if len(merged) == 0 {
		output.Text("No merged flows to clean up.")
		return nil
	}

	// Show what will be removed
	fmt.Println()
	output.Text("The following merged flows will be cleaned up:")
	for _, f := range merged {
		output.Text("  - %s (%s)", f.branch, f.task.Title)
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
	for _, f := range merged {
		if err := output.Spin(fmt.Sprintf("Cleaning up %s", f.branch), func() error {
			return sandbox.CleanQuiet(projectDir, f.branch)
		}); err != nil {
			output.Warning("Sandbox cleanup failed for %s: %v", f.branch, err)
		}

		repDir := reportDir(projectDir, f.branch)
		os.RemoveAll(repDir)
	}

	output.Success("Done. Cleaned up %d merged flow(s).", len(merged))
	return nil
}

type flowEntry struct {
	task   *Task
	branch string
}

// findMergedTasks fetches PR status for all flows concurrently and returns
// those whose PRs are in the MERGED state.
func findMergedTasks(wf *config.WorkflowConfig, flows []flowEntry) []flowEntry {
	if len(flows) == 0 {
		return nil
	}

	type flowResult struct {
		entry  flowEntry
		merged bool
	}
	results := make([]flowResult, len(flows))
	var mu sync.Mutex
	var wg sync.WaitGroup

	spinner := output.NewLineSpinner(len(flows))
	for i, f := range flows {
		results[i] = flowResult{entry: f}
		spinner.SetLine(i, fmt.Sprintf("%-30s %%s  %s", f.branch, f.task.Title))

		if f.task.PRNumber == "" {
			spinner.Resolve(i, fmt.Sprintf("%-15s", f.task.Phase))
			continue
		}

		wg.Add(1)
		go func(idx int, t *Task) {
			defer wg.Done()
			prStatus, err := fetchTaskPRStatus(wf, t)
			merged := err == nil && prStatus != nil && strings.ToUpper(prStatus.State) == "MERGED"
			mu.Lock()
			results[idx].merged = merged
			mu.Unlock()

			phase := string(t.Phase)
			if err == nil && prStatus != nil {
				phase = formatPRPhase(prStatus)
			}
			spinner.Resolve(idx, fmt.Sprintf("%-15s", phase))
		}(i, f.task)
	}

	go func() {
		wg.Wait()
	}()
	spinner.Run()

	var merged []flowEntry
	for _, r := range results {
		if r.merged {
			merged = append(merged, r.entry)
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

// FlowAbandon cancels a flow and cleans up.
func FlowAbandon(projectDir, branch string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	wf := cfg.Workflow

	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("no flow found for branch %q", branch)
	}

	// Try to load task for issue cleanup and display
	task, _ := LoadTask(sandboxState.WorktreePath)

	// Close issue if tracked
	if task != nil && task.MemoryRef != "" && wf != nil && wf.Issue != nil {
		if wf.Issue.SetStatus != "" {
			runShellCommand(wf.Issue.SetStatus, map[string]string{
				"IssueID": task.MemoryRef,
				"Status":  "cancelled",
			})
		}
		if wf.Issue.Close != "" {
			runShellCommand(wf.Issue.Close, map[string]string{
				"IssueID": task.MemoryRef,
			})
		}
	}

	// Clean up sandbox
	if err := output.Spin("Cleaning up sandbox", func() error {
		return sandbox.CleanQuiet(projectDir, branch)
	}); err != nil {
		output.Warning("Sandbox cleanup failed: %v", err)
	}

	// Remove reports
	repDir := reportDir(projectDir, branch)
	os.RemoveAll(repDir)

	title := branch
	if task != nil && task.Title != "" {
		title = task.Title
	}
	output.Success("Flow '%s' abandoned.", title)
	return nil
}
