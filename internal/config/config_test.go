package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig_CopyFilesIncludesEnv(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.CopyFiles) != 1 || cfg.CopyFiles[0] != ".env" {
		t.Errorf("DefaultConfig().CopyFiles = %v, want [\".env\"]", cfg.CopyFiles)
	}
}

func TestLoadConfig_ParsesCopyFiles(t *testing.T) {
	dir := t.TempDir()
	content := `copy_files = [".env", ".env.local", "config/secrets"]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{".env", ".env.local", "config/secrets"}
	if len(cfg.CopyFiles) != len(want) {
		t.Fatalf("CopyFiles length = %d, want %d", len(cfg.CopyFiles), len(want))
	}
	for i, v := range want {
		if cfg.CopyFiles[i] != v {
			t.Errorf("CopyFiles[%d] = %q, want %q", i, cfg.CopyFiles[i], v)
		}
	}
}

func TestLoad_MissingPRViewStaysEmpty(t *testing.T) {
	dir := t.TempDir()
	// Config has workflow.pr with create and merge but no view — simulates
	// configs created before the view command was added.
	content := `
[workflow]
branch = "$Slug"

[workflow.pr]
create = "gh pr create --title \"$Title\" --body \"$Description\""
merge = "gh pr merge \"$PRNumber\" --merge"
`
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Workflow.PR.View != "" {
		t.Errorf("PR.View = %q, want empty (no default backfill)", cfg.Workflow.PR.View)
	}
	// Existing values should be preserved.
	if cfg.Workflow.PR.Create != `gh pr create --title "$Title" --body "$Description"` {
		t.Errorf("PR.Create was overwritten: %q", cfg.Workflow.PR.Create)
	}
}

func TestLoad_PreservesExplicitPRView(t *testing.T) {
	dir := t.TempDir()
	content := `
[workflow]
branch = "$Slug"

[workflow.pr]
create = "gh pr create"
view = "custom-view-cmd"
`
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Workflow.PR.View != "custom-view-cmd" {
		t.Errorf("PR.View = %q, want %q", cfg.Workflow.PR.View, "custom-view-cmd")
	}
}

func TestLoad_NoWorkflowSection(t *testing.T) {
	dir := t.TempDir()
	content := `host_commands = ["git"]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Workflow != nil {
		t.Error("expected Workflow to be nil when not configured")
	}
}

func TestLoad_LegacyConfigFile(t *testing.T) {
	dir := t.TempDir()
	content := `host_commands = ["git"]` + "\n"
	// Write config using the legacy hidden filename.
	if err := os.WriteFile(filepath.Join(dir, LegacyConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load with legacy file: %v", err)
	}

	if len(cfg.HostCommands) != 1 || cfg.HostCommands[0] != "git" {
		t.Errorf("HostCommands = %v, want [\"git\"]", cfg.HostCommands)
	}
}

func TestLoad_PrefersNewOverLegacy(t *testing.T) {
	dir := t.TempDir()
	// Write both files — new name should win.
	legacy := `host_commands = ["git"]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, LegacyConfigFile), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	current := `host_commands = ["git", "gh"]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(current), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.HostCommands) != 2 {
		t.Errorf("HostCommands = %v, want [\"git\", \"gh\"] (new file should take priority)", cfg.HostCommands)
	}
}

func TestLoadConfig_ParsesPorts(t *testing.T) {
	dir := t.TempDir()
	content := `ports = ["3000", "8080:80", "127.0.0.1:3000:3000"]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := []string{"3000", "8080:80", "127.0.0.1:3000:3000"}
	if len(cfg.Ports) != len(want) {
		t.Fatalf("Ports length = %d, want %d", len(cfg.Ports), len(want))
	}
	for i, v := range want {
		if cfg.Ports[i] != v {
			t.Errorf("Ports[%d] = %q, want %q", i, cfg.Ports[i], v)
		}
	}
}

func TestLoadConfig_NoPortsField(t *testing.T) {
	dir := t.TempDir()
	content := `host_commands = ["git"]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Ports != nil {
		t.Errorf("Ports = %v, want nil when not configured", cfg.Ports)
	}
}

func TestSaveAndLoad_RoundTripPorts(t *testing.T) {
	dir := t.TempDir()

	cfg := &Config{
		Ports: []string{"3000", "8080:80", "127.0.0.1:3000:3000"},
	}
	if err := cfg.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Ports) != 3 {
		t.Fatalf("Ports length = %d, want 3", len(loaded.Ports))
	}
	want := []string{"3000", "8080:80", "127.0.0.1:3000:3000"}
	for i, v := range want {
		if loaded.Ports[i] != v {
			t.Errorf("Ports[%d] = %q, want %q", i, loaded.Ports[i], v)
		}
	}
}

func TestLoad_ServeConfig(t *testing.T) {
	dir := t.TempDir()
	content := `
[serve]
command = "npm start"
port = 3000
proxy_port = 8080
`
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Serve == nil {
		t.Fatal("expected Serve to be non-nil")
	}
	if cfg.Serve.Command != "npm start" {
		t.Errorf("Serve.Command = %q, want %q", cfg.Serve.Command, "npm start")
	}
	if cfg.Serve.Port != 3000 {
		t.Errorf("Serve.Port = %d, want 3000", cfg.Serve.Port)
	}
	if cfg.Serve.ProxyPort != 8080 {
		t.Errorf("Serve.ProxyPort = %d, want 8080", cfg.Serve.ProxyPort)
	}
}

func TestLoad_NoServeSection(t *testing.T) {
	dir := t.TempDir()
	content := `host_commands = ["git"]` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Serve != nil {
		t.Error("expected Serve to be nil when not configured")
	}
}

func TestSaveAndLoad_RoundTripCopyFiles(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultConfig()
	cfg.CopyFiles = []string{".env", "data/fixtures"}
	if err := cfg.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.CopyFiles) != 2 {
		t.Fatalf("CopyFiles length = %d, want 2", len(loaded.CopyFiles))
	}
	if loaded.CopyFiles[0] != ".env" || loaded.CopyFiles[1] != "data/fixtures" {
		t.Errorf("CopyFiles = %v, want [\".env\", \"data/fixtures\"]", loaded.CopyFiles)
	}
}
