package ai

import (
	"context"
	"net/http"
)

type OpenAICompletionsProvider struct {
	chat *OpenAIChatProvider
}

type OpenAICompletionsOption func(*OpenAICompletionsProvider)

func WithOpenAICompletionsHTTPClient(client *http.Client) OpenAICompletionsOption {
	return func(provider *OpenAICompletionsProvider) {
		if client != nil {
			provider.chat = newOpenAICompletionsChatProvider(client)
		}
	}
}

func NewOpenAICompletionsProvider(options ...OpenAICompletionsOption) *OpenAICompletionsProvider {
	provider := &OpenAICompletionsProvider{chat: newOpenAICompletionsChatProvider(nil)}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func newOpenAICompletionsChatProvider(client *http.Client) *OpenAIChatProvider {
	options := []OpenAIChatOption{
		withOpenAIChatContentToolCallsOnly(),
		withOpenAIChatFirstChoiceOnly(),
		withOpenAIChatFirstMetadataOnly(),
		withOpenAIChatIgnoreTopLevelErrorChunks(),
		withOpenAIChatSuppressMetadataEvents(),
		withOpenAIChatSuppressUsageEvents(),
	}
	if client != nil {
		options = append(options, WithOpenAIChatHTTPClient(client))
	}
	return NewOpenAIChatProvider(options...)
}

func (provider *OpenAICompletionsProvider) API() Api { return ApiOpenAICompletions }

func (provider *OpenAICompletionsProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	streamOptions := StreamOptionsFromSimple(options)
	if options.ReasoningLevel() != "" && options.ReasoningLevel() != ThinkingOff {
		if streamOptions.ProviderExtras == nil {
			streamOptions.ProviderExtras = map[string]any{}
		}
		streamOptions.ProviderExtras["reasoning_effort"] = openAICompletionsReasoningEffort(options.ReasoningLevel())
	}
	return provider.Stream(ctx, model, request, streamOptions)
}

func openAICompletionsReasoningEffort(level ThinkingLevel) string {
	switch level {
	case ThinkingMinimal:
		return "minimal"
	case ThinkingLow:
		return "low"
	case ThinkingMedium:
		return "medium"
	case ThinkingHigh, ThinkingXHigh:
		return "high"
	default:
		return ""
	}
}

func (provider *OpenAICompletionsProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	return provider.chat.Stream(ctx, model, request, options)
}
