package workflow

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
)

// slugify converts a title to a branch-safe slug.
func slugify(title string) string {
	s := strings.ToLower(title)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
		s = strings.TrimRight(s, "-")
	}
	return s
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
