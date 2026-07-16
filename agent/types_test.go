package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestToolResultDetailsPreserveRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var result ToolResult
	data := []byte(`{"callId":"call-1","name":"read","content":[{"type":"text","text":"ok"}],"details":{"ticket":9007199254740993},"isError":false}`)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.DetailsValue)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("tool result details should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestCustomMessageMarshalRejectsNonObjectPayloadLikeUpstream(t *testing.T) {
	message := Message{Kind: MessageKindCustom, Custom: &CustomMessage{Role: "custom", Timestamp: 1, Payload: []any{"x"}}}
	if _, err := json.Marshal(message); err == nil {
		t.Fatal("non-object custom payload should be rejected like upstream serde flatten")
	}
}

func TestAgentMessageMarshalRejectsNilVariantPayloadsLikeUpstream(t *testing.T) {
	for name, message := range map[string]Message{
		"llm":        {Kind: MessageKindLLM},
		"custom":     {Kind: MessageKindCustom},
		"toolResult": {Kind: MessageKindToolResult},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := json.Marshal(message); err == nil {
				t.Fatalf("%s message without variant payload should not marshal like upstream enum", name)
			}
		})
	}
}

func TestAgentMessageMarshalRejectsUnknownKindLikeUpstreamEnum(t *testing.T) {
	for name, message := range map[string]Message{
		"zero":    {},
		"unknown": {Kind: MessageKind("future")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := json.Marshal(message); err == nil {
				t.Fatalf("%s message kind should not marshal like upstream enum", name)
			}
		})
	}
}

func TestAgentUpstreamExportedNames(t *testing.T) {
	var options AgentOptions = Options{Model: ai.Model{ID: "model"}}
	instance := New(options)
	if instance == nil {
		t.Fatal("agent options alias should construct an agent")
	}

	var listener AgentListener = func(Event) {}
	if listener == nil {
		t.Fatal("agent listener alias should be assignable")
	}

	var message AgentMessage = NewUserMessage("hello")
	if message.Kind != MessageKindLLM {
		t.Fatalf("agent message alias mismatch: %#v", message)
	}
	if alias := user_message("hello"); alias.Kind != message.Kind || alias.LLM.Role != ai.RoleUser || alias.LLM.Content[0].Text != "hello" {
		t.Fatalf("user_message alias mismatch: %#v", alias)
	}
	if llm := message.AsLLM(); llm == nil || llm.Role != ai.RoleUser || llm.Content[0].Text != "hello" {
		t.Fatalf("agent message AsLLM mismatch: %#v", llm)
	}
	if llm := message.AsLlm(); llm == nil || llm.Role != ai.RoleUser || llm.Content[0].Text != "hello" {
		t.Fatalf("agent message AsLlm alias mismatch: %#v", llm)
	}
	if llm := message.ToPieAi(); llm == nil || llm.Role != ai.RoleUser || llm.Content[0].Text != "hello" {
		t.Fatalf("agent message ToPieAi alias mismatch: %#v", llm)
	}
	custom := Message{Kind: MessageKindCustom, Custom: &CustomMessage{Role: "notice"}}
	if llm := custom.AsLLM(); llm != nil {
		t.Fatalf("custom message AsLLM should be nil, got %#v", llm)
	}

	var state AgentState = instance.State()
	if state.Model == nil || state.Model.ID != "model" {
		t.Fatalf("agent state alias mismatch: %#v", state)
	}

	var toolResult AgentToolResult = ToolResult{Content: "ok"}
	if toolResult.Content != "ok" {
		t.Fatalf("agent tool result alias mismatch: %#v", toolResult)
	}

	var toolCall AgentToolCall = ai.ToolCall{ID: "call-1", Name: "read"}
	if toolCall.Name != "read" {
		t.Fatalf("agent tool call alias mismatch: %#v", toolCall)
	}

	var piMessage PiMessage = ai.Message{Role: ai.RoleUser}
	var piAssistant PiAssistantMessage = ai.AssistantMessage{Role: ai.AssistantRoleAssistant}
	var piText PiTextContent = ai.TextContent{Text: "hello"}
	var piImage PiImageContent = ai.ImageContent{Data: "aW1n", MimeType: "image/png"}
	var piToolResult PiToolResultMessage = ai.ToolResultMessage{ToolCallID: "call-1", ToolName: "read"}
	if piMessage.Role != ai.RoleUser || piAssistant.Role != ai.AssistantRoleAssistant || piText.Text != "hello" || piImage.MimeType != "image/png" || piToolResult.ToolName != "read" {
		t.Fatalf("pi re-export aliases mismatch: %#v %#v %#v %#v %#v", piMessage, piAssistant, piText, piImage, piToolResult)
	}

	var update AgentToolUpdate = func(AgentToolResult) {}
	update(toolResult)

	convert := DefaultConvertToLLMFn()
	converted := convert([]AgentMessage{message})
	if len(converted) != 1 || converted[0].Role != ai.RoleUser {
		t.Fatalf("default convert alias mismatch: %#v", converted)
	}
	if converted := DefaultConvertToLlm([]AgentMessage{message}); len(converted) != 1 || converted[0].Role != ai.RoleUser {
		t.Fatalf("DefaultConvertToLlm alias mismatch: %#v", converted)
	}
	var upstreamConvert ConvertToLlm = convert
	if got := upstreamConvert([]AgentMessage{message}); len(got) != 1 || got[0].Role != ai.RoleUser {
		t.Fatalf("ConvertToLlm alias mismatch: %#v", got)
	}
}

func TestDefaultStreamSendsLeadingSystemMessageAsCodexInstructions(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	messages := []ai.Message{
		{Role: ai.RoleSystem, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "product instructions"}}},
		{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hello"}}},
	}
	stream, err := DefaultStreamFn()(context.Background(), ai.Model{ID: "gpt-test", Provider: "openai-codex", API: ai.ApiOpenAICodexResponses, BaseURL: server.URL}, messages, nil, ai.SimpleStreamOptions{Base: ai.StreamOptions{APIKey: "test-token"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stream.Result(); !ok {
		t.Fatal("expected completed stream")
	}
	if requestBody["instructions"] != "product instructions" {
		t.Fatalf("instructions = %#v", requestBody["instructions"])
	}
	input, ok := requestBody["input"].([]any)
	if !ok || len(input) != 1 || input[0].(map[string]any)["role"] != "user" {
		t.Fatalf("input = %#v", requestBody["input"])
	}
}

func TestAgentUpstreamHookAndConfigNames(t *testing.T) {
	var event AgentEvent = Event{Type: EventTypeStart}
	if event.Type != EventTypeStart {
		t.Fatalf("agent event alias mismatch: %#v", event)
	}
	if EventTypeAgentStart != EventTypeStart || EventTypeAgentEnd != EventTypeDone || EventTypeToolExecutionUpdate != EventTypeToolUpdate {
		t.Fatalf("agent event variant aliases mismatch")
	}
	if ControlPlanePromptDecisionAllow != ControlPlaneAllow || ControlPlanePromptDecisionDeny != ControlPlaneDeny || ControlPlanePromptDecisionTimeout != ControlPlaneTimeout {
		t.Fatalf("control-plane prompt decision aliases mismatch")
	}
	if AgentToolErrorOther("boom").Error() != "boom" {
		t.Fatal("agent tool error alias mismatch")
	}

	var config AgentLoopConfig = Config{ConvertToLLM: DefaultConvertToLLMFn()}
	if config.ConvertToLLM == nil {
		t.Fatalf("agent loop config alias mismatch: %#v", config)
	}

	var stream StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		return nil, nil
	}
	if _, err := stream(context.Background(), ai.Model{}, nil, nil, ai.SimpleStreamOptions{}); err != nil {
		t.Fatalf("stream alias mismatch: %v", err)
	}
	defaultStream := DefaultStreamFn()
	streamEvents, err := defaultStream(context.Background(), ai.Model{ID: "missing", API: ai.Api("missing-api")}, nil, nil, ai.SimpleStreamOptions{})
	if err != nil {
		t.Fatalf("default stream should return an error stream instead of an error: %v", err)
	}
	message, ok := streamEvents.Result()
	if !ok || message.ErrorMessage != "No API provider registered for api: missing-api" {
		t.Fatalf("default stream mismatch: ok=%v message=%#v", ok, message)
	}

	var getKey GetApiKey = func(ctx context.Context, provider ai.Provider) (string, bool) {
		return "key", true
	}
	if key, ok := getKey(context.Background(), ai.Provider("openai")); !ok || key != "key" {
		t.Fatalf("get api key alias mismatch: %q %v", key, ok)
	}

	var runError AgentRunError = ErrAlreadyStreaming
	if !errors.Is(runError, ErrAlreadyStreaming) {
		t.Fatalf("agent run error alias mismatch: %v", runError)
	}
	if !errors.Is(AgentRunErrorAlreadyStreaming, ErrAlreadyStreaming) || AgentRunErrorOther("boom").Error() != "boom" {
		t.Fatalf("agent run error variant aliases mismatch")
	}
}

func TestAgentUpstreamEnumVariantAliases(t *testing.T) {
	if ToolExecutionModeSequential != ToolExecutionSequential || ToolExecutionModeParallel != ToolExecutionParallel {
		t.Fatalf("tool execution mode aliases mismatch")
	}
	if QueueModeAll != QueueAll || QueueModeOneAtATime != QueueOneAtATime {
		t.Fatalf("queue mode aliases mismatch")
	}
	if ThinkingLevelOff != ThinkingOff || ThinkingLevelMinimal != ThinkingMinimal || ThinkingLevelLow != ThinkingLow || ThinkingLevelMedium != ThinkingMedium || ThinkingLevelHigh != ThinkingHigh || ThinkingLevelXhigh != ThinkingXHigh {
		t.Fatalf("thinking level aliases mismatch")
	}
	if AgentEventControlPlanePromptResolved != EventTypeControlPlanePromptResolved || AgentEventToolExecutionStart != EventTypeToolExecutionStart || AgentEventToolExecutionUpdate != EventTypeToolUpdate || AgentEventToolExecutionEnd != EventTypeToolExecutionEnd {
		t.Fatalf("agent event aliases mismatch")
	}
	if PermissionClassificationAllow != PermissionAllow || PermissionClassificationPrompt != PermissionPrompt || PermissionClassificationBlock != PermissionBlock {
		t.Fatalf("permission classification aliases mismatch")
	}
}

func TestHookContextsExposeUpstreamFieldAliases(t *testing.T) {
	call := ai.ToolCall{ID: "call-1", Name: "read"}
	agentContext := AgentContext{SystemPrompt: "system"}
	before := BeforeToolCallContext{Call: call, ToolCall: call, AgentContext: agentContext, Context: agentContext}
	if before.ToolCall.ID != before.Call.ID || before.Context.SystemPrompt != before.AgentContext.SystemPrompt {
		t.Fatalf("before tool call alias mismatch: %#v", before)
	}
	after := AfterToolCallContext{Call: call, ToolCall: call, AgentContext: agentContext, Context: agentContext}
	if after.ToolCall.ID != after.Call.ID || after.Context.SystemPrompt != after.AgentContext.SystemPrompt {
		t.Fatalf("after tool call alias mismatch: %#v", after)
	}
	shouldStop := ShouldStopAfterTurnContext{AgentContext: agentContext, Context: agentContext}
	if shouldStop.Context.SystemPrompt != shouldStop.AgentContext.SystemPrompt {
		t.Fatalf("should stop alias mismatch: %#v", shouldStop)
	}
}

func TestAgentThinkingLevelHelpersMatchUpstream(t *testing.T) {
	cases := []struct {
		level ThinkingLevel
		text  string
		pie   ai.ThinkingLevel
		ok    bool
	}{
		{ThinkingOff, "off", "", false},
		{ThinkingMinimal, "minimal", ai.ThinkingMinimal, true},
		{ThinkingLow, "low", ai.ThinkingLow, true},
		{ThinkingMedium, "medium", ai.ThinkingMedium, true},
		{ThinkingHigh, "high", ai.ThinkingHigh, true},
		{ThinkingXHigh, "xhigh", ai.ThinkingXHigh, true},
	}

	for _, tc := range cases {
		if got := tc.level.AsStr(); got != tc.text {
			t.Fatalf("as-str mismatch for %s: %s", tc.level, got)
		}
		got, ok := tc.level.ToPieAI()
		if ok != tc.ok || got != tc.pie {
			t.Fatalf("to-pie-ai mismatch for %s: got %q %v want %q %v", tc.level, got, ok, tc.pie, tc.ok)
		}
	}
}

func TestCustomMessageUnmarshalRejectsMissingTimestampLikeUpstream(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"notice","text":"hello"}`), &message); err == nil {
		t.Fatalf("custom message missing timestamp should be rejected like upstream: %#v", message)
	}
}

func TestCustomMessageUnmarshalFlattensPayloadLikeUpstream(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"notice","timestamp":123,"text":"hello"}`), &message); err != nil {
		t.Fatal(err)
	}
	if message.Kind != MessageKindCustom || message.Custom == nil || message.Custom.Role != "notice" || message.Custom.Timestamp != 123 {
		t.Fatalf("custom message mismatch: %#v", message)
	}
	payload, ok := message.Custom.Payload.(map[string]any)
	if !ok || payload["text"] != "hello" {
		t.Fatalf("custom payload mismatch: %#v", message.Custom.Payload)
	}
}

func TestCustomMessagePayloadPreservesRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"notice","timestamp":123,"ticket":9007199254740993}`), &message); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(message.Custom.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("custom message payload should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestAgentMessageUnmarshalFallsBackToCustomWhenLLMShapeFailsLikeUpstream(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"user","timestamp":123,"text":"hello"}`), &message); err != nil {
		t.Fatal(err)
	}
	if message.Kind != MessageKindCustom || message.Custom == nil || message.Custom.Role != "user" || message.Custom.Timestamp != 123 {
		t.Fatalf("invalid LLM-shaped message should fall back to custom like upstream untagged enum: %#v", message)
	}
	payload, ok := message.Custom.Payload.(map[string]any)
	if !ok || payload["text"] != "hello" {
		t.Fatalf("custom fallback payload mismatch: %#v", message.Custom.Payload)
	}
}

func TestAgentMessageUnmarshalFallsBackToCustomWhenToolResultShapeFailsLikeUpstream(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{"role":"toolResult","timestamp":123,"text":"hello"}`), &message); err != nil {
		t.Fatal(err)
	}
	if message.Kind != MessageKindCustom || message.Custom == nil || message.Custom.Role != "toolResult" || message.Custom.Timestamp != 123 {
		t.Fatalf("invalid toolResult-shaped message should fall back to custom like upstream untagged enum: %#v", message)
	}
	payload, ok := message.Custom.Payload.(map[string]any)
	if !ok || payload["text"] != "hello" {
		t.Fatalf("custom fallback payload mismatch: %#v", message.Custom.Payload)
	}
}

func TestQueueModeJSONUsesKebabCaseLikeUpstream(t *testing.T) {
	data, err := json.Marshal(QueueOneAtATime)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"one-at-a-time"` {
		t.Fatalf("queue mode should marshal as upstream kebab-case, got %s", data)
	}
	var mode QueueMode
	if err := json.Unmarshal([]byte(`"one-at-a-time"`), &mode); err != nil {
		t.Fatal(err)
	}
	if mode != QueueOneAtATime {
		t.Fatalf("queue mode should decode upstream kebab-case, got %q", mode)
	}
}

func TestQueueModeUnmarshalRejectsUnknownValueLikeUpstream(t *testing.T) {
	for _, data := range [][]byte{[]byte(`"future"`), []byte(`"one_at_a_time"`)} {
		var mode QueueMode
		if err := json.Unmarshal(data, &mode); err == nil {
			t.Fatalf("queue mode should reject non-upstream value %s: %q", data, mode)
		}
	}
}

func TestQueueModeMarshalRejectsNonUpstreamValues(t *testing.T) {
	for _, mode := range []QueueMode{QueueAppend, QueueReplace, QueueMode("future")} {
		if data, err := json.Marshal(mode); err == nil {
			t.Fatalf("queue mode %q should not marshal to upstream wire, got %s", mode, data)
		}
	}
}

func TestToolExecutionModeJSONMatchesUpstream(t *testing.T) {
	for _, mode := range []ToolExecutionMode{ToolExecutionSequential, ToolExecutionParallel} {
		data, err := json.Marshal(mode)
		if err != nil {
			t.Fatal(err)
		}
		var decoded ToolExecutionMode
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded != mode {
			t.Fatalf("tool execution mode round-trip mismatch: got %q want %q", decoded, mode)
		}
	}
}

func TestToolExecutionModeJSONRejectsNonUpstreamValues(t *testing.T) {
	for _, mode := range []ToolExecutionMode{ToolExecutionAuto, ToolExecutionManual, ToolExecutionMode("future")} {
		if data, err := json.Marshal(mode); err == nil {
			t.Fatalf("tool execution mode %q should not marshal to upstream wire, got %s", mode, data)
		}
	}
	for _, data := range [][]byte{[]byte(`"auto"`), []byte(`"manual"`), []byte(`"future"`)} {
		var mode ToolExecutionMode
		if err := json.Unmarshal(data, &mode); err == nil {
			t.Fatalf("tool execution mode should reject non-upstream value %s: %q", data, mode)
		}
	}
}

func TestLLMMessageMarshalMatchesAIMessageWire(t *testing.T) {
	details := map[string]any{"exit_code": float64(1)}
	messages := []ai.Message{
		{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hello"}}, Timestamp: 1},
		{Role: ai.RoleAssistant, API: ai.ApiOpenAIResponses, Provider: "openai", Model: "gpt-test", Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "ok"}}, Usage: &ai.Usage{InputTokens: 1, OutputTokens: 2}, StopReason: ai.StopReasonToolCalls, Timestamp: 2},
		{Role: ai.RoleTool, ToolCallID: "call-1", ToolName: "read", Details: details, DetailsValue: details, IsError: true, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "done"}}, Timestamp: 3},
	}
	for _, llm := range messages {
		t.Run(string(llm.Role), func(t *testing.T) {
			agentData, err := json.Marshal(Message{Kind: MessageKindLLM, LLM: &llm})
			if err != nil {
				t.Fatal(err)
			}
			aiData, err := json.Marshal(llm)
			if err != nil {
				t.Fatal(err)
			}
			if string(agentData) != string(aiData) {
				t.Fatalf("agent llm wire should match ai message wire\nagent=%s\nai=%s", agentData, aiData)
			}
		})
	}
}

func TestLLMMessageMarshalDoesNotHTMLEscapeNestedContentLikeUpstream(t *testing.T) {
	llm := ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "<tag>&value"}}}
	data, err := marshalJSONNoHTMLEscape(Message{Kind: MessageKindLLM, LLM: &llm})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `\u003c`) || strings.Contains(string(data), `\u0026`) || strings.Contains(string(data), `\u003e`) {
		t.Fatalf("agent llm message should preserve literal HTML-sensitive characters like upstream serde_json, got %s", data)
	}
}

func TestToolResultMessageUnmarshalRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	fields := []string{"toolCallId", "toolName", "content", "isError"}
	base := map[string]any{
		"role":       "toolResult",
		"toolCallId": "call-1",
		"toolName":   "read",
		"content":    []any{map[string]any{"type": "text", "text": "ok"}},
		"isError":    false,
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
			var message Message
			if err := json.Unmarshal(data, &message); err == nil {
				t.Fatalf("missing toolResult %s should be rejected like upstream: %#v", field, message)
			}
		})
	}
}

func TestToolResultMessageMarshalMatchesAIMessageWire(t *testing.T) {
	details := map[string]any{"exit_code": float64(1)}
	content := []ai.ContentBlock{
		{Type: ai.ContentThinking, Thinking: "plan"},
		{Type: ai.ContentText, Text: "done"},
		{Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "nested", Name: "bad"}},
	}
	message := Message{Kind: MessageKindToolResult, ToolResult: &ToolResult{CallID: "call-1", Name: "read", ContentBlocks: content, Details: details, IsError: true}}
	agentData, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	aiData, err := json.Marshal(ai.Message{Role: ai.RoleTool, ToolCallID: "call-1", ToolName: "read", Name: "read", Content: content, Details: details, IsError: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(agentData) != string(aiData) {
		t.Fatalf("agent tool result wire should match ai message wire\nagent=%s\nai=%s", agentData, aiData)
	}
}

func TestToolResultMessageMarshalPreservesArbitraryDetailsValueLikeUpstream(t *testing.T) {
	message := Message{Kind: MessageKindToolResult, ToolResult: &ToolResult{CallID: "call-1", Name: "read", Content: "ok", DetailsValue: []any{"trace"}}}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	details, ok := object["details"].([]any)
	if !ok || len(details) != 1 {
		t.Fatalf("tool result should preserve arbitrary details value like upstream, got %s", data)
	}
}

func TestToolResultMessageUnmarshalPreservesArbitraryDetailsValueLikeUpstream(t *testing.T) {
	data := []byte(`{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[{"type":"text","text":"ok"}],"details":["trace"],"isError":false,"timestamp":0}`)
	var message Message
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatal(err)
	}
	if message.ToolResult == nil || len(message.ToolResult.DetailsValue.([]any)) != 1 || message.ToolResult.Details != nil {
		t.Fatalf("tool result details value mismatch: %#v", message.ToolResult)
	}
}

func TestToolResultMarshalMatchesAgentToolResultShapeLikeUpstream(t *testing.T) {
	terminate := true
	data, err := json.Marshal(ToolResult{CallID: "call-1", Name: "read", ContentBlocks: []ai.ContentBlock{{Type: ai.ContentThinking, Thinking: "plan"}, {Type: ai.ContentText, Text: "ok"}}, DetailsValue: []any{"trace"}, Error: "hidden", IsError: true, Terminate: &terminate})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	content := object["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["type"] != "text" {
		t.Fatalf("tool result content should serialize as upstream user blocks, got %s", data)
	}
	if _, ok := object["callID"]; ok {
		t.Fatalf("tool result should not serialize local callID, got %s", data)
	}
	if _, ok := object["error"]; ok {
		t.Fatalf("tool result should not serialize local error field, got %s", data)
	}
	if len(object["details"].([]any)) != 1 || object["terminate"] != true {
		t.Fatalf("tool result details/terminate mismatch, got %s", data)
	}
}

func TestToolResultMarshalDefaultContentAsEmptyArrayLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ToolResult{})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	content, ok := object["content"].([]any)
	if !ok || len(content) != 0 {
		t.Fatalf("default tool result content should be empty array like upstream, got %s", data)
	}
	if _, ok := object["details"]; !ok || object["details"] != nil {
		t.Fatalf("default tool result details should be explicit null like upstream, got %s", data)
	}
}

func TestToolResultUnmarshalMatchesAgentToolResultShapeLikeUpstream(t *testing.T) {
	data := []byte(`{"content":[{"type":"text","text":"ok"},{"type":"image","data":"aW1n","mimeType":"image/png"}],"details":["trace"],"terminate":true}`)
	var result ToolResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Content != "ok" || len(result.ContentBlocks) != 2 || len(result.DetailsValue.([]any)) != 1 || result.Details != nil || result.Terminate == nil || !*result.Terminate {
		t.Fatalf("tool result unmarshal mismatch: %#v", result)
	}
}

func TestToolResultUnmarshalRejectsMissingOrNullContentLikeUpstream(t *testing.T) {
	for name, data := range map[string][]byte{
		"missing": []byte(`{"details":null}`),
		"null":    []byte(`{"content":null,"details":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			var result ToolResult
			if err := json.Unmarshal(data, &result); err == nil {
				t.Fatalf("tool result should reject %s content like upstream: %#v", name, result)
			}
		})
	}
}

func TestToolResultUnmarshalRejectsNonUserContentBlocksLikeUpstream(t *testing.T) {
	for name, data := range map[string][]byte{
		"thinking": []byte(`{"content":[{"type":"thinking","thinking":"plan"}],"details":null}`),
		"toolCall": []byte(`{"content":[{"type":"toolCall","id":"call-1","name":"read","arguments":{}}],"details":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			var result ToolResult
			if err := json.Unmarshal(data, &result); err == nil {
				t.Fatalf("tool result should reject %s content block like upstream UserContentBlock: %#v", name, result)
			}
		})
	}
}

func TestToolResultUnmarshalKeepsLegacyGoWrapperFields(t *testing.T) {
	data := []byte(`{"CallID":"call-1","Name":"read","Content":"failed","Details":{"exit_code":1},"Error":"denied","IsError":true,"Terminate":true}`)
	var result ToolResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.CallID != "call-1" || result.Name != "read" || result.Content != "failed" || result.Details["exit_code"] != json.Number("1") || result.DetailsValue.(map[string]any)["exit_code"] != json.Number("1") || result.Error != "denied" || !result.IsError || result.Terminate == nil || !*result.Terminate {
		t.Fatalf("legacy tool result wrapper mismatch: %#v", result)
	}
}

func TestToolResultUnmarshalKeepsLegacyLowercaseFields(t *testing.T) {
	data := []byte(`{"callID":"call-1","name":"read","content":[{"type":"text","text":"failed"}],"details":{"exit_code":1},"error":"denied","isError":true,"terminate":true}`)
	var result ToolResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.CallID != "call-1" || result.Name != "read" || result.Content != "failed" || result.Details["exit_code"] != json.Number("1") || result.DetailsValue.(map[string]any)["exit_code"] != json.Number("1") || result.Error != "denied" || !result.IsError || result.Terminate == nil || !*result.Terminate {
		t.Fatalf("legacy lowercase tool result mismatch: %#v", result)
	}
}

func TestAgentToolErrorMessageAndWrapMatchUpstreamShape(t *testing.T) {
	message := NewAgentToolError("denied")
	if message.Error() != "denied" {
		t.Fatalf("message tool error mismatch: %q", message.Error())
	}

	cause := errors.New("disk failed")
	wrapped := WrapAgentToolError(cause)
	if wrapped.Error() != "disk failed" {
		t.Fatalf("wrapped tool error mismatch: %q", wrapped.Error())
	}
	if !errors.Is(wrapped, cause) {
		t.Fatalf("wrapped tool error should unwrap cause")
	}
}

func TestToolSpecsPreservesArbitraryParametersValueLikeUpstream(t *testing.T) {
	tool := arbitraryParametersTool{parameters: []any{"not", "object"}}
	specs := ToolSpecs([]Tool{tool})
	if len(specs) != 1 {
		t.Fatalf("tool spec count mismatch: %#v", specs)
	}
	parameters, ok := specs[0].Parameters.([]any)
	if !ok || len(parameters) != 2 {
		t.Fatalf("tool parameters should preserve arbitrary JSON value like upstream: %#v", specs[0].Parameters)
	}
}

type arbitraryParametersTool struct {
	parameters any
}

func (tool arbitraryParametersTool) Name() string { return "arbitrary" }

func (tool arbitraryParametersTool) Description() string { return "arbitrary parameters" }

func (tool arbitraryParametersTool) Parameters() any { return tool.parameters }

func (tool arbitraryParametersTool) Execute(context.Context, ai.ToolCall, ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{}, nil
}

type objectParametersTool struct{}

func (objectParametersTool) Name() string        { return "object" }
func (objectParametersTool) Description() string { return "object params" }
func (objectParametersTool) Execute(context.Context, ai.ToolCall, ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{}, nil
}
func (objectParametersTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}
}

func TestToolSpecsAcceptsMapParametersTools(t *testing.T) {
	specs := ToolSpecs([]Tool{objectParametersTool{}})
	if len(specs) != 1 || specs[0].Parameters == nil {
		t.Fatalf("specs = %#v", specs)
	}
}
