package workflow

import (
	"testing"
)

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
	if status.MergedAt != "2025-01-15T10:30:00Z" {
		t.Errorf("MergedAt = %q, want %q", status.MergedAt, "2025-01-15T10:30:00Z")
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
