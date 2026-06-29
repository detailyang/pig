package ai

import (
	"testing"
	"time"
)

func TestTransformMessagesDowngradesImagesForNonVisionModel(t *testing.T) {
	messages := []Message{{Role: RoleUser, Content: []ContentBlock{
		{Type: ContentText, Text: "look"},
		{Type: ContentImage, MimeType: "image/png", Data: "abc"},
		{Type: ContentImage, MimeType: "image/jpeg", Data: "def"},
	}}}

	out := TransformMessages(messages, Model{ID: "text", Input: []InputModality{InputText}})
	if len(out) != 1 || len(out[0].Content) != 2 {
		t.Fatalf("messages mismatch: %#v", out)
	}
	if out[0].Content[1].Type != ContentText || out[0].Content[1].Text != NonVisionUserImagePlaceholder {
		t.Fatalf("placeholder mismatch: %#v", out[0].Content)
	}
}

func TestTransformMessagesKeepsImagesForVisionModel(t *testing.T) {
	messages := []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentImage, MimeType: "image/png", Data: "abc"}}}}
	out := TransformMessages(messages, Model{ID: "vision", Input: []InputModality{InputText, InputImage}})
	if len(out) != 1 || len(out[0].Content) != 1 || out[0].Content[0].Type != ContentImage {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesSkipsErroredAssistantTurns(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, StopReason: StopReasonError, Content: []ContentBlock{{Type: ContentText, Text: "bad"}}},
		{Role: RoleAssistant, StopReason: StopReasonAborted, Content: []ContentBlock{{Type: ContentText, Text: "aborted"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "next"}}},
	}
	out := TransformMessages(messages, Model{ID: "target"})
	if len(out) != 1 || out[0].Role != RoleUser || out[0].Content[0].Text != "next" {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesUsesToolImagePlaceholder(t *testing.T) {
	messages := []Message{{Role: RoleTool, ToolCallID: "call-1", Content: []ContentBlock{{Type: ContentImage, MimeType: "image/png", Data: "abc"}}}}
	out := TransformMessages(messages, Model{ID: "text", Input: []InputModality{InputText}})
	if len(out) != 1 || len(out[0].Content) != 1 || out[0].Content[0].Text != NonVisionToolImagePlaceholder {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesConvertsThinkingAcrossModels(t *testing.T) {
	messages := []Message{{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{
		{Type: ContentThinking, Thinking: "private plan", ThinkingSignature: "sig-1"},
		{Type: ContentThinking, Thinking: "", ThinkingSignature: "empty"},
	}}}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 1 || len(out[0].Content) != 1 {
		t.Fatalf("messages mismatch: %#v", out)
	}
	if out[0].Content[0].Type != ContentText || out[0].Content[0].Text != "private plan" || out[0].Content[0].ThinkingSignature != "" {
		t.Fatalf("thinking mismatch: %#v", out[0].Content)
	}
}

func TestTransformMessagesDropsBlankThinkingLikeUpstream(t *testing.T) {
	messages := []Message{{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentThinking, Thinking: "  \n\t"}}}}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 1 || len(out[0].Content) != 0 {
		t.Fatalf("blank thinking should be dropped like upstream, got %#v", out)
	}
}

func TestTransformMessagesKeepsThinkingForSameModel(t *testing.T) {
	messages := []Message{{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "gpt", Content: []ContentBlock{{Type: ContentThinking, Thinking: "plan", ThinkingSignature: "sig-1"}}}}
	out := TransformMessages(messages, Model{ID: "gpt", Provider: Provider("openai"), API: ApiOpenAIResponses})
	if len(out) != 1 || len(out[0].Content) != 1 || out[0].Content[0].Type != ContentThinking || out[0].Content[0].ThinkingSignature != "sig-1" {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesKeepsRedactedThinkingForSameModelEvenWhenBlankLikeUpstream(t *testing.T) {
	messages := []Message{{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "gpt", Content: []ContentBlock{{Type: ContentThinking, Redacted: true}}}}
	out := TransformMessages(messages, Model{ID: "gpt", Provider: Provider("openai"), API: ApiOpenAIResponses})
	if len(out) != 1 || len(out[0].Content) != 1 || !out[0].Content[0].Redacted {
		t.Fatalf("same-model redacted thinking should be preserved like upstream: %#v", out)
	}
}

func TestTransformMessagesDropsBlankThinkingForSameModelLikeUpstream(t *testing.T) {
	messages := []Message{{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "gpt", Content: []ContentBlock{{Type: ContentThinking, Thinking: "  \n\t  "}}}}
	out := TransformMessages(messages, Model{ID: "gpt", Provider: Provider("openai"), API: ApiOpenAIResponses})
	if len(out) != 1 || len(out[0].Content) != 0 {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesStripsTextSignatureAcrossModels(t *testing.T) {
	messages := []Message{{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentText, Text: "answer", TextSignature: "sig-text"}}}}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 1 || len(out[0].Content) != 1 || out[0].Content[0].TextSignature != "" || out[0].Content[0].Text != "answer" {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesKeepsTextSignatureForSameModel(t *testing.T) {
	messages := []Message{{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "gpt", Content: []ContentBlock{{Type: ContentText, Text: "answer", TextSignature: "sig-text"}}}}
	out := TransformMessages(messages, Model{ID: "gpt", Provider: Provider("openai"), API: ApiOpenAIResponses})
	if len(out) != 1 || len(out[0].Content) != 1 || out[0].Content[0].TextSignature != "sig-text" {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesStripsToolCallThoughtSignatureAcrossModels(t *testing.T) {
	messages := []Message{{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", ToolCalls: []ToolCall{{ID: "call-1", Name: "read", ThoughtSignature: "sig-1"}}}}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 1 || len(out[0].ToolCalls) != 1 || out[0].ToolCalls[0].ThoughtSignature != "" {
		t.Fatalf("tool calls mismatch: %#v", out)
	}
}

func TestTransformMessagesNormalizesToolCallContentBlockAcrossModels(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}, ThoughtSignature: "sig-1"}}}},
		{Role: RoleTool, ToolCallID: "call-1", Name: "read", Content: []ContentBlock{{Type: ContentText, Text: "ok"}}},
	}
	out := TransformMessagesWithOptions(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic}, TransformOptions{NormalizeToolCallID: func(id string, _ Model, _ Message) string {
		return "norm-" + id
	}})
	if len(out) != 2 || len(out[0].Content) != 1 || out[0].Content[0].ToolCall == nil {
		t.Fatalf("messages mismatch: %#v", out)
	}
	call := out[0].Content[0].ToolCall
	if call.ID != "norm-call-1" || call.ThoughtSignature != "" || out[1].ToolCallID != "norm-call-1" {
		t.Fatalf("tool call mismatch: call=%#v toolResult=%#v", call, out[1])
	}
}

func TestTransformMessagesAllowsNormalizerToReturnEmptyToolCallIDLikeUpstream(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}}}},
		{Role: RoleTool, ToolCallID: "call-1", Name: "read", Content: []ContentBlock{{Type: ContentText, Text: "ok"}}},
	}
	out := TransformMessagesWithOptions(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic}, TransformOptions{NormalizeToolCallID: func(_ string, _ Model, _ Message) string {
		return ""
	}})
	if len(out) != 2 || out[0].Content[0].ToolCall == nil || out[0].Content[0].ToolCall.ID != "" || out[1].ToolCallID != "" {
		t.Fatalf("empty normalized tool call id should be preserved like upstream: %#v", out)
	}
}

func TestTransformMessagesDropsRedactedThinkingAcrossModels(t *testing.T) {
	messages := []Message{{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentThinking, Thinking: "secret", Redacted: true}}}}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 1 || len(out[0].Content) != 0 {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesNormalizesToolCallIDs(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", ToolCalls: []ToolCall{{ID: "call-1", Name: "read"}}},
		{Role: RoleTool, ToolCallID: "call-1", Name: "read", Content: []ContentBlock{{Type: ContentText, Text: "ok"}}},
	}
	normalizer := ToolCallIdNormalizer(func(id string, _ Model, _ Message) string {
		return "norm-" + id
	})
	out := TransformMessagesWithOptions(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic}, TransformOptions{NormalizeToolCallID: normalizer})
	if len(out) != 2 || out[0].ToolCalls[0].ID != "norm-call-1" || out[1].ToolCallID != "norm-call-1" {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesSynthesizesMissingToolResults(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "next"}}},
	}
	start := time.Now().UnixMilli()
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	end := time.Now().UnixMilli()
	if len(out) != 3 || out[1].Role != RoleTool || out[1].ToolCallID != "call-1" || out[1].Name != "read" || !out[1].IsError || out[1].Details != nil || out[1].Content[0].Text != MissingToolResultPlaceholder || out[2].Role != RoleUser {
		t.Fatalf("messages mismatch: %#v", out)
	}
	if out[1].Timestamp < start || out[1].Timestamp > end {
		t.Fatalf("timestamp mismatch: got %d, want between %d and %d", out[1].Timestamp, start, end)
	}
}

func TestTransformMessagesSynthesizesMissingToolResultsForContentToolCalls(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "next"}}},
	}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 3 || out[1].Role != RoleTool || out[1].ToolCallID != "call-1" || out[1].Name != "read" || out[1].Content[0].Text != MissingToolResultPlaceholder || out[2].Role != RoleUser {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesPendingToolCallsPreferContentBlocksLikeUpstream(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", ToolCalls: []ToolCall{{ID: "legacy", Name: "legacy"}}, Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "content", Name: "content"}}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "next"}}},
	}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 3 || out[1].Role != RoleTool || out[1].ToolCallID != "content" || out[1].Name != "content" || out[2].Role != RoleUser {
		t.Fatalf("pending synthetic result should come only from content tool calls like upstream: %#v", out)
	}
}

func TestTransformMessagesDoesNotDeduplicateContentToolCallsLikeUpstream(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{
			{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "first"}},
			{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "second"}},
		}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "next"}}},
	}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 4 || out[1].Role != RoleTool || out[1].Name != "first" || out[2].Role != RoleTool || out[2].Name != "second" || out[3].Role != RoleUser {
		t.Fatalf("duplicate content tool calls should both synthesize results like upstream: %#v", out)
	}
}

func TestTransformMessagesDoesNotSynthesizeExistingToolResults(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", ToolCalls: []ToolCall{{ID: "call-1", Name: "read"}}},
		{Role: RoleTool, ToolCallID: "call-1", Name: "read", Content: []ContentBlock{{Type: ContentText, Text: "ok"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "next"}}},
	}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 3 || out[1].Content[0].Text != "ok" {
		t.Fatalf("messages mismatch: %#v", out)
	}
}

func TestTransformMessagesExistingToolResultOnlyMatchesCurrentPendingBatchLikeUpstream(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}}}},
		{Role: RoleTool, ToolCallID: "call-1", Name: "read", Content: []ContentBlock{{Type: ContentText, Text: "first"}}},
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "write"}}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "next"}}},
	}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 5 || out[3].Role != RoleTool || out[3].Name != "write" || out[3].Content[0].Text != MissingToolResultPlaceholder || out[4].Role != RoleUser {
		t.Fatalf("duplicate earlier tool result should not satisfy later pending call like upstream: %#v", out)
	}
}

func TestTransformMessagesFlushesPendingBeforeSkippedErroredAssistantLikeUpstream(t *testing.T) {
	messages := []Message{
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}}}},
		{Role: RoleAssistant, Provider: Provider("openai"), API: ApiOpenAIResponses, Model: "source", StopReason: StopReasonError, Content: []ContentBlock{{Type: ContentText, Text: "failed"}}},
		{Role: RoleTool, ToolCallID: "call-1", Name: "read", Content: []ContentBlock{{Type: ContentText, Text: "late"}}},
	}
	out := TransformMessages(messages, Model{ID: "target", Provider: Provider("anthropic"), API: ApiAnthropic})
	if len(out) != 3 || out[1].Role != RoleTool || out[1].Content[0].Text != MissingToolResultPlaceholder || out[2].Role != RoleTool || out[2].Content[0].Text != "late" {
		t.Fatalf("errored assistant should flush previous pending calls before being skipped like upstream: %#v", out)
	}
}
