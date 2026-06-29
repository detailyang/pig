package debuglog

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/ai"
)

func TestWrapStreamFuncEmitsDebugLinesAndForwardsEvents(t *testing.T) {
	var lines []string
	base := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCallEnd, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash", Arguments: map[string]any{"token": "sk-abcdefghijklmnopqrstuvwxyz123456"}}})
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "hello"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	wrapped := WrapStreamFunc(base, func(line string) { lines = append(lines, line) })

	stream, err := wrapped(context.Background(), ai.Model{Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses, ID: "gpt-test"}, []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hi"}}}}, []ai.Tool{{Name: "bash"}}, ai.SimpleStreamOptions{Base: ai.StreamOptions{SessionID: "sess-1"}, ThinkingLevel: ai.ThinkingHigh})
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 3 || events[0].Type != ai.EventToolCallEnd || events[1].Type != ai.EventTextDelta || events[2].Type != ai.EventDone {
		t.Fatalf("wrapped stream should forward events unchanged, got %#v", events)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"[debug llm #1 start]", "provider=openai", "messages=1", "tools=1", "reasoning=high", "session=sess-1", "[debug llm #1 context] last_user", "[debug llm #1 tool-call] id=call-1 name=bash", "[debug llm #1 done] reason=stop"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in lines:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "sk-abcdefghijklmnopqrstuvwxyz123456") || !strings.Contains(joined, "[REDACTED:openai_anthropic_key]") {
		t.Fatalf("debug wrapper should redact tool args, got:\n%s", joined)
	}
}

func TestWrapStreamFnCompatAlias(t *testing.T) {
	wrapped := WrapStreamFn(func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}, nil)
	if wrapped == nil {
		t.Fatal("expected wrapped stream function")
	}
}

func TestDebugPreviewUpstreamConstantNames(t *testing.T) {
	if DEBUG_PREVIEW_MAX_CHARS != PreviewMaxChars || DEBUG_PREVIEW_MAX_LINES != PreviewMaxLines {
		t.Fatalf("debug preview constants mismatch: chars=%d lines=%d", DEBUG_PREVIEW_MAX_CHARS, DEBUG_PREVIEW_MAX_LINES)
	}
}

func TestWrapStreamFuncLogsClosedWithoutTerminalEvent(t *testing.T) {
	var lines []string
	wrapped := WrapStreamFunc(func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		return ai.NewAssistantMessageEventStream(), nil
	}, func(line string) { lines = append(lines, line) })
	stream, err := wrapped(context.Background(), ai.Model{Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses, ID: "gpt-test"}, nil, nil, ai.SimpleStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stream.Events()) != 0 {
		t.Fatalf("empty stream should stay empty, got %#v", stream.Events())
	}
	if joined := strings.Join(lines, "\n"); !strings.Contains(joined, "[debug llm #1 closed]") || !strings.Contains(joined, "stream ended without terminal event") {
		t.Fatalf("missing closed line in:\n%s", joined)
	}
}

func TestWrapStreamFuncTreatsLeadingSystemMessageLikeUpstreamContext(t *testing.T) {
	var lines []string
	wrapped := WrapStreamFunc(func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}, func(line string) { lines = append(lines, line) })
	_, err := wrapped(context.Background(), ai.Model{Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses, ID: "gpt-test"}, []ai.Message{{Role: ai.RoleSystem, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "system prompt"}}}, {Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hi"}}}}, nil, ai.SimpleStreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "messages=1") || !strings.Contains(joined, "system_chars=13") {
		t.Fatalf("leading system message should be logged like upstream context, got:\n%s", joined)
	}
}

func TestStartLineIncludesModelContextAndOptions(t *testing.T) {
	model := ai.Model{Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses, ID: "gpt-test"}
	ctx := ai.Context{SystemPrompt: "system", Messages: []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hello"}}}}, Tools: []ai.Tool{{Name: "read"}}}
	line := StartLine(7, model, ctx, Options{Reasoning: "high", SessionID: "sess-1"})
	for _, want := range []string{"[debug llm #7 start]", "provider=openai", "api=openai-responses", "model=gpt-test", "messages=1", "tools=1", "system_chars=6", "reasoning=high", "session=sess-1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %s", want, line)
		}
	}
}

func TestContextAndToolCallLinesAreRedacted(t *testing.T) {
	ctx := ai.Context{Messages: []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "token sk-abcdefghijklmnopqrstuvwxyz123456"}}}}}
	line, ok := ContextLine(1, ctx)
	if !ok || !strings.Contains(line, "last_user") || !strings.Contains(line, "[REDACTED:openai_anthropic_key]") || strings.Contains(line, "sk-abcdefghijklmnopqrstuvwxyz123456") {
		t.Fatalf("bad context line: %q ok=%v", line, ok)
	}
	toolLine := ToolCallLine(2, ai.ToolCall{ID: "call-1", Name: "example", Arguments: map[string]any{"token": "ghp_abcdefghijklmnopqrstuvwxyz0123456789"}})
	if !strings.Contains(toolLine, "[debug llm #2 tool-call]") || !strings.Contains(toolLine, "[REDACTED:github_token]") || strings.Contains(toolLine, "ghp_abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("bad tool line: %s", toolLine)
	}
	unescapedToolLine := ToolCallLine(3, ai.ToolCall{ID: "call-2", Name: "example", Arguments: map[string]any{"text": "<tag>&value"}})
	if strings.Contains(unescapedToolLine, `\u003c`) || strings.Contains(unescapedToolLine, `\u003e`) || strings.Contains(unescapedToolLine, `\u0026`) {
		t.Fatalf("debug tool args should not HTML-escape strings like upstream serde_json: %s", unescapedToolLine)
	}
	if !strings.Contains(unescapedToolLine, `"text": "<tag>&value"`) {
		t.Fatalf("debug tool args missing unescaped value: %s", unescapedToolLine)
	}
}

func TestDoneLineLogsUsageAssistantContentAndRedacts(t *testing.T) {
	message := ai.AssistantMessage{ResponseID: "resp-1", Usage: &ai.Usage{InputTokens: 3, OutputTokens: 4, CacheReadTokens: 5, CacheWriteTokens: 6, Cost: &ai.UsageCost{Total: 0.1234567}}, StopReason: ai.StopReasonEndTurn, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "assistant sk-abcdefghijklmnopqrstuvwxyz123456"}, {Type: ai.ContentThinking, Thinking: "thinking xoxb-1234567890-abcdef"}, {Type: ai.ContentImage, MimeType: "image/png"}, {Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash"}}}}
	line := DoneLine(3, ai.DoneReasonStop, message, 25*time.Millisecond)
	for _, want := range []string{"[debug llm #3 done]", "reason=stop", "stop=end_turn", "elapsed=25ms", "usage=input:3 output:4 cache_read:5 cache_write:6 total:0 cost:$0.123457", "response_id=resp-1", "[image:image/png]", "[tool-call:call-1:bash]", "[REDACTED:openai_anthropic_key]", "[REDACTED:slack_token]"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %s", want, line)
		}
	}
}

func TestDoneLineLogsExplicitTotalTokens(t *testing.T) {
	message := ai.AssistantMessage{Usage: &ai.Usage{InputTokens: 3, OutputTokens: 4, CacheReadTokens: 5, CacheWriteTokens: 6, TotalTokenCount: 99, HasTotalTokens: true}}
	line := DoneLine(4, ai.DoneReasonStop, message, time.Millisecond)
	if !strings.Contains(line, "usage=input:3 output:4 cache_read:5 cache_write:6 total:99") {
		t.Fatalf("expected explicit total tokens, got %s", line)
	}
}

func TestPreviewIsRedactedAndBounded(t *testing.T) {
	huge := make([]string, 200)
	for i := range huge {
		huge[i] = strings.Repeat("x", 100)
	}
	preview := Preview(strings.Join(huge, "\n") + " Bearer abcdefghijklmnopqrstuvwxyz")
	if !strings.Contains(preview, "[debug preview truncated:") {
		t.Fatalf("expected truncation marker: %s", preview)
	}
	if strings.Contains(preview, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("expected redaction: %s", preview)
	}
	if len([]rune(preview)) > PreviewMaxChars+128 || strings.Count(preview, "\n") > PreviewMaxLines+1 {
		t.Fatalf("preview too large chars=%d lines=%d", len([]rune(preview)), strings.Count(preview, "\n"))
	}
}

func TestUpstreamDebugHelperAliases(t *testing.T) {
	if DebugPreview("Bearer abcdefghijklmnopqrstuvwxyz") != Preview("Bearer abcdefghijklmnopqrstuvwxyz") {
		t.Fatal("DebugPreview should alias upstream debug_preview behavior")
	}
	if ElapsedMS(25*time.Millisecond) != "25ms" {
		t.Fatalf("ElapsedMS mismatch: %q", ElapsedMS(25*time.Millisecond))
	}
	var lines []string
	Emit(func(line string) { lines = append(lines, line) }, "hello")
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("Emit mismatch: %#v", lines)
	}
	block := ai.UserContentBlock{Type: ai.UserContentImage, MimeType: "image/png"}
	if UserContentBlockLog(block) != "[image:image/png]" {
		t.Fatalf("UserContentBlockLog mismatch: %q", UserContentBlockLog(block))
	}
	content := ai.UserContentBlocksValue([]ai.UserContentBlock{{Type: ai.UserContentText, Text: "first"}, block})
	if UserContentLog(content) != "first\n[image:image/png]" {
		t.Fatalf("UserContentLog mismatch: %q", UserContentLog(content))
	}
}

func TestWrapStreamFuncReturnsLiveStreamBeforeDone(t *testing.T) {
	baseStarted := make(chan struct{})
	stream := ai.NewAssistantMessageEventStream().MarkLive()
	base := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		close(baseStarted)
		return stream, nil
	}
	wrapped := WrapStreamFunc(base, func(string) {})
	resultCh := make(chan *ai.AssistantMessageEventStream, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := wrapped(context.Background(), ai.Model{Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses, ID: "gpt-test"}, []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hi"}}}}, nil, ai.SimpleStreamOptions{})
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()
	<-baseStarted
	stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "partial"})
	select {
	case err := <-errCh:
		t.Fatal(err)
	case result := <-resultCh:
		if result == nil || !result.IsLive() {
			t.Fatalf("wrapped live stream mismatch: %#v", result)
		}
		event, _, err := result.Next(context.Background(), 0)
		if err != nil || event.Type != ai.EventTextDelta || event.Delta != "partial" {
			t.Fatalf("wrapped stream did not forward live delta: event=%#v err=%v", event, err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("wrapped live stream should return before terminal event")
	}
	stream.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, DoneReason: ai.DoneReasonStop, Message: &ai.AssistantMessage{Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "done"}}}})
}
