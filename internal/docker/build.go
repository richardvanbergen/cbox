package docker

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed templates/Dockerfile.claude.tmpl templates/entrypoint.sh
var claudeFiles embed.FS

// BuildOptions controls how the Claude image is built.
type BuildOptions struct {
	ProjectDockerfile string // absolute path to a custom Dockerfile; empty = use embedded
	NoCache           bool   // pass --no-cache to docker build
}

// BuildClaudeImage builds the Claude container image from the embedded template
// or a custom Dockerfile specified in opts.
func BuildClaudeImage(imageName string, opts BuildOptions) error {
	tmpDir, err := os.MkdirTemp("", "cbox-")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Always extract embedded entrypoint.sh into the build context
	entrypoint, err := claudeFiles.ReadFile("templates/entrypoint.sh")
	if err != nil {
		return fmt.Errorf("reading embedded entrypoint.sh: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "entrypoint.sh"), entrypoint, 0755); err != nil {
		return fmt.Errorf("writing entrypoint.sh: %w", err)
	}

	// Resolve Dockerfile content: custom or embedded
	var dockerfileData []byte
	if opts.ProjectDockerfile != "" {
		dockerfileData, err = os.ReadFile(opts.ProjectDockerfile)
		if err != nil {
			return fmt.Errorf("reading custom Dockerfile %s: %w", opts.ProjectDockerfile, err)
		}
	} else {
		dockerfileData, err = claudeFiles.ReadFile("templates/Dockerfile.claude.tmpl")
		if err != nil {
			return fmt.Errorf("reading embedded Dockerfile: %w", err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile.claude"), dockerfileData, 0644); err != nil {
		return fmt.Errorf("writing Dockerfile.claude: %w", err)
	}

	buildArgs := []string{"build",
		"-f", filepath.Join(tmpDir, "Dockerfile.claude"),
		"-t", imageName,
	}
	if opts.NoCache {
		buildArgs = append(buildArgs, "--no-cache")
	}
	buildArgs = append(buildArgs, tmpDir)

	cmd := exec.Command("docker", buildArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Fprintln(os.Stdout)
	err = cmd.Run()
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return fmt.Errorf("building claude image: %w", err)
	}
	return nil
}

// EmbeddedDockerfile returns the contents of the embedded Dockerfile template.
func EmbeddedDockerfile() ([]byte, error) {
	return claudeFiles.ReadFile("templates/Dockerfile.claude.tmpl")
}

// ImageName returns a sanitized image name from the project name.
func ImageName(projectName, suffix string) string {
	name := strings.ToLower(projectName)
	name = strings.ReplaceAll(name, " ", "-")
	return "cbox-" + name + ":" + suffix
}
