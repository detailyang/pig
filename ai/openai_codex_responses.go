package ai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultCodexResponsesBaseURL = "https://chatgpt.com/backend-api"

type CodexResponsesProvider struct {
	client *http.Client
}

type OpenAICodexResponsesProvider = CodexResponsesProvider

type CodexResponsesOption func(*CodexResponsesProvider)

func WithCodexResponsesHTTPClient(client *http.Client) CodexResponsesOption {
	return func(provider *CodexResponsesProvider) {
		if client != nil {
			provider.client = client
		}
	}
}

func NewCodexResponsesProvider(options ...CodexResponsesOption) *CodexResponsesProvider {
	provider := &CodexResponsesProvider{client: nil}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func (provider *CodexResponsesProvider) API() Api { return ApiOpenAICodexResponses }

func (provider *CodexResponsesProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	streamOptions := StreamOptionsFromSimple(options)
	if effort, ok := codexResponsesReasoningEffort(options.ReasoningLevel()); ok {
		if streamOptions.ProviderExtras == nil {
			streamOptions.ProviderExtras = map[string]any{}
		}
		streamOptions.ProviderExtras["reasoning_effort"] = effort
	}
	return provider.Stream(ctx, model, request, streamOptions)
}

func (provider *CodexResponsesProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	token := options.APIKey
	if token == "" {
		if value := os.Getenv("CODEX_AUTH_TOKEN"); value != "" {
			token = value
		} else if value, ok := GetEnvAPIKey("openai-codex"); ok {
			token = value
		}
	}
	if token == "" {
		return codexResponsesProviderErrorStream(model, "Codex auth missing: set CODEX_AUTH_TOKEN or pass options.api_key")
	}
	client, err := effectiveHTTPClient(provider.client, options)
	if err != nil {
		return codexResponsesProviderErrorStream(model, "http client: "+err.Error())
	}
	body, err := BuildCodexResponsesRequestBody(model, request, options)
	if err != nil {
		return codexResponsesProviderErrorStream(model, err.Error())
	}
	payload, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return codexResponsesProviderErrorStream(model, err.Error())
	}
	baseURL := model.BaseURL
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BuildCodexResponsesURL(baseURL), bytes.NewReader(payload))
	if err != nil {
		return codexResponsesProviderErrorStream(model, "http error: "+err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("originator", "pi")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	if options.SessionID != "" {
		req.Header.Set("session_id", options.SessionID)
	}
	if accountID := resolveCodexAccountID(options.ProviderExtras); accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}
	applyModelAndOptionHeaders(req, model, options)
	response, err := sendWithRetry(client, req, payload, options)
	if err != nil {
		if stream, ok := AbortedStreamIfCanceled(model, err); ok {
			return stream
		}
		return codexResponsesProviderErrorStream(model, "http error: "+err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(response.Body)
		return codexResponsesProviderErrorStream(model, fmt.Sprintf("Codex API error (%s): %s", response.Status, strings.TrimSpace(string(data))))
	}
	stream := NewAssistantMessageEventStream()
	if err := ConsumeResponsesSSE(response.Body, stream, model); err != nil {
		if aborted, ok := AbortedStreamIfCanceled(model, err); ok {
			return aborted
		}
		return codexResponsesProviderErrorStream(model, err.Error())
	}
	return stream
}

func codexResponsesProviderErrorStream(model Model, message string) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	assistantMessage := AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonError, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &assistantMessage})
	return stream
}

func BuildCodexResponsesURL(baseURL string) string {
	raw := baseURL
	if strings.TrimSpace(raw) == "" {
		raw = defaultCodexResponsesBaseURL
	}
	normalized := strings.TrimRight(raw, "/")
	if strings.HasSuffix(normalized, "/codex/responses") {
		return normalized
	}
	if strings.HasSuffix(normalized, "/codex") {
		return normalized + "/responses"
	}
	return normalized + "/codex/responses"
}

func BuildCodexResponsesRequestBody(model Model, request Context, options StreamOptions) (map[string]any, error) {
	instructions := "You are a helpful assistant."
	messages := request.Messages
	if request.HasSystemPrompt || request.SystemPrompt != "" {
		instructions = request.SystemPrompt
	}
	body := map[string]any{
		"model":               model.ID,
		"store":               false,
		"stream":              true,
		"instructions":        instructions,
		"input":               ConvertMessagesForOpenAIResponses(messages),
		"include":             []string{"reasoning.encrypted_content"},
		"tool_choice":         "auto",
		"parallel_tool_calls": true,
	}
	if options.Temperature != nil {
		body["temperature"] = *options.Temperature
	}
	if options.SessionID != "" {
		body["prompt_cache_key"] = options.SessionID
	}
	if len(request.Tools) > 0 {
		body["tools"] = SerializeOpenAIResponsesTools(request.Tools)
	}
	if effort, ok := options.ProviderExtras["reasoning_effort"]; ok {
		body["reasoning"] = map[string]any{"effort": effort, "summary": "auto"}
	}
	return body, nil
}

func codexResponsesReasoningEffort(level ThinkingLevel) (string, bool) {
	switch level {
	case ThinkingMinimal:
		return "minimal", true
	case ThinkingLow:
		return "low", true
	case ThinkingMedium:
		return "medium", true
	case ThinkingHigh, ThinkingXHigh:
		return "high", true
	default:
		return "", false
	}
}

func resolveCodexAccountID(providerExtras map[string]any) string {
	if accountID, ok := providerExtras["chatgpt_account_id"].(string); ok {
		return accountID
	}
	return os.Getenv("CODEX_ACCOUNT_ID")
}
