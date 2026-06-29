package ai

import (
	"context"
	"net/http"
	"time"
	"unicode"
)

const mistralToolCallIDLength = 9

type MistralProvider struct {
	client *http.Client
}

type MistralOption func(*MistralProvider)

func WithMistralHTTPClient(client *http.Client) MistralOption {
	return func(provider *MistralProvider) {
		if client != nil {
			provider.client = client
		}
	}
}

func NewMistralProvider(options ...MistralOption) *MistralProvider {
	provider := &MistralProvider{client: nil}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func (provider *MistralProvider) API() Api { return ApiMistral }

func (provider *MistralProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	streamOptions := StreamOptionsFromSimple(options)
	if model.Reasoning && options.ReasoningLevel() != "" && options.ReasoningLevel() != ThinkingOff {
		if streamOptions.ProviderExtras == nil {
			streamOptions.ProviderExtras = map[string]any{}
		}
		streamOptions.ProviderExtras["reasoning_effort"] = mistralReasoningEffort(options.ReasoningLevel())
	}
	return provider.Stream(ctx, model, request, streamOptions)
}

func mistralReasoningEffort(level ThinkingLevel) string {
	switch level {
	case ThinkingMinimal, ThinkingLow:
		return "low"
	case ThinkingMedium:
		return "medium"
	case ThinkingHigh, ThinkingXHigh:
		return "high"
	default:
		return ""
	}
}

func (provider *MistralProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	if model.BaseURL == "" {
		model.BaseURL = "https://api.mistral.ai"
	}
	if model.Provider == "" {
		model.Provider = Provider("mistral")
	}
	chatModel := model
	chatModel.API = ApiOpenAI
	chatOptions := []OpenAIChatOption{WithOpenAIChatHTTPClient(provider.client), withOpenAIChatToolResultNames(), withOpenAIChatEmptyAssistantContentString(), withOpenAIChatDisableStreamOptions(), withOpenAIChatTextOnlyUserContent(), withOpenAIChatContentToolCallsOnly(), withOpenAIChatExplicitToolResultNameOnly(), withOpenAIChatIgnoreSystemRoleMessages(), withOpenAIChatFirstChoiceOnly(), withOpenAIChatIgnoreResponseModel(), withOpenAIChatFunctionCallFinishStops(), withOpenAIChatIgnoreToolCallIndex(), withOpenAIChatErrorPrefix("Mistral API error"), withOpenAIChatMissingAPIKeyMessage("MISTRAL_API_KEY is not set")}
	if options.SessionID != "" {
		chatOptions = append(chatOptions, WithOpenAIChatExtraHeaders(map[string]string{"x-affinity": options.SessionID}))
	}
	stream := NewOpenAIChatProvider(chatOptions...).Stream(ctx, chatModel, normalizeMistralContext(request), options)
	return normalizeMistralStream(stream, model)
}

func normalizeMistralContext(request Context) Context {
	out := request
	out.Messages = make([]Message, len(request.Messages))
	for index, message := range request.Messages {
		copy := message
		if len(message.Content) > 0 {
			copy.Content = make([]ContentBlock, len(message.Content))
			for blockIndex, block := range message.Content {
				if block.Type == ContentToolCall && block.ToolCall != nil {
					toolCall := *block.ToolCall
					toolCall.ID = normalizeMistralToolCallID(toolCall.ID)
					block.ToolCall = &toolCall
				}
				copy.Content[blockIndex] = block
			}
		}
		if len(message.ToolCalls) > 0 {
			copy.ToolCalls = make([]ToolCall, len(message.ToolCalls))
			for callIndex, call := range message.ToolCalls {
				call.ID = normalizeMistralToolCallID(call.ID)
				copy.ToolCalls[callIndex] = call
			}
		}
		if copy.ToolCallID != "" {
			copy.ToolCallID = normalizeMistralToolCallID(copy.ToolCallID)
		}
		out.Messages[index] = copy
	}
	return out
}

func normalizeMistralStream(input *AssistantMessageEventStream, model Model) *AssistantMessageEventStream {
	out := NewAssistantMessageEventStream()
	if input.IsLive() {
		out.MarkLive()
		go normalizeMistralStreamInto(input, out, model)
		return out
	}
	normalizeMistralStreamInto(input, out, model)
	return out
}

func normalizeMistralStreamInto(input *AssistantMessageEventStream, out *AssistantMessageEventStream, model Model) {
	partial := mistralEmptyPartial(model)
	started := false
	firstToolCallID := ""
	textStarted := false
	textIndex := -1
	textContent := ""
	toolStarted := false
	toolIndex := -1
	toolArguments := ""
	var toolCall *ToolCall
	nextContentIndex := 0
	var mergedUsage *Usage
	responseID := ""
	for index := 0; ; {
		event, next, err := input.Next(context.Background(), index)
		if err != nil {
			message := mistralEmptyPartial(model)
			message.StopReason = StopReasonError
			message.ErrorMessage = err.Error()
			out.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &message})
			return
		}
		index = next
		if event.Type == EventError {
			message := mistralEmptyPartial(model)
			message.StopReason = StopReasonError
			if event.Message != nil && event.Message.ErrorMessage != "" {
				message.ErrorMessage = event.Message.ErrorMessage
			} else {
				message.ErrorMessage = event.Error
			}
			event.Message = &message
			event.Error = ""
			out.Emit(event)
			return
		}
		if !started {
			out.Emit(AssistantMessageEvent{Type: EventStart, Partial: cloneAssistantMessage(partial)})
			started = true
		}
		if event.Type == EventStart {
			continue
		}
		if event.Type == EventMetadata {
			if event.ResponseID != "" && responseID == "" {
				responseID = event.ResponseID
			}
			continue
		}
		if event.Usage != nil {
			if mergedUsage == nil {
				mergedUsage = &Usage{}
			}
			if event.Usage.InputTokens != 0 {
				mergedUsage.InputTokens = event.Usage.InputTokens
			}
			if event.Usage.OutputTokens != 0 {
				mergedUsage.OutputTokens = event.Usage.OutputTokens
			}
			mergedUsage.TotalTokenCount = mergedUsage.InputTokens + mergedUsage.OutputTokens + mergedUsage.CacheReadTokens + mergedUsage.CacheWriteTokens
			mergedUsage.HasTotalTokens = true
			usage := *mergedUsage
			event.Usage = &usage
			if event.Type == EventUsage {
				continue
			}
		}
		if event.ToolCall != nil || event.Type == EventToolCallStart {
			call := toolCallFromMistralEvent(event)
			if call == nil {
				continue
			}
			call.ID = normalizeMistralToolCallID(call.ID)
			if firstToolCallID == "" && event.Type == EventToolCall {
				firstToolCallID = call.ID
			}
			if firstToolCallID == "" && event.Type == EventToolCallStart {
				firstToolCallID = call.ID
			}
			if firstToolCallID != "" && call.ID != "" && call.ID != firstToolCallID {
				continue
			}
			event.ToolCall = call
		}
		if (event.Type == EventToolCall || event.Type == EventToolCallStart) && event.ToolCall != nil {
			call := *event.ToolCall
			if call.Arguments == nil {
				call.Arguments = map[string]any{}
			}
			toolCall = &call
			if !toolStarted {
				toolIndex = nextContentIndex
				nextContentIndex++
				for len(partial.Content) <= toolIndex {
					partial.Content = append(partial.Content, ContentBlock{})
				}
				partial.Content[toolIndex] = ContentBlock{Type: ContentToolCall, ToolCall: toolCall}
				out.Emit(AssistantMessageEvent{Type: EventToolCallStart, ContentIndex: toolIndex, Partial: cloneAssistantMessage(partial)})
				toolStarted = true
			}
			continue
		}
		if event.Type == EventToolCallDelta && event.Delta != "" {
			if toolIndex < 0 {
				toolIndex = nextContentIndex
				nextContentIndex++
			}
			toolArguments += event.Delta
			event.ContentIndex = toolIndex
			event.Partial = cloneAssistantMessage(partial)
			out.Emit(event)
			continue
		}
		if event.Type == EventToolCallEnd {
			if toolStarted {
				event.ContentIndex = toolIndex
				if event.ToolCall != nil {
					toolCall = event.ToolCall
				}
				out.Emit(event)
			}
			toolStarted = false
			continue
		}
		if event.Type == EventTextStart {
			if !textStarted {
				textIndex = event.ContentIndex
				if textIndex < 0 {
					textIndex = nextContentIndex
				}
				if nextContentIndex <= textIndex {
					nextContentIndex = textIndex + 1
				}
				for len(partial.Content) <= textIndex {
					partial.Content = append(partial.Content, ContentBlock{})
				}
				partial.Content[textIndex] = ContentBlock{Type: ContentText}
				event.ContentIndex = textIndex
				event.Partial = cloneAssistantMessage(partial)
				textStarted = true
				out.Emit(event)
			}
			continue
		}
		if event.Type == EventTextDelta && event.Delta != "" {
			if !textStarted {
				textIndex = nextContentIndex
				nextContentIndex++
				for len(partial.Content) <= textIndex {
					partial.Content = append(partial.Content, ContentBlock{})
				}
				partial.Content[textIndex] = ContentBlock{Type: ContentText}
				out.Emit(AssistantMessageEvent{Type: EventTextStart, ContentIndex: textIndex, Partial: cloneAssistantMessage(partial)})
				textStarted = true
			}
			textContent += event.Delta
			for len(partial.Content) <= textIndex {
				partial.Content = append(partial.Content, ContentBlock{})
			}
			partial.Content[textIndex] = ContentBlock{Type: ContentText, Text: textContent}
			event.ContentIndex = textIndex
			event.Partial = cloneAssistantMessage(partial)
		}
		if event.Type == EventTextEnd {
			if textStarted {
				event.ContentIndex = textIndex
				if textContent == "" {
					textContent = event.Content
				}
				for len(partial.Content) <= textIndex {
					partial.Content = append(partial.Content, ContentBlock{})
				}
				partial.Content[textIndex] = ContentBlock{Type: ContentText, Text: textContent}
				event.Partial = cloneAssistantMessage(partial)
				out.Emit(event)
			}
			textStarted = false
			continue
		}
		if event.Type == EventDone {
			message := mistralEmptyPartial(model)
			message.ResponseID = responseID
			message.StopReason = stopReasonFromMistralDone(event.DoneReason)
			if mergedUsage != nil {
				usage := *mergedUsage
				message.Usage = &usage
			}
			if textStarted {
				for len(partial.Content) <= textIndex {
					partial.Content = append(partial.Content, ContentBlock{})
				}
				partial.Content[textIndex] = ContentBlock{Type: ContentText, Text: textContent}
				out.Emit(AssistantMessageEvent{Type: EventTextEnd, ContentIndex: textIndex, Content: textContent, Partial: cloneAssistantMessage(partial)})
				message.Content = append(message.Content, ContentBlock{Type: ContentText, Text: textContent})
			} else if textContent != "" {
				message.Content = append(message.Content, ContentBlock{Type: ContentText, Text: textContent})
			}
			if toolStarted && toolCall != nil {
				call := *toolCall
				if args, ok := parsePartialJSONObject(toolArguments); ok {
					call.Arguments = args
				}
				for len(message.Content) <= toolIndex {
					message.Content = append(message.Content, ContentBlock{})
				}
				message.Content[toolIndex] = ContentBlock{Type: ContentToolCall, ToolCall: &call}
				message.ToolCalls = append(message.ToolCalls, call)
				partial.Content = append([]ContentBlock(nil), message.Content...)
				partial.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
				out.Emit(AssistantMessageEvent{Type: EventToolCallEnd, ContentIndex: toolIndex, ToolCall: &call, Partial: cloneAssistantMessage(partial)})
			} else if toolCall != nil {
				call := *toolCall
				for len(message.Content) <= toolIndex {
					message.Content = append(message.Content, ContentBlock{})
				}
				message.Content[toolIndex] = ContentBlock{Type: ContentToolCall, ToolCall: &call}
				message.ToolCalls = append(message.ToolCalls, call)
			}
			out.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: event.DoneReason, Message: &message})
			return
		}
		out.Emit(event)
	}
}

func normalizeMistralToolCallID(id string) string {
	normalized := make([]rune, 0, len(id))
	for _, char := range id {
		if char <= unicode.MaxASCII && unicode.IsLetter(char) || char <= unicode.MaxASCII && unicode.IsDigit(char) {
			normalized = append(normalized, char)
		}
	}
	if len(normalized) == mistralToolCallIDLength {
		return string(normalized)
	}
	seed := string(normalized)
	if seed == "" {
		seed = id
	}
	short := ShortHash(seed)
	out := make([]rune, 0, mistralToolCallIDLength)
	for _, char := range short {
		if char <= unicode.MaxASCII && (unicode.IsLetter(char) || unicode.IsDigit(char)) {
			out = append(out, char)
			if len(out) == mistralToolCallIDLength {
				break
			}
		}
	}
	return string(out)
}

func toolCallFromMistralEvent(event AssistantMessageEvent) *ToolCall {
	if event.ToolCall != nil {
		call := *event.ToolCall
		return &call
	}
	if event.Partial == nil || event.ContentIndex < 0 || event.ContentIndex >= len(event.Partial.Content) {
		return nil
	}
	block := event.Partial.Content[event.ContentIndex]
	if block.Type != ContentToolCall || block.ToolCall == nil {
		return nil
	}
	call := *block.ToolCall
	return &call
}

func mistralEmptyPartial(model Model) AssistantMessage {
	return AssistantMessage{Role: AssistantRoleAssistant, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonEndTurn, Timestamp: time.Now().UnixMilli()}
}

func stopReasonFromMistralDone(reason DoneReason) StopReason {
	switch reason {
	case DoneReasonToolCalls:
		return StopReasonToolCalls
	case DoneReasonLength:
		return StopReasonMaxTokens
	default:
		return StopReasonEndTurn
	}
}
