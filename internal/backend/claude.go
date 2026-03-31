package backend

import (
	"os"
	"path/filepath"

	"github.com/richvanbergen/cbox/internal/docker"
)

type ClaudeBackend struct{}

func (ClaudeBackend) Name() Name { return Claude }

func (ClaudeBackend) DisplayName() string { return "Claude Code" }

func (ClaudeBackend) ImageName(projectName string) string {
	return docker.ImageName(projectName, string(Claude))
}

func (b ClaudeBackend) BuildImage(projectName string, opts docker.BuildOptions) (string, error) {
	imageName := b.ImageName(projectName)
	return imageName, docker.BuildClaudeImage(imageName, opts)
}

func (ClaudeBackend) ContainerName(projectName, branch string) string {
	return docker.ContainerName(projectName, branch, string(Claude))
}

func (b ClaudeBackend) RunContainer(spec RuntimeSpec, imageName string) (string, error) {
	containerName := b.ContainerName(spec.ProjectName, spec.Branch)
	extraEnv := map[string]string{}
	var mounts []docker.Mount

	// Prefer bind-mounting the host credentials file so the container stays
	// in sync with the host's login state (e.g. OAuth token refreshes).
	// Fall back to the Keychain env-var snapshot for hosts without the file.
	credsPath := filepath.Join(os.Getenv("HOME"), ".claude", ".credentials.json")
	if _, err := os.Stat(credsPath); err == nil {
		mounts = append(mounts, docker.Mount{
			Source:   credsPath,
			Target:   "/home/claude/.claude/.credentials.json",
			ReadOnly: true,
		})
	} else if creds := keychainPassword("Claude Code-credentials"); creds != "" {
		extraEnv["CLAUDE_CODE_CREDENTIALS"] = creds
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

func (ClaudeBackend) InjectInstructions(containerName string, spec RuntimeSpec) error {
	return docker.InjectClaudeMD(containerName, spec.HostCommands, spec.Commands, spec.Ports)
}

func (ClaudeBackend) RegisterMCP(containerName string, mcpPort int) error {
	return docker.InjectMCPConfig(containerName, mcpPort)
}

func (ClaudeBackend) Chat(containerName string, opts ChatOptions) error {
	return docker.Chat(containerName, opts.Chrome, opts.InitialPrompt, opts.Resume)
}

func (ClaudeBackend) ChatPrompt(containerName, prompt string) error {
	return docker.ChatPrompt(containerName, prompt)
}

func (ClaudeBackend) Shell(containerName string) error {
	return docker.Shell(containerName)
}

func (ClaudeBackend) HasConversationHistory(containerName string) (bool, error) {
	return docker.HasConversationHistory(containerName)
}

func (ClaudeBackend) EmbeddedDockerfile() ([]byte, error) {
	return docker.EmbeddedDockerfileForTemplate("templates/Dockerfile.claude.tmpl")
}
