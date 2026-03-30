package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/richvanbergen/cbox/internal/docker"
)

const (
	cboxInstructionsStart = "<!-- cbox-generated-claude-md:start -->"
	cboxInstructionsEnd   = "<!-- cbox-generated-claude-md:end -->"
)

func keychainPassword(service string) string {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-w").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func writeGeneratedFile(projectDir string, parts []string, filename, content string) (string, error) {
	dirParts := append([]string{projectDir, ".cbox"}, parts...)
	dir := filepath.Join(dirParts...)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating generated file dir: %w", err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("writing generated file %s: %w", path, err)
	}
	return path, nil
}

func buildInstructions(spec RuntimeSpec) string {
	return docker.BuildClaudeMD(spec.HostCommands, spec.Commands, spec.Ports)
}

func mergeWorkspaceClaudeMD(worktreePath, generated string) string {
	existingPath := filepath.Join(worktreePath, "CLAUDE.md")
	existing, err := os.ReadFile(existingPath)
	managed := cboxInstructionsStart + "\n" + strings.TrimRight(generated, "\n") + "\n" + cboxInstructionsEnd
	if err != nil || len(bytes.TrimSpace(existing)) == 0 {
		return managed + "\n"
	}

	content := string(existing)
	if start := strings.Index(content, cboxInstructionsStart); start >= 0 {
		if end := strings.Index(content[start:], cboxInstructionsEnd); end >= 0 {
			end += start + len(cboxInstructionsEnd)
			prefix := strings.TrimRight(content[:start], "\n")
			suffix := strings.TrimLeft(content[end:], "\n")
			switch {
			case prefix == "" && suffix == "":
				return managed + "\n"
			case prefix == "":
				return managed + "\n\n" + suffix
			case suffix == "":
				return prefix + "\n\n" + managed + "\n"
			default:
				return prefix + "\n\n" + managed + "\n\n" + suffix
			}
		}
	}

	return strings.TrimRight(content, "\n") + "\n\n" + managed + "\n"
}

func buildCursorMCPConfig(worktreePath string, port int) string {
	cfg := map[string]any{}
	existingPath := filepath.Join(worktreePath, ".cursor", "mcp.json")
	if existing, err := os.ReadFile(existingPath); err == nil {
		_ = json.Unmarshal(existing, &cfg)
	}

	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["cbox-host"] = map[string]any{
		"url": fmt.Sprintf("http://host.docker.internal:%d/mcp", port),
	}
	cfg["mcpServers"] = servers

	data, _ := json.MarshalIndent(cfg, "", "  ")
	return string(data) + "\n"
}
