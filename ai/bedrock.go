package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type BedrockEventStreamMessage struct {
	EventType     string
	ExceptionType string
	Payload       []byte
}

type bedrockStreamState struct {
	toolCalls    map[float64]*bedrockToolCallState
	contentIndex map[float64]int
	partial      AssistantMessage
	doneReason   DoneReason
}

type bedrockToolCallState struct {
	ID        string
	Name      string
	Arguments string
	Emitted   bool
}

type BedrockProvider struct {
	client *http.Client
}

type AmazonBedrockProvider = BedrockProvider

type BedrockOption func(*BedrockProvider)

func WithBedrockHTTPClient(client *http.Client) BedrockOption {
	return func(provider *BedrockProvider) {
		if client != nil {
			provider.client = client
		}
	}
}

func NewBedrockProvider(options ...BedrockOption) *BedrockProvider {
	provider := &BedrockProvider{client: nil}
	for _, option := range options {
		option(provider)
	}
	return provider
}

func Register() {}

func (provider *BedrockProvider) API() Api { return ApiBedrockConverseStream }

func (provider *BedrockProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	return provider.Stream(ctx, model, request, options.Base)
}

func (provider *BedrockProvider) Stream(ctx context.Context, model Model, request Context, options StreamOptions) *AssistantMessageEventStream {
	token := options.APIKey
	if token == "" {
		token = strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK"))
	}
	client, err := effectiveHTTPClient(provider.client, options)
	if err != nil {
		return bedrockProviderErrorStream(model, "http client: "+err.Error())
	}
	body, err := BuildBedrockRequestBody(request, options)
	if err != nil {
		return bedrockProviderErrorStream(model, err.Error())
	}
	payload, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return bedrockProviderErrorStream(model, err.Error())
	}
	baseURL := model.BaseURL
	if baseURL == "" {
		return bedrockProviderErrorStream(model, "Bedrock base URL is not set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, BuildBedrockConverseStreamURL(baseURL, model.ID), bytes.NewReader(payload))
	if err != nil {
		return bedrockProviderErrorStream(model, "http error: "+err.Error())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.amazon.eventstream")
	applyModelAndOptionHeaders(req, model, options)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if err := SignBedrockRequest(req, payload, baseURL); err != nil {
		return bedrockProviderErrorStream(model, "Bedrock auth missing: set AWS_BEARER_TOKEN_BEDROCK, pass options.api_key, or configure AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY")
	}
	response, err := sendWithRetry(client, req, payload, options)
	if err != nil {
		if stream, ok := AbortedStreamIfCanceled(model, err); ok {
			return stream
		}
		return bedrockProviderErrorStream(model, "http error: "+err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return bedrockProviderErrorStream(model, fmt.Sprintf("Bedrock API error (%s): %s", response.Status, strings.TrimSpace(string(body))))
	}
	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStreamForModel(response.Body, stream, model); err != nil {
		if aborted, ok := AbortedStreamIfCanceled(model, err); ok {
			return aborted
		}
		message := bedrockEmptyPartial(model)
		message.StopReason = StopReasonError
		message.ErrorMessage = "eventstream: " + err.Error()
		stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &message})
	}
	return stream
}

func bedrockProviderErrorStream(model Model, message string) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	assistantMessage := bedrockEmptyPartial(model)
	assistantMessage.StopReason = StopReasonError
	assistantMessage.ErrorMessage = message
	stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: &assistantMessage})
	return stream
}

func bedrockEmptyPartial(model Model) AssistantMessage {
	return AssistantMessage{Role: AssistantRoleAssistant, Content: []ContentBlock{}, API: model.API, Provider: model.Provider, Model: model.ID, Usage: &Usage{}, StopReason: StopReasonEndTurn, Timestamp: time.Now().UnixMilli()}
}

func SignBedrockRequest(req *http.Request, payload []byte, baseURL string) error {
	credentials, ok := AWSEnvCredentials()
	if !ok {
		return fmt.Errorf("Bedrock auth missing: set AWS_BEARER_TOKEN_BEDROCK or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY")
	}
	region := AWSRegion(baseURL)
	signed := SignSigV4(SigV4SigningRequest{
		Method:       req.Method,
		URL:          req.URL,
		Headers:      []SigV4Header{{Name: "content-type", Value: req.Header.Get("Content-Type")}, {Name: "accept", Value: req.Header.Get("Accept")}},
		Payload:      payload,
		Region:       region,
		Service:      "bedrock",
		AccessKey:    credentials.AccessKey,
		SecretKey:    credentials.SecretKey,
		SessionToken: credentials.SessionToken,
		AMZDate:      time.Now().UTC().Format("20060102T150405Z"),
	})
	for _, header := range signed.AllHeaders() {
		req.Header.Set(header.Name, header.Value)
	}
	return nil
}

type AWSCredentials struct {
	AccessKey    string
	SecretKey    string
	SessionToken string
}

type BedrockCreds struct {
	AccessKey    string
	SecretKey    string
	SessionToken string
	Region       string
}

type BedrockErrorKind string

const (
	BedrockErrorOther    BedrockErrorKind = "other"
	BedrockErrorExchange BedrockErrorKind = "exchange"
)

type BedrockError struct {
	Kind    BedrockErrorKind
	Message string
	Err     error
}

func (err BedrockError) Error() string {
	if err.Message != "" {
		return err.Message
	}
	if err.Err != nil {
		return err.Err.Error()
	}
	return "bedrock error"
}

func (err BedrockError) Unwrap() error { return err.Err }

func BedrockErrorNetwork(message string) BedrockError {
	return BedrockError{Kind: BedrockErrorExchange, Message: "network error: " + message}
}

func BedrockCredsFromEnv() (BedrockCreds, bool) {
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey == "" || secretKey == "" {
		return BedrockCreds{}, false
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	return BedrockCreds{AccessKey: accessKey, SecretKey: secretKey, SessionToken: os.Getenv("AWS_SESSION_TOKEN"), Region: region}, true
}

func (BedrockCreds) FromEnv() (BedrockCreds, bool) {
	return BedrockCredsFromEnv()
}

func InvokeBedrock(ctx context.Context, client *http.Client, baseURL string, credentials BedrockCreds, modelID string, body any) (map[string]any, error) {
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", credentials.Region)
	}
	payload, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return nil, BedrockError{Kind: BedrockErrorOther, Err: err}
	}
	requestURL := BuildBedrockInvokeURL(baseURL, modelID)
	request, err := newSignedBedrockRequest(ctx, requestURL, payload, credentials, nil)
	if err != nil {
		return nil, BedrockError{Kind: BedrockErrorOther, Err: err}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, BedrockError{Kind: BedrockErrorExchange, Message: fmt.Sprintf("network error: %s", err), Err: err}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, BedrockError{Kind: BedrockErrorExchange, Message: fmt.Sprintf("network error: %s", err), Err: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, BedrockError{Kind: BedrockErrorExchange, Message: fmt.Sprintf("HTTP %d: %s", response.StatusCode, truncateString(string(responseBody), 500))}
	}
	var out map[string]any
	if err := json.Unmarshal(responseBody, &out); err != nil {
		return nil, BedrockError{Kind: BedrockErrorOther, Message: fmt.Sprintf("parse json body: %s", err), Err: err}
	}
	return out, nil
}

func Invoke(ctx context.Context, client *http.Client, baseURL string, credentials BedrockCreds, modelID string, body any) (map[string]any, error) {
	return InvokeBedrock(ctx, client, baseURL, credentials, modelID, body)
}

func InvokeBedrockStream(ctx context.Context, client *http.Client, baseURL string, credentials BedrockCreds, modelID string, body any) ([]AWSEventMessage, error) {
	if client == nil {
		client = &http.Client{Timeout: 300 * time.Second}
	}
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", credentials.Region)
	}
	payload, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return nil, BedrockError{Kind: BedrockErrorOther, Err: err}
	}
	requestURL := BuildBedrockInvokeStreamURL(baseURL, modelID)
	request, err := newSignedBedrockRequest(ctx, requestURL, payload, credentials, []SigV4Header{{Name: "accept", Value: "application/vnd.amazon.eventstream"}})
	if err != nil {
		return nil, BedrockError{Kind: BedrockErrorOther, Err: err}
	}
	request.Header.Set("Accept", "application/vnd.amazon.eventstream")
	response, err := client.Do(request)
	if err != nil {
		return nil, BedrockError{Kind: BedrockErrorExchange, Message: fmt.Sprintf("network error: %s", err), Err: err}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, BedrockError{Kind: BedrockErrorExchange, Message: fmt.Sprintf("network error: %s", err), Err: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, BedrockError{Kind: BedrockErrorExchange, Message: fmt.Sprintf("HTTP %d: %s", response.StatusCode, truncateString(string(responseBody), 500))}
	}
	messages := []AWSEventMessage{}
	for len(responseBody) > 0 {
		message, consumed, err := ParseAWSEventStreamMessage(responseBody)
		if err != nil {
			return nil, BedrockError{Kind: BedrockErrorOther, Message: fmt.Sprintf("event-stream parse: %s", err), Err: err}
		}
		messages = append(messages, message)
		responseBody = responseBody[consumed:]
	}
	return messages, nil
}

func InvokeStream(ctx context.Context, client *http.Client, baseURL string, credentials BedrockCreds, modelID string, body any) ([]AWSEventMessage, error) {
	return InvokeBedrockStream(ctx, client, baseURL, credentials, modelID, body)
}

func newSignedBedrockRequest(ctx context.Context, requestURL string, payload []byte, credentials BedrockCreds, extraHeaders []SigV4Header) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return nil, err
	}
	headers := append([]SigV4Header{{Name: "content-type", Value: "application/json"}}, extraHeaders...)
	signed := SignSigV4(SigV4SigningRequest{
		Method:       http.MethodPost,
		URL:          parsedURL,
		Headers:      headers,
		Payload:      payload,
		Region:       credentials.Region,
		Service:      "bedrock",
		AccessKey:    credentials.AccessKey,
		SecretKey:    credentials.SecretKey,
		SessionToken: credentials.SessionToken,
		AMZDate:      time.Now().UTC().Format("20060102T150405Z"),
	})
	for _, header := range signed.AllHeaders() {
		request.Header.Set(header.Name, header.Value)
	}
	return request, nil
}

func AWSEnvCredentials() (AWSCredentials, bool) {
	credentials := AWSCredentials{
		AccessKey:    strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")),
		SecretKey:    strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")),
		SessionToken: strings.TrimSpace(os.Getenv("AWS_SESSION_TOKEN")),
	}
	return credentials, credentials.AccessKey != "" && credentials.SecretKey != ""
}

func AWSRegion(baseURL string) string {
	if region := strings.TrimSpace(os.Getenv("AWS_REGION")); region != "" {
		return region
	}
	if region := strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION")); region != "" {
		return region
	}
	parsed, err := url.Parse(baseURL)
	if err == nil {
		parts := strings.Split(parsed.Hostname(), ".")
		for index, part := range parts {
			if part == "bedrock-runtime" && index+1 < len(parts) {
				return parts[index+1]
			}
		}
	}
	return "us-east-1"
}

func ConsumeBedrockEventStream(reader io.Reader, stream *AssistantMessageEventStream) error {
	return ConsumeBedrockEventStreamForModel(reader, stream, Model{})
}

func ConsumeBedrockEventStreamForModel(reader io.Reader, stream *AssistantMessageEventStream, model Model) error {
	state := bedrockStreamState{toolCalls: map[float64]*bedrockToolCallState{}, contentIndex: map[float64]int{}, partial: bedrockEmptyPartial(model)}
	stream.Emit(AssistantMessageEvent{Type: EventStart, Partial: cloneAssistantMessage(state.partial)})
	buffer := make([]byte, 0, 8192)
	chunk := make([]byte, 4096)
	for {
		read, err := reader.Read(chunk)
		if read > 0 {
			buffer = append(buffer, chunk[:read]...)
			for len(buffer) > 0 {
				message, consumed, parseErr := ParseAWSEventStreamMessage(buffer)
				if parseErr != nil {
					if isPartialAWSEventStreamFrameError(parseErr, buffer) {
						break
					}
					return parseErr
				}
				bedrockMessage := BedrockEventStreamMessage{EventType: message.EventType(), ExceptionType: bedrockExceptionType(message), Payload: message.Payload}
				if !HandleBedrockEventWithState(bedrockMessage, stream, &state) {
					return nil
				}
				buffer = buffer[consumed:]
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			if IsCanceledError(err) {
				PushAborted(stream, model)
				return nil
			}
			return err
		}
	}
	if len(buffer) > 0 {
		_, _, err := ParseAWSEventStreamMessage(buffer)
		if err != nil {
			return err
		}
	}
	if _, ok := stream.Snapshot(); !ok {
		emitBedrockDone(stream, &state)
	}
	return nil
}

func isPartialAWSEventStreamFrameError(err error, buffer []byte) bool {
	return strings.HasPrefix(err.Error(), "frame too short:") && len(buffer) < 12 || strings.HasPrefix(err.Error(), "frame too short: need ")
}

func bedrockExceptionType(message AWSEventMessage) string {
	return message.headerString(":exception-type")
}

func DecodeBedrockEventStreamFrame(data []byte) (BedrockEventStreamMessage, []byte, bool) {
	message, consumed, err := ParseAWSEventStreamMessage(data)
	if err != nil {
		return BedrockEventStreamMessage{}, data, false
	}
	return BedrockEventStreamMessage{EventType: message.EventType(), ExceptionType: bedrockExceptionType(message), Payload: message.Payload}, data[consumed:], true
}

func HandleBedrockEvent(message BedrockEventStreamMessage, stream *AssistantMessageEventStream) bool {
	state := bedrockStreamState{toolCalls: map[float64]*bedrockToolCallState{}, contentIndex: map[float64]int{}}
	return HandleBedrockEventWithState(message, stream, &state)
}

func HandleBedrockEventWithState(message BedrockEventStreamMessage, stream *AssistantMessageEventStream, state *bedrockStreamState) bool {
	if state.partial.Timestamp == 0 {
		state.partial = bedrockEmptyPartial(Model{})
	}
	if state.contentIndex == nil {
		state.contentIndex = map[float64]int{}
	}
	if message.ExceptionType != "" {
		state.partial.StopReason = StopReasonError
		state.partial.ErrorMessage = message.ExceptionType + ": " + string(message.Payload)
		stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: cloneAssistantMessage(state.partial)})
		return false
	}
	var payload map[string]any
	if len(message.Payload) > 0 {
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			return true
		}
	}
	switch message.EventType {
	case "contentBlockStart":
		if start := nestedMap(payload, "start"); hasKey(start, "toolUse") {
			toolUse, _ := start["toolUse"].(map[string]any)
			index := numberValue(payload["contentBlockIndex"])
			call := &bedrockToolCallState{ID: stringValue(toolUse["toolUseId"]), Name: stringValue(toolUse["name"])}
			state.toolCalls[index] = call
			position := len(state.partial.Content)
			state.contentIndex[index] = position
			toolCall := &ToolCall{ID: call.ID, Name: call.Name, Arguments: map[string]any{}}
			state.partial.Content = append(state.partial.Content, ContentBlock{Type: ContentToolCall, ToolCall: toolCall})
			stream.Emit(AssistantMessageEvent{Type: EventToolCallStart, ContentIndex: position, Partial: cloneAssistantMessage(state.partial)})
			call.Emitted = true
		}
	case "contentBlockDelta":
		delta := nestedMap(payload, "delta")
		if text, ok := delta["text"].(string); ok {
			bedrockTextDelta(stream, state, numberValue(payload["contentBlockIndex"]), text)
		} else if reasoning := nestedMap(delta, "reasoningContent"); reasoning != nil {
			if text, ok := reasoning["text"].(string); ok {
				bedrockThinkingDelta(stream, state, numberValue(payload["contentBlockIndex"]), text)
			}
		} else if toolUse := nestedMap(delta, "toolUse"); toolUse != nil {
			index := numberValue(payload["contentBlockIndex"])
			call := state.toolCalls[index]
			if call == nil {
				call = &bedrockToolCallState{}
				state.toolCalls[index] = call
			}
			partial, ok := toolUse["input"].(string)
			if !ok {
				return true
			}
			call.Arguments += partial
			emitBedrockToolArgumentDelta(stream, state, index, call, partial)
		}
	case "contentBlockStop":
		index := numberValue(payload["contentBlockIndex"])
		if position, ok := state.contentIndex[index]; ok && position < len(state.partial.Content) {
			block := state.partial.Content[position]
			if block.Type == ContentText {
				stream.Emit(AssistantMessageEvent{Type: EventTextEnd, ContentIndex: position, Content: block.Text, Partial: cloneAssistantMessage(state.partial)})
			}
			if block.Type == ContentThinking {
				stream.Emit(AssistantMessageEvent{Type: EventThinkingEnd, ContentIndex: position, Content: block.Thinking, Partial: cloneAssistantMessage(state.partial)})
			}
		}
		if call := state.toolCalls[index]; call != nil {
			applyBedrockToolArguments(stream, state, index, call)
		}
	case "metadata":
		if usage := usageFromBedrock(payload); usage != nil {
			state.partial.Usage = usage
		}
	case "messageStop":
		doneReason := bedrockDoneReason(stringValue(payload["stopReason"]))
		state.doneReason = doneReason
		switch doneReason {
		case DoneReasonToolCalls:
			state.partial.StopReason = StopReasonToolCalls
		case DoneReasonLength:
			state.partial.StopReason = StopReasonMaxTokens
		default:
			state.partial.StopReason = StopReasonEndTurn
		}
	}
	return true
}

func emitBedrockDone(stream *AssistantMessageEventStream, state *bedrockStreamState) {
	reason := state.doneReason
	if reason == "" {
		reason = DoneReasonStop
	}
	stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: reason, Message: cloneAssistantMessage(state.partial)})
}

func bedrockDoneReason(stopReason string) DoneReason {
	switch stopReason {
	case "tool_use":
		return DoneReasonToolCalls
	case "max_tokens":
		return DoneReasonLength
	default:
		return DoneReasonStop
	}
}

func bedrockTextDelta(stream *AssistantMessageEventStream, state *bedrockStreamState, blockIndex float64, text string) {
	position, ok := state.contentIndex[blockIndex]
	if !ok {
		position = len(state.partial.Content)
		state.contentIndex[blockIndex] = position
		state.partial.Content = append(state.partial.Content, ContentBlock{Type: ContentText})
	}
	if position < len(state.partial.Content) && state.partial.Content[position].Type == ContentText && state.partial.Content[position].Text == "" {
		stream.Emit(AssistantMessageEvent{Type: EventTextStart, ContentIndex: position, Partial: cloneAssistantMessage(state.partial)})
	}
	if position >= len(state.partial.Content) {
		return
	}
	if state.partial.Content[position].Type == ContentText {
		state.partial.Content[position].Text += text
	}
	stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: position, Delta: text, Partial: cloneAssistantMessage(state.partial)})
}

func bedrockThinkingDelta(stream *AssistantMessageEventStream, state *bedrockStreamState, blockIndex float64, text string) {
	position, ok := state.contentIndex[blockIndex]
	if !ok {
		position = len(state.partial.Content)
		state.contentIndex[blockIndex] = position
		state.partial.Content = append(state.partial.Content, ContentBlock{Type: ContentThinking})
	}
	if position < len(state.partial.Content) && state.partial.Content[position].Type == ContentThinking && state.partial.Content[position].Thinking == "" {
		stream.Emit(AssistantMessageEvent{Type: EventThinkingStart, ContentIndex: position, Partial: cloneAssistantMessage(state.partial)})
	}
	if position >= len(state.partial.Content) {
		return
	}
	if state.partial.Content[position].Type == ContentThinking {
		state.partial.Content[position].Thinking += text
	}
	stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, ContentIndex: position, Delta: text, Partial: cloneAssistantMessage(state.partial)})
}

func applyBedrockToolArguments(stream *AssistantMessageEventStream, state *bedrockStreamState, blockIndex float64, call *bedrockToolCallState) {
	if call == nil {
		return
	}
	args := map[string]any{}
	if call.Arguments != "" {
		parsed, ok := parsePartialJSONObject(call.Arguments)
		if ok {
			args = parsed
		}
	}
	position, ok := state.contentIndex[blockIndex]
	if !ok || position >= len(state.partial.Content) || state.partial.Content[position].ToolCall == nil {
		return
	}
	updated := *state.partial.Content[position].ToolCall
	updated.Arguments = args
	state.partial.Content[position].ToolCall = &updated
	state.partial.ToolCalls = append(state.partial.ToolCalls, updated)
	call.Arguments = ""
	stream.Emit(AssistantMessageEvent{Type: EventToolCallEnd, ContentIndex: position, ToolCall: &updated, Partial: cloneAssistantMessage(state.partial)})
}

func emitBedrockToolArgumentDelta(stream *AssistantMessageEventStream, state *bedrockStreamState, blockIndex float64, call *bedrockToolCallState, delta string) {
	position, ok := state.contentIndex[blockIndex]
	if !ok {
		return
	}
	stream.Emit(AssistantMessageEvent{Type: EventToolCallDelta, ContentIndex: position, Delta: delta, Partial: cloneAssistantMessage(state.partial)})
}

func nestedMap(payload map[string]any, key string) map[string]any {
	value, _ := payload[key].(map[string]any)
	return value
}

func hasKey(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	_, ok := payload[key]
	return ok
}

func numberValue(value any) float64 {
	switch number := value.(type) {
	case float64:
		if number < 0 || math.Trunc(number) != number {
			return 0
		}
		return number
	case json.Number:
		parsed, err := number.Int64()
		if err == nil && parsed >= 0 {
			return float64(parsed)
		}
	}
	return 0
}

func usageFromBedrock(payload map[string]any) *Usage {
	usageMap, _ := payload["usage"].(map[string]any)
	if usageMap == nil {
		return nil
	}
	usage := &Usage{InputTokens: uintNumber(usageMap["inputTokens"]), OutputTokens: uintNumber(usageMap["outputTokens"]), CacheReadTokens: uintNumber(usageMap["cacheReadInputTokens"]), CacheWriteTokens: uintNumber(usageMap["cacheWriteInputTokens"])}
	usage.TotalTokenCount = usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	usage.HasTotalTokens = true
	return usage
}

func uintNumber(value any) int {
	number, _ := uintNumberPresent(value)
	return number
}

func uintNumberPresent(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		if number < 0 || math.Trunc(number) != number {
			return 0, false
		}
		return int(number), true
	case int:
		if number < 0 {
			return 0, false
		}
		return number, true
	case json.Number:
		parsed, err := number.Int64()
		if err != nil || parsed < 0 {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func BuildBedrockConverseStreamURL(baseURL, modelID string) string {
	return strings.TrimRight(baseURL, "/") + "/model/" + modelID + "/converse-stream"
}

func BuildBedrockInvokeURL(baseURL, modelID string) string {
	return strings.TrimRight(baseURL, "/") + "/model/" + modelID + "/invoke"
}

func BuildBedrockInvokeStreamURL(baseURL, modelID string) string {
	return strings.TrimRight(baseURL, "/") + "/model/" + modelID + "/invoke-with-response-stream"
}

func truncateString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func BuildBedrockRequestBody(request Context, options StreamOptions) (map[string]any, error) {
	body := map[string]any{"messages": ConvertMessagesForBedrock(request.Messages)}
	if system := bedrockSystem(request); len(system) > 0 {
		body["system"] = system
	}
	inferenceConfig := map[string]any{"maxTokens": 4096}
	if options.MaxTokens > 0 {
		inferenceConfig["maxTokens"] = options.MaxTokens
	}
	if options.Temperature != nil {
		inferenceConfig["temperature"] = *options.Temperature
	}
	body["inferenceConfig"] = inferenceConfig
	if len(request.Tools) > 0 {
		body["toolConfig"] = map[string]any{"tools": BedrockTools(request.Tools)}
	}
	return body, nil
}

func bedrockSystem(request Context) []map[string]any {
	if request.HasSystemPrompt || request.SystemPrompt != "" {
		return []map[string]any{{"text": request.SystemPrompt}}
	}
	return nil
}

func ConvertMessagesForBedrock(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case RoleSystem:
			continue
		case RoleUser:
			out = append(out, map[string]any{"role": "user", "content": bedrockUserContent(message.Content)})
		case RoleAssistant:
			content := bedrockAssistantContent(message.Content)
			if len(content) > 0 {
				out = append(out, map[string]any{"role": "assistant", "content": content})
			}
		case RoleTool:
			out = append(out, map[string]any{"role": "user", "content": []map[string]any{{"toolResult": map[string]any{"toolUseId": message.ToolCallID, "content": bedrockToolResultContent(message.Content), "status": bedrockToolStatus(message)}}}})
		}
	}
	return out
}

func bedrockToolResultContent(blocks []ContentBlock) []map[string]any {
	content := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentText:
			content = append(content, map[string]any{"text": block.Text})
		}
	}
	return content
}

func bedrockUserContent(blocks []ContentBlock) []map[string]any {
	content := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentText:
			content = append(content, map[string]any{"text": block.Text})
		case ContentImage:
			content = append(content, bedrockImageContent(block))
		}
	}
	return content
}

func bedrockImageContent(block ContentBlock) map[string]any {
	format := "png"
	if strings.HasPrefix(block.MimeType, "image/") {
		format = strings.TrimPrefix(block.MimeType, "image/")
	}
	return map[string]any{"image": map[string]any{"format": format, "source": map[string]any{"bytes": block.Data}}}
}

func bedrockAssistantContent(blocks []ContentBlock) []map[string]any {
	content := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentText:
			content = append(content, map[string]any{"text": block.Text})
		case ContentToolCall:
			if block.ToolCall != nil {
				content = append(content, map[string]any{"toolUse": map[string]any{"toolUseId": block.ToolCall.ID, "name": block.ToolCall.Name, "input": block.ToolCall.Arguments}})
			}
		}
	}
	return content
}

func BedrockTools(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"toolSpec": map[string]any{"name": tool.Name, "description": tool.Description, "inputSchema": map[string]any{"json": tool.Parameters}}})
	}
	return out
}

func bedrockToolStatus(message Message) string {
	if message.IsError {
		return "error"
	}
	return "success"
}
