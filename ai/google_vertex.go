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

type GoogleVertexProvider struct {
	client     *http.Client
	adcOptions GoogleVertexADCOptions
}

type GoogleVertexOption func(*GoogleVertexProvider)

func WithGoogleVertexHTTPClient(client *http.Client) GoogleVertexOption {
	return func(provider *GoogleVertexProvider) {
		if client != nil {
			provider.client = client
			provider.adcOptions.HTTPClient = client
		}
	}
}

func WithGoogleVertexADCOptions(options GoogleVertexADCOptions) GoogleVertexOption {
	return func(provider *GoogleVertexProvider) {
		provider.adcOptions = options
		if provider.adcOptions.HTTPClient == nil {
			provider.adcOptions.HTTPClient = provider.client
		}
	}
}

func NewGoogleVertexProvider(options ...GoogleVertexOption) *GoogleVertexProvider {
	provider := &GoogleVertexProvider{client: nil}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func (provider *GoogleVertexProvider) API() Api { return ApiGoogleVertex }

func (provider *GoogleVertexProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	return provider.Stream(ctx, model, request, GoogleStreamOptionsFromSimple(options))
}

func (provider *GoogleVertexProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	var account *GoogleVertexServiceAccount
	token := options.APIKey
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN"))
	}
	if token == "" {
		loadedAccount, err := LoadGoogleVertexServiceAccount("")
		if err != nil {
			return googleVertexProviderErrorStream(model, "Vertex access token missing: set GOOGLE_VERTEX_ACCESS_TOKEN, pass options.api_key, or configure GOOGLE_APPLICATION_CREDENTIALS")
		}
		account = &loadedAccount
		accessToken, err := FetchGoogleVertexAccessToken(ctx, provider.adcOptions, loadedAccount, "")
		if err != nil {
			return googleVertexProviderErrorStream(model, "Vertex ADC token exchange failed: "+err.Error())
		}
		token = accessToken.Token
	}
	project := strings.TrimSpace(os.Getenv("GOOGLE_VERTEX_PROJECT"))
	if project == "" && account != nil && account.ProjectID != nil {
		project = strings.TrimSpace(*account.ProjectID)
	}
	if project == "" {
		return googleVertexProviderErrorStream(model, "GOOGLE_VERTEX_PROJECT is not set")
	}
	client, err := effectiveHTTPClient(provider.client, options)
	if err != nil {
		return googleVertexProviderErrorStream(model, "http client: "+err.Error())
	}
	location := GoogleVertexLocation()
	body, err := BuildGoogleRequestBody(request, options)
	if err != nil {
		return googleVertexProviderErrorStream(model, err.Error())
	}
	data, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return googleVertexProviderErrorStream(model, err.Error())
	}
	baseURL := model.BaseURL
	if baseURL == "" {
		baseURL = GoogleVertexHost(location)
	} else {
		baseURL = ResolveGoogleVertexBaseURL(baseURL, location)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BuildGoogleVertexURL(baseURL, project, location, model.ID), bytes.NewReader(data))
	if err != nil {
		return googleVertexProviderErrorStream(model, "http error: "+err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	applyModelAndOptionHeaders(req, model, options)
	resp, err := sendWithRetry(client, req, data, options)
	if err != nil {
		if stream, ok := AbortedStreamIfCanceled(model, err); ok {
			return stream
		}
		return googleVertexProviderErrorStream(model, "http error: "+err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return googleVertexProviderErrorStream(model, fmt.Sprintf("Vertex API error (%s): %s", resp.Status, strings.TrimSpace(string(body))))
	}
	stream := NewAssistantMessageEventStream()
	if err := ConsumeGoogleSSEForModel(resp.Body, stream, model); err != nil {
		EmitErrorOrAborted(stream, model, err, func(message string) {
			stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Error: message})
			stream.Close(DoneReasonStop)
		})
	}
	return stream
}

func googleVertexProviderErrorStream(model Model, message string) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	assistantMessage := AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonError, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &assistantMessage})
	return stream
}

func GoogleVertexLocation() string {
	if location := strings.TrimSpace(os.Getenv("GOOGLE_VERTEX_LOCATION")); location != "" {
		return location
	}
	return "us-central1"
}

func GoogleVertexHost(location string) string {
	if location == "global" {
		return "https://aiplatform.googleapis.com"
	}
	return "https://" + location + "-aiplatform.googleapis.com"
}

func ResolveGoogleVertexBaseURL(baseURL string, location string) string {
	if baseURL == "https://{location}-aiplatform.googleapis.com" && location == "global" {
		return GoogleVertexHost(location)
	}
	return strings.ReplaceAll(baseURL, "{location}", location)
}

func BuildGoogleVertexURL(base, project, location, modelID string) string {
	return strings.TrimRight(base, "/") + "/v1/projects/" + project + "/locations/" + location + "/publishers/google/models/" + modelID + ":streamGenerateContent?alt=sse"
}
