package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseIssueJSON(t *testing.T) {
	input := `{
		"number": 24,
		"title": "Add structured task file",
		"body": "We need to restructure .cbox-task to YAML.",
		"state": "OPEN",
		"url": "https://github.com/owner/repo/issues/24",
		"labels": [
			{"name": "enhancement"},
			{"name": "workflow"}
		]
	}`

	info, err := parseIssueJSON(input)
	if err != nil {
		t.Fatalf("parseIssueJSON failed: %v", err)
	}

	if info.ID != "24" {
		t.Errorf("ID = %q, want %q", info.ID, "24")
	}
	if info.Title != "Add structured task file" {
		t.Errorf("Title = %q, want %q", info.Title, "Add structured task file")
	}
	if info.Body != "We need to restructure .cbox-task to YAML." {
		t.Errorf("Body = %q, want %q", info.Body, "We need to restructure .cbox-task to YAML.")
	}
	if info.State != "OPEN" {
		t.Errorf("State = %q, want %q", info.State, "OPEN")
	}
	if info.URL != "https://github.com/owner/repo/issues/24" {
		t.Errorf("URL = %q, want %q", info.URL, "https://github.com/owner/repo/issues/24")
	}
	if len(info.Labels) != 2 || info.Labels[0] != "enhancement" || info.Labels[1] != "workflow" {
		t.Errorf("Labels = %v, want [enhancement workflow]", info.Labels)
	}
}

func TestParseIssueJSON_NoLabels(t *testing.T) {
	input := `{"number": 1, "title": "Test", "body": "", "state": "OPEN", "url": "", "labels": []}`

	info, err := parseIssueJSON(input)
	if err != nil {
		t.Fatalf("parseIssueJSON failed: %v", err)
	}

	if info.ID != "1" {
		t.Errorf("ID = %q, want %q", info.ID, "1")
	}
	if info.Labels != nil {
		t.Errorf("Labels = %v, want nil", info.Labels)
	}
}

func TestParseIssueJSON_InvalidJSON(t *testing.T) {
	_, err := parseIssueJSON("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePRJSON(t *testing.T) {
	input := `{
		"number": 42,
		"state": "MERGED",
		"title": "Add feature X",
		"url": "https://github.com/owner/repo/pull/42",
		"mergedAt": "2025-01-15T10:30:00Z"
	}`

	status, err := parsePRJSON(input)
	if err != nil {
		t.Fatalf("parsePRJSON failed: %v", err)
	}

	if status.Number != "42" {
		t.Errorf("Number = %q, want %q", status.Number, "42")
	}
	if status.State != "MERGED" {
		t.Errorf("State = %q, want %q", status.State, "MERGED")
	}
	if status.Title != "Add feature X" {
		t.Errorf("Title = %q, want %q", status.Title, "Add feature X")
	}
	if status.URL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("URL = %q, want %q", status.URL, "https://github.com/owner/repo/pull/42")
	}
	if status.MergedAt != "2025-01-15T10:30:00Z" {
		t.Errorf("MergedAt = %q, want %q", status.MergedAt, "2025-01-15T10:30:00Z")
	}
}

func TestParsePRJSON_Open(t *testing.T) {
	input := `{"number": 7, "state": "OPEN", "title": "WIP", "url": "https://github.com/owner/repo/pull/7", "mergedAt": ""}`

	status, err := parsePRJSON(input)
	if err != nil {
		t.Fatalf("parsePRJSON failed: %v", err)
	}

	if status.State != "OPEN" {
		t.Errorf("State = %q, want %q", status.State, "OPEN")
	}
	if status.MergedAt != "" {
		t.Errorf("MergedAt = %q, want empty", status.MergedAt)
	}
}

func TestParsePRJSON_Closed(t *testing.T) {
	input := `{
		"number": 15,
		"state": "CLOSED",
		"title": "Abandoned feature",
		"url": "https://github.com/owner/repo/pull/15",
		"mergedAt": "",
		"closedAt": "2025-02-10T14:00:00Z"
	}`

	status, err := parsePRJSON(input)
	if err != nil {
		t.Fatalf("parsePRJSON failed: %v", err)
	}

	if status.State != "CLOSED" {
		t.Errorf("State = %q, want %q", status.State, "CLOSED")
	}
	if status.MergedAt != "" {
		t.Errorf("MergedAt = %q, want empty", status.MergedAt)
	}
	if status.ClosedAt != "2025-02-10T14:00:00Z" {
		t.Errorf("ClosedAt = %q, want %q", status.ClosedAt, "2025-02-10T14:00:00Z")
	}
}

func TestParsePRJSON_MergedWithClosedAt(t *testing.T) {
	input := `{
		"number": 42,
		"state": "MERGED",
		"title": "Add feature X",
		"url": "https://github.com/owner/repo/pull/42",
		"mergedAt": "2025-01-15T10:30:00Z",
		"closedAt": "2025-01-15T10:30:00Z"
	}`

	status, err := parsePRJSON(input)
	if err != nil {
		t.Fatalf("parsePRJSON failed: %v", err)
	}

	if status.State != "MERGED" {
		t.Errorf("State = %q, want %q", status.State, "MERGED")
	}
	if status.MergedAt != "2025-01-15T10:30:00Z" {
		t.Errorf("MergedAt = %q, want %q", status.MergedAt, "2025-01-15T10:30:00Z")
	}
	if status.ClosedAt != "2025-01-15T10:30:00Z" {
		t.Errorf("ClosedAt = %q, want %q", status.ClosedAt, "2025-01-15T10:30:00Z")
	}
}

func TestParsePRJSON_InvalidJSON(t *testing.T) {
	_, err := parsePRJSON("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePROutput(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantURL    string
		wantNumber string
		wantErr    bool
	}{
		{
			name:       "standard github URL",
			input:      "https://github.com/owner/repo/pull/42",
			wantURL:    "https://github.com/owner/repo/pull/42",
			wantNumber: "42",
		},
		{
			name:       "URL with trailing newline",
			input:      "https://github.com/owner/repo/pull/7\n",
			wantURL:    "https://github.com/owner/repo/pull/7",
			wantNumber: "7",
		},
		{
			name:       "URL with extra output before",
			input:      "some warning text\nhttps://github.com/owner/repo/pull/123",
			wantURL:    "https://github.com/owner/repo/pull/123",
			wantNumber: "123",
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no URL",
			input:   "something without a number",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, number, err := parsePROutput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
			if number != tt.wantNumber {
				t.Errorf("number = %q, want %q", number, tt.wantNumber)
			}
		})
	}
}

func TestWriteAndLoadTaskFile(t *testing.T) {
	dir := t.TempDir()

	tf := &TaskFile{
		Task: TaskInfo{
			Title:       "Fix the bug",
			Description: "It crashes on startup",
		},
		Issue: &IssueInfo{
			ID:     "42",
			Title:  "Fix the bug",
			Body:   "It crashes on startup",
			State:  "OPEN",
			Labels: []string{"bug", "critical"},
			URL:    "https://github.com/owner/repo/issues/42",
		},
		PR: &PRInfo{
			Number: "7",
			URL:    "https://github.com/owner/repo/pull/7",
		},
	}

	if err := writeStructuredTaskFile(dir, tf); err != nil {
		t.Fatalf("writeStructuredTaskFile failed: %v", err)
	}

	// Verify file exists
	path := filepath.Join(dir, taskFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading task file: %v", err)
	}
	content := string(data)

	// Check header comment
	if content[:1] != "#" {
		t.Error("expected file to start with comment header")
	}

	// Round-trip
	loaded, err := loadTaskFile(dir)
	if err != nil {
		t.Fatalf("loadTaskFile failed: %v", err)
	}

	if loaded.Task.Title != tf.Task.Title {
		t.Errorf("Task.Title = %q, want %q", loaded.Task.Title, tf.Task.Title)
	}
	if loaded.Task.Description != tf.Task.Description {
		t.Errorf("Task.Description = %q, want %q", loaded.Task.Description, tf.Task.Description)
	}
	if loaded.Issue == nil {
		t.Fatal("Issue is nil")
	}
	if loaded.Issue.ID != "42" {
		t.Errorf("Issue.ID = %q, want %q", loaded.Issue.ID, "42")
	}
	if len(loaded.Issue.Labels) != 2 {
		t.Errorf("Issue.Labels = %v, want 2 labels", loaded.Issue.Labels)
	}
	if loaded.PR == nil {
		t.Fatal("PR is nil")
	}
	if loaded.PR.Number != "7" {
		t.Errorf("PR.Number = %q, want %q", loaded.PR.Number, "7")
	}
}

func TestWriteTaskFile_NoPR(t *testing.T) {
	dir := t.TempDir()

	tf := &TaskFile{
		Task: TaskInfo{
			Title: "Simple task",
		},
		Issue: &IssueInfo{
			ID:    "10",
			Title: "Simple task",
		},
	}

	if err := writeStructuredTaskFile(dir, tf); err != nil {
		t.Fatalf("writeStructuredTaskFile failed: %v", err)
	}

	loaded, err := loadTaskFile(dir)
	if err != nil {
		t.Fatalf("loadTaskFile failed: %v", err)
	}

	if loaded.PR != nil {
		t.Error("expected PR to be nil")
	}
	if loaded.Issue == nil || loaded.Issue.ID != "10" {
		t.Error("expected Issue.ID to be 10")
	}
}
