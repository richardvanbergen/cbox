package worktree

import (
	"fmt"
	"io"
	"io/fs"
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

// CopyFiles copies a list of files or directories from projectDir to wtPath.
// Each pattern is relative to projectDir. Missing source files are silently
// skipped so that optional entries like ".env" don't cause errors.
func CopyFiles(projectDir, wtPath string, patterns []string) error {
	for _, pattern := range patterns {
		src := filepath.Join(projectDir, pattern)
		dst := filepath.Join(wtPath, pattern)

		info, err := os.Stat(src)
		if err != nil {
			// Source doesn't exist — skip silently.
			continue
		}

		if info.IsDir() {
			if err := copyDir(src, dst); err != nil {
				return fmt.Errorf("copying directory %s: %w", pattern, err)
			}
		} else {
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("copying file %s: %w", pattern, err)
			}
		}
	}
	return nil
}

// copyFile copies a single file from src to dst, preserving permissions.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// copyDir recursively copies a directory tree from src to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}
