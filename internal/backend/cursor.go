package backend

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/richvanbergen/cbox/internal/docker"
)

type CursorBackend struct{}

const cursorUser = "claude"

func (CursorBackend) Name() Name { return Cursor }

func (CursorBackend) DisplayName() string { return "Cursor Agent" }

func (CursorBackend) ImageName(projectName string) string {
	return docker.ImageName(projectName, string(Cursor))
}

func (b CursorBackend) BuildImage(projectName string, opts docker.BuildOptions) (string, error) {
	imageName := b.ImageName(projectName)
	return imageName, docker.BuildCursorImage(imageName, opts)
}

func (CursorBackend) ContainerName(projectName, branch string) string {
	return docker.ContainerName(projectName, branch, string(Cursor))
}

func (b CursorBackend) RunContainer(spec RuntimeSpec, imageName string) (string, error) {
	containerName := b.ContainerName(spec.ProjectName, spec.Branch)
	extraEnv := map[string]string{}
	if apiKey := strings.TrimSpace(os.Getenv("CURSOR_API_KEY")); apiKey != "" {
		extraEnv["CURSOR_API_KEY"] = apiKey
	} else if authToken := keychainPassword("cursor-access-token"); authToken != "" {
		extraEnv["CURSOR_AUTH_TOKEN"] = authToken
	}

	mounts := []docker.Mount{}

	if spec.MCPPort > 0 {
		cursorDir := filepath.Join(spec.ProjectDir, ".cbox", "cursor", safeBranch(spec.Branch), ".cursor")
		if err := mkdirAll(cursorDir); err != nil {
			return "", err
		}
		mcpPath := filepath.Join(cursorDir, "mcp.json")
		if err := writeFile(mcpPath, buildCursorMCPConfig(spec.WorktreePath, spec.MCPPort)); err != nil {
			return "", err
		}
		mounts = append(mounts, docker.Mount{
			Source:   cursorDir,
			Target:   "/home/claude/.cursor",
			ReadOnly: false,
		})
	}

	err := docker.RunContainer(docker.RunOptions{
		Name:           containerName,
		Image:          imageName,
		Network:        spec.NetworkName,
		WorktreePath:   spec.WorktreePath,
		GitMounts:      spec.GitMounts,
		EnvVars:        spec.EnvVars,
		ExtraEnv:       extraEnv,
		EnvFile:        spec.EnvFile,
		BridgeMappings: spec.BridgeMappings,
		Ports:          spec.Ports,
		Mounts:         mounts,
	})
	return containerName, err
}

func (CursorBackend) InjectInstructions(_ string, spec RuntimeSpec) error {
	content := mergeWorkspaceClaudeMD(spec.WorktreePath, buildInstructions(spec))
	return writeFile(filepath.Join(spec.WorktreePath, "CLAUDE.md"), content)
}

func (CursorBackend) RegisterMCP(string, int) error {
	return nil
}

func (CursorBackend) Chat(containerName string, opts ChatOptions) error {
	args := []string{"agent", "--force", "--approve-mcps"}
	if opts.Resume {
		args = append(args, "--continue")
	} else if opts.InitialPrompt != "" {
		args = append(args, opts.InitialPrompt)
	}
	return docker.ExecInteractive(containerName, cursorUser, args...)
}

func (CursorBackend) ChatPrompt(containerName, prompt string) error {
	args := []string{
		"agent",
		"--print",
		"--output-format", "json",
		"--force",
		"--trust",
		"--approve-mcps",
		prompt,
	}
	return docker.Exec(containerName, cursorUser, args...)
}

func (CursorBackend) Shell(containerName string) error {
	return docker.ExecInteractive(containerName, cursorUser, "bash")
}

func (CursorBackend) HasConversationHistory(containerName string) (bool, error) {
	out, err := docker.ExecOutput(containerName, cursorUser, "agent", "ls")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func (CursorBackend) EmbeddedDockerfile() ([]byte, error) {
	return docker.EmbeddedDockerfileForTemplate("templates/Dockerfile.cursor.tmpl")
}

func safeBranch(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0755)
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
