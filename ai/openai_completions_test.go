package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompletionsProviderUsesChatCompletionsProtocol(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization mismatch: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-model-header") != "model" || r.Header.Get("x-shared-header") != "options" {
			t.Fatalf("model headers mismatch model=%q shared=%q", r.Header.Get("x-model-header"), r.Header.Get("x-shared-header"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2}}\n\n",
			"data: [DONE]\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL, Headers: map[string]string{"x-model-header": "model", "x-shared-header": "model"}}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}}}, StreamOptions{APIKey: "test-key", Headers: map[string]string{"x-shared-header": "options"}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "hi" || message.Usage == nil || message.Usage.InputTokens != 1 || message.Usage.OutputTokens != 2 {
		t.Fatalf("message mismatch: %#v", message)
	}
	if body["model"] != "gpt-test" || body["stream"] != true {
		t.Fatalf("body mismatch: %#v", body)
	}
}

func TestOpenAICompletionsReplaysAssistantThinkingAsReasoningContent(t *testing.T) {
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

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "deepseek-test", Provider: Provider("deepseek"), API: ApiOpenAICompletions, BaseURL: server.URL, Compat: map[string]any{"requiresReasoningContentOnAssistantMessages": true}}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{
		Role:    RoleAssistant,
		Content: []ContentBlock{{Type: ContentThinking, Thinking: "plan"}, {Type: ContentText, Text: "answer"}},
	}}}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	assistant := messages[0].(map[string]any)
	if assistant["content"] != "answer" || assistant["reasoning_content"] != "plan" {
		t.Fatalf("assistant reasoning replay mismatch: %#v", assistant)
	}
}

func TestOpenAICompletionsUsesThinkingFormatForAssistantReasoningReplay(t *testing.T) {
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

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "together-test", Provider: Provider("together"), API: ApiOpenAICompletions, BaseURL: server.URL, Compat: map[string]any{"requiresReasoningContentOnAssistantMessages": true, "thinkingFormat": "together"}}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{
		Role:    RoleAssistant,
		Content: []ContentBlock{{Type: ContentThinking, Thinking: "plan"}, {Type: ContentText, Text: "answer"}},
	}}}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	assistant := messages[0].(map[string]any)
	if assistant["content"] != "answer" || assistant["reasoning"] != "plan" {
		t.Fatalf("assistant reasoning replay mismatch: %#v", assistant)
	}
	if _, ok := assistant["reasoning_content"]; ok {
		t.Fatalf("deepseek reasoning field should not be used for together: %#v", assistant)
	}
}

func TestOpenAICompletionsProviderDoesNotEmitStandaloneUsageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2}}\n\n",
			"data: [DONE]\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"})
	for _, event := range stream.Events() {
		if event.Type == EventUsage {
			t.Fatalf("openai_completions should update final message usage without standalone usage event like upstream: %#v", stream.Events())
		}
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil || message.Usage.InputTokens != 1 || message.Usage.OutputTokens != 2 {
		t.Fatalf("message usage mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAICompletionsProviderIgnoresNonU64UsageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":-1,\"completion_tokens\":2.5}}\n\n",
			"data: [DONE]\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok || message.Usage == nil {
		t.Fatalf("expected completed message with usage: %#v ok=%v", message, ok)
	}
	if message.Usage.InputTokens != 0 || message.Usage.OutputTokens != 0 || message.Usage.TotalTokens() != 0 {
		t.Fatalf("non-u64 usage fields should be ignored like upstream as_u64: %#v", message.Usage)
	}
}

func TestOpenAICompletionsProviderStreamDoesNotMapThinkingLevelLikeUpstream(t *testing.T) {
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

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL, Reasoning: true}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("direct Stream should only use provider_extras reasoning_effort like upstream: %#v", body)
	}
}

func TestOpenAICompletionsProviderStreamSimpleMapsThinkingLevelLikeUpstream(t *testing.T) {
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

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL, Reasoning: true}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key"}, ThinkingLevel: ThinkingXHigh}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning effort mismatch: %#v", body)
	}
}

func TestOpenAICompletionsProviderStreamSimpleOverridesBaseReasoningEffortLikeUpstream(t *testing.T) {
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

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL, Reasoning: true}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{

		ThinkingLevel: ThinkingHigh,
		Base:          StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "low"}, APIKey: "test-key"},
	}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["reasoning_effort"] != "high" {
		t.Fatalf("simple reasoning should override base provider extra like upstream: %#v", body)
	}
}

func TestOpenAICompletionsProviderSendsSystemRoleMessagesByDefault(t *testing.T) {
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

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	request := Context{Messages: []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "system instructions"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}},
	}}
	_, ok := provider.Stream(context.Background(), model, request, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	if len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || messages[0].(map[string]any)["content"] != "system instructions" || messages[1].(map[string]any)["role"] != "user" {
		t.Fatalf("system role messages should be sent by default: %#v", messages)
	}
}

func TestOpenAICompletionsProviderIgnoresLegacyAssistantToolCallsLikeUpstream(t *testing.T) {
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

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	request := Context{Messages: []Message{{
		Role:      RoleAssistant,
		ToolCalls: []ToolCall{{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
	}}}
	_, ok := provider.Stream(context.Background(), model, request, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	assistant := messages[0].(map[string]any)
	if _, ok := assistant["tool_calls"]; ok {
		t.Fatalf("legacy assistant ToolCalls should be ignored like upstream: %#v", assistant)
	}
}

func TestOpenAICompletionsProviderUsesOnlyFirstChoiceLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"first\"}},{\"delta\":{\"content\":\"second\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "first" {
		t.Fatalf("expected only first choice content like upstream, got %q", message.Text())
	}
}

func TestOpenAICompletionsProviderStartAndDoneUseCompletionsModelMetadataLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"})
	events := stream.Events()
	if len(events) < 2 || events[0].Type != EventStart || events[0].Partial == nil || events[0].Partial.API != ApiOpenAICompletions || events[0].Partial.Provider != Provider("openai") || events[0].Partial.Model != "gpt-test" {
		t.Fatalf("start metadata mismatch: %#v", events)
	}
	message, ok := stream.Result()
	if !ok || message.API != ApiOpenAICompletions || message.Provider != Provider("openai") || message.Model != "gpt-test" {
		t.Fatalf("done message metadata mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAICompletionsProviderIgnoresTopLevelErrorChunkLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"temporary\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"})
	for _, event := range stream.Events() {
		if event.Type == EventError {
			t.Fatalf("top-level error chunks should be ignored like upstream openai_completions: %#v", stream.Events())
		}
	}
	message, ok := stream.Result()
	if !ok || message.Text() != "ok" || message.StopReason != StopReasonEndTurn {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAICompletionsProviderKeepsFirstResponseMetadataLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"first-id\",\"model\":\"served-a\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"second-id\",\"model\":\"served-b\",\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.ResponseID != "first-id" || message.ResponseModel != "served-a" {
		t.Fatalf("expected first response metadata like upstream, got id=%q model=%q", message.ResponseID, message.ResponseModel)
	}
}

func TestOpenAICompletionsProviderDoesNotEmitStandaloneMetadataLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"cmpl-1\",\"model\":\"served-model\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"})
	for _, event := range stream.Events() {
		if event.Type == EventMetadata {
			t.Fatalf("openai_completions should keep metadata in final message without standalone metadata event like upstream: %#v", stream.Events())
		}
	}
	message, ok := stream.Result()
	if !ok || message.ResponseID != "cmpl-1" || message.ResponseModel != "served-model" {
		t.Fatalf("message metadata mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAICompletionsProviderMissingAPIKeyMentionsProviderEnvLikeUpstream(t *testing.T) {
	provider := NewOpenAICompletionsProvider()
	model := Model{ID: "deepseek-chat", Provider: Provider("deepseek"), API: ApiOpenAICompletions}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{}).Result()
	if !ok {
		t.Fatal("expected completed error")
	}
	want := "no API key for provider: deepseek; set DEEPSEEK_API_KEY or pass options.api_key"
	if message.StopReason != StopReasonError || message.ErrorMessage != want {
		t.Fatalf("missing key message mismatch: %#v", message)
	}
}

func TestOpenAICompletionsProviderReaderErrorCarriesModelMetadataLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[] }\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"})
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventDone || last.Message == nil || last.Message.API != ApiOpenAICompletions || last.Message.Provider != Provider("openai") || last.Message.Model != "gpt-test" {
		t.Fatalf("EOF completion should carry provider metadata: %#v", events)
	}
}

func TestOpenAICompletionsProviderRegisteredBuiltin(t *testing.T) {
	ClearAPIProviders()
	t.Cleanup(ClearAPIProviders)
	RegisterBuiltinProviders()
	if _, ok := GetAPIProvider(ApiOpenAICompletions); !ok {
		t.Fatal("openai completions provider was not registered")
	}
}

func TestOpenAICompletionsProviderPreservesSystemPrompt(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{SystemPrompt: "system prompt", HasSystemPrompt: true, Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}}}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	first := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "system prompt" {
		t.Fatalf("system message missing: %#v", body)
	}
}

func TestOpenAICompletionsProviderPreservesExplicitSystemPromptOnly(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAICompletionsProvider(WithOpenAICompletionsHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{SystemPrompt: "system prompt", HasSystemPrompt: true, Messages: []Message{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}},
	}}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	first := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "system prompt" {
		t.Fatalf("system prompt message missing: %#v", body)
	}
}
