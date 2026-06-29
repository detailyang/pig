package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type GoogleProvider struct {
	client *http.Client
}

type GoogleOption func(*GoogleProvider)

func WithGoogleHTTPClient(client *http.Client) GoogleOption {
	return func(provider *GoogleProvider) {
		if client != nil {
			provider.client = client
		}
	}
}

func NewGoogleProvider(options ...GoogleOption) *GoogleProvider {
	provider := &GoogleProvider{client: nil}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func (provider *GoogleProvider) API() Api { return ApiGoogleGenerativeAI }

func (provider *GoogleProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	return provider.Stream(ctx, model, request, GoogleStreamOptionsFromSimple(options))
}

func GoogleStreamOptionsFromSimple(options SimpleStreamOptions) StreamOptions {
	streamOptions := StreamOptionsFromSimple(options)
	if options.ReasoningLevel() != "" && options.ReasoningLevel() != ThinkingOff {
		if streamOptions.ProviderExtras == nil {
			streamOptions.ProviderExtras = map[string]any{}
		}
		if budget, ok := options.ThinkingBudgets.BudgetFor(options.ReasoningLevel()); ok {
			streamOptions.ProviderExtras["thinking_budget"] = budget
		} else {
			streamOptions.ProviderExtras["thinking_budget"] = googleDefaultThinkingBudget()
		}
	}
	return streamOptions
}

func TranslateSimple(options SimpleStreamOptions) StreamOptions {
	return GoogleStreamOptionsFromSimple(options)
}

func (provider *GoogleProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	apiKey := options.APIKey
	if apiKey == "" {
		if value, ok := GetEnvAPIKey("google"); ok {
			apiKey = value
		}
	}
	if apiKey == "" {
		return googleProviderErrorStream(model, "GOOGLE_API_KEY / GEMINI_API_KEY is not set")
	}
	client, err := effectiveHTTPClient(provider.client, options)
	if err != nil {
		return googleProviderErrorStream(model, "http client: "+err.Error())
	}
	body, err := BuildGoogleRequestBody(request, options)
	if err != nil {
		return googleProviderErrorStream(model, err.Error())
	}
	data, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return googleProviderErrorStream(model, err.Error())
	}
	baseURL := model.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BuildGoogleGenerativeURL(baseURL, model.ID), bytes.NewReader(data))
	if err != nil {
		return googleProviderErrorStream(model, "http error: "+err.Error())
	}
	req.Header.Set("x-goog-api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	applyModelAndOptionHeaders(req, model, options)
	resp, err := sendWithRetry(client, req, data, options)
	if err != nil {
		if stream, ok := AbortedStreamIfCanceled(model, err); ok {
			return stream
		}
		return googleProviderErrorStream(model, "http error: "+err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return googleProviderErrorStream(model, fmt.Sprintf("HTTP %s: %s", resp.Status, strings.TrimSpace(string(body))))
	}
	stream := NewAssistantMessageEventStream()
	if err := ConsumeGoogleSSEForModel(resp.Body, stream, model); err != nil {
		stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Error: err.Error()})
		stream.Close(DoneReasonStop)
	}
	return stream
}

func googleProviderErrorStream(model Model, message string) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	assistantMessage := AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonError, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &assistantMessage})
	return stream
}

func BuildGoogleGenerativeURL(base, modelID string) string {
	return strings.TrimRight(base, "/") + "/v1beta/models/" + modelID + ":streamGenerateContent?alt=sse"
}

func BuildGoogleRequestBody(request Context, options StreamOptions) (map[string]any, error) {
	body := map[string]any{"contents": ConvertMessagesForGoogle(request.Messages)}
	if system, ok := googleSystemInstruction(request); ok {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": system}}}
	}
	generationConfig := map[string]any{}
	if options.MaxTokens > 0 {
		generationConfig["maxOutputTokens"] = options.MaxTokens
	}
	if options.Temperature != nil {
		generationConfig["temperature"] = *options.Temperature
	}
	if budget, ok := googleThinkingBudgetOption(options); ok {
		generationConfig["thinkingConfig"] = map[string]any{"thinkingBudget": budget, "includeThoughts": true}
	}
	if len(generationConfig) > 0 {
		body["generationConfig"] = generationConfig
	}
	if len(request.Tools) > 0 {
		body["tools"] = []map[string]any{{"functionDeclarations": ConvertToolsForGoogle(request.Tools)}}
		if config, ok := googleToolChoiceConfig(options); ok {
			body["toolConfig"] = map[string]any{"functionCallingConfig": config}
		}
	}
	return body, nil
}

func googleToolChoiceConfig(options StreamOptions) (map[string]any, bool) {
	choice := options.ProviderExtras["tool_choice"]
	switch value := choice.(type) {
	case string:
		if mode, ok := googleToolChoiceMode(value); ok {
			return map[string]any{"mode": mode}, true
		}
	case []string:
		if len(value) > 0 {
			return map[string]any{"mode": "ANY", "allowedFunctionNames": append([]string(nil), value...)}, true
		}
	case []any:
		allowed := []string{}
		for _, item := range value {
			name, ok := item.(string)
			if !ok {
				return nil, false
			}
			allowed = append(allowed, name)
		}
		if len(allowed) > 0 {
			return map[string]any{"mode": "ANY", "allowedFunctionNames": allowed}, true
		}
	}
	return nil, false
}

func googleToolChoiceMode(choice string) (string, bool) {
	switch choice {
	case "required", "any":
		return "ANY", true
	case "auto":
		return "AUTO", true
	case "none":
		return "NONE", true
	default:
		return "", false
	}
}

func googleThinkingBudgetOption(options StreamOptions) (any, bool) {
	if budget, ok := options.ProviderExtras["thinking_budget"]; ok {
		return budget, true
	}
	return nil, false
}

func googleDefaultThinkingBudget() int {
	return 8192
}

func googleSystemInstruction(request Context) (string, bool) {
	return request.SystemPrompt, request.HasSystemPrompt || request.SystemPrompt != ""
}

func ConvertMessagesForGoogle(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case RoleSystem:
			continue
		case RoleUser:
			parts := googleUserParts(message.Content)
			if len(parts) > 0 {
				out = append(out, map[string]any{"role": "user", "parts": parts})
			}
		case RoleAssistant:
			parts := googleAssistantParts(message.Content)
			if len(parts) > 0 {
				out = append(out, map[string]any{"role": "model", "parts": parts})
			}
		case RoleTool:
			out = appendGoogleToolResult(out, message)
		}
	}
	return out
}

func toolCallsNotInContent(calls []ToolCall, blocks []ContentBlock) []ToolCall {
	seen := map[string]bool{}
	for _, block := range blocks {
		if block.Type == ContentToolCall && block.ToolCall != nil && block.ToolCall.ID != "" {
			seen[block.ToolCall.ID] = true
		}
	}
	out := make([]ToolCall, 0, len(calls))
	for _, call := range deduplicateToolCalls(calls) {
		if call.ID != "" && seen[call.ID] {
			continue
		}
		out = append(out, call)
	}
	return out
}

func appendGoogleToolResult(out []map[string]any, message Message) []map[string]any {
	part := map[string]any{"functionResponse": map[string]any{"name": message.ToolName, "response": map[string]any{"result": blocksText(message.Content)}}}
	if len(out) > 0 && out[len(out)-1]["role"] == "user" && googleHasFunctionResponse(out[len(out)-1]) {
		out[len(out)-1]["parts"] = append(out[len(out)-1]["parts"].([]map[string]any), part)
		return out
	}
	return append(out, map[string]any{"role": "user", "parts": []map[string]any{part}})
}

func googleHasFunctionResponse(message map[string]any) bool {
	parts, _ := message["parts"].([]map[string]any)
	for _, part := range parts {
		if _, ok := part["functionResponse"]; ok {
			return true
		}
	}
	return false
}

func googleUserParts(blocks []ContentBlock) []map[string]any {
	parts := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentText:
			parts = append(parts, map[string]any{"text": block.Text})
		case ContentImage:
			parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": block.MimeType, "data": block.Data}})
		}
	}
	return parts
}

func googleAssistantParts(blocks []ContentBlock) []map[string]any {
	parts := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentText:
			part := map[string]any{"text": block.Text}
			if block.TextSignature != "" {
				part["thoughtSignature"] = block.TextSignature
			}
			parts = append(parts, part)
		case ContentThinking:
			part := map[string]any{"text": block.Thinking, "thought": true}
			if block.ThinkingSignature != "" {
				part["thoughtSignature"] = block.ThinkingSignature
			}
			parts = append(parts, part)
		case ContentToolCall:
			if block.ToolCall != nil {
				parts = append(parts, googleToolCallParts([]ToolCall{*block.ToolCall})...)
			}
		}
	}
	return parts
}

func googleToolCallParts(calls []ToolCall) []map[string]any {
	parts := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		functionCall := map[string]any{"name": call.Name, "args": call.Arguments}
		part := map[string]any{"functionCall": functionCall}
		if call.ThoughtSignature != "" {
			part["thoughtSignature"] = call.ThoughtSignature
		}
		parts = append(parts, part)
	}
	return parts
}

func ConvertToolsForGoogle(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters})
	}
	return out
}

func ConsumeGoogleSSE(reader io.Reader, stream *AssistantMessageEventStream) error {
	return ConsumeGoogleSSEForModel(reader, stream, Model{})
}

func ConsumeGoogleSSEForModel(reader io.Reader, stream *AssistantMessageEventStream, model Model) error {
	partial := responsesEmptyPartial(model)
	openAIResponsesSetStreamPartial(stream, partial)
	stream.Emit(AssistantMessageEvent{Type: EventStart, Partial: cloneAssistantMessage(*partial)})
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	finishReason := ""
	responseID := ""
	state := googleStreamState{}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if !HandleGoogleChunkWithState([]byte(data), stream, &finishReason, &responseID, &state) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		if IsCanceledError(err) {
			PushAborted(stream, model)
			return nil
		}
		message := openAIResponsesBasePartial(stream)
		message.StopReason = StopReasonError
		message.ErrorMessage = "sse: " + err.Error()
		stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &message})
		return nil
	}
	if _, ok := stream.Snapshot(); !ok {
		partial := openAIResponsesBasePartial(stream)
		closeGoogleOpenBlock(stream, &partial, state.OpenBlock)
		openAIResponsesSetStreamPartial(stream, &partial)
		message := partial
		switch finishReason {
		case "tool_calls":
			message.StopReason = StopReasonToolCalls
			stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonToolCalls, Message: &message})
		case "length":
			message.StopReason = StopReasonMaxTokens
			stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonLength, Message: &message})
		case "error":
			message.StopReason = StopReasonError
			message.ErrorMessage = "google error"
			stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &message})
		default:
			message.StopReason = StopReasonEndTurn
			stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonStop, Message: &message})
		}
	}
	return nil
}

func ConsumeGeminiSSE(reader io.Reader, stream *AssistantMessageEventStream, model Model) error {
	return ConsumeGoogleSSEForModel(reader, stream, model)
}

func ConsumeGeminiSse(reader io.Reader, stream *AssistantMessageEventStream, model Model) error {
	return ConsumeGeminiSSE(reader, stream, model)
}

func HandleGoogleChunk(data []byte, stream *AssistantMessageEventStream, finishReason *string, responseID *string) bool {
	state := googleStreamState{}
	return HandleGoogleChunkWithState(data, stream, finishReason, responseID, &state)
}

type googleStreamState struct {
	OpenBlock   googleOpenBlock
	ToolCounter int
}

type googleOpenBlock int

const (
	googleOpenBlockNone googleOpenBlock = iota
	googleOpenBlockText
	googleOpenBlockThinking
)

func HandleGoogleChunkWithState(data []byte, stream *AssistantMessageEventStream, finishReason *string, responseID *string, state *googleStreamState) bool {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return true
	}
	if responseID != nil && *responseID == "" {
		if id := stringValue(payload["responseId"]); id != "" {
			*responseID = id
			googleSetPartialResponseID(stream, id)
		}
	}
	usage := usageFromGoogle(payload)
	candidates, _ := payload["candidates"].([]any)
	if len(candidates) == 0 {
		if usage != nil {
			openAIChatSetStreamPartialUsage(stream, usage)
		}
		return true
	}
	candidate, _ := candidates[0].(map[string]any)
	content, _ := candidate["content"].(map[string]any)
	parts, _ := content["parts"].([]any)
	for _, partAny := range parts {
		part, _ := partAny.(map[string]any)
		if text, ok := part["text"].(string); ok {
			partial := openAIResponsesBasePartial(stream)
			if boolValue(part["thought"]) {
				index := ensureGoogleOpenBlock(stream, &partial, &state.OpenBlock, googleOpenBlockThinking)
				block := partial.Content[index]
				block.Thinking += text
				block.ThinkingSignature = RetainGoogleThoughtSignature(block.ThinkingSignature, stringValue(part["thoughtSignature"]))
				stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, ContentIndex: index, Delta: text, Partial: openAIResponsesPartialContent(stream, index, block), ContentBlock: &ContentBlock{ThinkingSignature: block.ThinkingSignature}})
			} else {
				index := ensureGoogleOpenBlock(stream, &partial, &state.OpenBlock, googleOpenBlockText)
				block := partial.Content[index]
				block.Text += text
				stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: index, Delta: text, Partial: openAIResponsesPartialContent(stream, index, block)})
			}
		}
		if functionCall, ok := part["functionCall"].(map[string]any); ok {
			partial := openAIResponsesBasePartial(stream)
			closeGoogleOpenBlock(stream, &partial, state.OpenBlock)
			state.OpenBlock = googleOpenBlockNone
			args, ok := functionCall["args"].(map[string]any)
			if !ok {
				args = map[string]any{}
			}
			name := stringValue(functionCall["name"])
			id := stringValue(functionCall["id"])
			if id == "" {
				state.ToolCounter++
				id = fmt.Sprintf("%s_%d_%d", name, time.Now().UnixMilli(), state.ToolCounter)
			}
			toolCall := ToolCall{ID: id, Name: name, Arguments: args, ThoughtSignature: stringValue(part["thoughtSignature"])}
			partial.Content = append(partial.Content, ContentBlock{Type: ContentToolCall, ToolCall: &toolCall})
			partial.ToolCalls = append(partial.ToolCalls, toolCall)
			contentIndex := len(partial.Content) - 1
			openAIResponsesSetStreamPartial(stream, &partial)
			partialSnapshot := cloneAssistantMessage(partial)
			stream.Emit(AssistantMessageEvent{Type: EventToolCallStart, ContentIndex: contentIndex, Partial: partialSnapshot})
			stream.Emit(AssistantMessageEvent{Type: EventToolCallDelta, ContentIndex: contentIndex, Delta: toolCallArgumentsJSON(&toolCall), Partial: partialSnapshot})
			stream.Emit(AssistantMessageEvent{Type: EventToolCallEnd, ContentIndex: contentIndex, ToolCall: &toolCall, Partial: partialSnapshot})
		}
	}
	if reason := stringValue(candidate["finishReason"]); reason != "" {
		*finishReason = googleFinishReason(reason)
		partial := openAIResponsesBasePartial(stream)
		if googlePartialHasToolCall(partial) {
			*finishReason = "tool_calls"
		}
	}
	if usage != nil {
		openAIChatSetStreamPartialUsage(stream, usage)
	}
	return true
}

func googlePartialHasToolCall(message AssistantMessage) bool {
	for _, block := range message.Content {
		if block.Type == ContentToolCall {
			return true
		}
	}
	return false
}

func googleSetPartialResponseID(stream *AssistantMessageEventStream, id string) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.partial == nil {
		stream.partial = &AssistantMessage{}
	}
	stream.partial.ResponseID = id
}

func ensureGoogleOpenBlock(stream *AssistantMessageEventStream, partial *AssistantMessage, openBlock *googleOpenBlock, block googleOpenBlock) int {
	if *openBlock == block && len(partial.Content) > 0 {
		return len(partial.Content) - 1
	}
	closeGoogleOpenBlock(stream, partial, *openBlock)
	index := len(partial.Content)
	if block == googleOpenBlockThinking {
		partial.Content = append(partial.Content, ContentBlock{Type: ContentThinking})
		stream.Emit(AssistantMessageEvent{Type: EventThinkingStart, ContentIndex: index, Partial: cloneAssistantMessage(*partial)})
	} else {
		partial.Content = append(partial.Content, ContentBlock{Type: ContentText})
		stream.Emit(AssistantMessageEvent{Type: EventTextStart, ContentIndex: index, Partial: cloneAssistantMessage(*partial)})
	}
	openAIResponsesSetStreamPartial(stream, partial)
	*openBlock = block
	return index
}

func closeGoogleOpenBlock(stream *AssistantMessageEventStream, partial *AssistantMessage, openBlock googleOpenBlock) {
	if openBlock == googleOpenBlockNone || len(partial.Content) == 0 {
		return
	}
	index := len(partial.Content) - 1
	block := partial.Content[index]
	if openBlock == googleOpenBlockThinking && block.Type == ContentThinking {
		stream.Emit(AssistantMessageEvent{Type: EventThinkingEnd, ContentIndex: index, Content: block.Thinking, Partial: cloneAssistantMessage(*partial)})
	}
	if openBlock == googleOpenBlockText && block.Type == ContentText {
		stream.Emit(AssistantMessageEvent{Type: EventTextEnd, ContentIndex: index, Content: block.Text, Partial: cloneAssistantMessage(*partial)})
	}
}

func googleFinishReason(reason string) string {
	if strings.EqualFold(reason, "function_call") || strings.EqualFold(reason, "tool_calls") {
		return "tool_calls"
	}
	switch MapGoogleStopReason(strings.ToUpper(reason)) {
	case StopReasonMaxTokens:
		return "length"
	case StopReasonError:
		return "error"
	default:
		return "stop"
	}
}

func usageFromGoogle(payload map[string]any) *Usage {
	usageMap, _ := payload["usageMetadata"].(map[string]any)
	if usageMap == nil {
		return nil
	}
	promptTokens := uintNumber(usageMap["promptTokenCount"])
	cacheReadTokens := uintNumber(usageMap["cachedContentTokenCount"])
	inputTokens := promptTokens - cacheReadTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	usage := &Usage{
		InputTokens:     inputTokens,
		OutputTokens:    uintNumber(usageMap["candidatesTokenCount"]) + uintNumber(usageMap["thoughtsTokenCount"]),
		CacheReadTokens: cacheReadTokens,
	}
	if totalTokens, ok := uintNumberPresent(usageMap["totalTokenCount"]); ok {
		usage.TotalTokenCount = totalTokens
	} else {
		usage.TotalTokenCount = usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens
	}
	usage.HasTotalTokens = true
	return usage
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}
