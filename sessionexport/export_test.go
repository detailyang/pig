package sessionexport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

func TestRenderContextMatchesUpstreamTranscriptShape(t *testing.T) {
	ctx := session.Context{
		ThinkingLevel: "high",
		Model:         &session.ContextModel{Provider: "openai", ModelID: "gpt-test"},
		Messages: []agent.Message{
			agent.NewUserMessage("hello"),
			{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{
				{Type: ai.ContentText, Text: "answer"},
				{Type: ai.ContentThinking, Thinking: "private plan"},
				{Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash", Arguments: map[string]any{"cmd": "go test"}}},
				{Type: ai.ContentImage},
			}}},
			{Kind: agent.MessageKindToolResult, ToolResult: &agent.ToolResult{CallID: "call-1", Name: "bash", Content: "ok"}},
			{Kind: agent.MessageKindCustom, Custom: &agent.CustomMessage{Role: "notice", Payload: map[string]any{"level": "info", "count": 2}}},
		},
	}
	rendered := RenderContext(ctx)
	for _, want := range []string{
		"# Session Transcript\n\n",
		"- Model: `openai:gpt-test`",
		"- Thinking level: `high`",
		"- Messages: 4",
		"## 0. User\n\nhello",
		"## 1. Assistant\n\nanswer",
		"<details><summary>thinking</summary>",
		"```\nprivate plan\n```",
		"**tool call** `bash` `call-1`:",
		"```json",
		`{"cmd":"go test"}`,
		"`[image]`",
		"### tool result `call-1`\n\nok",
		"### custom: notice",
		"\"level\": \"info\"",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("missing %q in rendered transcript:\n%s", want, rendered)
		}
	}
}

func TestRenderContextToolCallArgumentsUseCompactJSONLikeUpstream(t *testing.T) {
	rendered := RenderContext(session.Context{ThinkingLevel: "off", Messages: []agent.Message{
		{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{
			{Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash", Arguments: map[string]any{"cmd": "go test"}}},
		}}},
	}})
	if !strings.Contains(rendered, "```json\n{\"cmd\":\"go test\"}\n```") {
		t.Fatalf("tool call arguments should render as compact JSON like upstream:\n%s", rendered)
	}
	if strings.Contains(rendered, "  \"cmd\"") {
		t.Fatalf("tool call arguments should not be pretty-printed like local-only format:\n%s", rendered)
	}
}

func TestRenderContextJSONSnippetsDoNotHTMLEscapeLikeUpstreamSerde(t *testing.T) {
	rendered := RenderContext(session.Context{ThinkingLevel: "off", Messages: []agent.Message{
		{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{
			{Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash", Arguments: map[string]any{"cmd": "<tag>&value"}}},
		}}},
		{Kind: agent.MessageKindCustom, Custom: &agent.CustomMessage{Role: "notice", Payload: map[string]any{"text": "<tag>&value"}}},
	}})
	if strings.Contains(rendered, `\u003c`) || strings.Contains(rendered, `\u003e`) || strings.Contains(rendered, `\u0026`) {
		t.Fatalf("export JSON snippets should not HTML-escape like upstream serde_json:\n%s", rendered)
	}
	if !strings.Contains(rendered, `{"cmd":"<tag>&value"}`) {
		t.Fatalf("tool call JSON missing unescaped compact arguments:\n%s", rendered)
	}
	if !strings.Contains(rendered, `"text": "<tag>&value"`) {
		t.Fatalf("custom JSON missing unescaped pretty payload:\n%s", rendered)
	}
}

func TestRenderContextToolResultContentBlocksMatchUpstream(t *testing.T) {
	rendered := RenderContext(session.Context{ThinkingLevel: "off", Messages: []agent.Message{
		{Kind: agent.MessageKindToolResult, ToolResult: &agent.ToolResult{CallID: "call-1", ContentBlocks: []ai.ContentBlock{
			{Type: ai.ContentText, Text: "first"},
			{Type: ai.ContentImage},
			{Type: ai.ContentText, Text: "second"},
		}}},
	}})
	if !strings.Contains(rendered, "### tool result `call-1`\n\nfirst\n\n`[image]`\n\nsecond") {
		t.Fatalf("tool result content blocks should render like upstream user content blocks:\n%s", rendered)
	}
}

func TestRenderUserContentMatchesUpstreamHelper(t *testing.T) {
	text := RenderUserContent(ai.UserContentTextValue("plain text"))
	if text != "plain text" {
		t.Fatalf("text user content mismatch: %q", text)
	}
	blocks := RenderUserContent(ai.UserContentBlocksValue([]ai.UserContentBlock{
		{Type: ai.UserContentText, Text: "first"},
		{Type: ai.UserContentImage, MimeType: "image/png"},
		{Type: ai.UserContentText, Text: "second"},
	}))
	if blocks != "first\n\n`[image]`\n\nsecond" {
		t.Fatalf("block user content mismatch: %q", blocks)
	}
}

func TestRenderContextUsesBuiltSessionContext(t *testing.T) {
	entries := []session.Entry{
		session.NewMessageEntry("u", nil, "2026-01-02T03:04:05Z", agent.NewUserMessage("before")),
		session.NewCompactionEntry("compact", nil, "2026-01-02T03:05:05Z", "summary", "a", 10, nil, true),
		session.NewMessageEntry("a", nil, "2026-01-02T03:06:05Z", agent.NewAssistantMessage("kept")),
	}
	rendered := RenderEntries(entries)
	if !strings.Contains(rendered, "### custom: compaction_summary") || !strings.Contains(rendered, "kept") || strings.Contains(rendered, "before") {
		t.Fatalf("render entries mismatch:\n%s", rendered)
	}
}

func TestRenderSessionBuildsContextLikeUpstreamRender(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendThinkingLevelChange("medium"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendModelChange("openai", "gpt-test"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello from session")); err != nil {
		t.Fatal(err)
	}

	rendered, err := Render(sess)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- Model: `openai:gpt-test`", "- Thinking level: `medium`", "## 0. User\n\nhello from session"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("Render should build context from session like upstream render(); missing %q in:\n%s", want, rendered)
		}
	}
}

func TestSaveBuildsContextFromSessionLikeUpstreamSave(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendMessage(agent.NewUserMessage("persist me")); err != nil {
		t.Fatal(err)
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
		t.Fatalf("saved transcript should come from session context:\n%s", string(data))
	}
}

func TestDefaultExportPathAndSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", dir)
	path := DefaultExportPath("sess-1")
	if path != filepath.Join(dir, "exports", "sess-1.md") {
		t.Fatalf("default export path mismatch: %s", path)
	}
	dest := filepath.Join(dir, "nested", "out.md")
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
		t.Fatalf("saved transcript mismatch:\n%s", string(data))
	}
}
