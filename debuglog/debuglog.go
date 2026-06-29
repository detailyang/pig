package debuglog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/bugreport"
)

const PreviewMaxChars = 4000
const PreviewMaxLines = 80

const DEBUG_PREVIEW_MAX_CHARS = PreviewMaxChars
const DEBUG_PREVIEW_MAX_LINES = PreviewMaxLines

type Options struct {
	Reasoning string
	SessionID string
}

type StreamFunc func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error)

func WrapStreamFunc(base StreamFunc, emit func(string)) StreamFunc {
	var sequence uint64
	return func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		callID := atomic.AddUint64(&sequence, 1)
		context := debugContext(messages, tools)
		emitDebugLine(emit, StartLine(callID, model, context, Options{Reasoning: string(options.ThinkingLevel), SessionID: options.Base.SessionID}))
		if line, ok := ContextLine(callID, context); ok {
			emitDebugLine(emit, line)
		}

		inner, err := base(ctx, model, messages, tools, options)
		if err != nil {
			return nil, err
		}
		started := time.Now()
		if inner.IsLive() {
			out := ai.NewAssistantMessageEventStream().MarkLive()
			go func() {
				index := 0
				for {
					event, next, err := inner.Next(ctx, index)
					if err != nil {
						out.Emit(ai.AssistantMessageEvent{Type: ai.EventError, ErrorReason: ai.ErrorReasonError, Error: err.Error()})
						return
					}
					index = next
					emitDebugEvent(emit, callID, started, event, inner)
					out.Emit(event)
					if event.IsTerminal() {
						return
					}
				}
			}()
			return out, nil
		}
		out := ai.NewAssistantMessageEventStream()
		sawTerminal := false
		for _, event := range inner.Events() {
			if event.IsTerminal() {
				sawTerminal = true
			}
			emitDebugEvent(emit, callID, started, event, inner)
			out.Emit(event)
		}
		if !sawTerminal {
			emitDebugLine(emit, fmt.Sprintf("[debug llm #%d closed] elapsed=%dms stream ended without terminal event", callID, time.Since(started).Milliseconds()))
		}
		return out, nil
	}
}

func emitDebugEvent(emit func(string), callID uint64, started time.Time, event ai.AssistantMessageEvent, stream *ai.AssistantMessageEventStream) {
	switch event.Type {
	case ai.EventToolCallEnd:
		if event.ToolCall != nil {
			emitDebugLine(emit, ToolCallLine(callID, *event.ToolCall))
		}
	case ai.EventDone:
		message := ai.AssistantMessage{}
		if event.Message != nil {
			message = *event.Message
		} else if event.Partial != nil {
			message = *event.Partial
		} else if result, ok := stream.Snapshot(); ok {
			message = result
		}
		emitDebugLine(emit, DoneLine(callID, event.DoneReason, message, time.Since(started)))
	case ai.EventError:
		message := event.Error
		if message == "" && event.Message != nil {
			message = event.Message.ErrorMessage
		}
		if message == "" {
			message = "unknown error"
		}
		emitDebugLine(emit, fmt.Sprintf("[debug llm #%d error] reason=%s elapsed=%dms message=\"%s\"", callID, event.ErrorReason, time.Since(started).Milliseconds(), Preview(message)))
	}
}

func WrapStreamFn(base StreamFunc, emit func(string)) StreamFunc {
	return WrapStreamFunc(base, emit)
}

func debugContext(messages []ai.Message, tools []ai.Tool) ai.Context {
	context := ai.Context{Messages: messages, Tools: tools}
	if len(messages) == 0 || messages[0].Role != ai.RoleSystem {
		return context
	}
	context.SystemPrompt = contentBlocksLog(messages[0].Content)
	context.Messages = append([]ai.Message(nil), messages[1:]...)
	return context
}

func emitDebugLine(emit func(string), line string) {
	if emit != nil {
		emit(line)
	}
}

func StartLine(callID uint64, model ai.Model, context ai.Context, options Options) string {
	reasoning := options.Reasoning
	if reasoning == "" {
		reasoning = "off"
	}
	session := options.SessionID
	if session == "" {
		session = "-"
	}
	return fmt.Sprintf("[debug llm #%d start] provider=%s api=%s model=%s messages=%d tools=%d system_chars=%d reasoning=%s session=%s", callID, model.Provider, model.API, model.ID, len(context.Messages), len(context.Tools), len([]rune(context.SystemPrompt)), reasoning, session)
}

func ContextLine(callID uint64, context ai.Context) (string, bool) {
	if len(context.Messages) == 0 {
		return "", false
	}
	last := context.Messages[len(context.Messages)-1]
	return fmt.Sprintf("[debug llm #%d context] last_%s:\n%s", callID, roleLabel(last), messageLog(last)), true
}

func ToolCallLine(callID uint64, toolCall ai.ToolCall) string {
	args, err := marshalJSONIndentNoHTMLEscape(toolCall.Arguments, "", "  ")
	if err != nil {
		args, _ = marshalJSONNoHTMLEscape(toolCall.Arguments)
	}
	return fmt.Sprintf("[debug llm #%d tool-call] id=%s name=%s args=\n%s", callID, toolCall.ID, toolCall.Name, Preview(string(args)))
}

func marshalJSONNoHTMLEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func marshalJSONIndentNoHTMLEscape(value any, prefix string, indent string) ([]byte, error) {
	data, err := marshalJSONNoHTMLEscape(value)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, data, prefix, indent); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func DoneLine(callID uint64, reason ai.DoneReason, message ai.AssistantMessage, elapsed time.Duration) string {
	usage := ai.Usage{}
	if message.Usage != nil {
		usage = *message.Usage
	}
	cost := 0.0
	if usage.Cost != nil {
		cost = usage.Cost.Total
	}
	responseID := message.ResponseID
	if responseID == "" {
		responseID = "-"
	}
	return fmt.Sprintf("[debug llm #%d done] reason=%s stop=%s elapsed=%dms usage=input:%d output:%d cache_read:%d cache_write:%d total:%d cost:$%.6f response_id=%s text:\n%s", callID, reason, message.StopReason, elapsed.Milliseconds(), usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheWriteTokens, usage.TotalTokenCount, cost, responseID, Preview(assistantLog(message)))
}

func Preview(input string) string {
	return boundedPreview(bugreport.Redact(input))
}

func DebugPreview(input string) string {
	return Preview(input)
}

func ElapsedMS(elapsed time.Duration) string {
	return fmt.Sprintf("%dms", elapsed.Milliseconds())
}

func Emit(emit func(string), text string) {
	emitDebugLine(emit, text)
}

func boundedPreview(input string) string {
	var out strings.Builder
	lines := 0
	chars := 0
	truncatedForLines := false
	truncatedForChars := false
	for _, segment := range strings.SplitAfter(input, "\n") {
		if lines >= PreviewMaxLines {
			truncatedForLines = true
			break
		}
		segmentChars := []rune(segment)
		consumed := 0
		for consumed < len(segmentChars) && chars < PreviewMaxChars {
			ch := segmentChars[consumed]
			out.WriteRune(ch)
			chars++
			consumed++
			if ch == '\n' {
				lines++
			}
		}
		if consumed < len(segmentChars) {
			truncatedForChars = true
			break
		}
		if !strings.HasSuffix(segment, "\n") {
			lines++
		}
	}
	if chars >= PreviewMaxChars && len([]rune(input)) > PreviewMaxChars {
		truncatedForChars = true
	}
	if truncatedForLines || truncatedForChars {
		if !strings.HasSuffix(out.String(), "\n") {
			out.WriteByte('\n')
		}
		fmt.Fprintf(&out, "[debug preview truncated: max %d lines / %d chars]", PreviewMaxLines, PreviewMaxChars)
	}
	return out.String()
}

func roleLabel(message ai.Message) string {
	switch message.Role {
	case ai.RoleUser:
		return "user"
	case ai.RoleAssistant:
		return "assistant"
	case ai.RoleTool:
		return "tool_result"
	default:
		return string(message.Role)
	}
}

func messageLog(message ai.Message) string {
	var raw string
	switch message.Role {
	case ai.RoleAssistant:
		raw = assistantLog(ai.AssistantMessage{Content: message.Content, ToolCalls: message.ToolCalls, ResponseID: message.ResponseID, Usage: message.Usage, StopReason: message.StopReason, ErrorMessage: message.ErrorMessage, Timestamp: message.Timestamp})
	case ai.RoleTool:
		raw = contentBlocksLog(message.Content)
	default:
		raw = contentBlocksLog(message.Content)
	}
	return Preview(raw)
}

func assistantLog(message ai.AssistantMessage) string {
	blocks := make([]string, 0, len(message.Content))
	for _, block := range message.Content {
		switch block.Type {
		case ai.ContentText:
			blocks = append(blocks, block.Text)
		case ai.ContentThinking:
			blocks = append(blocks, block.Thinking)
		case ai.ContentImage:
			blocks = append(blocks, fmt.Sprintf("[image:%s]", block.MimeType))
		case ai.ContentToolCall:
			if block.ToolCall != nil {
				blocks = append(blocks, fmt.Sprintf("[tool-call:%s:%s]", block.ToolCall.ID, block.ToolCall.Name))
			}
		}
	}
	return strings.Join(blocks, "\n")
}

func UserContentLog(content ai.UserContent) string {
	if content.Blocks == nil {
		return content.Text
	}
	parts := make([]string, 0, len(content.Blocks))
	for _, block := range content.Blocks {
		parts = append(parts, UserContentBlockLog(block))
	}
	return strings.Join(parts, "\n")
}

func UserContentBlockLog(block ai.UserContentBlock) string {
	switch block.Type {
	case ai.UserContentText:
		return block.Text
	case ai.UserContentImage:
		return fmt.Sprintf("[image:%s]", block.MimeType)
	default:
		return ""
	}
}

func contentBlocksLog(blocks []ai.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ai.ContentImage:
			parts = append(parts, fmt.Sprintf("[image:%s]", block.MimeType))
		case ai.ContentText:
			parts = append(parts, block.Text)
		case ai.ContentThinking:
			parts = append(parts, block.Thinking)
		case ai.ContentToolCall:
			if block.ToolCall != nil {
				parts = append(parts, fmt.Sprintf("[tool-call:%s:%s]", block.ToolCall.ID, block.ToolCall.Name))
			}
		}
	}
	return strings.Join(parts, "\n")
}
