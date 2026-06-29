package ai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
)

type BedrockAnthropicConverter struct {
	message AssistantMessage
}

type Converter = BedrockAnthropicConverter

func NewBedrockAnthropicConverter() *BedrockAnthropicConverter {
	return &BedrockAnthropicConverter{message: AssistantMessage{Role: AssistantRoleAssistant, API: Api("bedrock-anthropic"), Provider: Provider("amazon-bedrock"), Usage: &Usage{}, StopReason: StopReasonEndTurn}}
}

func NewConverter() *Converter {
	return NewBedrockAnthropicConverter()
}

func (Converter) New() *Converter {
	return NewConverter()
}

func (converter *BedrockAnthropicConverter) Ingest(message BedrockEventStreamMessage) ([]AssistantMessageEvent, error) {
	var envelope map[string]any
	if err := json.Unmarshal(message.Payload, &envelope); err != nil {
		return nil, fmt.Errorf("bedrock chunk not JSON: %w", err)
	}
	encoded, ok := envelope["bytes"].(string)
	if !ok {
		return nil, fmt.Errorf("bedrock chunk missing `bytes`")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("bedrock chunk b64 decode: %w", err)
	}
	var event map[string]any
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("anthropic event parse: %w", err)
	}
	return converter.handle(event)
}

func (converter *BedrockAnthropicConverter) handle(event map[string]any) ([]AssistantMessageEvent, error) {
	switch stringValue(event["type"]) {
	case "message_start":
		message, ok := event["message"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("anthropic event parse: missing message")
		}
		converter.message.Model = stringValue(message["model"])
		converter.message.ResponseID = stringValue(message["id"])
		return []AssistantMessageEvent{{Type: EventStart, Partial: converter.partial()}}, nil
	case "content_block_start":
		index, err := bedrockAnthropicIndex(event)
		if err != nil {
			return nil, err
		}
		converter.ensureContent(index)
		block, ok := event["content_block"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("anthropic event parse: missing content_block")
		}
		blockType, ok := block["type"].(string)
		if !ok {
			return nil, fmt.Errorf("anthropic event parse: invalid content_block.type")
		}
		switch blockType {
		case "text":
			converter.message.Content[index] = ContentBlock{Type: ContentText, Text: stringValue(block["text"])}
			return []AssistantMessageEvent{{Type: EventTextStart, ContentIndex: index, Partial: converter.partial()}}, nil
		case "thinking":
			converter.message.Content[index] = ContentBlock{Type: ContentThinking, Thinking: stringValue(block["thinking"])}
			return []AssistantMessageEvent{{Type: EventThinkingStart, ContentIndex: index, Partial: converter.partial()}}, nil
		case "tool_use":
			converter.message.Content[index] = ContentBlock{Type: ContentToolCall, ToolCall: &ToolCall{ID: stringValue(block["id"]), Name: stringValue(block["name"]), Arguments: map[string]any{}}}
			return []AssistantMessageEvent{{Type: EventToolCallStart, ContentIndex: index, Partial: converter.partial()}}, nil
		}
		return nil, nil
	case "content_block_delta":
		index, err := bedrockAnthropicIndex(event)
		if err != nil {
			return nil, err
		}
		converter.ensureContent(index)
		delta, ok := event["delta"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("anthropic event parse: missing delta")
		}
		deltaType, ok := delta["type"].(string)
		if !ok {
			return nil, fmt.Errorf("anthropic event parse: invalid delta.type")
		}
		switch deltaType {
		case "text_delta":
			text := stringValue(delta["text"])
			if converter.message.Content[index].Type == ContentText {
				converter.message.Content[index].Text += text
			}
			return []AssistantMessageEvent{{Type: EventTextDelta, ContentIndex: index, Delta: text, Partial: converter.partial()}}, nil
		case "thinking_delta":
			thinking := stringValue(delta["thinking"])
			if converter.message.Content[index].Type == ContentThinking {
				converter.message.Content[index].Thinking += thinking
			}
			return []AssistantMessageEvent{{Type: EventThinkingDelta, ContentIndex: index, Delta: thinking, Partial: converter.partial()}}, nil
		case "input_json_delta":
			partial := stringValue(delta["partial_json"])
			return []AssistantMessageEvent{{Type: EventToolCallDelta, ContentIndex: index, Delta: partial, Partial: converter.partial()}}, nil
		}
		return nil, nil
	case "content_block_stop":
		index, err := bedrockAnthropicIndex(event)
		if err != nil {
			return nil, err
		}
		if index < 0 || index >= len(converter.message.Content) {
			return nil, nil
		}
		block := converter.message.Content[index]
		switch block.Type {
		case ContentText:
			return []AssistantMessageEvent{{Type: EventTextEnd, ContentIndex: index, Content: block.Text, Partial: converter.partial()}}, nil
		case ContentThinking:
			return []AssistantMessageEvent{{Type: EventThinkingEnd, ContentIndex: index, Content: block.Thinking, Partial: converter.partial()}}, nil
		case ContentToolCall:
			if block.ToolCall == nil {
				return nil, nil
			}
			call := *block.ToolCall
			return []AssistantMessageEvent{{Type: EventToolCallEnd, ContentIndex: index, ToolCall: &call, Partial: converter.partial()}}, nil
		default:
			return nil, nil
		}
	case "message_delta":
		delta, ok := event["delta"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("anthropic event parse: missing delta")
		}
		if _, ok := delta["stop_reason"]; ok {
			switch stringValue(delta["stop_reason"]) {
			case "max_tokens":
				converter.message.StopReason = StopReasonMaxTokens
			case "tool_use":
				converter.message.StopReason = StopReasonToolCalls
			default:
				converter.message.StopReason = StopReasonEndTurn
			}
		}
		if usage, ok := event["usage"].(map[string]any); ok {
			converter.message.Usage.InputTokens = uintNumber(usage["input_tokens"])
			converter.message.Usage.OutputTokens = uintNumber(usage["output_tokens"])
			converter.message.Usage.CacheReadTokens = uintNumber(usage["cache_read_input_tokens"])
			converter.message.Usage.CacheWriteTokens = uintNumber(usage["cache_creation_input_tokens"])
			converter.message.Usage.TotalTokenCount = converter.message.Usage.InputTokens + converter.message.Usage.OutputTokens
			converter.message.Usage.HasTotalTokens = true
		}
		return nil, nil
	case "message_stop":
		doneReason := DoneReasonStop
		switch converter.message.StopReason {
		case StopReasonToolCalls:
			doneReason = DoneReasonToolCalls
		case StopReasonMaxTokens:
			doneReason = DoneReasonLength
		}
		return []AssistantMessageEvent{{Type: EventDone, DoneReason: doneReason, Partial: converter.partial(), Message: converter.partial()}}, nil
	case "error":
		if _, ok := event["error"].(map[string]any); !ok {
			return nil, fmt.Errorf("anthropic event parse: missing error")
		}
		converter.message.ErrorMessage = bedrockAnthropicErrorMessage(event)
		converter.message.StopReason = StopReasonError
		return []AssistantMessageEvent{{Type: EventError, ErrorReason: ErrorReasonProvider, Partial: converter.partial(), Message: converter.partial()}}, nil
	case "ping":
		return nil, nil
	}
	return nil, fmt.Errorf("anthropic event parse: unknown type %q", stringValue(event["type"]))
}

func bedrockAnthropicErrorMessage(event map[string]any) string {
	if errorBody, ok := event["error"].(map[string]any); ok {
		return stringValue(errorBody["message"])
	}
	return anthropicErrorMessage(event)
}

func bedrockAnthropicIndex(event map[string]any) (int, error) {
	value, ok := event["index"]
	if !ok {
		return 0, fmt.Errorf("anthropic event parse: invalid index")
	}
	var indexFloat float64
	switch indexNumber := value.(type) {
	case float64:
		indexFloat = indexNumber
	case json.Number:
		parsed, err := indexNumber.Float64()
		if err != nil {
			return 0, fmt.Errorf("anthropic event parse: invalid index")
		}
		indexFloat = parsed
	default:
		return 0, fmt.Errorf("anthropic event parse: invalid index")
	}
	if math.Trunc(indexFloat) != indexFloat {
		return 0, fmt.Errorf("anthropic event parse: invalid index")
	}
	index := int(indexFloat)
	if index < 0 {
		return 0, fmt.Errorf("anthropic event parse: negative index %d", index)
	}
	return index, nil
}

func (converter *BedrockAnthropicConverter) ensureContent(index int) {
	for len(converter.message.Content) <= index {
		converter.message.Content = append(converter.message.Content, ContentBlock{Type: ContentText})
	}
}

func (converter *BedrockAnthropicConverter) partial() *AssistantMessage {
	message := converter.message
	if message.Usage != nil {
		usage := *message.Usage
		message.Usage = &usage
	}
	message.Content = append([]ContentBlock(nil), message.Content...)
	for index := range message.Content {
		if message.Content[index].ToolCall != nil {
			call := *message.Content[index].ToolCall
			message.Content[index].ToolCall = &call
		}
	}
	return &message
}
