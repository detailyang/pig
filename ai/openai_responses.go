package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type OpenAIResponsesProvider struct {
	client *http.Client
}

type OpenAIResponsesOption func(*OpenAIResponsesProvider)

func WithHTTPClient(client *http.Client) OpenAIResponsesOption {
	return func(provider *OpenAIResponsesProvider) {
		if client != nil {
			provider.client = client
		}
	}
}

func NewOpenAIResponsesProvider(options ...OpenAIResponsesOption) *OpenAIResponsesProvider {
	provider := &OpenAIResponsesProvider{client: nil}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func (provider *OpenAIResponsesProvider) API() Api { return ApiOpenAIResponses }

func (provider *OpenAIResponsesProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	streamOptions := StreamOptionsFromSimple(options)
	if options.ReasoningLevel() != "" && options.ReasoningLevel() != ThinkingOff {
		if streamOptions.ProviderExtras == nil {
			streamOptions.ProviderExtras = map[string]any{}
		}
		streamOptions.ProviderExtras["reasoning_effort"] = string(options.ReasoningLevel())
	}
	return provider.Stream(ctx, model, request, streamOptions)
}

func (provider *OpenAIResponsesProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	apiKey := options.APIKey
	if apiKey == "" {
		if value, ok := GetEnvAPIKey(string(model.Provider)); ok {
			apiKey = value
		}
	}
	if apiKey == "" && model.Provider == Provider("openai") {
		if value, ok := GetEnvAPIKey("openai"); ok {
			apiKey = value
		}
	}
	if apiKey == "" {
		return openAIResponsesProviderErrorStream(model, missingOpenAICompatibleAPIKeyMessage(model))
	}
	client, err := effectiveHTTPClient(provider.client, options)
	if err != nil {
		return openAIResponsesProviderErrorStream(model, "http client: "+err.Error())
	}
	body, err := BuildOpenAIResponsesRequestBody(model, request, options)
	if err != nil {
		return openAIResponsesProviderErrorStream(model, err.Error())
	}
	payload, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return openAIResponsesProviderErrorStream(model, err.Error())
	}

	baseURL, err := ResolveProviderBaseURL(model, options)
	if err != nil {
		return openAIResponsesProviderErrorStream(model, err.Error())
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, BuildResponsesURL(resolveResponsesBaseURL(baseURL)), bytes.NewReader(payload))
	if err != nil {
		return openAIResponsesProviderErrorStream(model, "http error: "+err.Error())
	}
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if options.SessionID != "" {
		if openAIResponsesSendSessionIDHeader(model) {
			httpRequest.Header.Set("session_id", options.SessionID)
		}
		httpRequest.Header.Set("x-client-request-id", options.SessionID)
	}
	applyModelAndOptionHeaders(httpRequest, model, options)

	response, err := sendWithRetry(client, httpRequest, payload, options)
	if err != nil {
		if stream, ok := AbortedStreamIfCanceled(model, err); ok {
			return stream
		}
		return openAIResponsesProviderErrorStream(model, "http error: "+err.Error())
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return openAIResponsesProviderErrorStream(model, fmt.Sprintf("HTTP %s: %s", response.Status, strings.TrimSpace(string(data))))
	}

	stream := NewAssistantMessageEventStream().MarkLive()
	go func() {
		defer response.Body.Close()
		if err := ConsumeResponsesSSE(response.Body, stream, model); err != nil {
			if aborted, ok := AbortedStreamIfCanceled(model, err); ok {
				for _, event := range aborted.SnapshotEvents() {
					stream.Emit(event)
				}
				return
			}
			stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Error: err.Error()})
			stream.Close(DoneReasonStop)
		}
	}()
	return stream
}

func openAIResponsesProviderErrorStream(model Model, message string) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	assistantMessage := AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonError, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &assistantMessage})
	return stream
}

func resolveResponsesBaseURL(baseURL string) string {
	if baseURL != "" {
		return baseURL
	}
	return "https://api.openai.com"
}

func BuildResponsesURL(base string) string {
	trimmed := strings.TrimRight(base, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.Contains(trimmed, "/v1/") {
		return trimmed + "/responses"
	}
	return trimmed + "/v1/responses"
}

func BuildResponsesUrl(base string) string { return BuildResponsesURL(base) }

func BuildOpenAIResponsesRequestBody(model Model, request Context, options StreamOptions) (map[string]any, error) {
	body := map[string]any{
		"model":  model.ID,
		"input":  convertContextForOpenAIResponses(request, openAIResponsesReplayReasoning(model)),
		"stream": true,
		"store":  false,
	}
	if options.MaxTokens > 0 {
		body["max_output_tokens"] = options.MaxTokens
	}
	if options.Temperature != nil {
		body["temperature"] = *options.Temperature
	}
	if options.SessionID != "" && options.CacheRetention != CacheNone {
		body["prompt_cache_key"] = options.SessionID
	}
	if options.CacheRetention == CacheLong && openAIResponsesSupportsLongCacheRetention(model) {
		body["prompt_cache_retention"] = "24h"
	}
	if serviceTier, ok := options.ProviderExtras["service_tier"]; ok {
		body["service_tier"] = serviceTier
	}
	if request.HasTools || request.Tools != nil {
		body["tools"] = SerializeOpenAIResponsesTools(request.Tools)
	}
	if effort, summary, ok := openAIResponsesReasoningOptions(model, options); ok {
		body["reasoning"] = map[string]any{"effort": effort, "summary": summary}
		body["include"] = []string{"reasoning.encrypted_content"}
	}
	return body, nil
}

type Compat struct {
	SendSessionIDHeader        bool
	SupportsLongCacheRetention bool
	ReplayReasoningContent     bool
}

func ResolveCompat(model Model) Compat {
	return Compat{SendSessionIDHeader: openAIResponsesSendSessionIDHeader(model), SupportsLongCacheRetention: openAIResponsesSupportsLongCacheRetention(model), ReplayReasoningContent: openAIResponsesReplayReasoning(model)}
}

func BuildRequestBody(model Model, request Context, options StreamOptions, compat Compat) (map[string]any, error) {
	if model.Compat == nil {
		model.Compat = map[string]any{}
	}
	model.Compat["sendSessionIdHeader"] = compat.SendSessionIDHeader
	model.Compat["supportsLongCacheRetention"] = compat.SupportsLongCacheRetention
	model.Compat["requiresReasoningContentOnAssistantMessages"] = compat.ReplayReasoningContent
	return BuildOpenAIResponsesRequestBody(model, request, options)
}

func openAIResponsesReplayReasoning(model Model) bool {
	value, _ := model.Compat["requiresReasoningContentOnAssistantMessages"].(bool)
	return value
}

func openAIResponsesSendSessionIDHeader(model Model) bool {
	value, ok := model.Compat["sendSessionIdHeader"].(bool)
	return !ok || value
}

func openAIResponsesSupportsLongCacheRetention(model Model) bool {
	value, ok := model.Compat["supportsLongCacheRetention"].(bool)
	return !ok || value
}

func openAIResponsesReasoningOptions(model Model, options StreamOptions) (string, string, bool) {
	if !model.Reasoning {
		return "", "", false
	}
	summary := "auto"
	if value, ok := options.ProviderExtras["reasoning_summary"].(string); ok {
		summary = value
	}
	if value, ok := options.ProviderExtras["reasoning_effort"].(string); ok {
		return openAIResponsesReasoningEffort(model, ThinkingLevel(value)), summary, true
	}
	return "", "", false
}

func openAIResponsesReasoningEffort(model Model, level ThinkingLevel) string {
	mappedLevel := level
	switch level {
	case ThinkingMinimal, ThinkingLow, ThinkingMedium, ThinkingHigh, ThinkingXHigh:
	default:
		mappedLevel = ThinkingMedium
	}
	if mapped := model.ThinkingLevels[string(mappedLevel)]; mapped != nil {
		return *mapped
	}
	return string(level)
}

func SerializeOpenAIResponsesTools(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		})
	}
	return out
}

func SerializeTools(tools []Tool) []map[string]any {
	return SerializeOpenAIResponsesTools(tools)
}

func EmptyPartial(model Model) AssistantMessage {
	return AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, Timestamp: time.Now().UnixMilli()}
}

func ConvertMessagesForOpenAIResponses(messages []Message) []map[string]any {
	return convertMessagesForOpenAIResponses(messages, false)
}

func convertContextForOpenAIResponses(request Context, replayReasoning bool) []map[string]any {
	input := make([]map[string]any, 0, len(request.Messages)+1)
	if request.HasSystemPrompt || request.SystemPrompt != "" {
		input = append(input, map[string]any{"role": "system", "content": []map[string]any{{"type": "input_text", "text": request.SystemPrompt}}})
	}
	input = append(input, convertMessagesForOpenAIResponses(request.Messages, replayReasoning)...)
	return input
}

func convertMessagesForOpenAIResponses(messages []Message, replayReasoning bool) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case RoleUser:
			out = append(out, map[string]any{"role": "user", "content": userInputContent(message.Content)})
		case RoleAssistant:
			if replayReasoning {
				out = append(out, reasoningInputItems(message.Content)...)
			}
			if content := assistantOutputContent(message.Content); len(content) > 0 {
				out = append(out, map[string]any{"role": "assistant", "content": content})
			}
			for _, call := range openAIResponsesToolCallBlocks(message.Content) {
				argumentsMap := call.Arguments
				if argumentsMap == nil {
					argumentsMap = map[string]any{}
				}
				arguments, _ := marshalJSONNoHTMLEscape(argumentsMap)
				out = append(out, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": string(arguments)})
			}
		case RoleTool:
			out = append(out, map[string]any{"type": "function_call_output", "call_id": message.ToolCallID, "output": blocksText(message.Content)})
		}
	}
	return out
}

func openAIResponsesToolCallBlocks(blocks []ContentBlock) []ToolCall {
	calls := []ToolCall{}
	for _, block := range blocks {
		if block.Type == ContentToolCall && block.ToolCall != nil {
			calls = append(calls, *block.ToolCall)
		}
	}
	return calls
}

func reasoningInputItems(blocks []ContentBlock) []map[string]any {
	items := []map[string]any{}
	for _, block := range blocks {
		if block.Type == ContentThinking && block.Thinking != "" {
			items = append(items, map[string]any{"type": "reasoning", "summary": []map[string]any{{"type": "summary_text", "text": block.Thinking}}})
		}
	}
	return items
}

func userInputContent(blocks []ContentBlock) []map[string]any {
	content := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentImage:
			content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + block.MimeType + ";base64," + block.Data})
		case ContentText:
			content = append(content, map[string]any{"type": "input_text", "text": block.Text})
		}
	}
	return content
}

func assistantOutputContent(blocks []ContentBlock) []map[string]any {
	content := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == ContentText {
			content = append(content, map[string]any{"type": "output_text", "text": block.Text})
		}
	}
	return content
}

func blocksText(blocks []ContentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == ContentText {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

type sseEvent struct {
	event string
	data  string
}

func ConsumeResponsesSSE(reader io.Reader, stream *AssistantMessageEventStream, model Model) error {
	partial := responsesEmptyPartial(model)
	openAIResponsesSetStreamPartial(stream, partial)
	stream.Emit(AssistantMessageEvent{Type: EventStart, Partial: partial})
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	current := sseEvent{}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if current.data != "" || current.event != "" {
				if !HandleOpenAIResponsesEvent(current, stream) {
					return nil
				}
				current = sseEvent{}
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			current.event = value
		case "data":
			if current.data != "" {
				current.data += "\n"
			}
			current.data += value
		}
	}
	if err := scanner.Err(); err != nil {
		if IsCanceledError(err) {
			PushAborted(stream, model)
			return nil
		}
		message := *responsesEmptyPartial(model)
		message.StopReason = StopReasonError
		message.ErrorMessage = "sse: " + err.Error()
		stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonError, Message: &message})
		return nil
	}
	if current.data != "" || current.event != "" {
		HandleOpenAIResponsesEvent(current, stream)
	}
	if _, ok := stream.Snapshot(); !ok {
		message := openAIResponsesBasePartial(stream)
		message.StopReason = StopReasonEndTurn
		stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonStop, Message: &message})
	}
	return nil
}

func ConsumeResponsesSse(reader io.Reader, stream *AssistantMessageEventStream, model Model) error {
	return ConsumeResponsesSSE(reader, stream, model)
}

func responsesEmptyPartial(model Model) *AssistantMessage {
	return &AssistantMessage{
		Role:       AssistantRoleAssistant,
		API:        model.API,
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      &Usage{},
		StopReason: StopReasonEndTurn,
		Timestamp:  time.Now().UnixMilli(),
	}
}

func HandleOpenAIResponsesEvent(event sseEvent, stream *AssistantMessageEventStream) bool {
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.data), &payload); err != nil {
		return true
	}
	kind := event.event
	if kind == "" {
		if value, ok := payload["type"].(string); ok {
			kind = value
		}
	}
	switch kind {
	case "response.created", "response.in_progress":
		response, _ := payload["response"].(map[string]any)
		if id := stringValue(response["id"]); id != "" {
			partial := openAIResponsesStreamPartial(stream)
			partial.ResponseID = id
		}
	case "response.output_text.delta":
		index := lastOpenAIResponsesContentIndex(stream, ContentText, false)
		if index < 0 {
			index = nextOpenAIResponsesContentIndex(stream)
			stream.Emit(AssistantMessageEvent{Type: EventTextStart, ContentIndex: index, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentText})})
		}
		delta := stringValue(payload["delta"])
		text := openAIResponsesContentText(stream, index, ContentText) + delta
		stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: index, Delta: delta, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentText, Text: text})})
	case "response.output_text.done":
		if index := lastOpenAIResponsesContentIndex(stream, ContentText, false); index >= 0 {
			partialText := openAIResponsesContentText(stream, index, ContentText)
			text, ok := payload["text"].(string)
			if !ok {
				text = partialText
			}
			stream.Emit(AssistantMessageEvent{Type: EventTextEnd, ContentIndex: index, Content: text, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentText, Text: partialText})})
		}
	case "response.reasoning_summary_text.delta":
		index := lastOpenAIResponsesContentIndex(stream, ContentThinking, true)
		if index < 0 {
			index = nextOpenAIResponsesContentIndex(stream)
			stream.Emit(AssistantMessageEvent{Type: EventThinkingStart, ContentIndex: index, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentThinking})})
		}
		delta := stringValue(payload["delta"])
		thinking := openAIResponsesContentText(stream, index, ContentThinking) + delta
		stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, ContentIndex: index, Delta: delta, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentThinking, Thinking: thinking})})
	case "response.reasoning_summary_text.done":
		if index := lastOpenAIResponsesContentIndex(stream, ContentThinking, true); index >= 0 {
			thinking := stringValue(payload["text"])
			partialThinking := openAIResponsesContentText(stream, index, ContentThinking)
			stream.Emit(AssistantMessageEvent{Type: EventThinkingEnd, ContentIndex: index, Content: thinking, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentThinking, Thinking: partialThinking})})
		}
	case "response.output_item.added":
		item, _ := payload["item"].(map[string]any)
		switch item["type"] {
		case "reasoning":
			index := nextOpenAIResponsesContentIndex(stream)
			stream.Emit(AssistantMessageEvent{Type: EventThinkingStart, ContentIndex: index, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentThinking})})
		case "function_call":
			index := nextOpenAIResponsesContentIndex(stream)
			call := &ToolCall{ID: stringValue(item["call_id"]), Name: stringValue(item["name"]), Arguments: map[string]any{}}
			openAIResponsesUpsertPartialToolCall(stream, *call)
			stream.Emit(AssistantMessageEvent{Type: EventToolCallStart, ContentIndex: index, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentToolCall, ToolCall: call})})
		}
	case "response.function_call_arguments.delta":
		appendToolArguments(stream, stringValue(payload["delta"]))
	case "response.function_call_arguments.done":
		applyToolArguments(stream, stringValue(payload["arguments"]))
	case "response.completed":
		message := openAIResponsesBasePartial(stream)
		if usage := usageFromResponse(payload); usage != nil {
			addOpenAIResponsesUsage(&message, usage)
		}
		if completedHasToolCall(payload) {
			message.StopReason = StopReasonToolCalls
			stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonToolCalls, Message: &message})
		} else if responseStatus(payload) == "incomplete" {
			message.StopReason = StopReasonMaxTokens
			stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonLength, Message: &message})
		} else {
			message.StopReason = StopReasonEndTurn
			stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonStop, Message: &message})
		}
		return false
	case "response.failed", "response.error", "error":
		message := openAIResponsesBasePartial(stream)
		message.StopReason = StopReasonError
		message.ErrorMessage = responseErrorMessage(payload)
		stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonError, Message: &message})
		return false
	}
	return true
}

func lastOpenAIResponsesContentIndex(stream *AssistantMessageEventStream, blockType ContentType, options ...bool) int {
	searchAll := true
	if len(options) > 0 {
		searchAll = options[0]
	}
	events := stream.SnapshotEvents()
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		switch blockType {
		case ContentText:
			if event.Type == EventTextStart || event.Type == EventTextDelta || event.Type == EventTextEnd {
				return event.ContentIndex
			}
			if !searchAll && (event.Type == EventThinkingStart || event.Type == EventThinkingDelta || event.Type == EventThinkingEnd || event.Type == EventToolCallStart || event.Type == EventToolCallDelta || event.Type == EventToolCallEnd || event.Type == EventToolCall) {
				return -1
			}
		case ContentThinking:
			if event.Type == EventThinkingStart || event.Type == EventThinkingDelta || event.Type == EventThinkingEnd {
				return event.ContentIndex
			}
		}
	}
	return -1
}

func openAIResponsesStreamPartial(stream *AssistantMessageEventStream) *AssistantMessage {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.partial == nil {
		stream.partial = &AssistantMessage{}
	}
	return stream.partial
}

func openAIResponsesSetStreamPartial(stream *AssistantMessageEventStream, partial *AssistantMessage) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if partial == nil {
		stream.partial = nil
		return
	}
	copy := *partial
	stream.partial = &copy
}

func openAIResponsesBasePartial(stream *AssistantMessageEventStream) AssistantMessage {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.partial == nil {
		return AssistantMessage{}
	}
	return *stream.partial
}

func nextOpenAIResponsesContentIndex(stream *AssistantMessageEventStream) int {
	next := 0
	for _, event := range stream.SnapshotEvents() {
		switch event.Type {
		case EventTextStart, EventTextDelta, EventTextEnd, EventThinkingStart, EventThinkingDelta, EventThinkingEnd, EventToolCallStart, EventToolCallEnd, EventToolCall:
			if event.ContentIndex >= next {
				next = event.ContentIndex + 1
			}
		case EventContentBlock, EventContentUpdate:
			if event.ContentBlock != nil {
				next++
			}
		}
	}
	return next
}

func openAIResponsesContentText(stream *AssistantMessageEventStream, contentIndex int, blockType ContentType) string {
	message, _ := stream.Snapshot()
	if contentIndex < 0 || contentIndex >= len(message.Content) || message.Content[contentIndex].Type != blockType {
		return ""
	}
	if blockType == ContentThinking {
		return message.Content[contentIndex].Thinking
	}
	return message.Content[contentIndex].Text
}

func appendToolArguments(stream *AssistantMessageEventStream, delta string) {
	content := openAIResponsesBasePartial(stream).Content
	for index := len(content) - 1; index >= 0; index-- {
		call := content[index].ToolCall
		if call == nil {
			continue
		}
		stream.Emit(AssistantMessageEvent{Type: EventToolCallDelta, ContentIndex: index, Delta: delta, Partial: openAIResponsesBasePartialCopy(stream)})
		return
	}
}

func responseStatus(payload map[string]any) string {
	response, _ := payload["response"].(map[string]any)
	return stringValue(response["status"])
}

func applyToolArguments(stream *AssistantMessageEventStream, raw string) {
	content := openAIResponsesBasePartial(stream).Content
	for index := len(content) - 1; index >= 0; index-- {
		call := content[index].ToolCall
		if call == nil {
			continue
		}
		updated := *call
		if args, ok := parsePartialJSONObject(raw); ok {
			updated.Arguments = args
		}
		openAIResponsesUpsertPartialToolCall(stream, updated)
		stream.Emit(AssistantMessageEvent{Type: EventToolCallEnd, ContentIndex: index, ToolCall: &updated, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentToolCall, ToolCall: &updated})})
		return
	}
}

func openAIResponsesBasePartialCopy(stream *AssistantMessageEventStream) *AssistantMessage {
	message := openAIResponsesBasePartial(stream)
	message.Content = append([]ContentBlock(nil), message.Content...)
	return &message
}

func openAIResponsesPartialContent(stream *AssistantMessageEventStream, index int, block ContentBlock) *AssistantMessage {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.partial == nil {
		stream.partial = &AssistantMessage{}
	}
	for len(stream.partial.Content) <= index {
		stream.partial.Content = append(stream.partial.Content, ContentBlock{})
	}
	stream.partial.Content[index] = block
	copy := *stream.partial
	copy.Content = append([]ContentBlock(nil), stream.partial.Content...)
	return &copy
}

func openAIResponsesUpsertPartialToolCall(stream *AssistantMessageEventStream, call ToolCall) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.partial == nil {
		stream.partial = &AssistantMessage{}
	}
	for index := range stream.partial.ToolCalls {
		if call.ID == "" || stream.partial.ToolCalls[index].ID == call.ID {
			stream.partial.ToolCalls[index] = call
			return
		}
	}
	stream.partial.ToolCalls = append(stream.partial.ToolCalls, call)
}

func usageFromResponse(payload map[string]any) *Usage {
	response, _ := payload["response"].(map[string]any)
	usageMap, _ := response["usage"].(map[string]any)
	if usageMap == nil {
		return nil
	}
	usage := &Usage{InputTokens: uintNumber(usageMap["input_tokens"]), OutputTokens: uintNumber(usageMap["output_tokens"])}
	if details, ok := usageMap["input_tokens_details"].(map[string]any); ok {
		usage.CacheReadTokens = uintNumber(details["cached_tokens"])
		usage.CacheWriteTokens = uintNumber(details["cache_write_tokens"])
	}
	usage.TotalTokenCount = usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	usage.HasTotalTokens = true
	return usage
}

func addOpenAIResponsesUsage(message *AssistantMessage, usage *Usage) {
	if message.Usage == nil {
		copy := *usage
		message.Usage = &copy
		return
	}
	message.Usage.InputTokens += usage.InputTokens
	message.Usage.OutputTokens += usage.OutputTokens
	message.Usage.CacheReadTokens += usage.CacheReadTokens
	message.Usage.CacheWriteTokens += usage.CacheWriteTokens
	message.Usage.TotalTokenCount = message.Usage.InputTokens + message.Usage.OutputTokens + message.Usage.CacheReadTokens + message.Usage.CacheWriteTokens
	message.Usage.HasTotalTokens = true
}

func completedHasToolCall(payload map[string]any) bool {
	response, _ := payload["response"].(map[string]any)
	output, _ := response["output"].([]any)
	for _, item := range output {
		m, _ := item.(map[string]any)
		if m["type"] == "function_call" {
			return true
		}
	}
	return false
}

func responseErrorMessage(payload map[string]any) string {
	if errMap, ok := payload["error"].(map[string]any); ok {
		if message := stringValue(errMap["message"]); message != "" {
			return message
		}
	}
	if response, ok := payload["response"].(map[string]any); ok {
		if errMap, ok := response["error"].(map[string]any); ok {
			if message := stringValue(errMap["message"]); message != "" {
				return message
			}
		}
	}
	return "openai-responses error"
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func intNumber(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		parsed, err := strconv.ParseInt(n.String(), 10, 0)
		if err == nil {
			return int(parsed)
		}
	default:
		return 0
	}
	return 0
}
