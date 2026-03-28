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
	Serve        *ServeConfig      `toml:"serve,omitempty"`
}

type ServeConfig struct {
	Command   string `toml:"command,omitempty"`
	Port      int    `toml:"port,omitempty"`
	ProxyPort int    `toml:"proxy_port,omitempty"`
}

func DefaultConfig() *Config {
	return &Config{
		Env:          []string{"ANTHROPIC_API_KEY"},
		HostCommands: []string{"git", "gh"},
		CopyFiles:    []string{".env"},
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
