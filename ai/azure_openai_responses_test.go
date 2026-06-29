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

func TestBuildAzureOpenAIResponsesURL(t *testing.T) {
	got := BuildAzureOpenAIResponsesURL("https://example.openai.azure.com/", "v1")
	want := "https://example.openai.azure.com/openai/v1/responses?api-version=v1"
	if got != want {
		t.Fatalf("url mismatch: %s", got)
	}
}

func TestAzureOpenAIResponsesProviderRequestAndSSE(t *testing.T) {
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "gpt-test=deploy-test")

	var body map[string]any
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/responses" || r.URL.Query().Get("api-version") != "v1" {
			t.Fatalf("url mismatch: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("api-key") != "azure-key" {
			t.Fatalf("api-key mismatch: %s", r.Header.Get("api-key"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
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
			"event: response.output_text.delta\ndata: {\"delta\":\"az\"}\n\n",
			"event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"input_tokens_details\":{\"cached_tokens\":3,\"cache_write_tokens\":4}}}}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewAzureOpenAIResponsesProvider(WithAzureOpenAIResponsesHTTPClient(server.Client()))
	mapped := "high"
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses, BaseURL: server.URL, Reasoning: true, ThinkingLevels: map[string]*string{string(ThinkingMedium): &mapped}}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "<tag>&value"}}}}}, StreamOptions{APIKey: "azure-key", ProviderExtras: map[string]any{"reasoning_effort": "medium"}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "az" || message.StopReason != StopReasonEndTurn {
		t.Fatalf("message mismatch: %#v", message)
	}
	if message.Usage == nil || message.Usage.InputTokens != 1 || message.Usage.OutputTokens != 2 || message.Usage.CacheReadTokens != 3 || message.Usage.CacheWriteTokens != 4 || message.Usage.TotalTokenCount != 10 || !message.Usage.HasTotalTokens || message.Usage.TotalTokens() != 10 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
	if body["model"] != "deploy-test" || body["stream"] != true || body["store"] != false {
		t.Fatalf("body mismatch: %#v", body)
	}
	if body["reasoning"].(map[string]any)["effort"] != "high" {
		t.Fatalf("reasoning mismatch: %#v", body)
	}
	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("request body should not HTML-escape JSON strings like upstream serde_json: %s", rawBody)
	}
	if !strings.Contains(rawBody, `"text":"<tag>&value"`) {
		t.Fatalf("request body missing unescaped content: %s", rawBody)
	}
}

func TestAzureOpenAIResponsesProviderMissingAPIKeyErrorCarriesModelLikeUpstream(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	provider := NewAzureOpenAIResponsesProvider()
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses, BaseURL: "https://example.openai.azure.com"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{})
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gpt-test" || events[0].Message.Provider != Provider("azure") || events[0].Message.API != ApiAzureOpenAIResponses || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "AZURE_OPENAI_API_KEY is not set" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("missing key should carry provider-aware upstream message: %#v", events)
	}
}

func TestAzureOpenAIResponsesProviderMissingBaseURLErrorCarriesModelLikeUpstream(t *testing.T) {
	provider := NewAzureOpenAIResponsesProvider()
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "azure-key"})
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gpt-test" || events[0].Message.Provider != Provider("azure") || events[0].Message.API != ApiAzureOpenAIResponses || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "Azure OpenAI base URL is not set" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("missing base URL should carry provider-aware upstream message: %#v", events)
	}
}

func TestAzureOpenAIResponsesProviderStreamSimpleOverridesReasoningEffortLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	provider := NewAzureOpenAIResponsesProvider(WithAzureOpenAIResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses, BaseURL: server.URL, Reasoning: true}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "azure-key",

		ProviderExtras: map[string]any{"reasoning_effort": "high"}}, ThinkingLevel: ThinkingLow,
	}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["reasoning"].(map[string]any)["effort"] != "low" {
		t.Fatalf("reasoning effort should be overwritten by simple reasoning like upstream: %#v", body)
	}
}

func TestAzureOpenAIResponsesProviderDeploymentNameFromProviderExtras(t *testing.T) {
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "gpt-test=env-deploy")

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	provider := NewAzureOpenAIResponsesProvider(WithAzureOpenAIResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}}}, StreamOptions{
		APIKey: "azure-key",
		ProviderExtras: map[string]any{
			"azure_deployment_name": "extra-deploy",
		},
	}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["model"] != "extra-deploy" {
		t.Fatalf("deployment mismatch: %#v", body["model"])
	}
}

func TestAzureOpenAIResponsesDeploymentNameDefaultsToModelIDLikeUpstream(t *testing.T) {
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "")

	if got := ResolveAzureOpenAIDeploymentName("gpt-5", nil); got != "gpt-5" {
		t.Fatalf("deployment default mismatch: %q", got)
	}
}

func TestAzureOpenAIResponsesDeploymentNameFromEnvMapLikeUpstream(t *testing.T) {
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "other=ignored, gpt-5 = deploy-5 , malformed")

	if got := ResolveAzureOpenAIDeploymentName("gpt-5", nil); got != "deploy-5" {
		t.Fatalf("deployment env map mismatch: %q", got)
	}
}

func TestAzureOpenAIResponsesProviderAPIVersionFromProviderExtras(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api-version") != "2025-04-01-preview" {
			t.Fatalf("api version mismatch: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\"}}\n\n"))
	}))
	defer server.Close()

	provider := NewAzureOpenAIResponsesProvider(WithAzureOpenAIResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{
		APIKey:         "azure-key",
		ProviderExtras: map[string]any{"azure_api_version": "2025-04-01-preview"},
	}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
}

func TestAzureOpenAIResponsesProviderHTTPErrorMessageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad azure", http.StatusBadRequest)
	}))
	defer server.Close()

	provider := NewAzureOpenAIResponsesProvider(WithAzureOpenAIResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "azure-key"})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected error message")
	}
	if message.ErrorMessage != "Azure OpenAI API error (400 Bad Request): bad azure" {
		t.Fatalf("error mismatch: %#v", message)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gpt-test" || events[0].Message.Provider != Provider("azure") || events[0].Message.API != ApiAzureOpenAIResponses || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "Azure OpenAI API error (400 Bad Request): bad azure" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("http error should carry provider-aware upstream message: %#v", events)
	}
}

func TestAzureOpenAIResponsesProviderHTTPErrorBodyIsNotTruncatedLikeUpstream(t *testing.T) {
	body := strings.Repeat("x", 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	provider := NewAzureOpenAIResponsesProvider(WithAzureOpenAIResponsesHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "azure-key"}).Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected HTTP error, got %#v ok=%v", message, ok)
	}
	if !strings.HasSuffix(message.ErrorMessage, body) {
		t.Fatalf("Azure HTTP error body should not be truncated like upstream, got length %d", len(message.ErrorMessage))
	}
}

func TestAzureOpenAIResponsesProviderSendErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewAzureOpenAIResponsesProvider(WithAzureOpenAIResponsesHTTPClient(&http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}))
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses, BaseURL: "https://azure.invalid"}
	maxRetries := 0
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "azure-key", MaxRetries: &maxRetries})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "http error: Post \"https://azure.invalid/openai/v1/responses?api-version=v1\": dial failed" {
		t.Fatalf("send error mismatch: %#v ok=%v", message, ok)
	}
}

func TestAzureOpenAIResponsesProviderNewRequestErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewAzureOpenAIResponsesProvider()
	model := Model{ID: "gpt-test", Provider: Provider("azure"), API: ApiAzureOpenAIResponses, BaseURL: "://bad-url"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "azure-key"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http error: ") {
		t.Fatalf("request build error mismatch: %#v ok=%v", message, ok)
	}
}

func TestAzureOpenAIResponsesProviderRegisteredBuiltin(t *testing.T) {
	ClearAPIProviders()
	t.Cleanup(ClearAPIProviders)
	RegisterBuiltinProviders()
	if _, ok := GetAPIProvider(ApiAzureOpenAIResponses); !ok {
		t.Fatal("azure openai responses provider was not registered")
	}
}
