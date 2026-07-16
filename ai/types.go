package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type Api string

type KnownApi string

const (
	ApiAnthropic             Api = "anthropic-messages"
	ApiAnthropicMessages     Api = "anthropic-messages"
	ApiAnthropicLegacy       Api = "anthropic"
	ApiOpenAIResponses       Api = "openai-responses"
	ApiOpenAI                Api = "openai"
	ApiOpenAICompletions     Api = "openai-completions"
	ApiAzureOpenAIResponses  Api = "azure-openai-responses"
	ApiOpenAICodexResponses  Api = "openai-codex-responses"
	ApiMistral               Api = "mistral-conversations"
	ApiBedrockConverseStream Api = "bedrock-converse-stream"
	ApiGoogleGenerativeAI    Api = "google-generative-ai"
	ApiGoogleVertex          Api = "google-vertex"
	ApiFaux                  Api = "faux"
)

const (
	KnownApiOpenAICompletions     KnownApi = "openai-completions"
	KnownApiMistralConversations  KnownApi = "mistral-conversations"
	KnownApiOpenAIResponses       KnownApi = "openai-responses"
	KnownApiAzureOpenAIResponses  KnownApi = "azure-openai-responses"
	KnownApiOpenAICodexResponses  KnownApi = "openai-codex-responses"
	KnownApiAnthropicMessages     KnownApi = "anthropic-messages"
	KnownApiBedrockConverseStream KnownApi = "bedrock-converse-stream"
	KnownApiGoogleGenerativeAI    KnownApi = "google-generative-ai"
	KnownApiGoogleVertex          KnownApi = "google-vertex"
)

func NewKnownApi(api KnownApi) Api {
	return Api(api.String())
}

func Known(api KnownApi) Api {
	return NewKnownApi(api)
}

func (api KnownApi) String() string {
	return string(api)
}

func (api KnownApi) AsStr() string {
	return api.String()
}

func (api Api) AsStr() string { return string(api) }

type Provider string

func (provider Provider) AsStr() string { return string(provider) }

type ImagesApi string

type ImagesProvider string

func (provider ImagesProvider) AsStr() string { return string(provider) }

type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"

	ThinkingLevelMinimal = ThinkingMinimal
	ThinkingLevelLow     = ThinkingLow
	ThinkingLevelMedium  = ThinkingMedium
	ThinkingLevelHigh    = ThinkingHigh
	ThinkingLevelXhigh   = ThinkingXHigh
)

func (level ThinkingLevel) MarshalJSON() ([]byte, error) {
	if !isThinkingLevel(string(level)) {
		return nil, fmt.Errorf("ai thinking level unknown upstream value %q", level)
	}
	return json.Marshal(string(level))
}

func (level *ThinkingLevel) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if !isThinkingLevel(value) {
		return fmt.Errorf("ai thinking level unknown upstream value %q", value)
	}
	*level = ThinkingLevel(value)
	return nil
}

type ModelThinkingLevel string

const (
	ModelThinkingOff     ModelThinkingLevel = "off"
	ModelThinkingMinimal ModelThinkingLevel = "minimal"
	ModelThinkingLow     ModelThinkingLevel = "low"
	ModelThinkingMedium  ModelThinkingLevel = "medium"
	ModelThinkingHigh    ModelThinkingLevel = "high"
	ModelThinkingXHigh   ModelThinkingLevel = "xhigh"

	ModelThinkingLevelOff     = ModelThinkingOff
	ModelThinkingLevelMinimal = ModelThinkingMinimal
	ModelThinkingLevelLow     = ModelThinkingLow
	ModelThinkingLevelMedium  = ModelThinkingMedium
	ModelThinkingLevelHigh    = ModelThinkingHigh
	ModelThinkingLevelXhigh   = ModelThinkingXHigh
)

func (level ModelThinkingLevel) MarshalJSON() ([]byte, error) {
	if !isModelThinkingLevel(string(level)) {
		return nil, fmt.Errorf("ai model thinking level unknown value %q", level)
	}
	return json.Marshal(string(level))
}

func (level *ModelThinkingLevel) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if !isModelThinkingLevel(value) {
		return fmt.Errorf("ai model thinking level unknown value %q", value)
	}
	*level = ModelThinkingLevel(value)
	return nil
}

type ThinkingLevelMap map[ModelThinkingLevel]*string

func (levels ThinkingLevelMap) MarshalJSON() ([]byte, error) {
	object := make(map[string]*string, len(levels))
	for level, value := range levels {
		if !isModelThinkingLevel(string(level)) {
			return nil, fmt.Errorf("ai model thinkingLevelMap unknown key %q", level)
		}
		object[string(level)] = value
	}
	return marshalJSONNoHTMLEscape(object)
}

func (levels *ThinkingLevelMap) UnmarshalJSON(data []byte) error {
	var object map[string]*string
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	decoded := make(ThinkingLevelMap, len(object))
	for level, value := range object {
		if !isModelThinkingLevel(level) {
			return fmt.Errorf("ai model thinkingLevelMap unknown key %q", level)
		}
		decoded[ModelThinkingLevel(level)] = value
	}
	*levels = decoded
	return nil
}

type CacheRetention string

const (
	CacheNone      CacheRetention = "none"
	CacheShort     CacheRetention = "short"
	CacheEphemeral CacheRetention = "ephemeral"
	CacheLong      CacheRetention = "long"

	CacheRetentionNone  = CacheNone
	CacheRetentionShort = CacheShort
	CacheRetentionLong  = CacheLong
)

func (retention CacheRetention) MarshalJSON() ([]byte, error) {
	switch retention {
	case CacheNone, CacheShort, CacheLong:
		return json.Marshal(string(retention))
	default:
		return nil, fmt.Errorf("ai cache retention unknown upstream value %q", retention)
	}
}

func (retention *CacheRetention) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "none", "short", "long":
		*retention = CacheRetention(value)
	default:
		return fmt.Errorf("ai cache retention unknown upstream value %q", value)
	}
	return nil
}

type ThinkingBudgets struct {
	Minimal int `json:"minimal,omitempty"`
	Low     int `json:"low,omitempty"`
	Medium  int `json:"medium,omitempty"`
	High    int `json:"high,omitempty"`
	present thinkingBudgetsPresent
}

type thinkingBudgetsPresent struct {
	Minimal bool
	Low     bool
	Medium  bool
	High    bool
}

func (budgets ThinkingBudgets) MarshalJSON() ([]byte, error) {
	if budgets.Minimal < 0 {
		return nil, fmt.Errorf("ai thinking budget minimal cannot be negative")
	}
	if budgets.Low < 0 {
		return nil, fmt.Errorf("ai thinking budget low cannot be negative")
	}
	if budgets.Medium < 0 {
		return nil, fmt.Errorf("ai thinking budget medium cannot be negative")
	}
	if budgets.High < 0 {
		return nil, fmt.Errorf("ai thinking budget high cannot be negative")
	}
	object := map[string]int{}
	if budgets.present.Minimal || budgets.Minimal != 0 {
		object["minimal"] = budgets.Minimal
	}
	if budgets.present.Low || budgets.Low != 0 {
		object["low"] = budgets.Low
	}
	if budgets.present.Medium || budgets.Medium != 0 {
		object["medium"] = budgets.Medium
	}
	if budgets.present.High || budgets.High != 0 {
		object["high"] = budgets.High
	}
	return marshalJSONNoHTMLEscape(object)
}

func (budgets *ThinkingBudgets) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	var decoded ThinkingBudgets
	if raw, ok := object["minimal"]; ok && !isJSONNull(raw) {
		if err := json.Unmarshal(raw, &decoded.Minimal); err != nil {
			return err
		}
		if decoded.Minimal < 0 {
			return fmt.Errorf("ai thinking budget minimal cannot be negative")
		}
		decoded.present.Minimal = true
	}
	if raw, ok := object["low"]; ok && !isJSONNull(raw) {
		if err := json.Unmarshal(raw, &decoded.Low); err != nil {
			return err
		}
		if decoded.Low < 0 {
			return fmt.Errorf("ai thinking budget low cannot be negative")
		}
		decoded.present.Low = true
	}
	if raw, ok := object["medium"]; ok && !isJSONNull(raw) {
		if err := json.Unmarshal(raw, &decoded.Medium); err != nil {
			return err
		}
		if decoded.Medium < 0 {
			return fmt.Errorf("ai thinking budget medium cannot be negative")
		}
		decoded.present.Medium = true
	}
	if raw, ok := object["high"]; ok && !isJSONNull(raw) {
		if err := json.Unmarshal(raw, &decoded.High); err != nil {
			return err
		}
		if decoded.High < 0 {
			return fmt.Errorf("ai thinking budget high cannot be negative")
		}
		decoded.present.High = true
	}
	*budgets = decoded
	return nil
}

type Transport string

const (
	TransportSSE             Transport = "sse"
	TransportWebsocket       Transport = "websocket"
	TransportWebsocketCached Transport = "websocket-cached"
	TransportAuto            Transport = "auto"
	TransportHTTP            Transport = "http"
)

type ProviderResponse struct {
	Status  uint16
	Headers map[string]string
}

func (transport Transport) MarshalJSON() ([]byte, error) {
	switch transport {
	case TransportSSE, TransportWebsocket, TransportWebsocketCached, TransportAuto:
		return json.Marshal(string(transport))
	default:
		return nil, fmt.Errorf("ai transport unknown upstream value %q", transport)
	}
}

func (transport *Transport) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "sse", "websocket", "websocket-cached", "auto":
		*transport = Transport(value)
	default:
		return fmt.Errorf("ai transport unknown upstream value %q", value)
	}
	return nil
}

type StreamOptions struct {
	APIKey          string
	MaxTokens       int
	Temperature     *float64
	Transport       Transport
	CacheRetention  CacheRetention
	SessionID       string
	Headers         map[string]string
	TimeoutMS       int
	MaxRetries      *int
	MaxRetryDelayMS *int
	Metadata        map[string]any
	Abort           <-chan struct{}
	ProviderExtras  map[string]any
}

type SimpleStreamOptions struct {
	Base            StreamOptions
	Reasoning       ThinkingLevel
	ThinkingLevel   ThinkingLevel
	ThinkingBudgets ThinkingBudgets
}

func StreamOptionsFromSimple(options SimpleStreamOptions) StreamOptions {
	base := options.Base
	return base
}

func TranslateBaseStreamOptions(model Model, options SimpleStreamOptions) StreamOptions {
	_ = model
	return StreamOptionsFromSimple(options)
}

func TranslateBase(model Model, options SimpleStreamOptions) StreamOptions {
	return TranslateBaseStreamOptions(model, options)
}

func (options SimpleStreamOptions) ReasoningLevel() ThinkingLevel {
	if options.Reasoning != "" {
		return options.Reasoning
	}
	return options.ThinkingLevel
}

func (budgets ThinkingBudgets) BudgetFor(level ThinkingLevel) (int, bool) {
	switch level {
	case ThinkingMinimal:
		return budgets.Minimal, budgets.present.Minimal || budgets.Minimal != 0
	case ThinkingLow:
		return budgets.Low, budgets.present.Low || budgets.Low != 0
	case ThinkingMedium:
		return budgets.Medium, budgets.present.Medium || budgets.Medium != 0
	case ThinkingHigh, ThinkingXHigh:
		return budgets.High, budgets.present.High || budgets.High != 0
	default:
		return 0, false
	}
}

func isModelThinkingLevel(value string) bool {
	switch value {
	case "off", "minimal", "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func isThinkingLevel(value string) bool {
	switch value {
	case "minimal", "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

type ContentType string

const (
	ContentText     ContentType = "text"
	ContentThinking ContentType = "thinking"
	ContentImage    ContentType = "image"
	ContentToolCall ContentType = "tool_call"

	ContentBlockText     = ContentText
	ContentBlockThinking = ContentThinking
	ContentBlockImage    = ContentImage
	ContentBlockToolCall = ContentToolCall
)

type TextSignaturePhase string

const (
	TextSignatureCommentary  TextSignaturePhase = "commentary"
	TextSignatureFinalAnswer TextSignaturePhase = "final_answer"

	TextSignaturePhaseCommentary  = TextSignatureCommentary
	TextSignaturePhaseFinalAnswer = TextSignatureFinalAnswer
)

func (phase TextSignaturePhase) MarshalJSON() ([]byte, error) {
	switch phase {
	case TextSignatureCommentary, TextSignatureFinalAnswer:
		return json.Marshal(string(phase))
	default:
		return nil, fmt.Errorf("ai text signature phase unknown value %q", phase)
	}
}

func (phase *TextSignaturePhase) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "commentary", "final_answer":
		*phase = TextSignaturePhase(value)
	default:
		return fmt.Errorf("ai text signature phase unknown value %q", value)
	}
	return nil
}

type TextSignatureV1 struct {
	V     uint8              `json:"v"`
	ID    string             `json:"id"`
	Phase TextSignaturePhase `json:"phase,omitempty"`
}

func (signature *TextSignatureV1) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"v", "id"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai text signature missing required field %q", field)
		}
	}
	if err := rejectJSONNullFields(object, "text signature", "v", "id"); err != nil {
		return err
	}
	if rawPhase, ok := object["phase"]; ok && isJSONNull(rawPhase) {
		delete(object, "phase")
		var err error
		data, err = json.Marshal(object)
		if err != nil {
			return err
		}
	}
	type alias TextSignatureV1
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*signature = TextSignatureV1(decoded)
	return nil
}

type TextContent struct {
	Text                 string `json:"text"`
	TextSignature        string `json:"textSignature,omitempty"`
	textSignaturePresent bool
}

func (content TextContent) MarshalJSON() ([]byte, error) {
	object := map[string]any{"text": content.Text}
	if content.TextSignature != "" || content.textSignaturePresent {
		object["textSignature"] = content.TextSignature
	}
	return marshalJSONNoHTMLEscape(object)
}

func (content *TextContent) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if _, ok := object["text"]; !ok {
		return fmt.Errorf("ai text content missing required field %q", "text")
	}
	if err := rejectJSONNull(object, "text", "text content"); err != nil {
		return err
	}
	type alias TextContent
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if rawTextSignature, ok := object["textSignature"]; ok && !isJSONNull(rawTextSignature) {
		decoded.textSignaturePresent = true
	}
	*content = TextContent(decoded)
	return nil
}

type ThinkingContent struct {
	Thinking                 string `json:"thinking"`
	ThinkingSignature        string `json:"thinkingSignature,omitempty"`
	Redacted                 bool   `json:"redacted,omitempty"`
	thinkingSignaturePresent bool
}

func (content ThinkingContent) MarshalJSON() ([]byte, error) {
	object := map[string]any{"thinking": content.Thinking}
	if content.ThinkingSignature != "" || content.thinkingSignaturePresent {
		object["thinkingSignature"] = content.ThinkingSignature
	}
	if content.Redacted {
		object["redacted"] = true
	}
	return marshalJSONNoHTMLEscape(object)
}

func (content *ThinkingContent) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if _, ok := object["thinking"]; !ok {
		return fmt.Errorf("ai thinking content missing required field %q", "thinking")
	}
	if err := rejectJSONNull(object, "thinking", "thinking content"); err != nil {
		return err
	}
	if rawRedacted, ok := object["redacted"]; ok && isJSONNull(rawRedacted) {
		return fmt.Errorf("ai thinking content redacted cannot be null")
	}
	type alias ThinkingContent
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if rawThinkingSignature, ok := object["thinkingSignature"]; ok && !isJSONNull(rawThinkingSignature) {
		decoded.thinkingSignaturePresent = true
	}
	*content = ThinkingContent(decoded)
	return nil
}

type ImageContent struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

func (content *ImageContent) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"data", "mimeType"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai image content missing required field %q", field)
		}
	}
	if err := rejectJSONNullFields(object, "image content", "data", "mimeType"); err != nil {
		return err
	}
	type alias ImageContent
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*content = ImageContent(decoded)
	return nil
}

type UserContentBlockType string

const (
	UserContentText  UserContentBlockType = "text"
	UserContentImage UserContentBlockType = "image"
)

type UserContentBlock struct {
	Type                 UserContentBlockType `json:"type"`
	Text                 string               `json:"text,omitempty"`
	TextSignature        string               `json:"textSignature,omitempty"`
	Data                 string               `json:"data,omitempty"`
	MimeType             string               `json:"mimeType,omitempty"`
	textSignaturePresent bool
}

func NewTextUserContentBlock(text string) UserContentBlock {
	return UserContentBlock{Type: UserContentText, Text: text}
}

func (block UserContentBlock) MarshalJSON() ([]byte, error) {
	object := map[string]any{"type": string(block.Type)}
	switch block.Type {
	case UserContentText:
		object["text"] = block.Text
		if block.TextSignature != "" || block.textSignaturePresent {
			object["textSignature"] = block.TextSignature
		}
	case UserContentImage:
		object["data"] = block.Data
		object["mimeType"] = block.MimeType
	default:
		return nil, fmt.Errorf("ai user content block unknown type %q", block.Type)
	}
	return marshalJSONNoHTMLEscape(object)
}

func (block *UserContentBlock) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	type alias UserContentBlock
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	switch decoded.Type {
	case UserContentText:
		if _, ok := object["text"]; !ok {
			return fmt.Errorf("ai user content block text missing required field %q", "text")
		}
		if err := rejectJSONNull(object, "text", "user content block text"); err != nil {
			return err
		}
		if rawTextSignature, ok := object["textSignature"]; ok && !isJSONNull(rawTextSignature) {
			decoded.textSignaturePresent = true
		}
	case UserContentImage:
		for _, field := range []string{"data", "mimeType"} {
			if _, ok := object[field]; !ok {
				return fmt.Errorf("ai user content block image missing required field %q", field)
			}
		}
		if err := rejectJSONNullFields(object, "user content block image", "data", "mimeType"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("ai user content block unknown type %q", decoded.Type)
	}
	*block = UserContentBlock(decoded)
	return nil
}

type UserContent struct {
	Text   string
	Blocks []UserContentBlock
}

func UserContentTextValue(text string) UserContent {
	return UserContent{Text: text}
}

func UserContentBlocksValue(blocks []UserContentBlock) UserContent {
	return UserContent{Blocks: blocks}
}

func (content UserContent) MarshalJSON() ([]byte, error) {
	if content.Blocks != nil {
		return marshalJSONNoHTMLEscape(content.Blocks)
	}
	return marshalJSONNoHTMLEscape(content.Text)
}

func (content *UserContent) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		return fmt.Errorf("ai user content cannot be null")
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*content = UserContent{Text: text}
		return nil
	}
	var blocks []UserContentBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return err
	}
	*content = UserContent{Blocks: blocks}
	return nil
}

type UserRole string

const UserRoleUser UserRole = "user"

func (role UserRole) MarshalJSON() ([]byte, error) {
	switch role {
	case UserRoleUser:
		return json.Marshal(string(role))
	default:
		return nil, fmt.Errorf("ai user role unknown value %q", role)
	}
}

func (role *UserRole) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "user":
		*role = UserRoleUser
	default:
		return fmt.Errorf("ai user role unknown value %q", value)
	}
	return nil
}

type UserMessage struct {
	Role      UserRole    `json:"role,omitempty"`
	Content   UserContent `json:"content"`
	Timestamp int64       `json:"timestamp"`
}

func (message UserMessage) MarshalJSON() ([]byte, error) {
	if message.Role != "" && message.Role != UserRoleUser {
		return nil, fmt.Errorf("ai user message unknown role %q", message.Role)
	}
	return marshalJSONNoHTMLEscape(struct {
		Content   UserContent `json:"content"`
		Timestamp int64       `json:"timestamp"`
	}{Content: message.Content, Timestamp: message.Timestamp})
}

func (message *UserMessage) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"content", "timestamp"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai user message missing required field %q", field)
		}
	}
	if err := rejectJSONNull(object, "content", "user message"); err != nil {
		return err
	}
	if err := rejectJSONNull(object, "timestamp", "user message"); err != nil {
		return err
	}
	type alias UserMessage
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Role == "" {
		decoded.Role = UserRoleUser
	}
	*message = UserMessage(decoded)
	return nil
}

type ToolResultRole string

const ToolResultRoleToolResult ToolResultRole = "toolResult"

func (role ToolResultRole) MarshalJSON() ([]byte, error) {
	switch role {
	case ToolResultRoleToolResult:
		return json.Marshal(string(role))
	default:
		return nil, fmt.Errorf("ai tool result role unknown value %q", role)
	}
}

func (role *ToolResultRole) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "toolResult":
		*role = ToolResultRoleToolResult
	default:
		return fmt.Errorf("ai tool result role unknown value %q", value)
	}
	return nil
}

type ToolResultMessage struct {
	Role           ToolResultRole     `json:"role,omitempty"`
	ToolCallID     string             `json:"toolCallId"`
	ToolName       string             `json:"toolName"`
	Content        []UserContentBlock `json:"content"`
	Details        any                `json:"details,omitempty"`
	detailsPresent bool
	IsError        bool  `json:"isError"`
	Timestamp      int64 `json:"timestamp"`
}

func (message ToolResultMessage) MarshalJSON() ([]byte, error) {
	if message.Role != "" && message.Role != ToolResultRoleToolResult {
		return nil, fmt.Errorf("ai tool result message unknown role %q", message.Role)
	}
	content := message.Content
	if content == nil {
		content = []UserContentBlock{}
	}
	data, err := marshalJSONNoHTMLEscape(struct {
		ToolCallID string             `json:"toolCallId"`
		ToolName   string             `json:"toolName"`
		Content    []UserContentBlock `json:"content"`
		Details    any                `json:"details,omitempty"`
		IsError    bool               `json:"isError"`
		Timestamp  int64              `json:"timestamp"`
	}{ToolCallID: message.ToolCallID, ToolName: message.ToolName, Content: content, Details: message.Details, IsError: message.IsError, Timestamp: message.Timestamp})
	if err != nil {
		return nil, err
	}
	if !message.detailsPresent {
		return data, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	object["details"] = message.Details
	return marshalJSONNoHTMLEscape(object)
}

func (message *ToolResultMessage) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"toolCallId", "toolName", "content", "isError", "timestamp"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai tool result message missing required field %q", field)
		}
	}
	if isJSONNull(object["content"]) {
		return fmt.Errorf("ai tool result message content cannot be null")
	}
	if err := rejectJSONNullFields(object, "tool result message", "toolCallId", "toolName"); err != nil {
		return err
	}
	if isJSONNull(object["isError"]) {
		return fmt.Errorf("ai tool result message isError cannot be null")
	}
	if err := rejectJSONNull(object, "timestamp", "tool result message"); err != nil {
		return err
	}
	type alias ToolResultMessage
	var decoded alias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if decoded.Role == "" {
		decoded.Role = ToolResultRoleToolResult
	}
	*message = ToolResultMessage(decoded)
	return nil
}

type AssistantRole string

const AssistantRoleAssistant AssistantRole = "assistant"

func (role AssistantRole) MarshalJSON() ([]byte, error) {
	switch role {
	case AssistantRoleAssistant:
		return json.Marshal(string(role))
	default:
		return nil, fmt.Errorf("ai assistant role unknown value %q", role)
	}
}

func (role *AssistantRole) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "assistant":
		*role = AssistantRoleAssistant
	default:
		return fmt.Errorf("ai assistant role unknown value %q", value)
	}
	return nil
}

type ContentBlock struct {
	Type                     ContentType `json:"type"`
	Text                     string      `json:"text,omitempty"`
	TextSignature            string      `json:"textSignature,omitempty"`
	Thinking                 string      `json:"thinking,omitempty"`
	ThinkingSignature        string      `json:"thinkingSignature,omitempty"`
	Redacted                 bool        `json:"redacted,omitempty"`
	Data                     string      `json:"data,omitempty"`
	MimeType                 string      `json:"mimeType,omitempty"`
	ToolCall                 *ToolCall   `json:"toolCall,omitempty"`
	textSignaturePresent     bool
	thinkingSignaturePresent bool
}

func NewTextContentBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentText, Text: text}
}

func (block ContentBlock) MarshalJSON() ([]byte, error) {
	object := map[string]any{"type": string(block.Type)}
	switch block.Type {
	case ContentText:
		object["text"] = block.Text
		if block.TextSignature != "" || block.textSignaturePresent {
			object["textSignature"] = block.TextSignature
		}
	case ContentThinking:
		object["thinking"] = block.Thinking
		if block.ThinkingSignature != "" || block.thinkingSignaturePresent {
			object["thinkingSignature"] = block.ThinkingSignature
		}
		if block.Redacted {
			object["redacted"] = true
		}
	case ContentImage:
		object["data"] = block.Data
		object["mimeType"] = block.MimeType
	case ContentToolCall:
		if block.ToolCall == nil {
			return nil, fmt.Errorf("ai content block toolCall missing payload")
		}
		object["type"] = "toolCall"
		object["id"] = block.ToolCall.ID
		object["name"] = block.ToolCall.Name
		object["arguments"] = toolCallArgumentsWire(block.ToolCall.Arguments)
		if block.ToolCall.ThoughtSignature != "" || block.ToolCall.thoughtSignaturePresent {
			object["thoughtSignature"] = block.ToolCall.ThoughtSignature
		}
	default:
		return nil, fmt.Errorf("ai content block unknown type %q", block.Type)
	}
	return marshalJSONNoHTMLEscape(object)
}

func toolCallArgumentsWire(arguments map[string]any) map[string]any {
	if arguments == nil {
		return map[string]any{}
	}
	return arguments
}

func (block *ContentBlock) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	type alias ContentBlock
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if err := validateContentBlockFields(object, decoded.Type); err != nil {
		return err
	}
	if rawTextSignature, ok := object["textSignature"]; ok && !isJSONNull(rawTextSignature) {
		decoded.textSignaturePresent = true
	}
	if rawThinkingSignature, ok := object["thinkingSignature"]; ok && !isJSONNull(rawThinkingSignature) {
		decoded.thinkingSignaturePresent = true
	}
	if decoded.Type == "toolCall" {
		var toolCall ToolCall
		if err := json.Unmarshal(data, &toolCall); err != nil {
			return err
		}
		decoded.Type = ContentToolCall
		decoded.ToolCall = &toolCall
	}
	*block = ContentBlock(decoded)
	return nil
}

func validateContentBlockFields(object map[string]json.RawMessage, contentType ContentType) error {
	switch contentType {
	case ContentText:
		if _, ok := object["text"]; !ok {
			return fmt.Errorf("ai content block text missing required field %q", "text")
		}
		if err := rejectJSONNull(object, "text", "content block text"); err != nil {
			return err
		}
	case ContentThinking:
		if _, ok := object["thinking"]; !ok {
			return fmt.Errorf("ai content block thinking missing required field %q", "thinking")
		}
		if err := rejectJSONNull(object, "thinking", "content block thinking"); err != nil {
			return err
		}
		if rawRedacted, ok := object["redacted"]; ok && isJSONNull(rawRedacted) {
			return fmt.Errorf("ai content block thinking redacted cannot be null")
		}
	case ContentImage:
		for _, field := range []string{"data", "mimeType"} {
			if _, ok := object[field]; !ok {
				return fmt.Errorf("ai content block image missing required field %q", field)
			}
		}
		if err := rejectJSONNullFields(object, "content block image", "data", "mimeType"); err != nil {
			return err
		}
	case "toolCall":
		return nil
	default:
		return fmt.Errorf("ai content block unknown type %q", contentType)
	}
	return nil
}

type ToolCall struct {
	ID                      string         `json:"id"`
	Name                    string         `json:"name"`
	Arguments               map[string]any `json:"arguments,omitempty"`
	ThoughtSignature        string         `json:"thoughtSignature,omitempty"`
	thoughtSignaturePresent bool
}

func (call ToolCall) MarshalJSON() ([]byte, error) {
	type alias ToolCall
	data, err := marshalJSONNoHTMLEscape(struct {
		alias
		Arguments map[string]any `json:"arguments"`
	}{alias: alias(call), Arguments: toolCallArgumentsWire(call.Arguments)})
	if err != nil {
		return nil, err
	}
	if !call.thoughtSignaturePresent {
		return data, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	object["thoughtSignature"] = call.ThoughtSignature
	return marshalJSONNoHTMLEscape(object)
}

func (call *ToolCall) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"id", "name"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai tool call missing required field %q", field)
		}
	}
	if err := rejectJSONNullFields(object, "tool call", "id", "name"); err != nil {
		return err
	}
	if rawArguments, ok := object["arguments"]; ok && isJSONNull(rawArguments) {
		return fmt.Errorf("ai tool call arguments cannot be null")
	}
	thoughtSignaturePresent := false
	if rawThoughtSignature, ok := object["thoughtSignature"]; ok && !isJSONNull(rawThoughtSignature) {
		thoughtSignaturePresent = true
	}
	type alias ToolCall
	var decoded alias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if decoded.Arguments == nil {
		decoded.Arguments = map[string]any{}
	}
	decoded.thoughtSignaturePresent = thoughtSignaturePresent
	*call = ToolCall(decoded)
	return nil
}

type UsageCost struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
	Total      float64 `json:"total,omitempty"`
}

func (cost UsageCost) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cacheRead"`
		CacheWrite float64 `json:"cacheWrite"`
		Total      float64 `json:"total"`
	}{Input: cost.Input, Output: cost.Output, CacheRead: cost.CacheRead, CacheWrite: cost.CacheWrite, Total: cost.Total})
}

func (cost *UsageCost) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"input", "output", "cacheRead", "cacheWrite", "total"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai usage cost missing required field %q", field)
		}
	}
	if err := rejectJSONNullFields(object, "usage cost", "input", "output", "cacheRead", "cacheWrite", "total"); err != nil {
		return err
	}
	type alias UsageCost
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*cost = UsageCost(decoded)
	return nil
}

type Usage struct {
	Input            int        `json:"-"`
	Output           int        `json:"-"`
	CacheRead        int        `json:"-"`
	CacheWrite       int        `json:"-"`
	InputTokens      int        `json:"inputTokens,omitempty"`
	OutputTokens     int        `json:"outputTokens,omitempty"`
	CacheReadTokens  int        `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int        `json:"cacheWriteTokens,omitempty"`
	TotalTokenCount  int        `json:"totalTokens,omitempty"`
	HasTotalTokens   bool       `json:"-"`
	Cost             *UsageCost `json:"cost,omitempty"`
}

func (usage Usage) MarshalJSON() ([]byte, error) {
	input := coalesceUsageAlias(usage.Input, usage.InputTokens)
	output := coalesceUsageAlias(usage.Output, usage.OutputTokens)
	cacheRead := coalesceUsageAlias(usage.CacheRead, usage.CacheReadTokens)
	cacheWrite := coalesceUsageAlias(usage.CacheWrite, usage.CacheWriteTokens)
	if err := validateUsageTokens(usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens, usage.TotalTokenCount, ""); err != nil {
		return nil, err
	}
	if err := validateUsageTokens(usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite, usage.TotalTokenCount, ""); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Input       int       `json:"input"`
		Output      int       `json:"output"`
		CacheRead   int       `json:"cacheRead"`
		CacheWrite  int       `json:"cacheWrite"`
		TotalTokens int       `json:"totalTokens"`
		Cost        UsageCost `json:"cost"`
	}{
		Input:       input,
		Output:      output,
		CacheRead:   cacheRead,
		CacheWrite:  cacheWrite,
		TotalTokens: usage.TotalTokenCount,
		Cost:        usageCostValue(usage.Cost),
	})
}

func coalesceUsageAlias(upstream int, legacy int) int {
	if upstream != 0 {
		return upstream
	}
	return legacy
}

func usageCostValue(cost *UsageCost) UsageCost {
	if cost == nil {
		return UsageCost{}
	}
	return *cost
}

func (usage *Usage) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if !hasAnyKey(object, "inputTokens", "outputTokens", "cacheReadTokens", "cacheWriteTokens") {
		for _, field := range []string{"input", "output", "cacheRead", "cacheWrite", "totalTokens", "cost"} {
			if _, ok := object[field]; !ok {
				return fmt.Errorf("ai usage missing required field %q", field)
			}
		}
		if err := rejectJSONNullFields(object, "usage", "input", "output", "cacheRead", "cacheWrite", "totalTokens", "cost"); err != nil {
			return err
		}
	}
	var wire struct {
		InputTokens      int        `json:"inputTokens"`
		OutputTokens     int        `json:"outputTokens"`
		CacheReadTokens  int        `json:"cacheReadTokens"`
		CacheWriteTokens int        `json:"cacheWriteTokens"`
		Input            int        `json:"input"`
		Output           int        `json:"output"`
		CacheRead        int        `json:"cacheRead"`
		CacheWrite       int        `json:"cacheWrite"`
		TotalTokens      int        `json:"totalTokens"`
		Cost             *UsageCost `json:"cost"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if err := validateUsageTokens(wire.InputTokens, wire.OutputTokens, wire.CacheReadTokens, wire.CacheWriteTokens, 0, "Tokens"); err != nil {
		return err
	}
	if err := validateUsageTokens(wire.Input, wire.Output, wire.CacheRead, wire.CacheWrite, wire.TotalTokens, ""); err != nil {
		return err
	}
	*usage = Usage{InputTokens: wire.InputTokens, OutputTokens: wire.OutputTokens, CacheReadTokens: wire.CacheReadTokens, CacheWriteTokens: wire.CacheWriteTokens, Cost: wire.Cost}
	if _, ok := object["input"]; ok {
		usage.Input = wire.Input
		usage.InputTokens = wire.Input
	}
	if _, ok := object["output"]; ok {
		usage.Output = wire.Output
		usage.OutputTokens = wire.Output
	}
	if _, ok := object["cacheRead"]; ok {
		usage.CacheRead = wire.CacheRead
		usage.CacheReadTokens = wire.CacheRead
	}
	if _, ok := object["cacheWrite"]; ok {
		usage.CacheWrite = wire.CacheWrite
		usage.CacheWriteTokens = wire.CacheWrite
	}
	if _, ok := object["totalTokens"]; ok {
		usage.TotalTokenCount = wire.TotalTokens
		usage.HasTotalTokens = true
	}
	return nil
}

func validateUsageTokens(input, output, cacheRead, cacheWrite, totalTokens int, suffix string) error {
	if input < 0 {
		return fmt.Errorf("ai usage input%s cannot be negative", suffix)
	}
	if output < 0 {
		return fmt.Errorf("ai usage output%s cannot be negative", suffix)
	}
	if cacheRead < 0 {
		return fmt.Errorf("ai usage cacheRead%s cannot be negative", suffix)
	}
	if cacheWrite < 0 {
		return fmt.Errorf("ai usage cacheWrite%s cannot be negative", suffix)
	}
	if totalTokens < 0 {
		return fmt.Errorf("ai usage totalTokens cannot be negative")
	}
	return nil
}

func hasAnyKey(object map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		if _, ok := object[key]; ok {
			return true
		}
	}
	return false
}

func (usage Usage) TotalTokens() int {
	if usage.HasTotalTokens || usage.TotalTokenCount != 0 {
		return usage.TotalTokenCount
	}
	return usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
}

type StopReason string

const (
	StopReasonEndTurn   StopReason = "end_turn"
	StopReasonToolCalls StopReason = "tool_calls"
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonError     StopReason = "error"
	StopReasonAborted   StopReason = "aborted"

	StopReasonStop    = StopReasonEndTurn
	StopReasonLength  = StopReasonMaxTokens
	StopReasonToolUse = StopReasonToolCalls
)

func (reason StopReason) MarshalJSON() ([]byte, error) {
	value, ok := reason.WireValue()
	if !ok {
		return nil, fmt.Errorf("ai stop reason unknown value %q", reason)
	}
	return json.Marshal(value)
}

func (reason StopReason) WireValue() (string, bool) {
	switch reason {
	case "", StopReasonEndTurn:
		return "stop", true
	case StopReasonMaxTokens:
		return "length", true
	case StopReasonToolCalls:
		return "toolUse", true
	case StopReasonError:
		return "error", true
	case StopReasonAborted:
		return "aborted", true
	default:
		return "", false
	}
}

func (reason *StopReason) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "stop":
		*reason = StopReasonEndTurn
	case "length":
		*reason = StopReasonMaxTokens
	case "toolUse":
		*reason = StopReasonToolCalls
	case "error":
		*reason = StopReasonError
	case "aborted":
		*reason = StopReasonAborted
	default:
		return fmt.Errorf("ai stop reason unknown value %q", value)
	}
	return nil
}

type ImagesStopReason string

const (
	ImagesStopReasonStop    ImagesStopReason = "stop"
	ImagesStopReasonError   ImagesStopReason = "error"
	ImagesStopReasonAborted ImagesStopReason = "aborted"
)

func (reason ImagesStopReason) MarshalJSON() ([]byte, error) {
	switch reason {
	case ImagesStopReasonStop, ImagesStopReasonError, ImagesStopReasonAborted:
		return json.Marshal(string(reason))
	default:
		return nil, fmt.Errorf("ai images stop reason unknown value %q", reason)
	}
}

func (reason *ImagesStopReason) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "stop", "error", "aborted":
		*reason = ImagesStopReason(value)
	default:
		return fmt.Errorf("ai images stop reason unknown value %q", value)
	}
	return nil
}

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role                 Role           `json:"role"`
	API                  Api            `json:"api,omitempty"`
	Provider             Provider       `json:"provider,omitempty"`
	Model                string         `json:"model,omitempty"`
	ResponseModel        string         `json:"responseModel,omitempty"`
	ResponseID           string         `json:"responseId,omitempty"`
	Content              []ContentBlock `json:"content,omitempty"`
	ToolCalls            []ToolCall     `json:"toolCalls,omitempty"`
	Diagnostics          []any          `json:"diagnostics,omitempty"`
	ToolCallID           string         `json:"toolCallId,omitempty"`
	ToolName             string         `json:"toolName,omitempty"`
	Name                 string         `json:"name,omitempty"`
	Details              map[string]any `json:"-"`
	DetailsValue         any            `json:"details,omitempty"`
	detailsPresent       bool
	IsError              bool       `json:"isError,omitempty"`
	Usage                *Usage     `json:"usage,omitempty"`
	StopReason           StopReason `json:"stopReason,omitempty"`
	ErrorMessage         string     `json:"errorMessage,omitempty"`
	Timestamp            int64      `json:"timestamp,omitempty"`
	responseModelPresent bool
	responseIDPresent    bool
	diagnosticsPresent   bool
	errorMessagePresent  bool
}

func (message Message) MarshalJSON() ([]byte, error) {
	if message.Role != RoleSystem && message.Role != RoleUser && message.Role != RoleAssistant && message.Role != RoleTool {
		return nil, fmt.Errorf("ai message unknown role %q", message.Role)
	}
	type alias Message
	data, err := marshalJSONNoHTMLEscape(alias(message))
	if err != nil {
		return nil, err
	}
	if message.Role == RoleSystem {
		return data, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if message.Role == RoleUser && len(message.Content) == 1 && message.Content[0].Type == ContentText {
		object["content"] = message.Content[0].Text
	} else if message.Role == RoleUser {
		object["content"] = userContentBlocks(message.Content)
	}
	if message.Role == RoleUser {
		object["timestamp"] = message.Timestamp
	}
	if message.Role == RoleAssistant {
		object["api"] = message.API
		object["provider"] = message.Provider
		object["model"] = message.Model
		if message.Usage == nil {
			object["usage"] = Usage{}
		}
		object["stopReason"] = message.StopReason
		object["timestamp"] = message.Timestamp
		if message.responseModelPresent {
			object["responseModel"] = message.ResponseModel
		}
		if message.responseIDPresent {
			object["responseId"] = message.ResponseID
		}
		if message.diagnosticsPresent {
			object["diagnostics"] = message.Diagnostics
		}
		if message.errorMessagePresent {
			object["errorMessage"] = message.ErrorMessage
		}
		delete(object, "toolCalls")
	}
	if message.Role == RoleTool {
		object["role"] = "toolResult"
		object["content"] = userContentBlocks(message.Content)
		object["timestamp"] = message.Timestamp
		object["isError"] = message.IsError
		if message.detailsPresent {
			object["details"] = message.DetailsValue
		}
		if message.DetailsValue == nil && message.Details != nil {
			object["details"] = message.Details
		}
		delete(object, "name")
	}
	return marshalJSONNoHTMLEscape(object)
}

func userContentBlocks(blocks []ContentBlock) []ContentBlock {
	out := make([]ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == ContentText || block.Type == ContentImage {
			out = append(out, block)
		}
	}
	return out
}

func contentBlocksFromUserContentBlocks(blocks []UserContentBlock) []ContentBlock {
	out := make([]ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case UserContentText:
			out = append(out, ContentBlock{Type: ContentText, Text: block.Text, TextSignature: block.TextSignature, textSignaturePresent: block.textSignaturePresent})
		case UserContentImage:
			out = append(out, ContentBlock{Type: ContentImage, Data: block.Data, MimeType: block.MimeType})
		}
	}
	return out
}

func (message *Message) UnmarshalJSON(data []byte) error {
	type alias Message
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	var decoded alias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		var role Role
		if roleErr := json.Unmarshal(object["role"], &role); roleErr != nil || role != RoleUser {
			return err
		}
		var content string
		if contentErr := json.Unmarshal(object["content"], &content); contentErr != nil {
			return err
		}
		objectWithoutContent := make(map[string]json.RawMessage, len(object))
		for key, value := range object {
			objectWithoutContent[key] = value
		}
		delete(objectWithoutContent, "content")
		dataWithoutContent, marshalErr := json.Marshal(objectWithoutContent)
		if marshalErr != nil {
			return err
		}
		retryDecoder := json.NewDecoder(bytes.NewReader(dataWithoutContent))
		retryDecoder.UseNumber()
		if retryErr := retryDecoder.Decode(&decoded); retryErr != nil {
			return err
		}
		decoded.Content = []ContentBlock{{Type: ContentText, Text: content}}
	}
	if decoded.Role == "toolResult" {
		decoded.Role = RoleTool
	}
	if decoded.Role == RoleUser {
		if err := requireMessageFields(object, "user", "content", "timestamp"); err != nil {
			return err
		}
		if err := rejectJSONNull(object, "content", "message user"); err != nil {
			return err
		}
		if err := rejectJSONNull(object, "timestamp", "message user"); err != nil {
			return err
		}
		if err := requireUserContentBlocks(decoded.Content); err != nil {
			return err
		}
	}
	if decoded.Role == RoleAssistant {
		if err := requireMessageFields(object, "assistant", "content", "api", "provider", "model", "usage", "stopReason", "timestamp"); err != nil {
			return err
		}
		if err := rejectJSONNullFields(object, "message assistant", "api", "provider", "model"); err != nil {
			return err
		}
		if err := rejectJSONNull(object, "timestamp", "message assistant"); err != nil {
			return err
		}
		if rawResponseModel, ok := object["responseModel"]; ok && !isJSONNull(rawResponseModel) {
			decoded.responseModelPresent = true
		}
		if rawResponseID, ok := object["responseId"]; ok && !isJSONNull(rawResponseID) {
			decoded.responseIDPresent = true
		}
		if rawDiagnostics, ok := object["diagnostics"]; ok && !isJSONNull(rawDiagnostics) {
			decoded.diagnosticsPresent = true
		}
		if rawErrorMessage, ok := object["errorMessage"]; ok && !isJSONNull(rawErrorMessage) {
			decoded.errorMessagePresent = true
		}
	}
	if decoded.Role == RoleTool {
		if err := requireMessageFields(object, "toolResult", "toolCallId", "toolName", "content", "isError", "timestamp"); err != nil {
			return err
		}
		if isJSONNull(object["isError"]) {
			return fmt.Errorf("ai message toolResult isError cannot be null")
		}
		if err := rejectJSONNullFields(object, "message toolResult", "toolCallId", "toolName"); err != nil {
			return err
		}
		if err := rejectJSONNull(object, "timestamp", "message toolResult"); err != nil {
			return err
		}
		if err := requireUserContentBlocks(decoded.Content); err != nil {
			return err
		}
	}
	if decoded.DetailsValue != nil {
		if details, ok := decoded.DetailsValue.(map[string]any); ok {
			decoded.Details = details
		}
	}
	*message = Message(decoded)
	return nil
}

func requireUserContentBlocks(blocks []ContentBlock) error {
	for _, block := range blocks {
		if block.Type != ContentText && block.Type != ContentImage {
			return fmt.Errorf("ai user content block invalid type %q", block.Type)
		}
	}
	return nil
}

func requireMessageFields(object map[string]json.RawMessage, role string, fields ...string) error {
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai message %s missing required field %q", role, field)
		}
	}
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func rejectJSONNull(object map[string]json.RawMessage, field, context string) error {
	if isJSONNull(object[field]) {
		return fmt.Errorf("ai %s %s cannot be null", context, field)
	}
	return nil
}

func rejectJSONNullFields(object map[string]json.RawMessage, context string, fields ...string) error {
	for _, field := range fields {
		if err := rejectJSONNull(object, field, context); err != nil {
			return err
		}
	}
	return nil
}

type AssistantMessage struct {
	Role                 AssistantRole  `json:"role,omitempty"`
	Content              []ContentBlock `json:"content,omitempty"`
	ToolCalls            []ToolCall     `json:"toolCalls,omitempty"`
	API                  Api            `json:"api,omitempty"`
	Provider             Provider       `json:"provider,omitempty"`
	Model                string         `json:"model,omitempty"`
	Diagnostics          []any          `json:"diagnostics,omitempty"`
	ResponseModel        string         `json:"responseModel,omitempty"`
	ResponseID           string         `json:"responseId,omitempty"`
	Usage                *Usage         `json:"usage,omitempty"`
	StopReason           StopReason     `json:"stopReason,omitempty"`
	ErrorMessage         string         `json:"errorMessage,omitempty"`
	Timestamp            int64          `json:"timestamp,omitempty"`
	responseModelPresent bool
	responseIDPresent    bool
	errorMessagePresent  bool
}

func (message AssistantMessage) MarshalJSON() ([]byte, error) {
	if message.Role != "" && message.Role != AssistantRoleAssistant {
		return nil, fmt.Errorf("ai assistant message unknown role %q", message.Role)
	}
	usage := Usage{}
	if message.Usage != nil {
		usage = *message.Usage
	}
	content := message.Content
	if content == nil {
		content = []ContentBlock{}
	}
	object := map[string]any{
		"content":    content,
		"api":        message.API,
		"provider":   message.Provider,
		"model":      message.Model,
		"usage":      usage,
		"stopReason": message.StopReason,
		"timestamp":  message.Timestamp,
	}
	if message.ResponseModel != "" || message.responseModelPresent {
		object["responseModel"] = message.ResponseModel
	}
	if message.ResponseID != "" || message.responseIDPresent {
		object["responseId"] = message.ResponseID
	}
	if message.Diagnostics != nil {
		object["diagnostics"] = message.Diagnostics
	}
	if message.ErrorMessage != "" || message.errorMessagePresent {
		object["errorMessage"] = message.ErrorMessage
	}
	return marshalJSONNoHTMLEscape(object)
}

func (message *AssistantMessage) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"content", "api", "provider", "model", "usage", "stopReason", "timestamp"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai assistant message missing required field %q", field)
		}
	}
	if isJSONNull(object["content"]) {
		return fmt.Errorf("ai assistant message content cannot be null")
	}
	if err := rejectJSONNullFields(object, "assistant message", "api", "provider", "model"); err != nil {
		return err
	}
	if err := rejectJSONNull(object, "timestamp", "assistant message"); err != nil {
		return err
	}
	responseModelPresent := false
	if rawResponseModel, ok := object["responseModel"]; ok && !isJSONNull(rawResponseModel) {
		responseModelPresent = true
	}
	responseIDPresent := false
	if rawResponseID, ok := object["responseId"]; ok && !isJSONNull(rawResponseID) {
		responseIDPresent = true
	}
	errorMessagePresent := false
	if rawErrorMessage, ok := object["errorMessage"]; ok && !isJSONNull(rawErrorMessage) {
		errorMessagePresent = true
	}
	type alias AssistantMessage
	var decoded alias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if decoded.Role == "" {
		decoded.Role = AssistantRoleAssistant
	}
	decoded.responseModelPresent = responseModelPresent
	decoded.responseIDPresent = responseIDPresent
	decoded.errorMessagePresent = errorMessagePresent
	*message = AssistantMessage(decoded)
	return nil
}

func (message AssistantMessage) Text() string {
	var text string
	for _, block := range message.Content {
		if block.Type == ContentText {
			text += block.Text
		}
	}
	return text
}

type Tool struct {
	Name              string `json:"name"`
	Description       string `json:"description,omitempty"`
	Parameters        any    `json:"parameters,omitempty"`
	parametersPresent bool
}

func (tool Tool) MarshalJSON() ([]byte, error) {
	parameters := tool.Parameters
	if parameters == nil && !tool.parametersPresent {
		parameters = map[string]any{}
	}
	return marshalJSONNoHTMLEscape(struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	}{Name: tool.Name, Description: tool.Description, Parameters: parameters})
}

func (tool *Tool) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"name", "description", "parameters"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai tool missing required field %q", field)
		}
	}
	if err := rejectJSONNullFields(object, "tool", "name", "description"); err != nil {
		return err
	}
	type alias Tool
	var decoded alias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	decoded.parametersPresent = true
	*tool = Tool(decoded)
	return nil
}

type Context struct {
	SystemPrompt    string    `json:"systemPrompt,omitempty"`
	HasSystemPrompt bool      `json:"-"`
	Messages        []Message `json:"messages"`
	Tools           []Tool    `json:"tools,omitempty"`
	HasTools        bool      `json:"-"`
}

// ContextFromMessages promotes a leading system message to the provider-level system prompt.
func ContextFromMessages(messages []Message, tools []Tool) Context {
	context := Context{Messages: messages, Tools: tools}
	if len(messages) == 0 || messages[0].Role != RoleSystem {
		return context
	}
	context.SystemPrompt = blocksText(messages[0].Content)
	context.HasSystemPrompt = true
	context.Messages = messages[1:]
	return context
}

func (context Context) MarshalJSON() ([]byte, error) {
	type alias Context
	object := struct {
		alias
		SystemPrompt *string   `json:"systemPrompt,omitempty"`
		Messages     []Message `json:"messages"`
		Tools        *[]Tool   `json:"tools,omitempty"`
	}{alias: alias(context)}
	object.Messages = context.Messages
	if object.Messages == nil {
		object.Messages = []Message{}
	}
	if context.HasSystemPrompt || context.SystemPrompt != "" {
		object.SystemPrompt = &context.SystemPrompt
	}
	if context.HasTools || context.Tools != nil {
		tools := context.Tools
		if tools == nil {
			tools = []Tool{}
		}
		object.Tools = &tools
	}
	return marshalJSONNoHTMLEscape(object)
}

func (context *Context) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	type alias Context
	var decoded alias
	if rawMessages, ok := object["messages"]; ok && isJSONNull(rawMessages) {
		return fmt.Errorf("ai context messages cannot be null")
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Messages == nil {
		decoded.Messages = []Message{}
	}
	if rawSystemPrompt, ok := object["systemPrompt"]; ok && !isJSONNull(rawSystemPrompt) {
		decoded.HasSystemPrompt = true
	}
	if rawTools, ok := object["tools"]; ok && !isJSONNull(rawTools) {
		decoded.HasTools = true
	}
	*context = Context(decoded)
	return nil
}

type ImagesContext struct {
	Input []UserContentBlock `json:"input,omitempty"`
}

func (context ImagesContext) MarshalJSON() ([]byte, error) {
	input := context.Input
	if input == nil {
		input = []UserContentBlock{}
	}
	return marshalJSONNoHTMLEscape(struct {
		Input []UserContentBlock `json:"input"`
	}{Input: input})
}

func (context *ImagesContext) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if rawInput, ok := object["input"]; ok && isJSONNull(rawInput) {
		return fmt.Errorf("ai images context input cannot be null")
	}
	type alias ImagesContext
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Input == nil {
		decoded.Input = []UserContentBlock{}
	}
	*context = ImagesContext(decoded)
	return nil
}

type AssistantMessageEventType string

func (eventType AssistantMessageEventType) AsStr() string { return string(eventType) }

const (
	EventStart         AssistantMessageEventType = "start"
	EventTextStart     AssistantMessageEventType = "text_start"
	EventTextDelta     AssistantMessageEventType = "text_delta"
	EventTextEnd       AssistantMessageEventType = "text_end"
	EventThinkingStart AssistantMessageEventType = "thinking_start"
	EventThinkingDelta AssistantMessageEventType = "thinking_delta"
	EventThinkingEnd   AssistantMessageEventType = "thinking_end"
	EventToolCallStart AssistantMessageEventType = "tool_call_start"
	EventToolCallEnd   AssistantMessageEventType = "tool_call_end"
	EventContentBlock  AssistantMessageEventType = "content_block"
	EventContentUpdate AssistantMessageEventType = "content_update"
	EventToolCall      AssistantMessageEventType = "tool_call"
	EventToolCallDelta AssistantMessageEventType = "tool_call_delta"
	EventMetadata      AssistantMessageEventType = "metadata"
	EventUsage         AssistantMessageEventType = "usage"
	EventDone          AssistantMessageEventType = "done"
	EventError         AssistantMessageEventType = "error"

	AssistantMessageEventTextStart     = EventTextStart
	AssistantMessageEventTextDelta     = EventTextDelta
	AssistantMessageEventTextEnd       = EventTextEnd
	AssistantMessageEventThinkingStart = EventThinkingStart
	AssistantMessageEventThinkingDelta = EventThinkingDelta
	AssistantMessageEventThinkingEnd   = EventThinkingEnd
	AssistantMessageEventToolCallStart = EventToolCallStart
	AssistantMessageEventToolCallDelta = EventToolCallDelta
	AssistantMessageEventToolCallEnd   = EventToolCallEnd
)

type DoneReason string

func (reason DoneReason) AsStr() string { return string(reason) }

const (
	DoneReasonStop      DoneReason = "stop"
	DoneReasonToolCalls DoneReason = "tool_calls"
	DoneReasonToolUse   DoneReason = DoneReasonToolCalls
	DoneReasonLength    DoneReason = "length"
	DoneReasonAbort     DoneReason = "abort"
)

type ErrorReason string

func (reason ErrorReason) AsStr() string { return string(reason) }

const (
	ErrorReasonProvider ErrorReason = "error"
	ErrorReasonError    ErrorReason = ErrorReasonProvider
	ErrorReasonAbort    ErrorReason = "aborted"
	ErrorReasonAborted  ErrorReason = ErrorReasonAbort
)

type AssistantMessageEvent struct {
	Type          AssistantMessageEventType `json:"type"`
	ContentIndex  int                       `json:"contentIndex,omitempty"`
	Partial       *AssistantMessage         `json:"partial,omitempty"`
	Delta         string                    `json:"delta,omitempty"`
	Content       string                    `json:"content,omitempty"`
	ContentBlock  *ContentBlock             `json:"contentBlock,omitempty"`
	ToolCall      *ToolCall                 `json:"toolCall,omitempty"`
	ResponseModel string                    `json:"responseModel,omitempty"`
	ResponseID    string                    `json:"responseId,omitempty"`
	Usage         *Usage                    `json:"usage,omitempty"`
	DoneReason    DoneReason                `json:"doneReason,omitempty"`
	ErrorReason   ErrorReason               `json:"errorReason,omitempty"`
	Message       *AssistantMessage         `json:"message,omitempty"`
	Error         string                    `json:"error,omitempty"`
}

func (event AssistantMessageEvent) IsTerminal() bool {
	return event.Type == EventDone || event.Type == EventError
}

type InputModality string

const (
	InputText  InputModality = "text"
	InputImage InputModality = "image"

	InputModalityText  = InputText
	InputModalityImage = InputImage
)

func (modality InputModality) MarshalJSON() ([]byte, error) {
	switch modality {
	case InputText, InputImage:
		return json.Marshal(string(modality))
	default:
		return nil, fmt.Errorf("ai input modality unknown value %q", modality)
	}
}

func (modality *InputModality) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	switch value {
	case "text", "image":
		*modality = InputModality(value)
	default:
		return fmt.Errorf("ai input modality unknown value %q", value)
	}
	return nil
}

type ModelCost struct {
	Input      float64 `json:"input,omitempty"`
	Output     float64 `json:"output,omitempty"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
}

func (cost ModelCost) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cacheRead"`
		CacheWrite float64 `json:"cacheWrite"`
	}{Input: cost.Input, Output: cost.Output, CacheRead: cost.CacheRead, CacheWrite: cost.CacheWrite})
}

func (cost *ModelCost) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"input", "output", "cacheRead", "cacheWrite"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai model cost missing required field %q", field)
		}
	}
	if err := rejectJSONNullFields(object, "model cost", "input", "output", "cacheRead", "cacheWrite"); err != nil {
		return err
	}
	type alias ModelCost
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*cost = ModelCost(decoded)
	return nil
}

type Model struct {
	ID               string             `json:"id"`
	Name             string             `json:"name,omitempty"`
	API              Api                `json:"api"`
	Provider         Provider           `json:"provider"`
	BaseURL          string             `json:"baseUrl,omitempty"`
	Reasoning        bool               `json:"reasoning,omitempty"`
	Input            []InputModality    `json:"input,omitempty"`
	Cost             *ModelCost         `json:"cost,omitempty"`
	ContextWindow    int                `json:"contextWindow,omitempty"`
	MaxTokens        int                `json:"maxTokens,omitempty"`
	Headers          map[string]string  `json:"headers,omitempty"`
	ThinkingLevels   map[string]*string `json:"thinkingLevelMap,omitempty"`
	ThinkingLevelMap map[string]*string `json:"-"`
	Compat           map[string]any     `json:"compat,omitempty"`
	CompatValue      any                `json:"-"`
	headersPresent   bool
}

func (model Model) MarshalJSON() ([]byte, error) {
	thinkingLevels := model.ThinkingLevels
	if thinkingLevels == nil {
		thinkingLevels = model.ThinkingLevelMap
	}
	if model.ContextWindow < 0 {
		return nil, fmt.Errorf("ai model contextWindow cannot be negative")
	}
	if model.MaxTokens < 0 {
		return nil, fmt.Errorf("ai model maxTokens cannot be negative")
	}
	input := model.Input
	if input == nil {
		input = []InputModality{}
	}
	cost := ModelCost{}
	if model.Cost != nil {
		cost = *model.Cost
	}
	type alias Model
	object := struct {
		alias
		Name          string          `json:"name"`
		BaseURL       string          `json:"baseUrl"`
		Reasoning     bool            `json:"reasoning"`
		Input         []InputModality `json:"input"`
		Cost          ModelCost       `json:"cost"`
		ContextWindow int             `json:"contextWindow"`
		MaxTokens     int             `json:"maxTokens"`
	}{alias: alias(model), Name: model.Name, BaseURL: model.BaseURL, Reasoning: model.Reasoning, Input: input, Cost: cost, ContextWindow: model.ContextWindow, MaxTokens: model.MaxTokens}
	object.ThinkingLevels = thinkingLevels
	data, err := marshalJSONNoHTMLEscape(object)
	if err != nil {
		return nil, err
	}
	if model.Headers != nil && len(model.Headers) == 0 {
		var wire map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			return nil, err
		}
		wire["headers"] = model.Headers
		data, err = marshalJSONNoHTMLEscape(wire)
		if err != nil {
			return nil, err
		}
	}
	if thinkingLevels != nil && len(thinkingLevels) == 0 {
		var wire map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			return nil, err
		}
		wire["thinkingLevelMap"] = thinkingLevels
		data, err = marshalJSONNoHTMLEscape(wire)
		if err != nil {
			return nil, err
		}
	}
	if model.CompatValue == nil && model.Compat != nil && len(model.Compat) == 0 {
		var wire map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			return nil, err
		}
		wire["compat"] = model.Compat
		data, err = marshalJSONNoHTMLEscape(wire)
		if err != nil {
			return nil, err
		}
	}
	if model.CompatValue == nil {
		if err := validateThinkingLevelMapKeys(thinkingLevels); err != nil {
			return nil, err
		}
		return data, nil
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	if err := validateThinkingLevelMapKeys(thinkingLevels); err != nil {
		return nil, err
	}
	wire["compat"] = model.CompatValue
	return marshalJSONNoHTMLEscape(wire)
}

func (model *Model) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"id", "name", "api", "provider", "baseUrl", "reasoning", "cost", "contextWindow", "maxTokens"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai model missing required field %q", field)
		}
	}
	if rawInput, ok := object["input"]; ok && isJSONNull(rawInput) {
		return fmt.Errorf("ai model input cannot be null")
	}
	if err := rejectJSONNullFields(object, "model", "id", "name", "api", "provider", "baseUrl"); err != nil {
		return err
	}
	if err := rejectJSONNullFields(object, "model", "cost", "contextWindow", "maxTokens"); err != nil {
		return err
	}
	if isJSONNull(object["reasoning"]) {
		return fmt.Errorf("ai model reasoning cannot be null")
	}
	if rawThinkingLevels, ok := object["thinkingLevelMap"]; ok && !isJSONNull(rawThinkingLevels) {
		var thinkingLevels map[string]json.RawMessage
		if err := json.Unmarshal(rawThinkingLevels, &thinkingLevels); err != nil {
			return err
		}
		for level := range thinkingLevels {
			if !isModelThinkingLevel(level) {
				return fmt.Errorf("ai model thinkingLevelMap unknown key %q", level)
			}
		}
	}
	headersPresent := false
	if rawHeaders, ok := object["headers"]; ok && !isJSONNull(rawHeaders) {
		headersPresent = true
	}
	objectWithoutCompat := make(map[string]json.RawMessage, len(object))
	for key, value := range object {
		if key != "compat" {
			objectWithoutCompat[key] = value
		}
	}
	dataWithoutCompat, err := json.Marshal(objectWithoutCompat)
	if err != nil {
		return err
	}
	type alias Model
	var decoded alias
	if err := json.Unmarshal(dataWithoutCompat, &decoded); err != nil {
		return err
	}
	if decoded.ContextWindow < 0 {
		return fmt.Errorf("ai model contextWindow cannot be negative")
	}
	if decoded.MaxTokens < 0 {
		return fmt.Errorf("ai model maxTokens cannot be negative")
	}
	if rawCompat, ok := object["compat"]; ok && !isJSONNull(rawCompat) {
		var compatValue any
		decoder := json.NewDecoder(bytes.NewReader(rawCompat))
		decoder.UseNumber()
		if err := decoder.Decode(&compatValue); err != nil {
			return err
		}
		decoded.CompatValue = compatValue
		if compat, ok := compatValue.(map[string]any); ok {
			decoded.Compat = compat
		}
	}
	if decoded.Input == nil {
		decoded.Input = []InputModality{}
	}
	decoded.ThinkingLevelMap = decoded.ThinkingLevels
	decoded.headersPresent = headersPresent
	*model = Model(decoded)
	return nil
}

func validateThinkingLevelMapKeys(levels map[string]*string) error {
	for level := range levels {
		if !isModelThinkingLevel(level) {
			return fmt.Errorf("ai model thinkingLevelMap unknown key %q", level)
		}
	}
	return nil
}

type ImagesModel struct {
	ID             string            `json:"id"`
	Name           string            `json:"name,omitempty"`
	API            ImagesApi         `json:"api"`
	Provider       ImagesProvider    `json:"provider"`
	BaseURL        string            `json:"baseUrl,omitempty"`
	Input          []InputModality   `json:"input,omitempty"`
	Output         []InputModality   `json:"output,omitempty"`
	Cost           *ModelCost        `json:"cost,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	headersPresent bool
}

func (model ImagesModel) MarshalJSON() ([]byte, error) {
	input := model.Input
	if input == nil {
		input = []InputModality{}
	}
	output := model.Output
	if output == nil {
		output = []InputModality{}
	}
	cost := ModelCost{}
	if model.Cost != nil {
		cost = *model.Cost
	}
	type alias ImagesModel
	data, err := marshalJSONNoHTMLEscape(struct {
		alias
		Name    string          `json:"name"`
		BaseURL string          `json:"baseUrl"`
		Input   []InputModality `json:"input"`
		Output  []InputModality `json:"output"`
		Cost    ModelCost       `json:"cost"`
	}{alias: alias(model), Name: model.Name, BaseURL: model.BaseURL, Input: input, Output: output, Cost: cost})
	if err != nil {
		return nil, err
	}
	if model.Headers != nil && len(model.Headers) == 0 {
		var wire map[string]any
		if err := json.Unmarshal(data, &wire); err != nil {
			return nil, err
		}
		wire["headers"] = model.Headers
		return marshalJSONNoHTMLEscape(wire)
	}
	return data, nil
}

func (model *ImagesModel) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"id", "name", "api", "provider", "baseUrl", "cost"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai images model missing required field %q", field)
		}
	}
	if err := rejectJSONNullFields(object, "images model", "id", "name", "api", "provider", "baseUrl"); err != nil {
		return err
	}
	if err := rejectJSONNull(object, "cost", "images model"); err != nil {
		return err
	}
	for _, field := range []string{"input", "output"} {
		if raw, ok := object[field]; ok && isJSONNull(raw) {
			return fmt.Errorf("ai images model %s cannot be null", field)
		}
	}
	headersPresent := false
	if rawHeaders, ok := object["headers"]; ok && !isJSONNull(rawHeaders) {
		headersPresent = true
	}
	type alias ImagesModel
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Input == nil {
		decoded.Input = []InputModality{}
	}
	if decoded.Output == nil {
		decoded.Output = []InputModality{}
	}
	decoded.headersPresent = headersPresent
	*model = ImagesModel(decoded)
	return nil
}

type AssistantImages struct {
	API                 ImagesApi        `json:"api"`
	Provider            ImagesProvider   `json:"provider"`
	Model               string           `json:"model"`
	Output              []ContentBlock   `json:"output,omitempty"`
	ResponseID          string           `json:"responseId,omitempty"`
	Usage               *Usage           `json:"usage,omitempty"`
	StopReason          ImagesStopReason `json:"stopReason"`
	ErrorMessage        string           `json:"errorMessage,omitempty"`
	Timestamp           int64            `json:"timestamp"`
	responseIDPresent   bool
	errorMessagePresent bool
}

func (images AssistantImages) MarshalJSON() ([]byte, error) {
	output := userContentBlocks(images.Output)
	stopReason := images.StopReason
	if stopReason == "" {
		stopReason = ImagesStopReasonStop
	}
	type alias AssistantImages
	data, err := marshalJSONNoHTMLEscape(struct {
		alias
		Output     []ContentBlock   `json:"output"`
		StopReason ImagesStopReason `json:"stopReason"`
		Timestamp  int64            `json:"timestamp"`
	}{alias: alias(images), Output: output, StopReason: stopReason, Timestamp: images.Timestamp})
	if err != nil {
		return nil, err
	}
	if !images.responseIDPresent && !images.errorMessagePresent {
		return data, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if images.responseIDPresent {
		object["responseId"] = images.ResponseID
	}
	if images.errorMessagePresent {
		object["errorMessage"] = images.ErrorMessage
	}
	return marshalJSONNoHTMLEscape(object)
}

func (images *AssistantImages) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"api", "provider", "model", "output", "stopReason", "timestamp"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("ai assistant images missing required field %q", field)
		}
	}
	if isJSONNull(object["output"]) {
		return fmt.Errorf("ai assistant images output cannot be null")
	}
	if err := rejectJSONNullFields(object, "assistant images", "api", "provider", "model"); err != nil {
		return err
	}
	if err := rejectJSONNull(object, "timestamp", "assistant images"); err != nil {
		return err
	}
	responseIDPresent := false
	if rawResponseID, ok := object["responseId"]; ok && !isJSONNull(rawResponseID) {
		responseIDPresent = true
	}
	errorMessagePresent := false
	if rawErrorMessage, ok := object["errorMessage"]; ok && !isJSONNull(rawErrorMessage) {
		errorMessagePresent = true
	}
	type alias AssistantImages
	var outputBlocks []UserContentBlock
	if err := json.Unmarshal(object["output"], &outputBlocks); err != nil {
		return err
	}
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	decoded.Output = contentBlocksFromUserContentBlocks(outputBlocks)
	decoded.responseIDPresent = responseIDPresent
	decoded.errorMessagePresent = errorMessagePresent
	*images = AssistantImages(decoded)
	return nil
}
