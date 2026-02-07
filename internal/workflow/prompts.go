package workflow

const defaultResearchPrompt = `You have been given the following task:

Title: {{.Title}}
{{- if .Description}}
Description: {{.Description}}
{{- end}}

Your job is to research the codebase and create an implementation plan for this task.

Steps:
1. Explore the codebase to understand the relevant code, architecture, and patterns
2. Identify the files that need to be created or modified
3. Create a detailed implementation plan with specific steps

When you have finished your research and created a plan, you MUST call the cbox_report MCP tool with:
- type: "plan"
- title: A short summary of your plan
- body: Your full implementation plan in markdown

Do NOT implement anything yet. Only research and plan.`

const defaultExecutePrompt = `You have been given the following task:

Title: {{.Title}}
{{- if .Description}}
Description: {{.Description}}
{{- end}}

Here is the implementation plan from the research phase:

{{.Plan}}

Your job is to implement this plan:
1. Follow the plan steps to implement the changes
2. Write tests where appropriate
3. Make sure the code compiles and tests pass
4. Commit your changes with clear commit messages

When you are done, you MUST call the cbox_report MCP tool with:
- type: "done"
- title: A short summary of what was accomplished
- body: A detailed summary of the changes made, files modified, and any notes for reviewers`

// renderPrompt renders a prompt template with the given data.
// If customPrompt is non-empty, it is used instead of the default.
func renderPrompt(defaultPrompt, customPrompt string, data any) (string, error) {
	tmpl := defaultPrompt
	if customPrompt != "" {
		tmpl = customPrompt
	}
	return renderTemplate(tmpl, data)
}
