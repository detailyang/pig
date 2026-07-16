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
)

func TestBuildCodexResponsesURL(t *testing.T) {
	cases := map[string]string{
		"":                            "https://chatgpt.com/backend-api/codex/responses",
		"   ":                         "https://chatgpt.com/backend-api/codex/responses",
		"https://x/backend-api":       "https://x/backend-api/codex/responses",
		" https://x/backend-api ":     " https://x/backend-api /codex/responses",
		"https://x/backend-api/codex": "https://x/backend-api/codex/responses",
		"https://x/codex/responses/":  "https://x/codex/responses",
	}
	for input, want := range cases {
		if got := BuildCodexResponsesURL(input); got != want {
			t.Fatalf("%q => %q, want %q", input, got, want)
		}
	}
}

func TestBuildCodexResponsesRequestBodyIgnoresSystemRoleInstructionsLikeUpstream(t *testing.T) {
	body, err := BuildCodexResponsesRequestBody(Model{ID: "gpt-5-codex"}, Context{Messages: []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "be a coder"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}},
	}, Tools: []Tool{{Name: "read", Parameters: map[string]any{"type": "object"}}}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if body["instructions"] != "You are a helpful assistant." || body["store"] != false || body["tool_choice"] != "auto" || body["parallel_tool_calls"] != true {
		t.Fatalf("body mismatch: %#v", body)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 1 || input[0]["role"] != "user" {
		t.Fatalf("input mismatch: %#v", input)
	}
	include := body["include"].([]string)
	if len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include mismatch: %#v", include)
	}
	if body["tools"] == nil {
		t.Fatalf("expected tools: %#v", body)
	}
}

func TestBuildCodexResponsesRequestBodyUsesContextSystemPrompt(t *testing.T) {
	body, err := BuildCodexResponsesRequestBody(Model{ID: "gpt-5-codex"}, Context{
		SystemPrompt: "be a coder",
		Messages:     []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if body["instructions"] != "be a coder" {
		t.Fatalf("instructions mismatch: %#v", body)
	}
	input := body["input"].([]map[string]any)
	if len(input) != 1 || input[0]["role"] != "user" {
		t.Fatalf("input mismatch: %#v", input)
	}
}

func TestBuildCodexResponsesRequestBodyPreservesExplicitEmptyInstructionsLikeUpstream(t *testing.T) {
	body, err := BuildCodexResponsesRequestBody(Model{ID: "gpt-5-codex"}, Context{HasSystemPrompt: true}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if body["instructions"] != "" {
		t.Fatalf("explicit empty system prompt should become empty instructions like upstream Some(empty): %#v", body)
	}
}

func TestCodexResponsesInfersToolStopReasonWhenCompletedOutputIsOmitted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"event: response.output_item.added\ndata: {\"item\":{\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"read\"}}\n\n",
			"event: response.function_call_arguments.done\ndata: {\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}\n\n",
			"event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewCodexResponsesProvider(WithCodexResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-token"}).Result()
	if !ok || message.StopReason != StopReasonToolCalls || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("message = %#v ok=%v", message, ok)
	}
}

func TestCodexResponsesProviderRequestAndSSE(t *testing.T) {
	t.Setenv("CODEX_ACCOUNT_ID", "acct-1")
	var body map[string]any
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/codex/responses" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer codex-token" || r.Header.Get("originator") != "pi" || r.Header.Get("OpenAI-Beta") != "responses=experimental" || r.Header.Get("chatgpt-account-id") != "acct-1" {
			t.Fatalf("headers mismatch auth=%q originator=%q beta=%q acct=%q", r.Header.Get("Authorization"), r.Header.Get("originator"), r.Header.Get("OpenAI-Beta"), r.Header.Get("chatgpt-account-id"))
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
			"event: response.output_text.delta\ndata: {\"delta\":\"co\"}\n\n",
			"event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"input_tokens_details\":{\"cached_tokens\":4,\"cache_write_tokens\":5}}}}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewCodexResponsesProvider(WithCodexResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: server.URL + "/backend-api"}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "<tag>&value"}}}}}, StreamOptions{APIKey: "codex-token"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "co" || message.Usage == nil || message.Usage.InputTokens != 2 || message.Usage.OutputTokens != 3 || message.Usage.CacheReadTokens != 4 || message.Usage.CacheWriteTokens != 5 || message.Usage.TotalTokenCount != 14 || !message.Usage.HasTotalTokens || message.Usage.TotalTokens() != 14 {
		t.Fatalf("message mismatch: %#v", message)
	}
	if body["instructions"] != "You are a helpful assistant." || body["tool_choice"] != "auto" {
		t.Fatalf("body mismatch: %#v", body)
	}
	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("request body should not HTML-escape JSON strings like upstream serde_json: %s", rawBody)
	}
	if !strings.Contains(rawBody, `"text":"<tag>&value"`) {
		t.Fatalf("request body missing unescaped content: %s", rawBody)
	}
}

func TestCodexResponsesProviderUsesCodexAuthTokenBeforeProviderEnvLikeUpstream(t *testing.T) {
	t.Setenv("CODEX_AUTH_TOKEN", "codex-auth-token")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer codex-auth-token" {
			t.Fatalf("authorization mismatch: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	provider := NewCodexResponsesProvider(WithCodexResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: server.URL + "/backend-api"}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
}

func TestCodexResponsesProviderMissingAuthMessageLikeUpstream(t *testing.T) {
	t.Setenv("CODEX_AUTH_TOKEN", "")
	t.Setenv("OPENAI_API_KEY", "")
	provider := NewCodexResponsesProvider()
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected auth error, got %#v ok=%v", message, ok)
	}
	if message.ErrorMessage != "Codex auth missing: set CODEX_AUTH_TOKEN or pass options.api_key" {
		t.Fatalf("auth error mismatch: %q", message.ErrorMessage)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gpt-5-codex" || events[0].Message.Provider != Provider("openai-codex") || events[0].Message.API != ApiOpenAICodexResponses || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "Codex auth missing: set CODEX_AUTH_TOKEN or pass options.api_key" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("auth error should carry provider-aware upstream message: %#v", events)
	}
}

func TestCodexResponsesProviderHTTPErrorMessageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad codex"))
	}))
	defer server.Close()

	provider := NewCodexResponsesProvider(WithCodexResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: server.URL + "/backend-api"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "codex-token"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected HTTP error, got %#v ok=%v", message, ok)
	}
	if message.ErrorMessage != "Codex API error (400 Bad Request): bad codex" {
		t.Fatalf("error mismatch: %q", message.ErrorMessage)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gpt-5-codex" || events[0].Message.Provider != Provider("openai-codex") || events[0].Message.API != ApiOpenAICodexResponses || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "Codex API error (400 Bad Request): bad codex" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("HTTP error should carry provider-aware upstream message: %#v", events)
	}
}

func TestCodexResponsesProviderHTTPErrorBodyIsNotTruncatedLikeUpstream(t *testing.T) {
	body := strings.Repeat("x", 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	provider := NewCodexResponsesProvider(WithCodexResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: server.URL + "/backend-api"}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "codex-token"}).Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected HTTP error, got %#v ok=%v", message, ok)
	}
	if !strings.HasSuffix(message.ErrorMessage, body) {
		t.Fatalf("Codex HTTP error body should not be truncated like upstream, got length %d", len(message.ErrorMessage))
	}
}

func TestCodexResponsesProviderSendErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewCodexResponsesProvider(WithCodexResponsesHTTPClient(&http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}))
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: "https://chatgpt.invalid/backend-api"}
	maxRetries := 0
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "codex-token", MaxRetries: &maxRetries})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "http error: Post \"https://chatgpt.invalid/backend-api/codex/responses\": dial failed" {
		t.Fatalf("send error mismatch: %#v ok=%v", message, ok)
	}
}

func TestCodexResponsesProviderNewRequestErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewCodexResponsesProvider()
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: "://bad-url"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "codex-token"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http error: ") {
		t.Fatalf("request build error mismatch: %#v ok=%v", message, ok)
	}
}

func TestCodexResponsesProviderPassesSessionID(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("session_id") != "sess-1" {
			t.Fatalf("session_id mismatch: %q", r.Header.Get("session_id"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	provider := NewCodexResponsesProvider(WithCodexResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: server.URL + "/backend-api"}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "codex-token", SessionID: "sess-1"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["prompt_cache_key"] != "sess-1" {
		t.Fatalf("prompt_cache_key mismatch: %#v", body["prompt_cache_key"])
	}
}

func TestCodexResponsesProviderUsesProviderExtras(t *testing.T) {
	t.Setenv("CODEX_ACCOUNT_ID", "env-acct")

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("chatgpt-account-id") != "extra-acct" {
			t.Fatalf("chatgpt-account-id mismatch: %q", r.Header.Get("chatgpt-account-id"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	provider := NewCodexResponsesProvider(WithCodexResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: server.URL + "/backend-api"}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{
		APIKey: "codex-token",
		ProviderExtras: map[string]any{
			"chatgpt_account_id": "extra-acct",
			"reasoning_effort":   "high",
		},
	}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning mismatch: %#v", reasoning)
	}
}

func TestResolveCodexAccountIDPreservesEnvWhitespaceLikeUpstream(t *testing.T) {
	t.Setenv("CODEX_ACCOUNT_ID", " acct-1 ")
	if got := resolveCodexAccountID(nil); got != " acct-1 " {
		t.Fatalf("account id mismatch: %q", got)
	}
}

func TestCodexResponsesProviderReasoningExtrasOverrideThinkingLevel(t *testing.T) {
	body, err := BuildCodexResponsesRequestBody(Model{ID: "gpt-5-codex"}, Context{}, StreamOptions{
		ProviderExtras: map[string]any{
			"reasoning_effort": "high",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" {
		t.Fatalf("reasoning effort mismatch: %#v", reasoning)
	}
}

func TestCodexResponsesProviderDirectBodyIgnoresThinkingLevelLikeUpstream(t *testing.T) {
	body, err := BuildCodexResponsesRequestBody(Model{ID: "gpt-5-codex"}, Context{}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["reasoning"]; ok {
		t.Fatalf("direct request body should only use provider_extras reasoning_effort like upstream: %#v", body)
	}
}

func TestCodexResponsesProviderStreamSimpleMapsThinkingLevelLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	provider := NewCodexResponsesProvider(WithCodexResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-5-codex", Provider: Provider("openai-codex"), API: ApiOpenAICodexResponses, BaseURL: server.URL + "/backend-api"}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "codex-token"}, ThinkingLevel: ThinkingXHigh}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	reasoning := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning mismatch: %#v", reasoning)
	}
}

func TestCodexResponsesProviderRegisteredBuiltin(t *testing.T) {
	ClearAPIProviders()
	t.Cleanup(ClearAPIProviders)
	RegisterBuiltinProviders()
	if _, ok := GetAPIProvider(ApiOpenAICodexResponses); !ok {
		t.Fatal("codex responses provider was not registered")
	}
}

func TestOpenAICodexResponsesProviderAliasMatchesUpstreamName(t *testing.T) {
	provider := OpenAICodexResponsesProvider{}
	if provider.API() != ApiOpenAICodexResponses {
		t.Fatalf("unexpected API: %s", provider.API())
	}
}
