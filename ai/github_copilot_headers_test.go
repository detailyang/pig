package ai

import "testing"

func TestCopilotHeadersMatchUpstream(t *testing.T) {
	headers := CopilotHeaders()
	if headers["copilot-integration-id"] != "vscode-chat" {
		t.Fatalf("integration header mismatch: %#v", headers)
	}
	if headers["editor-version"] != "pie-ai-rs/0.75" {
		t.Fatalf("editor version header mismatch: %#v", headers)
	}
}
