package workflow

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
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

// renderTemplate renders a Go template string with the given data.
func renderTemplate(tmpl string, data any) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}

// runShellCommand renders a template and executes it as a shell command.
// Returns the trimmed stdout output.
func runShellCommand(tmpl string, data any) (string, error) {
	rendered, err := renderTemplate(tmpl, data)
	if err != nil {
		return "", err
	}

	cmd := exec.Command("sh", "-c", rendered)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if err != nil {
		return output, fmt.Errorf("command %q failed: %s: %w", rendered, output, err)
	}

	return output, nil
}
