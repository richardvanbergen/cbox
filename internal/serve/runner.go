package serve

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// runnerOutput is the JSON written to stdout for the parent process to read.
type runnerOutput struct {
	Port int `json:"port"`
}

// RunServeCommand allocates a port, prints it as JSON to stdout, then runs the
// user's command with PORT set in the environment. It blocks until SIGTERM/SIGINT,
// forwarding the signal to the child process.
func RunServeCommand(command string, fixedPort int) error {
	port, err := AllocatePort(fixedPort)
	if err != nil {
		return err
	}

	data, err := json.Marshal(runnerOutput{Port: port})
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}
	fmt.Println(string(data))

	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting serve command: %w", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	cmd.Process.Signal(syscall.SIGTERM)
	cmd.Wait()
	return nil
}
