package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// TaskFile is the structured representation of a .cbox-task file.
type TaskFile struct {
	Task  TaskInfo   `yaml:"task"`
	Issue *IssueInfo `yaml:"issue,omitempty"`
	PR    *PRInfo    `yaml:"pr,omitempty"`
}

// TaskInfo holds the top-level task description.
type TaskInfo struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description,omitempty"`
}

// IssueInfo holds structured issue data.
type IssueInfo struct {
	ID     string   `yaml:"id"`
	Title  string   `yaml:"title,omitempty"`
	Body   string   `yaml:"body,omitempty"`
	State  string   `yaml:"state,omitempty"`
	Labels []string `yaml:"labels,omitempty"`
	URL    string   `yaml:"url,omitempty"`
}

// PRInfo holds pull request metadata.
type PRInfo struct {
	Number string `yaml:"number"`
	URL    string `yaml:"url,omitempty"`
	State  string `yaml:"state,omitempty"`
}

const taskFileName = ".cbox-task"

// writeStructuredTaskFile marshals a TaskFile to YAML and writes it to the worktree.
func writeStructuredTaskFile(worktreePath string, tf *TaskFile) error {
	data, err := yaml.Marshal(tf)
	if err != nil {
		return fmt.Errorf("marshaling task file: %w", err)
	}

	content := "# This file is managed by cbox. Do not edit manually.\n" + string(data)
	path := filepath.Join(worktreePath, taskFileName)
	return os.WriteFile(path, []byte(content), 0644)
}

// loadTaskFile reads and parses a .cbox-task YAML file from the worktree.
func loadTaskFile(worktreePath string) (*TaskFile, error) {
	path := filepath.Join(worktreePath, taskFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading task file: %w", err)
	}

	var tf TaskFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parsing task file: %w", err)
	}
	return &tf, nil
}

// parseIssueJSON parses the JSON output from `gh issue view --json`.
func parseIssueJSON(jsonStr string) (*IssueInfo, error) {
	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		URL    string `json:"url"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("parsing issue JSON: %w", err)
	}

	info := &IssueInfo{
		ID:    fmt.Sprintf("%d", raw.Number),
		Title: raw.Title,
		Body:  raw.Body,
		State: raw.State,
		URL:   raw.URL,
	}

	for _, l := range raw.Labels {
		info.Labels = append(info.Labels, l.Name)
	}

	return info, nil
}

// PRStatus holds the state of a pull request fetched from the provider.
type PRStatus struct {
	Number   string
	State    string // e.g. "OPEN", "CLOSED", "MERGED"
	Title    string
	URL      string
	MergedAt string
}

// parsePRJSON parses the JSON output from `gh pr view --json`.
func parsePRJSON(jsonStr string) (*PRStatus, error) {
	var raw struct {
		Number   int    `json:"number"`
		State    string `json:"state"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		MergedAt string `json:"mergedAt"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("parsing PR JSON: %w", err)
	}

	return &PRStatus{
		Number:   fmt.Sprintf("%d", raw.Number),
		State:    raw.State,
		Title:    raw.Title,
		URL:      raw.URL,
		MergedAt: raw.MergedAt,
	}, nil
}

// parsePROutput extracts PR URL and number from a `gh pr create` URL.
// The URL is expected to be like https://github.com/owner/repo/pull/123.
func parsePROutput(output string) (url, number string, err error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", "", fmt.Errorf("empty PR output")
	}

	// Find a URL in the output
	re := regexp.MustCompile(`https://github\.com/[^\s]+/pull/(\d+)`)
	matches := re.FindStringSubmatch(output)
	if matches == nil {
		// Fall back: treat the whole output as a URL and try to extract a trailing number
		reFallback := regexp.MustCompile(`/(\d+)\s*$`)
		fb := reFallback.FindStringSubmatch(output)
		if fb != nil {
			return output, fb[1], nil
		}
		return output, "", fmt.Errorf("could not extract PR number from: %s", output)
	}

	return matches[0], matches[1], nil
}
