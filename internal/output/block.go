package output

import "encoding/json"

// Block is a unit of structured output that can be rendered to the terminal.
type Block interface {
	BlockType() string
}

// TextBlock represents a text content block from Claude.
type TextBlock struct {
	Text string
}

func (b TextBlock) BlockType() string { return "text" }

// ToolUseBlock represents a tool_use content block from Claude.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (b ToolUseBlock) BlockType() string { return "tool_use" }

// ProgressBlock represents a cbox operational progress message.
type ProgressBlock struct {
	Message string
}

func (b ProgressBlock) BlockType() string { return "progress" }

// SuccessBlock represents a cbox success message.
type SuccessBlock struct {
	Message string
}

func (b SuccessBlock) BlockType() string { return "success" }

// WarningBlock represents a cbox warning message.
type WarningBlock struct {
	Message string
}

func (b WarningBlock) BlockType() string { return "warning" }

// ErrorBlock represents a cbox error message.
type ErrorBlock struct {
	Message string
}

func (b ErrorBlock) BlockType() string { return "error" }
