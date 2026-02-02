package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/richvanbergen/cbox/internal/bridge"
)

const StateFile = ".cbox.state.json"

type State struct {
	ClaudeContainer string `json:"claude_container"`
	AppContainer    string `json:"app_container"`
	NetworkName     string `json:"network_name"`
	WorktreePath    string `json:"worktree_path"`
	Branch          string `json:"branch"`
	ClaudeImage     string `json:"claude_image"`
	AppImage        string `json:"app_image"`
	ProjectDir      string                `json:"project_dir"`
	BridgeProxyPID  int                   `json:"bridge_proxy_pid,omitempty"`
	BridgeMappings  []bridge.ProxyMapping `json:"bridge_mappings,omitempty"`
	MCPProxyPID     int                   `json:"mcp_proxy_pid,omitempty"`
	MCPProxyPort    int                   `json:"mcp_proxy_port,omitempty"`
}

func LoadState(projectDir string) (*State, error) {
	path := filepath.Join(projectDir, StateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no active sandbox (missing %s): %w", StateFile, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}
	return &s, nil
}

func SaveState(projectDir string, s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	path := filepath.Join(projectDir, StateFile)
	return os.WriteFile(path, data, 0644)
}

func RemoveState(projectDir string) error {
	path := filepath.Join(projectDir, StateFile)
	return os.Remove(path)
}
