package workflow

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/richvanbergen/cbox/internal/config"
	"github.com/richvanbergen/cbox/internal/output"
	"github.com/richvanbergen/cbox/internal/sandbox"
	"github.com/richvanbergen/cbox/internal/worktree"
)

// FlowNew bootstraps a new task: polishes the description, creates a branch,
// starts a sandbox, and writes .cbox/task.json with phase "new".
func FlowNew(projectDir, roughDesc string) error {
	cfg, err := config.Load(projectDir)
	if err != nil {
		return err
	}

	wf := cfg.Workflow
	if wf == nil {
		return fmt.Errorf("no workflow config — run 'cbox flow init' first")
	}

	// Step 1: Polish the rough description into a title + description
	title, description := polishTask(roughDesc)

	// Step 2: Accept / Edit / Regenerate loop
	title, description, err = confirmTask(title, description, roughDesc, cfg.Editor)
	if err != nil {
		return err
	}

	// Step 3: Slugify the title → branch name
	slug := slugify(title)
	branchTmpl := "$Slug"
	if wf.Branch != "" {
		branchTmpl = wf.Branch
	}
	branch := expandVars(branchTmpl, map[string]string{"Slug": slug})

	// Resolve branch name conflicts
	branch, slug = resolveBranchConflict(projectDir, branch, slug)

	// Check if task.json already exists in the target worktree
	wtPath := worktree.WorktreePath(projectDir, branch)
	if TaskExists(wtPath) {
		return fmt.Errorf("task already exists for branch %q — use 'cbox flow shape %s' to continue", branch, branch)
	}

	// Step 4: Start sandbox (creates worktree + container)
	repDir := reportDir(projectDir, branch)
	if err := output.Spin("Starting sandbox", func() error {
		return sandbox.UpWithOptions(projectDir, branch, sandbox.UpOptions{
			ReportDir:  repDir,
			FlowBranch: branch,
		})
	}); err != nil {
		return fmt.Errorf("starting sandbox: %w", err)
	}

	// Get container info from sandbox state
	sandboxState, err := sandbox.LoadState(projectDir, branch)
	if err != nil {
		return fmt.Errorf("loading sandbox state: %w", err)
	}

	// Step 5: Create and write task file
	task := NewTask(slug, branch, title, description)
	task.Container = sandboxState.ClaudeContainer
	if err := SaveTask(sandboxState.WorktreePath, task); err != nil {
		return fmt.Errorf("writing task file: %w", err)
	}

	output.Success("Task created on branch '%s'.", branch)
	output.Text("Next: run 'cbox flow shape %s' to begin planning.", branch)
	return nil
}

// polishTask uses an LLM to generate a polished title and description
// from the user's rough input, falling back to simple summarization.
func polishTask(roughDesc string) (title, description string) {
	title, description = llmPolishTask(roughDesc)
	if title != "" && description != "" {
		return title, description
	}
	// Fallback: summarize for title, use rough desc as description
	return summarize(roughDesc), roughDesc
}

// llmPolishTask calls Claude to produce a polished title and description.
func llmPolishTask(roughDesc string) (string, string) {
	prompt := fmt.Sprintf(
		`Given this rough task description, generate a polished title (under 70 characters) and a clear, detailed description.

Rough input: %q

Reply in exactly this format (no extra text):
TITLE: <your title here>
DESCRIPTION: <your description here>`,
		roughDesc,
	)
	cmd := exec.Command("claude", "-p", prompt, "--model", "claude-haiku-4-5-20251001")
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	return parseTitleDescription(string(out))
}

// parseTitleDescription extracts TITLE: and DESCRIPTION: from LLM output.
func parseTitleDescription(s string) (title, description string) {
	s = strings.TrimSpace(s)

	titleIdx := strings.Index(s, "TITLE:")
	if titleIdx < 0 {
		return "", ""
	}
	rest := s[titleIdx+len("TITLE:"):]

	descIdx := strings.Index(rest, "DESCRIPTION:")
	if descIdx >= 0 {
		title = strings.TrimSpace(rest[:descIdx])
		description = strings.TrimSpace(rest[descIdx+len("DESCRIPTION:"):])
	} else {
		title = strings.TrimSpace(rest)
	}
	return title, description
}

const taskEditTemplate = `

# -- Edit the task above --
# First line: task title
# Everything after the blank line: description
# Lines starting with '#' are ignored
# Leave empty to cancel
`

// confirmTask shows the polished title/description and prompts for
// Accept / Edit / Regenerate. Returns the final title and description.
func confirmTask(title, desc, roughDesc, editorCfg string) (string, string, error) {
	for {
		fmt.Println()
		output.Text("Title: %s", title)
		fmt.Println()
		output.Text("%s", desc)
		fmt.Println()

		fmt.Print("[A]ccept  [E]dit  [R]egenerate: ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", "", fmt.Errorf("reading input: %w", err)
		}

		switch strings.TrimSpace(strings.ToLower(input)) {
		case "a", "accept", "":
			return title, desc, nil
		case "e", "edit":
			newTitle, newDesc, editErr := editTitleDescription(title, desc, editorCfg)
			if editErr != nil {
				output.Warning("Editor error: %v — try again.", editErr)
				continue
			}
			return newTitle, newDesc, nil
		case "r", "regenerate":
			title, desc = polishTask(roughDesc)
			// Loop back to display
		default:
			output.Warning("Invalid choice. Enter A, E, or R.")
		}
	}
}

// editTitleDescription opens an editor with the title and description,
// and parses the result back.
func editTitleDescription(title, desc, editorCfg string) (string, string, error) {
	editor := resolveEditor(editorCfg)
	if editor == "" {
		return "", "", fmt.Errorf("no editor found: set CBOX_EDITOR, VISUAL, or EDITOR env var, or add editor to cbox.toml")
	}

	content := title + "\n\n" + desc + taskEditTemplate

	tmpFile, err := os.CreateTemp("", "cbox-task-*.txt")
	if err != nil {
		return "", "", fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return "", "", fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command(editor, tmpFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return "", "", fmt.Errorf("reading temp file: %w", err)
	}

	text := stripComments(string(data))
	if text == "" {
		return "", "", fmt.Errorf("empty content after editing")
	}

	// First line is the title, everything after the first blank line is the description
	parts := strings.SplitN(text, "\n\n", 2)
	newTitle := strings.TrimSpace(parts[0])
	var newDesc string
	if len(parts) > 1 {
		newDesc = strings.TrimSpace(parts[1])
	}
	if newTitle == "" {
		return "", "", fmt.Errorf("empty title after editing")
	}

	return newTitle, newDesc, nil
}

// branchExists checks if a local git branch exists in the project.
func branchExists(projectDir, branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branch)
	cmd.Dir = projectDir
	return cmd.Run() == nil
}

// resolveBranchConflict appends a numeric suffix if the branch name
// already exists in the project.
func resolveBranchConflict(projectDir, branch, slug string) (string, string) {
	if !branchExists(projectDir, branch) {
		return branch, slug
	}

	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%d", branch, i)
		if !branchExists(projectDir, candidate) {
			return candidate, fmt.Sprintf("%s-%d", slug, i)
		}
	}

	return branch, slug
}
