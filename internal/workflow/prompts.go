package workflow

const defaultYoloPrompt = `You have been assigned the following task:

$TaskContent

Read the codebase, plan your approach, implement the changes, write tests, and commit.
When done, call the cbox_report MCP tool with type "done", a title summarizing what you did, and a body with details for reviewers.`

// renderPrompt expands variables in a prompt template.
// If customPrompt is non-empty, it is used instead of the default.
func renderPrompt(defaultPrompt, customPrompt string, data map[string]string) string {
	tmpl := defaultPrompt
	if customPrompt != "" {
		tmpl = customPrompt
	}
	return expandVars(tmpl, data)
}
