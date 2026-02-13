package serve

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
)

var extraPortRe = regexp.MustCompile(`\$Port(\d+)`)

// runnerOutput is the JSON written to stdout for the parent process to read.
type runnerOutput struct {
	Port int `json:"port"`
}

// RunServeCommand allocates a port, prints it as JSON to stdout, then runs the
// user's command with port variables substituted. $Port is the primary port
// (used for Traefik routing). Additional ports ($Port2, $Port3, ...) are
// auto-allocated for services that need their own ports (e.g. dev tools).
func RunServeCommand(command string, fixedPort int, dir string) error {
	port, err := AllocatePort(fixedPort)
	if err != nil {
		return err
	}

	data, err := json.Marshal(runnerOutput{Port: port})
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}
	fmt.Println(string(data))

	// Allocate extra ports for $Port2, $Port3, etc. before replacing $Port
	// (otherwise $Port2 would be partially matched by $Port).
	expanded, err := expandExtraPorts(command)
	if err != nil {
		return err
	}
	expanded = strings.ReplaceAll(expanded, "$Port", fmt.Sprintf("%d", port))
	cmd := exec.Command("sh", "-c", expanded)
	if dir != "" {
		cmd.Dir = dir
	}
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

// expandExtraPorts finds all $Port2, $Port3, ... variables in the command and
// replaces each with a freshly allocated random port.
func expandExtraPorts(command string) (string, error) {
	matches := extraPortRe.FindAllString(command, -1)
	if len(matches) == 0 {
		return command, nil
	}

	// Deduplicate — same variable used twice gets the same port.
	allocated := make(map[string]string)
	for _, m := range matches {
		if _, ok := allocated[m]; ok {
			continue
		}
		p, err := AllocatePort(0)
		if err != nil {
			return "", fmt.Errorf("allocating extra port for %s: %w", m, err)
		}
		allocated[m] = fmt.Sprintf("%d", p)
	}

	result := command
	for variable, port := range allocated {
		result = strings.ReplaceAll(result, variable, port)
	}
	return result, nil
}
