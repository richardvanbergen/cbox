package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const ConfigFile = "cbox.toml"
const LegacyConfigFile = ".cbox.toml"

type Config struct {
	Commands     map[string]string `toml:"commands,omitempty"`
	Env          []string          `toml:"env,omitempty"`
	EnvFile      string            `toml:"env_file,omitempty"`
	Browser      bool              `toml:"browser,omitempty"`
	HostCommands []string          `toml:"host_commands,omitempty"`
	CopyFiles    []string          `toml:"copy_files,omitempty"`
	Ports        []string          `toml:"ports,omitempty"`
	Dockerfile   string            `toml:"dockerfile,omitempty"`
	Open         string            `toml:"open,omitempty"`
	Editor       string            `toml:"editor,omitempty"`
	Workflow     *WorkflowConfig   `toml:"workflow,omitempty"`
	Serve        *ServeConfig      `toml:"serve,omitempty"`
}

type ServeConfig struct {
	Command   string `toml:"command,omitempty"`
	Port      int    `toml:"port,omitempty"`
	ProxyPort int    `toml:"proxy_port,omitempty"`
}

type WorkflowConfig struct {
	Branch  string                `toml:"branch,omitempty"`
	Issue   *WorkflowIssueConfig  `toml:"issue,omitempty"`
	PR      *WorkflowPRConfig     `toml:"pr,omitempty"`
	Prompts *WorkflowPromptConfig `toml:"prompts,omitempty"`
}

type WorkflowIssueConfig struct {
	Create    string `toml:"create,omitempty"`
	View      string `toml:"view,omitempty"`
	Close     string `toml:"close,omitempty"`
	SetStatus string `toml:"set_status,omitempty"`
	Comment   string `toml:"comment,omitempty"`
}

type WorkflowPRConfig struct {
	Create string `toml:"create,omitempty"`
	Merge  string `toml:"merge,omitempty"`
	View   string `toml:"view,omitempty"`
}

type WorkflowPromptConfig struct {
	Yolo string `toml:"yolo,omitempty"`
}

func DefaultConfig() *Config {
	return &Config{
		Commands: map[string]string{
			"build": "echo 'TODO: set your build command'",
			"test":  "echo 'TODO: set your test command'",
			"run":   "echo 'TODO: set your run command'",
		},
		Env:          []string{"ANTHROPIC_API_KEY"},
		HostCommands: []string{"git", "gh"},
		CopyFiles:    []string{".env"},
	}
}

func DefaultWorkflowConfig() *WorkflowConfig {
	return &WorkflowConfig{
		Branch: "$Slug",
		Issue: &WorkflowIssueConfig{
			Create:    `gh issue create --title "$Title" --body "$Description" | grep -o '[0-9]*$'`,
			View:      `gh issue view "$IssueID" --json number,title,body,labels,state,url`,
			Close:     `gh issue close "$IssueID"`,
			SetStatus: `gh issue edit "$IssueID" --add-label "$Status"`,
			Comment:   `gh issue comment "$IssueID" --body "$Body"`,
		},
		PR: &WorkflowPRConfig{
			Create: `gh pr create --title "$Title" --body "$Description"`,
			Merge:  `gh pr merge "$PRNumber" --merge`,
			View:   `gh pr view "$PRNumber" --json number,state,title,url,mergedAt,closedAt`,
		},
	}
}

func Load(projectDir string) (*Config, error) {
	path := filepath.Join(projectDir, ConfigFile)
	if _, err := os.Stat(path); err != nil {
		// Fall back to legacy hidden filename for existing projects.
		legacy := filepath.Join(projectDir, LegacyConfigFile)
		if _, legacyErr := os.Stat(legacy); legacyErr == nil {
			path = legacy
		}
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("reading %s: %w", ConfigFile, err)
	}
	return &cfg, nil
}

func (c *Config) Save(projectDir string) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	path := filepath.Join(projectDir, ConfigFile)
	return os.WriteFile(path, buf.Bytes(), 0644)
}
