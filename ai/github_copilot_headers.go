package ai

func CopilotHeaders() map[string]string {
	return map[string]string{
		"copilot-integration-id": "vscode-chat",
		"editor-version":         "pie-ai-rs/0.75",
	}
}
