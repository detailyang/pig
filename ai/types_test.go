package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestKnownApiMapsToApiStringsLikeUpstream(t *testing.T) {
	known := map[KnownApi]Api{
		KnownApiOpenAICompletions:     ApiOpenAICompletions,
		KnownApiMistralConversations:  ApiMistral,
		KnownApiOpenAIResponses:       ApiOpenAIResponses,
		KnownApiAzureOpenAIResponses:  ApiAzureOpenAIResponses,
		KnownApiOpenAICodexResponses:  ApiOpenAICodexResponses,
		KnownApiAnthropicMessages:     ApiAnthropicMessages,
		KnownApiBedrockConverseStream: ApiBedrockConverseStream,
		KnownApiGoogleGenerativeAI:    ApiGoogleGenerativeAI,
		KnownApiGoogleVertex:          ApiGoogleVertex,
	}
	for knownAPI, want := range known {
		if got := NewKnownApi(knownAPI); got != want {
			t.Fatalf("known api mismatch for %s: got %s want %s", knownAPI, got, want)
		}
		if got := Known(knownAPI); got != want {
			t.Fatalf("known api constructor mismatch for %s: got %s want %s", knownAPI, got, want)
		}
		if got := knownAPI.String(); Api(got) != want {
			t.Fatalf("known api string mismatch for %s: got %s want %s", knownAPI, got, want)
		}
		if got := knownAPI.AsStr(); Api(got) != want {
			t.Fatalf("known api as-str mismatch for %s: got %s want %s", knownAPI, got, want)
		}
	}
}

func TestProviderResponseTypeMatchesUpstream(t *testing.T) {
	response := ProviderResponse{Status: 200, Headers: map[string]string{"x-request-id": "req-1"}}
	if response.Status != 200 || response.Headers["x-request-id"] != "req-1" {
		t.Fatalf("provider response mismatch: %#v", response)
	}
}

func TestAIUpstreamEnumVariantAliases(t *testing.T) {
	if CacheRetentionNone != CacheNone || CacheRetentionShort != CacheShort || CacheRetentionLong != CacheLong {
		t.Fatalf("cache retention aliases mismatch")
	}
	if TextSignaturePhaseCommentary != TextSignatureCommentary || TextSignaturePhaseFinalAnswer != TextSignatureFinalAnswer {
		t.Fatalf("text signature phase aliases mismatch")
	}
	if ContentBlockText != ContentText || ContentBlockThinking != ContentThinking || ContentBlockImage != ContentImage || ContentBlockToolCall != ContentToolCall {
		t.Fatalf("content block aliases mismatch")
	}
	if AssistantMessageEventTextStart != EventTextStart || AssistantMessageEventTextDelta != EventTextDelta || AssistantMessageEventTextEnd != EventTextEnd || AssistantMessageEventThinkingStart != EventThinkingStart || AssistantMessageEventThinkingDelta != EventThinkingDelta || AssistantMessageEventThinkingEnd != EventThinkingEnd || AssistantMessageEventToolCallStart != EventToolCallStart || AssistantMessageEventToolCallDelta != EventToolCallDelta || AssistantMessageEventToolCallEnd != EventToolCallEnd {
		t.Fatalf("assistant message event aliases mismatch")
	}
}

func TestToolCallArgumentsPreserveRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var call ToolCall
	data := []byte(`{"id":"call-1","name":"read","arguments":{"ticket":9007199254740993}}`)
	if err := json.Unmarshal(data, &call); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(call.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("tool call arguments should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestMessageDiagnosticsPreserveRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var output Message
	data := []byte(`{"role":"assistant","content":[],"api":"responses","provider":"openai","model":"gpt-test","diagnostics":[{"ticket":9007199254740993}],"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":123}`)
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(output.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("assistant diagnostics should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestAssistantMessageDiagnosticsPreserveRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var output AssistantMessage
	data := []byte(`{"content":[],"api":"responses","provider":"openai","model":"gpt-test","diagnostics":[{"ticket":9007199254740993}],"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":123}`)
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(output.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("assistant message diagnostics should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestMessageToolResultDetailsPreserveRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var output Message
	data := []byte(`{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[{"type":"text","text":"ok"}],"details":{"ticket":9007199254740993},"isError":false,"timestamp":123}`)
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(output.DetailsValue)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("tool result details should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestToolResultMessageDetailsPreserveRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var output ToolResultMessage
	data := []byte(`{"toolCallId":"call-1","toolName":"read","content":[{"type":"text","text":"ok"}],"details":{"ticket":9007199254740993},"isError":false,"timestamp":123}`)
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(output.Details)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("tool result message details should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestModelRegistryCustomOverridesBuiltin(t *testing.T) {
	ClearCustomModels()
	RegisterBuiltinModel(Model{ID: "m1", Provider: Provider("openai"), API: ApiOpenAIResponses, Name: "builtin"})
	RegisterCustomModel(Model{ID: "m1", Provider: Provider("openai"), API: ApiAnthropic, Name: "custom"})

	model, ok := GetModel(Provider("openai"), "m1")
	if !ok {
		t.Fatal("expected model")
	}
	if model.Name != "custom" || model.API != ApiAnthropic {
		t.Fatalf("custom model should override builtin, got %#v", model)
	}

	UnregisterCustomModel(Provider("openai"), "m1")
	model, ok = GetModel(Provider("openai"), "m1")
	if !ok || model.Name != "builtin" {
		t.Fatalf("expected builtin after unregister, got %#v ok=%v", model, ok)
	}
}

func TestAssistantMessageEventStreamResult(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, Delta: "hel"})
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, Delta: "lo"})
	stream.Emit(AssistantMessageEvent{Type: EventToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}})
	stream.Emit(AssistantMessageEvent{Type: EventUsage, Usage: &Usage{InputTokens: 3, OutputTokens: 2}})
	stream.Close(DoneReasonStop)

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed assistant message")
	}
	if message.Text() != "hello" {
		t.Fatalf("text mismatch: %q", message.Text())
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Name != "read" {
		t.Fatalf("tool calls mismatch: %#v", message.ToolCalls)
	}
	if message.Usage == nil || message.Usage.InputTokens != 3 || message.Usage.OutputTokens != 2 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
	if message.Timestamp == 0 {
		t.Fatalf("expected timestamp: %#v", message)
	}

	events := stream.Events()
	if len(events) != 5 || events[4].Type != EventDone {
		t.Fatalf("expected immutable event history with done event, got %#v", events)
	}
}

func TestCreateAssistantMessageEventStreamMatchesUpstreamFactory(t *testing.T) {
	stream := CreateAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, Delta: "hi"})
	stream.Close(DoneReasonStop)

	message, ok := stream.Result()
	if !ok || message.Text() != "hi" {
		t.Fatalf("factory stream mismatch: ok=%v message=%#v", ok, message)
	}
}

func TestErrorStreamResultHasTimestamp(t *testing.T) {
	message, ok := ErrorStream("boom").Result()
	if !ok || message.StopReason != StopReasonError || message.Timestamp == 0 {
		t.Fatalf("error stream mismatch: %#v ok=%v", message, ok)
	}
}

func TestAssistantMessageEventStreamResultMapsAbort(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Close(DoneReasonAbort)
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonAborted {
		t.Fatalf("result mismatch: %#v ok=%v", message, ok)
	}
}

func TestAssistantMessageEventIsTerminalMatchesUpstream(t *testing.T) {
	if !((AssistantMessageEvent{Type: EventDone}).IsTerminal()) {
		t.Fatal("done event should be terminal")
	}
	if !((AssistantMessageEvent{Type: EventError}).IsTerminal()) {
		t.Fatal("error event should be terminal")
	}
	if (AssistantMessageEvent{Type: EventTextDelta}).IsTerminal() {
		t.Fatal("text delta event should not be terminal")
	}
}

func TestAssistantMessageEventStreamResultMergesThinkingDeltas(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, Delta: "pla", ContentBlock: &ContentBlock{ThinkingSignature: "sig", Redacted: true}})
	stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, Delta: "n"})
	stream.Close(DoneReasonStop)

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed assistant message")
	}
	if len(message.Content) != 1 {
		t.Fatalf("expected one thinking block, got %#v", message.Content)
	}
	block := message.Content[0]
	if block.Type != ContentThinking || block.Thinking != "plan" || block.ThinkingSignature != "sig" || !block.Redacted {
		t.Fatalf("thinking block mismatch: %#v", block)
	}
}

func TestAssistantMessageEventStreamResultMergesIndexedDeltasLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, ContentIndex: 1, Delta: "pla"})
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: 0, Delta: "hel"})
	stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, ContentIndex: 1, Delta: "n"})
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: 0, Delta: "lo"})
	stream.Close(DoneReasonStop)

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed assistant message")
	}
	if len(message.Content) != 2 || message.Content[0].Type != ContentText || message.Content[0].Text != "hello" || message.Content[1].Type != ContentThinking || message.Content[1].Thinking != "plan" {
		t.Fatalf("indexed delta content mismatch: %#v", message.Content)
	}
}

func TestAssistantMessageEventStreamResultHandlesUpstreamStartEvents(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventThinkingStart, ContentIndex: 1, Partial: &AssistantMessage{Content: []ContentBlock{{}, {Type: ContentThinking, ThinkingSignature: "sig", Redacted: true}}}})
	stream.Emit(AssistantMessageEvent{Type: EventTextStart, ContentIndex: 0})
	stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, ContentIndex: 1, Delta: "plan"})
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: 0, Delta: "answer"})
	stream.Close(DoneReasonStop)

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed assistant message")
	}
	if len(message.Content) != 2 || message.Content[0].Type != ContentText || message.Content[0].Text != "answer" {
		t.Fatalf("text start mismatch: %#v", message.Content)
	}
	if message.Content[1].Type != ContentThinking || message.Content[1].Thinking != "plan" || message.Content[1].ThinkingSignature != "sig" || !message.Content[1].Redacted {
		t.Fatalf("thinking start mismatch: %#v", message.Content[1])
	}
}

func TestAssistantMessageEventStreamResultHandlesUpstreamEndEvents(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventTextEnd, ContentIndex: 0, Content: "answer"})
	stream.Emit(AssistantMessageEvent{Type: EventThinkingEnd, ContentIndex: 1, Content: "plan"})
	stream.Emit(AssistantMessageEvent{Type: EventToolCallEnd, ContentIndex: 2, ToolCall: &ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
	stream.Close(DoneReasonToolCalls)

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed assistant message")
	}
	if len(message.Content) != 3 || message.Content[0].Text != "answer" || message.Content[1].Thinking != "plan" || message.Content[2].ToolCall == nil || message.Content[2].ToolCall.Name != "read" {
		t.Fatalf("upstream end events should build content blocks: %#v", message.Content)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call-1" || message.StopReason != StopReasonToolCalls {
		t.Fatalf("upstream tool call end mismatch: %#v", message)
	}
}

func TestAssistantMessageEventStreamResultBuffersToolCallDeltaFragments(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventToolCallStart, ContentIndex: 0, Partial: &AssistantMessage{Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}}}}})
	stream.Emit(AssistantMessageEvent{Type: EventToolCallDelta, ContentIndex: 0, Delta: `{"path":`})
	stream.Emit(AssistantMessageEvent{Type: EventToolCallDelta, ContentIndex: 0, Delta: `"README.md"}`})
	stream.Close(DoneReasonToolCalls)

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed assistant message")
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool call delta arguments mismatch: %#v", message.ToolCalls)
	}
	if len(message.Content) != 1 || message.Content[0].ToolCall == nil || message.Content[0].ToolCall.Arguments["path"] != "README.md" {
		t.Fatalf("tool call delta content mismatch: %#v", message.Content)
	}
}

func TestAssistantMessageEventStreamResultHonorsContentIndexLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventThinkingEnd, ContentIndex: 1, Content: "plan"})
	stream.Emit(AssistantMessageEvent{Type: EventTextEnd, ContentIndex: 0, Content: "answer"})
	stream.Close(DoneReasonStop)

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed assistant message")
	}
	if len(message.Content) != 2 || message.Content[0].Type != ContentText || message.Content[0].Text != "answer" || message.Content[1].Type != ContentThinking || message.Content[1].Thinking != "plan" {
		t.Fatalf("content_index order mismatch: %#v", message.Content)
	}
}

func TestAssistantMessageEventStreamResultUsesUpstreamPartialMetadata(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: 0, Delta: "hi", Partial: &AssistantMessage{ResponseModel: "served", ResponseID: "resp-1", Usage: &Usage{InputTokens: 3}}})
	stream.Close(DoneReasonStop)

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed assistant message")
	}
	if message.Text() != "hi" || message.ResponseModel != "served" || message.ResponseID != "resp-1" || message.Usage == nil || message.Usage.InputTokens != 3 {
		t.Fatalf("partial metadata mismatch: %#v", message)
	}
}

func TestAssistantMessageEventStreamResultUsesUpstreamTerminalMessage(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, Delta: "partial"})
	stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonLength, Message: &AssistantMessage{Content: []ContentBlock{{Type: ContentText, Text: "final"}}, Usage: &Usage{InputTokens: 1}, Timestamp: 123}})

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed assistant message")
	}
	if message.Text() != "final" || message.Usage == nil || message.Usage.InputTokens != 1 || message.Timestamp != 123 || message.StopReason != StopReasonMaxTokens {
		t.Fatalf("done terminal message mismatch: %#v", message)
	}

	errorStream := NewAssistantMessageEventStream()
	errorStream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &AssistantMessage{ErrorMessage: "provider boom", Timestamp: 456}})
	errorMessage, ok := errorStream.Result()
	if !ok || errorMessage.ErrorMessage != "provider boom" || errorMessage.Timestamp != 456 || errorMessage.StopReason != StopReasonError {
		t.Fatalf("error terminal message mismatch: %#v ok=%v", errorMessage, ok)
	}
}

func TestAssistantMessageEventStreamResultPreservesTerminalMessageStopReasonLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonLength, Message: &AssistantMessage{StopReason: StopReasonToolCalls}})

	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonToolCalls {
		t.Fatalf("terminal message stop reason should be preserved: %#v ok=%v", message, ok)
	}
}

func TestAssistantMessageEventStreamResultUsesFirstTerminalEventLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonStop, Message: &AssistantMessage{Content: []ContentBlock{{Type: ContentText, Text: "first"}}}})
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, Delta: " ignored"})
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonError, Message: &AssistantMessage{ErrorMessage: "second"}})

	message, ok := stream.Result()
	if !ok || message.Text() != "first" || message.ErrorMessage != "" {
		t.Fatalf("first terminal event should win: %#v ok=%v", message, ok)
	}
}

func TestAssistantMessageEventReasonsMatchUpstreamNames(t *testing.T) {
	if DoneReasonToolUse != DoneReasonToolCalls {
		t.Fatalf("tool use reason should alias local tool calls reason: %q", DoneReasonToolUse)
	}
	if ErrorReasonError != "error" || ErrorReasonProvider != ErrorReasonError {
		t.Fatalf("error reason should use upstream error name: error=%q provider=%q", ErrorReasonError, ErrorReasonProvider)
	}
	if ErrorReasonAborted != "aborted" || ErrorReasonAbort != ErrorReasonAborted {
		t.Fatalf("aborted reason should use upstream aborted name: aborted=%q abort=%q", ErrorReasonAborted, ErrorReasonAbort)
	}

	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonAborted, Message: &AssistantMessage{ErrorMessage: "aborted"}})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonAborted {
		t.Fatalf("aborted error reason should map to aborted stop reason: %#v ok=%v", message, ok)
	}
}

func TestMessageJSONRoundTrip(t *testing.T) {
	details := map[string]any{"exit_code": float64(1)}
	input := Message{Role: RoleTool, ResponseModel: "served", ResponseID: "resp-1", ToolCallID: "call-1", ToolName: "read", IsError: true, Details: details, DetailsValue: details, ErrorMessage: "failed", Timestamp: 123, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}
	data, err := input.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var output Message
	if err := output.UnmarshalJSON(data); err != nil {
		t.Fatal(err)
	}
	outputData, err := output.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, outputData) {
		t.Fatalf("round trip mismatch: %#v != %#v", input, output)
	}
}

func TestContextFromMessagesPromotesLeadingSystemMessage(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "first"}, {Type: ContentText, Text: "second"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}},
	}
	tools := []Tool{{Name: "read"}}

	context := ContextFromMessages(messages, tools)
	if !context.HasSystemPrompt || context.SystemPrompt != "first\nsecond" {
		t.Fatalf("system prompt = %#v", context)
	}
	if len(context.Messages) != 1 || context.Messages[0].Role != RoleUser || len(context.Tools) != 1 {
		t.Fatalf("context = %#v", context)
	}
}

func TestContextFromMessagesPreservesExplicitEmptySystemPrompt(t *testing.T) {
	context := ContextFromMessages([]Message{{Role: RoleSystem}}, nil)
	if !context.HasSystemPrompt || context.SystemPrompt != "" || len(context.Messages) != 0 {
		t.Fatalf("context = %#v", context)
	}
}

func TestContextSystemPromptOptionSemanticsLikeUpstream(t *testing.T) {
	omitted, err := json.Marshal(Context{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(omitted, []byte("systemPrompt")) {
		t.Fatalf("zero context should omit systemPrompt like upstream None, got %s", omitted)
	}

	explicitEmpty, err := json.Marshal(Context{HasSystemPrompt: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(explicitEmpty, []byte(`"systemPrompt":""`)) {
		t.Fatalf("explicit empty systemPrompt should marshal like Some(empty), got %s", explicitEmpty)
	}

	var context Context
	if err := json.Unmarshal([]byte(`{"systemPrompt":"","messages":[]}`), &context); err != nil {
		t.Fatal(err)
	}
	if !context.HasSystemPrompt || context.SystemPrompt != "" {
		t.Fatalf("unmarshal should preserve explicit empty systemPrompt: %#v", context)
	}
}

func TestContextToolsOptionSemanticsLikeUpstream(t *testing.T) {
	omitted, err := json.Marshal(Context{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(omitted, []byte("tools")) {
		t.Fatalf("zero context should omit tools like upstream None, got %s", omitted)
	}

	explicitEmpty, err := json.Marshal(Context{HasTools: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(explicitEmpty, []byte(`"tools":[]`)) {
		t.Fatalf("explicit empty tools should marshal like Some(empty), got %s", explicitEmpty)
	}

	var context Context
	if err := json.Unmarshal([]byte(`{"messages":[],"tools":[]}`), &context); err != nil {
		t.Fatal(err)
	}
	if !context.HasTools || len(context.Tools) != 0 {
		t.Fatalf("unmarshal should preserve explicit empty tools: %#v", context)
	}
}

func TestContextNullOptionsDecodeAsNoneLikeUpstream(t *testing.T) {
	var context Context
	if err := json.Unmarshal([]byte(`{"messages":[],"systemPrompt":null,"tools":null}`), &context); err != nil {
		t.Fatal(err)
	}
	if context.HasSystemPrompt || context.HasTools {
		t.Fatalf("null options should decode as None like upstream: %#v", context)
	}
	data, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("systemPrompt")) || bytes.Contains(data, []byte("tools")) {
		t.Fatalf("null options should re-marshal as omitted None like upstream, got %s", data)
	}
}

func TestContextMessagesVecSemanticsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Context{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"messages":[]`)) {
		t.Fatalf("nil messages should marshal as empty Vec, got %s", data)
	}

	var context Context
	if err := json.Unmarshal([]byte(`{"messages":null}`), &context); err == nil {
		t.Fatalf("messages null should be rejected like upstream Vec: %#v", context)
	}
	if err := json.Unmarshal([]byte(`{}`), &context); err != nil || context.Messages == nil {
		t.Fatalf("missing messages should default to empty Vec, context=%#v err=%v", context, err)
	}
}

func TestContextNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	context := Context{SystemPrompt: "system <>&", HasSystemPrompt: true, Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "a < b && c > d"}}}}}
	data, err := marshalJSONNoHTMLEscape(context)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("context JSON should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `system <>&`) || !strings.Contains(text, `a < b && c > d`) {
		t.Fatalf("context JSON should preserve literal strings, got %s", text)
	}
}

func TestMessageMarshalsToolResultRoleLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Message{Role: RoleTool, ToolCallID: "call-1", ToolName: "read", Content: []ContentBlock{{Type: ContentText, Text: "ok"}}, Timestamp: 123})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["role"] != "toolResult" || object["toolCallId"] != "call-1" || object["toolName"] != "read" || object["timestamp"] != float64(123) {
		t.Fatalf("tool result message should serialize like upstream, got %s", data)
	}
	if _, ok := object["name"]; ok {
		t.Fatalf("tool result message should not serialize local name field, got %s", data)
	}
}

func TestMessageMarshalsToolResultRequiredTimestampLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Message{Role: RoleTool, ToolCallID: "call-1", ToolName: "read", Content: []ContentBlock{{Type: ContentText, Text: "ok"}}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["timestamp"] != float64(0) {
		t.Fatalf("tool result message should include required zero timestamp like upstream, got %s", data)
	}
}

func TestMessagePreservesToolResultDetailsValueLikeUpstream(t *testing.T) {
	data := []byte(`{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[{"type":"text","text":"ok"}],"details":["trace"],"isError":false,"timestamp":123}`)
	var output Message
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if output.Role != RoleTool || len(output.DetailsValue.([]any)) != 1 || output.Details != nil {
		t.Fatalf("tool result details value mismatch: %#v", output)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if len(object["details"].([]any)) != 1 {
		t.Fatalf("tool result details value should serialize like upstream, got %s", encoded)
	}
}

func TestMessageToolResultNullDetailsDecodesAsNoneLikeUpstream(t *testing.T) {
	data := []byte(`{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[],"details":null,"isError":false,"timestamp":123}`)
	var output Message
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["details"]; ok {
		t.Fatalf("tool result details:null should re-marshal as omitted None like upstream, got %s", encoded)
	}
}

func TestMessageUnmarshalRejectsMissingToolResultRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"toolCallId", "toolName", "content", "isError", "timestamp"}
	base := map[string]any{
		"role":       "toolResult",
		"toolCallId": "call-1",
		"toolName":   "read",
		"content":    []any{map[string]any{"type": "text", "text": "ok"}},
		"isError":    false,
		"timestamp":  float64(123),
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var output Message
			if err := json.Unmarshal(data, &output); err == nil {
				t.Fatalf("missing toolResult %s should be rejected like upstream: %#v", field, output)
			}
		})
	}
}

func TestMessageUnmarshalRejectsMissingOrUnknownRoleLikeUpstream(t *testing.T) {
	for name, data := range map[string][]byte{
		"missing": []byte(`{"content":"hello","timestamp":123}`),
		"unknown": []byte(`{"role":"custom","content":"hello","timestamp":123}`),
	} {
		t.Run(name, func(t *testing.T) {
			var message Message
			if err := json.Unmarshal(data, &message); err == nil {
				t.Fatalf("%s role should be rejected like upstream tagged enum: %#v", name, message)
			}
		})
	}
}

func TestMessageMarshalRejectsMissingOrUnknownRoleLikeUpstream(t *testing.T) {
	for _, message := range []Message{{}, {Role: Role("custom")}} {
		if data, err := json.Marshal(message); err == nil {
			t.Fatalf("message role %q should not marshal like upstream tagged enum, got %s", message.Role, data)
		}
	}
}

func TestMessageUnmarshalRejectsNullToolResultIsErrorLikeUpstreamBool(t *testing.T) {
	var output Message
	if err := json.Unmarshal([]byte(`{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[],"isError":null,"timestamp":123}`), &output); err == nil {
		t.Fatalf("toolResult isError null should be rejected like upstream bool: %#v", output)
	}
}

func TestMessageUnmarshalRejectsNullTimestampLikeUpstreamI64(t *testing.T) {
	tests := map[string][]byte{
		"user":       []byte(`{"role":"user","content":"hello","timestamp":null}`),
		"assistant":  []byte(`{"role":"assistant","content":[],"api":"openai-responses","provider":"openai","model":"gpt-test","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":null}`),
		"toolResult": []byte(`{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[],"isError":false,"timestamp":null}`),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			var output Message
			if err := json.Unmarshal(data, &output); err == nil {
				t.Fatalf("%s timestamp null should be rejected like upstream i64: %#v", name, output)
			}
		})
	}
}

func TestMessageUnmarshalRejectsNullUserContentLikeUpstreamUntagged(t *testing.T) {
	var output Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":null,"timestamp":123}`), &output); err == nil {
		t.Fatalf("user content null should be rejected like upstream untagged content: %#v", output)
	}
}

func TestMessageUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	tests := map[string][]byte{
		"assistant api":         []byte(`{"role":"assistant","content":[],"api":null,"provider":"openai","model":"gpt-test","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":123}`),
		"assistant provider":    []byte(`{"role":"assistant","content":[],"api":"openai-responses","provider":null,"model":"gpt-test","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":123}`),
		"assistant model":       []byte(`{"role":"assistant","content":[],"api":"openai-responses","provider":"openai","model":null,"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":123}`),
		"toolResult toolCallId": []byte(`{"role":"toolResult","toolCallId":null,"toolName":"read","content":[],"isError":false,"timestamp":123}`),
		"toolResult toolName":   []byte(`{"role":"toolResult","toolCallId":"call-1","toolName":null,"content":[],"isError":false,"timestamp":123}`),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			var output Message
			if err := json.Unmarshal(data, &output); err == nil {
				t.Fatalf("%s null should be rejected like upstream String: %#v", name, output)
			}
		})
	}
}

func TestMessageMarshalsToolResultContentAsUserBlocksLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Message{Role: RoleTool, ToolCallID: "call-1", ToolName: "read", Content: []ContentBlock{
		{Type: ContentThinking, Thinking: "plan"},
		{Type: ContentText, Text: "ok"},
		{Type: ContentToolCall, ToolCall: &ToolCall{ID: "nested", Name: "bad"}},
		{Type: ContentImage, Data: "aW1n", MimeType: "image/png"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	content := object["content"].([]any)
	if len(content) != 2 || content[0].(map[string]any)["type"] != "text" || content[1].(map[string]any)["type"] != "image" {
		t.Fatalf("tool result content should only serialize upstream user blocks, got %s", data)
	}
}

func TestMessageUnmarshalRejectsNonUserContentBlocksLikeUpstream(t *testing.T) {
	tests := map[string][]byte{
		"user thinking":         []byte(`{"role":"user","content":[{"type":"thinking","thinking":"plan"}],"timestamp":123}`),
		"user tool call":        []byte(`{"role":"user","content":[{"type":"toolCall","id":"call-1","name":"read"}],"timestamp":123}`),
		"tool result thinking":  []byte(`{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[{"type":"thinking","thinking":"plan"}],"isError":false,"timestamp":123}`),
		"tool result tool call": []byte(`{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[{"type":"toolCall","id":"call-1","name":"read"}],"isError":false,"timestamp":123}`),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			var message Message
			if err := json.Unmarshal(data, &message); err == nil {
				t.Fatalf("%s should reject non-user content blocks like upstream: %#v", name, message)
			}
		})
	}
}

func TestMessageMarshalsSimpleUserContentAsStringLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Message{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}, Timestamp: 123})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["content"] != "hello" {
		t.Fatalf("simple user content should serialize as upstream string form, got %s", data)
	}
}

func TestMessageNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	cases := map[string]Message{
		"user":   {Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "a < b && c > d"}}},
		"system": {Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "a < b && c > d"}}},
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := marshalJSONNoHTMLEscape(message)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
				t.Fatalf("%s message JSON should match upstream serde_json formatting without HTML escaping, got %s", name, text)
			}
			if !strings.Contains(text, `a < b && c > d`) {
				t.Fatalf("%s message JSON should preserve literal text, got %s", name, text)
			}
		})
	}
}

func TestContentBlocksNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	cases := map[string]any{
		"thinking":  ThinkingContent{Thinking: "a < b && c > d"},
		"userBlock": UserContentBlock{Type: UserContentText, Text: "a < b && c > d"},
		"block":     ContentBlock{Type: ContentThinking, Thinking: "a < b && c > d"},
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := marshalJSONNoHTMLEscape(value)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
				t.Fatalf("%s JSON should match upstream serde_json formatting without HTML escaping, got %s", name, text)
			}
			if !strings.Contains(text, `a < b && c > d`) {
				t.Fatalf("%s JSON should preserve literal text, got %s", name, text)
			}
		})
	}
}

func TestMessageMarshalsUserRequiredTimestampLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Message{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["timestamp"] != float64(0) {
		t.Fatalf("user message should include required zero timestamp like upstream, got %s", data)
	}
}

func TestMessageMarshalsUserContentAsUserBlocksLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Message{Role: RoleUser, Content: []ContentBlock{
		{Type: ContentThinking, Thinking: "plan"},
		{Type: ContentText, Text: "ok"},
		{Type: ContentToolCall, ToolCall: &ToolCall{ID: "nested", Name: "bad"}},
		{Type: ContentImage, Data: "aW1n", MimeType: "image/png"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	content := object["content"].([]any)
	if len(content) != 2 || content[0].(map[string]any)["type"] != "text" || content[1].(map[string]any)["type"] != "image" {
		t.Fatalf("user content should only serialize upstream user blocks, got %s", data)
	}
}

func TestMessageUnmarshalRejectsMissingUserRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"content", "timestamp"}
	base := map[string]any{"role": "user", "content": "hello", "timestamp": float64(123)}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var message Message
			if err := json.Unmarshal(data, &message); err == nil {
				t.Fatalf("missing user %s should be rejected like upstream: %#v", field, message)
			}
		})
	}
}

func TestMessageUnmarshalsSimpleUserContentStringLikeUpstream(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":"hello","timestamp":123}`), &message); err != nil {
		t.Fatal(err)
	}
	if message.Role != RoleUser || message.Timestamp != 123 || len(message.Content) != 1 || message.Content[0].Type != ContentText || message.Content[0].Text != "hello" {
		t.Fatalf("user string content mismatch: %#v", message)
	}
}

func TestMessageMarshalsAssistantRequiredFieldsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Message{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"api", "provider", "model", "usage", "stopReason", "timestamp"} {
		if _, ok := object[field]; !ok {
			t.Fatalf("assistant message should include required %s like upstream, got %s", field, data)
		}
	}
	if object["api"] != "" || object["provider"] != "" || object["model"] != "" || object["stopReason"] != "stop" || object["timestamp"] != float64(0) {
		t.Fatalf("assistant required field defaults mismatch, got %s", data)
	}
}

func TestMessageUnmarshalsUpstreamAssistantUsageAndStopReason(t *testing.T) {
	data := []byte(`{"role":"assistant","content":[{"type":"text","text":"ok"}],"api":"responses","provider":"openai","model":"gpt-test","usage":{"input":3,"output":5,"cacheRead":7,"cacheWrite":11,"totalTokens":26,"cost":{"input":0.1,"output":0.2,"cacheRead":0.3,"cacheWrite":0.4,"total":1.0}},"stopReason":"toolUse","timestamp":123}`)
	var output Message
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if output.Usage == nil {
		t.Fatalf("usage should decode from upstream fields: %#v", output)
	}
	if output.Usage.InputTokens != 3 || output.Usage.OutputTokens != 5 || output.Usage.CacheReadTokens != 7 || output.Usage.CacheWriteTokens != 11 {
		t.Fatalf("usage token mismatch: %#v", output.Usage)
	}
	if output.Usage.Cost == nil || output.Usage.Cost.Input != 0.1 || output.Usage.Cost.Output != 0.2 || output.Usage.Cost.CacheRead != 0.3 || output.Usage.Cost.CacheWrite != 0.4 || output.Usage.Cost.Total != 1.0 {
		t.Fatalf("usage cost mismatch: %#v", output.Usage.Cost)
	}
	if output.StopReason != StopReasonToolCalls {
		t.Fatalf("stop reason mismatch: %q", output.StopReason)
	}
}

func TestMessageUnmarshalRejectsMissingAssistantRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"content", "api", "provider", "model", "usage", "stopReason", "timestamp"}
	base := map[string]any{
		"role":       "assistant",
		"content":    []any{map[string]any{"type": "text", "text": "ok"}},
		"api":        "responses",
		"provider":   "openai",
		"model":      "gpt-test",
		"usage":      map[string]any{"input": 1, "output": 2, "cacheRead": 3, "cacheWrite": 4, "totalTokens": 10, "cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0}},
		"stopReason": "stop",
		"timestamp":  float64(123),
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var output Message
			if err := json.Unmarshal(data, &output); err == nil {
				t.Fatalf("missing assistant %s should be rejected like upstream: %#v", field, output)
			}
		})
	}
}

func TestUsageMarshalsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Usage{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["input"] != float64(1) || object["output"] != float64(2) || object["cacheRead"] != float64(3) || object["cacheWrite"] != float64(4) || object["totalTokens"] != float64(0) {
		t.Fatalf("usage should serialize upstream field names and total, got %s", data)
	}
	if _, ok := object["inputTokens"]; ok {
		t.Fatalf("usage should not serialize local token field names, got %s", data)
	}
	cost := object["cost"].(map[string]any)
	if cost["input"] != float64(0) || cost["output"] != float64(0) || cost["cacheRead"] != float64(0) || cost["cacheWrite"] != float64(0) || cost["total"] != float64(0) {
		t.Fatalf("usage should serialize required zero cost object like upstream, got %s", data)
	}
}

func TestUsageUpstreamFieldAliasesMirrorTokenFields(t *testing.T) {
	usage := Usage{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"input":1`) || !strings.Contains(string(data), `"cacheRead":3`) {
		t.Fatalf("usage aliases should marshal as upstream fields: %s", data)
	}
	encodedUsage := []byte(`{"input":5,"output":6,"cacheRead":7,"cacheWrite":8,"totalTokens":9,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}`)
	if err := json.Unmarshal(encodedUsage, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.Input != 5 || usage.Output != 6 || usage.CacheRead != 7 || usage.CacheWrite != 8 || usage.InputTokens != 5 || usage.OutputTokens != 6 || usage.CacheReadTokens != 7 || usage.CacheWriteTokens != 8 || usage.TotalTokenCount != 9 {
		t.Fatalf("usage upstream aliases should decode: %#v", usage)
	}
}

func TestUsageMarshalRejectsNegativeTokenFieldsLikeUpstreamU64(t *testing.T) {
	tests := map[string]Usage{
		"input":       {InputTokens: -1},
		"output":      {OutputTokens: -1},
		"cacheRead":   {CacheReadTokens: -1},
		"cacheWrite":  {CacheWriteTokens: -1},
		"totalTokens": {HasTotalTokens: true, TotalTokenCount: -1},
	}
	for name, usage := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := json.Marshal(usage); err == nil {
				t.Fatalf("negative usage %s should not marshal like upstream u64", name)
			}
		})
	}
}

func TestUsageUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"input", "output", "cacheRead", "cacheWrite", "totalTokens", "cost"}
	base := map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "totalTokens": 0, "cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0}}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var usage Usage
			if err := json.Unmarshal(data, &usage); err == nil {
				t.Fatalf("missing usage %s should be rejected like upstream: %#v", field, usage)
			}
		})
	}
}

func TestUsageUnmarshalKeepsLegacyTokenFields(t *testing.T) {
	data := []byte(`{"inputTokens":1,"outputTokens":2,"cacheReadTokens":3,"cacheWriteTokens":4,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}`)
	var usage Usage
	if err := json.Unmarshal(data, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 1 || usage.OutputTokens != 2 || usage.CacheReadTokens != 3 || usage.CacheWriteTokens != 4 {
		t.Fatalf("legacy usage token fields should still decode: %#v", usage)
	}
}

func TestUsageUnmarshalUpstreamFieldsOverrideLegacyEvenWhenZero(t *testing.T) {
	data := []byte(`{"inputTokens":7,"outputTokens":8,"cacheReadTokens":9,"cacheWriteTokens":10,"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}`)
	var usage Usage
	if err := json.Unmarshal(data, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.CacheReadTokens != 0 || usage.CacheWriteTokens != 0 {
		t.Fatalf("upstream usage fields should explicitly override legacy fields: %#v", usage)
	}
}

func TestUsageUnmarshalPreservesExplicitZeroTotalTokensLikeUpstream(t *testing.T) {
	data := []byte(`{"input":1,"output":2,"cacheRead":3,"cacheWrite":4,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}`)
	var usage Usage
	if err := json.Unmarshal(data, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens() != 0 {
		t.Fatalf("explicit zero totalTokens should override computed total like upstream: %#v", usage)
	}
}

func TestUsageUnmarshalRejectsNegativeTokenFieldsLikeUpstreamU64(t *testing.T) {
	base := map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "totalTokens": 0, "cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0}}
	for _, field := range []string{"input", "output", "cacheRead", "cacheWrite", "totalTokens"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = -1
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var usage Usage
			if err := json.Unmarshal(data, &usage); err == nil {
				t.Fatalf("negative usage %s should be rejected like upstream u64: %#v", field, usage)
			}
		})
	}
}

func TestUsageUnmarshalRejectsNullNumericFieldsLikeUpstreamU64(t *testing.T) {
	base := map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "totalTokens": 0, "cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0}}
	for _, field := range []string{"input", "output", "cacheRead", "cacheWrite", "totalTokens"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var usage Usage
			if err := json.Unmarshal(data, &usage); err == nil {
				t.Fatalf("usage %s null should be rejected like upstream u64: %#v", field, usage)
			}
		})
	}
}

func TestUsageUnmarshalPreservesTotalTokensLikeUpstream(t *testing.T) {
	var usage Usage
	data := []byte(`{"input":3,"output":4,"cacheRead":5,"cacheWrite":6,"totalTokens":18,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}`)
	if err := json.Unmarshal(data, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 3 || usage.OutputTokens != 4 || usage.CacheReadTokens != 5 || usage.CacheWriteTokens != 6 || usage.TotalTokenCount != 18 || !usage.HasTotalTokens {
		t.Fatalf("usage totals mismatch: %#v", usage)
	}
}

func TestUsageCostUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"input", "output", "cacheRead", "cacheWrite", "total"}
	base := map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var cost UsageCost
			if err := json.Unmarshal(data, &cost); err == nil {
				t.Fatalf("missing usage cost %s should be rejected like upstream: %#v", field, cost)
			}
		})
	}
}

func TestUsageCostUnmarshalRejectsNullNumericFieldsLikeUpstreamF64(t *testing.T) {
	base := map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0}
	for _, field := range []string{"input", "output", "cacheRead", "cacheWrite", "total"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var cost UsageCost
			if err := json.Unmarshal(data, &cost); err == nil {
				t.Fatalf("usage cost %s null should be rejected like upstream f64: %#v", field, cost)
			}
		})
	}
}

func TestStopReasonMarshalsLikeUpstream(t *testing.T) {
	tests := map[StopReason]string{
		StopReasonEndTurn:   `"stop"`,
		StopReasonMaxTokens: `"length"`,
		StopReasonToolCalls: `"toolUse"`,
		StopReasonError:     `"error"`,
		StopReasonAborted:   `"aborted"`,
	}
	for reason, want := range tests {
		data, err := json.Marshal(reason)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s should marshal as %s like upstream, got %s", reason, want, data)
		}
	}
}

func TestStopReasonUnmarshalRejectsUnknownValueLikeUpstream(t *testing.T) {
	var reason StopReason
	if err := json.Unmarshal([]byte(`"future"`), &reason); err == nil {
		t.Fatalf("unknown stop reason should be rejected like upstream enum: %q", reason)
	}
}

func TestStopReasonMarshalRejectsUnknownValueLikeUpstream(t *testing.T) {
	if _, err := json.Marshal(StopReason("future")); err == nil {
		t.Fatal("unknown stop reason should not marshal like upstream enum")
	}
}

func TestImagesStopReasonUnmarshalRejectsUnknownValueLikeUpstream(t *testing.T) {
	var reason ImagesStopReason
	if err := json.Unmarshal([]byte(`"future"`), &reason); err == nil {
		t.Fatalf("unknown images stop reason should be rejected like upstream enum: %q", reason)
	}
}

func TestImagesStopReasonMarshalRejectsUnknownValueLikeUpstream(t *testing.T) {
	if _, err := json.Marshal(ImagesStopReason("future")); err == nil {
		t.Fatal("unknown images stop reason should not marshal like upstream enum")
	}
}

func TestMessageUnmarshalsUpstreamAssistantDiagnostics(t *testing.T) {
	data := []byte(`{"role":"assistant","content":[],"api":"responses","provider":"openai","model":"gpt-test","diagnostics":[{"kind":"trace","value":1}],"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":123}`)
	var output Message
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Diagnostics) != 1 || output.Diagnostics[0].(map[string]any)["kind"] != "trace" {
		t.Fatalf("diagnostics should round-trip from upstream wire: %#v", output.Diagnostics)
	}
}

func TestMessageRoundTripPreservesAssistantExplicitEmptyOptionalFieldsLikeUpstream(t *testing.T) {
	input := []byte(`{"role":"assistant","content":[],"api":"responses","provider":"openai","model":"gpt-test","responseModel":"","responseId":"","diagnostics":[],"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","errorMessage":"","timestamp":123}`)
	var message Message
	if err := json.Unmarshal(input, &message); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"responseModel", "responseId", "errorMessage"} {
		value, ok := object[field].(string)
		if !ok || value != "" {
			t.Fatalf("assistant explicit empty %s should round-trip like upstream Some(\"\"), got %s", field, data)
		}
	}
	if diagnostics, ok := object["diagnostics"].([]any); !ok || len(diagnostics) != 0 {
		t.Fatalf("assistant explicit empty diagnostics should round-trip like upstream Some([]), got %s", data)
	}
}

func TestCacheRetentionJSONMatchesUpstreamValues(t *testing.T) {
	data, err := json.Marshal(CacheLong)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"long"` {
		t.Fatalf("cache retention should marshal using upstream value, got %s", data)
	}

	var retention CacheRetention
	if err := json.Unmarshal([]byte(`"short"`), &retention); err != nil {
		t.Fatal(err)
	}
	if retention != CacheShort {
		t.Fatalf("cache retention mismatch: %q", retention)
	}
}

func TestCacheRetentionJSONRejectsNonUpstreamValues(t *testing.T) {
	if _, err := json.Marshal(CacheEphemeral); err == nil {
		t.Fatal("ephemeral cache retention should not marshal as upstream ai CacheRetention")
	}
	for _, value := range []string{"ephemeral", "unknown"} {
		t.Run(value, func(t *testing.T) {
			var retention CacheRetention
			if err := json.Unmarshal([]byte(strconv.Quote(value)), &retention); err == nil {
				t.Fatalf("cache retention %q should be rejected like upstream", value)
			}
		})
	}
}

func TestStreamOptionsFromSimplePreservesBaseLikeUpstream(t *testing.T) {
	baseRetries := 2
	overrideTemperature := 0.7
	options := StreamOptionsFromSimple(SimpleStreamOptions{
		Base: StreamOptions{
			APIKey:         "base-key",
			MaxRetries:     &baseRetries,
			ProviderExtras: map[string]any{"service_tier": "priority"},
		},
		ThinkingLevel: ThinkingHigh,
	})
	if options.APIKey != "base-key" || options.MaxRetries == nil || *options.MaxRetries != 2 || options.ProviderExtras["service_tier"] != "priority" {
		t.Fatalf("base stream options were not preserved: %#v", options)
	}
	options.Temperature = &overrideTemperature
	options.Transport = TransportWebsocketCached
	if options.Temperature == nil || *options.Temperature != overrideTemperature || options.Transport != TransportWebsocketCached {
		t.Fatalf("simple stream options should override base fields: %#v", options)
	}
}

func TestThinkingLevelJSONMatchesUpstream(t *testing.T) {
	data, err := json.Marshal(ThinkingXHigh)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"xhigh"` {
		t.Fatalf("thinking level json mismatch: %s", data)
	}

	var level ThinkingLevel
	if err := json.Unmarshal([]byte(`"minimal"`), &level); err != nil {
		t.Fatal(err)
	}
	if level != ThinkingMinimal {
		t.Fatalf("thinking level mismatch: %q", level)
	}
}

func TestThinkingLevelJSONRejectsOffAndUnknownLikeUpstream(t *testing.T) {
	for _, level := range []ThinkingLevel{ThinkingOff, ThinkingLevel("turbo")} {
		if data, err := json.Marshal(level); err == nil {
			t.Fatalf("thinking level %q should not marshal like upstream enum, got %s", level, data)
		}
	}
	for _, value := range []string{"off", "turbo"} {
		t.Run(value, func(t *testing.T) {
			var level ThinkingLevel
			if err := json.Unmarshal([]byte(strconv.Quote(value)), &level); err == nil {
				t.Fatalf("thinking level %q should be rejected like upstream enum: %q", value, level)
			}
		})
	}
}

func TestTranslateBaseStreamOptionsMatchesUpstreamHelper(t *testing.T) {
	maxRetries := 3
	simple := SimpleStreamOptions{
		Base:          StreamOptions{APIKey: "base-key", MaxRetries: &maxRetries, ProviderExtras: map[string]any{"service_tier": "priority"}},
		ThinkingLevel: ThinkingHigh,
	}
	options := TranslateBaseStreamOptions(Model{ID: "m"}, simple)
	if options.APIKey != "base-key" || options.MaxRetries == nil || *options.MaxRetries != 3 || options.ProviderExtras["service_tier"] != "priority" {
		t.Fatalf("translated options mismatch: %#v", options)
	}
	if got := TranslateBase(Model{ID: "m"}, simple); !reflect.DeepEqual(got, options) {
		t.Fatalf("upstream translate_base wrapper mismatch: %#v want %#v", got, options)
	}
	if _, ok := options.ProviderExtras["thinking_level"]; ok {
		t.Fatalf("base translator should drop simple thinking fields like upstream: %#v", options.ProviderExtras)
	}
}

func TestStreamOptionsTypesDoNotExposeBaseURLLikeUpstream(t *testing.T) {
	if _, ok := reflect.TypeOf(StreamOptions{}).FieldByName("BaseURL"); ok {
		t.Fatal("StreamOptions should not expose BaseURL like upstream")
	}
	if _, ok := reflect.TypeOf(SimpleStreamOptions{}).FieldByName("BaseURL"); ok {
		t.Fatal("SimpleStreamOptions should not expose BaseURL like upstream")
	}
}

func TestStreamOptionsDoesNotExposeSimpleThinkingFieldsLikeUpstream(t *testing.T) {
	streamType := reflect.TypeOf(StreamOptions{})
	for _, field := range []string{"ThinkingLevel", "ThinkingBudgets"} {
		if _, ok := streamType.FieldByName(field); ok {
			t.Fatalf("StreamOptions should not expose %s like upstream", field)
		}
	}
}

func TestStreamOptionsDoesNotExposeCallbackFieldsLikeUpstream(t *testing.T) {
	streamType := reflect.TypeOf(StreamOptions{})
	for _, field := range []string{"OnPayload", "OnResponse"} {
		if _, ok := streamType.FieldByName(field); ok {
			t.Fatalf("StreamOptions should not expose %s like upstream", field)
		}
	}
}

func TestStreamOptionsExposesAbortLikeUpstream(t *testing.T) {
	streamType := reflect.TypeOf(StreamOptions{})
	field, ok := streamType.FieldByName("Abort")
	if !ok {
		t.Fatal("StreamOptions should expose Abort like upstream")
	}
	if field.Type != reflect.TypeOf((<-chan struct{})(nil)) {
		t.Fatalf("StreamOptions Abort should be a receive-only cancellation channel, got %s", field.Type)
	}
}

func TestSimpleStreamOptionsShapeMatchesUpstream(t *testing.T) {
	simpleType := reflect.TypeOf(SimpleStreamOptions{})
	if simpleType.NumField() != 4 {
		t.Fatalf("SimpleStreamOptions should expose only upstream fields, got %d", simpleType.NumField())
	}
	for _, field := range []string{"Base", "Reasoning", "ThinkingLevel", "ThinkingBudgets"} {
		if _, ok := simpleType.FieldByName(field); !ok {
			t.Fatalf("SimpleStreamOptions missing upstream field %s", field)
		}
	}
}

func TestSimpleStreamOptionsReasoningAliasMatchesThinkingLevel(t *testing.T) {
	options := SimpleStreamOptions{Reasoning: ThinkingHigh}
	if options.Reasoning != ThinkingHigh {
		t.Fatalf("reasoning alias mismatch: %#v", options)
	}
}

func TestTransportJSONMatchesUpstreamKebabCase(t *testing.T) {
	data, err := json.Marshal(TransportWebsocketCached)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"websocket-cached"` {
		t.Fatalf("transport should marshal using upstream kebab-case, got %s", data)
	}

	var transport Transport
	if err := json.Unmarshal([]byte(`"websocket-cached"`), &transport); err != nil {
		t.Fatal(err)
	}
	if transport != TransportWebsocketCached {
		t.Fatalf("transport mismatch: %q", transport)
	}
}

func TestTransportJSONRejectsNonUpstreamValues(t *testing.T) {
	if _, err := json.Marshal(TransportHTTP); err == nil {
		t.Fatal("http transport should not marshal as upstream ai Transport")
	}
	for _, value := range []string{"http", "websocket_cached", "unknown"} {
		t.Run(value, func(t *testing.T) {
			var transport Transport
			if err := json.Unmarshal([]byte(strconv.Quote(value)), &transport); err == nil {
				t.Fatalf("transport %q should be rejected like upstream", value)
			}
		})
	}
}

func TestThinkingBudgetsJSONPreservesExplicitZeroLikeUpstreamOption(t *testing.T) {
	var budgets ThinkingBudgets
	if err := json.Unmarshal([]byte(`{"minimal":0,"low":1024}`), &budgets); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(budgets)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["minimal"] != float64(0) || object["low"] != float64(1024) {
		t.Fatalf("thinking budgets should preserve explicit Some(0) like upstream Option, got %s", data)
	}
	if _, ok := object["medium"]; ok {
		t.Fatalf("missing budget should remain omitted like None, got %s", data)
	}
}

func TestThinkingBudgetsBudgetForPreservesExplicitZeroLikeUpstreamOption(t *testing.T) {
	var budgets ThinkingBudgets
	if err := json.Unmarshal([]byte(`{"low":0}`), &budgets); err != nil {
		t.Fatal(err)
	}
	budget, ok := budgets.BudgetFor(ThinkingLow)
	if !ok || budget != 0 {
		t.Fatalf("explicit zero budget should be present like upstream Option, budget=%d ok=%v", budget, ok)
	}
}

func TestThinkingBudgetsUnmarshalRejectsNegativeValuesLikeUpstreamU32(t *testing.T) {
	for _, field := range []string{"minimal", "low", "medium", "high"} {
		t.Run(field, func(t *testing.T) {
			data := []byte(fmt.Sprintf(`{"%s":-1}`, field))
			var budgets ThinkingBudgets
			if err := json.Unmarshal(data, &budgets); err == nil {
				t.Fatalf("negative %s budget should be rejected like upstream u32: %#v", field, budgets)
			}
		})
	}
}

func TestThinkingBudgetsMarshalRejectsNegativeValuesLikeUpstreamU32(t *testing.T) {
	tests := map[string]ThinkingBudgets{
		"minimal": {Minimal: -1},
		"low":     {Low: -1},
		"medium":  {Medium: -1},
		"high":    {High: -1},
	}
	for name, budgets := range tests {
		t.Run(name, func(t *testing.T) {
			if data, err := json.Marshal(budgets); err == nil {
				t.Fatalf("negative %s budget should not marshal like upstream u32, got %s", name, data)
			}
		})
	}
}

func TestContentBlockTextSignatureJSONRoundTrip(t *testing.T) {
	input := ContentBlock{Type: ContentText, Text: "hi", TextSignature: "sig-text"}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output ContentBlock
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if output.TextSignature != "sig-text" {
		t.Fatalf("text signature mismatch: %#v json=%s", output, data)
	}
}

func TestContentTextConstructorsMatchUpstream(t *testing.T) {
	block := NewTextContentBlock("hello")
	if block.Type != ContentText || block.Text != "hello" || block.TextSignature != "" || block.textSignaturePresent {
		t.Fatalf("content text constructor mismatch: %#v", block)
	}

	userBlock := NewTextUserContentBlock("hello")
	if userBlock.Type != UserContentText || userBlock.Text != "hello" || userBlock.TextSignature != "" || userBlock.textSignaturePresent {
		t.Fatalf("user content text constructor mismatch: %#v", userBlock)
	}
}

func TestContentBlockRoundTripPreservesExplicitEmptySignaturesLikeUpstream(t *testing.T) {
	input := []byte(`{"type":"thinking","thinking":"plan","thinkingSignature":"","redacted":false}`)
	var block ContentBlock
	if err := json.Unmarshal(input, &block); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if signature, ok := object["thinkingSignature"].(string); !ok || signature != "" {
		t.Fatalf("explicit empty thinkingSignature should round-trip like upstream Some(\"\"), got %s", data)
	}
}

func TestContentBlockUnmarshalRejectsNullThinkingRedactedLikeUpstreamBool(t *testing.T) {
	var block ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"thinking","thinking":"plan","redacted":null}`), &block); err == nil {
		t.Fatalf("thinking content block redacted null should be rejected like upstream bool: %#v", block)
	}
}

func TestContentBlockUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	tests := map[string][]byte{
		"text":       []byte(`{"type":"text","text":null}`),
		"thinking":   []byte(`{"type":"thinking","thinking":null}`),
		"image data": []byte(`{"type":"image","data":null,"mimeType":"image/png"}`),
		"image mime": []byte(`{"type":"image","data":"aW1n","mimeType":null}`),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			var block ContentBlock
			if err := json.Unmarshal(data, &block); err == nil {
				t.Fatalf("%s null should be rejected like upstream String: %#v", name, block)
			}
		})
	}
}

func TestTextSignatureV1JSONMatchesUpstream(t *testing.T) {
	input := TextSignatureV1{V: 1, ID: "sig-1", Phase: TextSignatureFinalAnswer}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"v":1,"id":"sig-1","phase":"final_answer"}` {
		t.Fatalf("text signature json mismatch: %s", data)
	}

	var output TextSignatureV1
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if output.V != 1 || output.ID != "sig-1" || output.Phase != TextSignatureFinalAnswer {
		t.Fatalf("text signature mismatch: %#v", output)
	}
}

func TestTextSignatureV1OmitsEmptyPhaseLikeUpstream(t *testing.T) {
	data, err := json.Marshal(TextSignatureV1{V: 1, ID: "sig-1"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"v":1,"id":"sig-1"}` {
		t.Fatalf("empty phase should be omitted like upstream Option, got %s", data)
	}
}

func TestTextSignatureV1UnmarshalAcceptsNullPhaseLikeUpstreamOption(t *testing.T) {
	var signature TextSignatureV1
	if err := json.Unmarshal([]byte(`{"v":1,"id":"sig-1","phase":null}`), &signature); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(signature)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"v":1,"id":"sig-1"}` {
		t.Fatalf("null phase should round-trip as omitted None like upstream, got %s", data)
	}
}

func TestTextSignatureV1UnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	for _, field := range []string{"v", "id"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{"v": 1, "id": "sig-1"}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var signature TextSignatureV1
			if err := json.Unmarshal(data, &signature); err == nil {
				t.Fatalf("missing text signature %s should be rejected like upstream: %#v", field, signature)
			}
		})
	}
}

func TestTextSignatureV1UnmarshalRejectsNullRequiredFieldsLikeUpstream(t *testing.T) {
	for _, field := range []string{"v", "id"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{"v": 1, "id": "sig-1"}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var signature TextSignatureV1
			if err := json.Unmarshal(data, &signature); err == nil {
				t.Fatalf("text signature %s null should be rejected like upstream: %#v", field, signature)
			}
		})
	}
}

func TestTextSignaturePhaseJSONRejectsUnknownValueLikeUpstream(t *testing.T) {
	if _, err := json.Marshal(TextSignaturePhase("draft")); err == nil {
		t.Fatal("unknown text signature phase should not marshal like upstream enum")
	}
	var phase TextSignaturePhase
	if err := json.Unmarshal([]byte(`"draft"`), &phase); err == nil {
		t.Fatalf("unknown text signature phase should be rejected like upstream enum: %q", phase)
	}
}

func TestContentStructJSONMatchesUpstream(t *testing.T) {
	textData, err := json.Marshal(TextContent{})
	if err != nil {
		t.Fatal(err)
	}
	if string(textData) != `{"text":""}` {
		t.Fatalf("text content required defaults mismatch: %s", textData)
	}
	textData, err = json.Marshal(TextContent{Text: "hello", TextSignature: "sig-text"})
	if err != nil {
		t.Fatal(err)
	}
	if string(textData) != `{"text":"hello","textSignature":"sig-text"}` {
		t.Fatalf("text content signature json mismatch: %s", textData)
	}

	thinkingData, err := json.Marshal(ThinkingContent{})
	if err != nil {
		t.Fatal(err)
	}
	if string(thinkingData) != `{"thinking":""}` {
		t.Fatalf("thinking content required defaults mismatch: %s", thinkingData)
	}
	thinkingData, err = json.Marshal(ThinkingContent{Thinking: "plan", ThinkingSignature: "sig-thinking", Redacted: true})
	if err != nil {
		t.Fatal(err)
	}
	var thinkingObject map[string]any
	if err := json.Unmarshal(thinkingData, &thinkingObject); err != nil {
		t.Fatal(err)
	}
	if thinkingObject["thinking"] != "plan" || thinkingObject["thinkingSignature"] != "sig-thinking" || thinkingObject["redacted"] != true {
		t.Fatalf("thinking content json mismatch: %#v", thinkingObject)
	}

	imageData, err := json.Marshal(ImageContent{Data: "aW1n", MimeType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if string(imageData) != `{"data":"aW1n","mimeType":"image/png"}` {
		t.Fatalf("image content json mismatch: %s", imageData)
	}
}

func TestContentStructRoundTripPreservesExplicitEmptySignaturesLikeUpstream(t *testing.T) {
	var text TextContent
	if err := json.Unmarshal([]byte(`{"text":"hello","textSignature":""}`), &text); err != nil {
		t.Fatal(err)
	}
	textData, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	var textObject map[string]any
	if err := json.Unmarshal(textData, &textObject); err != nil {
		t.Fatal(err)
	}
	if signature, ok := textObject["textSignature"].(string); !ok || signature != "" {
		t.Fatalf("explicit empty textSignature should round-trip like upstream Some(\"\"), got %s", textData)
	}

	var thinking ThinkingContent
	if err := json.Unmarshal([]byte(`{"thinking":"plan","thinkingSignature":""}`), &thinking); err != nil {
		t.Fatal(err)
	}
	thinkingData, err := json.Marshal(thinking)
	if err != nil {
		t.Fatal(err)
	}
	var thinkingObject map[string]any
	if err := json.Unmarshal(thinkingData, &thinkingObject); err != nil {
		t.Fatal(err)
	}
	if signature, ok := thinkingObject["thinkingSignature"].(string); !ok || signature != "" {
		t.Fatalf("explicit empty thinkingSignature should round-trip like upstream Some(\"\"), got %s", thinkingData)
	}
}

func TestContentStructUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	tests := map[string]struct {
		data   []byte
		decode func([]byte) error
	}{
		"text text":         {data: []byte(`{}`), decode: func(data []byte) error { var value TextContent; return json.Unmarshal(data, &value) }},
		"thinking thinking": {data: []byte(`{}`), decode: func(data []byte) error { var value ThinkingContent; return json.Unmarshal(data, &value) }},
		"image data":        {data: []byte(`{"mimeType":"image/png"}`), decode: func(data []byte) error { var value ImageContent; return json.Unmarshal(data, &value) }},
		"image mimeType":    {data: []byte(`{"data":"aW1n"}`), decode: func(data []byte) error { var value ImageContent; return json.Unmarshal(data, &value) }},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := tt.decode(tt.data); err == nil {
				t.Fatalf("%s missing required field should be rejected like upstream", name)
			}
		})
	}
}

func TestThinkingContentUnmarshalRejectsNullRedactedLikeUpstreamBool(t *testing.T) {
	var content ThinkingContent
	if err := json.Unmarshal([]byte(`{"thinking":"plan","redacted":null}`), &content); err == nil {
		t.Fatalf("thinking redacted null should be rejected like upstream bool: %#v", content)
	}
}

func TestContentStructUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	tests := map[string]struct {
		data   []byte
		decode func([]byte) error
	}{
		"text":       {data: []byte(`{"text":null}`), decode: func(data []byte) error { var value TextContent; return json.Unmarshal(data, &value) }},
		"thinking":   {data: []byte(`{"thinking":null}`), decode: func(data []byte) error { var value ThinkingContent; return json.Unmarshal(data, &value) }},
		"image data": {data: []byte(`{"data":null,"mimeType":"image/png"}`), decode: func(data []byte) error { var value ImageContent; return json.Unmarshal(data, &value) }},
		"image mime": {data: []byte(`{"data":"aW1n","mimeType":null}`), decode: func(data []byte) error { var value ImageContent; return json.Unmarshal(data, &value) }},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if err := tt.decode(tt.data); err == nil {
				t.Fatalf("%s null should be rejected like upstream String", name)
			}
		})
	}
}

func TestUserContentBlockJSONMatchesUpstream(t *testing.T) {
	textData, err := json.Marshal(UserContentBlock{Type: UserContentText, Text: "hello", TextSignature: "sig-text"})
	if err != nil {
		t.Fatal(err)
	}
	var textObject map[string]any
	if err := json.Unmarshal(textData, &textObject); err != nil {
		t.Fatal(err)
	}
	if textObject["type"] != "text" || textObject["text"] != "hello" || textObject["textSignature"] != "sig-text" {
		t.Fatalf("user text content block json mismatch: %#v", textObject)
	}

	imageData, err := json.Marshal(UserContentBlock{Type: UserContentImage, Data: "aW1n", MimeType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	var imageObject map[string]any
	if err := json.Unmarshal(imageData, &imageObject); err != nil {
		t.Fatal(err)
	}
	if imageObject["type"] != "image" || imageObject["data"] != "aW1n" || imageObject["mimeType"] != "image/png" {
		t.Fatalf("user image content block json mismatch: %#v", imageObject)
	}

	var block UserContentBlock
	if err := json.Unmarshal([]byte(`{"type":"image","data":"aW1n","mimeType":"image/png"}`), &block); err != nil {
		t.Fatal(err)
	}
	if block.Type != UserContentImage || block.Data != "aW1n" || block.MimeType != "image/png" {
		t.Fatalf("user content block mismatch: %#v", block)
	}
}

func TestUserContentBlockRoundTripPreservesExplicitEmptyTextSignatureLikeUpstream(t *testing.T) {
	data := []byte(`{"type":"text","text":"hello","textSignature":""}`)
	var block UserContentBlock
	if err := json.Unmarshal(data, &block); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if signature, ok := object["textSignature"].(string); !ok || signature != "" {
		t.Fatalf("explicit empty user textSignature should round-trip like upstream Some(\"\"), got %s", encoded)
	}
}

func TestUserContentBlockUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	tests := map[string][]byte{
		"text":       []byte(`{"type":"text","text":null}`),
		"image data": []byte(`{"type":"image","data":null,"mimeType":"image/png"}`),
		"image mime": []byte(`{"type":"image","data":"aW1n","mimeType":null}`),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			var block UserContentBlock
			if err := json.Unmarshal(data, &block); err == nil {
				t.Fatalf("%s null should be rejected like upstream String: %#v", name, block)
			}
		})
	}
}

func TestUserContentBlockRejectsNonUserBlocksLikeUpstream(t *testing.T) {
	if _, err := json.Marshal(UserContentBlock{Type: UserContentBlockType("thinking")}); err == nil {
		t.Fatal("thinking should not marshal as upstream user content block")
	}
	var block UserContentBlock
	if err := json.Unmarshal([]byte(`{"type":"thinking","thinking":"plan"}`), &block); err == nil {
		t.Fatalf("thinking should be rejected like upstream user content block: %#v", block)
	}
}

func TestUserContentJSONMatchesUpstreamUntaggedUnion(t *testing.T) {
	textData, err := json.Marshal(UserContent{Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if string(textData) != `"hello"` {
		t.Fatalf("user content text json mismatch: %s", textData)
	}

	blocksData, err := json.Marshal(UserContent{Blocks: []UserContentBlock{{Type: UserContentText, Text: "hello"}}})
	if err != nil {
		t.Fatal(err)
	}
	var blockObjects []map[string]any
	if err := json.Unmarshal(blocksData, &blockObjects); err != nil {
		t.Fatal(err)
	}
	if len(blockObjects) != 1 || blockObjects[0]["type"] != "text" || blockObjects[0]["text"] != "hello" {
		t.Fatalf("user content blocks json mismatch: %#v", blockObjects)
	}

	var text UserContent
	if err := json.Unmarshal([]byte(`"hello"`), &text); err != nil {
		t.Fatal(err)
	}
	if text.Text != "hello" || text.Blocks != nil {
		t.Fatalf("user text content mismatch: %#v", text)
	}

	var blocks UserContent
	if err := json.Unmarshal([]byte(`[{"type":"text","text":"hello"}]`), &blocks); err != nil {
		t.Fatal(err)
	}
	if blocks.Text != "" || len(blocks.Blocks) != 1 || blocks.Blocks[0].Text != "hello" {
		t.Fatalf("user blocks content mismatch: %#v", blocks)
	}
}

func TestUserContentNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	cases := map[string]any{
		"text":   UserContent{Text: "a < b && c > d"},
		"blocks": UserContent{Blocks: []UserContentBlock{{Type: UserContentText, Text: "a < b && c > d"}}},
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := marshalJSONNoHTMLEscape(value)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
				t.Fatalf("%s user content JSON should match upstream serde_json formatting without HTML escaping, got %s", name, text)
			}
			if !strings.Contains(text, `a < b && c > d`) {
				t.Fatalf("%s user content JSON should preserve literal text, got %s", name, text)
			}
		})
	}
}

func TestUserContentUnmarshalRejectsNullLikeUpstreamUntagged(t *testing.T) {
	var content UserContent
	if err := json.Unmarshal([]byte(`null`), &content); err == nil {
		t.Fatalf("user content null should be rejected like upstream untagged union: %#v", content)
	}
}

func TestUserMessageJSONMatchesUpstream(t *testing.T) {
	data, err := json.Marshal(UserMessage{Role: UserRoleUser, Content: UserContent{Text: "hello"}, Timestamp: 123})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["role"]; ok {
		t.Fatalf("user message should skip inner role like upstream, got %s", data)
	}
	if object["content"] != "hello" || object["timestamp"] != float64(123) {
		t.Fatalf("user message json mismatch: %#v", object)
	}

	var output UserMessage
	if err := json.Unmarshal([]byte(`{"role":"user","content":"hello","timestamp":123}`), &output); err != nil {
		t.Fatal(err)
	}
	if output.Role != UserRoleUser || output.Content.Text != "hello" || output.Timestamp != 123 {
		t.Fatalf("user message mismatch: %#v", output)
	}
}

func TestDirectMessageNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	cases := map[string]any{
		"user":       UserMessage{Content: UserContent{Text: "a < b && c > d"}, Timestamp: 123},
		"toolResult": ToolResultMessage{ToolCallID: "call-1", ToolName: "read", Content: []UserContentBlock{{Type: UserContentText, Text: "a < b && c > d"}}, IsError: false, Timestamp: 123},
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := marshalJSONNoHTMLEscape(value)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
				t.Fatalf("%s message JSON should match upstream serde_json formatting without HTML escaping, got %s", name, text)
			}
			if !strings.Contains(text, `a < b && c > d`) {
				t.Fatalf("%s message JSON should preserve literal text, got %s", name, text)
			}
		})
	}
}

func TestUserMessageUnmarshalDefaultsRoleLikeUpstream(t *testing.T) {
	var output UserMessage
	if err := json.Unmarshal([]byte(`{"content":"hello","timestamp":123}`), &output); err != nil {
		t.Fatal(err)
	}
	if output.Role != UserRoleUser {
		t.Fatalf("user message role should default to user like upstream: %#v", output)
	}
}

func TestUserMessageUnmarshalRejectsNullTimestampLikeUpstreamI64(t *testing.T) {
	var output UserMessage
	if err := json.Unmarshal([]byte(`{"content":"hello","timestamp":null}`), &output); err == nil {
		t.Fatalf("user timestamp null should be rejected like upstream i64: %#v", output)
	}
}

func TestUserMessageUnmarshalRejectsNullContentLikeUpstreamUntagged(t *testing.T) {
	var output UserMessage
	if err := json.Unmarshal([]byte(`{"content":null,"timestamp":123}`), &output); err == nil {
		t.Fatalf("user content null should be rejected like upstream untagged content: %#v", output)
	}
}

func TestUserRoleJSONRejectsUnknownValueLikeUpstream(t *testing.T) {
	if _, err := json.Marshal(UserRole("assistant")); err == nil {
		t.Fatal("unknown user role should not marshal like upstream enum")
	}
	var role UserRole
	if err := json.Unmarshal([]byte(`"assistant"`), &role); err == nil {
		t.Fatalf("unknown user role should be rejected like upstream enum: %q", role)
	}
}

func TestUserMessageMarshalRejectsUnknownRoleLikeUpstream(t *testing.T) {
	if data, err := json.Marshal(UserMessage{Role: UserRole("assistant"), Content: UserContent{Text: "hello"}, Timestamp: 123}); err == nil {
		t.Fatalf("user message with unknown role should not marshal like upstream enum, got %s", data)
	}
}

func TestToolResultMessageJSONMatchesUpstream(t *testing.T) {
	data, err := json.Marshal(ToolResultMessage{Role: ToolResultRoleToolResult, ToolCallID: "call-1", ToolName: "read", Content: []UserContentBlock{{Type: UserContentText, Text: "ok"}}, Details: map[string]any{"trace": "yes"}, IsError: true, Timestamp: 123})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["role"]; ok {
		t.Fatalf("tool result message should skip inner role like upstream, got %s", data)
	}
	if object["toolCallId"] != "call-1" || object["toolName"] != "read" || object["isError"] != true || object["timestamp"] != float64(123) {
		t.Fatalf("tool result message json mismatch: %#v", object)
	}
	if details, ok := object["details"].(map[string]any); !ok || details["trace"] != "yes" {
		t.Fatalf("tool result details mismatch: %#v", object["details"])
	}

	var output ToolResultMessage
	if err := json.Unmarshal([]byte(`{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[{"type":"text","text":"ok"}],"isError":true,"timestamp":123}`), &output); err != nil {
		t.Fatal(err)
	}
	if output.Role != ToolResultRoleToolResult || output.ToolCallID != "call-1" || output.ToolName != "read" || len(output.Content) != 1 || output.Content[0].Text != "ok" || !output.IsError || output.Timestamp != 123 {
		t.Fatalf("tool result message mismatch: %#v", output)
	}
}

func TestToolResultMessageNullDetailsDecodesAsNoneLikeUpstream(t *testing.T) {
	data := []byte(`{"toolCallId":"call-1","toolName":"read","content":[],"details":null,"isError":false,"timestamp":123}`)
	var message ToolResultMessage
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["details"]; ok {
		t.Fatalf("tool result message details:null should re-marshal as omitted None like upstream, got %s", encoded)
	}
}

func TestToolResultMessageUnmarshalDefaultsRoleLikeUpstream(t *testing.T) {
	var output ToolResultMessage
	if err := json.Unmarshal([]byte(`{"toolCallId":"call-1","toolName":"read","content":[],"isError":false,"timestamp":123}`), &output); err != nil {
		t.Fatal(err)
	}
	if output.Role != ToolResultRoleToolResult {
		t.Fatalf("tool result role should default to toolResult like upstream: %#v", output)
	}
}

func TestToolResultMessageContentUsesVecSemanticsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ToolResultMessage{ToolCallID: "call-1", ToolName: "read"})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	content, ok := object["content"].([]any)
	if !ok || len(content) != 0 {
		t.Fatalf("tool result nil content should marshal as empty array like upstream Vec, got %s", data)
	}
	var output ToolResultMessage
	if err := json.Unmarshal([]byte(`{"toolCallId":"call-1","toolName":"read","content":null,"isError":false,"timestamp":123}`), &output); err == nil {
		t.Fatalf("tool result null content should be rejected like upstream Vec: %#v", output)
	}
}

func TestToolResultMessageUnmarshalRejectsNullIsErrorLikeUpstreamBool(t *testing.T) {
	var message ToolResultMessage
	if err := json.Unmarshal([]byte(`{"toolCallId":"call-1","toolName":"read","content":[],"isError":null,"timestamp":123}`), &message); err == nil {
		t.Fatalf("tool result isError null should be rejected like upstream bool: %#v", message)
	}
}

func TestToolResultMessageUnmarshalRejectsNullTimestampLikeUpstreamI64(t *testing.T) {
	var message ToolResultMessage
	if err := json.Unmarshal([]byte(`{"toolCallId":"call-1","toolName":"read","content":[],"isError":false,"timestamp":null}`), &message); err == nil {
		t.Fatalf("tool result timestamp null should be rejected like upstream i64: %#v", message)
	}
}

func TestToolResultMessageUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	for _, field := range []string{"toolCallId", "toolName"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{"toolCallId": "call-1", "toolName": "read", "content": []any{}, "isError": false, "timestamp": 123}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var message ToolResultMessage
			if err := json.Unmarshal(data, &message); err == nil {
				t.Fatalf("tool result %s null should be rejected like upstream String: %#v", field, message)
			}
		})
	}
}

func TestToolResultRoleJSONRejectsUnknownValueLikeUpstream(t *testing.T) {
	if _, err := json.Marshal(ToolResultRole("tool")); err == nil {
		t.Fatal("unknown tool result role should not marshal like upstream enum")
	}
	var role ToolResultRole
	if err := json.Unmarshal([]byte(`"tool"`), &role); err == nil {
		t.Fatalf("unknown tool result role should be rejected like upstream enum: %q", role)
	}
}

func TestToolResultMessageMarshalRejectsUnknownRoleLikeUpstream(t *testing.T) {
	if data, err := json.Marshal(ToolResultMessage{Role: ToolResultRole("tool"), ToolCallID: "call-1", ToolName: "read", Content: []UserContentBlock{}, IsError: false, Timestamp: 123}); err == nil {
		t.Fatalf("tool result message with unknown role should not marshal like upstream enum, got %s", data)
	}
}

func TestAssistantMessageDirectJSONMatchesUpstream(t *testing.T) {
	data, err := json.Marshal(AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{{Type: ContentText, Text: "ok"}}, API: ApiOpenAIResponses, Provider: Provider("openai"), Model: "gpt-test", Usage: &Usage{}, StopReason: StopReasonEndTurn, Timestamp: 123})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["role"]; ok {
		t.Fatalf("assistant message should skip inner role like upstream, got %s", data)
	}
	for _, field := range []string{"content", "api", "provider", "model", "usage", "stopReason", "timestamp"} {
		if _, ok := object[field]; !ok {
			t.Fatalf("assistant message missing required %s: %s", field, data)
		}
	}
	if object["api"] != "openai-responses" || object["provider"] != "openai" || object["model"] != "gpt-test" || object["stopReason"] != "stop" || object["timestamp"] != float64(123) {
		t.Fatalf("assistant message json mismatch: %#v", object)
	}
}

func TestAssistantMessageNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	data, err := marshalJSONNoHTMLEscape(AssistantMessage{Content: []ContentBlock{{Type: ContentText, Text: "a < b && c > d"}}, Usage: &Usage{Cost: &UsageCost{}}, StopReason: StopReasonEndTurn})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("assistant message JSON should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) {
		t.Fatalf("assistant message JSON should preserve literal text, got %s", text)
	}
}

func TestAssistantMessageDirectJSONOmitsOptionalFieldsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(AssistantMessage{Content: []ContentBlock{}, Usage: &Usage{Cost: &UsageCost{}}, StopReason: StopReasonEndTurn})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"responseModel", "responseId", "diagnostics", "errorMessage"} {
		if _, ok := object[field]; ok {
			t.Fatalf("assistant message should omit optional %s when empty, got %s", field, data)
		}
	}
}

func TestAssistantMessageRoundTripPreservesExplicitEmptyOptionalFieldsLikeUpstream(t *testing.T) {
	input := []byte(`{"content":[],"api":"openai-responses","provider":"openai","model":"gpt-test","responseModel":"","responseId":"","diagnostics":[],"usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","errorMessage":"","timestamp":123}`)
	var message AssistantMessage
	if err := json.Unmarshal(input, &message); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"responseModel", "responseId", "errorMessage"} {
		value, ok := object[field].(string)
		if !ok || value != "" {
			t.Fatalf("explicit empty %s should round-trip like upstream Some(\"\"), got %s", field, data)
		}
	}
	if diagnostics, ok := object["diagnostics"].([]any); !ok || len(diagnostics) != 0 {
		t.Fatalf("explicit empty diagnostics should round-trip like upstream Some([]), got %s", data)
	}
}

func TestAssistantMessageDirectJSONDoesNotEmitLegacyToolCallsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(AssistantMessage{Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}}}, ToolCalls: []ToolCall{{ID: "call-1", Name: "read"}}, Usage: &Usage{Cost: &UsageCost{}}, StopReason: StopReasonToolCalls})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["toolCalls"]; ok {
		t.Fatalf("assistant message should encode tool calls as content blocks only like upstream, got %s", data)
	}
}

func TestAssistantMessageContentUsesVecSemanticsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(AssistantMessage{Usage: &Usage{Cost: &UsageCost{}}, StopReason: StopReasonEndTurn})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	content, ok := object["content"].([]any)
	if !ok || len(content) != 0 {
		t.Fatalf("assistant nil content should marshal as empty array like upstream Vec, got %s", data)
	}
	var output AssistantMessage
	if err := json.Unmarshal([]byte(`{"content":null,"api":"openai-responses","provider":"openai","model":"gpt-test","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":123}`), &output); err == nil {
		t.Fatalf("assistant null content should be rejected like upstream Vec: %#v", output)
	}
}

func TestAssistantMessageUnmarshalRejectsNullTimestampLikeUpstreamI64(t *testing.T) {
	var output AssistantMessage
	if err := json.Unmarshal([]byte(`{"content":[],"api":"openai-responses","provider":"openai","model":"gpt-test","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":null}`), &output); err == nil {
		t.Fatalf("assistant timestamp null should be rejected like upstream i64: %#v", output)
	}
}

func TestAssistantMessageUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	for _, field := range []string{"api", "provider", "model"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{"content": []any{}, "api": "openai-responses", "provider": "openai", "model": "gpt-test", "usage": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "totalTokens": 0, "cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0}}, "stopReason": "stop", "timestamp": 123}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var message AssistantMessage
			if err := json.Unmarshal(data, &message); err == nil {
				t.Fatalf("assistant %s null should be rejected like upstream String: %#v", field, message)
			}
		})
	}
}

func TestAssistantRoleJSONRejectsUnknownValueLikeUpstream(t *testing.T) {
	if _, err := json.Marshal(AssistantRole("user")); err == nil {
		t.Fatal("unknown assistant role should not marshal like upstream enum")
	}
	var role AssistantRole
	if err := json.Unmarshal([]byte(`"user"`), &role); err == nil {
		t.Fatalf("unknown assistant role should be rejected like upstream enum: %q", role)
	}
}

func TestAssistantMessageMarshalRejectsUnknownRoleLikeUpstream(t *testing.T) {
	if data, err := json.Marshal(AssistantMessage{Role: AssistantRole("user"), Content: []ContentBlock{}, Usage: &Usage{}, StopReason: StopReasonEndTurn}); err == nil {
		t.Fatalf("assistant message with unknown role should not marshal like upstream enum, got %s", data)
	}
}

func TestContentBlockUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	tests := map[string]struct {
		data    []byte
		missing string
	}{
		"text":       {data: []byte(`{"type":"text"}`), missing: "text"},
		"thinking":   {data: []byte(`{"type":"thinking"}`), missing: "thinking"},
		"image data": {data: []byte(`{"type":"image","mimeType":"image/png"}`), missing: "data"},
		"image mime": {data: []byte(`{"type":"image","data":"aW1n"}`), missing: "mimeType"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var block ContentBlock
			if err := json.Unmarshal(tt.data, &block); err == nil {
				t.Fatalf("missing content block %s should be rejected like upstream: %#v", tt.missing, block)
			}
		})
	}
}

func TestContentBlockUnmarshalRejectsUnknownTypeLikeUpstream(t *testing.T) {
	var block ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"audio","data":"abc"}`), &block); err == nil {
		t.Fatalf("unknown content block type should be rejected like upstream tagged enum: %#v", block)
	}
}

func TestContentBlockMarshalRejectsUnknownTypeLikeUpstream(t *testing.T) {
	for _, block := range []ContentBlock{{}, {Type: ContentType("audio")}} {
		if data, err := json.Marshal(block); err == nil {
			t.Fatalf("unknown content block type should not marshal like upstream tagged enum, got %s", data)
		}
	}
}

func TestContentBlockUnmarshalsUpstreamToolCallShape(t *testing.T) {
	data := []byte(`{"type":"toolCall","id":"call-1","name":"read","arguments":{"path":"README.md"},"thoughtSignature":"sig-1"}`)
	var output ContentBlock
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if output.Type != ContentToolCall || output.ToolCall == nil {
		t.Fatalf("upstream toolCall should decode as local tool call block: %#v", output)
	}
	if output.ToolCall.ID != "call-1" || output.ToolCall.Name != "read" || output.ToolCall.Arguments["path"] != "README.md" || output.ToolCall.ThoughtSignature != "sig-1" {
		t.Fatalf("tool call mismatch: %#v", output.ToolCall)
	}
}

func TestContentBlockMarshalsToolCallLikeUpstream(t *testing.T) {
	input := ContentBlock{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}, ThoughtSignature: "sig-1"}}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["type"] != "toolCall" || object["id"] != "call-1" || object["name"] != "read" || object["thoughtSignature"] != "sig-1" {
		t.Fatalf("tool call content block should serialize upstream wire shape, got %s", data)
	}
	if _, ok := object["toolCall"]; ok {
		t.Fatalf("tool call content block should not nest local toolCall field, got %s", data)
	}
}

func TestContentBlockMarshalsNilToolCallArgumentsAsEmptyObjectLikeUpstream(t *testing.T) {
	input := ContentBlock{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	arguments, ok := object["arguments"].(map[string]any)
	if !ok || len(arguments) != 0 {
		t.Fatalf("nil tool call arguments should serialize as empty object like upstream, got %s", data)
	}
}

func TestContentBlockMarshalRejectsNilToolCallLikeUpstreamTaggedVariant(t *testing.T) {
	if _, err := json.Marshal(ContentBlock{Type: ContentToolCall}); err == nil {
		t.Fatal("toolCall content block without ToolCall payload should not marshal as upstream tagged variant")
	}
}

func TestToolCallMarshalsNilArgumentsAsEmptyObjectLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ToolCall{ID: "call-1", Name: "read"})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	arguments, ok := object["arguments"].(map[string]any)
	if !ok || len(arguments) != 0 {
		t.Fatalf("nil tool call arguments should serialize as empty object like upstream, got %s", data)
	}
}

func TestToolCallNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	data, err := marshalJSONNoHTMLEscape(ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"text": "a < b && c > d"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("tool call JSON should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) {
		t.Fatalf("tool call JSON should preserve literal argument string, got %s", text)
	}
}

func TestToolCallUnmarshalsMissingArgumentsAsEmptyObjectLikeUpstream(t *testing.T) {
	var output ToolCall
	if err := json.Unmarshal([]byte(`{"id":"call-1","name":"read"}`), &output); err != nil {
		t.Fatal(err)
	}
	if output.Arguments == nil || len(output.Arguments) != 0 {
		t.Fatalf("missing tool call arguments should decode as empty object like upstream: %#v", output.Arguments)
	}
}

func TestToolCallUnmarshalRejectsNullArgumentsLikeUpstreamMap(t *testing.T) {
	var output ToolCall
	if err := json.Unmarshal([]byte(`{"id":"call-1","name":"read","arguments":null}`), &output); err == nil {
		t.Fatalf("null tool call arguments should be rejected like upstream map: %#v", output)
	}
}

func TestToolCallRoundTripPreservesExplicitEmptyThoughtSignatureLikeUpstream(t *testing.T) {
	var output ToolCall
	if err := json.Unmarshal([]byte(`{"id":"call-1","name":"read","arguments":{},"thoughtSignature":""}`), &output); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if signature, ok := object["thoughtSignature"].(string); !ok || signature != "" {
		t.Fatalf("explicit empty thoughtSignature should round-trip like upstream Some(\"\"), got %s", data)
	}
}

func TestToolCallUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	for _, field := range []string{"id", "name"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{"id": "call-1", "name": "read"}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var call ToolCall
			if err := json.Unmarshal(data, &call); err == nil {
				t.Fatalf("missing tool call %s should be rejected like upstream: %#v", field, call)
			}
		})
	}
}

func TestToolCallUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	for _, field := range []string{"id", "name"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{"id": "call-1", "name": "read"}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var call ToolCall
			if err := json.Unmarshal(data, &call); err == nil {
				t.Fatalf("tool call %s null should be rejected like upstream String: %#v", field, call)
			}
		})
	}
}

func TestToolMarshalIncludesRequiredEmptyFieldsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Tool{Name: "read"})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["description"] != "" {
		t.Fatalf("tool should include required empty description like upstream, got %s", data)
	}
	parameters, ok := object["parameters"].(map[string]any)
	if !ok || len(parameters) != 0 {
		t.Fatalf("tool should include required empty parameters object like upstream, got %s", data)
	}
}

func TestToolNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	data, err := marshalJSONNoHTMLEscape(Tool{Name: "read", Description: "a < b && c > d", Parameters: map[string]any{"description": "body <>&"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("tool JSON should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) || !strings.Contains(text, `body <>&`) {
		t.Fatalf("tool JSON should preserve literal strings, got %s", text)
	}
}

func TestToolUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"name", "description", "parameters"}
	base := map[string]any{"name": "read", "description": "read files", "parameters": map[string]any{"type": "object"}}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var tool Tool
			if err := json.Unmarshal(data, &tool); err == nil {
				t.Fatalf("missing tool %s should be rejected like upstream: %#v", field, tool)
			}
		})
	}
}

func TestToolUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	for _, field := range []string{"name", "description"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{"name": "read", "description": "read files", "parameters": map[string]any{"type": "object"}}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var tool Tool
			if err := json.Unmarshal(data, &tool); err == nil {
				t.Fatalf("tool %s null should be rejected like upstream String: %#v", field, tool)
			}
		})
	}
}

func TestToolParametersPreserveArbitraryJSONValueLikeUpstream(t *testing.T) {
	for _, input := range []string{
		`{"name":"read","description":"read files","parameters":["not","object"]}`,
		`{"name":"read","description":"read files","parameters":null}`,
	} {
		t.Run(input, func(t *testing.T) {
			var tool Tool
			if err := json.Unmarshal([]byte(input), &tool); err != nil {
				t.Fatalf("tool parameters should accept arbitrary JSON like upstream: %v", err)
			}
			data, err := json.Marshal(tool)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != input {
				t.Fatalf("tool parameters should round-trip arbitrary JSON like upstream, got %s", data)
			}
		})
	}
}

func TestToolParametersPreserveRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var tool Tool
	if err := json.Unmarshal([]byte(`{"name":"read","description":"read files","parameters":{"ticket":9007199254740993}}`), &tool); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(tool.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ticket":9007199254740993}` {
		t.Fatalf("tool parameters should preserve raw JSON number like upstream serde_json::Value, got %s", data)
	}
}

func TestModelUnmarshalRejectsNullDefaultVecFieldsLikeUpstream(t *testing.T) {
	data := []byte(`{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":null,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":0,"maxTokens":0}`)
	var model Model
	if err := json.Unmarshal(data, &model); err == nil {
		t.Fatalf("model input null should be rejected like upstream Vec: %#v", model)
	}
}

func TestModelCompatPreservesArbitraryJSONValueLikeUpstream(t *testing.T) {
	data := []byte(`{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":[],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":0,"maxTokens":0,"compat":["flag",{"nested":true}]}`)
	var model Model
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	items, ok := model.CompatValue.([]any)
	if !ok || len(items) != 2 || items[0] != "flag" {
		t.Fatalf("compat should preserve arbitrary upstream JSON value: %#v", model.CompatValue)
	}
	out, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(out, &object); err != nil {
		t.Fatal(err)
	}
	compat, ok := object["compat"].([]any)
	if !ok || len(compat) != 2 || compat[0] != "flag" {
		t.Fatalf("compat should re-marshal arbitrary upstream JSON value, got %s", out)
	}

	model = Model{ID: "m", Name: "Model", API: ApiOpenAIResponses, Provider: Provider("openai"), Cost: &ModelCost{}, Compat: map[string]any{"requiresReasoningContentOnAssistantMessages": true}}
	out, err = json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &object); err != nil {
		t.Fatal(err)
	}
	compatObject, ok := object["compat"].(map[string]any)
	if !ok || compatObject["requiresReasoningContentOnAssistantMessages"] != true {
		t.Fatalf("compat map should remain supported for provider code, got %s", out)
	}
}

func TestModelCompatPreservesRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	data := []byte(`{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":[],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":0,"maxTokens":0,"compat":{"ticket":9007199254740993}}`)
	var model Model
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(model.CompatValue)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("model compat should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestModelHeadersPreservesExplicitEmptyObjectLikeUpstreamOption(t *testing.T) {
	data := []byte(`{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":[],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":0,"maxTokens":0,"headers":{}}`)
	var model Model
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(out, &object); err != nil {
		t.Fatal(err)
	}
	headers, ok := object["headers"].(map[string]any)
	if !ok || len(headers) != 0 {
		t.Fatalf("explicit empty headers should round-trip like Some(empty), got %s", out)
	}
}

func TestModelThinkingLevelMapRejectsUnknownKeyLikeUpstream(t *testing.T) {
	data := []byte(`{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":[],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":0,"maxTokens":0,"thinkingLevelMap":{"turbo":"enabled"}}`)
	var model Model
	if err := json.Unmarshal(data, &model); err == nil {
		t.Fatalf("unknown thinkingLevelMap key should be rejected like upstream ModelThinkingLevel enum: %#v", model)
	}
	if _, err := json.Marshal(Model{ID: "m", Name: "Model", API: ApiOpenAIResponses, Provider: Provider("openai"), Cost: &ModelCost{}, ThinkingLevels: map[string]*string{"turbo": nil}}); err == nil {
		t.Fatal("unknown thinkingLevelMap key should not marshal like upstream ModelThinkingLevel enum")
	}
}

func TestModelThinkingLevelMapAllowsNullValuesLikeUpstream(t *testing.T) {
	data := []byte(`{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":[],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":0,"maxTokens":0,"thinkingLevelMap":{"off":null,"high":"enabled"}}`)
	var model Model
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	if _, ok := model.ThinkingLevels["off"]; !ok || model.ThinkingLevels["off"] != nil || model.ThinkingLevels["high"] == nil || *model.ThinkingLevels["high"] != "enabled" {
		t.Fatalf("thinkingLevelMap should preserve Option<String> values: %#v", model.ThinkingLevels)
	}
	if _, ok := model.ThinkingLevelMap["off"]; !ok || model.ThinkingLevelMap["off"] != nil || model.ThinkingLevelMap["high"] == nil || *model.ThinkingLevelMap["high"] != "enabled" {
		t.Fatalf("thinkingLevelMap alias should preserve Option<String> values: %#v", model.ThinkingLevelMap)
	}
}

func TestModelThinkingLevelJSONMatchesUpstream(t *testing.T) {
	data, err := json.Marshal(ModelThinkingOff)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"off"` {
		t.Fatalf("model thinking level json mismatch: %s", data)
	}
	var level ModelThinkingLevel
	if err := json.Unmarshal([]byte(`"xhigh"`), &level); err != nil {
		t.Fatal(err)
	}
	if level != ModelThinkingXHigh {
		t.Fatalf("model thinking level mismatch: %q", level)
	}
	if _, err := json.Marshal(ModelThinkingLevel("turbo")); err == nil {
		t.Fatal("unknown model thinking level should not marshal like upstream enum")
	}
	if err := json.Unmarshal([]byte(`"turbo"`), &level); err == nil {
		t.Fatalf("unknown model thinking level should be rejected like upstream enum: %q", level)
	}
}

func TestThinkingLevelMapJSONMatchesUpstream(t *testing.T) {
	enabled := "enabled"
	input := ThinkingLevelMap{ModelThinkingOff: nil, ModelThinkingHigh: &enabled}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["off"]; !ok || object["off"] != nil || object["high"] != "enabled" {
		t.Fatalf("thinking level map json mismatch: %#v", object)
	}
	var output ThinkingLevelMap
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if _, ok := output[ModelThinkingOff]; !ok || output[ModelThinkingOff] != nil || output[ModelThinkingHigh] == nil || *output[ModelThinkingHigh] != "enabled" {
		t.Fatalf("thinking level map mismatch: %#v", output)
	}
	if err := json.Unmarshal([]byte(`{"turbo":"enabled"}`), &output); err == nil {
		t.Fatalf("unknown thinking level map key should be rejected like upstream enum: %#v", output)
	}
}

func TestModelMarshalPreservesExplicitEmptyThinkingLevelMapLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Model{ID: "gpt-test", API: ApiOpenAIResponses, Provider: Provider("openai"), ThinkingLevels: map[string]*string{}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if levels, ok := object["thinkingLevelMap"].(map[string]any); !ok || len(levels) != 0 {
		t.Fatalf("explicit empty thinkingLevelMap should serialize like upstream Some({}), got %s", data)
	}
}

func TestModelMarshalPreservesExplicitEmptyCompatLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Model{ID: "gpt-test", API: ApiOpenAIResponses, Provider: Provider("openai"), Compat: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if compat, ok := object["compat"].(map[string]any); !ok || len(compat) != 0 {
		t.Fatalf("explicit empty compat should serialize like upstream Some({}), got %s", data)
	}
}

func TestModelMarshalIncludesRequiredEmptyFieldsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Model{ID: "gpt-test", API: ApiOpenAIResponses, Provider: Provider("openai")})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"name", "baseUrl", "reasoning", "input", "cost", "contextWindow", "maxTokens"} {
		if _, ok := object[field]; !ok {
			t.Fatalf("model should include required %s like upstream, got %s", field, data)
		}
	}
	if object["name"] != "" || object["baseUrl"] != "" || object["reasoning"] != false || object["contextWindow"] != float64(0) || object["maxTokens"] != float64(0) {
		t.Fatalf("model required zero values mismatch, got %s", data)
	}
	if input, ok := object["input"].([]any); !ok || len(input) != 0 {
		t.Fatalf("model input should default to empty array like upstream, got %s", data)
	}
	cost, ok := object["cost"].(map[string]any)
	if !ok || cost["input"] != float64(0) || cost["output"] != float64(0) || cost["cacheRead"] != float64(0) || cost["cacheWrite"] != float64(0) {
		t.Fatalf("model cost should include upstream zero cost fields, got %s", data)
	}
}

func TestModelNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	data, err := marshalJSONNoHTMLEscape(Model{ID: "gpt-test", Name: "a < b && c > d", API: ApiOpenAIResponses, Provider: Provider("openai"), BaseURL: "https://example.test/<model>&x", CompatValue: map[string]any{"note": "body <>&"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("model JSON should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) || !strings.Contains(text, `https://example.test/<model>&x`) || !strings.Contains(text, `body <>&`) {
		t.Fatalf("model JSON should preserve literal strings, got %s", text)
	}
}

func TestModelMarshalPreservesExplicitEmptyHeadersLikeUpstream(t *testing.T) {
	data, err := json.Marshal(Model{ID: "gpt-test", API: ApiOpenAIResponses, Provider: Provider("openai"), Headers: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if headers, ok := object["headers"].(map[string]any); !ok || len(headers) != 0 {
		t.Fatalf("explicit empty headers should serialize like upstream Some({}), got %s", data)
	}
}

func TestModelCostUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"input", "output", "cacheRead", "cacheWrite"}
	base := map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var cost ModelCost
			if err := json.Unmarshal(data, &cost); err == nil {
				t.Fatalf("missing model cost %s should be rejected like upstream: %#v", field, cost)
			}
		})
	}
}

func TestModelCostUnmarshalRejectsNullNumericFieldsLikeUpstreamF64(t *testing.T) {
	base := map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}
	for _, field := range []string{"input", "output", "cacheRead", "cacheWrite"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var cost ModelCost
			if err := json.Unmarshal(data, &cost); err == nil {
				t.Fatalf("model cost %s null should be rejected like upstream f64: %#v", field, cost)
			}
		})
	}
}

func TestModelUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"id", "name", "api", "provider", "baseUrl", "reasoning", "cost", "contextWindow", "maxTokens"}
	base := map[string]any{
		"id":            "gpt-test",
		"name":          "GPT Test",
		"api":           "openai-responses",
		"provider":      "openai",
		"baseUrl":       "https://example.test",
		"reasoning":     false,
		"input":         []any{},
		"cost":          map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
		"contextWindow": 0,
		"maxTokens":     0,
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var model Model
			if err := json.Unmarshal(data, &model); err == nil {
				t.Fatalf("missing model %s should be rejected like upstream: %#v", field, model)
			}
		})
	}
}

func TestModelUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	base := map[string]any{
		"id":            "gpt-test",
		"name":          "GPT Test",
		"api":           "openai-responses",
		"provider":      "openai",
		"baseUrl":       "https://example.test",
		"reasoning":     false,
		"input":         []any{},
		"cost":          map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
		"contextWindow": 0,
		"maxTokens":     0,
	}
	for _, field := range []string{"id", "name", "api", "provider", "baseUrl"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var model Model
			if err := json.Unmarshal(data, &model); err == nil {
				t.Fatalf("model %s null should be rejected like upstream String: %#v", field, model)
			}
		})
	}
}

func TestModelUnmarshalRejectsNullReasoningLikeUpstreamBool(t *testing.T) {
	data := []byte(`{"id":"gpt-test","name":"GPT Test","api":"openai-responses","provider":"openai","baseUrl":"https://example.test","reasoning":null,"input":[],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":0,"maxTokens":0}`)
	var model Model
	if err := json.Unmarshal(data, &model); err == nil {
		t.Fatalf("model reasoning null should be rejected like upstream bool: %#v", model)
	}
}

func TestModelUnmarshalRejectsNullNumericAndCostFieldsLikeUpstream(t *testing.T) {
	base := map[string]any{
		"id":            "gpt-test",
		"name":          "GPT Test",
		"api":           "openai-responses",
		"provider":      "openai",
		"baseUrl":       "https://example.test",
		"reasoning":     false,
		"input":         []any{},
		"cost":          map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
		"contextWindow": 0,
		"maxTokens":     0,
	}
	for _, field := range []string{"cost", "contextWindow", "maxTokens"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var model Model
			if err := json.Unmarshal(data, &model); err == nil {
				t.Fatalf("model %s null should be rejected like upstream required field: %#v", field, model)
			}
		})
	}
}

func TestModelJSONRejectsNegativeU32FieldsLikeUpstream(t *testing.T) {
	base := map[string]any{
		"id":            "gpt-test",
		"name":          "GPT Test",
		"api":           "openai-responses",
		"provider":      "openai",
		"baseUrl":       "https://example.test",
		"reasoning":     false,
		"input":         []any{},
		"cost":          map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
		"contextWindow": 0,
		"maxTokens":     0,
	}
	for _, field := range []string{"contextWindow", "maxTokens"} {
		t.Run("unmarshal "+field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = -1
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var model Model
			if err := json.Unmarshal(data, &model); err == nil {
				t.Fatalf("negative model %s should be rejected like upstream u32: %#v", field, model)
			}
		})
	}
	if _, err := json.Marshal(Model{ID: "gpt-test", API: ApiOpenAIResponses, Provider: Provider("openai"), ContextWindow: -1}); err == nil {
		t.Fatal("negative model contextWindow should not marshal like upstream u32")
	}
	if _, err := json.Marshal(Model{ID: "gpt-test", API: ApiOpenAIResponses, Provider: Provider("openai"), MaxTokens: -1}); err == nil {
		t.Fatal("negative model maxTokens should not marshal like upstream u32")
	}
}

func TestImagesModelMarshalIncludesRequiredEmptyFieldsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ImagesModel{ID: "img-test", API: ImagesApi("images"), Provider: ImagesProvider("openai")})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"name", "baseUrl", "input", "output", "cost"} {
		if _, ok := object[field]; !ok {
			t.Fatalf("images model should include required %s like upstream, got %s", field, data)
		}
	}
	if object["name"] != "" || object["baseUrl"] != "" {
		t.Fatalf("images model required zero values mismatch, got %s", data)
	}
	if input, ok := object["input"].([]any); !ok || len(input) != 0 {
		t.Fatalf("images model input should default to empty array like upstream, got %s", data)
	}
	if output, ok := object["output"].([]any); !ok || len(output) != 0 {
		t.Fatalf("images model output should default to empty array like upstream, got %s", data)
	}
	if _, ok := object["cost"].(map[string]any); !ok {
		t.Fatalf("images model cost should include upstream zero cost object, got %s", data)
	}
}

func TestImagesModelNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	data, err := marshalJSONNoHTMLEscape(ImagesModel{ID: "img-test", Name: "a < b && c > d", API: ImagesApi("images"), Provider: ImagesProvider("openai"), BaseURL: "https://example.test/<model>&x"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("images model JSON should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) || !strings.Contains(text, `https://example.test/<model>&x`) {
		t.Fatalf("images model JSON should preserve literal strings, got %s", text)
	}
}

func TestImagesModelMarshalPreservesExplicitEmptyHeadersLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ImagesModel{ID: "img-test", API: ImagesApi("images"), Provider: ImagesProvider("openai"), Headers: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if headers, ok := object["headers"].(map[string]any); !ok || len(headers) != 0 {
		t.Fatalf("explicit empty images model headers should serialize like upstream Some({}), got %s", data)
	}
}

func TestImagesModelUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"id", "name", "api", "provider", "baseUrl", "cost"}
	base := map[string]any{
		"id":       "img-test",
		"name":     "Image Test",
		"api":      "images",
		"provider": "openai",
		"baseUrl":  "https://example.test",
		"input":    []any{},
		"output":   []any{},
		"cost":     map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var model ImagesModel
			if err := json.Unmarshal(data, &model); err == nil {
				t.Fatalf("missing images model %s should be rejected like upstream: %#v", field, model)
			}
		})
	}
}

func TestImagesModelUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	base := map[string]any{
		"id":       "img-test",
		"name":     "Image Test",
		"api":      "images",
		"provider": "openai",
		"baseUrl":  "https://example.test",
		"input":    []any{},
		"output":   []any{},
		"cost":     map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
	}
	for _, field := range []string{"id", "name", "api", "provider", "baseUrl"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var model ImagesModel
			if err := json.Unmarshal(data, &model); err == nil {
				t.Fatalf("images model %s null should be rejected like upstream String: %#v", field, model)
			}
		})
	}
}

func TestImagesModelUnmarshalRejectsNullDefaultVecFieldsLikeUpstream(t *testing.T) {
	base := map[string]any{
		"id":       "img-test",
		"name":     "Image Test",
		"api":      "images",
		"provider": "openai",
		"baseUrl":  "https://example.test",
		"input":    []any{},
		"output":   []any{},
		"cost":     map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
	}
	for _, field := range []string{"input", "output"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var model ImagesModel
			if err := json.Unmarshal(data, &model); err == nil {
				t.Fatalf("images model %s null should be rejected like upstream Vec: %#v", field, model)
			}
		})
	}
}

func TestImagesModelUnmarshalRejectsNullCostLikeUpstreamStruct(t *testing.T) {
	data := []byte(`{"id":"img-test","name":"Image Test","api":"images","provider":"openai","baseUrl":"https://example.test","input":[],"output":[],"cost":null}`)
	var model ImagesModel
	if err := json.Unmarshal(data, &model); err == nil {
		t.Fatalf("images model cost null should be rejected like upstream struct: %#v", model)
	}
}

func TestImagesModelHeadersPreservesExplicitEmptyObjectLikeUpstreamOption(t *testing.T) {
	data := []byte(`{"id":"img-test","name":"Image Test","api":"images","provider":"openai","baseUrl":"https://example.test","input":[],"output":[],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"headers":{}}`)
	var model ImagesModel
	if err := json.Unmarshal(data, &model); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(out, &object); err != nil {
		t.Fatal(err)
	}
	headers, ok := object["headers"].(map[string]any)
	if !ok || len(headers) != 0 {
		t.Fatalf("explicit empty images model headers should round-trip like Some(empty), got %s", out)
	}
}

func TestInputModalityUnmarshalRejectsUnknownValueLikeUpstream(t *testing.T) {
	var modality InputModality
	if err := json.Unmarshal([]byte(`"audio"`), &modality); err == nil {
		t.Fatalf("unknown input modality should be rejected like upstream enum: %q", modality)
	}
}

func TestInputModalityMarshalRejectsUnknownValueLikeUpstream(t *testing.T) {
	if _, err := json.Marshal(InputModality("audio")); err == nil {
		t.Fatal("unknown input modality should not marshal like upstream enum")
	}
}

func TestModelUnmarshalRejectsUnknownInputModalityLikeUpstream(t *testing.T) {
	data := []byte(`{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":["audio"],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":0,"maxTokens":0}`)
	var model Model
	if err := json.Unmarshal(data, &model); err == nil {
		t.Fatalf("unknown model input modality should be rejected like upstream enum: %#v", model)
	}
}

func TestAssistantImagesMarshalIncludesRequiredFieldsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(AssistantImages{API: ImagesApi("images"), Provider: ImagesProvider("openai"), Model: "img-test", Output: []ContentBlock{{Type: ContentThinking, Thinking: "plan"}, {Type: ContentText, Text: "ok"}, {Type: ContentImage, Data: "aW1n", MimeType: "image/png"}}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"output", "stopReason", "timestamp"} {
		if _, ok := object[field]; !ok {
			t.Fatalf("assistant images should include required %s like upstream, got %s", field, data)
		}
	}
	if object["stopReason"] != "stop" || object["timestamp"] != float64(0) {
		t.Fatalf("assistant images required defaults mismatch, got %s", data)
	}
	output := object["output"].([]any)
	if len(output) != 2 || output[0].(map[string]any)["type"] != "text" || output[1].(map[string]any)["type"] != "image" {
		t.Fatalf("assistant images output should serialize as upstream user blocks, got %s", data)
	}
}

func TestAssistantImagesNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	data, err := marshalJSONNoHTMLEscape(AssistantImages{API: ImagesApi("images"), Provider: ImagesProvider("openai"), Model: "img-test", Output: []ContentBlock{{Type: ContentText, Text: "a < b && c > d"}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("assistant images JSON should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) {
		t.Fatalf("assistant images JSON should preserve literal output text, got %s", text)
	}
}

func TestImagesContextUsesUserContentBlocksLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ImagesContext{Input: []UserContentBlock{{Type: UserContentText, Text: "draw"}, {Type: UserContentImage, Data: "aW1n", MimeType: "image/png"}}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string][]map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	input := object["input"]
	if len(input) != 2 || input[0]["type"] != "text" || input[0]["text"] != "draw" || input[1]["type"] != "image" || input[1]["data"] != "aW1n" || input[1]["mimeType"] != "image/png" {
		t.Fatalf("images context wire mismatch: %s", data)
	}

	var context ImagesContext
	if err := json.Unmarshal([]byte(`{"input":[{"type":"thinking","thinking":"plan"}]}`), &context); err == nil {
		t.Fatalf("images context should reject non-user content blocks like upstream: %#v", context)
	}
}

func TestImagesContextNoHTMLEscapeEncoderMatchesUpstreamSerde(t *testing.T) {
	data, err := marshalJSONNoHTMLEscape(ImagesContext{Input: []UserContentBlock{{Type: UserContentText, Text: "a < b && c > d"}}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("images context JSON should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) {
		t.Fatalf("images context JSON should preserve literal input text, got %s", text)
	}
}

func TestImagesContextInputUsesVecSemanticsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ImagesContext{})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	input, ok := object["input"].([]any)
	if !ok || len(input) != 0 {
		t.Fatalf("images context nil input should marshal as empty array like upstream Vec, got %s", data)
	}
	var context ImagesContext
	if err := json.Unmarshal([]byte(`{"input":null}`), &context); err == nil {
		t.Fatalf("images context null input should be rejected like upstream Vec: %#v", context)
	}
	if err := json.Unmarshal([]byte(`{}`), &context); err != nil || context.Input == nil {
		t.Fatalf("images context missing input should default to empty Vec, context=%#v err=%v", context, err)
	}
}

func TestAssistantImagesOutputUsesVecSemanticsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(AssistantImages{API: ImagesApi("images"), Provider: ImagesProvider("openai"), Model: "img-test"})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	output, ok := object["output"].([]any)
	if !ok || len(output) != 0 {
		t.Fatalf("assistant images nil output should marshal as empty array like upstream Vec, got %s", data)
	}
	var images AssistantImages
	if err := json.Unmarshal([]byte(`{"api":"images","provider":"openai","model":"img-test","output":null,"stopReason":"stop","timestamp":123}`), &images); err == nil {
		t.Fatalf("assistant images null output should be rejected like upstream Vec: %#v", images)
	}
}

func TestAssistantImagesUnmarshalRejectsNonUserOutputBlocksLikeUpstream(t *testing.T) {
	for name, data := range map[string][]byte{
		"thinking": []byte(`{"api":"images","provider":"openai","model":"img-test","output":[{"type":"thinking","thinking":"plan"}],"stopReason":"stop","timestamp":123}`),
		"toolCall": []byte(`{"api":"images","provider":"openai","model":"img-test","output":[{"type":"toolCall","id":"call-1","name":"read"}],"stopReason":"stop","timestamp":123}`),
	} {
		t.Run(name, func(t *testing.T) {
			var images AssistantImages
			if err := json.Unmarshal(data, &images); err == nil {
				t.Fatalf("assistant images should reject %s output block like upstream UserContentBlock: %#v", name, images)
			}
		})
	}
}

func TestAssistantImagesUnmarshalRejectsNullTimestampLikeUpstreamI64(t *testing.T) {
	var images AssistantImages
	if err := json.Unmarshal([]byte(`{"api":"images","provider":"openai","model":"img-test","output":[],"stopReason":"stop","timestamp":null}`), &images); err == nil {
		t.Fatalf("assistant images timestamp null should be rejected like upstream i64: %#v", images)
	}
}

func TestAssistantImagesUnmarshalRejectsNullRequiredStringsLikeUpstreamString(t *testing.T) {
	for _, field := range []string{"api", "provider", "model"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{"api": "images", "provider": "openai", "model": "img-test", "output": []any{}, "stopReason": "stop", "timestamp": 123}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var images AssistantImages
			if err := json.Unmarshal(data, &images); err == nil {
				t.Fatalf("assistant images %s null should be rejected like upstream String: %#v", field, images)
			}
		})
	}
}

func TestAssistantImagesRoundTripPreservesExplicitEmptyOptionalStringsLikeUpstream(t *testing.T) {
	input := []byte(`{"api":"images","provider":"openai","model":"img-test","output":[],"responseId":"","stopReason":"stop","errorMessage":"","timestamp":123}`)
	var images AssistantImages
	if err := json.Unmarshal(input, &images); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(images)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if responseID, ok := object["responseId"].(string); !ok || responseID != "" {
		t.Fatalf("explicit empty responseId should round-trip like upstream Some(\"\"), got %s", data)
	}
	if errorMessage, ok := object["errorMessage"].(string); !ok || errorMessage != "" {
		t.Fatalf("explicit empty errorMessage should round-trip like upstream Some(\"\"), got %s", data)
	}
}

func TestAssistantImagesUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"api", "provider", "model", "output", "stopReason", "timestamp"}
	base := map[string]any{
		"api":        "images",
		"provider":   "openai",
		"model":      "img-test",
		"output":     []any{map[string]any{"type": "text", "text": "ok"}},
		"stopReason": "stop",
		"timestamp":  float64(123),
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var images AssistantImages
			if err := json.Unmarshal(data, &images); err == nil {
				t.Fatalf("missing assistant images %s should be rejected like upstream: %#v", field, images)
			}
		})
	}
}
