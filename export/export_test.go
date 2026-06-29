package export

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/sessionrunner"
)

func TestExportPackageRenderContextAndUserContent(t *testing.T) {
	ctx := session.Context{
		Model:         &session.ContextModel{Provider: "openai", ModelID: "gpt-test"},
		ThinkingLevel: "medium",
		Messages: []agent.Message{
			agent.NewUserMessage("hello"),
			{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "answer"}, {Type: ai.ContentThinking, Thinking: "trace"}, {Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash", Arguments: map[string]any{"text": "<tag>&value"}}}}}},
			{Kind: agent.MessageKindToolResult, ToolResult: &agent.ToolResult{CallID: "call-1", ContentBlocks: []ai.ContentBlock{{Type: ai.ContentText, Text: "result"}, {Type: ai.ContentImage}}}},
		},
	}
	rendered := RenderContext(ctx)
	for _, want := range []string{"# Session Transcript", "- Model: `openai:gpt-test`", "- Thinking level: `medium`", "## 0. User\n\nhello", "## 1. Assistant", "<details><summary>thinking</summary>", "**tool call** `bash` `call-1`", `"text":"<tag>&value"`, "### tool result `call-1`", "result\n\n`[image]`"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered transcript missing %q:\n%s", want, rendered)
		}
	}
	content := RenderUserContent(ai.UserContentBlocksValue([]ai.UserContentBlock{{Type: ai.UserContentText, Text: "first"}, {Type: ai.UserContentImage}, {Type: ai.UserContentText, Text: "second"}}))
	if content != "first\n\n`[image]`\n\nsecond" {
		t.Fatalf("user content mismatch: %q", content)
	}
}

func TestExportPackageRenderAndSaveSession(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendThinkingLevelChange("low"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendModelChange("anthropic", "claude-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("persist me")); err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(sess)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, "- Model: `anthropic:claude-test`") || !strings.Contains(rendered, "persist me") {
		t.Fatalf("render mismatch:\n%s", rendered)
	}
	dest := filepath.Join(t.TempDir(), "nested", "session.md")
	written, err := Save(sess, dest)
	if err != nil {
		t.Fatal(err)
	}
	if written != dest {
		t.Fatalf("written path mismatch: %s", written)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "persist me") {
		t.Fatalf("saved transcript mismatch:\n%s", string(data))
	}
}

func TestExportPackageSavesSessionRunnerTranscriptInOrderLikeUpstream(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	replies := []string{"first ack", "second ack"}
	runner := sessionrunner.SessionRunner{Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")}, Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		if len(replies) == 0 {
			t.Fatal("unexpected extra stream call")
		}
		reply := replies[0]
		replies = replies[1:]
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: reply})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}}
	if _, err := sess.AppendMessage(agent.NewUserMessage("first question")); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), sessionrunner.RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("second question")); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), sessionrunner.RunSessionInput{Session: sess}); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "transcript.md")
	written, err := Save(sess, dest)
	if err != nil {
		t.Fatal(err)
	}
	if written != dest {
		t.Fatalf("written path mismatch: %s", written)
	}
	bodyBytes, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	body := string(bodyBytes)
	positions := []int{
		strings.Index(body, "first question"),
		strings.Index(body, "first ack"),
		strings.Index(body, "second question"),
		strings.Index(body, "second ack"),
	}
	for _, position := range positions {
		if position < 0 {
			t.Fatalf("transcript missing expected content:\n%s", body)
		}
	}
	if !(positions[0] < positions[1] && positions[1] < positions[2] && positions[2] < positions[3]) || !strings.Contains(body, "# Session Transcript") {
		t.Fatalf("transcript order mismatch:\n%s", body)
	}
}

func TestExportPackageDefaultPathAndSaveContext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", dir)
	if got := DefaultExportPath("sess-1"); got != filepath.Join(dir, "exports", "sess-1.md") {
		t.Fatalf("default path mismatch: %s", got)
	}
	dest := filepath.Join(dir, "out", "ctx.md")
	written, err := SaveContext(session.Context{ThinkingLevel: "off", Messages: []agent.Message{agent.NewUserMessage("hi")}}, dest)
	if err != nil {
		t.Fatal(err)
	}
	if written != dest {
		t.Fatalf("written path mismatch: %s", written)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Session Transcript") || !strings.Contains(string(data), "## 0. User") {
		t.Fatalf("saved context mismatch:\n%s", string(data))
	}
}
