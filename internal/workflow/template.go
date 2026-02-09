package workflow

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// slugify converts a title to a short branch-safe slug using an LLM,
// falling back to a simple mechanical truncation if that fails.
func slugify(title string) string {
	if name := llmSlugify(title); name != "" {
		return name
	}
	return fallbackSlugify(title)
}

func llmSlugify(title string) string {
	prompt := fmt.Sprintf(
		`Generate a short git branch name (2-4 words, lowercase, hyphen-separated) for this task: %q. Reply with ONLY the branch name, nothing else.`,
		title,
	)
	cmd := exec.Command("claude", "-p", prompt, "--model", "claude-haiku-4-5-20251001")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))
	// Sanitize: only allow lowercase alphanumeric and hyphens
	name = strings.ToLower(name)
	name = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(name, "")
	name = strings.Trim(name, "-")
	if name == "" || len(name) > 40 {
		return ""
	}
	return name
}

func fallbackSlugify(title string) string {
	s := strings.ToLower(title)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	// Take first 3 hyphen-separated words
	parts := strings.SplitN(s, "-", 4)
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return strings.Join(parts, "-")
}

// summarize generates a short title (under 70 chars) from a longer description
// using an LLM, falling back to simple truncation.
func summarize(description string) string {
	if title := llmSummarize(description); title != "" {
		return title
	}
	return fallbackSummarize(description)
}

func llmSummarize(description string) string {
	prompt := fmt.Sprintf(
		`Summarize this task as a short issue title (under 70 characters, no quotes): %q. Reply with ONLY the title, nothing else.`,
		description,
	)
	cmd := exec.Command("claude", "-p", prompt, "--model", "claude-haiku-4-5-20251001")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	title := strings.TrimSpace(string(out))
	if title == "" || len(title) > 70 {
		return ""
	}
	return title
}

func fallbackSummarize(description string) string {
	if len(description) <= 70 {
		return description
	}
	// Truncate at last space before 70 chars
	s := description[:70]
	if i := strings.LastIndex(s, " "); i > 20 {
		s = s[:i]
	}
	return s
}

// expandVars expands $VarName references in a string using the provided data map.
// Unknown variables are left unexpanded.
func expandVars(s string, data map[string]string) string {
	return os.Expand(s, func(key string) string {
		if v, ok := data[key]; ok {
			return v
		}
		return "${" + key + "}"
	})
}

// runShellCommand executes a shell command with template data passed as
// environment variables. Commands reference values with $VarName which the
// shell expands — safe for values containing metacharacters (backticks,
// quotes, etc.).
// Returns the trimmed stdout output.
func runShellCommand(cmdStr string, data map[string]string) (string, error) {
	return runShellCommandInDir(cmdStr, data, "")
}

// runShellCommandInDir is like runShellCommand but executes in the given directory.
func runShellCommandInDir(cmdStr string, data map[string]string, dir string) (string, error) {
	cmd := exec.Command("sh", "-c", cmdStr)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Env = os.Environ()
	for k, v := range data {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())

	if err != nil {
		// Include both stdout and stderr in the error message for debugging
		combined := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		return output, fmt.Errorf("command %q failed: %s: %w", cmdStr, combined, err)
	}

	return output, nil
}
