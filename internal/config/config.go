package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const ConfigFile = ".cbox.yml"

type Config struct {
	Commands     map[string]string `yaml:"commands,omitempty"`
	Env          []string          `yaml:"env,omitempty"`
	EnvFile      string            `yaml:"env_file,omitempty"`
	Browser      bool              `yaml:"browser,omitempty"`
	HostCommands []string          `yaml:"host_commands,omitempty"`
	Dockerfile   string            `yaml:"dockerfile,omitempty"`
	Workflow     *WorkflowConfig   `yaml:"workflow,omitempty"`
}

type WorkflowConfig struct {
	Branch  string              `yaml:"branch,omitempty"`
	Issue   *WorkflowIssueConfig  `yaml:"issue,omitempty"`
	PR      *WorkflowPRConfig     `yaml:"pr,omitempty"`
	Prompts *WorkflowPromptConfig `yaml:"prompts,omitempty"`
}

type WorkflowIssueConfig struct {
	Create    string `yaml:"create,omitempty"`
	SetStatus string `yaml:"set_status,omitempty"`
	Comment   string `yaml:"comment,omitempty"`
}

type WorkflowPRConfig struct {
	Create string `yaml:"create,omitempty"`
	Merge  string `yaml:"merge,omitempty"`
}

type WorkflowPromptConfig struct {
	Research string `yaml:"research,omitempty"`
	Execute  string `yaml:"execute,omitempty"`
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
	}
}

func DefaultWorkflowConfig() *WorkflowConfig {
	return &WorkflowConfig{
		Branch: "{{.Slug}}",
		Issue: &WorkflowIssueConfig{
			Create:    `gh issue create --title "{{.Title}}" --body "{{.Description}}" --json number -q .number`,
			SetStatus: `gh issue edit {{.IssueID}} --add-label "{{.Status}}"`,
			Comment:   `gh issue comment {{.IssueID}} --body "{{.Body}}"`,
		},
		PR: &WorkflowPRConfig{
			Create: `gh pr create --title "{{.Title}}" --body "{{.Description}}" --json url -q .url`,
			Merge:  `gh pr merge {{.PRURL}} --merge`,
		},
	}
}

func Load(projectDir string) (*Config, error) {
	path := filepath.Join(projectDir, ConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", ConfigFile, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ConfigFile, err)
	}

	return &cfg, nil
}

func (c *Config) Save(projectDir string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	path := filepath.Join(projectDir, ConfigFile)
	return os.WriteFile(path, data, 0644)
}
