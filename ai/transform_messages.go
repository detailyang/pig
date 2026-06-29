package ai

import (
	"strings"
	"time"
)

const (
	NonVisionUserImagePlaceholder = "(image omitted: model does not support images)"
	NonVisionToolImagePlaceholder = "(tool image omitted: model does not support images)"
	MissingToolResultPlaceholder  = "No result provided"
)

type TransformOptions struct {
	NormalizeToolCallID ToolCallIdNormalizer
}

type ToolCallIdNormalizer func(id string, model Model, message Message) string

func TransformMessages(messages []Message, model Model) []Message {
	return TransformMessagesWithOptions(messages, model, TransformOptions{})
}

func TransformMessagesWithOptions(messages []Message, model Model, options TransformOptions) []Message {
	out := make([]Message, 0, len(messages))
	toolCallIDMap := map[string]string{}
	for _, message := range messages {
		sameModel := message.Provider == model.Provider && message.API == model.API && message.Model == model.ID
		message.Content = transformAssistantContentBlocks(message.Content, sameModel, model, message, options, toolCallIDMap)
		message.Content = transformContentBlocks(message.Content, imagePlaceholderForRole(message.Role), modelSupportsImages(model))
		message.ToolCalls = transformToolCalls(message.ToolCalls, sameModel, model, message, options, toolCallIDMap)
		if message.Role == RoleTool {
			if normalized, ok := toolCallIDMap[message.ToolCallID]; ok {
				message.ToolCallID = normalized
			}
		}
		out = append(out, message)
	}
	return synthesizeMissingToolResults(out)
}

func transformAssistantContentBlocks(blocks []ContentBlock, sameModel bool, model Model, message Message, options TransformOptions, toolCallIDMap map[string]string) []ContentBlock {
	out := make([]ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentThinking:
			if block.Redacted {
				if sameModel {
					out = append(out, block)
				}
				continue
			}
			if strings.TrimSpace(block.Thinking) == "" && !(sameModel && block.ThinkingSignature != "") {
				continue
			}
			if sameModel {
				out = append(out, block)
				continue
			}
			out = append(out, ContentBlock{Type: ContentText, Text: block.Thinking})
		case ContentText:
			if sameModel {
				out = append(out, block)
			} else {
				out = append(out, ContentBlock{Type: ContentText, Text: block.Text})
			}
		case ContentToolCall:
			if block.ToolCall == nil {
				out = append(out, block)
				continue
			}
			toolCall := *block.ToolCall
			if !sameModel {
				toolCall.ThoughtSignature = ""
				if options.NormalizeToolCallID != nil {
					normalized := options.NormalizeToolCallID(toolCall.ID, model, message)
					if normalized != toolCall.ID {
						toolCallIDMap[toolCall.ID] = normalized
						toolCall.ID = normalized
					}
				}
			}
			copyBlock := block
			copyBlock.ToolCall = &toolCall
			out = append(out, copyBlock)
		default:
			out = append(out, block)
		}
	}
	return out
}

func transformToolCalls(toolCalls []ToolCall, sameModel bool, model Model, message Message, options TransformOptions, toolCallIDMap map[string]string) []ToolCall {
	if sameModel {
		return toolCalls
	}
	out := make([]ToolCall, len(toolCalls))
	copy(out, toolCalls)
	for index := range out {
		out[index].ThoughtSignature = ""
		if options.NormalizeToolCallID != nil {
			normalized := options.NormalizeToolCallID(out[index].ID, model, message)
			if normalized != out[index].ID {
				toolCallIDMap[out[index].ID] = normalized
				out[index].ID = normalized
			}
		}
	}
	return out
}

func synthesizeMissingToolResults(messages []Message) []Message {
	result := make([]Message, 0, len(messages))
	var pending []ToolCall
	existingIDs := map[string]bool{}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		for _, call := range pending {
			if !existingIDs[call.ID] {
				result = append(result, Message{Role: RoleTool, ToolCallID: call.ID, ToolName: call.Name, Name: call.Name, Content: []ContentBlock{{Type: ContentText, Text: MissingToolResultPlaceholder}}, IsError: true, StopReason: StopReasonError, Timestamp: time.Now().UnixMilli()})
			}
		}
		pending = nil
		existingIDs = map[string]bool{}
	}
	for _, message := range messages {
		switch message.Role {
		case RoleAssistant:
			flush()
			if message.StopReason == StopReasonError || message.StopReason == StopReasonAborted {
				continue
			}
			toolCalls := messageToolCalls(message)
			if len(toolCalls) > 0 {
				pending = toolCalls
				existingIDs = map[string]bool{}
			}
			result = append(result, message)
		case RoleTool:
			existingIDs[message.ToolCallID] = true
			result = append(result, message)
		case RoleUser:
			flush()
			result = append(result, message)
		default:
			result = append(result, message)
		}
	}
	flush()
	return result
}

func messageToolCalls(message Message) []ToolCall {
	toolCalls := make([]ToolCall, 0, len(message.Content))
	for _, block := range message.Content {
		if block.Type == ContentToolCall && block.ToolCall != nil {
			toolCalls = append(toolCalls, *block.ToolCall)
		}
	}
	return toolCalls
}

func deduplicateToolCalls(calls []ToolCall) []ToolCall {
	seen := map[string]bool{}
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		if call.ID != "" {
			if seen[call.ID] {
				continue
			}
			seen[call.ID] = true
		}
		out = append(out, call)
	}
	return out
}

func messageToolName(message Message) string {
	if message.ToolName != "" {
		return message.ToolName
	}
	return message.Name
}

func transformContentBlocks(blocks []ContentBlock, imagePlaceholder string, supportsImages bool) []ContentBlock {
	if supportsImages || imagePlaceholder == "" {
		return blocks
	}
	out := make([]ContentBlock, 0, len(blocks))
	previousWasPlaceholder := false
	for _, block := range blocks {
		if block.Type == ContentImage {
			if !previousWasPlaceholder {
				out = append(out, ContentBlock{Type: ContentText, Text: imagePlaceholder})
			}
			previousWasPlaceholder = true
			continue
		}
		previousWasPlaceholder = block.Type == ContentText && block.Text == imagePlaceholder
		out = append(out, block)
	}
	return out
}

func imagePlaceholderForRole(role Role) string {
	switch role {
	case RoleUser:
		return NonVisionUserImagePlaceholder
	case RoleTool:
		return NonVisionToolImagePlaceholder
	default:
		return ""
	}
}

func modelSupportsImages(model Model) bool {
	for _, input := range model.Input {
		if input == InputImage {
			return true
		}
	}
	return false
}
