package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const stateDir = ".cbox"

type FlowState struct {
	Branch      string    `json:"branch"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Phase       string    `json:"phase"`
	IssueID     string    `json:"issue_id,omitempty"`
	PRURL       string    `json:"pr_url,omitempty"`
	PRNumber    string    `json:"pr_number,omitempty"`
	AutoMode    bool      `json:"auto_mode"`
	Chatted     bool      `json:"chatted"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func flowStateFilePath(projectDir, branch string) string {
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	return filepath.Join(projectDir, stateDir, "flow-"+safeBranch+".json")
}

func LoadFlowState(projectDir, branch string) (*FlowState, error) {
	path := flowStateFilePath(projectDir, branch)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no flow for branch %q: %w", branch, err)
	}

	var s FlowState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing flow state: %w", err)
	}
	return &s, nil
}

func SaveFlowState(projectDir string, s *FlowState) error {
	dir := filepath.Join(projectDir, stateDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	s.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling flow state: %w", err)
	}

	path := flowStateFilePath(projectDir, s.Branch)
	return os.WriteFile(path, data, 0644)
}

func RemoveFlowState(projectDir, branch string) error {
	path := flowStateFilePath(projectDir, branch)
	return os.Remove(path)
}

func ListFlowStates(projectDir string) ([]*FlowState, error) {
	pattern := filepath.Join(projectDir, stateDir, "flow-*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("globbing flow state files: %w", err)
	}

	var states []*FlowState
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		var s FlowState
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		states = append(states, &s)
	}
	return states, nil
}
