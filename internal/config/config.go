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
