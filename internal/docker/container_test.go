package docker

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func hasDocker() bool {
	return exec.Command("docker", "info").Run() == nil
}

// TestTerminalEnvArgs verifies that terminalEnvArgs returns -e flags only for
// terminal env vars that are actually set in the environment.
func TestTerminalEnvArgs(t *testing.T) {
	// Clear all terminal env vars to start from a known state.
	termVars := []string{
		"COLORTERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION",
		"LC_TERMINAL", "LC_TERMINAL_VERSION",
		"KITTY_WINDOW_ID", "KITTY_PID", "ITERM_SESSION_ID",
		"WT_SESSION", "WT_PROFILE_ID", "TERMINAL_EMULATOR",
		"WEZTERM_PANE", "KONSOLE_VERSION", "VTE_VERSION",
	}
	for _, v := range termVars {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}

	// With nothing set, should return nil.
	args := terminalEnvArgs()
	if len(args) != 0 {
		t.Errorf("expected no args when no vars set, got %v", args)
	}

	// Set a couple of vars and verify the output.
	t.Setenv("TERM_PROGRAM", "iTerm2")
	t.Setenv("COLORTERM", "truecolor")

	args = terminalEnvArgs()
	expected := []string{"-e", "COLORTERM=truecolor", "-e", "TERM_PROGRAM=iTerm2"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want)
		}
	}
}

// TestTerminalEnvArgsSingleVar verifies correct output when exactly one var is set.
func TestTerminalEnvArgsSingleVar(t *testing.T) {
	termVars := []string{
		"COLORTERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION",
		"LC_TERMINAL", "LC_TERMINAL_VERSION",
		"KITTY_WINDOW_ID", "KITTY_PID", "ITERM_SESSION_ID",
		"WT_SESSION", "WT_PROFILE_ID", "TERMINAL_EMULATOR",
		"WEZTERM_PANE", "KONSOLE_VERSION", "VTE_VERSION",
	}
	for _, v := range termVars {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}

	t.Setenv("KITTY_WINDOW_ID", "42")

	args := terminalEnvArgs()
	expected := []string{"-e", "KITTY_WINDOW_ID=42"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want)
		}
	}
}

// TestBuildClaudeMD_AllCommands verifies that when all well-known commands are
// configured, none appear in the "not available" section.
func TestBuildClaudeMD_AllCommands(t *testing.T) {
	commands := map[string]string{
		"build": "go build ./...",
		"test":  "go test ./...",
		"run":   "go run ./cmd/app",
		"setup": "go mod download",
	}

	md := BuildClaudeMD([]string{"git"}, commands, nil)

	for _, name := range []string{"build", "test", "run", "setup"} {
		if !strings.Contains(md, "cbox_"+name+":") {
			t.Errorf("expected cbox_%s to appear as available", name)
		}
	}
	if strings.Contains(md, "is NOT available") {
		t.Error("no commands should be marked unavailable when all are configured")
	}
}

// TestBuildClaudeMD_NoCommands verifies that when no commands are configured,
// all well-known commands appear as unavailable.
func TestBuildClaudeMD_NoCommands(t *testing.T) {
	md := BuildClaudeMD([]string{"git"}, nil, nil)

	if !strings.Contains(md, "No project commands are configured") {
		t.Error("expected 'No project commands are configured' message")
	}
	for _, name := range []string{"build", "run", "setup", "test"} {
		want := "cbox_" + name + " is NOT available"
		if !strings.Contains(md, want) {
			t.Errorf("expected %q in output", want)
		}
	}
}

// TestBuildClaudeMD_PartialCommands verifies that only unconfigured well-known
// commands appear as unavailable.
func TestBuildClaudeMD_PartialCommands(t *testing.T) {
	commands := map[string]string{
		"build": "go build ./...",
		"test":  "go test ./...",
	}

	md := BuildClaudeMD(nil, commands, nil)

	// build and test should be listed as available
	if !strings.Contains(md, "cbox_build: `go build ./...`") {
		t.Error("expected cbox_build to be listed as available")
	}
	if !strings.Contains(md, "cbox_test: `go test ./...`") {
		t.Error("expected cbox_test to be listed as available")
	}

	// run and setup should be listed as unavailable
	if !strings.Contains(md, "cbox_run is NOT available") {
		t.Error("expected cbox_run to be listed as unavailable")
	}
	if !strings.Contains(md, "cbox_setup is NOT available") {
		t.Error("expected cbox_setup to be listed as unavailable")
	}

	// build and test should NOT be listed as unavailable
	if strings.Contains(md, "cbox_build is NOT available") {
		t.Error("cbox_build should not be listed as unavailable")
	}
	if strings.Contains(md, "cbox_test is NOT available") {
		t.Error("cbox_test should not be listed as unavailable")
	}
}

// TestBuildClaudeMD_CustomCommand verifies that non-well-known commands are
// listed as available but don't affect the unavailable list.
func TestBuildClaudeMD_CustomCommand(t *testing.T) {
	commands := map[string]string{
		"lint": "golangci-lint run",
	}

	md := BuildClaudeMD(nil, commands, nil)

	if !strings.Contains(md, "cbox_lint: `golangci-lint run`") {
		t.Error("expected custom command cbox_lint to be listed")
	}
	// All well-known commands should still be listed as unavailable
	for _, name := range []string{"build", "run", "setup", "test"} {
		want := "cbox_" + name + " is NOT available"
		if !strings.Contains(md, want) {
			t.Errorf("expected %q in output", want)
		}
	}
}

// TestBuildClaudeMD_SetupCommand verifies that setup is recognised as a
// well-known command and appears correctly.
func TestBuildClaudeMD_SetupCommand(t *testing.T) {
	commands := map[string]string{
		"setup": "npm install",
	}

	md := BuildClaudeMD(nil, commands, nil)

	if !strings.Contains(md, "cbox_setup: `npm install`") {
		t.Error("expected cbox_setup to be listed as available")
	}
	if strings.Contains(md, "cbox_setup is NOT available") {
		t.Error("cbox_setup should not be listed as unavailable when configured")
	}
}

// TestBuildClaudeMD_ExtrasAppended verifies that extra sections are appended.
func TestBuildClaudeMD_ExtrasAppended(t *testing.T) {
	extra := "## Custom Section\n\nThis is a custom section."
	md := BuildClaudeMD(nil, nil, nil, extra)

	if !strings.Contains(md, "## Custom Section") {
		t.Error("expected extra section to be appended")
	}
}

// TestBuildClaudeMD_SetupInHelpText verifies that the self-healing section
// mentions the setup command in the example toml.
func TestBuildClaudeMD_SetupInHelpText(t *testing.T) {
	md := BuildClaudeMD(nil, nil, nil)

	if !strings.Contains(md, `setup = "go mod download"`) {
		t.Error("expected setup command in the cbox.toml example")
	}
}

// TestStopAndRemoveNonExistent verifies that StopAndRemove returns nil when
// the container does not exist (rather than leaking an error).
func TestStopAndRemoveNonExistent(t *testing.T) {
	if !hasDocker() {
		t.Skip("docker not available")
	}

	err := StopAndRemove("cbox-test-nonexistent-container-12345")
	if err != nil {
		t.Errorf("StopAndRemove on non-existent container returned error: %v", err)
	}
}

// TestStopAndRemoveRunning verifies that StopAndRemove successfully stops and
// removes a running container.
func TestStopAndRemoveRunning(t *testing.T) {
	if !hasDocker() {
		t.Skip("docker not available")
	}

	name := "cbox-test-stopandremove"

	// Clean up in case a previous test run left a container behind.
	exec.Command("docker", "rm", "-f", name).Run()

	// Start a simple container.
	cmd := exec.Command("docker", "run", "-d", "--name", name, "alpine", "sleep", "300")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to start test container: %s: %v", string(out), err)
	}

	// Ensure cleanup even if the test fails.
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", name).Run()
	})

	if err := StopAndRemove(name); err != nil {
		t.Fatalf("StopAndRemove returned error: %v", err)
	}

	// Verify the container is gone.
	check := exec.Command("docker", "inspect", name)
	if err := check.Run(); err == nil {
		t.Error("container still exists after StopAndRemove")
	}
}
