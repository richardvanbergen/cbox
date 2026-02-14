package docker

import (
	"os"
	"os/exec"
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
