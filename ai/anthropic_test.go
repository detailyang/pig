package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnthropicProviderRequestAndSSE(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") == "" {
			t.Fatalf("headers mismatch api=%q version=%q", r.Header.Get("x-api-key"), r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("anthropic-beta") != "prompt-caching-2024-07-31,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14" {
			t.Fatalf("anthropic-beta mismatch: %q", r.Header.Get("anthropic-beta"))
		}
		if r.Header.Get("x-session-affinity") != "sess-1" {
			t.Fatalf("x-session-affinity mismatch: %q", r.Header.Get("x-session-affinity"))
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("accept mismatch: %s", r.Header.Get("Accept"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hel\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{}}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"README.md\\\"}\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":4}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL, Compat: map[string]any{"sendSessionAffinityHeaders": true}}
	request := Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}}, Tools: []Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}}}
	message, ok := provider.Stream(context.Background(), model, request, StreamOptions{APIKey: "test-key", MaxTokens: 128, SessionID: "sess-1"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "hello" {
		t.Fatalf("text mismatch: %q", message.Text())
	}
	if message.StopReason != StopReasonToolCalls {
		t.Fatalf("stop reason mismatch: %s", message.StopReason)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "toolu_1" || message.ToolCalls[0].Name != "read" || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool calls mismatch: %#v", message.ToolCalls)
	}
	if message.Usage == nil || message.Usage.OutputTokens != 4 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
	if body["model"] != "claude-test" || body["stream"] != true || body["max_tokens"] != float64(128) {
		t.Fatalf("body mismatch: %#v", body)
	}
	if tools := body["tools"].([]any); tools[0].(map[string]any)["name"] != "read" {
		t.Fatalf("tools mismatch: %#v", tools)
	}
}

func TestAnthropicProviderRequestBodyDoesNotHTMLEscapeLikeUpstreamSerde(t *testing.T) {
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		rawBody = string(data)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL}
	request := Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "<tag>&value"}}}}}
	provider.Stream(context.Background(), model, request, StreamOptions{APIKey: "test-key"}).Result()

	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("request body should not HTML-escape JSON strings like upstream serde_json: %s", rawBody)
	}
	if !strings.Contains(rawBody, `"text":"<tag>&value"`) {
		t.Fatalf("request body missing unescaped content: %s", rawBody)
	}
}

func TestAnthropicProviderAbortDuringSSEEmitsAbortedLikeUpstream(t *testing.T) {
	abort := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n"))
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL}
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(abort)
	}()

	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", Abort: abort}).Result()
	if !ok || message.StopReason != StopReasonAborted || message.ErrorMessage != "aborted" {
		t.Fatalf("aborted result mismatch: %#v ok=%v", message, ok)
	}
}

func TestBuildAnthropicRequestBodyIncludesUserIDMetadata(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{}, StreamOptions{Metadata: map[string]any{"user_id": "user-1", "ignored": map[string]any{"nested": true}}})
	metadata, ok := body["metadata"].(map[string]any)
	if !ok || len(metadata) != 1 || metadata["user_id"] != "user-1" {
		t.Fatalf("metadata mismatch: %#v", body["metadata"])
	}
}

func TestBuildAnthropicRequestBodyPreservesEmptyUserIDMetadataLikeUpstream(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{}, StreamOptions{Metadata: map[string]any{"user_id": ""}})
	metadata, ok := body["metadata"].(map[string]any)
	if !ok || len(metadata) != 1 || metadata["user_id"] != "" {
		t.Fatalf("empty user_id metadata should be preserved like upstream Value: %#v", body["metadata"])
	}
}

func TestBuildAnthropicRequestBodyPreservesNonStringUserIDMetadataLikeUpstream(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{}, StreamOptions{Metadata: map[string]any{"user_id": 123}})
	metadata, ok := body["metadata"].(map[string]any)
	if !ok || len(metadata) != 1 || metadata["user_id"] != 123 {
		t.Fatalf("non-string user_id metadata should be preserved like upstream Value: %#v", body["metadata"])
	}
}

func TestAnthropicProviderStreamSimplePassesThinkingLevel(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL, Compat: map[string]any{"sendSessionAffinityHeaders": true}}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key"}, ThinkingLevel: ThinkingLow}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	thinking := body["thinking"].(map[string]any)
	if thinking["budget_tokens"] != float64(4096) {
		t.Fatalf("thinking mismatch: %#v", thinking)
	}
}

func TestAnthropicProviderStreamSimpleUsesThinkingBudgets(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key"}, ThinkingLevel: ThinkingMedium, ThinkingBudgets: ThinkingBudgets{Medium: 8192}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	thinking := body["thinking"].(map[string]any)
	if thinking["budget_tokens"] != float64(8192) {
		t.Fatalf("thinking mismatch: %#v", thinking)
	}
}

func TestAnthropicProviderStreamSimplePreservesExplicitZeroThinkingBudgetLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL}
	var budgets ThinkingBudgets
	if err := json.Unmarshal([]byte(`{"medium":0}`), &budgets); err != nil {
		t.Fatal(err)
	}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key"}, ThinkingLevel: ThinkingMedium, ThinkingBudgets: budgets}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	thinking := body["thinking"].(map[string]any)
	if thinking["budget_tokens"] != float64(0) {
		t.Fatalf("explicit zero thinking budget should override default like upstream Some(0): %#v", thinking)
	}
}

func TestAnthropicProviderStreamSimpleUsesSimpleBudgetOverModelMapLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	mapped := "2048"
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL, ThinkingLevels: map[string]*string{string(ThinkingHigh): &mapped}}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key"}, ThinkingLevel: ThinkingHigh}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	thinking := body["thinking"].(map[string]any)
	if thinking["budget_tokens"] != float64(16384) {
		t.Fatalf("simple thinking should use upstream default budget instead of model map: %#v", thinking)
	}
}

func TestAnthropicProviderStreamSimplePassesCacheRetention(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL, Compat: map[string]any{"sendSessionAffinityHeaders": true}}
	_, ok := provider.StreamSimple(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}}}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key", CacheRetention: CacheLong}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	messages := body["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	cacheControl := content[0].(map[string]any)["cache_control"].(map[string]any)
	if cacheControl["ttl"] != "1h" {
		t.Fatalf("cache_control mismatch: %#v", cacheControl)
	}
}

func TestAnthropicProviderStreamSimplePassesSessionIDAndMetadata(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-session-affinity") != "sess-1" {
			t.Fatalf("x-session-affinity mismatch: %q", r.Header.Get("x-session-affinity"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL, Compat: map[string]any{"sendSessionAffinityHeaders": true}}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key", SessionID: "sess-1", Metadata: map[string]any{"user_id": "user-1"}}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	metadata := body["metadata"].(map[string]any)
	if metadata["user_id"] != "user-1" {
		t.Fatalf("metadata mismatch: %#v", metadata)
	}
}

func TestBuildAnthropicRequestBodyDefaultsMaxTokensToModelLikeUpstream(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test", MaxTokens: 8192}, Context{}, StreamOptions{})
	if body["max_tokens"] != 8192 {
		t.Fatalf("max_tokens should default to model maxTokens like upstream: %#v", body)
	}
}

func TestBuildAnthropicRequestBodyPreservesZeroModelMaxTokensLikeUpstream(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{}, StreamOptions{})
	if body["max_tokens"] != 0 {
		t.Fatalf("zero model maxTokens should stay zero like upstream: %#v", body)
	}
}

func TestBuildAnthropicRequestBodyPreservesExplicitEmptyToolsLikeUpstream(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{HasTools: true}, StreamOptions{})
	tools, ok := body["tools"].([]map[string]any)
	if !ok || len(tools) != 0 {
		t.Fatalf("explicit empty tools should serialize like upstream Some(empty): %#v", body["tools"])
	}
}

func TestAnthropicProviderCanDisableSessionAffinityHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-session-affinity") != "" {
			t.Fatalf("x-session-affinity should be omitted: %q", r.Header.Get("x-session-affinity"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL, Compat: map[string]any{"sendSessionAffinityHeaders": false}}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", SessionID: "sess-1"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
}

func TestAnthropicProviderOmitsSessionAffinityHeaderByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-session-affinity") != "" {
			t.Fatalf("x-session-affinity should be omitted by default: %q", r.Header.Get("x-session-affinity"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", SessionID: "sess-1"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
}

func TestAnthropicProviderSendsSessionAffinityForCompatProvidersByDefault(t *testing.T) {
	for _, model := range []Model{
		{ID: "claude-test", Provider: Provider("fireworks"), API: ApiAnthropic},
		{ID: "claude-test", Provider: Provider("cloudflare-ai-gateway"), API: ApiAnthropic, BaseURL: "https://gateway.example.com/anthropic"},
	} {
		t.Run(string(model.Provider), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-session-affinity") != "sess-1" {
					t.Fatalf("x-session-affinity mismatch: %q", r.Header.Get("x-session-affinity"))
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
			}))
			defer server.Close()

			provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
			model.BaseURL = server.URL + "/anthropic"
			_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", SessionID: "sess-1"}).Result()
			if !ok {
				t.Fatal("expected completed message")
			}
		})
	}
}

func TestBuildAnthropicRequestBodyUsesThinkingLevel(t *testing.T) {
	temperature := 0.7
	body := BuildAnthropicRequestBody(
		Model{ID: "claude-test"},
		Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}}},
		StreamOptions{Temperature: &temperature, ProviderExtras: map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": 16384}}},
	)
	thinking := body["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 16384 {
		t.Fatalf("thinking mismatch: %#v", thinking)
	}
	if _, ok := body["temperature"]; ok {
		t.Fatalf("temperature should be omitted when thinking is enabled: %#v", body)
	}
}

func TestBuildAnthropicRequestBodyUsesModelThinkingLevelMap(t *testing.T) {
	budget := "2048"
	body := BuildAnthropicRequestBody(
		Model{ID: "claude-test", ThinkingLevels: map[string]*string{string(ThinkingLow): &budget}},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": 2048}}},
	)
	thinking := body["thinking"].(map[string]any)
	if thinking["budget_tokens"] != 2048 {
		t.Fatalf("thinking mismatch: %#v", thinking)
	}
}

func TestBuildAnthropicRequestBodyDoesNotEnableThinkingWhenOff(t *testing.T) {
	temperature := 0.7
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{}, StreamOptions{Temperature: &temperature})
	if _, ok := body["thinking"]; ok {
		t.Fatalf("thinking should be omitted: %#v", body)
	}
	if body["temperature"] != temperature {
		t.Fatalf("temperature mismatch: %#v", body)
	}
}

func TestBuildAnthropicRequestBodyIgnoresSystemRoleMessagesLikeUpstream(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{Messages: []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "be concise"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}},
	}}, StreamOptions{})
	if _, ok := body["system"]; ok {
		t.Fatalf("system role messages should be ignored like upstream Message enum: %#v", body["system"])
	}
	messages := body["messages"].([]map[string]any)
	if len(messages) != 1 || messages[0]["role"] != "user" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
}

func TestBuildAnthropicRequestBodyUsesContextSystemPrompt(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{
		SystemPrompt: "be concise",
		Messages:     []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}},
	}, StreamOptions{CacheRetention: CacheEphemeral})
	system := body["system"].([]map[string]any)
	if len(system) != 1 || system[0]["type"] != "text" || system[0]["text"] != "be concise" || system[0]["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("system mismatch: %#v", system)
	}
	messages := body["messages"].([]map[string]any)
	if len(messages) != 1 || messages[0]["role"] != "user" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
}

func TestBuildAnthropicRequestBodyPreservesExplicitEmptySystemPromptLikeUpstream(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{
		HasSystemPrompt: true,
		Messages:        []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}},
	}, StreamOptions{})
	system, ok := body["system"].([]map[string]any)
	if !ok || len(system) != 1 || system[0]["type"] != "text" || system[0]["text"] != "" {
		t.Fatalf("explicit empty system prompt should be preserved like upstream Some(empty): %#v", body["system"])
	}
}

func TestBuildAnthropicRequestBodyIgnoresMultipleSystemRoleTextBlocksLikeUpstream(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{Messages: []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "one"}, {Type: ContentText, Text: "two"}}},
	}}, StreamOptions{})
	if _, ok := body["system"]; ok {
		t.Fatalf("system role text blocks should be ignored like upstream: %#v", body["system"])
	}
}

func TestBuildAnthropicRequestBodyIgnoresEmptySystemRoleTextBlockLikeUpstream(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{Messages: []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: ""}}},
	}}, StreamOptions{})
	if _, ok := body["system"]; ok {
		t.Fatalf("empty system role text block should be ignored like upstream: %#v", body["system"])
	}
}

func TestBuildAnthropicRequestBodyAppliesCacheControl(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{
		SystemPrompt: "sys",
		Messages: []Message{
			{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "first"}}},
			{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "last"}}},
		},
		Tools: []Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}},
	}, StreamOptions{CacheRetention: CacheEphemeral})
	system := body["system"].([]map[string]any)
	if system[0]["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("system cache mismatch: %#v", system)
	}
	messages := body["messages"].([]map[string]any)
	firstContent := messages[0]["content"].([]map[string]any)
	if _, ok := firstContent[0]["cache_control"]; ok {
		t.Fatalf("first user should not get cache_control: %#v", firstContent)
	}
	lastContent := messages[1]["content"].([]map[string]any)
	if lastContent[0]["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("last user cache mismatch: %#v", lastContent)
	}
	tools := body["tools"].([]map[string]any)
	if tools[0]["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("tool cache mismatch: %#v", tools)
	}
}

func TestBuildAnthropicRequestBodyLongCacheControlAddsTTL(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{Messages: []Message{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}},
	}}, StreamOptions{CacheRetention: CacheLong})
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	cacheControl := content[0]["cache_control"].(map[string]any)
	if cacheControl["type"] != "ephemeral" || cacheControl["ttl"] != "1h" {
		t.Fatalf("cache_control mismatch: %#v", cacheControl)
	}
}

func TestBuildAnthropicRequestBodyCanDisableLongCacheRetention(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test", Compat: map[string]any{"supportsLongCacheRetention": false}}, Context{Messages: []Message{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}},
	}}, StreamOptions{CacheRetention: CacheLong})
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	cacheControl := content[0]["cache_control"].(map[string]any)
	if cacheControl["type"] != "ephemeral" {
		t.Fatalf("cache_control mismatch: %#v", cacheControl)
	}
	if _, ok := cacheControl["ttl"]; ok {
		t.Fatalf("ttl should be omitted: %#v", cacheControl)
	}
}

func TestBuildAnthropicRequestBodyCanDisableToolCacheControl(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test", Compat: map[string]any{"supportsCacheControlOnTools": false}}, Context{Tools: []Tool{
		{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}},
	}}, StreamOptions{CacheRetention: CacheEphemeral})
	tools := body["tools"].([]map[string]any)
	if _, ok := tools[0]["cache_control"]; ok {
		t.Fatalf("tool cache_control should be omitted: %#v", tools)
	}
}

func TestBuildAnthropicRequestBodyFireworksDefaultsDisableUnsupportedCacheControl(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test", Provider: Provider("fireworks")}, Context{
		Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}},
		Tools:    []Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}},
	}, StreamOptions{CacheRetention: CacheLong})
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	cacheControl := content[0]["cache_control"].(map[string]any)
	if _, ok := cacheControl["ttl"]; ok {
		t.Fatalf("ttl should be omitted for fireworks by default: %#v", cacheControl)
	}
	tools := body["tools"].([]map[string]any)
	if _, ok := tools[0]["cache_control"]; ok {
		t.Fatalf("tool cache_control should be omitted for fireworks by default: %#v", tools)
	}
}

func TestBuildAnthropicRequestBodyCompatCanEnableFireworksCacheControl(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test", Provider: Provider("fireworks"), Compat: map[string]any{"supportsLongCacheRetention": true, "supportsCacheControlOnTools": true}}, Context{
		Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}},
		Tools:    []Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}},
	}, StreamOptions{CacheRetention: CacheLong})
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	cacheControl := content[0]["cache_control"].(map[string]any)
	if cacheControl["ttl"] != "1h" {
		t.Fatalf("ttl mismatch: %#v", cacheControl)
	}
	tools := body["tools"].([]map[string]any)
	if tools[0]["cache_control"].(map[string]any)["ttl"] != "1h" {
		t.Fatalf("tool cache_control mismatch: %#v", tools)
	}
}

func TestBuildAnthropicRequestBodyAppliesShortCacheControlByDefault(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{Messages: []Message{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}},
	}}, StreamOptions{})
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	if content[0]["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("cache_control mismatch: %#v", content)
	}
}

func TestBuildAnthropicRequestBodyOmitsCacheControlWhenNone(t *testing.T) {
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{Messages: []Message{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}},
	}}, StreamOptions{CacheRetention: CacheNone})
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	if _, ok := content[0]["cache_control"]; ok {
		t.Fatalf("cache_control should be omitted: %#v", content)
	}
}

func TestBuildAnthropicRequestBodyUsesThinkingProviderExtra(t *testing.T) {
	temperature := 0.7
	body := BuildAnthropicRequestBody(Model{ID: "claude-test"}, Context{}, StreamOptions{
		Temperature: &temperature,
		ProviderExtras: map[string]any{
			"thinking": map[string]any{"type": "enabled", "budget_tokens": 1234},
		},
	})
	thinking := body["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 1234 {
		t.Fatalf("thinking mismatch: %#v", thinking)
	}
	if _, ok := body["temperature"]; ok {
		t.Fatalf("temperature should be omitted with thinking enabled: %#v", body)
	}
}

func TestConvertMessagesForAnthropicPreservesToolResultContentBlocks(t *testing.T) {
	messages := ConvertMessagesForAnthropic([]Message{{
		Role:       RoleTool,
		ToolCallID: "tu_1",
		Content: []ContentBlock{
			{Type: ContentText, Text: "one"},
			{Type: ContentText, Text: "two"},
		},
	}})
	content := messages[0]["content"].([]map[string]any)
	toolResult := content[0]
	blocks := toolResult["content"].([]map[string]any)
	if len(blocks) != 2 || blocks[0]["type"] != "text" || blocks[0]["text"] != "one" || blocks[1]["text"] != "two" {
		t.Fatalf("tool result content mismatch: %#v", toolResult["content"])
	}
}

func TestConvertMessagesForAnthropicPreservesImageBlocks(t *testing.T) {
	messages := ConvertMessagesForAnthropic([]Message{{
		Role: RoleUser,
		Content: []ContentBlock{
			{Type: ContentImage, MimeType: "image/png", Data: "abc"},
		},
	}})
	content := messages[0]["content"].([]map[string]any)
	image := content[0]
	source := image["source"].(map[string]any)
	if image["type"] != "image" || source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != "abc" {
		t.Fatalf("image mismatch: %#v", image)
	}
}

func TestConvertMessagesForAnthropicDoesNotInferToolResultErrorFromStopReasonLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForAnthropic([]Message{{
		Role:       RoleTool,
		ToolCallID: "tu_1",
		Content:    []ContentBlock{{Type: ContentText, Text: "failed"}},
		StopReason: StopReasonError,
	}})
	content := messages[0]["content"].([]map[string]any)
	toolResult := content[0]
	if toolResult["is_error"] != false {
		t.Fatalf("tool result mismatch: %#v", toolResult)
	}
}

func TestConvertMessagesForAnthropicUsesExplicitToolResultError(t *testing.T) {
	messages := ConvertMessagesForAnthropic([]Message{{
		Role:       RoleTool,
		ToolCallID: "tu_1",
		Content:    []ContentBlock{{Type: ContentText, Text: "failed"}},
		IsError:    true,
	}})
	content := messages[0]["content"].([]map[string]any)
	if content[0]["is_error"] != true {
		t.Fatalf("tool result mismatch: %#v", content[0])
	}
}

func TestConvertMessagesForAnthropicPreservesToolCallContentBlock(t *testing.T) {
	messages := ConvertMessagesForAnthropic([]Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{
			ID:        "toolu_1",
			Name:      "read",
			Arguments: map[string]any{"path": "README.md"},
		}}},
	}})
	if len(messages) != 1 {
		t.Fatalf("messages mismatch: %#v", messages)
	}
	content := messages[0]["content"].([]map[string]any)
	toolUse := content[0]
	if toolUse["type"] != "tool_use" || toolUse["id"] != "toolu_1" || toolUse["name"] != "read" || toolUse["input"].(map[string]any)["path"] != "README.md" {
		t.Fatalf("tool use mismatch: %#v", toolUse)
	}
}

func TestConvertMessagesForAnthropicPreservesEmptyAssistantLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForAnthropic([]Message{{Role: RoleAssistant}})
	if len(messages) != 1 || messages[0]["role"] != "assistant" {
		t.Fatalf("empty assistant message should be preserved like upstream: %#v", messages)
	}
	content := messages[0]["content"].([]map[string]any)
	if len(content) != 0 {
		t.Fatalf("empty assistant content mismatch: %#v", content)
	}
}

func TestConvertMessagesForAnthropicPreservesThinking(t *testing.T) {
	messages := ConvertMessagesForAnthropic([]Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{
			Type:              ContentThinking,
			Thinking:          "plan",
			ThinkingSignature: "sig-1",
		}},
	}})
	if len(messages) != 1 {
		t.Fatalf("messages mismatch: %#v", messages)
	}
	content := messages[0]["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "thinking" || content[0]["thinking"] != "plan" || content[0]["signature"] != "sig-1" {
		t.Fatalf("content mismatch: %#v", content)
	}
}

func TestConvertMessagesForAnthropicPreservesExplicitEmptyThinkingSignatureLikeUpstream(t *testing.T) {
	var block ContentBlock
	if err := json.Unmarshal([]byte(`{"type":"thinking","thinking":"plan","thinkingSignature":""}`), &block); err != nil {
		t.Fatal(err)
	}
	messages := ConvertMessagesForAnthropic([]Message{{Role: RoleAssistant, Content: []ContentBlock{block}}})
	content := messages[0]["content"].([]map[string]any)
	if signature, ok := content[0]["signature"].(string); !ok || signature != "" {
		t.Fatalf("explicit empty thinking signature should be preserved like upstream Some(empty): %#v", content[0])
	}
}

func TestConvertMessagesForAnthropicSerializesRedactedThinkingLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForAnthropic([]Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{
			Type:              ContentThinking,
			Thinking:          "[Reasoning redacted]",
			ThinkingSignature: "redacted-data",
			Redacted:          true,
		}},
	}})
	content := messages[0]["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "thinking" || content[0]["thinking"] != "[Reasoning redacted]" || content[0]["signature"] != "redacted-data" {
		t.Fatalf("content mismatch: %#v", content)
	}
}

func TestConsumeAnthropicSSEMapsMaxTokensStopReason(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonMaxTokens {
		t.Fatalf("max tokens mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeAnthropicSSEMapsRefusalStopReason(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"sorry\"}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonEndTurn || message.Text() != "sorry" {
		t.Fatalf("refusal mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeAnthropicSSEUsesStopReasonOverToolPresenceLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{}}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonEndTurn || len(message.ToolCalls) != 1 {
		t.Fatalf("stop reason should follow upstream message_delta, not tool presence: %#v ok=%v", message, ok)
	}
}

func TestConsumeAnthropicSSEAccumulatesUsage(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":5,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":3}}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil || message.Usage.InputTokens != 5 || message.Usage.OutputTokens != 7 || message.Usage.CacheReadTokens != 2 || message.Usage.CacheWriteTokens != 3 || message.Usage.TotalTokenCount != 17 || !message.Usage.HasTotalTokens || message.Usage.TotalTokens() != 17 {
		t.Fatalf("usage mismatch: %#v ok=%v", message.Usage, ok)
	}
	if message.ResponseID != "msg_1" {
		t.Fatalf("response id mismatch: %#v", message)
	}
}

func TestConsumeAnthropicSSEIgnoresNonU64UsageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":-1,\"cache_read_input_tokens\":1.5}}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":2,\"cache_creation_input_tokens\":4}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil {
		t.Fatalf("expected completed message with usage: %#v ok=%v", message, ok)
	}
	if message.Usage.InputTokens != 0 || message.Usage.OutputTokens != 2 || message.Usage.CacheReadTokens != 0 || message.Usage.CacheWriteTokens != 4 || message.Usage.TotalTokens() != 6 {
		t.Fatalf("non-u64 usage fields should be ignored like upstream as_u64: %#v", message.Usage)
	}
}

func TestConsumeAnthropicSSENormalizesInvalidContentBlockIndexLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":-1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1.5,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("invalid indexes should default to content block 0 like upstream as_u64: %#v ok=%v", message.ToolCalls, ok)
	}
	for _, event := range stream.Events() {
		if event.Type == EventToolCallDelta && event.ContentIndex != 0 {
			t.Fatalf("tool arg delta index should normalize to 0: %#v", event)
		}
	}
}

func TestConsumeAnthropicSSEDoesNotEmitUsageEventLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range stream.Events() {
		if event.Type == EventUsage {
			t.Fatalf("Anthropic stream should not emit separate usage event like upstream: %#v", stream.Events())
		}
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil || message.Usage.OutputTokens != 7 {
		t.Fatalf("usage should remain on terminal message: %#v ok=%v", message.Usage, ok)
	}
}

func TestConsumeAnthropicSSEMessageStartDoesNotEmitMetadataLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\"}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(stream.Events()) != 0 {
		t.Fatalf("message_start should only update partial state like upstream: %#v", stream.Events())
	}
}

func TestConsumeAnthropicSSEAccumulatesUsageAcrossDeltas(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":4}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil || message.Usage.OutputTokens != 7 {
		t.Fatalf("usage mismatch: %#v ok=%v", message.Usage, ok)
	}
}

func TestConsumeAnthropicSSEIgnoresInitialTextBlockLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"hel\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Text != "lo" {
		t.Fatalf("text mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeAnthropicSSEDoneEventCarriesTerminalMessageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSEForModel(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"hi\"}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream, Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic})
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventDone || last.Message == nil || last.Message.Text() != "" || last.Message.StopReason != StopReasonMaxTokens || last.Message.Model != "claude-test" {
		t.Fatalf("done event should carry terminal message: %#v", last)
	}
}

func TestConsumeAnthropicSSEIgnoresInitialThinkingBlockLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"pla\",\"signature\":\"sig-\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"n\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"1\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Thinking != "n" || message.Content[0].ThinkingSignature != "1" {
		t.Fatalf("thinking mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeAnthropicSSEThinkingStartDoesNotUseInitialFieldsLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"prefill\",\"signature\":\"sig\"}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventThinkingStart {
		t.Fatalf("thinking start events mismatch: %#v", events)
	}
	if len(events[0].Partial.Content) != 1 || events[0].Partial.Content[0].Thinking != "" || events[0].Partial.Content[0].ThinkingSignature != "" {
		t.Fatalf("thinking start should initialize an empty block like upstream: %#v", events[0])
	}
}

func TestConsumeAnthropicSSEParsesPartialToolArguments(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"README.md\\\"\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool call mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestConsumeAnthropicSSEContentBlockStopConsumesToolArgBufferLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"line\\\":1}\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	toolEndCount := 0
	for _, event := range stream.Events() {
		if event.Type == EventToolCallEnd {
			toolEndCount++
			if toolEndCount == 2 && (event.ToolCall == nil || event.ToolCall.Arguments["line"] != json.Number("1") || event.ToolCall.Arguments["path"] != nil) {
				t.Fatalf("second stop should parse only the fresh buffer like upstream: %#v", stream.Events())
			}
		}
	}
	if toolEndCount != 2 {
		t.Fatalf("expected two tool end events: %#v", stream.Events())
	}
}

func TestConsumeAnthropicSSEMessageStopDoesNotFlushToolArgBufferLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || len(message.ToolCalls[0].Arguments) != 0 {
		t.Fatalf("message_stop should not parse buffered tool args like upstream: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestConsumeAnthropicSSEToolStartIgnoresInitialInputLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{\"path\":\"README.md\"}}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || len(message.ToolCalls[0].Arguments) != 0 {
		t.Fatalf("tool start should initialize empty arguments like upstream: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestConsumeAnthropicSSEInputJSONDeltaUsesPayloadIndexLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"README.md\\\"\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || len(message.ToolCalls[0].Arguments) != 0 {
		t.Fatalf("input_json_delta should not be remapped to current tool index: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestConsumeAnthropicSSEEmitsToolArgumentDeltas(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\"}}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	for _, event := range events {
		if event.Type == EventToolCallDelta && event.Delta == "{\"path\":" && event.ToolCall == nil && event.Partial != nil && event.Partial.Content[1].ToolCall != nil && event.Partial.Content[1].ToolCall.ID == "toolu_1" {
			return
		}
	}
	t.Fatalf("events mismatch: %#v", events)
}

func TestConsumeAnthropicSSEEmitsThinkingDeltas(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Type != ContentThinking || message.Content[0].Thinking != "plan" {
		t.Fatalf("thinking mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeAnthropicSSEEmitsEmptyThinkingDeltaLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"\"}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventThinkingDelta || events[0].Delta != "" {
		t.Fatalf("empty thinking delta should be emitted like upstream: %#v", events)
	}
}

func TestConsumeAnthropicSSEEmitsContentBlockStartEventsLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"thinking\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool-1\",\"name\":\"read\",\"input\":{}}}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 3 || events[0].Type != EventTextStart || events[0].ContentIndex != 0 || events[1].Type != EventThinkingStart || events[1].ContentIndex != 1 || events[2].Type != EventToolCallStart || events[2].ContentIndex != 2 {
		t.Fatalf("content_block_start events mismatch: %#v", events)
	}
	if events[0].Partial == nil || events[0].Partial.Content[0].Type != ContentText || events[1].Partial == nil || events[1].Partial.Content[1].Type != ContentThinking || events[2].Partial == nil || events[2].Partial.Content[2].ToolCall == nil || events[2].Partial.Content[2].ToolCall.ID != "tool-1" {
		t.Fatalf("content_block_start partials mismatch: %#v", events)
	}
}

func TestConsumeAnthropicSSEContentBlockStartFillsSkippedIndexesWithTextLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"thinking\"}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Partial == nil || len(events[0].Partial.Content) != 3 {
		t.Fatalf("start partial mismatch: %#v", events)
	}
	if events[0].Partial.Content[0].Type != ContentText || events[0].Partial.Content[0].Text != "" || events[0].Partial.Content[1].Type != ContentText || events[0].Partial.Content[1].Text != "" || events[0].Partial.Content[2].Type != ContentThinking {
		t.Fatalf("skipped indexes should be empty text blocks like upstream: %#v", events[0].Partial.Content)
	}
}

func TestConsumeAnthropicSSETextStartDoesNotEmitInitialTextLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"prefill\"}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventTextStart {
		t.Fatalf("text start events mismatch: %#v", events)
	}
	if len(events[0].Partial.Content) != 1 || events[0].Partial.Content[0].Text != "" {
		t.Fatalf("text start should initialize an empty block like upstream: %#v", events[0])
	}
}

func TestConsumeAnthropicSSEDeltaEventsCarryUpdatedPartialLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"thinking\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 4 || events[1].Type != EventTextDelta || events[1].ContentIndex != 0 || events[1].Partial == nil || events[1].Partial.Content[0].Text != "hi" || events[3].Type != EventThinkingDelta || events[3].ContentIndex != 1 || events[3].Partial == nil || events[3].Partial.Content[1].Thinking != "plan" {
		t.Fatalf("delta partials mismatch: %#v", events)
	}
}

func TestConsumeAnthropicSSEEmitsContentBlockEndEventsLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventTextEnd || last.ContentIndex != 0 || last.Content != "hi" || last.Partial == nil || last.Partial.Content[0].Text != "hi" {
		t.Fatalf("content_block_stop should emit text_end like upstream: %#v", events)
	}
}

func TestConsumeAnthropicSSEEmitsThinkingAndToolCallEndEventsLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool-1\",\"name\":\"read\",\"input\":{}}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	foundThinkingEnd := false
	foundToolEnd := false
	for _, event := range events {
		if event.Type == EventThinkingEnd && event.ContentIndex == 0 && event.Content == "plan" && event.Partial != nil {
			foundThinkingEnd = true
		}
		if event.Type == EventToolCallEnd && event.ContentIndex == 1 && event.ToolCall != nil && event.ToolCall.ID == "tool-1" && event.Partial != nil {
			foundToolEnd = true
		}
	}
	if !foundThinkingEnd || !foundToolEnd {
		t.Fatalf("content_block_stop end events mismatch: %#v", events)
	}
}

func TestConsumeAnthropicSSEEmitsThinkingDeltaImmediately(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventThinkingDelta || events[0].Delta != "plan" {
		t.Fatalf("events mismatch: %#v", events)
	}
}

func TestConsumeAnthropicSSEDeltaWithoutStartDoesNotCreatePartialContentLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 2 || events[0].Type != EventTextDelta || events[1].Type != EventThinkingDelta {
		t.Fatalf("events mismatch: %#v", events)
	}
	if events[0].Partial == nil || len(events[0].Partial.Content) != 0 || events[1].Partial == nil || len(events[1].Partial.Content) != 0 {
		t.Fatalf("delta partials should not synthesize missing content blocks like upstream: %#v", events)
	}
}

func TestConsumeAnthropicSSEMessageStopUsesProviderPartialLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 0 {
		t.Fatalf("terminal message should come from provider partial, not replayed deltas: %#v ok=%v", message, ok)
	}
}

func TestConsumeAnthropicSSEIgnoresSignatureDeltaWithoutThinkingStartLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-1\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 0 {
		t.Fatalf("thinking signature mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeAnthropicSSESignatureDeltaUpdatesPartialWithoutImmediateEvent(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-1\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range stream.Events() {
		if event.Type == EventContentUpdate {
			t.Fatalf("Anthropic thinking stream should not emit content_update like upstream: %#v", stream.Events())
		}
		if event.Type == EventThinkingEnd {
			if event.ContentBlock != nil {
				t.Fatalf("thinking_end should not carry Go-only content_block like upstream: %#v", event)
			}
			if event.Partial == nil || len(event.Partial.Content) != 1 || event.Partial.Content[0].ThinkingSignature != "sig-1" {
				t.Fatalf("thinking_end partial should carry signature: %#v", event)
			}
			return
		}
	}
	t.Fatalf("missing thinking_end event: %#v", stream.Events())
}

func TestConsumeAnthropicSSESignatureDeltaWithoutThinkingStartDoesNotCreateContentLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-1\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 0 {
		t.Fatalf("signature_delta without thinking start should not synthesize content: %#v ok=%v", message, ok)
	}
	for _, event := range stream.Events() {
		if event.Type == EventContentUpdate {
			t.Fatalf("signature_delta without thinking start should not emit content_update: %#v", stream.Events())
		}
	}
}

func TestConsumeAnthropicSSEInputJSONDeltaWithoutToolStartStillEmitsDeltaLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range stream.Events() {
		if event.Type == EventToolCallDelta {
			if event.ContentIndex != 0 || event.Delta != "{\"path\":" || event.Partial == nil || len(event.Partial.Content) != 0 {
				t.Fatalf("tool delta mismatch: %#v", event)
			}
			return
		}
	}
	t.Fatalf("missing tool call delta without tool start like upstream: %#v", stream.Events())
}

func TestConsumeAnthropicSSEThinkingEndPreservesSignatureInResult(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"plan\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-1\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].ThinkingSignature != "sig-1" {
		t.Fatalf("thinking signature should survive content_block_stop: %#v ok=%v", message, ok)
	}
}

func TestConsumeAnthropicSSEEmitsRedactedThinkingBlock(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSE(strings.NewReader(strings.Join([]string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"redacted_thinking\",\"data\":\"redacted-data\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Type != ContentThinking || !message.Content[0].Redacted || message.Content[0].ThinkingSignature != "redacted-data" {
		t.Fatalf("redacted thinking mismatch: %#v ok=%v", message, ok)
	}
	events := stream.Events()
	if len(events) == 0 || events[0].Type != EventThinkingStart || events[0].Partial == nil || len(events[0].Partial.Content) != 1 || !events[0].Partial.Content[0].Redacted || events[0].Partial.Content[0].ThinkingSignature != "redacted-data" {
		t.Fatalf("redacted thinking should start like upstream: %#v", events)
	}
}

func TestAnthropicProviderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer server.Close()
	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "m", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k"})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed error")
	}
	if message.StopReason != StopReasonError || message.ErrorMessage == "" {
		t.Fatalf("expected error, got %#v", message)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "m" || events[0].Message.Provider != Provider("anthropic") || events[0].Message.API != ApiAnthropic || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "HTTP 400 Bad Request: bad" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("HTTP error should carry provider-aware upstream message: %#v", events)
	}
}

func TestAnthropicProviderSendErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewAnthropicProvider(WithAnthropicHTTPClient(&http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}))
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: "https://anthropic.invalid"}
	maxRetries := 0
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k", MaxRetries: &maxRetries})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "http error: Post \"https://anthropic.invalid/v1/messages\": dial failed" {
		t.Fatalf("send error mismatch: %#v ok=%v", message, ok)
	}
}

func TestAnthropicProviderNewRequestErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewAnthropicProvider()
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic, BaseURL: "://bad-url"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http error: ") {
		t.Fatalf("request build error mismatch: %#v ok=%v", message, ok)
	}
}

func TestAnthropicProviderMissingAPIKeyErrorCarriesModelLikeUpstream(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	provider := NewAnthropicProvider()
	model := Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{})
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "claude-test" || events[0].Message.Provider != Provider("anthropic") || events[0].Message.API != ApiAnthropic || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "ANTHROPIC_API_KEY is not set" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("missing key should carry provider-aware upstream message: %#v", events)
	}
}

func TestConsumeAnthropicSSEForModelErrorEventMatchesUpstreamShape(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeAnthropicSSEForModel(strings.NewReader("event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"bad anthropic\"}}\n\n"), stream, Model{ID: "claude-test", Provider: Provider("anthropic"), API: ApiAnthropic})
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "claude-test" || events[0].Message.Provider != Provider("anthropic") || events[0].Message.API != ApiAnthropic || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "bad anthropic" {
		t.Fatalf("SSE error should carry upstream-style message: %#v", events)
	}
}
