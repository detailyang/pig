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

type roundTripErrorFunc func(*http.Request) (*http.Response, error)

func (fn roundTripErrorFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestBuildResponsesURL(t *testing.T) {
	cases := map[string]string{
		"https://api.openai.com":               "https://api.openai.com/v1/responses",
		"https://api.openai.com/v1":            "https://api.openai.com/v1/responses",
		"https://api.openai.com/v1/":           "https://api.openai.com/v1/responses",
		"https://x.com/v2":                     "https://x.com/v2/responses",
		"https://gateway.example.com/openai":   "https://gateway.example.com/openai/v1/responses",
		"https://gateway.example.com/v1/proxy": "https://gateway.example.com/v1/proxy/responses",
	}
	for input, want := range cases {
		if got := BuildResponsesURL(input); got != want {
			t.Fatalf("%s => %s, want %s", input, got, want)
		}
	}
}

func TestOpenAIResponsesProviderRequestAndSSE(t *testing.T) {
	var body map[string]any
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization mismatch: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("accept mismatch: %s", r.Header.Get("Accept"))
		}
		if r.Header.Get("session_id") != "sess-1" || r.Header.Get("x-client-request-id") != "sess-1" {
			t.Fatalf("session headers mismatch session_id=%q x-client-request-id=%q", r.Header.Get("session_id"), r.Header.Get("x-client-request-id"))
		}
		if r.Header.Get("x-extra") != "present" {
			t.Fatalf("extra header mismatch: %q", r.Header.Get("x-extra"))
		}
		if r.Header.Get("x-model-header") != "model" || r.Header.Get("x-shared-header") != "options" {
			t.Fatalf("model headers mismatch model=%q shared=%q", r.Header.Get("x-model-header"), r.Header.Get("x-shared-header"))
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		rawBody = string(data)
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: response.output_text.delta\ndata: {\"delta\":\"hel\"}\n\n",
			"event: response.output_text.delta\ndata: {\"delta\":\"lo\"}\n\n",
			"event: response.output_item.added\ndata: {\"item\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"read\"}}\n\n",
			"event: response.function_call_arguments.done\ndata: {\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}\n\n",
			"event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"function_call\"}],\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"input_tokens_details\":{\"cached_tokens\":2,\"cache_write_tokens\":1}}}}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL, Headers: map[string]string{"x-model-header": "model", "x-shared-header": "model"}}
	request := Context{
		Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "<tag>&value"}}}},
		Tools:    []Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}},
	}
	message, ok := provider.Stream(context.Background(), model, request, StreamOptions{APIKey: "test-key", MaxTokens: 128, SessionID: "sess-1", Headers: map[string]string{"x-extra": "present", "x-shared-header": "options"}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "hello" {
		t.Fatalf("text mismatch: %q", message.Text())
	}
	if message.StopReason != StopReasonToolCalls {
		t.Fatalf("stop reason mismatch: %s", message.StopReason)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call-1" || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool call mismatch: %#v", message.ToolCalls)
	}
	if message.Usage == nil || message.Usage.InputTokens != 3 || message.Usage.OutputTokens != 4 || message.Usage.CacheReadTokens != 2 || message.Usage.CacheWriteTokens != 1 || message.Usage.TotalTokenCount != 10 || !message.Usage.HasTotalTokens || message.Usage.TotalTokens() != 10 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
	if message.Role != AssistantRoleAssistant || message.API != ApiOpenAIResponses || message.Provider != Provider("openai") || message.Model != "gpt-test" || message.Timestamp == 0 {
		t.Fatalf("metadata mismatch: %#v", message)
	}
	if body["model"] != "gpt-test" || body["stream"] != true || body["store"] != false || body["max_output_tokens"] != float64(128) || body["prompt_cache_key"] != "sess-1" {
		t.Fatalf("request body mismatch: %#v", body)
	}
	input := body["input"].([]any)
	if input[0].(map[string]any)["role"] != "user" {
		t.Fatalf("input mismatch: %#v", input)
	}
	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("request body should not HTML-escape JSON strings like upstream serde_json: %s", rawBody)
	}
	if !strings.Contains(rawBody, `"text":"<tag>&value"`) {
		t.Fatalf("request body missing unescaped content: %s", rawBody)
	}
	tools := body["tools"].([]any)
	if tools[0].(map[string]any)["type"] != "function" || tools[0].(map[string]any)["name"] != "read" {
		t.Fatalf("tools mismatch: %#v", tools)
	}
}

func TestOpenAIResponsesProviderReturnsLiveStreamBeforeDone(t *testing.T) {
	finalChunk := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: {\"delta\":\"hi\"}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-finalChunk
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()
	defer close(finalChunk)

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for index := 0; ; {
		event, next, err := stream.Next(ctx, index)
		if err != nil {
			t.Fatal(err)
		}
		index = next
		if event.Type == EventTextDelta {
			if event.Delta != "hi" {
				t.Fatalf("text delta mismatch: %#v", event)
			}
			return
		}
	}
}

func TestOpenAIResponsesProviderMissingAPIKeyMentionsProviderEnvLikeUpstream(t *testing.T) {
	provider := NewOpenAIResponsesProvider()
	model := Model{ID: "deepseek-reasoner", Provider: Provider("deepseek"), API: ApiOpenAIResponses}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed error")
	}
	want := "no API key for provider: deepseek; set DEEPSEEK_API_KEY or pass options.api_key"
	if message.StopReason != StopReasonError || message.ErrorMessage != want {
		t.Fatalf("missing key message mismatch: %#v", message)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "deepseek-reasoner" || events[0].Message.Provider != Provider("deepseek") || events[0].Message.API != ApiOpenAIResponses || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != want || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("missing key should carry provider-aware upstream message: %#v", events)
	}
}

func TestOpenAIResponsesProviderRetriesServerError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: {\"delta\":\"ok\"}\n\nevent: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL}
	maxRetryDelayMS := 1
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", MaxRetryDelayMS: &maxRetryDelayMS}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if attempts != 2 || message.Text() != "ok" {
		t.Fatalf("retry mismatch attempts=%d text=%q", attempts, message.Text())
	}
}

func TestOpenAIResponsesProviderCanDisableRetries(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, "temporary", http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL}
	maxRetries := 0
	provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", MaxRetries: &maxRetries}).Result()
	if attempts != 1 {
		t.Fatalf("expected one attempt, got %d", attempts)
	}
}

func TestBuildOpenAIResponsesRequestBodyUsesSessionIDForPromptCache(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(Model{ID: "gpt-test"}, Context{}, StreamOptions{SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	if body["prompt_cache_key"] != "sess-1" {
		t.Fatalf("prompt cache key mismatch: %#v", body)
	}
}

func TestBuildOpenAIResponsesRequestBodyUsesContextSystemPrompt(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(Model{ID: "gpt-test"}, Context{
		SystemPrompt: "be helpful",
		Messages:     []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 2 || input[0]["role"] != "system" || input[1]["role"] != "user" {
		t.Fatalf("input mismatch: %#v", input)
	}
	content := input[0]["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "input_text" || content[0]["text"] != "be helpful" {
		t.Fatalf("system content mismatch: %#v", content)
	}
}

func TestBuildOpenAIResponsesRequestBodyPreservesExplicitEmptySystemPromptLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(Model{ID: "gpt-test"}, Context{
		HasSystemPrompt: true,
		Messages:        []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 2 || input[0]["role"] != "system" || input[1]["role"] != "user" {
		t.Fatalf("explicit empty system prompt should be preserved like upstream Some(empty): %#v", input)
	}
	content := input[0]["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "input_text" || content[0]["text"] != "" {
		t.Fatalf("system content mismatch: %#v", content)
	}
}

func TestBuildOpenAIResponsesRequestBodyIgnoresSystemRoleMessagesLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(Model{ID: "gpt-test"}, Context{
		Messages: []Message{
			{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "hidden"}}},
			{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}},
		},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 1 || input[0]["role"] != "user" {
		t.Fatalf("RoleSystem messages should be ignored like upstream: %#v", input)
	}
}

func TestBuildOpenAIResponsesRequestBodyOmitsPromptCacheWhenCacheNone(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(Model{ID: "gpt-test"}, Context{}, StreamOptions{SessionID: "sess-1", CacheRetention: CacheNone})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt cache key should be omitted: %#v", body)
	}
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt cache retention should be omitted: %#v", body)
	}
}

func TestBuildOpenAIResponsesRequestBodyLongCacheRetention(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(Model{ID: "gpt-test"}, Context{}, StreamOptions{SessionID: "sess-1", CacheRetention: CacheLong})
	if err != nil {
		t.Fatal(err)
	}
	if body["prompt_cache_key"] != "sess-1" || body["prompt_cache_retention"] != "24h" {
		t.Fatalf("prompt cache mismatch: %#v", body)
	}
}

func TestBuildOpenAIResponsesRequestBodyCanDisableLongCacheRetention(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(Model{ID: "gpt-test", Compat: map[string]any{"supportsLongCacheRetention": false}}, Context{}, StreamOptions{SessionID: "sess-1", CacheRetention: CacheLong})
	if err != nil {
		t.Fatal(err)
	}
	if body["prompt_cache_key"] != "sess-1" {
		t.Fatalf("prompt cache key mismatch: %#v", body)
	}
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention should be omitted: %#v", body)
	}
	compat := ResolveCompat(Model{Compat: map[string]any{"supportsLongCacheRetention": false, "sendSessionIdHeader": false, "requiresReasoningContentOnAssistantMessages": true}})
	if compat.SupportsLongCacheRetention || compat.SendSessionIDHeader || !compat.ReplayReasoningContent {
		t.Fatalf("compat mismatch: %#v", compat)
	}
	aliasBody, err := BuildRequestBody(Model{ID: "gpt-test", Compat: map[string]any{"supportsLongCacheRetention": false}}, Context{}, StreamOptions{SessionID: "sess-1", CacheRetention: CacheLong}, compat)
	if err != nil || aliasBody["prompt_cache_retention"] != nil || aliasBody["prompt_cache_key"] != "sess-1" {
		t.Fatalf("upstream-named body mismatch body=%#v err=%v", aliasBody, err)
	}
}

func TestOpenAIResponsesUpstreamNamedHelpers(t *testing.T) {
	tools := SerializeTools([]Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}})
	if len(tools) != 1 || tools[0]["type"] != "function" || tools[0]["name"] != "read" {
		t.Fatalf("serialize tools mismatch: %#v", tools)
	}
	partial := EmptyPartial(Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses})
	if partial.Model != "gpt-test" || partial.Provider != Provider("openai") || partial.API != ApiOpenAIResponses || partial.Role != AssistantRoleAssistant || partial.Usage == nil || len(partial.Content) != 0 {
		t.Fatalf("empty partial mismatch: %#v", partial)
	}
}

func TestBuildOpenAIResponsesRequestBodyUsesServiceTierExtra(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(Model{ID: "gpt-test"}, Context{}, StreamOptions{ProviderExtras: map[string]any{"service_tier": "priority"}})
	if err != nil {
		t.Fatal(err)
	}
	if body["service_tier"] != "priority" {
		t.Fatalf("service tier mismatch: %#v", body)
	}
}

func TestBuildOpenAIResponsesRequestBodyPreservesExplicitEmptyToolsLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(Model{ID: "gpt-test"}, Context{HasTools: true}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tools, ok := body["tools"].([]map[string]any)
	if !ok || len(tools) != 0 {
		t.Fatalf("explicit empty tools should be serialized like upstream Some(empty), got %#v", body["tools"])
	}
}

func TestOpenAIResponsesProviderStreamSimplePassesSessionID(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("session_id") != "sess-1" || r.Header.Get("x-client-request-id") != "sess-1" {
			t.Fatalf("session headers mismatch session_id=%q x-client-request-id=%q", r.Header.Get("session_id"), r.Header.Get("x-client-request-id"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key", SessionID: "sess-1", ProviderExtras: map[string]any{"service_tier": "priority"}}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["prompt_cache_key"] != "sess-1" {
		t.Fatalf("prompt cache key mismatch: %#v", body)
	}
	if body["service_tier"] != "priority" {
		t.Fatalf("service tier mismatch: %#v", body)
	}
}

func TestOpenAIResponsesProviderStreamSimpleMapsThinkingLevelLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	mapped := "xhigh"
	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL, Reasoning: true, ThinkingLevels: map[string]*string{string(ThinkingHigh): &mapped}}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key"}, ThinkingLevel: ThinkingHigh}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "xhigh" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning mismatch: %#v", reasoning)
	}
}

func TestOpenAIResponsesProviderStreamSimpleOverridesBaseReasoningEffortLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL, Reasoning: true}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{

		ThinkingLevel: ThinkingHigh,
		Base:          StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "low"}, APIKey: "test-key"},
	}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("simple reasoning should override base provider extra like upstream: %#v", reasoning)
	}
}

func TestOpenAIResponsesProviderCanDisableSessionIDHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("session_id") != "" || r.Header.Get("x-client-request-id") != "sess-1" {
			t.Fatalf("session headers mismatch session_id=%q x-client-request-id=%q", r.Header.Get("session_id"), r.Header.Get("x-client-request-id"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL, Compat: map[string]any{"sendSessionIdHeader": false}}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", SessionID: "sess-1"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
}

func TestHandleOpenAIResponsesEventAccumulatesFunctionCallArgumentDeltas(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.created", data: `{"response":{"id":"resp_1"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.delta", data: `{"delta":"{\"path\":"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.delta", data: `{"delta":"\"README.md\"}"}`}, stream)
	stream.Close(DoneReasonToolCalls)
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool call mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
	if message.ResponseID != "resp_1" {
		t.Fatalf("response id mismatch: %#v", message)
	}
}

func TestHandleOpenAIResponsesToolDeltaPartialKeepsArgumentsEmptyUntilDoneLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.delta", data: `{"delta":"{\"path\":"}`}, stream)
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventToolCallDelta || last.Partial == nil || len(last.Partial.Content) != 1 || last.Partial.Content[0].ToolCall == nil {
		t.Fatalf("tool delta event mismatch: %#v", last)
	}
	if len(last.Partial.Content[0].ToolCall.Arguments) != 0 {
		t.Fatalf("tool delta partial should keep empty arguments until done like upstream: %#v", last.Partial.Content[0].ToolCall)
	}
}

func TestHandleOpenAIResponsesToolDeltaPartialIsNotMutatedByDoneLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.delta", data: `{"delta":"{\"path\":"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.done", data: `{"arguments":"{\"path\":\"README.md\"}"}`}, stream)
	events := stream.Events()
	delta := events[1]
	if delta.Type != EventToolCallDelta || delta.Partial == nil || len(delta.Partial.Content) != 1 || delta.Partial.Content[0].ToolCall == nil {
		t.Fatalf("tool delta event mismatch: %#v", events)
	}
	if len(delta.Partial.Content[0].ToolCall.Arguments) != 0 {
		t.Fatalf("tool delta partial should remain immutable after done like upstream: %#v", delta.Partial.Content[0].ToolCall)
	}
}

func TestHandleOpenAIResponsesEventCapturesInProgressResponseIDLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.in_progress", data: `{"response":{"id":"resp_in_progress"}}`}, stream)
	stream.Close(DoneReasonStop)
	message, ok := stream.Result()
	if !ok || message.ResponseID != "resp_in_progress" {
		t.Fatalf("response id mismatch: %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesCreatedUpdatesPartialWithoutEventLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.created", data: `{"response":{"id":"resp_1"}}`}, stream)
	if events := stream.Events(); len(events) != 0 {
		t.Fatalf("created should update partial without emitting an event: %#v", events)
	}
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"ok"}`}, stream)
	stream.Close(DoneReasonStop)
	message, ok := stream.Result()
	if !ok || message.ResponseID != "resp_1" {
		t.Fatalf("response id mismatch: %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesCompletedAccumulatesUsageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	partial := openAIResponsesStreamPartial(stream)
	partial.Usage = &Usage{InputTokens: 2, OutputTokens: 3, CacheReadTokens: 5, CacheWriteTokens: 7}
	HandleOpenAIResponsesEvent(sseEvent{event: "response.completed", data: `{"response":{"status":"completed","usage":{"input_tokens":11,"output_tokens":13,"input_tokens_details":{"cached_tokens":17,"cache_write_tokens":19}}}}`}, stream)

	message, ok := stream.Result()
	if !ok || message.Usage == nil {
		t.Fatalf("expected completed message with usage: %#v ok=%v", message, ok)
	}
	if message.Usage.InputTokens != 13 || message.Usage.OutputTokens != 16 || message.Usage.CacheReadTokens != 22 || message.Usage.CacheWriteTokens != 26 || message.Usage.TotalTokenCount != 77 || !message.Usage.HasTotalTokens {
		t.Fatalf("usage should accumulate like upstream: %#v", message.Usage)
	}
}

func TestIntNumberAcceptsJSONNumberLikeSerdeValue(t *testing.T) {
	if got := intNumber(json.Number("9001")); got != 9001 {
		t.Fatalf("json.Number should parse like serde_json number, got %d", got)
	}
}

func TestHandleOpenAIResponsesEventIgnoresNonU64UsageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.completed", data: `{"response":{"status":"completed","usage":{"input_tokens":-1,"output_tokens":2,"input_tokens_details":{"cached_tokens":1.5,"cache_write_tokens":4}}}}`}, stream)
	message, ok := stream.Result()
	if !ok || message.Usage == nil {
		t.Fatalf("expected completed message with usage: %#v ok=%v", message, ok)
	}
	if message.Usage.InputTokens != 0 || message.Usage.OutputTokens != 2 || message.Usage.CacheReadTokens != 0 || message.Usage.CacheWriteTokens != 4 || message.Usage.TotalTokens() != 6 {
		t.Fatalf("non-u64 usage fields should be ignored like upstream as_u64: %#v", message.Usage)
	}
}

func TestHandleOpenAIResponsesEventDoneArgumentsOverrideDeltas(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.delta", data: `{"delta":"{\"path\":\"draft\"}"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.done", data: `{"arguments":"{\"path\":\"final\"}"}`}, stream)
	stream.Close(DoneReasonToolCalls)
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "final" {
		t.Fatalf("tool call mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestHandleOpenAIResponsesEventParsesPartialDoneArguments(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.done", data: `{"arguments":"{\"path\":\"README.md\""}`}, stream)
	stream.Close(DoneReasonToolCalls)
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool call mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestBuildOpenAIResponsesRequestBodyDoesNotUseThinkingLevelWithoutProviderExtraLikeUpstream(t *testing.T) {
	mapped := "xhigh"
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true, ThinkingLevels: map[string]*string{string(ThinkingHigh): &mapped}},
		Context{},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["reasoning"]; ok {
		t.Fatalf("direct request body should only use provider_extras reasoning_effort like upstream: %#v", body)
	}
	if _, ok := body["include"]; ok {
		t.Fatalf("direct request body should omit reasoning include without provider extra like upstream: %#v", body)
	}
}

func TestBuildOpenAIResponsesRequestBodyUsesReasoningExtras(t *testing.T) {
	mapped := "xhigh"
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true, ThinkingLevels: map[string]*string{string(ThinkingHigh): &mapped}},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "high", "reasoning_summary": "detailed"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "xhigh" || reasoning["summary"] != "detailed" {
		t.Fatalf("reasoning mismatch: %#v", reasoning)
	}
	include := body["include"].([]string)
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include mismatch: %#v", include)
	}
}

func TestBuildOpenAIResponsesRequestBodyPreservesEmptyReasoningSummaryLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "high", "reasoning_summary": ""}},
	)
	if err != nil {
		t.Fatal(err)
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["summary"] != "" {
		t.Fatalf("empty reasoning_summary should be preserved like upstream Value string: %#v", reasoning)
	}
}

func TestBuildOpenAIResponsesRequestBodyReasoningExtrasOverrideThinkingLevel(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "high"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("reasoning mismatch: %#v", reasoning)
	}
}

func TestBuildOpenAIResponsesRequestBodyPreservesEmptyReasoningEffortLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": ""}},
	)
	if err != nil {
		t.Fatal(err)
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "" || reasoning["summary"] != "auto" {
		t.Fatalf("empty reasoning_effort should be preserved like upstream as_str: %#v", reasoning)
	}
}

func TestBuildOpenAIResponsesRequestBodyUnknownReasoningEffortUsesMediumMapLikeUpstream(t *testing.T) {
	mapped := "mapped-medium"
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true, ThinkingLevels: map[string]*string{string(ThinkingMedium): &mapped}},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "surprise"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "mapped-medium" {
		t.Fatalf("unknown reasoning effort should use medium map like upstream: %#v", reasoning)
	}
}

func TestBuildOpenAIResponsesRequestBodyPreservesEmptyMappedReasoningEffortLikeUpstream(t *testing.T) {
	mapped := ""
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true, ThinkingLevels: map[string]*string{string(ThinkingHigh): &mapped}},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "high"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "" {
		t.Fatalf("empty mapped reasoning effort should be preserved like upstream Some(empty): %#v", reasoning)
	}
}

func TestBuildOpenAIResponsesRequestBodyReplaysThinkingAsReasoning(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true, Compat: map[string]any{"requiresReasoningContentOnAssistantMessages": true}},
		Context{Messages: []Message{{
			Role:    RoleAssistant,
			Content: []ContentBlock{{Type: ContentThinking, Thinking: "plan"}, {Type: ContentText, Text: "answer"}},
		}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 2 || input[0]["type"] != "reasoning" || input[1]["role"] != "assistant" {
		t.Fatalf("input mismatch: %#v", input)
	}
	summary := input[0]["summary"].([]map[string]any)
	if len(summary) != 1 || summary[0]["type"] != "summary_text" || summary[0]["text"] != "plan" {
		t.Fatalf("summary mismatch: %#v", summary)
	}
}

func TestBuildOpenAIResponsesRequestBodyDoesNotReplayEmptyThinkingLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true, Compat: map[string]any{"requiresReasoningContentOnAssistantMessages": true}},
		Context{Messages: []Message{{
			Role:    RoleAssistant,
			Content: []ContentBlock{{Type: ContentThinking}, {Type: ContentText, Text: "answer"}},
		}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 1 || input[0]["type"] == "reasoning" || input[0]["role"] != "assistant" {
		t.Fatalf("empty thinking should not replay as reasoning like upstream: %#v", input)
	}
}

func TestBuildOpenAIResponsesRequestBodyDoesNotReplayThinkingWithoutCompatFlag(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test", Reasoning: true},
		Context{Messages: []Message{{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentThinking, Thinking: "plan"}, {Type: ContentText, Text: "answer"}}}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 1 || input[0]["role"] != "assistant" {
		t.Fatalf("input mismatch: %#v", input)
	}
}

func TestBuildOpenAIResponsesRequestBodyDoesNotReplayThinkingForPlainModel(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test"},
		Context{Messages: []Message{{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentThinking, Thinking: "plan"}, {Type: ContentText, Text: "answer"}}}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 1 || input[0]["role"] != "assistant" {
		t.Fatalf("input mismatch: %#v", input)
	}
}

func TestBuildOpenAIResponsesRequestBodySerializesToolCallContentBlock(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test"},
		Context{Messages: []Message{{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}}}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 1 || input[0]["type"] != "function_call" || input[0]["call_id"] != "call-1" || input[0]["name"] != "read" {
		t.Fatalf("input mismatch: %#v", input)
	}
	if input[0]["arguments"] != `{"path":"README.md"}` {
		t.Fatalf("arguments mismatch: %#v", input[0])
	}
}

func TestBuildOpenAIResponsesRequestBodyToolCallArgumentsDoNotHTMLEscapeLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test"},
		Context{Messages: []Message{{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "write", Arguments: map[string]any{"text": "a < b && c > d"}}}}}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if input[0]["arguments"] != `{"text":"a < b && c > d"}` {
		t.Fatalf("arguments should match upstream serde_json formatting: %#v", input[0])
	}
}

func TestBuildOpenAIResponsesRequestBodyIgnoresLegacyToolCallsLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test"},
		Context{Messages: []Message{{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 0 {
		t.Fatalf("legacy ToolCalls should be ignored like upstream: %#v", input)
	}
}

func TestBuildOpenAIResponsesRequestBodyUserContentIgnoresNonUserBlocksLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test"},
		Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{
			{Type: ContentThinking, Thinking: "plan"},
			{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
			{Type: ContentText, Text: "hello"},
		}}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	content := input[0]["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "input_text" || content[0]["text"] != "hello" {
		t.Fatalf("user content should only include text/image blocks like upstream: %#v", content)
	}
}

func TestBuildOpenAIResponsesRequestBodyKeepsContentToolCallWhenLegacyDuplicatePresent(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test"},
		Context{Messages: []Message{{
			Role:      RoleAssistant,
			Content:   []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}},
			ToolCalls: []ToolCall{{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
		}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 1 {
		t.Fatalf("content tool call should remain while legacy ToolCalls are ignored: %#v", input)
	}
}

func TestBuildOpenAIResponsesRequestBodySerializesNilToolCallArgumentsAsEmptyObjectLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIResponsesRequestBody(
		Model{ID: "gpt-test"},
		Context{Messages: []Message{{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read"}}}}}},
		StreamOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 1 || input[0]["arguments"] != "{}" {
		t.Fatalf("nil tool call arguments should serialize as empty object like upstream: %#v", input)
	}
}

func TestHandleOpenAIResponsesEventMapsIncompleteStatus(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{
		event: "response.completed",
		data:  `{"response":{"status":"incomplete","output":[]}}`,
	}, stream)
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonMaxTokens {
		t.Fatalf("incomplete mismatch: %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesSeparatesReasoningSummaryParts(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"reasoning"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_part.added", data: `{"summary_index":0,"part":{"type":"summary_text","text":""}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.delta", data: `{"summary_index":0,"delta":"**Planning structured confirmation question**"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.done", data: `{"summary_index":0,"text":"**Planning structured confirmation question**"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_part.added", data: `{"summary_index":1,"part":{"type":"summary_text","text":""}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.delta", data: `{"summary_index":1,"delta":"**Refining concise confirmation format**"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.done", data: `{"summary_index":1,"text":"**Refining concise confirmation format**"}`}, stream)
	stream.Close(DoneReasonStop)

	message, ok := stream.Result()
	if !ok || len(message.Content) != 2 || message.Content[0].Thinking != "**Planning structured confirmation question**" || message.Content[1].Thinking != "**Refining concise confirmation format**" {
		t.Fatalf("thinking parts = %#v ok=%v", message.Content, ok)
	}
	var deltas string
	for _, event := range stream.Events() {
		if event.Type == EventThinkingDelta {
			deltas += event.Delta
		}
	}
	if deltas != "**Planning structured confirmation question**\n\n**Refining concise confirmation format**" {
		t.Fatalf("streamed thinking = %q", deltas)
	}
}

func TestHandleOpenAIResponsesEventUsesReasoningDoneText(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.delta", data: `{"delta":"pla"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.done", data: `{"text":"plan"}`}, stream)
	stream.Close(DoneReasonStop)
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Type != ContentThinking || message.Content[0].Thinking != "plan" {
		t.Fatalf("thinking mismatch: %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesEventClearsReasoningWhenDoneHasNoTextLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.delta", data: `{"delta":"keep"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.done", data: `{}`}, stream)
	stream.Close(DoneReasonStop)
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Type != ContentThinking || message.Content[0].Thinking != "" {
		t.Fatalf("thinking mismatch: %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesEventUsesOutputTextDoneText(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"draft"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.done", data: `{"text":"final"}`}, stream)
	stream.Close(DoneReasonStop)
	message, ok := stream.Result()
	if !ok || message.Text() != "final" {
		t.Fatalf("text mismatch: %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesEventKeepsTextWhenDoneHasNoText(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"keep"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.done", data: `{}`}, stream)
	stream.Close(DoneReasonStop)
	message, ok := stream.Result()
	if !ok || message.Text() != "keep" {
		t.Fatalf("text mismatch: %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesEventCoalescesTextDeltas(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"hel"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"lo"}`}, stream)
	stream.Close(DoneReasonStop)
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Text != "hello" {
		t.Fatalf("text blocks mismatch: %#v ok=%v", message.Content, ok)
	}
}

func TestHandleOpenAIResponsesTextDeltaStartsNewBlockAfterThinkingLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"hello"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.delta", data: `{"delta":"plan"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"world"}`}, stream)
	stream.Close(DoneReasonStop)

	message, ok := stream.Result()
	if !ok || len(message.Content) != 3 || message.Content[0].Text != "hello" || message.Content[1].Thinking != "plan" || message.Content[2].Text != "world" {
		t.Fatalf("content sequence mismatch: %#v ok=%v", message.Content, ok)
	}
}

func TestHandleOpenAIResponsesTextDeltaStartsBlockLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"hi"}`}, stream)
	events := stream.Events()
	if len(events) != 2 || events[0].Type != EventTextStart || events[1].Type != EventTextDelta || events[0].ContentIndex != 0 || events[1].ContentIndex != 0 {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesReasoningDeltaStartsBlockLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.delta", data: `{"delta":"plan"}`}, stream)
	events := stream.Events()
	if len(events) != 2 || events[0].Type != EventThinkingStart || events[1].Type != EventThinkingDelta || events[0].ContentIndex != 0 || events[1].ContentIndex != 0 {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesTextDoneEndsBlockLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"draft"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.done", data: `{"text":"final"}`}, stream)
	events := stream.Events()
	if len(events) != 3 || events[2].Type != EventTextEnd || events[2].ContentIndex != 0 || events[2].Content != "final" {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesTextEventsCarryPartialLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"hi"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.done", data: `{}`}, stream)
	events := stream.Events()
	if len(events) != 3 || events[0].Partial == nil || events[1].Partial == nil || events[2].Partial == nil || events[1].Partial.Content[0].Text != "hi" || events[2].Partial.Content[0].Text != "hi" {
		t.Fatalf("partial mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesThinkingDoneEndsBlockLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.delta", data: `{"delta":"draft"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.done", data: `{"text":"final"}`}, stream)
	events := stream.Events()
	if len(events) != 3 || events[2].Type != EventThinkingEnd || events[2].ContentIndex != 0 || events[2].Content != "final" {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesThinkingEventsCarryPartialLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.delta", data: `{"delta":"hi"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.reasoning_summary_text.done", data: `{}`}, stream)
	events := stream.Events()
	if len(events) != 3 || events[0].Partial == nil || events[1].Partial == nil || events[2].Partial == nil || events[1].Partial.Content[0].Thinking != "hi" || events[2].Partial.Content[0].Thinking != "hi" {
		t.Fatalf("partial mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesReasoningOutputItemStartsBlockLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"reasoning"}}`}, stream)
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventThinkingStart || events[0].ContentIndex != 0 {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesFunctionCallOutputItemStartsToolCallLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventToolCallStart || events[0].ContentIndex != 0 || events[0].Partial == nil || len(events[0].Partial.Content) != 1 || events[0].Partial.Content[0].ToolCall == nil || events[0].Partial.Content[0].ToolCall.ID != "call-1" {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesFunctionCallArgumentsDoneEndsToolCallLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.done", data: `{"arguments":"{\"path\":\"README.md\"}"}`}, stream)
	events := stream.Events()
	if len(events) != 2 || events[1].Type != EventToolCallEnd || events[1].ContentIndex != 0 || events[1].ToolCall == nil || events[1].ToolCall.Arguments["path"] != "README.md" {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesFunctionCallArgumentsDoneKeepsExistingArgsWhenParsedValueIsNotObjectLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.done", data: `{"arguments":"{\"path\":\"README.md\"}"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.done", data: `{"arguments":"[]"}`}, stream)

	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventToolCallEnd || last.ToolCall == nil || last.ToolCall.Arguments["path"] != "README.md" {
		t.Fatalf("tool args should be preserved when parsed value is not an object like upstream: %#v", events)
	}
}

func TestHandleOpenAIResponsesFunctionCallArgumentsDoneCarriesPartialLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"hello"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.done", data: `{"arguments":"{\"path\":\"README.md\"}"}`}, stream)
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventToolCallEnd || last.Partial == nil || len(last.Partial.Content) != 2 || last.Partial.Content[0].Text != "hello" || last.Partial.Content[1].ToolCall == nil || last.Partial.Content[1].ToolCall.Arguments["path"] != "README.md" {
		t.Fatalf("tool call end partial mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesFunctionCallArgumentsDeltaUsesPartialLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"function_call","call_id":"call-1","name":"read"}}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.function_call_arguments.delta", data: `{"delta":"{\"path\":"}`}, stream)
	events := stream.Events()
	if len(events) != 2 || events[1].Type != EventToolCallDelta || events[1].ContentIndex != 0 || events[1].Delta != `{"path":` || events[1].ToolCall != nil || events[1].Partial == nil {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestConsumeOpenAIResponsesSSEStartsWithStartEventLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeResponsesSSE(strings.NewReader("event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"), stream, Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses}); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 2 || events[0].Type != EventStart || events[1].Type != EventDone {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestConsumeOpenAIResponsesSSEPartialsInheritModelMetadataLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeResponsesSSE(strings.NewReader("event: response.output_text.delta\ndata: {\"delta\":\"ok\"}\n\n"), stream, Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses}); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 3 || events[1].Partial == nil || events[2].Partial == nil || events[2].Partial.Model != "gpt-test" || events[2].Partial.Provider != Provider("openai") || events[2].Partial.API != ApiOpenAIResponses {
		t.Fatalf("partial metadata mismatch: %#v", events)
	}
}

func TestConsumeOpenAIResponsesSSEPartialsKeepPriorContentLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	input := strings.Join([]string{
		"event: response.output_text.delta\ndata: {\"delta\":\"hello\"}\n\n",
		"event: response.reasoning_summary_text.delta\ndata: {\"delta\":\"plan\"}\n\n",
	}, "")
	if err := ConsumeResponsesSSE(strings.NewReader(input), stream, Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses}); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	thinkingDelta := events[4]
	if thinkingDelta.Type != EventThinkingDelta || thinkingDelta.Partial == nil || len(thinkingDelta.Partial.Content) != 2 || thinkingDelta.Partial.Content[0].Text != "hello" || thinkingDelta.Partial.Content[1].Thinking != "plan" {
		t.Fatalf("partial content mismatch: %#v", events)
	}
}

func TestConsumeOpenAIResponsesSSEEOFDoneCarriesMessageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeResponsesSSE(strings.NewReader("event: response.output_text.delta\ndata: {\"delta\":\"ok\"}\n\n"), stream, Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses}); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 4 || events[3].Type != EventDone || events[3].Message == nil || events[3].Message.Text() != "ok" || events[3].Message.StopReason != StopReasonEndTurn {
		t.Fatalf("done event mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesCompletedDoneCarriesMessageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"ok"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.completed", data: `{"response":{"status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2}}}`}, stream)
	events := stream.Events()
	if len(events) != 3 || events[2].Type != EventDone || events[2].Message == nil || events[2].Message.Text() != "ok" || events[2].Message.Usage == nil || events[2].Message.Usage.InputTokens != 1 || events[2].Message.StopReason != StopReasonEndTurn {
		t.Fatalf("done event mismatch: %#v", events)
	}
}

func TestHandleOpenAIResponsesEventAddsReasoningOutputItem(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_item.added", data: `{"item":{"type":"reasoning"}}`}, stream)
	stream.Close(DoneReasonStop)
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Type != ContentThinking {
		t.Fatalf("reasoning item mismatch: %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesCompletedUsesPartialTextNotTextEndContentLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"draft"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.done", data: `{"text":"final"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.completed", data: `{"response":{"status":"completed","output":[]}}`}, stream)
	message, ok := stream.Result()
	if !ok || message.Text() != "draft" {
		t.Fatalf("completed message should use upstream partial text, got %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesEventErrorPreservesPartialTextLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"partial"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.failed", data: `{"response":{"error":{"message":"bad"}}}`}, stream)
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "bad" || message.Text() != "partial" {
		t.Fatalf("error partial mismatch: %#v ok=%v", message, ok)
	}
}

func TestHandleOpenAIResponsesErrorEventCarriesMessageAndIsTerminalLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	HandleOpenAIResponsesEvent(sseEvent{event: "response.output_text.delta", data: `{"delta":"partial"}`}, stream)
	HandleOpenAIResponsesEvent(sseEvent{event: "response.failed", data: `{"response":{"error":{"message":"bad"}}}`}, stream)
	events := stream.Events()
	if len(events) != 3 || events[2].Type != EventError || events[2].ErrorReason != ErrorReasonError || events[2].Error != "" || events[2].Message == nil || events[2].Message.StopReason != StopReasonError || events[2].Message.ErrorMessage != "bad" || events[2].Message.Text() != "partial" {
		t.Fatalf("error event mismatch: %#v", events)
	}
}

func TestConsumeOpenAIResponsesSSEReaderErrorMatchesUpstreamShape(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeResponsesSSE(&errorAfterLineReader{line: "event: response.output_text.delta\ndata: {\"delta\":\"partial\"}\n\n"}, stream, Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses})
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventError || last.Error != "" || last.Message == nil || last.Message.Model != "gpt-test" || last.Message.Provider != Provider("openai") || last.Message.API != ApiOpenAIResponses || last.Message.StopReason != StopReasonError || last.Message.Text() != "" || !strings.Contains(last.Message.ErrorMessage, "sse: read failed") {
		t.Fatalf("reader error should be upstream-style error event: %#v", events)
	}
}

func TestOpenAIResponsesProviderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "m", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k"})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed error")
	}
	if message.StopReason != StopReasonError || message.ErrorMessage == "" {
		t.Fatalf("expected error message, got %#v", message)
	}
	if message.ErrorMessage != "HTTP 400 Bad Request: bad" {
		t.Fatalf("HTTP error mismatch: %q", message.ErrorMessage)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "m" || events[0].Message.Provider != Provider("openai") || events[0].Message.API != ApiOpenAIResponses || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "HTTP 400 Bad Request: bad" {
		t.Fatalf("HTTP error should carry provider-aware upstream message: %#v", events)
	}
}

func TestOpenAIResponsesProviderHTTPErrorBodyIsNotTruncatedLikeUpstream(t *testing.T) {
	body := strings.Repeat("x", 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "m", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k"}).Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected HTTP error, got %#v ok=%v", message, ok)
	}
	if !strings.HasSuffix(message.ErrorMessage, body) {
		t.Fatalf("OpenAI Responses HTTP error body should not be truncated like upstream, got length %d", len(message.ErrorMessage))
	}
}

func TestOpenAIResponsesProviderSendErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewOpenAIResponsesProvider(WithHTTPClient(&http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}))
	model := Model{ID: "m", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: "https://example.invalid"}
	maxRetries := 0
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k", MaxRetries: &maxRetries})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "http error: Post \"https://example.invalid/v1/responses\": dial failed" {
		t.Fatalf("send error mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAIResponsesProviderDefaultClientUsesUserAgentLikeUpstream(t *testing.T) {
	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider()
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if userAgent != UserAgent() {
		t.Fatalf("default client user-agent = %q want %q", userAgent, UserAgent())
	}
}

func TestOpenAIResponsesProviderDefaultClientErrorMatchesUpstream(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "://bad-proxy")
	provider := NewOpenAIResponsesProvider()
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: "https://example.invalid"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http client: ") {
		t.Fatalf("client error mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAIResponsesProviderNewRequestErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewOpenAIResponsesProvider()
	model := Model{ID: "m", Provider: Provider("openai"), API: ApiOpenAIResponses, BaseURL: "://bad-url"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http error: ") {
		t.Fatalf("request build error mismatch: %#v ok=%v", message, ok)
	}
}
