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

func TestLoad_AppliesDefaultPRView(t *testing.T) {
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

	defaults := DefaultWorkflowConfig()
	if cfg.Workflow.PR.View != defaults.PR.View {
		t.Errorf("PR.View = %q, want default %q", cfg.Workflow.PR.View, defaults.PR.View)
	}
	// Existing values should be preserved, not overwritten.
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
