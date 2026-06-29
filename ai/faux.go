package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

var fauxState = struct {
	sync.Mutex
	responses []AssistantMessage
}{responses: []AssistantMessage{}}

type FauxProvider struct{}

func NewFauxProvider() *FauxProvider { return &FauxProvider{} }

func (provider *FauxProvider) API() Api { return ApiFaux }

func (provider *FauxProvider) StreamSimple(ctx context.Context, model Model, request Context, options SimpleStreamOptions) *AssistantMessageEventStream {
	return provider.Stream(ctx, model, request, StreamOptionsFromSimple(options))
}

func (provider *FauxProvider) Stream(_ context.Context, model Model, _ Context, _ StreamOptions) *AssistantMessageEventStream {
	message, ok := popFauxResponse()
	if !ok {
		message = AssistantMessage{
			Role:       AssistantRoleAssistant,
			Content:    []ContentBlock{FauxText("[faux] hello")},
			API:        model.API,
			Provider:   model.Provider,
			Model:      model.ID,
			Usage:      &Usage{},
			StopReason: StopReasonEndTurn,
			Timestamp:  time.Now().UnixMilli(),
		}
	}
	stream := NewAssistantMessageEventStream()
	ReplayFauxMessage(stream, message)
	return stream
}

func SetFauxResponses(responses []AssistantMessage) {
	fauxState.Lock()
	defer fauxState.Unlock()
	fauxState.responses = append([]AssistantMessage(nil), responses...)
}

func AppendFauxResponses(responses []AssistantMessage) {
	fauxState.Lock()
	defer fauxState.Unlock()
	fauxState.responses = append(fauxState.responses, responses...)
}

func ClearFauxResponses() {
	fauxState.Lock()
	defer fauxState.Unlock()
	fauxState.responses = nil
}

func FauxText(text string) ContentBlock {
	return ContentBlock{Type: ContentText, Text: text}
}

func FauxThinking(thinking string) ContentBlock {
	return ContentBlock{Type: ContentThinking, Thinking: thinking}
}

func FauxToolCall(name string, arguments map[string]any) ContentBlock {
	return ContentBlock{Type: ContentToolCall, ToolCall: &ToolCall{ID: "faux_" + fauxUUIDSimple(), Name: name, Arguments: arguments}}
}

func fauxUUIDSimple() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("%032d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes[:])
}

func FauxAssistantMessage(content []ContentBlock) AssistantMessage {
	message := AssistantMessage{
		Role:       AssistantRoleAssistant,
		Content:    content,
		API:        ApiFaux,
		Provider:   Provider("faux"),
		Model:      "faux",
		Usage:      &Usage{},
		StopReason: StopReasonEndTurn,
		Timestamp:  time.Now().UnixMilli(),
	}
	for _, block := range content {
		if isFauxToolCallBlock(block) {
			message.StopReason = StopReasonToolCalls
			break
		}
	}
	return message
}

func ReplayFauxMessage(stream *AssistantMessageEventStream, message AssistantMessage) {
	doneMessage := fauxMessageWithToolCalls(message)
	partial := message
	partial.Content = nil
	partial.StopReason = StopReasonEndTurn
	stream.Emit(AssistantMessageEvent{Type: EventStart, Partial: cloneAssistantMessage(partial)})
	for index, block := range message.Content {
		switch {
		case isFauxToolCallBlock(block):
			partial.Content = append(partial.Content, block)
			stream.Emit(AssistantMessageEvent{Type: EventToolCallStart, ContentIndex: index, Partial: cloneAssistantMessage(partial)})
			stream.Emit(AssistantMessageEvent{Type: EventToolCallDelta, ContentIndex: index, Delta: toolCallArgumentsJSON(block.ToolCall), Partial: cloneAssistantMessage(partial)})
			stream.Emit(AssistantMessageEvent{Type: EventToolCallEnd, ContentIndex: index, ToolCall: block.ToolCall, Partial: cloneAssistantMessage(partial)})
		case block.Type == ContentText:
			partial.Content = append(partial.Content, ContentBlock{Type: ContentText})
			stream.Emit(AssistantMessageEvent{Type: EventTextStart, ContentIndex: index, Partial: cloneAssistantMessage(partial)})
			partial.Content[index].Text = block.Text
			stream.Emit(AssistantMessageEvent{Type: EventTextDelta, ContentIndex: index, Delta: block.Text, Partial: cloneAssistantMessage(partial)})
			stream.Emit(AssistantMessageEvent{Type: EventTextEnd, ContentIndex: index, Content: block.Text, Partial: cloneAssistantMessage(partial)})
		case block.Type == ContentThinking:
			partial.Content = append(partial.Content, ContentBlock{Type: ContentThinking})
			stream.Emit(AssistantMessageEvent{Type: EventThinkingStart, ContentIndex: index, Partial: cloneAssistantMessage(partial)})
			partial.Content[index].Thinking = block.Thinking
			stream.Emit(AssistantMessageEvent{Type: EventThinkingDelta, ContentIndex: index, Delta: block.Thinking, Partial: cloneAssistantMessage(partial)})
			stream.Emit(AssistantMessageEvent{Type: EventThinkingEnd, ContentIndex: index, Content: block.Thinking, Partial: cloneAssistantMessage(partial)})
		default:
			partial.Content = append(partial.Content, block)
		}
	}
	partial.StopReason = message.StopReason
	partial.Usage = message.Usage
	switch message.StopReason {
	case StopReasonToolCalls:
		stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonToolCalls, Message: cloneAssistantMessage(doneMessage)})
	case StopReasonMaxTokens:
		stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonLength, Message: cloneAssistantMessage(doneMessage)})
	case StopReasonError:
		stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonProvider, Message: cloneAssistantMessage(doneMessage)})
	case StopReasonAborted:
		stream.Emit(AssistantMessageEvent{Type: EventError, ErrorReason: ErrorReasonAbort, Message: cloneAssistantMessage(doneMessage)})
	default:
		stream.Emit(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonStop, Message: cloneAssistantMessage(doneMessage)})
	}
}

func fauxMessageWithToolCalls(message AssistantMessage) AssistantMessage {
	if len(message.ToolCalls) > 0 {
		return message
	}
	for _, block := range message.Content {
		if isFauxToolCallBlock(block) {
			message.ToolCalls = append(message.ToolCalls, *block.ToolCall)
		}
	}
	return message
}

func cloneAssistantMessage(message AssistantMessage) *AssistantMessage {
	clone := message
	clone.Content = append([]ContentBlock(nil), message.Content...)
	clone.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
	if message.Usage != nil {
		usage := *message.Usage
		clone.Usage = &usage
	}
	return &clone
}

func toolCallArgumentsJSON(toolCall *ToolCall) string {
	if toolCall == nil || toolCall.Arguments == nil {
		return "{}"
	}
	data, err := marshalJSONNoHTMLEscape(toolCall.Arguments)
	if err != nil {
		return ""
	}
	return string(data)
}

func popFauxResponse() (AssistantMessage, bool) {
	fauxState.Lock()
	defer fauxState.Unlock()
	if len(fauxState.responses) == 0 {
		return AssistantMessage{}, false
	}
	message := fauxState.responses[0]
	fauxState.responses = fauxState.responses[1:]
	return message, true
}

func isFauxToolCallBlock(block ContentBlock) bool {
	return block.Type == ContentToolCall && block.ToolCall != nil
}
