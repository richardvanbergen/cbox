package workflow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// PRStatus holds the state of a pull request fetched from the provider.
type PRStatus struct {
	Number   string
	State    string // e.g. "OPEN", "CLOSED", "MERGED"
	Title    string
	URL      string
	MergedAt string
	ClosedAt string
}

// parsePRJSON parses the JSON output from `gh pr view --json`.
func parsePRJSON(jsonStr string) (*PRStatus, error) {
	var raw struct {
		Number   int    `json:"number"`
		State    string `json:"state"`
		Title    string `json:"title"`
		URL      string `json:"url"`
		MergedAt string `json:"mergedAt"`
		ClosedAt string `json:"closedAt"`
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
		ClosedAt: raw.ClosedAt,
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
