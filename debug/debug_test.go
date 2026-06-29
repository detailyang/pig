package debug

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/ai"
)

func TestDebugPackageMirrorsUpstreamHelpers(t *testing.T) {
	model := ai.Model{Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses, ID: "gpt-test"}
	context := ai.Context{
		SystemPrompt: "system",
		Messages:     []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hello"}}}},
		Tools:        []ai.Tool{{Name: "shell"}},
	}
	start := StartLine(7, model, context, Options{Reasoning: "medium", SessionID: "sess"})
	if start != "[debug llm #7 start] provider=openai api=openai-responses model=gpt-test messages=1 tools=1 system_chars=6 reasoning=medium session=sess" {
		t.Fatalf("start line mismatch: %q", start)
	}
	line, ok := ContextLine(7, context)
	if !ok || !strings.Contains(line, "[debug llm #7 context] last_user:\nhello") {
		t.Fatalf("context line mismatch: %q ok=%v", line, ok)
	}
	if Preview("Bearer abcdefghijklmnopqrstuvwxyz") != "[REDACTED:bearer_token]" {
		t.Fatalf("preview should redact bearer tokens")
	}
	if ElapsedMS(25*time.Millisecond) != "25ms" {
		t.Fatalf("elapsed mismatch")
	}
}

func TestDebugPackageWrapStreamFn(t *testing.T) {
	base := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, DoneReason: ai.DoneReasonStop, Message: &ai.AssistantMessage{Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "done"}}}})
		return stream, nil
	}
	var lines []string
	wrapped := WrapStreamFn(base, func(line string) { lines = append(lines, line) })
	stream, err := wrapped(context.Background(), ai.Model{Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses, ID: "gpt-test"}, []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hi"}}}}, nil, ai.SimpleStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stream.Events()) != 1 || len(lines) != 3 {
		t.Fatalf("wrap mismatch events=%d lines=%#v", len(stream.Events()), lines)
	}
	if !strings.Contains(lines[0], "[debug llm #1 start]") || !strings.Contains(lines[1], "last_user") || !strings.Contains(lines[2], "[debug llm #1 done]") {
		t.Fatalf("debug lines mismatch: %#v", lines)
	}
}
