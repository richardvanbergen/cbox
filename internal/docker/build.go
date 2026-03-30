package docker

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed templates/Dockerfile.claude.tmpl templates/Dockerfile.cursor.tmpl templates/entrypoint.sh
var claudeFiles embed.FS

// BuildOptions controls how a backend image is built.
type BuildOptions struct {
	ProjectDockerfile string // absolute path to a custom Dockerfile; empty = use embedded
	NoCache           bool   // pass --no-cache to docker build
}

// BuildImage builds a backend container image from an embedded template or a
// custom Dockerfile specified in opts.
func BuildImage(imageName, embeddedTemplate string, opts BuildOptions) error {
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
		dockerfileData, err = claudeFiles.ReadFile(embeddedTemplate)
		if err != nil {
			return fmt.Errorf("reading embedded Dockerfile: %w", err)
		}
	}
	const dockerfileName = "Dockerfile.cbox-runtime"
	if err := os.WriteFile(filepath.Join(tmpDir, dockerfileName), dockerfileData, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", dockerfileName, err)
	}

	buildArgs := []string{"build",
		"-f", filepath.Join(tmpDir, dockerfileName),
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
		return fmt.Errorf("building image: %w", err)
	}
	return nil
}

// BuildClaudeImage builds the Claude container image from the embedded template
// or a custom Dockerfile specified in opts.
func BuildClaudeImage(imageName string, opts BuildOptions) error {
	return BuildImage(imageName, "templates/Dockerfile.claude.tmpl", opts)
}

// BuildCursorImage builds the Cursor container image from the embedded template
// or a custom Dockerfile specified in opts.
func BuildCursorImage(imageName string, opts BuildOptions) error {
	return BuildImage(imageName, "templates/Dockerfile.cursor.tmpl", opts)
}

// EmbeddedDockerfile returns the default embedded Dockerfile template.
func EmbeddedDockerfile() ([]byte, error) {
	return EmbeddedDockerfileForTemplate("templates/Dockerfile.claude.tmpl")
}

// EmbeddedDockerfileForTemplate returns the contents of the requested embedded Dockerfile template.
func EmbeddedDockerfileForTemplate(template string) ([]byte, error) {
	return claudeFiles.ReadFile(template)
}

// ImageName returns a sanitized image name from the project name.
func ImageName(projectName, suffix string) string {
	name := strings.ToLower(projectName)
	name = strings.ReplaceAll(name, " ", "-")
	return "cbox-" + name + ":" + suffix
}
