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

type OpenAIChatProvider struct {
	client                      *http.Client
	extraHeaders                map[string]string
	includeToolResultName       bool
	emptyAssistantContentString bool
	disableStreamOptions        bool
	textOnlyUserContent         bool
	contentToolCallsOnly        bool
	explicitToolResultNameOnly  bool
	ignoreSystemRoleMessages    bool
	firstChoiceOnly             bool
	firstMetadataOnly           bool
	ignoreResponseModel         bool
	ignoreTopLevelErrorChunks   bool
	suppressMetadataEvents      bool
	suppressUsageEvents         bool
	functionCallFinishStops     bool
	ignoreToolCallIndex         bool
	errorPrefix                 string
	missingAPIKeyMessage        string
}

type OpenAIChatOption func(*OpenAIChatProvider)

func WithOpenAIChatHTTPClient(client *http.Client) OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		if client != nil {
			provider.client = client
		}
	}
}

func WithOpenAIChatExtraHeaders(headers map[string]string) OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.extraHeaders = headers
	}
}

func withOpenAIChatToolResultNames() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.includeToolResultName = true
	}
}

func withOpenAIChatEmptyAssistantContentString() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.emptyAssistantContentString = true
	}
}

func withOpenAIChatDisableStreamOptions() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.disableStreamOptions = true
	}
}

func withOpenAIChatTextOnlyUserContent() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.textOnlyUserContent = true
	}
}

func withOpenAIChatContentToolCallsOnly() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.contentToolCallsOnly = true
	}
}

func withOpenAIChatExplicitToolResultNameOnly() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.explicitToolResultNameOnly = true
	}
}

func withOpenAIChatIgnoreSystemRoleMessages() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.ignoreSystemRoleMessages = true
	}
}

func withOpenAIChatFirstChoiceOnly() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.firstChoiceOnly = true
	}
}

func withOpenAIChatFirstMetadataOnly() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.firstMetadataOnly = true
	}
}

func withOpenAIChatIgnoreResponseModel() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.ignoreResponseModel = true
	}
}

func withOpenAIChatIgnoreTopLevelErrorChunks() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.ignoreTopLevelErrorChunks = true
	}
}

func withOpenAIChatSuppressMetadataEvents() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.suppressMetadataEvents = true
	}
}

func withOpenAIChatSuppressUsageEvents() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.suppressUsageEvents = true
	}
}

func withOpenAIChatFunctionCallFinishStops() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.functionCallFinishStops = true
	}
}

func withOpenAIChatIgnoreToolCallIndex() OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.ignoreToolCallIndex = true
	}
}

func withOpenAIChatErrorPrefix(prefix string) OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.errorPrefix = prefix
	}
}

func withOpenAIChatMissingAPIKeyMessage(message string) OpenAIChatOption {
	return func(provider *OpenAIChatProvider) {
		provider.missingAPIKeyMessage = message
	}
}

func NewOpenAIChatProvider(options ...OpenAIChatOption) *OpenAIChatProvider {
	provider := &OpenAIChatProvider{client: nil}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func (provider *OpenAIChatProvider) API() Api { return ApiOpenAI }

func (provider *OpenAIChatProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	return provider.Stream(ctx, model, request, StreamOptionsFromSimple(options))
}

func (provider *OpenAIChatProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
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
		if provider.missingAPIKeyMessage != "" {
			return openAIChatProviderErrorStream(model, provider.missingAPIKeyMessage)
		}
		return openAIChatProviderErrorStream(model, missingOpenAICompatibleAPIKeyMessage(model))
	}
	client, err := effectiveHTTPClient(provider.client, options)
	if err != nil {
		return openAIChatProviderErrorStream(model, "http client: "+err.Error())
	}
	body, err := provider.buildOpenAIChatRequestBody(model, request, options)
	if err != nil {
		return openAIChatProviderErrorStream(model, err.Error())
	}
	data, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return openAIChatProviderErrorStream(model, err.Error())
	}
	baseURL, err := ResolveProviderBaseURL(model, options)
	if err != nil {
		return openAIChatProviderErrorStream(model, err.Error())
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, BuildOpenAIChatURL(baseURL), bytes.NewReader(data))
	if err != nil {
		return openAIChatProviderErrorStream(model, "http error: "+err.Error())
	}
	httpRequest.Header.Set("Authorization", "Bearer "+apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	if options.SessionID != "" && openAIChatSendSessionAffinityHeaders(model) {
		httpRequest.Header.Set("x-affinity", options.SessionID)
	}
	for key, value := range provider.extraHeaders {
		if value != "" {
			httpRequest.Header.Set(key, value)
		}
	}
	applyModelAndOptionHeaders(httpRequest, model, options)
	response, err := sendWithRetry(client, httpRequest, data, options)
	if err != nil {
		if stream, ok := AbortedStreamIfCanceled(model, err); ok {
			return stream
		}
		return openAIChatProviderErrorStream(model, "http error: "+err.Error())
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		prefix := provider.errorPrefix
		if prefix == "" {
			prefix = "HTTP"
			return openAIChatProviderErrorStream(model, fmt.Sprintf("%s %s: %s", prefix, response.Status, strings.TrimSpace(string(data))))
		}
		return openAIChatProviderErrorStream(model, fmt.Sprintf("%s (%s): %s", prefix, response.Status, strings.TrimSpace(string(data))))
	}
	stream := NewAssistantMessageEventStream().MarkLive()
	go func() {
		defer response.Body.Close()
		if err := provider.consumeOpenAIChatSSE(response.Body, stream, model); err != nil {
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

func BuildOpenAIChatURL(base string) string {
	trimmed := strings.TrimRight(base, "/")
	if strings.HasSuffix(trimmed, "/v1") || strings.Contains(trimmed, "/v1/") {
		return trimmed + "/chat/completions"
	}
	return trimmed + "/v1/chat/completions"
}

func BuildOpenAIChatRequestBody(model Model, request Context, options StreamOptions) (map[string]any, error) {
	return buildOpenAIChatRequestBody(model, request, options, false, false, false, false, false, false, false, openAIChatReasoningReplayField(model))
}

func (provider *OpenAIChatProvider) buildOpenAIChatRequestBody(model Model, request Context, options StreamOptions) (map[string]any, error) {
	return buildOpenAIChatRequestBody(model, request, options, provider.includeToolResultName, provider.emptyAssistantContentString, provider.disableStreamOptions, provider.textOnlyUserContent, provider.contentToolCallsOnly, provider.explicitToolResultNameOnly, provider.ignoreSystemRoleMessages, openAIChatReasoningReplayField(model))
}

func buildOpenAIChatRequestBody(model Model, request Context, options StreamOptions, includeToolResultName bool, emptyAssistantContentString bool, disableStreamOptions bool, textOnlyUserContent bool, contentToolCallsOnly bool, explicitToolResultNameOnly bool, ignoreSystemRoleMessages bool, reasoningReplayField string) (map[string]any, error) {
	messages := convertMessagesForOpenAIChat(request.Messages, includeToolResultName, emptyAssistantContentString, textOnlyUserContent, contentToolCallsOnly, explicitToolResultNameOnly, ignoreSystemRoleMessages, reasoningReplayField)
	if request.HasSystemPrompt || request.SystemPrompt != "" {
		messages = append([]map[string]any{{"role": "system", "content": request.SystemPrompt}}, messages...)
	}
	body := map[string]any{
		"model":    model.ID,
		"messages": messages,
		"stream":   true,
	}
	if !disableStreamOptions {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	if options.MaxTokens > 0 {
		body[openAIChatMaxTokensField(model)] = options.MaxTokens
	}
	if options.Temperature != nil {
		body["temperature"] = *options.Temperature
	}
	if effort, ok := options.ProviderExtras["reasoning_effort"]; ok && openAIChatSupportsReasoningEffort(model) {
		body["reasoning_effort"] = effort
	}
	if len(request.Tools) > 0 {
		body["tools"] = SerializeOpenAIChatTools(request.Tools)
	}
	return body, nil
}

func openAIChatMaxTokensField(model Model) string {
	if field, ok := model.Compat["maxTokensField"].(string); ok && field != "" {
		return field
	}
	return "max_tokens"
}

func openAIChatSendSessionAffinityHeaders(model Model) bool {
	value, ok := model.Compat["sendSessionAffinityHeaders"].(bool)
	return ok && value
}

func openAIChatReasoningReplayField(model Model) string {
	value, _ := model.Compat["requiresReasoningContentOnAssistantMessages"].(bool)
	if !value {
		return ""
	}
	switch format, _ := model.Compat["thinkingFormat"].(string); format {
	case "together":
		return "reasoning"
	case "openai":
		return "reasoning_text"
	default:
		return "reasoning_content"
	}
}

func openAIChatSupportsReasoningEffort(model Model) bool {
	value, ok := model.Compat["supportsReasoningEffort"].(bool)
	return !ok || value
}

func openAIChatZaiToolStream(model Model) bool {
	value, _ := model.Compat["zaiToolStream"].(bool)
	return value
}

func ConvertMessagesForOpenAIChat(messages []Message) []map[string]any {
	return convertMessagesForOpenAIChat(messages, false, false, false, false, false, false, "")
}

func convertMessagesForOpenAIChat(messages []Message, includeToolResultName bool, emptyAssistantContentString bool, textOnlyUserContent bool, contentToolCallsOnly bool, explicitToolResultNameOnly bool, ignoreSystemRoleMessages bool, reasoningReplayField string) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case RoleSystem:
			if ignoreSystemRoleMessages {
				continue
			}
			out = append(out, map[string]any{"role": string(message.Role), "content": blocksText(message.Content)})
		case RoleUser:
			content := openAIChatUserContent(message.Content)
			if textOnlyUserContent {
				content = blocksText(message.Content)
			}
			out = append(out, map[string]any{"role": "user", "content": content})
		case RoleAssistant:
			entry := map[string]any{"role": "assistant"}
			if text := blocksText(message.Content); text != "" {
				entry["content"] = text
			} else if emptyAssistantContentString {
				entry["content"] = ""
			} else {
				entry["content"] = nil
			}
			messageToolCalls := message.ToolCalls
			if contentToolCallsOnly {
				messageToolCalls = nil
			}
			toolCalls := openAIChatToolCalls(deduplicateToolCalls(append(openAIChatToolCallBlocks(message.Content), messageToolCalls...)))
			if len(toolCalls) > 0 {
				entry["tool_calls"] = toolCalls
			}
			if reasoningReplayField != "" {
				if reasoning := openAIChatThinkingText(message.Content); reasoning != "" {
					entry[reasoningReplayField] = reasoning
				}
			}
			out = append(out, entry)
		case RoleTool:
			entry := map[string]any{"role": "tool", "tool_call_id": message.ToolCallID, "content": blocksText(message.Content)}
			name := messageToolName(message)
			if explicitToolResultNameOnly {
				name = message.ToolName
			}
			if includeToolResultName && (name != "" || explicitToolResultNameOnly) {
				entry["name"] = name
			}
			out = append(out, entry)
		}
	}
	return out
}

func openAIChatThinkingText(blocks []ContentBlock) string {
	parts := []string{}
	for _, block := range blocks {
		if block.Type == ContentThinking && block.Thinking != "" {
			parts = append(parts, block.Thinking)
		}
	}
	return strings.Join(parts, "")
}

func openAIChatToolCallBlocks(blocks []ContentBlock) []ToolCall {
	calls := []ToolCall{}
	for _, block := range blocks {
		if block.Type == ContentToolCall && block.ToolCall != nil {
			calls = append(calls, *block.ToolCall)
		}
	}
	return calls
}

func openAIChatUserContent(blocks []ContentBlock) any {
	hasImage := false
	for _, block := range blocks {
		if block.Type == ContentImage {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return blocksText(blocks)
	}
	content := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentImage:
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + block.MimeType + ";base64," + block.Data}})
		case ContentText:
			content = append(content, map[string]any{"type": "text", "text": block.Text})
		}
	}
	return content
}

func SerializeOpenAIChatTools(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters}})
	}
	return out
}

func openAIChatToolCalls(calls []ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		arguments, _ := marshalJSONNoHTMLEscape(call.Arguments)
		out = append(out, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(arguments)}})
	}
	return out
}

func ConsumeOpenAIChatSSE(reader io.Reader, stream *AssistantMessageEventStream) error {
	return ConsumeOpenAIChatSSEForModel(reader, stream, "")
}

func ConsumeOpenAIChatSSEForModel(reader io.Reader, stream *AssistantMessageEventStream, modelID string) error {
	return consumeOpenAIChatSSE(reader, stream, Model{ID: modelID}, false, false, false, false, false, false, false, false)
}

func (provider *OpenAIChatProvider) consumeOpenAIChatSSE(reader io.Reader, stream *AssistantMessageEventStream, model Model) error {
	return consumeOpenAIChatSSE(reader, stream, model, provider.firstChoiceOnly, provider.firstMetadataOnly, provider.ignoreResponseModel, provider.ignoreTopLevelErrorChunks, provider.suppressMetadataEvents, provider.suppressUsageEvents, provider.functionCallFinishStops, provider.ignoreToolCallIndex || openAIChatZaiToolStream(model))
}

func consumeOpenAIChatSSE(reader io.Reader, stream *AssistantMessageEventStream, model Model, firstChoiceOnly bool, firstMetadataOnly bool, ignoreResponseModel bool, ignoreTopLevelErrorChunks bool, suppressMetadataEvents bool, suppressUsageEvents bool, functionCallFinishStops bool, ignoreToolCallIndex bool) error {
	partial := responsesEmptyPartial(model)
	openAIResponsesSetStreamPartial(stream, partial)
	started := false
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	calls := map[int]*chatToolCallDelta{}
	metadata := chatStreamMetadata{}
	finishReason := ""
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			if !started {
				stream.Emit(AssistantMessageEvent{Type: EventStart, Partial: partial})
				started = true
			}
			if _, ok := stream.Snapshot(); !ok {
				closeOpenAIChatStream(stream, finishReason, calls, functionCallFinishStops)
			}
			return nil
		}
		if !isOpenAIChatTopLevelErrorChunk([]byte(data), ignoreTopLevelErrorChunks) && !started {
			stream.Emit(AssistantMessageEvent{Type: EventStart, Partial: partial})
			started = true
		}
		if !handleOpenAIChatChunk([]byte(data), stream, calls, &finishReason, model.ID, firstChoiceOnly, firstMetadataOnly, ignoreResponseModel, ignoreTopLevelErrorChunks, suppressMetadataEvents, suppressUsageEvents, &metadata, ignoreToolCallIndex) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		if IsCanceledError(err) {
			PushAborted(stream, model)
			return nil
		}
		stream.Emit(openAIChatModelErrorEvent(model, "sse: "+err.Error()))
		return nil
	}
	if _, ok := stream.Snapshot(); !ok {
		closeOpenAIChatStream(stream, finishReason, calls, functionCallFinishStops)
	}
	return nil
}

func isOpenAIChatTopLevelErrorChunk(data []byte, ignoreTopLevelErrorChunks bool) bool {
	if ignoreTopLevelErrorChunks {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return false
	}
	_, ok := payload["error"].(map[string]any)
	return ok
}

func closeOpenAIChatStream(stream *AssistantMessageEventStream, finishReason string, calls map[int]*chatToolCallDelta, functionCallFinishStops bool) {
	message, _ := stream.Snapshot()
	partial := openAIResponsesBasePartial(stream)
	finalizeOpenAIChatToolArguments(&partial, calls)
	openAIResponsesSetStreamPartial(stream, &partial)
	emitOpenAIChatContentEnds(stream, partial)
	partial.Usage = nil
	mergePartialAssistantMessage(&message, &partial)
	switch finishReason {
	case "tool_calls":
		message.StopReason = StopReasonToolCalls
		stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonToolCalls, Message: &message})
	case "function_call":
		if functionCallFinishStops {
			message.StopReason = StopReasonEndTurn
			stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonStop, Message: &message})
			return
		}
		message.StopReason = StopReasonToolCalls
		stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonToolCalls, Message: &message})
	case "length", "model_length":
		message.StopReason = StopReasonMaxTokens
		stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonLength, Message: &message})
	case "content_filter", "network_error":
		message.StopReason = StopReasonError
		message.ErrorMessage = "Provider finish_reason: " + finishReason
		stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &message})
	default:
		message.StopReason = StopReasonEndTurn
		stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonStop, Message: &message})
	}
}

func finalizeOpenAIChatToolArguments(partial *AssistantMessage, calls map[int]*chatToolCallDelta) {
	for _, call := range calls {
		if call == nil || call.ContentIndex < 0 || call.ContentIndex >= len(partial.Content) {
			continue
		}
		block := &partial.Content[call.ContentIndex]
		if block.Type != ContentToolCall || block.ToolCall == nil {
			continue
		}
		if args, ok := parsePartialJSONObject(call.Arguments); ok {
			updated := *block.ToolCall
			updated.Arguments = args
			block.ToolCall = &updated
		}
	}
}

func emitOpenAIChatContentEnds(stream *AssistantMessageEventStream, partial AssistantMessage) {
	for index, block := range partial.Content {
		if block.Type == ContentText {
			stream.Emit(AssistantMessageEvent{Type: EventTextEnd, ContentIndex: index, Content: block.Text, Partial: cloneAssistantMessage(partial)})
		}
	}
	for index, block := range partial.Content {
		if block.Type == ContentThinking {
			stream.Emit(AssistantMessageEvent{Type: EventThinkingEnd, ContentIndex: index, Content: block.Thinking, Partial: cloneAssistantMessage(partial)})
		}
	}
	for index, block := range partial.Content {
		if block.Type == ContentToolCall && block.ToolCall != nil {
			call := *block.ToolCall
			stream.Emit(AssistantMessageEvent{Type: EventToolCallEnd, ContentIndex: index, ToolCall: &call, Partial: cloneAssistantMessage(partial)})
		}
	}
}

type chatToolCallDelta struct {
	ID           string
	Name         string
	Arguments    string
	ContentIndex int
	Emitted      bool
}

func HandleOpenAIChatChunk(data []byte, stream *AssistantMessageEventStream, calls map[int]*chatToolCallDelta, finishReason *string) bool {
	return HandleOpenAIChatChunkForModel(data, stream, calls, finishReason, "")
}

func HandleOpenAIChatChunkForModel(data []byte, stream *AssistantMessageEventStream, calls map[int]*chatToolCallDelta, finishReason *string, modelID string) bool {
	return handleOpenAIChatChunk(data, stream, calls, finishReason, modelID, false, false, false, false, false, false, nil, false)
}

type chatStreamMetadata struct {
	ResponseID    string
	ResponseModel string
}

func handleOpenAIChatChunk(data []byte, stream *AssistantMessageEventStream, calls map[int]*chatToolCallDelta, finishReason *string, modelID string, firstChoiceOnly bool, firstMetadataOnly bool, ignoreResponseModel bool, ignoreTopLevelErrorChunks bool, suppressMetadataEvents bool, suppressUsageEvents bool, metadata *chatStreamMetadata, ignoreToolCallIndex bool) bool {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return true
	}
	if errMap, ok := payload["error"].(map[string]any); ok && !ignoreTopLevelErrorChunks {
		stream.Emit(openAIChatErrorEvent(modelID, stringValue(errMap["message"])))
		return false
	}
	if id := stringValue(payload["id"]); id != "" && shouldEmitChatResponseID(firstMetadataOnly, metadata) {
		if !suppressMetadataEvents {
			stream.Emit(AssistantMessageEvent{Type: EventMetadata, ResponseID: id})
		}
		openAIChatSetStreamPartialMetadata(stream, id, "")
		if metadata != nil {
			metadata.ResponseID = id
		}
	}
	if responseModel := stringValue(payload["model"]); !ignoreResponseModel && responseModel != "" && (modelID == "" || responseModel != modelID) && shouldEmitChatResponseModel(firstMetadataOnly, metadata) {
		if !suppressMetadataEvents {
			stream.Emit(AssistantMessageEvent{Type: EventMetadata, ResponseModel: responseModel})
		}
		openAIChatSetStreamPartialMetadata(stream, "", responseModel)
		if metadata != nil {
			metadata.ResponseModel = responseModel
		}
	}
	if usage := usageFromChat(payload); usage != nil {
		openAIChatSetStreamPartialUsage(stream, usage)
		if !suppressUsageEvents {
			stream.Emit(AssistantMessageEvent{Type: EventUsage, Usage: usage})
		}
	}
	choices, _ := payload["choices"].([]any)
	if firstChoiceOnly && len(choices) > 1 {
		choices = choices[:1]
	}
	for _, choiceAny := range choices {
		choice, _ := choiceAny.(map[string]any)
		if reason := stringValue(choice["finish_reason"]); reason != "" {
			*finishReason = reason
		}
		delta, _ := choice["delta"].(map[string]any)
		if content := stringValue(delta["content"]); content != "" {
			index := lastOpenAIResponsesContentIndex(stream, ContentText)
			if index < 0 {
				index = nextOpenAIResponsesContentIndex(stream)
				stream.Emit(AssistantMessageEvent{Type: EventTextStart, ContentIndex: index, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentText})})
			}
			text := openAIResponsesContentText(stream, index, ContentText) + content
			stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: index, Delta: content, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentText, Text: text})})
		}
		if reasoning := openAIChatReasoningDelta(delta); reasoning != "" {
			index := lastOpenAIResponsesContentIndex(stream, ContentThinking)
			if index < 0 {
				index = nextOpenAIResponsesContentIndex(stream)
				stream.Emit(AssistantMessageEvent{Type: EventThinkingStart, ContentIndex: index, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentThinking})})
			}
			thinking := openAIResponsesContentText(stream, index, ContentThinking) + reasoning
			stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, ContentIndex: index, Delta: reasoning, Partial: openAIResponsesPartialContent(stream, index, ContentBlock{Type: ContentThinking, Thinking: thinking})})
		}
		applyChatToolCallDeltas(stream, calls, delta["tool_calls"], ignoreToolCallIndex)
	}
	return true
}

func openAIChatErrorEvent(modelID, message string) AssistantMessageEvent {
	return openAIChatModelErrorEvent(Model{ID: modelID}, message)
}

func openAIChatModelErrorEvent(model Model, message string) AssistantMessageEvent {
	assistantMessage := AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonError, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
	return AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &assistantMessage}
}

func openAIChatProviderErrorStream(model Model, message string) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	assistantMessage := AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonError, ErrorMessage: message, Timestamp: time.Now().UnixMilli()}
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &assistantMessage})
	return stream
}

func openAIChatSetStreamPartialUsage(stream *AssistantMessageEventStream, usage *Usage) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.partial == nil {
		stream.partial = &AssistantMessage{}
	}
	copy := *usage
	stream.partial.Usage = &copy
}

func openAIChatSetStreamPartialMetadata(stream *AssistantMessageEventStream, responseID string, responseModel string) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.partial == nil {
		stream.partial = &AssistantMessage{}
	}
	if responseID != "" {
		stream.partial.ResponseID = responseID
	}
	if responseModel != "" {
		stream.partial.ResponseModel = responseModel
	}
}

func shouldEmitChatResponseID(firstMetadataOnly bool, metadata *chatStreamMetadata) bool {
	return !firstMetadataOnly || metadata == nil || metadata.ResponseID == ""
}

func shouldEmitChatResponseModel(firstMetadataOnly bool, metadata *chatStreamMetadata) bool {
	return !firstMetadataOnly || metadata == nil || metadata.ResponseModel == ""
}

func openAIChatReasoningDelta(delta map[string]any) string {
	for _, field := range []string{"reasoning_content", "reasoning", "reasoning_text"} {
		if value := stringValue(delta[field]); value != "" {
			return value
		}
	}
	return ""
}

func applyChatToolCallDeltas(stream *AssistantMessageEventStream, calls map[int]*chatToolCallDelta, raw any, ignoreIndex bool) {
	items, _ := raw.([]any)
	for _, itemAny := range items {
		item, _ := itemAny.(map[string]any)
		index := uintNumber(item["index"])
		if ignoreIndex {
			index = 0
		}
		call := calls[index]
		if call == nil {
			call = &chatToolCallDelta{ContentIndex: nextOpenAIResponsesContentIndex(stream)}
			calls[index] = call
		}
		if id := stringValue(item["id"]); id != "" {
			call.ID = id
		}
		function, _ := item["function"].(map[string]any)
		if name := stringValue(function["name"]); name != "" {
			call.Name = name
		}
		arguments := stringValue(function["arguments"])
		if arguments != "" {
			call.Arguments += arguments
		}
		if !call.Emitted {
			toolCall := &ToolCall{ID: call.ID, Name: call.Name, Arguments: map[string]any{}}
			stream.Emit(AssistantMessageEvent{Type: EventToolCallStart, ContentIndex: call.ContentIndex, Partial: openAIResponsesPartialContent(stream, call.ContentIndex, ContentBlock{Type: ContentToolCall, ToolCall: toolCall})})
			call.Emitted = true
		}
		if call.Emitted && call.Arguments != "" {
			applyChatToolArguments(stream, call, arguments)
		}
	}
}

func applyChatToolArguments(stream *AssistantMessageEventStream, call *chatToolCallDelta, delta string) {
	if delta == "" {
		return
	}
	updated := ToolCall{ID: call.ID, Name: call.Name, Arguments: map[string]any{}}
	stream.Emit(AssistantMessageEvent{Type: EventToolCallDelta, ContentIndex: call.ContentIndex, Delta: delta, Partial: openAIResponsesPartialContent(stream, call.ContentIndex, ContentBlock{Type: ContentToolCall, ToolCall: &updated})})
}

func usageFromChat(payload map[string]any) *Usage {
	usageMap, _ := payload["usage"].(map[string]any)
	if usageMap == nil {
		return nil
	}
	usage := &Usage{InputTokens: uintNumber(usageMap["prompt_tokens"]), OutputTokens: uintNumber(usageMap["completion_tokens"])}
	if details, ok := usageMap["prompt_tokens_details"].(map[string]any); ok {
		usage.CacheReadTokens = uintNumber(details["cached_tokens"])
	}
	usage.TotalTokenCount = usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	usage.HasTotalTokens = true
	return usage
}
