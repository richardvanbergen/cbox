package docker

import (
	"os/exec"
	"testing"
)

func hasDocker() bool {
	return exec.Command("docker", "info").Run() == nil
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
