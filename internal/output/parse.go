package output

import (
	"encoding/json"
	"fmt"
)

// rawBlock is used to partially decode Claude's JSON content blocks.
type rawBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ParseClaudeBlocks parses the JSON content block array from Claude's
// --output-format json response and converts each element to a Block.
// Unknown block types fall through to TextBlock with a type annotation.
func ParseClaudeBlocks(data []byte) ([]Block, error) {
	var raws []rawBlock
	if err := json.Unmarshal(data, &raws); err != nil {
		return nil, fmt.Errorf("parsing claude blocks: %w", err)
	}

	blocks := make([]Block, 0, len(raws))
	for _, r := range raws {
		switch r.Type {
		case "text":
			blocks = append(blocks, TextBlock{Text: r.Text})
		case "tool_use":
			blocks = append(blocks, ToolUseBlock{
				ID:    r.ID,
				Name:  r.Name,
				Input: r.Input,
			})
		default:
			blocks = append(blocks, TextBlock{
				Text: fmt.Sprintf("[%s] %s", r.Type, r.Text),
			})
		}
	}
	return blocks, nil
}
