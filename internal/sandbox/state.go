package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/richvanbergen/cbox/internal/bridge"
)

const StateDir = ".cbox"

type State struct {
	ClaudeContainer string `json:"claude_container"`
	AppContainer    string `json:"app_container"`
	NetworkName     string `json:"network_name"`
	WorktreePath    string `json:"worktree_path"`
	Branch          string `json:"branch"`
	ClaudeImage     string `json:"claude_image"`
	AppImage        string `json:"app_image"`
	ProjectDir      string                `json:"project_dir"`
	Running         bool                  `json:"running"`
	BridgeProxyPID  int                   `json:"bridge_proxy_pid,omitempty"`
	BridgeMappings  []bridge.ProxyMapping `json:"bridge_mappings,omitempty"`
	MCPProxyPID     int                   `json:"mcp_proxy_pid,omitempty"`
	MCPProxyPort    int                   `json:"mcp_proxy_port,omitempty"`
}

func stateFilePath(projectDir, branch string) string {
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	return filepath.Join(projectDir, StateDir, safeBranch+".state.json")
}

func LoadState(projectDir, branch string) (*State, error) {
	path := stateFilePath(projectDir, branch)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no sandbox for branch %q (missing %s): %w", branch, path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}
	return &s, nil
}

func SaveState(projectDir, branch string, s *State) error {
	dir := filepath.Join(projectDir, StateDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	path := stateFilePath(projectDir, branch)
	return os.WriteFile(path, data, 0644)
}

func RemoveState(projectDir, branch string) error {
	path := stateFilePath(projectDir, branch)
	return os.Remove(path)
}

func ListStates(projectDir string) ([]*State, error) {
	pattern := filepath.Join(projectDir, StateDir, "*.state.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing state files: %w", err)
	}

	var states []*State
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var s State
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		states = append(states, &s)
	}
	return states, nil
}
