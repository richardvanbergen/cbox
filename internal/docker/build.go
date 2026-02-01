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

//go:embed hostcmd-client-linux-amd64
var hostcmdClientAmd64 []byte

//go:embed hostcmd-client-linux-arm64
var hostcmdClientArm64 []byte

// HostCmdClientBinary returns the cross-compiled client binary for the given architecture.
func HostCmdClientBinary(arch string) ([]byte, error) {
	switch arch {
	case "amd64":
		return hostcmdClientAmd64, nil
	case "arm64":
		return hostcmdClientArm64, nil
	default:
		return nil, fmt.Errorf("unsupported architecture: %s", arch)
	}
}

// BuildBaseImage builds the user's production image from their Dockerfile.
func BuildBaseImage(contextDir, dockerfile, target, imageName string) error {
	args := []string{"build", "-f", dockerfile, "-t", imageName}
	if target != "" {
		args = append(args, "--target", target)
	}
	args = append(args, contextDir)

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building base image: %w", err)
	}
	return nil
}

// BuildClaudeImage builds the Claude container image from the embedded template.
func BuildClaudeImage(imageName string) error {
	tmpDir, err := os.MkdirTemp("", "cbox-")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write all embedded files into the build context
	files := []struct{ embed, local string }{
		{"templates/Dockerfile.claude.tmpl", "Dockerfile.claude"},
		{"templates/entrypoint.sh", "entrypoint.sh"},
	}
	for _, f := range files {
		data, err := claudeFiles.ReadFile(f.embed)
		if err != nil {
			return fmt.Errorf("reading embedded %s: %w", f.embed, err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, f.local), data, 0755); err != nil {
			return fmt.Errorf("writing %s: %w", f.local, err)
		}
	}

	cmd := exec.Command("docker", "build",
		"-f", filepath.Join(tmpDir, "Dockerfile.claude"),
		"-t", imageName,
		tmpDir,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building claude image: %w", err)
	}
	return nil
}

// ImageName returns a sanitized image name from the project name.
func ImageName(projectName, suffix string) string {
	name := strings.ToLower(projectName)
	name = strings.ReplaceAll(name, " ", "-")
	return "cbox-" + name + ":" + suffix
}
