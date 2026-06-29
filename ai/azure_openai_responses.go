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

const defaultAzureOpenAIResponsesAPIVersion = "v1"

type AzureOpenAIResponsesProvider struct {
	client *http.Client
}

type AzureOpenAIResponsesOption func(*AzureOpenAIResponsesProvider)

func WithAzureOpenAIResponsesHTTPClient(client *http.Client) AzureOpenAIResponsesOption {
	return func(provider *AzureOpenAIResponsesProvider) {
		if client != nil {
			provider.client = client
		}
	}
}

func NewAzureOpenAIResponsesProvider(options ...AzureOpenAIResponsesOption) *AzureOpenAIResponsesProvider {
	provider := &AzureOpenAIResponsesProvider{client: nil}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func (provider *AzureOpenAIResponsesProvider) API() Api { return ApiAzureOpenAIResponses }

func (provider *AzureOpenAIResponsesProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	streamOptions := StreamOptionsFromSimple(options)
	if options.ReasoningLevel() != "" && options.ReasoningLevel() != ThinkingOff {
		if streamOptions.ProviderExtras == nil {
			streamOptions.ProviderExtras = map[string]any{}
		}
		streamOptions.ProviderExtras["reasoning_effort"] = string(options.ReasoningLevel())
	}
	return provider.Stream(ctx, model, request, streamOptions)
}

func (provider *AzureOpenAIResponsesProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	apiKey := options.APIKey
	if apiKey == "" {
		if value, ok := GetEnvAPIKey("azure-openai-responses"); ok {
			apiKey = value
		}
	}
	if apiKey == "" {
		return azureOpenAIResponsesProviderErrorStream(model, "AZURE_OPENAI_API_KEY is not set")
	}
	baseURL := model.BaseURL
	if baseURL == "" {
		return azureOpenAIResponsesProviderErrorStream(model, "Azure OpenAI base URL is not set")
	}
	client, err := effectiveHTTPClient(provider.client, options)
	if err != nil {
		return azureOpenAIResponsesProviderErrorStream(model, "http client: "+err.Error())
	}
	body, err := BuildOpenAIResponsesRequestBody(model, request, options)
	if err != nil {
		return azureOpenAIResponsesProviderErrorStream(model, err.Error())
	}
	body["model"] = ResolveAzureOpenAIDeploymentName(model.ID, options.ProviderExtras)
	payload, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return azureOpenAIResponsesProviderErrorStream(model, err.Error())
	}
	apiVersion := defaultAzureOpenAIResponsesAPIVersion
	if value, ok := options.ProviderExtras["azure_api_version"].(string); ok {
		apiVersion = value
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BuildAzureOpenAIResponsesURL(baseURL, apiVersion), bytes.NewReader(payload))
	if err != nil {
		return azureOpenAIResponsesProviderErrorStream(model, "http error: "+err.Error())
	}
	req.Header.Set("api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	applyModelAndOptionHeaders(req, model, options)

	response, err := sendWithRetry(client, req, payload, options)
	if err != nil {
		if stream, ok := AbortedStreamIfCanceled(model, err); ok {
			return stream
		}
		return azureOpenAIResponsesProviderErrorStream(model, "http error: "+err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(response.Body)
		return azureOpenAIResponsesProviderErrorStream(model, fmt.Sprintf("Azure OpenAI API error (%s): %s", response.Status, strings.TrimSpace(string(data))))
	}
	stream := NewAssistantMessageEventStream()
	if err := ConsumeResponsesSSE(response.Body, stream, model); err != nil {
		if aborted, ok := AbortedStreamIfCanceled(model, err); ok {
			return aborted
		}
		return azureOpenAIResponsesProviderErrorStream(model, err.Error())
	}
	return stream
}

func azureOpenAIResponsesProviderErrorStream(model Model, message string) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	assistantMessage := AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonError, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &assistantMessage})
	return stream
}

func BuildAzureOpenAIResponsesURL(base, apiVersion string) string {
	return strings.TrimRight(base, "/") + "/openai/v1/responses?api-version=" + apiVersion
}

func ResolveAzureOpenAIDeploymentName(modelID string, providerExtras map[string]any) string {
	if name, ok := providerExtras["azure_deployment_name"].(string); ok {
		return name
	}
	for _, entry := range strings.Split(os.Getenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP"), ",") {
		model, deployment, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if ok && strings.TrimSpace(model) == modelID {
			return strings.TrimSpace(deployment)
		}
	}
	return modelID
}
