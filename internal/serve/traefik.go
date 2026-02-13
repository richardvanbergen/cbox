package serve

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/richvanbergen/cbox/internal/docker"
)

const defaultProxyPort = 80

// TraefikContainerName returns the deterministic Traefik container name for a project.
func TraefikContainerName(projectName string) string {
	return "cbox-" + projectName + "-traefik"
}

// dynamicDir returns the path to the Traefik dynamic config directory.
func dynamicDir(projectDir string) string {
	return filepath.Join(projectDir, ".cbox", "traefik", "dynamic")
}

// EnsureTraefik starts the Traefik container if it is not already running.
func EnsureTraefik(projectDir, projectName string, proxyPort int) error {
	if proxyPort <= 0 {
		proxyPort = defaultProxyPort
	}

	name := TraefikContainerName(projectName)

	running, _ := docker.IsRunning(name)
	if running {
		return nil
	}

	dynDir := dynamicDir(projectDir)
	if err := os.MkdirAll(dynDir, 0755); err != nil {
		return fmt.Errorf("creating traefik dynamic dir: %w", err)
	}

	// Remove any stale container first (stopped but not removed)
	exec.Command("docker", "rm", "-f", name).Run()

	cmd := exec.Command("docker", "run", "-d",
		"--name", name,
		"-p", fmt.Sprintf("%d:80", proxyPort),
		"--add-host", "host.docker.internal:host-gateway",
		"-v", dynDir+":/etc/traefik/dynamic",
		"traefik:v3",
		"--entryPoints.web.address=:80",
		"--providers.file.directory=/etc/traefik/dynamic",
		"--providers.file.watch=true",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("starting traefik container: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// AddRoute writes a Traefik dynamic config file that routes the given hostname
// to the backend port on the host.
func AddRoute(projectDir, safeBranch, projectName string, backendPort int) error {
	dynDir := dynamicDir(projectDir)
	if err := os.MkdirAll(dynDir, 0755); err != nil {
		return fmt.Errorf("creating traefik dynamic dir: %w", err)
	}

	host := fmt.Sprintf("%s.%s.dev.localhost", safeBranch, projectName)
	content := fmt.Sprintf(`http:
  routers:
    %s:
      rule: "Host(`+"`%s`"+`)"
      service: %s
  services:
    %s:
      loadBalancer:
        servers:
          - url: "http://host.docker.internal:%d"
`, safeBranch, host, safeBranch, safeBranch, backendPort)

	path := filepath.Join(dynDir, safeBranch+".yml")
	return os.WriteFile(path, []byte(content), 0644)
}

// RemoveRoute deletes the Traefik dynamic config file for a branch.
func RemoveRoute(projectDir, safeBranch string) error {
	path := filepath.Join(dynamicDir(projectDir), safeBranch+".yml")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// HasRoutes checks if any .yml route files exist in the dynamic dir.
func HasRoutes(projectDir string) (bool, error) {
	pattern := filepath.Join(dynamicDir(projectDir), "*.yml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

// StopTraefik stops and removes the Traefik container.
func StopTraefik(projectName string) error {
	name := TraefikContainerName(projectName)
	return docker.StopAndRemove(name)
}
