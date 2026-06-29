package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const anthropicVersion = "2023-06-01"
const anthropicBetas = "prompt-caching-2024-07-31,interleaved-thinking-2025-05-14,fine-grained-tool-streaming-2025-05-14"

type AnthropicProvider struct {
	client *http.Client
}

type AnthropicOption func(*AnthropicProvider)

func WithAnthropicHTTPClient(client *http.Client) AnthropicOption {
	return func(provider *AnthropicProvider) {
		if client != nil {
			provider.client = client
		}
	}
}

func NewAnthropicProvider(options ...AnthropicOption) *AnthropicProvider {
	provider := &AnthropicProvider{client: nil}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func (provider *AnthropicProvider) API() Api { return ApiAnthropic }

func (provider *AnthropicProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	streamOptions := StreamOptionsFromSimple(options)
	if options.ReasoningLevel() != "" && options.ReasoningLevel() != ThinkingOff {
		if streamOptions.ProviderExtras == nil {
			streamOptions.ProviderExtras = map[string]any{}
		}
		streamOptions.ProviderExtras["thinking"] = map[string]any{"type": "enabled", "budget_tokens": anthropicSimpleThinkingBudget(options)}
	}
	return provider.Stream(ctx, model, request, streamOptions)
}

func anthropicSimpleThinkingBudget(options SimpleStreamOptions) int {
	if budget, ok := options.ThinkingBudgets.BudgetFor(options.ReasoningLevel()); ok {
		return budget
	}
	switch options.ReasoningLevel() {
	case ThinkingMinimal:
		return 1024
	case ThinkingLow:
		return 4096
	case ThinkingMedium:
		return 8192
	case ThinkingHigh:
		return 16384
	case ThinkingXHigh:
		return 32768
	default:
		return 0
	}
}

func (provider *AnthropicProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	apiKey := options.APIKey
	if apiKey == "" {
		if value, ok := GetEnvAPIKey("anthropic"); ok {
			apiKey = value
		}
	}
	if apiKey == "" {
		return anthropicProviderErrorStream(model, "ANTHROPIC_API_KEY is not set")
	}
	client, err := effectiveHTTPClient(provider.client, options)
	if err != nil {
		return anthropicProviderErrorStream(model, "http client: "+err.Error())
	}
	body := BuildAnthropicRequestBody(model, request, options)
	payload, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return anthropicProviderErrorStream(model, err.Error())
	}
	baseURL, err := ResolveProviderBaseURL(model, options)
	if err != nil {
		return anthropicProviderErrorStream(model, err.Error())
	}
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return anthropicProviderErrorStream(model, "http error: "+err.Error())
	}
	httpRequest.Header.Set("x-api-key", apiKey)
	httpRequest.Header.Set("anthropic-version", anthropicVersion)
	httpRequest.Header.Set("anthropic-beta", anthropicBetas)
	httpRequest.Header.Set("content-type", "application/json")
	httpRequest.Header.Set("accept", "text/event-stream")
	if options.SessionID != "" && anthropicSendSessionAffinityHeaders(model) {
		httpRequest.Header.Set("x-session-affinity", options.SessionID)
	}
	applyModelAndOptionHeaders(httpRequest, model, options)
	resp, err := sendWithRetry(client, httpRequest, payload, options)
	if err != nil {
		if stream, ok := AbortedStreamIfCanceled(model, err); ok {
			return stream
		}
		return anthropicProviderErrorStream(model, "http error: "+err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return anthropicProviderErrorStream(model, fmt.Sprintf("HTTP %s: %s", resp.Status, strings.TrimSpace(string(data))))
	}
	stream := NewAssistantMessageEventStream()
	if err := ConsumeAnthropicSSEForModel(resp.Body, stream, model); err != nil {
		EmitErrorOrAborted(stream, model, err, func(messageText string) {
			message := anthropicEmptyPartial(model)
			message.StopReason = StopReasonError
			message.ErrorMessage = messageText
			stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &message})
		})
	}
	return stream
}

func anthropicProviderErrorStream(model Model, message string) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	assistantMessage := anthropicEmptyPartial(model)
	assistantMessage.StopReason = StopReasonError
	assistantMessage.ErrorMessage = message
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &assistantMessage})
	return stream
}

func anthropicEmptyPartial(model Model) AssistantMessage {
	return AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonEndTurn, Timestamp: time.Now().UnixMilli()}
}

func BuildAnthropicRequestBody(model Model, request Context, options StreamOptions) map[string]any {
	cacheControl := anthropicCacheControl(options.CacheRetention, model)
	body := map[string]any{
		"model":    model.ID,
		"messages": convertMessagesForAnthropic(request.Messages, cacheControl),
		"stream":   true,
	}
	if system := anthropicSystemBlocks(request); len(system) > 0 {
		if cacheControl != nil {
			for index := range system {
				system[index]["cache_control"] = cloneMap(cacheControl)
			}
		}
		body["system"] = system
	}
	if options.MaxTokens > 0 {
		body["max_tokens"] = options.MaxTokens
	} else {
		body["max_tokens"] = model.MaxTokens
	}
	if options.Temperature != nil {
		body["temperature"] = *options.Temperature
	}
	if thinking, ok := options.ProviderExtras["thinking"]; ok {
		body["thinking"] = thinking
		if thinkingMap, ok := thinking.(map[string]any); ok && thinkingMap["type"] == "enabled" {
			delete(body, "temperature")
		}
	}
	if request.HasTools || request.Tools != nil {
		body["tools"] = serializeAnthropicTools(request.Tools, cacheControl, model)
	}
	if userID, ok := options.Metadata["user_id"]; ok {
		body["metadata"] = map[string]any{"user_id": userID}
	}
	return body
}

func anthropicCacheControl(retention CacheRetention, model Model) map[string]any {
	switch retention {
	case CacheNone:
		return nil
	case CacheLong:
		cacheControl := map[string]any{"type": "ephemeral"}
		if anthropicSupportsLongCacheRetention(model) {
			cacheControl["ttl"] = "1h"
		}
		return cacheControl
	default:
		return map[string]any{"type": "ephemeral"}
	}
}

func anthropicSupportsLongCacheRetention(model Model) bool {
	value, ok := model.Compat["supportsLongCacheRetention"].(bool)
	if ok {
		return value
	}
	return string(model.Provider) != "fireworks"
}

func anthropicSupportsCacheControlOnTools(model Model) bool {
	value, ok := model.Compat["supportsCacheControlOnTools"].(bool)
	if ok {
		return value
	}
	return string(model.Provider) != "fireworks"
}

func anthropicSendSessionAffinityHeaders(model Model) bool {
	value, ok := model.Compat["sendSessionAffinityHeaders"].(bool)
	if ok {
		return value
	}
	provider := string(model.Provider)
	return provider == "fireworks" || provider == "cloudflare-ai-gateway" && strings.Contains(model.BaseURL, "anthropic")
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func anthropicSystemBlocks(request Context) []map[string]any {
	if request.HasSystemPrompt || request.SystemPrompt != "" {
		return []map[string]any{{"type": "text", "text": request.SystemPrompt}}
	}
	return nil
}

func ConvertMessagesForAnthropic(messages []Message) []map[string]any {
	return convertMessagesForAnthropic(messages, nil)
}

func convertMessagesForAnthropic(messages []Message, cacheControl map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	lastUserIndex := lastAnthropicUserIndex(messages)
	for index, message := range messages {
		switch message.Role {
		case RoleSystem:
			continue
		case RoleUser:
			out = append(out, map[string]any{"role": "user", "content": anthropicContent(message.Content, cacheControlForMessage(index, cacheControl, lastUserIndex))})
		case RoleAssistant:
			out = append(out, map[string]any{"role": "assistant", "content": anthropicContent(message.Content, nil)})
		case RoleTool:
			block := map[string]any{"type": "tool_result", "tool_use_id": message.ToolCallID, "is_error": anthropicToolResultIsError(message), "content": anthropicContent(message.Content, nil)}
			if index == lastUserIndex && cacheControl != nil {
				block["cache_control"] = cloneMap(cacheControl)
			}
			out = append(out, map[string]any{"role": "user", "content": []map[string]any{block}})
		}
	}
	return out
}

func anthropicToolResultIsError(message Message) bool {
	return message.IsError
}

func lastAnthropicUserIndex(messages []Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == RoleUser || messages[index].Role == RoleTool {
			return index
		}
	}
	return -1
}

func cacheControlForMessage(index int, cacheControl map[string]any, lastUserIndex int) map[string]any {
	if cacheControl == nil || index != lastUserIndex {
		return nil
	}
	return cacheControl
}

func anthropicContent(blocks []ContentBlock, cacheControl map[string]any) []map[string]any {
	content := make([]map[string]any, 0, len(blocks))
	lastBlockIndex := len(blocks) - 1
	for index, block := range blocks {
		switch block.Type {
		case ContentText:
			item := map[string]any{"type": "text", "text": block.Text}
			if cacheControl != nil && index == lastBlockIndex {
				item["cache_control"] = cloneMap(cacheControl)
			}
			content = append(content, item)
		case ContentThinking:
			item := map[string]any{"type": "thinking", "thinking": block.Thinking}
			if block.ThinkingSignature != "" || block.thinkingSignaturePresent {
				item["signature"] = block.ThinkingSignature
			}
			content = append(content, item)
		case ContentImage:
			item := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": block.MimeType, "data": block.Data}}
			if cacheControl != nil && index == lastBlockIndex {
				item["cache_control"] = cloneMap(cacheControl)
			}
			content = append(content, item)
		case ContentToolCall:
			if block.ToolCall != nil {
				content = append(content, map[string]any{"type": "tool_use", "id": block.ToolCall.ID, "name": block.ToolCall.Name, "input": block.ToolCall.Arguments})
			}
		}
	}
	return content
}

func SerializeAnthropicTools(tools []Tool) []map[string]any {
	return serializeAnthropicTools(tools, nil, Model{})
}

func serializeAnthropicTools(tools []Tool, cacheControl map[string]any, model Model) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	lastToolIndex := len(tools) - 1
	for index, tool := range tools {
		item := map[string]any{"name": tool.Name, "description": tool.Description, "input_schema": tool.Parameters}
		if cacheControl != nil && index == lastToolIndex && anthropicSupportsCacheControlOnTools(model) {
			item["cache_control"] = cloneMap(cacheControl)
		}
		out = append(out, item)
	}
	return out
}

func ConsumeAnthropicSSE(reader io.Reader, stream *AssistantMessageEventStream) error {
	return ConsumeAnthropicSSEForModel(reader, stream, Model{})
}

func ConsumeAnthropicSSEForModel(reader io.Reader, stream *AssistantMessageEventStream, model Model) error {
	partial := anthropicEmptyPartial(model)
	toolJSON := map[int]string{}
	stopReason := ""
	usage := &Usage{}
	return ConsumeGenericSSE(reader, func(event sseEvent) bool {
		var payload map[string]any
		if err := json.Unmarshal([]byte(event.data), &payload); err != nil {
			return true
		}
		kind := event.event
		if kind == "" {
			kind = stringValue(payload["type"])
		}
		switch kind {
		case "message_start":
			if message, ok := payload["message"].(map[string]any); ok {
				if id := stringValue(message["id"]); id != "" {
					partial.ResponseID = id
				}
				if usageMap, ok := message["usage"].(map[string]any); ok {
					addAnthropicUsage(usage, usageMap)
				}
			}
		case "content_block_start":
			index := uintNumber(payload["index"])
			block, _ := payload["content_block"].(map[string]any)
			switch block["type"] {
			case "text":
				setAnthropicContentBlockAt(&partial, index, ContentBlock{Type: ContentText})
				stream.Emit(AssistantMessageEvent{Type: EventTextStart, ContentIndex: index, Partial: cloneAssistantMessage(partial)})
			case "thinking":
				setAnthropicContentBlockAt(&partial, index, ContentBlock{Type: ContentThinking})
				stream.Emit(AssistantMessageEvent{Type: EventThinkingStart, ContentIndex: index, Partial: cloneAssistantMessage(partial)})
			case "redacted_thinking":
				setAnthropicContentBlockAt(&partial, index, ContentBlock{Type: ContentThinking, Thinking: "[Reasoning redacted]", ThinkingSignature: stringValue(block["data"]), Redacted: true})
				stream.Emit(AssistantMessageEvent{Type: EventThinkingStart, ContentIndex: index, Partial: cloneAssistantMessage(partial)})
			case "tool_use":
				toolCall := &ToolCall{ID: stringValue(block["id"]), Name: stringValue(block["name"]), Arguments: map[string]any{}}
				setAnthropicContentBlockAt(&partial, index, ContentBlock{Type: ContentToolCall, ToolCall: toolCall})
				upsertAnthropicToolCall(&partial, *toolCall)
				stream.Emit(AssistantMessageEvent{Type: EventToolCallStart, ContentIndex: index, Partial: cloneAssistantMessage(partial)})
			}
		case "content_block_delta":
			index := uintNumber(payload["index"])
			delta, _ := payload["delta"].(map[string]any)
			switch delta["type"] {
			case "text_delta":
				text := stringValue(delta["text"])
				if index >= 0 && index < len(partial.Content) && partial.Content[index].Type == ContentText {
					partial.Content[index].Text += text
				}
				stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: index, Delta: text, Partial: cloneAssistantMessage(partial)})
			case "thinking_delta":
				thinking := stringValue(delta["thinking"])
				if index >= 0 && index < len(partial.Content) && partial.Content[index].Type == ContentThinking {
					partial.Content[index].Thinking += thinking
				}
				stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, ContentIndex: index, Delta: thinking, Partial: cloneAssistantMessage(partial)})
			case "signature_delta":
				signature := stringValue(delta["signature"])
				if index >= 0 && index < len(partial.Content) && partial.Content[index].Type == ContentThinking {
					partial.Content[index].ThinkingSignature += signature
				}
			case "input_json_delta":
				fragment := stringValue(delta["partial_json"])
				toolJSON[index] += fragment
				emitAnthropicToolArgDelta(stream, &partial, index, fragment, toolJSON[index])
			}
		case "content_block_stop":
			index := uintNumber(payload["index"])
			applyAnthropicToolArgs(&partial, index, toolJSON)
			if index >= 0 && index < len(partial.Content) {
				switch block := partial.Content[index]; block.Type {
				case ContentText:
					stream.Emit(AssistantMessageEvent{Type: EventTextEnd, ContentIndex: index, Content: block.Text, Partial: cloneAssistantMessage(partial)})
				case ContentThinking:
					stream.Emit(AssistantMessageEvent{Type: EventThinkingEnd, ContentIndex: index, Content: block.Thinking, Partial: cloneAssistantMessage(partial)})
				case ContentToolCall:
					if block.ToolCall != nil {
						stream.Emit(AssistantMessageEvent{Type: EventToolCallEnd, ContentIndex: index, ToolCall: block.ToolCall, Partial: cloneAssistantMessage(partial)})
					}
				}
			}
		case "message_delta":
			if delta, ok := payload["delta"].(map[string]any); ok {
				stopReason = stringValue(delta["stop_reason"])
			}
			if usageMap, ok := payload["usage"].(map[string]any); ok {
				addAnthropicUsage(usage, usageMap)
			}
		case "message_stop":
			doneReason := DoneReasonStop
			if stopReason == "tool_use" {
				doneReason = DoneReasonToolCalls
			} else if stopReason == "max_tokens" {
				doneReason = DoneReasonLength
			}
			partial.Usage = usage
			message := partial
			if message.Timestamp == 0 {
				message.Timestamp = partial.Timestamp
			}
			if doneReason == DoneReasonToolCalls {
				message.StopReason = StopReasonToolCalls
			} else if doneReason == DoneReasonLength {
				message.StopReason = StopReasonMaxTokens
			} else {
				message.StopReason = StopReasonEndTurn
			}
			stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: doneReason, Message: &message})
			return false
		case "error":
			partial.StopReason = StopReasonError
			partial.ErrorMessage = anthropicErrorMessage(payload)
			stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &partial})
			return false
		}
		return true
	})
}

func ConsumeGenericSSE(reader io.Reader, handle func(sseEvent) bool) error {
	return consumeSSE(reader, handle)
}

func consumeSSE(reader io.Reader, handle func(sseEvent) bool) error {
	// Keep in one helper so future providers can share the OpenAI-compatible SSE parser.
	return consumeResponsesSSEWithHandler(reader, handle)
}

func consumeResponsesSSEWithHandler(reader io.Reader, handle func(sseEvent) bool) error {
	return ConsumeSSE(reader, func(event SSEEvent) bool {
		return handle(sseEvent{event: event.Event, data: event.Data})
	})
}

func applyAnthropicToolArgs(partial *AssistantMessage, index int, buffers map[int]string) {
	raw := buffers[index]
	delete(buffers, index)
	if raw == "" {
		return
	}
	if partial == nil || index < 0 || index >= len(partial.Content) || partial.Content[index].ToolCall == nil {
		return
	}
	args, ok := parsePartialJSONObject(raw)
	if !ok {
		args = map[string]any{}
	}
	partial.Content[index].ToolCall.Arguments = args
	upsertAnthropicToolCall(partial, *partial.Content[index].ToolCall)
}

func emitAnthropicToolArgDelta(stream *AssistantMessageEventStream, partial *AssistantMessage, index int, delta string, raw string) {
	if partial == nil {
		return
	}
	stream.Emit(AssistantMessageEvent{Type: EventToolCallDelta, ContentIndex: index, Delta: delta, Partial: cloneAssistantMessage(*partial)})
}

func setAnthropicContentBlockAt(message *AssistantMessage, index int, block ContentBlock) {
	if index < 0 {
		message.Content = append(message.Content, block)
		return
	}
	for len(message.Content) <= index {
		message.Content = append(message.Content, ContentBlock{Type: ContentText})
	}
	message.Content[index] = block
}

func upsertAnthropicToolCall(partial *AssistantMessage, call ToolCall) {
	for index := range partial.ToolCalls {
		if call.ID == "" || partial.ToolCalls[index].ID == call.ID {
			partial.ToolCalls[index] = call
			return
		}
	}
	partial.ToolCalls = append(partial.ToolCalls, call)
}

func addAnthropicUsage(usage *Usage, usageMap map[string]any) {
	usage.InputTokens += uintNumber(usageMap["input_tokens"])
	usage.OutputTokens += uintNumber(usageMap["output_tokens"])
	usage.CacheReadTokens += uintNumber(usageMap["cache_read_input_tokens"])
	usage.CacheWriteTokens += uintNumber(usageMap["cache_creation_input_tokens"])
	usage.TotalTokenCount = usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	usage.HasTotalTokens = true
}

func anthropicErrorMessage(payload map[string]any) string {
	if errMap, ok := payload["error"].(map[string]any); ok {
		if message := stringValue(errMap["message"]); message != "" {
			return message
		}
	}
	return "anthropic error"
}
