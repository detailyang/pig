package ai

import (
	"context"
	"regexp"
	"testing"
)

func TestFauxProviderQueuesResponses(t *testing.T) {
	ClearFauxResponses()
	t.Cleanup(ClearFauxResponses)
	SetFauxResponses([]AssistantMessage{FauxAssistantMessage([]ContentBlock{FauxText("queued")})})

	provider := NewFauxProvider()
	message, ok := provider.Stream(context.Background(), Model{ID: "faux", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "queued" || message.StopReason != StopReasonEndTurn {
		t.Fatalf("message mismatch: %#v", message)
	}

	fallback, ok := provider.Stream(context.Background(), Model{ID: "faux", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{}).Result()
	if !ok || fallback.Text() != "[faux] hello" {
		t.Fatalf("fallback mismatch: %#v ok=%v", fallback, ok)
	}
}

func TestFauxProviderFallbackMessageMetadataLikeUpstream(t *testing.T) {
	ClearFauxResponses()
	t.Cleanup(ClearFauxResponses)

	message, ok := NewFauxProvider().Stream(context.Background(), Model{ID: "model-1", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Role != AssistantRoleAssistant || message.API != ApiFaux || message.Provider != Provider("faux") || message.Model != "model-1" {
		t.Fatalf("fallback metadata mismatch: %#v", message)
	}
	if message.Usage == nil || message.Timestamp == 0 {
		t.Fatalf("fallback should include default usage and timestamp like upstream: %#v", message)
	}
}

func TestFauxProviderReplaysQueuedEmptyMessageLikeUpstream(t *testing.T) {
	ClearFauxResponses()
	t.Cleanup(ClearFauxResponses)
	SetFauxResponses([]AssistantMessage{FauxAssistantMessage(nil)})

	message, ok := NewFauxProvider().Stream(context.Background(), Model{ID: "model-1", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "" || len(message.Content) != 0 || message.Model != "faux" {
		t.Fatalf("queued empty message should not fall back: %#v", message)
	}
}

func TestFauxAssistantMessageMetadataLikeUpstream(t *testing.T) {
	message := FauxAssistantMessage([]ContentBlock{FauxText("queued")})
	if message.Role != AssistantRoleAssistant || message.API != ApiFaux || message.Provider != Provider("faux") || message.Model != "faux" {
		t.Fatalf("metadata mismatch: %#v", message)
	}
	if message.Usage == nil || message.Timestamp == 0 || message.StopReason != StopReasonEndTurn {
		t.Fatalf("default fields mismatch: %#v", message)
	}

	toolMessage := FauxAssistantMessage([]ContentBlock{FauxToolCall("read", map[string]any{"path": "README.md"})})
	if toolMessage.StopReason != StopReasonToolCalls {
		t.Fatalf("tool stop reason mismatch: %#v", toolMessage)
	}
}

func TestFauxToolCallIDShapeLikeUpstream(t *testing.T) {
	block := FauxToolCall("read", map[string]any{"path": "README.md"})
	if block.ToolCall == nil {
		t.Fatal("expected tool call")
	}
	if !regexp.MustCompile(`^faux_[0-9a-f]{32}$`).MatchString(block.ToolCall.ID) {
		t.Fatalf("tool call id should match upstream UUID simple shape, got %q", block.ToolCall.ID)
	}
}

func TestFauxProviderReplaysTextEventSequenceLikeUpstream(t *testing.T) {
	ClearFauxResponses()
	t.Cleanup(ClearFauxResponses)
	SetFauxResponses([]AssistantMessage{FauxAssistantMessage([]ContentBlock{FauxText("queued")})})

	events := NewFauxProvider().Stream(context.Background(), Model{ID: "faux", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{}).Events()
	if len(events) != 5 {
		t.Fatalf("event count mismatch: %#v", events)
	}
	want := []AssistantMessageEventType{EventStart, EventTextStart, EventTextDelta, EventTextEnd, EventDone}
	for index, event := range events {
		if event.Type != want[index] {
			t.Fatalf("event %d mismatch: got %s want %s; events=%#v", index, event.Type, want[index], events)
		}
	}
	if events[1].ContentIndex != 0 || events[2].Delta != "queued" || events[3].Content != "queued" || events[4].DoneReason != DoneReasonStop {
		t.Fatalf("event payload mismatch: %#v", events)
	}
}

func TestFauxProviderReplaysToolCalls(t *testing.T) {
	ClearFauxResponses()
	t.Cleanup(ClearFauxResponses)
	AppendFauxResponses([]AssistantMessage{FauxAssistantMessage([]ContentBlock{FauxThinking("plan"), FauxToolCall("read", map[string]any{"path": "README.md"})})})

	message, ok := NewFauxProvider().Stream(context.Background(), Model{ID: "faux", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if len(message.Content) != 2 || message.Content[0].Type != ContentThinking || message.Content[0].Thinking != "plan" || message.Content[1].Type != ContentToolCall {
		t.Fatalf("thinking mismatch: %#v", message.Content)
	}
	if message.StopReason != StopReasonToolCalls || len(message.ToolCalls) != 1 || message.ToolCalls[0].Name != "read" || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool call mismatch: %#v stop=%s", message.ToolCalls, message.StopReason)
	}
}

func TestFauxProviderReplaysMaxTokensStopReasonLikeUpstream(t *testing.T) {
	ClearFauxResponses()
	t.Cleanup(ClearFauxResponses)
	SetFauxResponses([]AssistantMessage{{Content: []ContentBlock{FauxText("partial")}, StopReason: StopReasonMaxTokens}})

	message, ok := NewFauxProvider().Stream(context.Background(), Model{ID: "faux", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.StopReason != StopReasonMaxTokens {
		t.Fatalf("stop reason mismatch: %#v", message)
	}
}

func TestFauxProviderReplaysErrorEventLikeUpstream(t *testing.T) {
	ClearFauxResponses()
	t.Cleanup(ClearFauxResponses)
	SetFauxResponses([]AssistantMessage{{Content: []ContentBlock{FauxText("partial")}, StopReason: StopReasonError, ErrorMessage: "boom"}})

	events := NewFauxProvider().Stream(context.Background(), Model{ID: "faux", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{}).Events()
	last := events[len(events)-1]
	if last.Type != EventError || last.ErrorReason != ErrorReasonProvider || last.Error != "" || last.Message == nil || last.Message.StopReason != StopReasonError || last.Message.ErrorMessage != "boom" {
		t.Fatalf("error event mismatch: %#v", events)
	}
}

func TestFauxProviderReplaysAbortedErrorReasonLikeUpstream(t *testing.T) {
	ClearFauxResponses()
	t.Cleanup(ClearFauxResponses)
	SetFauxResponses([]AssistantMessage{{Content: []ContentBlock{FauxText("partial")}, StopReason: StopReasonAborted, ErrorMessage: "aborted"}})

	events := NewFauxProvider().Stream(context.Background(), Model{ID: "faux", API: ApiFaux, Provider: Provider("faux")}, Context{}, StreamOptions{}).Events()
	last := events[len(events)-1]
	if last.Type != EventError || last.ErrorReason != ErrorReasonAbort || last.Error != "" || last.Message == nil || last.Message.StopReason != StopReasonAborted || last.Message.ErrorMessage != "aborted" {
		t.Fatalf("aborted event mismatch: %#v", events)
	}
}

func TestFauxProviderRegisteredBuiltin(t *testing.T) {
	ClearAPIProviders()
	t.Cleanup(ClearAPIProviders)
	RegisterBuiltinProviders()
	if _, ok := GetAPIProvider(ApiFaux); !ok {
		t.Fatal("faux provider was not registered")
	}
}
