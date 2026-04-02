package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/richvanbergen/cbox/internal/backend"
	"github.com/richvanbergen/cbox/internal/bridge"
)

const StateDir = ".cbox"

type State struct {
	Backend          string                `json:"backend,omitempty"`
	RuntimeContainer string                `json:"runtime_container,omitempty"`
	NetworkName      string                `json:"network_name"`
	WorktreePath     string                `json:"worktree_path"`
	Branch           string                `json:"branch"`
	RuntimeImage     string                `json:"runtime_image,omitempty"`
	ProjectDir       string                `json:"project_dir"`
	Running          bool                  `json:"running"`
	BridgeProxyPID   int                   `json:"bridge_proxy_pid,omitempty"`
	BridgeMappings   []bridge.ProxyMapping `json:"bridge_mappings,omitempty"`
	MCPProxyPID      int                   `json:"mcp_proxy_pid,omitempty"`
	MCPProxyPort     int                   `json:"mcp_proxy_port,omitempty"`
	Ports            []string              `json:"ports,omitempty"`
	ServePID         int                   `json:"serve_pid,omitempty"`
	ServePort        int                   `json:"serve_port,omitempty"`
	ServeURL         string                `json:"serve_url,omitempty"`

	SourceBranch string `json:"source_branch,omitempty"`

	ClaudeContainer string `json:"claude_container,omitempty"`
	ClaudeImage     string `json:"claude_image,omitempty"`
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
	s.Normalize()
	return &s, nil
}

func SaveState(projectDir, branch string, s *State) error {
	dir := filepath.Join(projectDir, StateDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	s.Normalize()
	s.ClaudeContainer = ""
	s.ClaudeImage = ""

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
		s.Normalize()
		states = append(states, &s)
	}
	return states, nil
}

func (s *State) Normalize() {
	if s.Backend == "" {
		s.Backend = string(backend.Claude)
	}
	if s.RuntimeContainer == "" {
		s.RuntimeContainer = s.ClaudeContainer
	}
	if s.RuntimeImage == "" {
		s.RuntimeImage = s.ClaudeImage
	}
}
