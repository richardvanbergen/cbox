package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreePath returns the path for a worktree based on the project dir and branch name.
// e.g., ~/Code/myproject + feat-x → ~/Code/myproject--feat-x
func WorktreePath(projectDir, branch string) string {
	base := filepath.Base(projectDir)
	parent := filepath.Dir(projectDir)
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	return filepath.Join(parent, base+"--"+safeBranch)
}

// Create creates a new git worktree for the given branch.
// If the branch doesn't exist, it creates it.
// If the worktree already exists, it returns the existing path.
func Create(projectDir, branch string) (string, error) {
	wtPath := WorktreePath(projectDir, branch)

	// If the worktree directory already exists, reuse it.
	if info, err := os.Stat(wtPath); err == nil && info.IsDir() {
		return wtPath, nil
	}

	// Try checking out existing branch first
	cmd := exec.Command("git", "worktree", "add", wtPath, branch)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Branch doesn't exist, create it
		cmd = exec.Command("git", "worktree", "add", wtPath, "-b", branch)
		cmd.Dir = projectDir
		out, err = cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
		}
	}

	return wtPath, nil
}

// Remove removes a git worktree.
func Remove(projectDir, wtPath string) error {
	cmd := exec.Command("git", "worktree", "remove", wtPath, "--force")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// List returns the git worktree list for the given project directory.
func List(projectDir string) (string, error) {
	cmd := exec.Command("git", "worktree", "list")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git worktree list: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return strings.TrimSpace(string(out)), nil
}

// CurrentBranch returns the current git branch name.
func CurrentBranch(projectDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// DeleteBranch deletes a local branch.
func DeleteBranch(projectDir, branch string) error {
	cmd := exec.Command("git", "branch", "-D", branch)
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git branch -D: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
