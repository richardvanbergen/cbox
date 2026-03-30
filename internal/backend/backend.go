package backend

import (
	"fmt"
	"strings"

	"github.com/richvanbergen/cbox/internal/bridge"
	"github.com/richvanbergen/cbox/internal/docker"
)

type Name string

const (
	Claude Name = "claude"
	Cursor Name = "cursor"
)

// RuntimeSpec contains the backend-independent sandbox settings.
type RuntimeSpec struct {
	ProjectDir     string
	ProjectName    string
	Branch         string
	WorktreePath   string
	NetworkName    string
	GitMounts      *docker.GitMountConfig
	EnvVars        []string
	EnvFile        string
	BridgeMappings []bridge.ProxyMapping
	Ports          []string
	HostCommands   []string
	Commands       map[string]string
	MCPPort        int
}

type ChatOptions struct {
	Chrome        bool
	InitialPrompt string
	Resume        bool
}

type Backend interface {
	Name() Name
	DisplayName() string
	ImageName(projectName string) string
	BuildImage(projectName string, opts docker.BuildOptions) (string, error)
	ContainerName(projectName, branch string) string
	RunContainer(spec RuntimeSpec, imageName string) (string, error)
	InjectInstructions(containerName string, spec RuntimeSpec) error
	RegisterMCP(containerName string, mcpPort int) error
	Chat(containerName string, opts ChatOptions) error
	ChatPrompt(containerName, prompt string) error
	Shell(containerName string) error
	HasConversationHistory(containerName string) (bool, error)
	EmbeddedDockerfile() ([]byte, error)
}

func ParseName(raw string) Name {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(Claude):
		return Claude
	case string(Cursor):
		return Cursor
	default:
		return Name(strings.ToLower(strings.TrimSpace(raw)))
	}
}

func Get(name Name) (Backend, error) {
	switch ParseName(string(name)) {
	case Claude:
		return ClaudeBackend{}, nil
	case Cursor:
		return CursorBackend{}, nil
	default:
		return nil, fmt.Errorf("unsupported backend %q", name)
	}
}
