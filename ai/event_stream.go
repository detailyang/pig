package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type AssistantMessageEventStream struct {
	mu        sync.Mutex
	cond      *sync.Cond
	events    []AssistantMessageEvent
	closed    bool
	live      bool
	timestamp int64
	partial   *AssistantMessage
}

type AssistantMessageEventSender struct {
	stream *AssistantMessageEventStream
}

func NewAssistantMessageEventStream() *AssistantMessageEventStream {
	stream := &AssistantMessageEventStream{timestamp: time.Now().UnixMilli()}
	stream.cond = sync.NewCond(&stream.mu)
	return stream
}

func NewAssistantMessageEventStreamWithSender() (*AssistantMessageEventStream, AssistantMessageEventSender) {
	stream := NewAssistantMessageEventStream()
	return stream, AssistantMessageEventSender{stream: stream}
}

func CreateAssistantMessageEventStream() *AssistantMessageEventStream {
	return NewAssistantMessageEventStream()
}

func CreateAssistantMessageEventStreamWithSender() (*AssistantMessageEventStream, AssistantMessageEventSender) {
	return NewAssistantMessageEventStreamWithSender()
}

func (stream *AssistantMessageEventStream) MarkLive() *AssistantMessageEventStream {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.live = true
	return stream
}

func (stream *AssistantMessageEventStream) IsLive() bool {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.live
}

func (sender AssistantMessageEventSender) Push(event AssistantMessageEvent) {
	if sender.stream == nil {
		return
	}
	sender.stream.Emit(event)
}

func (sender AssistantMessageEventSender) IsClosed() bool {
	if sender.stream == nil {
		return true
	}
	sender.stream.mu.Lock()
	defer sender.stream.mu.Unlock()
	return sender.stream.closed
}

func (sender AssistantMessageEventSender) Close(reason DoneReason) {
	if sender.stream == nil {
		return
	}
	sender.stream.Close(reason)
}

func (stream *AssistantMessageEventStream) Emit(event AssistantMessageEvent) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return
	}
	stream.events = append(stream.events, event)
	if event.Type == EventDone || event.Type == EventError {
		stream.closed = true
	}
	if stream.cond != nil {
		stream.cond.Broadcast()
	}
}

func (stream *AssistantMessageEventStream) Close(reason DoneReason) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return
	}
	stream.closed = true
	var partial *AssistantMessage
	if stream.partial != nil {
		copy := *stream.partial
		partial = &copy
	}
	stream.events = append(stream.events, AssistantMessageEvent{Type: EventDone, DoneReason: reason, Partial: partial})
	if stream.cond != nil {
		stream.cond.Broadcast()
	}
}

func (stream *AssistantMessageEventStream) Next(ctx context.Context, index int) (AssistantMessageEvent, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			stream.mu.Lock()
			if stream.cond != nil {
				stream.cond.Broadcast()
			}
			stream.mu.Unlock()
		case <-done:
		}
	}()
	defer close(done)
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.cond == nil {
		stream.cond = sync.NewCond(&stream.mu)
	}
	for index >= len(stream.events) && !stream.closed {
		if err := ctx.Err(); err != nil {
			return AssistantMessageEvent{}, index, err
		}
		stream.cond.Wait()
	}
	if index < len(stream.events) {
		return stream.events[index], index + 1, nil
	}
	if err := ctx.Err(); err != nil {
		return AssistantMessageEvent{}, index, err
	}
	return AssistantMessageEvent{}, index, fmt.Errorf("assistant stream closed")
}

func (stream *AssistantMessageEventStream) Events() []AssistantMessageEvent {
	if stream.IsLive() {
		stream.waitForTerminal(context.Background())
	}
	return stream.SnapshotEvents()
}

func (stream *AssistantMessageEventStream) SnapshotEvents() []AssistantMessageEvent {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	events := make([]AssistantMessageEvent, len(stream.events))
	copy(events, stream.events)
	return events
}

func (stream *AssistantMessageEventStream) Result() (AssistantMessage, bool) {
	stream.waitForTerminal(context.Background())
	return stream.Snapshot()
}

func (stream *AssistantMessageEventStream) Snapshot() (AssistantMessage, bool) {
	events := stream.SnapshotEvents()
	message := AssistantMessage{Timestamp: stream.timestamp}
	completed := false
	toolArgumentBuffers := map[int]string{}
	for _, event := range events {
		mergePartialAssistantMessage(&message, event.Partial)
		switch event.Type {
		case EventTextStart:
			setContentBlockAt(&message, event.ContentIndex, contentBlockFromPartial(event.Partial, event.ContentIndex, ContentBlock{Type: ContentText}))
		case EventTextEnd:
			setContentBlockAt(&message, event.ContentIndex, ContentBlock{Type: ContentText, Text: event.Content})
		case EventTextDelta:
			if shouldUseIndexedDelta(message, event.ContentIndex, ContentText) {
				appendTextDeltaAt(&message, event.ContentIndex, event.Delta)
				continue
			}
			if len(message.Content) > 0 && message.Content[len(message.Content)-1].Type == ContentText {
				message.Content[len(message.Content)-1].Text += event.Delta
			} else {
				message.Content = append(message.Content, ContentBlock{Type: ContentText, Text: event.Delta})
			}
		case EventThinkingStart:
			setContentBlockAt(&message, event.ContentIndex, contentBlockFromPartial(event.Partial, event.ContentIndex, ContentBlock{Type: ContentThinking}))
		case EventThinkingEnd:
			block := ContentBlock{Type: ContentThinking, Thinking: event.Content}
			if event.ContentBlock != nil {
				block.ThinkingSignature = event.ContentBlock.ThinkingSignature
				block.Redacted = event.ContentBlock.Redacted
			} else if event.Partial != nil && event.ContentIndex >= 0 && event.ContentIndex < len(event.Partial.Content) {
				block.ThinkingSignature = event.Partial.Content[event.ContentIndex].ThinkingSignature
				block.Redacted = event.Partial.Content[event.ContentIndex].Redacted
			}
			setContentBlockAt(&message, event.ContentIndex, block)
		case EventThinkingDelta:
			if shouldUseIndexedDelta(message, event.ContentIndex, ContentThinking) {
				appendThinkingDeltaAt(&message, event.ContentIndex, event.Delta, event.ContentBlock)
				continue
			}
			block := ContentBlock{Type: ContentThinking, Thinking: event.Delta}
			if event.ContentBlock != nil {
				block.ThinkingSignature = event.ContentBlock.ThinkingSignature
				block.Redacted = event.ContentBlock.Redacted
			}
			if len(message.Content) > 0 && message.Content[len(message.Content)-1].Type == ContentThinking {
				message.Content[len(message.Content)-1].Thinking += block.Thinking
				if event.ContentBlock != nil {
					message.Content[len(message.Content)-1].ThinkingSignature = block.ThinkingSignature
					message.Content[len(message.Content)-1].Redacted = block.Redacted
				}
			} else {
				message.Content = append(message.Content, block)
			}
		case EventContentBlock:
			if event.ContentBlock != nil {
				message.Content = append(message.Content, *event.ContentBlock)
			}
		case EventContentUpdate:
			if event.ContentBlock != nil {
				for index := len(message.Content) - 1; index >= 0; index-- {
					if message.Content[index].Type == event.ContentBlock.Type {
						message.Content[index] = *event.ContentBlock
						break
					}
				}
			}
		case EventToolCallStart:
			mergeToolCallFromPartial(&message, event.ContentIndex, event.Partial)
		case EventToolCallEnd:
			if event.ToolCall != nil {
				call := *event.ToolCall
				setContentBlockAt(&message, event.ContentIndex, ContentBlock{Type: ContentToolCall, ToolCall: &call})
				replaced := false
				for index := range message.ToolCalls {
					if call.ID == "" || message.ToolCalls[index].ID == call.ID {
						message.ToolCalls[index] = call
						replaced = true
						break
					}
				}
				if !replaced {
					message.ToolCalls = append(message.ToolCalls, call)
				}
			}
		case EventToolCall:
			if event.ToolCall != nil {
				message.ToolCalls = append(message.ToolCalls, *event.ToolCall)
				call := *event.ToolCall
				message.Content = append(message.Content, ContentBlock{Type: ContentToolCall, ToolCall: &call})
			}
		case EventToolCallDelta:
			if event.Delta != "" {
				toolArgumentBuffers[event.ContentIndex] += event.Delta
				applyToolCallArgumentsAt(&message, event.ContentIndex, toolArgumentBuffers[event.ContentIndex])
			}
			if event.ToolCall != nil {
				for index := len(message.ToolCalls) - 1; index >= 0; index-- {
					if event.ToolCall.ID == "" || message.ToolCalls[index].ID == event.ToolCall.ID {
						message.ToolCalls[index] = *event.ToolCall
						break
					}
				}
				for index := len(message.Content) - 1; index >= 0; index-- {
					if message.Content[index].Type != ContentToolCall || message.Content[index].ToolCall == nil {
						continue
					}
					if event.ToolCall.ID == "" || message.Content[index].ToolCall.ID == event.ToolCall.ID {
						call := *event.ToolCall
						message.Content[index].ToolCall = &call
						break
					}
				}
			}
		case EventUsage:
			if event.Usage != nil {
				usage := *event.Usage
				message.Usage = &usage
			}
		case EventMetadata:
			if event.ResponseModel != "" {
				message.ResponseModel = event.ResponseModel
			}
			if event.ResponseID != "" {
				message.ResponseID = event.ResponseID
			}
		case EventError:
			completed = true
			if event.Message != nil {
				message = *event.Message
			}
			if message.StopReason == "" {
				if event.ErrorReason == ErrorReasonAbort {
					message.StopReason = StopReasonAborted
				} else {
					message.StopReason = StopReasonError
				}
			}
			if event.Error != "" {
				message.ErrorMessage = event.Error
			}
			return message, completed
		case EventDone:
			completed = true
			if event.Message != nil {
				message = *event.Message
			}
			if message.StopReason == "" {
				switch event.DoneReason {
				case DoneReasonToolCalls:
					message.StopReason = StopReasonToolCalls
				case DoneReasonLength:
					message.StopReason = StopReasonMaxTokens
				case DoneReasonAbort:
					message.StopReason = StopReasonAborted
				default:
					message.StopReason = StopReasonEndTurn
				}
			}
			return message, completed
		}
	}
	return message, completed
}

func (stream *AssistantMessageEventStream) waitForTerminal(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			stream.mu.Lock()
			if stream.cond != nil {
				stream.cond.Broadcast()
			}
			stream.mu.Unlock()
		case <-done:
		}
	}()
	defer close(done)
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.cond == nil {
		stream.cond = sync.NewCond(&stream.mu)
	}
	for !streamHasTerminalLocked(stream.events) && !stream.closed {
		if ctx.Err() != nil {
			return
		}
		stream.cond.Wait()
	}
}

func streamHasTerminalLocked(events []AssistantMessageEvent) bool {
	for _, event := range events {
		if event.Type == EventDone || event.Type == EventError {
			return true
		}
	}
	return false
}

func setContentBlockAt(message *AssistantMessage, index int, block ContentBlock) {
	if index < 0 {
		message.Content = append(message.Content, block)
		return
	}
	for len(message.Content) <= index {
		message.Content = append(message.Content, ContentBlock{})
	}
	message.Content[index] = block
}

func shouldUseIndexedDelta(message AssistantMessage, index int, blockType ContentType) bool {
	if index < 0 {
		return false
	}
	if index > 0 {
		return true
	}
	if index >= len(message.Content) {
		return false
	}
	return message.Content[index].Type == "" || message.Content[index].Type == blockType
}

func appendTextDeltaAt(message *AssistantMessage, index int, delta string) {
	ensureContentIndex(message, index)
	if message.Content[index].Type != ContentText {
		message.Content[index] = ContentBlock{Type: ContentText}
	}
	message.Content[index].Text += delta
}

func appendThinkingDeltaAt(message *AssistantMessage, index int, delta string, metadata *ContentBlock) {
	ensureContentIndex(message, index)
	if message.Content[index].Type != ContentThinking {
		message.Content[index] = ContentBlock{Type: ContentThinking}
	}
	message.Content[index].Thinking += delta
	if metadata != nil {
		message.Content[index].ThinkingSignature = metadata.ThinkingSignature
		message.Content[index].Redacted = metadata.Redacted
	}
}

func ensureContentIndex(message *AssistantMessage, index int) {
	for len(message.Content) <= index {
		message.Content = append(message.Content, ContentBlock{})
	}
}

func contentBlockFromPartial(partial *AssistantMessage, index int, fallback ContentBlock) ContentBlock {
	if partial == nil || index < 0 || index >= len(partial.Content) || partial.Content[index].Type == "" {
		return fallback
	}
	return partial.Content[index]
}

func mergeToolCallFromPartial(message *AssistantMessage, index int, partial *AssistantMessage) {
	if partial == nil || index < 0 || index >= len(partial.Content) || partial.Content[index].ToolCall == nil {
		return
	}
	call := *partial.Content[index].ToolCall
	setContentBlockAt(message, index, ContentBlock{Type: ContentToolCall, ToolCall: &call})
	for toolIndex := range message.ToolCalls {
		if call.ID != "" && message.ToolCalls[toolIndex].ID == call.ID {
			message.ToolCalls[toolIndex] = call
			return
		}
	}
	message.ToolCalls = append(message.ToolCalls, call)
}

func applyToolCallArgumentsAt(message *AssistantMessage, index int, raw string) {
	if index < 0 || index >= len(message.Content) || message.Content[index].ToolCall == nil {
		return
	}
	var arguments map[string]any
	if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
		return
	}
	message.Content[index].ToolCall.Arguments = arguments
	call := *message.Content[index].ToolCall
	for toolIndex := range message.ToolCalls {
		if call.ID == "" || message.ToolCalls[toolIndex].ID == call.ID {
			message.ToolCalls[toolIndex] = call
			return
		}
	}
	message.ToolCalls = append(message.ToolCalls, call)
}

func mergePartialAssistantMessage(message *AssistantMessage, partial *AssistantMessage) {
	if partial == nil {
		return
	}
	if partial.Role != "" {
		message.Role = partial.Role
	}
	if partial.API != "" {
		message.API = partial.API
	}
	if partial.Provider != "" {
		message.Provider = partial.Provider
	}
	if partial.Model != "" {
		message.Model = partial.Model
	}
	if partial.ResponseModel != "" {
		message.ResponseModel = partial.ResponseModel
	}
	if partial.ResponseID != "" {
		message.ResponseID = partial.ResponseID
	}
	if partial.Usage != nil {
		usage := *partial.Usage
		message.Usage = &usage
	}
	if partial.ErrorMessage != "" {
		message.ErrorMessage = partial.ErrorMessage
	}
}
