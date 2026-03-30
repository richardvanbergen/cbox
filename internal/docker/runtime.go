package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/richvanbergen/cbox/internal/bridge"
	"github.com/richvanbergen/cbox/internal/output"
)

// Mount describes an additional bind mount for a runtime container.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// RunOptions controls generic container startup behavior shared across backends.
type RunOptions struct {
	Name           string
	Image          string
	Network        string
	WorktreePath   string
	GitMounts      *GitMountConfig
	EnvVars        []string
	ExtraEnv       map[string]string
	EnvFile        string
	BridgeMappings []bridge.ProxyMapping
	Ports          []string
	Mounts         []Mount
}

// RunContainer starts a backend runtime container with the shared cbox mounts.
func RunContainer(opts RunOptions) error {
	currentUser := os.Getenv("USER")

	args := []string{
		"run", "-d",
		"--name", opts.Name,
		"--network", opts.Network,
		"-v", opts.WorktreePath + ":/workspace",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
	}

	if opts.GitMounts != nil && opts.GitMounts.ProjectGitDir != "" && opts.GitMounts.ContainerGitFile != "" {
		args = append(args,
			"-v", opts.GitMounts.ProjectGitDir+":/repo/.git",
			"-v", opts.GitMounts.ContainerGitFile+":/workspace/.git:ro",
		)
	}

	for _, m := range opts.Mounts {
		mount := m.Source + ":" + m.Target
		if m.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}

	for _, p := range opts.Ports {
		args = append(args, "-p", p)
	}

	if len(opts.BridgeMappings) > 0 {
		mappingsJSON, err := bridge.MarshalMappings(opts.BridgeMappings)
		if err == nil {
			args = append(args, "-e", "CHROME_BRIDGE_MAPPINGS="+mappingsJSON)
			args = append(args, "-e", "USER="+currentUser)
		}
	}

	for _, env := range opts.EnvVars {
		val := os.Getenv(env)
		if val != "" {
			args = append(args, "-e", env+"="+val)
		}
	}

	for key, value := range opts.ExtraEnv {
		if value != "" {
			args = append(args, "-e", key+"="+value)
		}
	}

	if opts.EnvFile != "" {
		if _, err := os.Stat(opts.EnvFile); err == nil {
			args = append(args, "--env-file", opts.EnvFile)
		}
	}

	args = append(args, opts.Image)

	cmd := exec.Command("docker", args...)
	cw := output.NewCommandWriter(os.Stdout)
	cmd.Stdout = cw
	cmd.Stderr = cw
	runErr := cmd.Run()
	cw.Close()
	if runErr != nil {
		return fmt.Errorf("docker run (%s): %w", opts.Name, runErr)
	}
	return nil
}

// ExecInteractive replaces the current process with `docker exec -it`.
func ExecInteractive(container, user string, commandArgs ...string) error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found: %w", err)
	}

	args := []string{"docker", "exec", "-it"}
	args = append(args, terminalEnvArgs()...)
	if user != "" {
		args = append(args, "-u", user)
	}
	args = append(args, container)
	args = append(args, commandArgs...)
	return syscall.Exec(dockerPath, args, os.Environ())
}

// Exec runs a command inside a container and streams stdout/stderr.
func Exec(container, user string, commandArgs ...string) error {
	cmd := exec.Command("docker", dockerExecArgs(container, user, commandArgs...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExecOutput runs a command inside a container and returns stdout only.
func ExecOutput(container, user string, commandArgs ...string) ([]byte, error) {
	cmd := exec.Command("docker", dockerExecArgs(container, user, commandArgs...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker exec output (%s): %w", strings.Join(commandArgs, " "), err)
	}
	return out, nil
}

// ExecCombinedOutput runs a command inside a container and returns combined output.
func ExecCombinedOutput(container, user string, commandArgs ...string) ([]byte, error) {
	cmd := exec.Command("docker", dockerExecArgs(container, user, commandArgs...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("docker exec combined output (%s): %w", strings.Join(commandArgs, " "), err)
	}
	return out, nil
}

func dockerExecArgs(container, user string, commandArgs ...string) []string {
	args := []string{"exec"}
	if user != "" {
		args = append(args, "-u", user)
	}
	args = append(args, container)
	args = append(args, commandArgs...)
	return args
}
