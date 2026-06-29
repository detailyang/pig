package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuildGoogleVertexURL(t *testing.T) {
	got := BuildGoogleVertexURL("https://us-central1-aiplatform.googleapis.com/", "project-a", "us-central1", "gemini-test")
	want := "https://us-central1-aiplatform.googleapis.com/v1/projects/project-a/locations/us-central1/publishers/google/models/gemini-test:streamGenerateContent?alt=sse"
	if got != want {
		t.Fatalf("url mismatch: %s", got)
	}
}

func TestGoogleVertexHost(t *testing.T) {
	if got := GoogleVertexHost("us-central1"); got != "https://us-central1-aiplatform.googleapis.com" {
		t.Fatalf("regional host mismatch: %s", got)
	}
	if got := GoogleVertexHost("global"); got != "https://aiplatform.googleapis.com" {
		t.Fatalf("global host mismatch: %s", got)
	}
}

func TestResolveGoogleVertexBaseURLReplacesCatalogLocationPlaceholder(t *testing.T) {
	if got := ResolveGoogleVertexBaseURL("https://{location}-aiplatform.googleapis.com", "us-central1"); got != "https://us-central1-aiplatform.googleapis.com" {
		t.Fatalf("regional base url mismatch: %s", got)
	}
	if got := ResolveGoogleVertexBaseURL("https://{location}-aiplatform.googleapis.com", "global"); got != "https://aiplatform.googleapis.com" {
		t.Fatalf("global base url mismatch: %s", got)
	}
}

func TestGoogleVertexProviderRequestAndSSE(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/project-a/locations/us-central1/publishers/google/models/gemini-test:streamGenerateContent" || r.URL.Query().Get("alt") != "sse" {
			t.Fatalf("url mismatch: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer vertex-token" {
			t.Fatalf("authorization mismatch: %s", r.Header.Get("Authorization"))
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

	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}}}, StreamOptions{APIKey: "vertex-token"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "hello" || message.StopReason != StopReasonEndTurn {
		t.Fatalf("message mismatch: %#v", message)
	}
	if message.Usage == nil || message.Usage.InputTokens != 5 || message.Usage.OutputTokens != 2 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
	if message.API != ApiGoogleVertex || message.Provider != Provider("google") || message.Model != "gemini-test" {
		t.Fatalf("message metadata mismatch: %#v", message)
	}
	contents := body["contents"].([]any)
	if contents[0].(map[string]any)["role"] != "user" {
		t.Fatalf("contents mismatch: %#v", contents)
	}
}

func TestGoogleVertexProviderRequestBodyDoesNotHTMLEscapeLikeUpstreamSerde(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")

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

	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: server.URL}
	request := Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "<tag>&value"}}}}}
	provider.Stream(context.Background(), model, request, StreamOptions{APIKey: "vertex-token"}).Result()

	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("request body should not HTML-escape JSON strings like upstream serde_json: %s", rawBody)
	}
	if !strings.Contains(rawBody, `"text":"<tag>&value"`) {
		t.Fatalf("request body missing unescaped content: %s", rawBody)
	}
}

func TestGoogleVertexProviderResolvesCatalogLocationBaseURL(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/us-central1/v1/projects/project-a/locations/us-central1/publishers/google/models/gemini-test:streamGenerateContent" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google-vertex"), API: ApiGoogleVertex, BaseURL: server.URL + "/{location}"}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "vertex-token"}).Result()
	if !ok || message.Text() != "ok" {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
}

func TestGoogleVertexProviderSSEEventsUseVertexModelMetadataLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "vertex-token"})
	events := stream.Events()
	if len(events) < 2 || events[0].Type != EventStart || events[0].Partial == nil || events[0].Partial.API != ApiGoogleVertex || events[0].Partial.Provider != Provider("google") || events[0].Partial.Model != "gemini-test" {
		t.Fatalf("Vertex start event metadata mismatch: %#v", events)
	}
	last := events[len(events)-1]
	if last.Type != EventDone || last.Message == nil || last.Message.API != ApiGoogleVertex || last.Message.Provider != Provider("google") || last.Message.Model != "gemini-test" || last.Message.Text() != "hi" {
		t.Fatalf("Vertex done event message metadata mismatch: %#v", last)
	}
}

func TestGoogleVertexProviderStreamSimpleUsesThinkingBudgetsLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: server.URL}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "vertex-token"}, ThinkingLevel: ThinkingLow, ThinkingBudgets: ThinkingBudgets{Low: 2048}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	thinkingConfig := body["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
	if thinkingConfig["thinkingBudget"] != float64(2048) || thinkingConfig["includeThoughts"] != true {
		t.Fatalf("thinking config mismatch: %#v", thinkingConfig)
	}
}

func TestGoogleVertexProviderDoesNotEmitStandaloneMetadataOrUsageLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"responseId\":\"vertex-resp-1\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":4}}\n\n"))
	}))
	defer server.Close()

	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "vertex-token"})
	for _, event := range stream.Events() {
		if event.Type == EventMetadata || event.Type == EventUsage {
			t.Fatalf("Vertex stream should not emit standalone metadata/usage events like upstream: %#v", stream.Events())
		}
	}
	message, ok := stream.Result()
	if !ok || message.ResponseID != "vertex-resp-1" || message.Usage == nil || message.Usage.InputTokens != 3 || message.Usage.OutputTokens != 4 || message.API != ApiGoogleVertex {
		t.Fatalf("done message should retain response id, usage, and Vertex metadata: %#v ok=%v", message, ok)
	}
}

func TestGoogleVertexProviderHTTPErrorMessageLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad vertex", http.StatusBadRequest)
	}))
	defer server.Close()

	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "vertex-token"})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected error message")
	}
	if message.ErrorMessage != "Vertex API error (400 Bad Request): bad vertex" {
		t.Fatalf("error mismatch: %#v", message)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gemini-test" || events[0].Message.Provider != Provider("google") || events[0].Message.API != ApiGoogleVertex || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "Vertex API error (400 Bad Request): bad vertex" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("http error should carry provider-aware upstream message: %#v", events)
	}
}

func TestGoogleVertexProviderHTTPErrorBodyIsNotTruncatedLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")
	body := strings.Repeat("x", 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(server.Client()))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "vertex-token"}).Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected HTTP error, got %#v ok=%v", message, ok)
	}
	if !strings.HasSuffix(message.ErrorMessage, body) {
		t.Fatalf("Vertex HTTP error body should not be truncated like upstream, got length %d", len(message.ErrorMessage))
	}
}

func TestGoogleVertexProviderSendErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")
	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(&http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: "https://vertex.invalid"}
	maxRetries := 0
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "vertex-token", MaxRetries: &maxRetries})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "http error: Post \"https://vertex.invalid/v1/projects/project-a/locations/us-central1/publishers/google/models/gemini-test:streamGenerateContent?alt=sse\": dial failed" {
		t.Fatalf("send error mismatch: %#v ok=%v", message, ok)
	}
}

func TestGoogleVertexProviderNewRequestErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "project-a")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")
	provider := NewGoogleVertexProvider()
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: "://bad-url"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "vertex-token"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http error: ") {
		t.Fatalf("request build error mismatch: %#v ok=%v", message, ok)
	}
}

func TestGoogleVertexProviderMissingProjectErrorCarriesModelLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")
	provider := NewGoogleVertexProvider()
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "vertex-token"})
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gemini-test" || events[0].Message.Provider != Provider("google") || events[0].Message.API != ApiGoogleVertex || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "GOOGLE_VERTEX_PROJECT is not set" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("missing project should carry provider-aware upstream message: %#v", events)
	}
}

func TestGoogleVertexProviderUsesServiceAccountADCWhenAccessTokenMissing(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_ACCESS_TOKEN", "")
	t.Setenv("GOOGLE_VERTEX_PROJECT", "")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "us-central1")
	var tokenRequestSeen bool
	var vertexRequestSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenRequestSeen = true
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "adc-token", "expires_in": 3600})
			return
		}
		if r.URL.Path != "/v1/projects/project-from-adc/locations/us-central1/publishers/google/models/gemini-test:streamGenerateContent" {
			t.Fatalf("unexpected provider request: %s", r.URL.Path)
		}
		vertexRequestSeen = true
		if r.Header.Get("Authorization") != "Bearer adc-token" {
			t.Fatalf("authorization mismatch: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"adc ok\"}]},\"finishReason\":\"STOP\"}]}\n\n"))
	}))
	defer server.Close()

	credentialFile, err := os.CreateTemp(t.TempDir(), "adc-*.json")
	if err != nil {
		t.Fatal(err)
	}
	projectID := "project-from-adc"
	credential := GoogleVertexServiceAccount{ClientEmail: "svc@proj.iam.gserviceaccount.com", PrivateKey: testGoogleVertexPrivateKey(t), TokenURI: server.URL + "/token", ProjectID: &projectID}
	if err := json.NewEncoder(credentialFile).Encode(credential); err != nil {
		t.Fatal(err)
	}
	_ = credentialFile.Close()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", credentialFile.Name())

	provider := NewGoogleVertexProvider(WithGoogleVertexHTTPClient(server.Client()), WithGoogleVertexADCOptions(GoogleVertexADCOptions{HTTPClient: server.Client(), Now: func() time.Time { return time.Unix(1700000000, 0) }}))
	model := Model{ID: "gemini-test", Provider: Provider("google"), API: ApiGoogleVertex, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if !tokenRequestSeen || !vertexRequestSeen || message.Text() != "adc ok" {
		t.Fatalf("ADC flow mismatch token=%v vertex=%v message=%#v", tokenRequestSeen, vertexRequestSeen, message)
	}
}

func TestGoogleVertexProviderRegisteredBuiltin(t *testing.T) {
	ClearAPIProviders()
	t.Cleanup(ClearAPIProviders)
	RegisterBuiltinProviders()
	if _, ok := GetAPIProvider(ApiGoogleVertex); !ok {
		t.Fatal("google vertex provider was not registered")
	}
}
