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

func TestBuildGoogleGenerativeURL(t *testing.T) {
	got := BuildGoogleGenerativeURL("https://generativelanguage.googleapis.com/", "gemini-test")
	want := "https://generativelanguage.googleapis.com/v1beta/models/gemini-test:streamGenerateContent?alt=sse"
	if got != want {
		t.Fatalf("url mismatch: %s", got)
	}
}

func TestGoogleProviderRequestAndSSE(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-test:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Fatalf("url mismatch: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("x-goog-api-key") != "google-key" {
			t.Fatalf("api key mismatch: %s", r.Header.Get("x-goog-api-key"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n\n",
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":2}}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}}, Tools: []Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}}}, StreamOptions{APIKey: "google-key", MaxTokens: 32}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "hello" || message.StopReason != StopReasonEndTurn {
		t.Fatalf("message mismatch: %#v", message)
	}
	if message.Usage == nil || message.Usage.InputTokens != 5 || message.Usage.OutputTokens != 2 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
	contents := body["contents"].([]any)
	if contents[0].(map[string]any)["role"] != "user" {
		t.Fatalf("contents mismatch: %#v", contents)
	}
	if body["tools"] == nil {
		t.Fatalf("expected tools in body: %#v", body)
	}
	generationConfig := body["generationConfig"].(map[string]any)
	if generationConfig["maxOutputTokens"] != float64(32) {
		t.Fatalf("generation config mismatch: %#v", generationConfig)
	}
}

func TestGoogleProviderRequestBodyDoesNotHTMLEscapeLikeUpstreamSerde(t *testing.T) {
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		rawBody = string(data)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	request := Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "<tag>&value"}}}}}
	provider.Stream(context.Background(), model, request, StreamOptions{APIKey: "google-key"}).Result()

	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("request body should not HTML-escape JSON strings like upstream serde_json: %s", rawBody)
	}
	if !strings.Contains(rawBody, `"text":"<tag>&value"`) {
		t.Fatalf("request body missing unescaped content: %s", rawBody)
	}
}

func TestGoogleProviderMissingAPIKeyErrorCarriesModelLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	provider := NewGoogleProvider()
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{})
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gemini-test" || events[0].Message.Provider != Provider("google") || events[0].Message.API != ApiGoogleGenerativeAI || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "GOOGLE_API_KEY / GEMINI_API_KEY is not set" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("missing key should carry provider-aware upstream message: %#v", events)
	}
}

func TestGoogleProviderHTTPErrorMessageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad google", http.StatusBadRequest)
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "HTTP 400 Bad Request: bad google" {
		t.Fatalf("http error mismatch: %#v ok=%v", message, ok)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gemini-test" || events[0].Message.Provider != Provider("google") || events[0].Message.API != ApiGoogleGenerativeAI || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "HTTP 400 Bad Request: bad google" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("http error should carry provider-aware upstream message: %#v", events)
	}
}

func TestGoogleProviderHTTPErrorBodyIsNotTruncatedLikeUpstream(t *testing.T) {
	body := strings.Repeat("x", 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"}).Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected HTTP error, got %#v ok=%v", message, ok)
	}
	if !strings.HasSuffix(message.ErrorMessage, body) {
		t.Fatalf("Google HTTP error body should not be truncated like upstream, got length %d", len(message.ErrorMessage))
	}
}

func TestGoogleProviderSendErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewGoogleProvider(WithGoogleHTTPClient(&http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: "https://google.invalid"}
	maxRetries := 0
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key", MaxRetries: &maxRetries})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "http error: Post \"https://google.invalid/v1beta/models/gemini-test:streamGenerateContent?alt=sse\": dial failed" {
		t.Fatalf("send error mismatch: %#v ok=%v", message, ok)
	}
}

func TestGoogleProviderNewRequestErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewGoogleProvider()
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: "://bad-url"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http error: ") {
		t.Fatalf("request build error mismatch: %#v ok=%v", message, ok)
	}
}

func TestGoogleProviderUsesOnlyFirstCandidateLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"first\"}]},\"finishReason\":\"STOP\"},{\"content\":{\"parts\":[{\"text\":\"second\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "first" {
		t.Fatalf("Google SSE should only consume first candidate like upstream: %#v", message)
	}
}

func TestGoogleProviderPreservesFirstResponseIDLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"responseId\":\"resp-1\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}]}\n\n",
			"data: {\"responseId\":\"resp-2\",\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.ResponseID != "resp-1" {
		t.Fatalf("response id mismatch: %#v", message)
	}
}

func TestGoogleProviderTextLifecycleEventsMatchUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"responseId\":\"resp-1\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n\n",
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}]}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	events := stream.Events()
	if len(events) < 5 || events[0].Type != EventStart || events[1].Type != EventTextStart || events[2].Type != EventTextDelta || events[3].Type != EventTextDelta || events[len(events)-2].Type != EventTextEnd || events[len(events)-1].Type != EventDone {
		t.Fatalf("Google text event lifecycle mismatch: %#v", events)
	}
	if events[1].ContentIndex != 0 || events[2].ContentIndex != 0 || events[len(events)-2].Content != "hello" {
		t.Fatalf("Google text content index/end mismatch: %#v", events)
	}
	if events[1].Partial == nil || events[1].Partial.ResponseID != "resp-1" || events[2].Partial == nil || events[2].Partial.ResponseID != "resp-1" {
		t.Fatalf("Google text partial should carry response id like upstream: %#v %#v", events[1].Partial, events[2].Partial)
	}
	if events[len(events)-1].Message == nil || events[len(events)-1].Message.Text() != "hello" || events[len(events)-1].Message.ResponseID != "resp-1" {
		t.Fatalf("Google done event should carry final message like upstream: %#v", events[len(events)-1])
	}
}

func TestGoogleProviderThinkingLifecycleEventsMatchUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pla\",\"thought\":true,\"thoughtSignature\":\"sig-1\"}]}}]}\n\n",
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"n\",\"thought\":true},{\"text\":\"answer\"}]},\"finishReason\":\"STOP\"}]}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	events := stream.Events()
	if len(events) < 8 || events[0].Type != EventStart || events[1].Type != EventThinkingStart || events[2].Type != EventThinkingDelta || events[3].Type != EventThinkingDelta || events[4].Type != EventThinkingEnd || events[5].Type != EventTextStart || events[6].Type != EventTextDelta || events[len(events)-2].Type != EventTextEnd || events[len(events)-1].Type != EventDone {
		t.Fatalf("Google thinking event lifecycle mismatch: %#v", events)
	}
	if events[1].ContentIndex != 0 || events[2].ContentIndex != 0 || events[4].Content != "plan" || events[5].ContentIndex != 1 || events[6].ContentIndex != 1 {
		t.Fatalf("Google thinking/text content index mismatch: %#v", events)
	}
	if events[2].ContentBlock == nil || events[2].ContentBlock.ThinkingSignature != "sig-1" || events[3].Partial == nil || events[3].Partial.Content[0].ThinkingSignature != "sig-1" {
		t.Fatalf("Google thinking signature should be retained like upstream: %#v", events)
	}
	if events[len(events)-1].Message == nil || len(events[len(events)-1].Message.Content) != 2 || events[len(events)-1].Message.Content[0].Thinking != "plan" || events[len(events)-1].Message.Content[1].Text != "answer" {
		t.Fatalf("Google done event should carry thinking and text content: %#v", events[len(events)-1])
	}
}

func TestGoogleProviderPreservesEmptyTextPartLifecycleLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	events := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"}).Events()
	if len(events) != 5 || events[0].Type != EventStart || events[1].Type != EventTextStart || events[2].Type != EventTextDelta || events[3].Type != EventTextEnd || events[4].Type != EventDone {
		t.Fatalf("empty Google text should still emit lifecycle events like upstream: %#v", events)
	}
	if events[2].Delta != "" || events[3].Content != "" || events[4].Message == nil || len(events[4].Message.Content) != 1 || events[4].Message.Content[0].Text != "" {
		t.Fatalf("empty Google text content mismatch: %#v", events)
	}
}

func TestGoogleProviderPreservesEmptyThinkingPartLifecycleLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"\",\"thought\":true,\"thoughtSignature\":\"sig-1\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	events := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"}).Events()
	if len(events) != 5 || events[0].Type != EventStart || events[1].Type != EventThinkingStart || events[2].Type != EventThinkingDelta || events[3].Type != EventThinkingEnd || events[4].Type != EventDone {
		t.Fatalf("empty Google thinking should still emit lifecycle events like upstream: %#v", events)
	}
	if events[2].Delta != "" || events[2].ContentBlock == nil || events[2].ContentBlock.ThinkingSignature != "sig-1" || events[4].Message == nil || len(events[4].Message.Content) != 1 || events[4].Message.Content[0].ThinkingSignature != "sig-1" {
		t.Fatalf("empty Google thinking content mismatch: %#v", events)
	}
}

func TestGoogleProviderTextDeltaPartialDoesNotIncludeSameChunkUsageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4}}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	events := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"}).Events()
	if len(events) < 3 || events[2].Type != EventTextDelta {
		t.Fatalf("expected text delta event: %#v", events)
	}
	if events[2].Partial == nil || events[2].Partial.Usage == nil || events[2].Partial.Usage.InputTokens != 0 || events[2].Partial.Usage.OutputTokens != 0 {
		t.Fatalf("text delta partial should not include usage from the same chunk like upstream: %#v", events[2].Partial)
	}
	if events[len(events)-1].Message == nil || events[len(events)-1].Message.Usage == nil || events[len(events)-1].Message.Usage.InputTokens != 3 || events[len(events)-1].Message.Usage.OutputTokens != 4 {
		t.Fatalf("done message should still include usage: %#v", events[len(events)-1])
	}
}

func TestBuildGoogleRequestBodyDoesNotUseThinkingLevelDirectlyLikeUpstream(t *testing.T) {
	body, err := BuildGoogleRequestBody(Context{}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["generationConfig"]; ok {
		t.Fatalf("direct request body should only use provider_extras thinking_budget like upstream: %#v", body)
	}
}

func TestBuildGoogleRequestBodyUsesThinkingBudgetExtra(t *testing.T) {
	body, err := BuildGoogleRequestBody(Context{}, StreamOptions{ProviderExtras: map[string]any{"thinking_budget": 4096}})
	if err != nil {
		t.Fatal(err)
	}
	generationConfig := body["generationConfig"].(map[string]any)
	thinkingConfig := generationConfig["thinkingConfig"].(map[string]any)
	if thinkingConfig["thinkingBudget"] != 4096 || thinkingConfig["includeThoughts"] != true {
		t.Fatalf("thinking config mismatch: %#v", thinkingConfig)
	}
	translated := TranslateSimple(SimpleStreamOptions{Reasoning: ThinkingHigh, ThinkingBudgets: ThinkingBudgets{High: 4096}})
	if translated.ProviderExtras["thinking_budget"] != 4096 {
		t.Fatalf("translate simple mismatch: %#v", translated.ProviderExtras)
	}
}

func TestBuildGoogleRequestBodyUsesToolChoiceExtra(t *testing.T) {
	body, err := BuildGoogleRequestBody(Context{Tools: []Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}}}, StreamOptions{ProviderExtras: map[string]any{"tool_choice": "required"}})
	if err != nil {
		t.Fatal(err)
	}
	toolConfig := body["toolConfig"].(map[string]any)
	functionCallingConfig := toolConfig["functionCallingConfig"].(map[string]any)
	if functionCallingConfig["mode"] != "ANY" {
		t.Fatalf("tool choice config mismatch: %#v", toolConfig)
	}
}

func TestBuildGoogleRequestBodyUsesAllowedFunctionNamesToolChoice(t *testing.T) {
	body, err := BuildGoogleRequestBody(Context{Tools: []Tool{
		{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}},
		{Name: "write", Description: "write files", Parameters: map[string]any{"type": "object"}},
	}}, StreamOptions{ProviderExtras: map[string]any{"tool_choice": []string{"read"}}})
	if err != nil {
		t.Fatal(err)
	}
	toolConfig := body["toolConfig"].(map[string]any)
	functionCallingConfig := toolConfig["functionCallingConfig"].(map[string]any)
	allowed := functionCallingConfig["allowedFunctionNames"].([]string)
	if functionCallingConfig["mode"] != "ANY" || len(allowed) != 1 || allowed[0] != "read" {
		t.Fatalf("allowed function tool choice mismatch: %#v", functionCallingConfig)
	}
}

func TestConvertMessagesForGooglePreservesEmptyAssistantTextLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentText}}}})
	if len(messages) != 1 {
		t.Fatalf("expected assistant message with empty text part, got %#v", messages)
	}
	parts := messages[0]["parts"].([]map[string]any)
	if len(parts) != 1 || parts[0]["text"] != "" {
		t.Fatalf("empty assistant text part mismatch: %#v", parts)
	}
}

func TestGoogleProviderStreamSimplePassesProviderExtras(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "google-key", ProviderExtras: map[string]any{"thinking_budget": 4096}}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	generationConfig := body["generationConfig"].(map[string]any)
	thinkingConfig := generationConfig["thinkingConfig"].(map[string]any)
	if thinkingConfig["thinkingBudget"] != float64(4096) {
		t.Fatalf("thinking config mismatch: %#v", thinkingConfig)
	}
}

func TestGoogleProviderStreamSimpleUsesThinkingBudgets(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "google-key"}, ThinkingLevel: ThinkingLow, ThinkingBudgets: ThinkingBudgets{Low: 2048}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	generationConfig := body["generationConfig"].(map[string]any)
	thinkingConfig := generationConfig["thinkingConfig"].(map[string]any)
	if thinkingConfig["thinkingBudget"] != float64(2048) {
		t.Fatalf("thinking config mismatch: %#v", thinkingConfig)
	}
}

func TestGoogleProviderStreamSimplePreservesExplicitZeroThinkingBudgetLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	var budgets ThinkingBudgets
	if err := json.Unmarshal([]byte(`{"low":0}`), &budgets); err != nil {
		t.Fatal(err)
	}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "google-key"}, ThinkingLevel: ThinkingLow, ThinkingBudgets: budgets}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	generationConfig := body["generationConfig"].(map[string]any)
	thinkingConfig := generationConfig["thinkingConfig"].(map[string]any)
	if thinkingConfig["thinkingBudget"] != float64(0) {
		t.Fatalf("explicit zero thinking budget should override default like upstream Some(0): %#v", thinkingConfig)
	}
}

func TestGoogleProviderStreamSimpleOverridesBaseThinkingBudgetLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{

		ThinkingLevel:   ThinkingHigh,
		ThinkingBudgets: ThinkingBudgets{High: 2048},
		Base:            StreamOptions{ProviderExtras: map[string]any{"thinking_budget": 4096}, APIKey: "google-key"},
	}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	generationConfig := body["generationConfig"].(map[string]any)
	thinkingConfig := generationConfig["thinkingConfig"].(map[string]any)
	if thinkingConfig["thinkingBudget"] != float64(2048) {
		t.Fatalf("simple thinking should override base provider extra like upstream: %#v", thinkingConfig)
	}
}

func TestBuildGoogleRequestBodyIgnoresSystemRoleMessagesLikeUpstream(t *testing.T) {
	body, err := BuildGoogleRequestBody(Context{Messages: []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "be brief"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}},
	}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["systemInstruction"]; ok {
		t.Fatalf("system role message should be ignored like upstream Message enum: %#v", body["systemInstruction"])
	}
	contents := body["contents"].([]map[string]any)
	if len(contents) != 1 || contents[0]["role"] != "user" {
		t.Fatalf("contents mismatch: %#v", contents)
	}
}

func TestBuildGoogleRequestBodyUsesContextSystemPrompt(t *testing.T) {
	body, err := BuildGoogleRequestBody(Context{
		SystemPrompt: "be brief",
		Messages:     []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	systemInstruction := body["systemInstruction"].(map[string]any)
	systemParts := systemInstruction["parts"].([]map[string]any)
	if len(systemParts) != 1 || systemParts[0]["text"] != "be brief" {
		t.Fatalf("system instruction mismatch: %#v", systemInstruction)
	}
	contents := body["contents"].([]map[string]any)
	if len(contents) != 1 || contents[0]["role"] != "user" {
		t.Fatalf("contents mismatch: %#v", contents)
	}
}

func TestBuildGoogleRequestBodyPreservesExplicitEmptySystemPromptLikeUpstream(t *testing.T) {
	body, err := BuildGoogleRequestBody(Context{
		HasSystemPrompt: true,
		Messages:        []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	systemInstruction, ok := body["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatalf("explicit empty system prompt should be preserved like upstream Some(empty): %#v", body)
	}
	systemParts := systemInstruction["parts"].([]map[string]any)
	if len(systemParts) != 1 || systemParts[0]["text"] != "" {
		t.Fatalf("system instruction mismatch: %#v", systemInstruction)
	}
}

func TestBuildGoogleRequestBodyToolsBecomeFunctionDeclarationsLikeUpstream(t *testing.T) {
	body, err := BuildGoogleRequestBody(Context{Tools: []Tool{{Name: "lookup", Description: "look", Parameters: map[string]any{"type": "object"}}}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tools := body["tools"].([]map[string]any)
	declarations := tools[0]["functionDeclarations"].([]map[string]any)
	if len(declarations) != 1 || declarations[0]["name"] != "lookup" || declarations[0]["description"] != "look" {
		t.Fatalf("function declarations mismatch: %#v", body["tools"])
	}
	parameters := declarations[0]["parameters"].(map[string]any)
	if parameters["type"] != "object" {
		t.Fatalf("parameters mismatch: %#v", parameters)
	}
}

func TestConvertMessagesForGooglePreservesUserImageParts(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{Role: RoleUser, Content: []ContentBlock{
		{Type: ContentText, Text: "look"},
		{Type: ContentImage, MimeType: "image/png", Data: "abc"},
	}}})
	parts := messages[0]["parts"].([]map[string]any)
	inlineData := parts[1]["inlineData"].(map[string]any)
	if parts[0]["text"] != "look" || inlineData["mimeType"] != "image/png" || inlineData["data"] != "abc" {
		t.Fatalf("parts mismatch: %#v", parts)
	}
}

func TestConvertMessagesForGoogleSkipsUserWithEmptyPartsLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{Role: RoleUser}})
	if len(messages) != 0 {
		t.Fatalf("empty user parts should be skipped like upstream: %#v", messages)
	}
}

func TestConvertMessagesForGoogleGroupsToolResults(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{
		{Role: RoleTool, Name: "legacy", ToolName: "read", Content: []ContentBlock{{Type: ContentText, Text: "one"}}},
		{Role: RoleTool, ToolName: "write", Content: []ContentBlock{{Type: ContentText, Text: "two"}}},
	})
	if len(messages) != 1 || messages[0]["role"] != "user" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
	parts := messages[0]["parts"].([]map[string]any)
	first := parts[0]["functionResponse"].(map[string]any)
	second := parts[1]["functionResponse"].(map[string]any)
	if first["name"] != "read" || first["response"].(map[string]any)["result"] != "one" || second["name"] != "write" || second["response"].(map[string]any)["result"] != "two" {
		t.Fatalf("parts mismatch: %#v", parts)
	}
}

func TestConvertMessagesForGoogleUsesToolNameOnlyLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{Role: RoleTool, Name: "legacy", Content: []ContentBlock{{Type: ContentText, Text: "one"}}}})
	parts := messages[0]["parts"].([]map[string]any)
	functionResponse := parts[0]["functionResponse"].(map[string]any)
	if functionResponse["name"] != "" {
		t.Fatalf("Google tool result should use only toolName like upstream, got %#v", functionResponse)
	}
}

func TestConvertMessagesForGoogleDoesNotGroupToolResultAfterUserText(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{
		{Role: RoleTool, Name: "read", Content: []ContentBlock{{Type: ContentText, Text: "one"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "next"}}},
		{Role: RoleTool, Name: "write", Content: []ContentBlock{{Type: ContentText, Text: "two"}}},
	})
	if len(messages) != 3 {
		t.Fatalf("messages mismatch: %#v", messages)
	}
}

func TestBuildGoogleRequestBodyDoesNotEnableThinkingWhenOff(t *testing.T) {
	temperature := 0.7
	body, err := BuildGoogleRequestBody(Context{}, StreamOptions{Temperature: &temperature})
	if err != nil {
		t.Fatal(err)
	}
	generationConfig := body["generationConfig"].(map[string]any)
	if _, ok := generationConfig["thinkingConfig"]; ok {
		t.Fatalf("thinking config should be omitted: %#v", generationConfig)
	}
	if generationConfig["temperature"] != temperature {
		t.Fatalf("temperature mismatch: %#v", generationConfig)
	}
}

func TestConvertMessagesForGooglePreservesThinkingSignature(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{
			Type:              ContentThinking,
			Thinking:          "plan",
			ThinkingSignature: "sig-1",
		}},
	}})
	if len(messages) != 1 || messages[0]["role"] != "model" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
	parts := messages[0]["parts"].([]map[string]any)
	if len(parts) != 1 || parts[0]["text"] != "plan" || parts[0]["thought"] != true || parts[0]["thoughtSignature"] != "sig-1" {
		t.Fatalf("parts mismatch: %#v", parts)
	}
}

func TestConvertMessagesForGooglePreservesTextSignature(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{
			Type:          ContentText,
			Text:          "answer",
			TextSignature: "sig-text",
		}},
	}})
	if len(messages) != 1 || messages[0]["role"] != "model" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
	parts := messages[0]["parts"].([]map[string]any)
	if len(parts) != 1 || parts[0]["text"] != "answer" || parts[0]["thoughtSignature"] != "sig-text" {
		t.Fatalf("parts mismatch: %#v", parts)
	}
}

func TestConvertMessagesForGooglePreservesToolCallSignature(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{
			ID:               "call-1",
			Name:             "read",
			Arguments:        map[string]any{"path": "README.md"},
			ThoughtSignature: "sig-1",
		}}},
	}})
	if len(messages) != 1 || messages[0]["role"] != "model" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
	parts := messages[0]["parts"].([]map[string]any)
	functionCall := parts[0]["functionCall"].(map[string]any)
	if _, ok := functionCall["id"]; ok {
		t.Fatalf("functionCall id should be omitted like upstream: %#v", functionCall)
	}
	if functionCall["name"] != "read" || functionCall["args"].(map[string]any)["path"] != "README.md" || parts[0]["thoughtSignature"] != "sig-1" {
		t.Fatalf("parts mismatch: %#v", parts)
	}
}

func TestConvertMessagesForGooglePreservesToolCallContentBlock(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{
			ID:               "call-1",
			Name:             "read",
			Arguments:        map[string]any{"path": "README.md"},
			ThoughtSignature: "sig-1",
		}}},
	}})
	if len(messages) != 1 || messages[0]["role"] != "model" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
	parts := messages[0]["parts"].([]map[string]any)
	functionCall := parts[0]["functionCall"].(map[string]any)
	if _, ok := functionCall["id"]; ok {
		t.Fatalf("functionCall id should be omitted like upstream: %#v", functionCall)
	}
	if functionCall["name"] != "read" || functionCall["args"].(map[string]any)["path"] != "README.md" || parts[0]["thoughtSignature"] != "sig-1" {
		t.Fatalf("parts mismatch: %#v", parts)
	}
}

func TestConvertMessagesForGoogleDeduplicatesToolCallBlocks(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{
		Role:      RoleAssistant,
		Content:   []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}},
		ToolCalls: []ToolCall{{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
	}})
	parts := messages[0]["parts"].([]map[string]any)
	if len(parts) != 1 {
		t.Fatalf("tool calls should be deduplicated: %#v", parts)
	}
}

func TestConvertMessagesForGoogleIgnoresLegacyAssistantToolCallsLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForGoogle([]Message{{
		Role:      RoleAssistant,
		ToolCalls: []ToolCall{{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
	}})
	if len(messages) != 0 {
		t.Fatalf("legacy assistant tool calls should be ignored like upstream: %#v", messages)
	}
}

func TestGoogleProviderFunctionCallStopsForToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"call-1\",\"name\":\"read\",\"args\":{\"path\":\"README.md\"}},\"thoughtSignature\":\"sig-1\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "read"}}}}}, StreamOptions{APIKey: "google-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.StopReason != StopReasonToolCalls {
		t.Fatalf("stop reason mismatch: %s", message.StopReason)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call-1" || message.ToolCalls[0].Name != "read" || message.ToolCalls[0].Arguments["path"] != "README.md" || message.ToolCalls[0].ThoughtSignature != "sig-1" {
		t.Fatalf("tool calls mismatch: %#v", message.ToolCalls)
	}
}

func TestGoogleProviderFunctionCallPreservesJSONNumberArgs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"call-1\",\"name\":\"read\",\"args\":{\"line\":1}}}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "read"}}}}}, StreamOptions{APIKey: "google-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["line"] != json.Number("1") {
		t.Fatalf("tool call args should preserve JSON number like upstream serde_json::Value: %#v", message.ToolCalls)
	}
}

func TestGoogleProviderFunctionCallMissingArgsDefaultsToEmptyObjectLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"call-1\",\"name\":\"read\"}}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"}).Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments == nil || len(message.ToolCalls[0].Arguments) != 0 {
		t.Fatalf("missing functionCall args should default to empty object like upstream: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestGoogleProviderFunctionCallWithoutFinishReasonStopsLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"call-1\",\"name\":\"read\",\"args\":{}}}]}}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.StopReason != StopReasonEndTurn {
		t.Fatalf("functionCall without finishReason should keep upstream default stop, got %#v", message)
	}
	last := stream.Events()[len(stream.Events())-1]
	if last.Type != EventDone || last.DoneReason != DoneReasonStop {
		t.Fatalf("done reason mismatch: %#v", last)
	}
}

func TestGoogleProviderFunctionCallEventsMatchUpstreamLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"call-1\",\"name\":\"read\",\"args\":{\"path\":\"README.md\"}}}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	events := stream.Events()
	if len(events) < 5 || events[0].Type != EventStart || events[1].Type != EventToolCallStart || events[2].Type != EventToolCallDelta || events[3].Type != EventToolCallEnd || events[4].Type != EventDone {
		t.Fatalf("Google functionCall lifecycle mismatch: %#v", events)
	}
	if events[1].ContentIndex != 0 || events[2].ContentIndex != 0 || events[3].ContentIndex != 0 || events[2].Delta != "{\"path\":\"README.md\"}" {
		t.Fatalf("Google functionCall event content mismatch: %#v", events)
	}
	if events[1].Partial == nil || events[1].Partial.Content[0].ToolCall == nil || events[3].ToolCall == nil || events[3].ToolCall.Arguments["path"] != "README.md" || events[4].Message == nil || len(events[4].Message.ToolCalls) != 1 {
		t.Fatalf("Google functionCall partial/done mismatch: %#v", events)
	}
	for _, event := range events {
		if event.Type == EventToolCall {
			t.Fatalf("Google functionCall should use upstream start/delta/end events, got legacy event: %#v", events)
		}
	}
}

func TestGoogleProviderClosesTextBeforeFunctionCallInSamePartLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"before\",\"functionCall\":{\"id\":\"call-1\",\"name\":\"read\",\"args\":{\"path\":\"README.md\"}}}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	events := stream.Events()
	if len(events) < 7 || events[1].Type != EventTextStart || events[2].Type != EventTextDelta || events[3].Type != EventTextEnd || events[4].Type != EventToolCallStart || events[5].Type != EventToolCallDelta || events[6].Type != EventToolCallEnd {
		t.Fatalf("Google should close text before function call in the same part like upstream: %#v", events)
	}
	if events[3].Content != "before" || events[4].ContentIndex != 1 {
		t.Fatalf("Google text/tool indexes mismatch: %#v", events)
	}
}

func TestGoogleProviderGeneratesMissingFunctionCallID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"read\",\"args\":{} }},{\"functionCall\":{\"name\":\"read\",\"args\":{} }}]}}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"}).Result()
	if !ok || len(message.ToolCalls) != 2 {
		t.Fatalf("tool calls mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
	if message.ToolCalls[0].ID == "" || message.ToolCalls[1].ID == "" || message.ToolCalls[0].ID == message.ToolCalls[1].ID {
		t.Fatalf("generated ids mismatch: %#v", message.ToolCalls)
	}
	if !strings.HasPrefix(message.ToolCalls[0].ID, "read_") || !strings.HasPrefix(message.ToolCalls[1].ID, "read_") {
		t.Fatalf("generated ids should include tool name: %#v", message.ToolCalls)
	}
}

func TestGoogleProviderGeneratesUniqueFunctionCallIDsAcrossChunksLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"read\",\"args\":{} }}]}}]}\n\n",
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"read\",\"args\":{} }}]}}]}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"}).Result()
	if !ok || len(message.ToolCalls) != 2 {
		t.Fatalf("tool calls mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
	if message.ToolCalls[0].ID == message.ToolCalls[1].ID || !strings.HasSuffix(message.ToolCalls[0].ID, "_1") || !strings.HasSuffix(message.ToolCalls[1].ID, "_2") {
		t.Fatalf("generated ids should keep a stream-level counter like upstream: %#v", message.ToolCalls)
	}
}

func TestGoogleProviderMapsMaxTokensFinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]},\"finishReason\":\"MAX_TOKENS\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"}).Result()
	if !ok || message.StopReason != StopReasonMaxTokens {
		t.Fatalf("max tokens mismatch: %#v ok=%v", message, ok)
	}
}

func TestGoogleProviderMapsSafetyFinishReasonToError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"finishReason\":\"SAFETY\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "google-key"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage == "" {
		t.Fatalf("safety mismatch: %#v ok=%v", message, ok)
	}
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventError || last.Error != "" || last.Message == nil || last.Message.Model != "gemini-test" || last.Message.Provider != Provider("google") || last.Message.API != ApiGoogleGenerativeAI || last.Message.StopReason != StopReasonError || last.Message.ErrorMessage != "google error" {
		t.Fatalf("safety error should carry upstream-style message: %#v", last)
	}
}

func TestConsumeGoogleSSEReaderErrorMatchesUpstreamShape(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeGoogleSSEForModel(&errorAfterLineReader{line: "data: {\"candidates\":[] }\n"}, stream, Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI})
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventError || last.Error != "" || last.Message == nil || last.Message.Model != "gemini-test" || last.Message.Provider != Provider("google") || last.Message.API != ApiGoogleGenerativeAI || last.Message.StopReason != StopReasonError || !strings.Contains(last.Message.ErrorMessage, "sse: read failed") {
		t.Fatalf("reader error should be upstream-style error event: %#v", events)
	}
}

func TestGoogleProviderThinkingPartsAndThoughtTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"plan\",\"thought\":true,\"thoughtSignature\":\"sig-1\"},{\"text\":\"answer\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4,\"thoughtsTokenCount\":2,\"cachedContentTokenCount\":1}}\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewGoogleProvider(WithGoogleHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "think"}}}}}, StreamOptions{APIKey: "google-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if len(message.Content) != 2 || message.Content[0].Type != ContentThinking || message.Content[0].Thinking != "plan" || message.Content[0].ThinkingSignature != "sig-1" || message.Content[1].Type != ContentText || message.Content[1].Text != "answer" {
		t.Fatalf("content mismatch: %#v", message.Content)
	}
	if message.Usage == nil || message.Usage.InputTokens != 2 || message.Usage.CacheReadTokens != 1 || message.Usage.OutputTokens != 6 || message.Usage.TotalTokenCount != 9 || !message.Usage.HasTotalTokens || message.Usage.TotalTokens() != 9 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
}

func TestConsumeGoogleSSEDoesNotEmitStandaloneMetadataOrUsageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeGeminiSSE(strings.NewReader("data: {\"responseId\":\"google-resp-1\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4}}\n\n"), stream, Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleGenerativeAI})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range stream.Events() {
		if event.Type == EventMetadata || event.Type == EventUsage {
			t.Fatalf("Google stream should not emit standalone metadata/usage events like upstream: %#v", stream.Events())
		}
	}
	message, ok := stream.Result()
	if !ok || message.ResponseID != "google-resp-1" || message.Usage == nil || message.Usage.InputTokens != 3 || message.Usage.OutputTokens != 4 {
		t.Fatalf("done message should retain response id and usage: %#v ok=%v", message, ok)
	}
}

func TestConsumeGoogleSSESaturatesCachedPromptUsage(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeGoogleSSE(strings.NewReader("data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"cachedContentTokenCount\":3}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil || message.Usage.InputTokens != 0 || message.Usage.CacheReadTokens != 3 {
		t.Fatalf("usage mismatch: %#v ok=%v", message.Usage, ok)
	}
}

func TestConsumeGoogleSSEUsesTotalTokenCountLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeGoogleSSE(strings.NewReader("data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"cachedContentTokenCount\":1,\"candidatesTokenCount\":4,\"thoughtsTokenCount\":2,\"totalTokenCount\":42}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil || message.Usage.TotalTokens() != 42 {
		t.Fatalf("Google usage should preserve totalTokenCount like upstream: %#v ok=%v", message.Usage, ok)
	}
}

func TestConsumeGoogleSSEPreservesZeroTotalTokenCountLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeGoogleSSE(strings.NewReader("data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4,\"totalTokenCount\":0}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil || message.Usage.TotalTokens() != 0 {
		t.Fatalf("Google usage should preserve explicit zero totalTokenCount like upstream: %#v ok=%v", message.Usage, ok)
	}
}

func TestConsumeGoogleSSEIgnoresNonU64UsageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeGoogleSSE(strings.NewReader("data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":-1,\"cachedContentTokenCount\":1.5,\"candidatesTokenCount\":2,\"thoughtsTokenCount\":3.5,\"totalTokenCount\":4.5}}\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil {
		t.Fatalf("expected completed message with usage: %#v ok=%v", message, ok)
	}
	if message.Usage.InputTokens != 0 || message.Usage.OutputTokens != 2 || message.Usage.CacheReadTokens != 0 || message.Usage.TotalTokens() != 2 {
		t.Fatalf("non-u64 usage fields should be ignored like upstream as_u64: %#v", message.Usage)
	}
}

func TestGoogleProviderRegisteredBuiltin(t *testing.T) {
	ClearAPIProviders()
	t.Cleanup(ClearAPIProviders)
	RegisterBuiltinProviders()
	if _, ok := GetAPIProvider(ApiGoogleGenerativeAI); !ok {
		t.Fatal("google provider was not registered")
	}
}
