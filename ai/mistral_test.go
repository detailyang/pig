package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMistralProviderRequestAndSSE(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer mistral-key" {
			t.Fatalf("authorization mismatch: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-affinity") != "sess-1" {
			t.Fatalf("x-affinity mismatch: %q", r.Header.Get("x-affinity"))
		}
		if r.Header.Get("x-model-header") != "model" || r.Header.Get("x-shared-header") != "options" {
			t.Fatalf("model headers mismatch model=%q shared=%q", r.Header.Get("x-model-header"), r.Header.Get("x-shared-header"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"id\":\"mistral-resp-1\",\"choices\":[{\"delta\":{\"content\":\"bon\"}}]}\n\n",
			"data: {\"choices\":[{\"delta\":{\"content\":\"jour\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7}}\n\n",
			"data: [DONE]\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL, Reasoning: true, Headers: map[string]string{"x-model-header": "model", "x-shared-header": "model"}}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}}}, StreamOptions{APIKey: "mistral-key", MaxTokens: 64, SessionID: "sess-1", Headers: map[string]string{"x-shared-header": "options"}, ProviderExtras: map[string]any{"reasoning_effort": "medium"}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "bonjour" || message.StopReason != StopReasonEndTurn {
		t.Fatalf("message mismatch: %#v", message)
	}
	if message.ResponseID != "mistral-resp-1" {
		t.Fatalf("response id mismatch: %#v", message)
	}
	if message.Usage == nil || message.Usage.InputTokens != 11 || message.Usage.OutputTokens != 7 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
	if body["model"] != "mistral-large" || body["max_tokens"] != float64(64) || body["stream"] != true {
		t.Fatalf("body mismatch: %#v", body)
	}
	if body["reasoning_effort"] != "medium" {
		t.Fatalf("reasoning effort mismatch: %#v", body)
	}
}

func TestMistralProviderReturnsLiveStreamBeforeDone(t *testing.T) {
	finalChunk := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-finalChunk
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()
	defer close(finalChunk)

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for index := 0; ; {
		event, next, err := stream.Next(ctx, index)
		if err != nil {
			t.Fatal(err)
		}
		index = next
		if event.Type == EventTextDelta {
			if event.Delta != "hi" || event.Partial == nil || event.Partial.API != ApiMistral {
				t.Fatalf("text delta mismatch: %#v", event)
			}
			return
		}
	}
}

func TestMistralProviderHTTPErrorMessageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad mistral"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected error message, got %#v ok=%v", message, ok)
	}
	if message.ErrorMessage != "Mistral API error (400 Bad Request): bad mistral" {
		t.Fatalf("error message mismatch: %q", message.ErrorMessage)
	}
}

func TestMistralProviderSendErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewMistralProvider(WithMistralHTTPClient(&http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: "https://mistral.invalid"}
	maxRetries := 0
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key", MaxRetries: &maxRetries})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "http error: Post \"https://mistral.invalid/v1/chat/completions\": dial failed" {
		t.Fatalf("send error mismatch: %#v ok=%v", message, ok)
	}
}

func TestMistralProviderNewRequestErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewMistralProvider()
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: "://bad-url"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http error: ") {
		t.Fatalf("request build error mismatch: %#v ok=%v", message, ok)
	}
}

func TestMistralProviderMissingAPIKeyMessageLikeUpstream(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "")
	provider := NewMistralProvider()
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{}).Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected error message, got %#v ok=%v", message, ok)
	}
	if message.ErrorMessage != "MISTRAL_API_KEY is not set" {
		t.Fatalf("error message mismatch: %q", message.ErrorMessage)
	}
}

func TestMistralProviderErrorEventMatchesUpstreamShape(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "")
	provider := NewMistralProvider()
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{})
	events := stream.Events()
	if len(events) != 1 {
		t.Fatalf("Mistral error should not emit start/done like upstream: %#v", events)
	}
	if events[0].Type != EventError || events[0].ErrorReason != ErrorReasonProvider || events[0].Error != "" {
		t.Fatalf("error event mismatch: %#v", events[0])
	}
	if events[0].Message == nil || events[0].Message.Model != "mistral-large" || events[0].Message.Provider != Provider("mistral") || events[0].Message.API != ApiMistral || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "MISTRAL_API_KEY is not set" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("error message partial mismatch: %#v", events[0].Message)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "MISTRAL_API_KEY is not set" {
		t.Fatalf("error result mismatch: %#v ok=%v", message, ok)
	}
}

func TestMistralProviderStreamDoesNotMapThinkingLevelToReasoningEffortLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL, Reasoning: true}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("direct Stream should only use provider_extras reasoning_effort like upstream: %#v", body)
	}
}

func TestMistralProviderStreamSimpleOverridesBaseReasoningEffortLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL, Reasoning: true}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{

		ThinkingLevel: ThinkingHigh,
		Base:          StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "low"}, APIKey: "mistral-key"},
	}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["reasoning_effort"] != "high" {
		t.Fatalf("simple reasoning should override base provider extra like upstream: %#v", body)
	}
}

func TestMistralProviderOmitsStreamOptionsLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if _, ok := body["stream_options"]; ok {
		t.Fatalf("Mistral request should omit stream_options like upstream: %#v", body)
	}
}

func TestMistralProviderUserImagesAreIgnoredLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{
		{Type: ContentText, Text: "look"},
		{Type: ContentImage, MimeType: "image/png", Data: "abc"},
		{Type: ContentText, Text: "again"},
	}}}}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	if content := messages[0].(map[string]any)["content"]; content != "look\nagain" {
		t.Fatalf("Mistral should send text-only content like upstream, got %#v", content)
	}
}

func TestMistralProviderIgnoresRoleSystemMessagesLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "sys"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}},
	}}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	if len(messages) != 1 || messages[0].(map[string]any)["role"] != "user" {
		t.Fatalf("RoleSystem messages should be ignored like upstream: %#v", messages)
	}
}

func TestMistralProviderIncludesContextSystemPromptLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{SystemPrompt: "be helpful", Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}}}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || messages[0].(map[string]any)["content"] != "be helpful" || messages[1].(map[string]any)["role"] != "user" {
		t.Fatalf("system prompt should be sent as first system message like upstream: %#v", messages)
	}
}

func TestMistralProviderNormalizesRequestToolCallIDs(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-abc-123456789", Name: "read", Arguments: map[string]any{"path": "README.md"}}}}},
		{Role: RoleTool, ToolCallID: "call-abc-123456789", ToolName: "read", Content: []ContentBlock{{Type: ContentText, Text: "ok"}}},
	}}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	assistantToolCalls := messages[0].(map[string]any)["tool_calls"].([]any)
	if assistantToolCalls[0].(map[string]any)["id"] != "1adip3qzk" {
		t.Fatalf("assistant tool id mismatch: %#v", assistantToolCalls)
	}
	if messages[1].(map[string]any)["tool_call_id"] != "1adip3qzk" {
		t.Fatalf("tool result id mismatch: %#v", messages[1])
	}
	if messages[1].(map[string]any)["name"] != "read" {
		t.Fatalf("tool result name mismatch: %#v", messages[1])
	}
}

func TestMistralProviderDoesNotInferToolResultNameLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-abc-123456789", Name: "read", Arguments: map[string]any{"path": "README.md"}}}}},
		{Role: RoleTool, ToolCallID: "call-abc-123456789", Content: []ContentBlock{{Type: ContentText, Text: "ok"}}},
	}}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	if name := messages[1].(map[string]any)["name"]; name != "" {
		t.Fatalf("Mistral should use only explicit ToolName like upstream, got %#v in %#v", name, messages[1])
	}
}

func TestMistralProviderIgnoresLegacyAssistantToolCallsLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-abc-123456789", Name: "read", Arguments: map[string]any{"path": "README.md"}}}},
	}}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	if _, ok := messages[0].(map[string]any)["tool_calls"]; ok {
		t.Fatalf("Mistral should only serialize content tool calls like upstream: %#v", messages[0])
	}
}

func TestMistralProviderUsesEmptyStringForAssistantToolOnlyContentLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-abc-123456789", Name: "read", Arguments: map[string]any{"path": "README.md"}}}},
	}}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	if content := messages[0].(map[string]any)["content"]; content != "" {
		t.Fatalf("assistant tool-only content should be empty string like upstream, got %#v", content)
	}
}

func TestMistralProviderNormalizesStreamToolCallIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-abc-123456789\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "1adip3qzk" {
		t.Fatalf("tool call mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestMistralProviderKeepsOnlyFirstStreamingToolCallLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-abc-123456789\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}},{\"index\":1,\"id\":\"call-def-987654321\",\"function\":{\"name\":\"write\",\"arguments\":\"{\\\"path\\\":\\\"out.txt\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok || len(message.ToolCalls) != 1 {
		t.Fatalf("tool call mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
	if message.ToolCalls[0].Name != "read" || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("Mistral should keep only first streaming tool call like upstream: %#v", message.ToolCalls)
	}
}

func TestMistralProviderKeepsLaterToolCallArgumentDeltasLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-abc-123456789\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}},{\"index\":1,\"id\":\"call-def-987654321\",\"function\":{\"name\":\"write\",\"arguments\":\"{\\\"path\\\":\\\"out.txt\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"})
	var deltas []string
	for _, event := range stream.Events() {
		if event.Type == EventToolCallDelta {
			if event.ToolCall != nil {
				t.Fatalf("Mistral ToolCallDelta should not expose tool_call like upstream: %#v", event)
			}
			deltas = append(deltas, event.Delta)
		}
	}
	if len(deltas) != 2 || deltas[0] != `{"path":"README.md"}` || deltas[1] != `{"path":"out.txt"}` {
		t.Fatalf("Mistral should forward later tool call argument deltas like upstream, got %#v events=%#v", deltas, stream.Events())
	}
}

func TestMistralProviderStreamsOnlyFirstChoiceLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"first\"},\"finish_reason\":\"stop\"},{\"delta\":{\"content\":\"second\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "first" {
		t.Fatalf("Mistral should only consume choices[0] like upstream, got %q", message.Text())
	}
}

func TestMistralProviderIgnoresStreamedModelMetadataLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"mistral-resp-1\",\"model\":\"served-model\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.ResponseID != "mistral-resp-1" {
		t.Fatalf("response id mismatch: %#v", message)
	}
	if message.ResponseModel != "" {
		t.Fatalf("Mistral should not record streamed model metadata like upstream, got %q", message.ResponseModel)
	}
}

func TestMistralProviderIgnoresPromptTokenDetailsLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"prompt_tokens_details\":{\"cached_tokens\":5}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Usage == nil || message.Usage.InputTokens != 11 || message.Usage.OutputTokens != 7 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
	if message.Usage.CacheReadTokens != 0 || message.Usage.CacheWriteTokens != 0 {
		t.Fatalf("Mistral should ignore token details like upstream: %#v", message.Usage)
	}
}

func TestMistralProviderIgnoresNonU64UsageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":-1,\"completion_tokens\":2.5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok || message.Usage == nil {
		t.Fatalf("expected completed message with usage: %#v ok=%v", message, ok)
	}
	if message.Usage.InputTokens != 0 || message.Usage.OutputTokens != 0 || message.Usage.TotalTokens() != 0 {
		t.Fatalf("non-u64 usage fields should be ignored like upstream as_u64: %#v", message.Usage)
	}
}

func TestMistralProviderMergesSparseUsageChunksLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{}],\"usage\":{\"prompt_tokens\":11}}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"completion_tokens\":7}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Usage == nil || message.Usage.InputTokens != 11 || message.Usage.OutputTokens != 7 || message.Usage.TotalTokenCount != 18 || !message.Usage.HasTotalTokens || message.Usage.TotalTokens() != 18 {
		t.Fatalf("Mistral should merge sparse usage chunks like upstream: %#v", message.Usage)
	}
}

func TestMistralProviderPutsUsageOnDoneMessageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"})
	events := stream.Events()
	if len(events) != 5 {
		t.Fatalf("event count mismatch: %#v", events)
	}
	for _, event := range events {
		if event.Type == EventUsage {
			t.Fatalf("Mistral should not emit standalone usage events like upstream: %#v", events)
		}
	}
	done := events[len(events)-1]
	if done.Type != EventDone || done.Message == nil || done.Message.Usage == nil || done.Message.Usage.InputTokens != 11 || done.Message.Usage.OutputTokens != 7 {
		t.Fatalf("done usage mismatch: %#v", done)
	}
	if done.Message.StopReason != StopReasonEndTurn || done.Message.Timestamp == 0 {
		t.Fatalf("done message metadata mismatch: %#v", done.Message)
	}
}

func TestMistralProviderTextEventsMatchUpstreamSequence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"bon\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"jour\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"})
	events := stream.Events()
	if len(events) != 6 {
		t.Fatalf("event count mismatch: %#v", events)
	}
	if events[0].Type != EventStart || events[0].Partial == nil || events[0].Partial.Model != "mistral-large" || events[0].Partial.Provider != Provider("mistral") || events[0].Partial.API != ApiMistral || events[0].Partial.StopReason != StopReasonEndTurn || events[0].Partial.Timestamp == 0 || events[0].Partial.Usage == nil {
		t.Fatalf("start mismatch: %#v", events[0])
	}
	if events[1].Type != EventTextStart || events[1].ContentIndex != 0 {
		t.Fatalf("text start mismatch: %#v", events[0])
	}
	if events[1].Partial == nil || events[1].Partial.API != ApiMistral || events[1].Partial.Provider != Provider("mistral") || events[1].Partial.Model != "mistral-large" || len(events[1].Partial.Content) != 1 || events[1].Partial.Content[0].Type != ContentText || events[1].Partial.Content[0].Text != "" {
		t.Fatalf("text start partial should use empty Mistral text block like upstream: %#v", events[1].Partial)
	}
	if events[2].Type != EventTextDelta || events[2].ContentIndex != 0 || events[2].Delta != "bon" {
		t.Fatalf("first delta mismatch: %#v", events[1])
	}
	if events[2].Partial == nil || len(events[2].Partial.Content) != 1 || events[2].Partial.Content[0].Type != ContentText || events[2].Partial.Content[0].Text != "bon" || events[2].Partial.API != ApiMistral {
		t.Fatalf("first delta partial should carry accumulated Mistral text: %#v", events[2].Partial)
	}
	if events[3].Type != EventTextDelta || events[3].ContentIndex != 0 || events[3].Delta != "jour" {
		t.Fatalf("second delta mismatch: %#v", events[2])
	}
	if events[3].Partial == nil || len(events[3].Partial.Content) != 1 || events[3].Partial.Content[0].Type != ContentText || events[3].Partial.Content[0].Text != "bonjour" || events[3].Partial.API != ApiMistral {
		t.Fatalf("second delta partial should carry accumulated Mistral text: %#v", events[3].Partial)
	}
	if events[4].Type != EventTextEnd || events[4].ContentIndex != 0 || events[4].Content != "bonjour" {
		t.Fatalf("text end mismatch: %#v", events[3])
	}
	if events[4].Partial == nil || len(events[4].Partial.Content) != 1 || events[4].Partial.Content[0].Type != ContentText || events[4].Partial.Content[0].Text != "bonjour" || events[4].Partial.API != ApiMistral {
		t.Fatalf("text end partial should carry final Mistral text: %#v", events[4].Partial)
	}
	if events[5].Type != EventDone || events[5].DoneReason != DoneReasonStop {
		t.Fatalf("done mismatch: %#v", events[4])
	}
}

func TestMistralProviderIgnoresEmptyContentDeltaLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"})
	events := stream.Events()
	if len(events) != 2 || events[0].Type != EventStart || events[1].Type != EventDone {
		t.Fatalf("empty content deltas should not emit text events like upstream: %#v", events)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 0 || message.Text() != "" {
		t.Fatalf("empty content should not create final text: %#v ok=%v", message, ok)
	}
}

func TestMistralProviderToolCallEventsMatchUpstreamSequence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-abc-123456789\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"README.md\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"})
	events := stream.Events()
	if len(events) != 6 {
		t.Fatalf("event count mismatch: %#v", events)
	}
	if events[0].Type != EventStart || events[0].Partial == nil || events[0].Partial.Model != "mistral-large" || events[0].Partial.Provider != Provider("mistral") || events[0].Partial.API != ApiMistral || events[0].Partial.StopReason != StopReasonEndTurn || events[0].Partial.Timestamp == 0 || events[0].Partial.Usage == nil {
		t.Fatalf("start mismatch: %#v", events[0])
	}
	if events[1].Type != EventToolCallStart || events[1].ContentIndex != 0 || events[1].Partial == nil {
		t.Fatalf("tool start mismatch: %#v", events[0])
	}
	if events[1].Partial.API != ApiMistral || events[1].Partial.Provider != Provider("mistral") || events[1].Partial.Model != "mistral-large" {
		t.Fatalf("tool start partial should use Mistral message metadata: %#v", events[1].Partial)
	}
	if events[2].Type != EventToolCallDelta || events[2].ContentIndex != 0 || events[2].Delta != "{\"path\":" {
		t.Fatalf("first tool delta mismatch: %#v", events[1])
	}
	if events[2].Partial.API != ApiMistral || events[2].Partial.Provider != Provider("mistral") || events[2].Partial.Model != "mistral-large" {
		t.Fatalf("tool delta partial should use Mistral message metadata: %#v", events[2].Partial)
	}
	if events[2].Partial == nil || len(events[2].Partial.Content) != 1 || events[2].Partial.Content[0].ToolCall == nil || len(events[2].Partial.Content[0].ToolCall.Arguments) != 0 {
		t.Fatalf("tool delta partial should keep empty arguments until end like upstream: %#v", events[2])
	}
	if events[3].Type != EventToolCallDelta || events[3].ContentIndex != 0 || events[3].Delta != "\"README.md\"}" {
		t.Fatalf("second tool delta mismatch: %#v", events[2])
	}
	if events[4].Type != EventToolCallEnd || events[4].ContentIndex != 0 || events[4].ToolCall == nil || events[4].ToolCall.Name != "read" || events[4].ToolCall.Arguments["path"] != "README.md" {
		t.Fatalf("tool end mismatch: %#v", events[3])
	}
	if events[5].Type != EventDone || events[5].DoneReason != DoneReasonToolCalls {
		t.Fatalf("done mismatch: %#v", events[4])
	}
}

func TestMistralProviderIgnoresToolCallIndexLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-abc-123456789\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"function\":{\"arguments\":\"\\\"README.md\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("Mistral stream should ignore tool_call index like upstream: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestMistralProviderEmitsToolCallEndWithoutArgumentsLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-abc-123456789\",\"function\":{\"name\":\"read\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"})
	for _, event := range stream.Events() {
		if event.Type == EventToolCallEnd && event.ToolCall != nil && event.ToolCall.Name == "read" && len(event.ToolCall.Arguments) == 0 {
			return
		}
	}
	t.Fatalf("Mistral should emit ToolCallEnd without arguments like upstream, got %#v", stream.Events())
}

func TestMistralProviderNormalizesContentToolCallIDs(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{
			ID:        "call-abc-123456789",
			Name:      "read",
			Arguments: map[string]any{"path": "README.md"},
		}}},
	}}}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	assistantToolCalls := messages[0].(map[string]any)["tool_calls"].([]any)
	if assistantToolCalls[0].(map[string]any)["id"] != "1adip3qzk" {
		t.Fatalf("assistant tool id mismatch: %#v", assistantToolCalls)
	}
}

func TestNormalizeMistralToolCallID(t *testing.T) {
	if got := normalizeMistralToolCallID("abcdefghi"); got != "abcdefghi" {
		t.Fatalf("valid id changed: %q", got)
	}
	if got := normalizeMistralToolCallID("!@#"); got != "1o2vf6t1o" {
		t.Fatalf("symbol id mismatch: %q", got)
	}
}

func TestNormalizeMistralStreamDoesNotDuplicateDone(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Close(DoneReasonStop)
	out := normalizeMistralStream(stream, Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral})
	events := out.Events()
	if len(events) != 2 || events[0].Type != EventStart || events[1].Type != EventDone {
		t.Fatalf("events mismatch: %#v", events)
	}
}

func TestNormalizeMistralStreamSynthesizedTextEventsCarryPartialLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, Delta: "hi"})
	stream.Close(DoneReasonStop)
	out := normalizeMistralStream(stream, Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral})
	events := out.Events()
	if len(events) != 5 || events[0].Type != EventStart || events[1].Type != EventTextStart || events[2].Type != EventTextDelta || events[3].Type != EventTextEnd || events[4].Type != EventDone {
		t.Fatalf("events mismatch: %#v", events)
	}
	if events[1].Partial == nil || len(events[1].Partial.Content) != 1 || events[1].Partial.Content[0].Type != ContentText || events[1].Partial.Content[0].Text != "" || events[1].Partial.API != ApiMistral {
		t.Fatalf("synthesized text start partial mismatch: %#v", events[1].Partial)
	}
	if events[2].Partial == nil || len(events[2].Partial.Content) != 1 || events[2].Partial.Content[0].Type != ContentText || events[2].Partial.Content[0].Text != "hi" || events[2].Partial.API != ApiMistral {
		t.Fatalf("text delta partial mismatch: %#v", events[2].Partial)
	}
	if events[3].Partial == nil || len(events[3].Partial.Content) != 1 || events[3].Partial.Content[0].Type != ContentText || events[3].Partial.Content[0].Text != "hi" || events[3].Partial.API != ApiMistral {
		t.Fatalf("synthesized text end partial mismatch: %#v", events[3].Partial)
	}
	if events[4].Message == nil || events[4].Message.Text() != "hi" || events[4].Message.API != ApiMistral {
		t.Fatalf("done message mismatch: %#v", events[4])
	}
}

func TestMistralProviderStreamSimplePassesSessionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-affinity") != "sess-1" {
			t.Fatalf("x-affinity mismatch: %q", r.Header.Get("x-affinity"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "mistral-key", SessionID: "sess-1"}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
}

func TestMistralProviderStreamSimpleMapsMinimalReasoningToLowLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL, Reasoning: true}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "mistral-key"}, ThinkingLevel: ThinkingMinimal}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["reasoning_effort"] != "low" {
		t.Fatalf("reasoning effort mismatch: %#v", body)
	}
}

func TestMistralProviderFunctionCallFinishReasonStopsLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"function_call\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewMistralProvider(WithMistralHTTPClient(server.Client()))
	model := Model{ID: "mistral-large", Provider: Provider("mistral"), API: ApiMistral, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "mistral-key"}).Result()
	if !ok || message.StopReason != StopReasonEndTurn {
		t.Fatalf("Mistral function_call finish_reason should stop like upstream: %#v ok=%v", message, ok)
	}
}

func TestMistralProviderRegisteredBuiltin(t *testing.T) {
	ClearAPIProviders()
	t.Cleanup(ClearAPIProviders)
	RegisterBuiltinProviders()
	if _, ok := GetAPIProvider(ApiMistral); !ok {
		t.Fatal("mistral provider was not registered")
	}
}
