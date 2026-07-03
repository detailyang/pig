package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type errorAfterLineReader struct {
	line string
	done bool
}

func (reader *errorAfterLineReader) Read(buffer []byte) (int, error) {
	if !reader.done {
		reader.done = true
		return copy(buffer, reader.line), nil
	}
	return 0, errors.New("read failed")
}

func TestBuildOpenAIChatURL(t *testing.T) {
	cases := map[string]string{
		"https://api.openai.com":               "https://api.openai.com/v1/chat/completions",
		"https://api.openai.com/v1":            "https://api.openai.com/v1/chat/completions",
		"https://api.openai.com/v1/":           "https://api.openai.com/v1/chat/completions",
		"https://x.com/v2":                     "https://x.com/v2/chat/completions",
		"https://gateway.example.com/openai":   "https://gateway.example.com/openai/v1/chat/completions",
		"https://gateway.example.com/v1/proxy": "https://gateway.example.com/v1/proxy/chat/completions",
	}
	for input, want := range cases {
		if got := BuildOpenAIChatURL(input); got != want {
			t.Fatalf("%s => %s, want %s", input, got, want)
		}
	}
}

func TestOpenAIChatProviderDoesNotExposeStaleThinkingDisableOption(t *testing.T) {
	providerType := reflect.TypeOf(OpenAIChatProvider{})
	if _, ok := providerType.FieldByName("disableThinkingLevelReasoningEffort"); ok {
		t.Fatal("OpenAIChatProvider should not keep a thinking-level disable option after StreamOptions no longer has ThinkingLevel")
	}
}

func TestOpenAIChatProviderRequestAndSSE(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization mismatch: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("accept mismatch: %s", r.Header.Get("Accept"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"model\":\"served-model\",\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n",
			"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n",
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"\"}}]}}]}\n\n",
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"README.md\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":4}}\n\n",
			"data: [DONE]\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAI, BaseURL: server.URL}
	request := Context{
		Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hello"}}}},
		Tools:    []Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}},
	}
	message, ok := provider.Stream(context.Background(), model, request, StreamOptions{APIKey: "test-key", MaxTokens: 128}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "hello" {
		t.Fatalf("text mismatch: %q", message.Text())
	}
	if message.ResponseModel != "served-model" {
		t.Fatalf("response model mismatch: %#v", message)
	}
	if message.StopReason != StopReasonToolCalls {
		t.Fatalf("stop reason mismatch: %s", message.StopReason)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call-1" || message.ToolCalls[0].Name != "read" || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool call mismatch: %#v", message.ToolCalls)
	}
	if len(message.Content) != 2 || message.Content[1].Type != ContentToolCall || message.Content[1].ToolCall == nil || message.Content[1].ToolCall.Arguments["path"] != "README.md" {
		t.Fatalf("tool call content mismatch: %#v", message.Content)
	}
	if message.Usage == nil || message.Usage.InputTokens != 3 || message.Usage.OutputTokens != 4 {
		t.Fatalf("usage mismatch: %#v", message.Usage)
	}
	if body["model"] != "gpt-test" || body["stream"] != true || body["max_tokens"] != float64(128) {
		t.Fatalf("request body mismatch: %#v", body)
	}
	streamOptions := body["stream_options"].(map[string]any)
	if streamOptions["include_usage"] != true {
		t.Fatalf("stream options mismatch: %#v", streamOptions)
	}
	messages := body["messages"].([]any)
	if messages[0].(map[string]any)["role"] != "user" || messages[0].(map[string]any)["content"] != "hello" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
	tools := body["tools"].([]any)
	function := tools[0].(map[string]any)["function"].(map[string]any)
	if tools[0].(map[string]any)["type"] != "function" || function["name"] != "read" {
		t.Fatalf("tools mismatch: %#v", tools)
	}
}

func TestOpenAIChatProviderRequestBodyDoesNotHTMLEscapeLikeUpstreamSerde(t *testing.T) {
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		rawBody = string(data)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAI, BaseURL: server.URL}
	request := Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "<tag>&value"}}}}}
	provider.Stream(context.Background(), model, request, StreamOptions{APIKey: "test-key"}).Result()

	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("request body should not HTML-escape JSON strings like upstream serde_json: %s", rawBody)
	}
	if !strings.Contains(rawBody, `"content":"<tag>&value"`) {
		t.Fatalf("request body missing unescaped content: %s", rawBody)
	}
}

func TestOpenAIChatProviderAbortBeforeResponseEmitsAbortedLikeUpstream(t *testing.T) {
	abort := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAI, BaseURL: server.URL}
	close(abort)

	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", Abort: abort}).Result()
	if !ok || message.StopReason != StopReasonAborted || message.ErrorMessage != "aborted" {
		t.Fatalf("aborted result mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAIChatProviderAbortDuringSSEEmitsAbortedLikeUpstream(t *testing.T) {
	abort := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAI, BaseURL: server.URL}
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(abort)
	}()

	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", Abort: abort}).Result()
	if !ok || message.StopReason != StopReasonAborted || message.ErrorMessage != "aborted" {
		t.Fatalf("aborted result mismatch: %#v ok=%v", message, ok)
	}
}

func TestBuildOpenAIChatRequestBodyAddsReasoningEffort(t *testing.T) {
	body, err := BuildOpenAIChatRequestBody(
		Model{ID: "mistral-test", Reasoning: true},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "medium"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if body["reasoning_effort"] != "medium" {
		t.Fatalf("reasoning effort mismatch: %#v", body)
	}

	body, err = BuildOpenAIChatRequestBody(
		Model{ID: "mistral-test", Reasoning: true},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "minimal"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if body["reasoning_effort"] != "minimal" {
		t.Fatalf("minimal reasoning effort mismatch: %#v", body)
	}

	body, err = BuildOpenAIChatRequestBody(Model{ID: "plain"}, Context{}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("reasoning effort should be omitted: %#v", body)
	}
}

func TestBuildOpenAIChatRequestBodyReasoningEffortExtraOverridesThinkingLevel(t *testing.T) {
	body, err := BuildOpenAIChatRequestBody(
		Model{ID: "mistral-test", Reasoning: true},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "medium"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if body["reasoning_effort"] != "medium" {
		t.Fatalf("reasoning effort mismatch: %#v", body)
	}
}

func TestBuildOpenAIChatRequestBodyOmitsReasoningEffortWhenCompatDisablesIt(t *testing.T) {
	body, err := BuildOpenAIChatRequestBody(
		Model{ID: "kimi-test", Reasoning: true, Compat: map[string]any{"supportsReasoningEffort": false}},
		Context{},
		StreamOptions{ProviderExtras: map[string]any{"reasoning_effort": "high"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("reasoning_effort should be omitted when compat disables it: %#v", body)
	}
}

func TestBuildOpenAIChatRequestBodyUsesCompatMaxTokensField(t *testing.T) {
	body, err := BuildOpenAIChatRequestBody(
		Model{ID: "gpt-test", Compat: map[string]any{"maxTokensField": "max_completion_tokens"}},
		Context{},
		StreamOptions{MaxTokens: 128},
	)
	if err != nil {
		t.Fatal(err)
	}
	if body["max_completion_tokens"] != 128 {
		t.Fatalf("compat max tokens field missing: %#v", body)
	}
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("default max_tokens should be replaced by compat field: %#v", body)
	}
}

func TestBuildOpenAIChatRequestBodyUsesContextSystemPrompt(t *testing.T) {
	body, err := BuildOpenAIChatRequestBody(Model{ID: "gpt-test"}, Context{
		SystemPrompt: "be helpful",
		Messages:     []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]map[string]any)
	if len(messages) != 2 || messages[0]["role"] != "system" || messages[0]["content"] != "be helpful" || messages[1]["role"] != "user" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
}

func TestBuildOpenAIChatRequestBodyPreservesExplicitEmptySystemPromptLikeUpstream(t *testing.T) {
	body, err := BuildOpenAIChatRequestBody(Model{ID: "gpt-test"}, Context{
		HasSystemPrompt: true,
		Messages:        []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]map[string]any)
	if len(messages) != 2 || messages[0]["role"] != "system" || messages[0]["content"] != "" || messages[1]["role"] != "user" {
		t.Fatalf("explicit empty system prompt should be preserved like upstream Some(empty): %#v", messages)
	}
}

func TestConvertMessagesForOpenAIChatUsesImageURLParts(t *testing.T) {
	messages := ConvertMessagesForOpenAIChat([]Message{{Role: RoleUser, Content: []ContentBlock{
		{Type: ContentText, Text: "look"},
		{Type: ContentImage, MimeType: "image/png", Data: "abc"},
	}}})
	content := messages[0]["content"].([]map[string]any)
	if len(content) != 2 || content[0]["type"] != "text" || content[0]["text"] != "look" {
		t.Fatalf("text part mismatch: %#v", content)
	}
	imageURL := content[1]["image_url"].(map[string]any)
	if content[1]["type"] != "image_url" || imageURL["url"] != "data:image/png;base64,abc" {
		t.Fatalf("image part mismatch: %#v", content[1])
	}
}

func TestConvertMessagesForOpenAIChatAssistantToolCallsUseNullContent(t *testing.T) {
	messages := ConvertMessagesForOpenAIChat([]Message{{
		Role:      RoleAssistant,
		ToolCalls: []ToolCall{{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
	}})
	if len(messages) != 1 || messages[0]["role"] != "assistant" {
		t.Fatalf("message mismatch: %#v", messages)
	}
	if _, ok := messages[0]["content"]; !ok || messages[0]["content"] != nil {
		t.Fatalf("assistant content should be explicit nil: %#v", messages[0])
	}
	if messages[0]["tool_calls"] == nil {
		t.Fatalf("tool calls missing: %#v", messages[0])
	}
}

func TestConvertMessagesForOpenAIChatToolCallArgumentsDoNotHTMLEscapeLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForOpenAIChat([]Message{{
		Role:      RoleAssistant,
		ToolCalls: []ToolCall{{ID: "call-1", Name: "write", Arguments: map[string]any{"text": "a < b && c > d"}}},
	}})

	toolCalls := messages[0]["tool_calls"].([]map[string]any)
	function := toolCalls[0]["function"].(map[string]any)
	if function["arguments"] != `{"text":"a < b && c > d"}` {
		t.Fatalf("tool call arguments should match upstream serde_json formatting: %#v", function["arguments"])
	}
}

func TestConvertMessagesForOpenAIChatOmitsToolName(t *testing.T) {
	messages := ConvertMessagesForOpenAIChat([]Message{{
		Role:       RoleTool,
		ToolCallID: "call-1",
		Name:       "legacy",
		ToolName:   "read",
		Content:    []ContentBlock{{Type: ContentText, Text: "ok"}},
	}})
	if _, ok := messages[0]["name"]; ok {
		t.Fatalf("tool name should be omitted: %#v", messages[0])
	}
}

func TestConvertMessagesForOpenAIChatAssistantToolCallBlocks(t *testing.T) {
	messages := ConvertMessagesForOpenAIChat([]Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{
			ID:        "call-1",
			Name:      "read",
			Arguments: map[string]any{"path": "README.md"},
		}}},
	}})
	toolCalls := messages[0]["tool_calls"].([]map[string]any)
	if len(toolCalls) != 1 || toolCalls[0]["id"] != "call-1" {
		t.Fatalf("tool calls mismatch: %#v", messages[0])
	}
}

func TestConvertMessagesForOpenAIChatDeduplicatesToolCallBlocks(t *testing.T) {
	messages := ConvertMessagesForOpenAIChat([]Message{{
		Role:      RoleAssistant,
		Content:   []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}},
		ToolCalls: []ToolCall{{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
	}})
	toolCalls := messages[0]["tool_calls"].([]map[string]any)
	if len(toolCalls) != 1 {
		t.Fatalf("tool calls should be deduplicated: %#v", messages[0])
	}
}

func TestOpenAIChatProviderStreamSimplePassesProviderExtras(t *testing.T) {
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

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "mistral-test", Provider: Provider("openai"), API: ApiOpenAI, BaseURL: server.URL, Reasoning: true}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{Base: StreamOptions{APIKey: "test-key", ProviderExtras: map[string]any{"reasoning_effort": "medium"}}}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["reasoning_effort"] != "medium" {
		t.Fatalf("reasoning effort mismatch: %#v", body)
	}
}

func TestOpenAIChatProviderReturnsLiveStreamBeforeDone(t *testing.T) {
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

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAI, BaseURL: server.URL}
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

func TestOpenAIChatProviderSendsSessionAffinityForCompatProviders(t *testing.T) {
	var affinity string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		affinity = r.Header.Get("x-affinity")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "cf-test", Provider: Provider("cloudflare-workers-ai"), API: ApiOpenAICompletions, BaseURL: server.URL, Compat: map[string]any{"sendSessionAffinityHeaders": true}}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", SessionID: "sess-1"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if affinity != "sess-1" {
		t.Fatalf("x-affinity mismatch: %q", affinity)
	}
}

func TestOpenAIChatProviderOmitsSessionAffinityWithoutCompat(t *testing.T) {
	var affinity string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		affinity = r.Header.Get("x-affinity")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAICompletions, BaseURL: server.URL}
	_, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key", SessionID: "sess-1"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if affinity != "" {
		t.Fatalf("x-affinity should be omitted: %q", affinity)
	}
}

func TestConsumeOpenAIChatSSEMapsLengthFinishReason(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeOpenAIChatSSE(strings.NewReader(strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"length\"}]}\n\n",
		"data: [DONE]\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonMaxTokens {
		t.Fatalf("length mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeOpenAIChatSSEParsesCachedPromptTokens(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeOpenAIChatSSE(strings.NewReader(strings.Join([]string{
		"data: {\"id\":\"chatcmpl_1\",\"model\":\"served-model\",\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"prompt_tokens_details\":{\"cached_tokens\":3}}}\n\n",
		"data: [DONE]\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil || message.Usage.InputTokens != 5 || message.Usage.OutputTokens != 2 || message.Usage.CacheReadTokens != 3 || message.Usage.TotalTokenCount != 10 || !message.Usage.HasTotalTokens || message.Usage.TotalTokens() != 10 {
		t.Fatalf("usage mismatch: %#v ok=%v", message, ok)
	}
	if message.ResponseID != "chatcmpl_1" {
		t.Fatalf("response id mismatch: %#v", message)
	}
	if message.ResponseModel != "served-model" {
		t.Fatalf("response model mismatch: %#v", message)
	}
}

func TestConsumeOpenAIChatSSEIgnoresNonU64UsageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeOpenAIChatSSE(strings.NewReader(strings.Join([]string{
		"data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":-1,\"completion_tokens\":2,\"prompt_tokens_details\":{\"cached_tokens\":1.5}}}\n\n",
		"data: [DONE]\n\n",
	}, "")), stream)
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

func TestConsumeOpenAIChatSSEEmitsToolArgumentDeltas(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	payload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{\"path\":"}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	err = ConsumeOpenAIChatSSE(strings.NewReader("data: "+string(payload)+"\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 3 || events[2].Type != EventToolCallDelta || events[2].Delta != "{\"path\":" || events[2].Partial == nil || events[2].Partial.Content[0].ToolCall.ID != "call-1" {
		t.Fatalf("events mismatch: %#v", events)
	}
}

func TestConsumeOpenAIChatSSEToolCallEventsMatchUpstreamShape(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	payload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{\"path\":"}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ConsumeOpenAIChatSSE(strings.NewReader("data: "+string(payload)+"\n\n"), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 4 || events[0].Type != EventStart || events[1].Type != EventToolCallStart || events[2].Type != EventToolCallDelta || events[1].ContentIndex != 0 || events[2].ContentIndex != 0 || events[1].Partial == nil || events[2].Partial == nil || events[2].ToolCall != nil {
		t.Fatalf("tool call events should match upstream start/delta partial shape: %#v", events)
	}
}

func TestConsumeOpenAIChatSSEToolCallEndBeforeDoneLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	firstPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{\"path\":"}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	secondPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"delta":         map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": "\"README.md\"}"}}}},
		"finish_reason": "tool_calls",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ConsumeOpenAIChatSSE(strings.NewReader("data: "+string(firstPayload)+"\n\n"+"data: "+string(secondPayload)+"\n\n"+"data: [DONE]\n\n"), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 5 || events[len(events)-2].Type != EventToolCallEnd || events[len(events)-2].ToolCall == nil || events[len(events)-2].ToolCall.Arguments["path"] != "README.md" || events[len(events)-1].Type != EventDone {
		t.Fatalf("tool call end should precede done like upstream: %#v", events)
	}
}

func TestConsumeOpenAIChatSSEToolCallDeltaPartialKeepsArgumentsEmptyUntilEndLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	firstPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{\"path\":"}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	secondPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"delta":         map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": "\"README.md\"}"}}}},
		"finish_reason": "tool_calls",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := ConsumeOpenAIChatSSE(strings.NewReader("data: "+string(firstPayload)+"\n\n"+"data: "+string(secondPayload)+"\n\n"+"data: [DONE]\n\n"), stream); err != nil {
		t.Fatal(err)
	}

	events := stream.Events()
	var deltaEvent AssistantMessageEvent
	var endEvent AssistantMessageEvent
	for _, event := range events {
		if event.Type == EventToolCallDelta {
			deltaEvent = event
		}
		if event.Type == EventToolCallEnd {
			endEvent = event
		}
	}
	if deltaEvent.Partial == nil || len(deltaEvent.Partial.Content) == 0 || deltaEvent.Partial.Content[0].ToolCall == nil || len(deltaEvent.Partial.Content[0].ToolCall.Arguments) != 0 {
		t.Fatalf("tool call delta partial should not expose parsed arguments before end: %#v", deltaEvent)
	}
	if endEvent.ToolCall == nil || endEvent.ToolCall.Arguments["path"] != "README.md" {
		t.Fatalf("tool call end should expose parsed arguments: %#v", endEvent)
	}
}

func TestConsumeOpenAIChatSSEKeepsParallelToolArgumentDeltasByIndex(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	firstPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{\"path\":\"a"}},
		map[string]any{"index": 1, "id": "call-2", "type": "function", "function": map[string]any{"name": "write", "arguments": "{\"path\":\"b\"}"}},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	secondPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": 0, "function": map[string]any{"arguments": "\"}"}},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	err = ConsumeOpenAIChatSSE(strings.NewReader("data: "+string(firstPayload)+"\n\n"+"data: "+string(secondPayload)+"\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 2 || message.ToolCalls[0].Arguments["path"] != "a" || message.ToolCalls[1].Arguments["path"] != "b" {
		t.Fatalf("tool calls mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
	events := stream.Events()
	var lastDelta AssistantMessageEvent
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Type == EventToolCallDelta {
			lastDelta = events[index]
			break
		}
	}
	if lastDelta.Type != EventToolCallDelta || lastDelta.Partial == nil || lastDelta.Partial.Content[0].ToolCall.ID != "call-1" || lastDelta.Delta != "\"}" {
		t.Fatalf("last delta mismatch: %#v", lastDelta)
	}
}

func TestConsumeOpenAIChatSSENormalizesInvalidToolCallIndexLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	firstPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": -1, "id": "call-1", "type": "function", "function": map[string]any{"name": "read", "arguments": "{\"path\":"}},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	secondPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": 1.5, "function": map[string]any{"arguments": "\"README.md\"}"}},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	err = ConsumeOpenAIChatSSE(strings.NewReader("data: "+string(firstPayload)+"\n\n"+"data: "+string(secondPayload)+"\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("invalid tool call indexes should default to 0 like upstream as_u64: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestConsumeOpenAIChatSSEAppliesToolArgumentsBeforeIDAndName(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	firstPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": 0, "function": map[string]any{"arguments": "{\"path\":\"README.md\"}"}},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	secondPayload, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
		map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "read"}},
	}}}}})
	if err != nil {
		t.Fatal(err)
	}
	err = ConsumeOpenAIChatSSE(strings.NewReader("data: "+string(firstPayload)+"\n\n"+"data: "+string(secondPayload)+"\n\n"), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool calls mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestOpenAIChatProviderUsesZaiToolStreamCompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"function\":{\"arguments\":\"\\\"README.md\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "glm-5.1", Provider: Provider("zai"), API: ApiOpenAICompletions, BaseURL: server.URL, Compat: map[string]any{"zaiToolStream": true}}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "test-key"}).Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("zai tool stream should merge indexed chunks into one call: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestConsumeOpenAIChatSSEParsesReasoningDelta(t *testing.T) {
	for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text"} {
		t.Run(field, func(t *testing.T) {
			stream := NewAssistantMessageEventStream()
			err := ConsumeOpenAIChatSSE(strings.NewReader(strings.Join([]string{
				"data: {\"choices\":[{\"delta\":{\"" + field + "\":\"plan\"}}]}\n\n",
				"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n",
				"data: [DONE]\n\n",
			}, "")), stream)
			if err != nil {
				t.Fatal(err)
			}
			message, ok := stream.Result()
			if !ok || len(message.Content) != 2 || message.Content[0].Type != ContentThinking || message.Content[0].Thinking != "plan" || message.Content[1].Type != ContentText || message.Content[1].Text != "answer" {
				t.Fatalf("content mismatch: %#v ok=%v", message.Content, ok)
			}
		})
	}
}

func TestConsumeOpenAIChatSSEReasoningDeltaStartsBlockLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeOpenAIChatSSE(strings.NewReader("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"plan\"}}]}\n\n"), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 3 || events[0].Type != EventStart || events[1].Type != EventThinkingStart || events[2].Type != EventThinkingDelta || events[1].ContentIndex != 0 || events[2].ContentIndex != 0 || events[2].Partial == nil {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestConsumeOpenAIChatSSETextDeltaStartsBlockLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeOpenAIChatSSE(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 3 || events[0].Type != EventStart || events[1].Type != EventTextStart || events[2].Type != EventTextDelta || events[1].ContentIndex != 0 || events[2].ContentIndex != 0 || events[2].Partial == nil {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestConsumeOpenAIChatSSETextAndThinkingEndBeforeDoneLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeOpenAIChatSSE(strings.NewReader(strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"plan\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n",
		"data: [DONE]\n\n",
	}, "")), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 7 || events[len(events)-3].Type != EventTextEnd || events[len(events)-3].Content != "answer" || events[len(events)-2].Type != EventThinkingEnd || events[len(events)-2].Content != "plan" || events[len(events)-1].Type != EventDone {
		t.Fatalf("end events should precede done like upstream: %#v", events)
	}
}

func TestConsumeOpenAIChatSSEStartsWithStartEventLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeOpenAIChatSSEForModel(strings.NewReader("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"), stream, "gpt-test"); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 2 || events[0].Type != EventStart || events[0].Partial == nil || events[0].Partial.Model != "gpt-test" || events[1].Type != EventDone {
		t.Fatalf("event sequence mismatch: %#v", events)
	}
}

func TestConsumeOpenAIChatSSEEOFDoneCarriesMessageLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeOpenAIChatSSEForModel(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"), stream, "gpt-test"); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventDone || last.Message == nil || last.Message.Text() != "ok" || last.Message.Model != "gpt-test" {
		t.Fatalf("done message mismatch: %#v", events)
	}
}

func TestConsumeOpenAIChatSSETopLevelErrorMatchesUpstreamShape(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeOpenAIChatSSEForModel(strings.NewReader("data: {\"error\":{\"message\":\"bad stream\"}}\n\n"), stream, "gpt-test"); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "gpt-test" || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "bad stream" {
		t.Fatalf("top-level error should be single upstream-style event, got %#v", events)
	}
}

func TestConsumeOpenAIChatSSEReaderErrorMatchesUpstreamShape(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeOpenAIChatSSEForModel(&errorAfterLineReader{line: "data: {\"choices\":[] }\n"}, stream, "gpt-test"); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventError || last.Error != "" || last.Message == nil || last.Message.Model != "gpt-test" || last.Message.StopReason != StopReasonError || !strings.Contains(last.Message.ErrorMessage, "sse: read failed") {
		t.Fatalf("reader error should be upstream-style error event, got %#v", events)
	}
}

func TestConsumeOpenAIChatSSEReaderErrorCarriesProviderMetadataLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	model := Model{ID: "gpt-test", Provider: Provider("openai"), API: ApiOpenAI}
	if err := consumeOpenAIChatSSE(&errorAfterLineReader{line: "data: {\"choices\":[] }\n"}, stream, model, false, false, false, false, false, false, false, false); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventError || last.Error != "" || last.Message == nil || last.Message.Model != "gpt-test" || last.Message.Provider != Provider("openai") || last.Message.API != ApiOpenAI || last.Message.StopReason != StopReasonError || !strings.Contains(last.Message.ErrorMessage, "sse: read failed") {
		t.Fatalf("reader error should carry full provider metadata: %#v", events)
	}
}

func TestConsumeOpenAIChatSSEMapsModelLengthFinishReason(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeOpenAIChatSSE(strings.NewReader(strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"model_length\"}]}\n\n",
		"data: [DONE]\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonMaxTokens {
		t.Fatalf("model length mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeOpenAIChatSSEMapsContentFilterFinishReasonToError(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeOpenAIChatSSE(strings.NewReader(strings.Join([]string{
		"data: {\"choices\":[{\"finish_reason\":\"content_filter\"}]}\n\n",
		"data: [DONE]\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage == "" {
		t.Fatalf("content filter mismatch: %#v ok=%v", message, ok)
	}
	events := stream.Events()
	last := events[len(events)-1]
	if last.Type != EventError || last.Error != "" || last.Message == nil || last.Message.StopReason != StopReasonError || last.Message.ErrorMessage != "Provider finish_reason: content_filter" {
		t.Fatalf("error event should carry upstream-style message: %#v", last)
	}
}

func TestConsumeOpenAIChatSSEMapsFunctionCallFinishReason(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	err := ConsumeOpenAIChatSSE(strings.NewReader(strings.Join([]string{
		"data: {\"choices\":[{\"finish_reason\":\"function_call\"}]}\n\n",
		"data: [DONE]\n\n",
	}, "")), stream)
	if err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonToolCalls {
		t.Fatalf("function call mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAIChatProviderHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request body", http.StatusBadRequest)
	}))
	defer server.Close()

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "m", API: ApiOpenAI, Provider: Provider("openai"), BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k"})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed error")
	}
	if message.StopReason != StopReasonError || message.ErrorMessage == "" {
		t.Fatalf("expected error message, got %#v", message)
	}
	if !strings.Contains(message.ErrorMessage, "HTTP 400 Bad Request: bad request body") {
		t.Fatalf("expected status and body like upstream, got %q", message.ErrorMessage)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Message == nil || events[0].Message.Model != "m" || events[0].Message.Provider != Provider("openai") || events[0].Message.API != ApiOpenAI {
		t.Fatalf("http error should carry provider-aware upstream message: %#v", events)
	}
}

func TestOpenAIChatProviderSendErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(&http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}))
	model := Model{ID: "gpt-test", API: ApiOpenAI, Provider: Provider("openai"), BaseURL: "https://openai.invalid"}
	maxRetries := 0
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k", MaxRetries: &maxRetries})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "http error: Post \"https://openai.invalid/v1/chat/completions\": dial failed" {
		t.Fatalf("send error mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAIChatProviderNewRequestErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewOpenAIChatProvider()
	model := Model{ID: "gpt-test", API: ApiOpenAI, Provider: Provider("openai"), BaseURL: "://bad-url"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "k"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http error: ") {
		t.Fatalf("request build error mismatch: %#v ok=%v", message, ok)
	}
}

func TestOpenAIChatProviderMissingAPIKeyErrorCarriesModelLikeUpstream(t *testing.T) {
	provider := NewOpenAIChatProvider()
	model := Model{ID: "gpt-test", API: ApiOpenAI, Provider: Provider("openai")}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{})
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Message == nil || events[0].Message.Model != "gpt-test" || events[0].Message.API != ApiOpenAI || events[0].Message.Provider != Provider("openai") || events[0].Message.ErrorMessage == "" {
		t.Fatalf("missing key should produce provider-aware error event like upstream: %#v", events)
	}
}
