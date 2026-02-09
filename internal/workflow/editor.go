package workflow

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const editorTemplate = `
# Enter your flow description above.
# Lines starting with '#' will be ignored.
# An empty description aborts the flow.
`

// EditDescription opens an editor for the user to write a flow description.
// It resolves the editor using: CBOX_EDITOR > editorFromConfig > VISUAL > EDITOR.
func EditDescription(editorFromConfig string) (string, error) {
	editor := resolveEditor(editorFromConfig)
	if editor == "" {
		return "", fmt.Errorf("no editor found: set CBOX_EDITOR, VISUAL, or EDITOR env var, or add editor to .cbox.toml")
	}

	tmpFile, err := os.CreateTemp("", "cbox-description-*.txt")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(editorTemplate); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("writing template: %w", err)
	}
	tmpFile.Close()

	cmd := exec.Command(editor, tmpFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("reading temp file: %w", err)
	}

	desc := stripComments(string(content))
	if desc == "" {
		return "", fmt.Errorf("aborting: empty description")
	}

	return desc, nil
}

// resolveEditor returns the editor command to use, checking in priority order.
func resolveEditor(editorFromConfig string) string {
	if v := os.Getenv("CBOX_EDITOR"); v != "" {
		return v
	}
	if editorFromConfig != "" {
		return editorFromConfig
	}
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if v := os.Getenv("EDITOR"); v != "" {
		return v
	}
	return ""
}

// stripComments removes lines starting with '#' and trims whitespace.
func stripComments(s string) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
