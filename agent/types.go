package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/detailyang/pig/ai"
)

type ToolExecutionMode string

const (
	ToolExecutionSequential ToolExecutionMode = "sequential"
	ToolExecutionParallel   ToolExecutionMode = "parallel"
	ToolExecutionAuto       ToolExecutionMode = "auto"
	ToolExecutionManual     ToolExecutionMode = "manual"

	ToolExecutionModeSequential = ToolExecutionSequential
	ToolExecutionModeParallel   = ToolExecutionParallel
)

func (mode ToolExecutionMode) MarshalJSON() ([]byte, error) {
	switch mode {
	case ToolExecutionSequential, ToolExecutionParallel:
		return json.Marshal(string(mode))
	default:
		return nil, fmt.Errorf("agent tool execution mode unknown upstream value %q", mode)
	}
}

func (mode *ToolExecutionMode) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "sequential", "parallel":
		*mode = ToolExecutionMode(value)
	default:
		return fmt.Errorf("agent tool execution mode unknown upstream value %q", value)
	}
	return nil
}

type QueueMode string

const (
	QueueAll        QueueMode = "all"
	QueueOneAtATime QueueMode = "one_at_a_time"
	QueueAppend     QueueMode = "append"
	QueueReplace    QueueMode = "replace"

	QueueModeAll        = QueueAll
	QueueModeOneAtATime = QueueOneAtATime
)

func (mode QueueMode) MarshalJSON() ([]byte, error) {
	switch mode {
	case QueueAll:
		return json.Marshal("all")
	case QueueOneAtATime:
		return json.Marshal("one-at-a-time")
	default:
		return nil, fmt.Errorf("agent queue mode unknown upstream value %q", mode)
	}
}

func (mode *QueueMode) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "all":
		*mode = QueueAll
	case "one-at-a-time":
		*mode = QueueOneAtATime
	default:
		return fmt.Errorf("agent queue mode unknown value %q", value)
	}
	return nil
}

type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"

	ThinkingLevelOff     = ThinkingOff
	ThinkingLevelMinimal = ThinkingMinimal
	ThinkingLevelLow     = ThinkingLow
	ThinkingLevelMedium  = ThinkingMedium
	ThinkingLevelHigh    = ThinkingHigh
	ThinkingLevelXhigh   = ThinkingXHigh
)

func (level ThinkingLevel) AsStr() string {
	return string(level)
}

func (level ThinkingLevel) ToPieAI() (ai.ThinkingLevel, bool) {
	switch level {
	case ThinkingMinimal:
		return ai.ThinkingMinimal, true
	case ThinkingLow:
		return ai.ThinkingLow, true
	case ThinkingMedium:
		return ai.ThinkingMedium, true
	case ThinkingHigh:
		return ai.ThinkingHigh, true
	case ThinkingXHigh:
		return ai.ThinkingXHigh, true
	default:
		return "", false
	}
}

type MessageKind string

const (
	MessageKindLLM        MessageKind = "llm"
	MessageKindCustom     MessageKind = "custom"
	MessageKindToolResult MessageKind = "tool_result"
)

type CustomMessage struct {
	Role      string
	Timestamp int64
	Payload   any
}

type ToolResult struct {
	CallID        string
	Name          string
	Content       string
	ContentBlocks []ai.ContentBlock
	Details       map[string]any
	DetailsValue  any
	Error         string
	IsError       bool
	Terminate     *bool
}

func (result ToolResult) MarshalJSON() ([]byte, error) {
	object := map[string]any{
		"content": toolResultValueContentBlocks(&result),
		"details": toolResultDetailsValue(&result),
	}
	if result.Terminate != nil {
		object["terminate"] = result.Terminate
	}
	return marshalJSONNoHTMLEscape(object)
}

func (result *ToolResult) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	_, hasUpstreamContent := object["content"]
	_, hasLegacyContent := object["Content"]
	_, hasLegacyContentBlocks := object["ContentBlocks"]
	if !hasUpstreamContent && !hasLegacyContent && !hasLegacyContentBlocks {
		return fmt.Errorf("agent tool result missing required field %q", "content")
	}
	if rawContent, ok := object["content"]; ok && bytes.Equal(bytes.TrimSpace(rawContent), []byte("null")) {
		return fmt.Errorf("agent tool result field %q must not be null", "content")
	}
	var wire struct {
		CallID        string                `json:"callID"`
		Name          string                `json:"name"`
		Content       []ai.UserContentBlock `json:"content"`
		ContentText   string                `json:"Content"`
		ContentBlocks []ai.ContentBlock     `json:"ContentBlocks"`
		Details       map[string]any        `json:"Details"`
		DetailsValue  any                   `json:"details"`
		Error         string                `json:"error"`
		IsError       bool                  `json:"isError"`
		Terminate     *bool                 `json:"terminate"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	blocks := contentBlocksFromUserContentBlocks(wire.Content)
	if len(blocks) == 0 {
		blocks = wire.ContentBlocks
	}
	if len(blocks) == 0 && wire.ContentText != "" {
		blocks = []ai.ContentBlock{{Type: ai.ContentText, Text: wire.ContentText}}
	}
	result.CallID = wire.CallID
	result.Name = wire.Name
	result.ContentBlocks = userContentBlocks(blocks)
	result.Content = textFromContentBlocks(result.ContentBlocks)
	result.DetailsValue = wire.DetailsValue
	if result.DetailsValue == nil && wire.Details != nil {
		result.Details = wire.Details
		result.DetailsValue = wire.Details
	} else if details, ok := result.DetailsValue.(map[string]any); ok {
		result.Details = details
	} else {
		result.Details = nil
	}
	result.Error = wire.Error
	result.IsError = wire.IsError
	result.Terminate = wire.Terminate
	return nil
}

type Message struct {
	Kind       MessageKind
	LLM        *ai.Message
	Custom     *CustomMessage
	ToolResult *ToolResult
}

type AgentMessage = Message

func (message Message) AsLLM() *ai.Message {
	if message.Kind != MessageKindLLM {
		return nil
	}
	return message.LLM
}

func (message Message) AsLlm() *ai.Message { return message.AsLLM() }

func (message Message) ToPieAi() *ai.Message { return message.AsLLM() }

func (message Message) MarshalJSON() ([]byte, error) {
	switch message.Kind {
	case MessageKindLLM:
		if message.LLM == nil {
			return nil, fmt.Errorf("agent llm message missing payload")
		}
		return marshalJSONNoHTMLEscape(llmMessageWire(message.LLM))
	case MessageKindCustom:
		if message.Custom == nil {
			return nil, fmt.Errorf("agent custom message missing payload")
		}
		payload := map[string]any{"role": message.Custom.Role, "timestamp": message.Custom.Timestamp}
		if fields, ok := message.Custom.Payload.(map[string]any); ok {
			for key, value := range fields {
				payload[key] = value
			}
		} else if message.Custom.Payload != nil {
			return nil, fmt.Errorf("agent custom message payload must be an object for flattened serialization")
		}
		return marshalJSONNoHTMLEscape(payload)
	case MessageKindToolResult:
		if message.ToolResult == nil {
			return nil, fmt.Errorf("agent tool result message missing payload")
		}
		return marshalJSONNoHTMLEscape(toolResultMessageWire(message.ToolResult))
	default:
		return nil, fmt.Errorf("agent message unknown kind %q", message.Kind)
	}
}

func toolResultMessageWire(result *ToolResult) map[string]any {
	message := ai.Message{
		Role:         ai.RoleTool,
		ToolCallID:   result.CallID,
		ToolName:     result.Name,
		Name:         result.Name,
		Content:      toolResultContentBlocks(result),
		Details:      result.Details,
		DetailsValue: toolResultDetailsValue(result),
		IsError:      result.IsError || result.Error != "",
	}
	data, err := marshalJSONNoHTMLEscape(message)
	if err != nil {
		return map[string]any{}
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return map[string]any{}
	}
	return object
}

func toolResultContentBlocks(result *ToolResult) []ai.ContentBlock {
	return userContentBlocks(toolResultRawContentBlocks(result))
}

func toolResultValueContentBlocks(result *ToolResult) []ai.ContentBlock {
	if len(result.ContentBlocks) > 0 {
		return userContentBlocks(result.ContentBlocks)
	}
	if result.Content != "" {
		return []ai.ContentBlock{{Type: ai.ContentText, Text: result.Content}}
	}
	return []ai.ContentBlock{}
}

func toolResultRawContentBlocks(result *ToolResult) []ai.ContentBlock {
	if len(result.ContentBlocks) > 0 {
		return result.ContentBlocks
	}
	return []ai.ContentBlock{{Type: ai.ContentText, Text: result.Content}}
}

func userContentBlocks(blocks []ai.ContentBlock) []ai.ContentBlock {
	out := make([]ai.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == ai.ContentText || block.Type == ai.ContentImage {
			out = append(out, block)
		}
	}
	return out
}

func contentBlocksFromUserContentBlocks(blocks []ai.UserContentBlock) []ai.ContentBlock {
	out := make([]ai.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ai.UserContentText:
			out = append(out, ai.ContentBlock{Type: ai.ContentText, Text: block.Text, TextSignature: block.TextSignature})
		case ai.UserContentImage:
			out = append(out, ai.ContentBlock{Type: ai.ContentImage, Data: block.Data, MimeType: block.MimeType})
		}
	}
	return out
}

func textFromContentBlocks(blocks []ai.ContentBlock) string {
	var text string
	for _, block := range blocks {
		if block.Type == ai.ContentText {
			text += block.Text
		}
	}
	return text
}

func toolResultDetailsValue(result *ToolResult) any {
	if result.DetailsValue != nil {
		return result.DetailsValue
	}
	return result.Details
}

func llmMessageWire(message *ai.Message) any {
	if message == nil {
		return nil
	}
	data, err := marshalJSONNoHTMLEscape(message)
	if err != nil {
		return message
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return message
	}
	return object
}

func (message *Message) UnmarshalJSON(data []byte) error {
	var wrapper struct {
		Kind       MessageKind    `json:"Kind"`
		LLM        *ai.Message    `json:"LLM"`
		Custom     *CustomMessage `json:"Custom"`
		ToolResult *ToolResult    `json:"ToolResult"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	if wrapper.Kind != "" {
		*message = Message{Kind: wrapper.Kind, LLM: wrapper.LLM, Custom: wrapper.Custom, ToolResult: wrapper.ToolResult}
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	var role string
	if err := json.Unmarshal(object["role"], &role); err != nil {
		return err
	}
	if role == "toolResult" {
		var llm ai.Message
		if err := json.Unmarshal(data, &llm); err == nil {
			text := textFromContentBlocks(llm.Content)
			*message = Message{Kind: MessageKindToolResult, ToolResult: &ToolResult{CallID: llm.ToolCallID, Name: llm.ToolName, Content: text, ContentBlocks: llm.Content, Details: llm.Details, DetailsValue: llm.DetailsValue, IsError: llm.IsError}}
			return nil
		}
	}
	if role == string(ai.RoleUser) || role == string(ai.RoleAssistant) {
		var llm ai.Message
		if err := json.Unmarshal(data, &llm); err == nil {
			*message = Message{Kind: MessageKindLLM, LLM: &llm}
			return nil
		}
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	delete(payload, "role")
	timestamp := int64(0)
	if rawTimestamp, ok := payload["timestamp"].(json.Number); ok {
		parsedTimestamp, err := rawTimestamp.Int64()
		if err != nil {
			return err
		}
		timestamp = parsedTimestamp
		delete(payload, "timestamp")
	} else if rawTimestamp, ok := payload["timestamp"].(float64); ok {
		timestamp = int64(rawTimestamp)
		delete(payload, "timestamp")
	} else {
		return fmt.Errorf("agent custom message missing required field %q", "timestamp")
	}
	*message = Message{Kind: MessageKindCustom, Custom: &CustomMessage{Role: role, Timestamp: timestamp, Payload: payload}}
	return nil
}

func NewUserMessage(text string) Message {
	message := ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: text}}}
	return Message{Kind: MessageKindLLM, LLM: &message}
}

func user_message(text string) Message {
	return NewUserMessage(text)
}

func NewAssistantMessage(text string) Message {
	message := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: text}}}
	return Message{Kind: MessageKindLLM, LLM: &message}
}

func NewToolResultMessage(result ToolResult) Message {
	copyResult := result
	return Message{Kind: MessageKindToolResult, ToolResult: &copyResult}
}

func DefaultConvertToLLM(messages []Message) []ai.Message {
	converted := make([]ai.Message, 0, len(messages))
	for _, message := range messages {
		switch message.Kind {
		case MessageKindLLM:
			if message.LLM != nil {
				converted = append(converted, *message.LLM)
			}
		case MessageKindToolResult:
			if message.ToolResult != nil {
				converted = append(converted, ai.Message{
					Role:         ai.RoleTool,
					ToolCallID:   message.ToolResult.CallID,
					ToolName:     message.ToolResult.Name,
					Name:         message.ToolResult.Name,
					Details:      message.ToolResult.Details,
					DetailsValue: toolResultDetailsValue(message.ToolResult),
					IsError:      message.ToolResult.IsError || message.ToolResult.Error != "",
					Content:      toolResultContentBlocks(message.ToolResult),
				})
			}
		}
	}
	return converted
}

func DefaultConvertToLlm(messages []Message) []ai.Message { return DefaultConvertToLLM(messages) }

func DefaultConvertToLLMFn() ConvertToLLM {
	return DefaultConvertToLLM
}

type ToolUpdateFunc func(ToolResult)

type AgentToolUpdate = ToolUpdateFunc

type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error)
}

type AgentTool = Tool

type AgentToolCall = ai.ToolCall

type AgentToolResult = ToolResult

type AgentToolError struct {
	message string
	cause   error
}

func NewAgentToolError(message string) AgentToolError {
	return AgentToolError{message: message}
}

func WrapAgentToolError(err error) AgentToolError {
	return AgentToolError{cause: err}
}

func AgentToolErrorOther(err any) AgentToolError {
	switch value := err.(type) {
	case AgentToolError:
		return value
	case error:
		return WrapAgentToolError(value)
	case string:
		return NewAgentToolError(value)
	default:
		return NewAgentToolError(fmt.Sprint(value))
	}
}

func (err AgentToolError) Error() string {
	if err.cause != nil {
		return err.cause.Error()
	}
	return err.message
}

func (err AgentToolError) Unwrap() error {
	return err.cause
}

type ToolExecutionModeOverride interface {
	ExecutionMode() ToolExecutionMode
}

type ToolArgumentPreparer interface {
	PrepareArguments(arguments map[string]any) map[string]any
}

type ToolArgumentValuePreparer interface {
	PrepareArgumentsValue(arguments any) any
}

type ToolPermissionClassifier interface {
	PermissionClassification(arguments map[string]any) PermissionClassification
}

type ToolPermissionValueClassifier interface {
	PermissionClassificationValue(arguments any) PermissionClassification
}

type ToolPermissionReasoner interface {
	PermissionReason(arguments map[string]any) string
}

type ToolPermissionValueReasoner interface {
	PermissionReasonValue(arguments any) string
}

type ToolDefinition interface {
	Parameters() any
}

type ToolObjectDefinition interface {
	Parameters() map[string]any
}

func ToolSpecs(tools []Tool) []ai.Tool {
	specs := make([]ai.Tool, 0, len(tools))
	for _, tool := range tools {
		spec := ai.Tool{Name: tool.Name(), Description: tool.Description()}
		if defined, ok := tool.(ToolObjectDefinition); ok {
			spec.Parameters = defined.Parameters()
		} else if defined, ok := tool.(ToolDefinition); ok {
			spec.Parameters = defined.Parameters()
		}
		specs = append(specs, spec)
	}
	return specs
}

type AgentContext struct {
	Context         context.Context
	State           State
	SystemPrompt    string
	HasSystemPrompt bool
	Messages        []Message
	Tools           []Tool
}

type State struct {
	SystemPrompt     string
	Model            *ai.Model
	ThinkingLevel    *ai.ThinkingLevel
	Tools            []Tool
	Messages         []Message
	StreamingMessage *Message
	PendingToolCalls []string
	IsStreaming      bool
	Running          bool
	ErrorMessage     string
}

type AgentState = State

type EventType string

const (
	EventTypeStart                      EventType = "start"
	EventTypeTurnStart                  EventType = "turn_start"
	EventTypeTurnEnd                    EventType = "turn_end"
	EventTypeMessageStart               EventType = "message_start"
	EventTypeMessageUpdate              EventType = "message_update"
	EventTypeMessageEnd                 EventType = "message_end"
	EventTypeAssistant                  EventType = "assistant"
	EventTypeToolCall                   EventType = "tool_call"
	EventTypeToolExecutionStart         EventType = "tool_execution_start"
	EventTypeToolUpdate                 EventType = "tool_update"
	EventTypeToolExecutionEnd           EventType = "tool_execution_end"
	EventTypeToolResult                 EventType = "tool_result"
	EventTypeSystemPrompt               EventType = "system_prompt"
	EventTypeControlPlanePromptResolved EventType = "control_plane_prompt_resolved"
	EventTypeError                      EventType = "error"
	EventTypeDone                       EventType = "done"

	EventTypeAgentStart          = EventTypeStart
	EventTypeAgentEnd            = EventTypeDone
	EventTypeToolExecutionUpdate = EventTypeToolUpdate

	AgentEventControlPlanePromptResolved = EventTypeControlPlanePromptResolved
	AgentEventToolExecutionStart         = EventTypeToolExecutionStart
	AgentEventToolExecutionUpdate        = EventTypeToolUpdate
	AgentEventToolExecutionEnd           = EventTypeToolExecutionEnd
)

type Event struct {
	Type                       EventType                  `json:"type"`
	Message                    *Message                   `json:"message,omitempty"`
	Messages                   []Message                  `json:"messages,omitempty"`
	ToolResults                []ToolResult               `json:"toolResults,omitempty"`
	AssistantMessage           *ai.AssistantMessage       `json:"assistantMessage,omitempty"`
	AssistantMessageEvent      *ai.AssistantMessageEvent  `json:"assistantMessageEvent,omitempty"`
	LLMMessage                 *ai.Message                `json:"llmMessage,omitempty"`
	ToolCall                   *ai.ToolCall               `json:"toolCall,omitempty"`
	ToolArgs                   any                        `json:"toolArgs,omitempty"`
	ToolResult                 *ToolResult                `json:"toolResult,omitempty"`
	IsError                    bool                       `json:"isError,omitempty"`
	ControlPlanePrompt         *ControlPlanePromptRequest `json:"controlPlanePrompt,omitempty"`
	ControlPlanePromptDecision ControlPlanePromptDecision `json:"controlPlanePromptDecision,omitempty"`
	ControlPlanePromptReason   string                     `json:"controlPlanePromptReason,omitempty"`
	Error                      error                      `json:"error,omitempty"`
}

type AgentEvent = Event

func (event Event) MarshalJSON() ([]byte, error) {
	if event.Type == EventTypeControlPlanePromptResolved {
		object := map[string]any{
			"type":     event.Type,
			"decision": event.ControlPlanePromptDecision,
		}
		if event.ControlPlanePrompt != nil {
			object["toolCallId"] = event.ControlPlanePrompt.ToolCallID
			object["toolName"] = event.ControlPlanePrompt.ToolName
			object["argsHash"] = event.ControlPlanePrompt.ArgsHash
			object["label"] = event.ControlPlanePrompt.Label
		}
		if event.ControlPlanePromptReason != "" {
			object["reason"] = event.ControlPlanePromptReason
		}
		return marshalJSONNoHTMLEscape(object)
	}
	if event.Type == EventTypeToolExecutionStart || event.Type == EventTypeToolUpdate || event.Type == EventTypeToolExecutionEnd {
		object := map[string]any{"type": event.Type}
		if event.ToolCall != nil {
			object["toolCallId"] = event.ToolCall.ID
			object["toolName"] = event.ToolCall.Name
		}
		if event.Type == EventTypeToolExecutionStart || event.Type == EventTypeToolUpdate {
			object["args"] = event.ToolArgs
		}
		if event.Type == EventTypeToolUpdate && event.ToolResult != nil {
			object["partialResult"] = event.ToolResult
		}
		if event.Type == EventTypeToolExecutionEnd {
			if event.ToolResult != nil {
				object["result"] = event.ToolResult
			}
			object["isError"] = event.IsError
		}
		return marshalJSONNoHTMLEscape(object)
	}
	type alias Event
	return marshalJSONNoHTMLEscape(alias(event))
}

type StreamFunc func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error)

type StreamFn = StreamFunc

func DefaultStreamFn() StreamFn {
	return func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		return ai.StreamSimple(ctx, model, ai.ContextFromMessages(messages, tools), options), nil
	}
}

type ConvertToLLM func([]Message) []ai.Message

type ConvertToLlm = ConvertToLLM

type TransformAgentContext func(context.Context, []Message) ([]Message, error)

type TransformContext func(context.Context, []ai.Message) ([]ai.Message, error)

type GetAPIKey func(context.Context, ai.Provider) (string, bool)

type GetApiKey = GetAPIKey

type BeforeToolCallContext struct {
	AssistantMessage ai.AssistantMessage
	Call             ai.ToolCall
	ToolCall         ai.ToolCall
	Args             any
	AgentContext     AgentContext
	Context          AgentContext
}

type BeforeToolCallResult struct {
	Block  bool
	Reason string
	Skip   bool
	Result *ToolResult
	Prompt *ControlPlanePromptRequest
}

type BeforeToolCallHook func(context.Context, BeforeToolCallContext) (BeforeToolCallResult, error)

type AfterToolCallContext struct {
	AssistantMessage ai.AssistantMessage
	Call             ai.ToolCall
	ToolCall         ai.ToolCall
	Args             any
	Result           ToolResult
	IsError          bool
	AgentContext     AgentContext
	Context          AgentContext
}

type AfterToolCallResult struct {
	Content          *string
	ContentBlocks    []ai.ContentBlock
	ContentBlocksSet bool
	Details          map[string]any
	DetailsValue     any
	DetailsValueSet  bool
	IsError          *bool
	Terminate        *bool
}

type AfterToolCallHook func(context.Context, AfterToolCallContext) (AfterToolCallResult, error)

type ControlPlanePromptRequest struct {
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	ArgsHash   string `json:"argsHash"`
	Label      string `json:"label"`
	Payload    any    `json:"payload"`
	Reason     string `json:"reason"`
}

type OnControlPlanePromptHook func(context.Context, ControlPlanePromptRequest) (ControlPlanePromptDecision, error)

type ShouldStopAfterTurnContext struct {
	State        State
	Message      ai.AssistantMessage
	ToolResults  []ToolResult
	AgentContext AgentContext
	Context      AgentContext
	NewMessages  []Message
}

type ShouldStopHook func(context.Context, ShouldStopAfterTurnContext) (bool, error)

type PrepareNextTurnContext = ShouldStopAfterTurnContext

type AgentLoopTurnUpdate struct {
	Context       *AgentContext
	Messages      []Message
	SystemPrompt  *string
	Model         *ai.Model
	ThinkingLevel *ai.ThinkingLevel
}

type PrepareNextTurnHook func(context.Context, PrepareNextTurnContext) (*AgentLoopTurnUpdate, error)

type MessageQueueProvider func(context.Context) ([]Message, error)

type Config struct {
	SimpleOptions         ai.SimpleStreamOptions
	ConvertToLLM          ConvertToLLM
	TransformAgentContext TransformAgentContext
	TransformContext      TransformContext
	GetAPIKey             GetAPIKey
	BeforeToolCall        BeforeToolCallHook
	AfterToolCall         AfterToolCallHook
	OnControlPlanePrompt  OnControlPlanePromptHook
	ShouldStopAfterTurn   ShouldStopHook
	PrepareNextTurn       PrepareNextTurnHook
	GetSteeringMessages   MessageQueueProvider
	GetFollowUpMessages   MessageQueueProvider
	ToolExecution         ToolExecutionMode
}

type AgentLoopConfig = Config

type PermissionClassification string

const (
	PermissionAllow  PermissionClassification = "allow"
	PermissionPrompt PermissionClassification = "prompt"
	PermissionBlock  PermissionClassification = "block"
	PermissionAsk    PermissionClassification = PermissionPrompt
	PermissionDeny   PermissionClassification = PermissionBlock

	PermissionClassificationAllow  = PermissionAllow
	PermissionClassificationPrompt = PermissionPrompt
	PermissionClassificationBlock  = PermissionBlock
)

type ControlPlanePromptDecision string

const (
	ControlPlaneAllow   ControlPlanePromptDecision = "allow"
	ControlPlaneDeny    ControlPlanePromptDecision = "deny"
	ControlPlaneTimeout ControlPlanePromptDecision = "timeout"

	ControlPlanePromptDecisionAllow   = ControlPlaneAllow
	ControlPlanePromptDecisionDeny    = ControlPlaneDeny
	ControlPlanePromptDecisionTimeout = ControlPlaneTimeout
)

func (decision ControlPlanePromptDecision) AsAuditString() string {
	switch decision {
	case ControlPlaneAllow, ControlPlaneDeny, ControlPlaneTimeout:
		return string(decision)
	default:
		return string(ControlPlaneDeny)
	}
}

func (decision ControlPlanePromptDecision) AsAuditStr() string {
	return decision.AsAuditString()
}

type ControlPlanePromptResolution struct {
	Decision ControlPlanePromptDecision
	Reason   string
}

var ErrToolNotFound = errors.New("agent tool not found")

var ErrAlreadyStreaming = errors.New("agent is already streaming")

type AgentRunError = error

var AgentRunErrorAlreadyStreaming = ErrAlreadyStreaming

func AgentRunErrorOther(message string) AgentRunError {
	return errors.New(message)
}

type PiAssistantMessage = ai.AssistantMessage
type PiImageContent = ai.ImageContent
type PiMessage = ai.Message
type PiTextContent = ai.TextContent
type PiToolResultMessage = ai.ToolResultMessage
