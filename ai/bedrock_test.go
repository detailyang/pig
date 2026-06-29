package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type bedrockErrorAfterFrameReader struct {
	frame []byte
	done  bool
}

func (reader *bedrockErrorAfterFrameReader) Read(buffer []byte) (int, error) {
	if !reader.done {
		reader.done = true
		return copy(buffer, reader.frame), nil
	}
	return 0, errors.New("read failed")
}

type bedrockSplitFrameReader struct {
	chunks [][]byte
}

func (reader *bedrockSplitFrameReader) Read(buffer []byte) (int, error) {
	for len(reader.chunks) > 0 && len(reader.chunks[0]) == 0 {
		reader.chunks = reader.chunks[1:]
	}
	if len(reader.chunks) == 0 {
		return 0, io.EOF
	}
	chunk := reader.chunks[0]
	read := copy(buffer, chunk)
	reader.chunks[0] = chunk[read:]
	return read, nil
}

func bedrockEventFrame(eventType string, payload []byte) []byte {
	return bedrockEventFrameWithHeader(":event-type", eventType, payload)
}

func bedrockExceptionFrame(exceptionType string, payload []byte) []byte {
	return bedrockEventFrameWithHeader(":exception-type", exceptionType, payload)
}

func base64BedrockAnthropicPayload(event string) []byte {
	return []byte(`{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(event)) + `"}`)
}

func bedrockEventFrameWithHeader(name, value string, payload []byte) []byte {
	headers := []byte{byte(len(name))}
	headers = append(headers, []byte(name)...)
	headers = append(headers, 7)
	headers = append(headers, byte(len(value)>>8), byte(len(value)))
	headers = append(headers, []byte(value)...)
	return bedrockEventFrameWithHeadersAndCRC(headers, payload)
}

func bedrockEventFrameWithHeadersAndCRC(headers []byte, payload []byte) []byte {
	totalLen := 12 + len(headers) + len(payload) + 4
	out := []byte{byte(totalLen >> 24), byte(totalLen >> 16), byte(totalLen >> 8), byte(totalLen)}
	out = append(out, byte(len(headers)>>24), byte(len(headers)>>16), byte(len(headers)>>8), byte(len(headers)))
	preludeCRC := AWSEventStreamCRC32(out[:8])
	out = append(out, byte(preludeCRC>>24), byte(preludeCRC>>16), byte(preludeCRC>>8), byte(preludeCRC))
	out = append(out, headers...)
	out = append(out, payload...)
	messageCRC := AWSEventStreamCRC32(out)
	out = append(out, byte(messageCRC>>24), byte(messageCRC>>16), byte(messageCRC>>8), byte(messageCRC))
	return out
}

func awsEventStreamStringHeader(name, value string) []byte {
	header := []byte{byte(len(name))}
	header = append(header, []byte(name)...)
	header = append(header, 7)
	header = append(header, byte(len(value)>>8), byte(len(value)))
	header = append(header, []byte(value)...)
	return header
}

func awsEventStreamBoolHeader(name string, value bool) []byte {
	header := []byte{byte(len(name))}
	header = append(header, []byte(name)...)
	if value {
		header = append(header, 0)
	} else {
		header = append(header, 1)
	}
	return header
}

func awsEventStreamIntHeader(name string, value int32) []byte {
	header := []byte{byte(len(name))}
	header = append(header, []byte(name)...)
	header = append(header, 4, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
	return header
}

func TestDecodeBedrockEventStreamFrame(t *testing.T) {
	frame := bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"text":"hi"}}`))
	message, rest, ok := DecodeBedrockEventStreamFrame(frame)
	if !ok || len(rest) != 0 {
		t.Fatalf("decode mismatch ok=%v rest=%d", ok, len(rest))
	}
	if message.EventType != "contentBlockDelta" || string(message.Payload) != `{"delta":{"text":"hi"}}` {
		t.Fatalf("message mismatch: %#v", message)
	}
	if _, _, ok := DecodeBedrockEventStreamFrame(frame[:8]); ok {
		t.Fatal("expected partial frame to wait")
	}
}

func TestDecodeBedrockEventStreamFrameRejectsBadCRCLikeUpstream(t *testing.T) {
	frame := bedrockEventFrame("messageStop", []byte(`{}`))
	frame[len(frame)-1] ^= 0xff
	message, rest, ok := DecodeBedrockEventStreamFrame(frame)
	if ok || len(rest) != len(frame) || message.EventType != "" {
		t.Fatalf("bad CRC should fail and leave input untouched ok=%v rest=%d message=%#v", ok, len(rest), message)
	}
}

func TestConsumeBedrockEventStreamErrorsOnPartialFrame(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(bedrockEventFrame("messageStop", []byte(`{}`))[:8]), stream); err == nil {
		t.Fatal("expected partial frame error")
	}
}

func TestConsumeBedrockEventStreamRejectsBadCRCLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	frame := bedrockEventFrame("messageStop", []byte(`{}`))
	frame[len(frame)-1] ^= 0xff
	if err := ConsumeBedrockEventStream(bytes.NewReader(frame), stream); err == nil || !strings.Contains(err.Error(), "message CRC mismatch") {
		t.Fatalf("expected message CRC mismatch, got %v", err)
	}
}

func TestConsumeBedrockEventStreamEmitsCompleteFrameBeforeReaderErrorLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	reader := &bedrockErrorAfterFrameReader{frame: bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"text":"hi"}}`))}
	err := ConsumeBedrockEventStream(reader, stream)
	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("expected read error after complete frame, got %v", err)
	}
	events := stream.Events()
	if len(events) == 0 || events[len(events)-1].Type != EventTextDelta || events[len(events)-1].Delta != "hi" {
		t.Fatalf("complete frame should be emitted before reader error, got %#v", events)
	}
}

func TestConsumeBedrockEventStreamWaitsForSplitFrameLikeUpstream(t *testing.T) {
	frame := bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"text":"hi"}}`))
	reader := &bedrockSplitFrameReader{chunks: [][]byte{frame[:8], frame[8:]}}
	stream := NewAssistantMessageEventStream()

	if err := ConsumeBedrockEventStream(reader, stream); err != nil {
		t.Fatal(err)
	}
	for _, event := range stream.Events() {
		if event.Type == EventTextDelta && event.Delta == "hi" {
			return
		}
	}
	t.Fatalf("split frame should emit after completion, got %#v", stream.Events())
}

func TestConsumeBedrockEventStreamEmitsStartLikeUpstream(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream}
	if err := ConsumeBedrockEventStreamForModel(bytes.NewReader(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`))), stream, model); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 1 || events[0].Type != EventStart || events[0].Partial == nil || events[0].Partial.Model != "anthropic.claude-3" || events[0].Partial.Provider != Provider("bedrock") {
		t.Fatalf("Bedrock stream should begin with upstream Start event, got %#v", events)
	}
}

func TestConsumeBedrockEventStreamException(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream}
	if err := ConsumeBedrockEventStreamForModel(bytes.NewReader(bedrockExceptionFrame("throttlingException", []byte(`throttled`))), stream, model); err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "throttlingException: throttled" {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
	events := stream.Events()
	if len(events) != 2 || events[0].Type != EventStart || events[1].Type != EventError || events[1].Error != "" || events[1].Message == nil || events[1].Message.Model != "anthropic.claude-3" || events[1].Message.Provider != Provider("bedrock") || events[1].Message.API != ApiBedrockConverseStream || events[1].Message.StopReason != StopReasonError || events[1].Message.ErrorMessage != "throttlingException: throttled" {
		t.Fatalf("exception should carry upstream-style message: %#v", events)
	}
}

func TestConsumeBedrockEventStreamReasoningAndToolUse(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"reasoningContent":{"text":"plan"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStart", []byte(`{"start":{"toolUse":{"toolUseId":"tu_1","name":"read"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"toolUse":{"input":"{\"path\":"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"toolUse":{"input":"\"README.md\"}"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"tool_use"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if len(message.Content) != 2 || message.Content[0].Type != ContentThinking || message.Content[0].Thinking != "plan" || message.Content[1].Type != ContentToolCall {
		t.Fatalf("thinking mismatch: %#v", message.Content)
	}
	if message.StopReason != StopReasonToolCalls || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "tu_1" || message.ToolCalls[0].Name != "read" || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool calls mismatch: %#v stop=%s", message.ToolCalls, message.StopReason)
	}
}

func TestConsumeBedrockEventStreamParsesPartialToolUseInput(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockStart", []byte(`{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"tu_1","name":"read"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"path\":\"README.md\""}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":1}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"tool_use"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("tool calls mismatch: %#v ok=%v", message.ToolCalls, ok)
	}
}

func TestConsumeBedrockEventStreamEmitsToolCallEndForEmptyInputLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockStart", []byte(`{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"tu_1","name":"read"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":1}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"tool_use"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	for _, event := range stream.Events() {
		if event.Type == EventToolCallEnd && event.ToolCall != nil && event.ToolCall.ID == "tu_1" && event.ToolCall.Name == "read" && len(event.ToolCall.Arguments) == 0 {
			return
		}
	}
	t.Fatalf("empty toolUse input should still emit ToolCallEnd like upstream, got %#v", stream.Events())
}

func TestConsumeBedrockEventStreamTreatsNullToolUseAsEmptyLikeUpstream(t *testing.T) {
	for _, toolUse := range []string{"null", `"not-an-object"`} {
		var frames bytes.Buffer
		frames.Write(bedrockEventFrame("contentBlockStart", []byte(`{"contentBlockIndex":1,"start":{"toolUse":`+toolUse+`}}`)))
		frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":1}`)))
		frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"tool_use"}`)))

		stream := NewAssistantMessageEventStream()
		if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
			t.Fatal(err)
		}
		message, ok := stream.Result()
		if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "" || message.ToolCalls[0].Name != "" || len(message.ToolCalls[0].Arguments) != 0 {
			t.Fatalf("%s toolUse should create an empty tool call like upstream: %#v ok=%v", toolUse, message, ok)
		}
	}
}

func TestConsumeBedrockEventStreamEmitsToolArgumentDeltas(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockStart", []byte(`{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"tu_1","name":"read"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"path\":"}}}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 3 || events[0].Type != EventStart || events[2].Type != EventToolCallDelta || events[2].ContentIndex != 0 || events[2].Delta != "{\"path\":" || events[2].ToolCall != nil || events[2].Partial == nil {
		t.Fatalf("events mismatch: %#v", events)
	}
}

func TestConsumeBedrockEventStreamSkipsToolInputWithoutStartLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{}"}}}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"tool_use"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	for _, event := range stream.Events() {
		if event.Type == EventToolCallDelta || event.Type == EventToolCallEnd {
			t.Fatalf("tool input without start should be ignored like upstream, got %#v", stream.Events())
		}
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 0 || len(message.Content) != 0 || message.StopReason != StopReasonToolCalls {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeBedrockEventStreamSkipsToolUseWithoutInputLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockStart", []byte(`{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"tu_1","name":"read"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1,"delta":{"toolUse":{}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":1}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"tool_use"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	for _, event := range stream.Events() {
		if event.Type == EventToolCallDelta {
			t.Fatalf("toolUse without input should not emit ToolCallDelta like upstream, got %#v", stream.Events())
		}
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "tu_1" || len(message.ToolCalls[0].Arguments) != 0 {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeBedrockEventStreamNormalizesInvalidContentBlockIndexLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockStart", []byte(`{"contentBlockIndex":-1,"start":{"toolUse":{"toolUseId":"tu_1","name":"read"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1.5,"delta":{"toolUse":{"input":"{\"path\":\"README.md\"}"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"tool_use"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("invalid contentBlockIndex should normalize to 0 like upstream as_u64: %#v ok=%v", message, ok)
	}
}

func TestConsumeBedrockEventStreamToolCallPartialSnapshotsMatchUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockStart", []byte(`{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"tu_1","name":"read"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"path\":"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1,"delta":{"toolUse":{"input":"\"README.md\"}"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":1}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) < 5 || events[0].Type != EventStart || events[1].Type != EventToolCallStart || events[2].Type != EventToolCallDelta || events[3].Type != EventToolCallDelta || events[4].Type != EventToolCallEnd {
		t.Fatalf("tool lifecycle mismatch: %#v", events)
	}
	if events[1].Partial == nil || len(events[1].Partial.Content) != 1 || events[1].Partial.Content[0].ToolCall == nil || len(events[1].Partial.Content[0].ToolCall.Arguments) != 0 {
		t.Fatalf("tool call start partial should contain empty tool args: %#v", events[1].Partial)
	}
	if events[2].Partial == nil || events[2].Partial.Content[0].ToolCall == nil || len(events[2].Partial.Content[0].ToolCall.Arguments) != 0 {
		t.Fatalf("delta partial should keep empty tool args: %#v", events[2].Partial)
	}
	if events[3].Partial == nil || events[3].Partial.Content[0].ToolCall == nil || len(events[3].Partial.Content[0].ToolCall.Arguments) != 0 {
		t.Fatalf("delta partial should not be mutated by later stop: %#v", events[3].Partial)
	}
	if events[4].Partial == nil || events[4].Partial.Content[0].ToolCall == nil || events[4].Partial.Content[0].ToolCall.Arguments["path"] != "README.md" {
		t.Fatalf("end partial should contain final tool args: %#v", events[4].Partial)
	}
}

func TestConsumeBedrockEventStreamKeepsToolUseIndexAfterTextBlock(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"hello"}}`)))
	frames.Write(bedrockEventFrame("contentBlockStart", []byte(`{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"tu_1","name":"read"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"path\":\"README.md\"}"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":1}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"tool_use"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Text() != "hello" || len(message.ToolCalls) != 1 || message.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeBedrockEventStreamTextLifecycleMatchesUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"hel"}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"lo"}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 6 || events[0].Type != EventStart || events[1].Type != EventTextStart || events[2].Type != EventTextDelta || events[3].Type != EventTextDelta || events[4].Type != EventTextEnd || events[5].Type != EventDone {
		t.Fatalf("Bedrock text lifecycle mismatch: %#v", events)
	}
	if events[1].ContentIndex != 0 || events[2].ContentIndex != 0 || events[4].Content != "hello" {
		t.Fatalf("Bedrock text event content mismatch: %#v", events)
	}
	if events[5].Message == nil || events[5].Message.Text() != "hello" || events[5].Message.StopReason != StopReasonEndTurn {
		t.Fatalf("Bedrock done message mismatch: %#v", events[5])
	}
}

func TestConsumeBedrockEventStreamDeltaPrefersTextOverReasoningLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"hello","reasoningContent":{"text":"plan"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	for _, event := range stream.Events() {
		if event.Type == EventThinkingStart || event.Type == EventThinkingDelta || event.Type == EventThinkingEnd {
			t.Fatalf("text delta should win over reasoningContent like upstream, got %#v", stream.Events())
		}
	}
	message, ok := stream.Result()
	if !ok || message.Text() != "hello" || len(message.Content) != 1 || message.Content[0].Type != ContentText {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeBedrockEventStreamIgnoresNonStringTextForReasoningLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":null,"reasoningContent":{"text":"plan"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 5 || events[0].Type != EventStart || events[1].Type != EventThinkingStart || events[2].Type != EventThinkingDelta || events[3].Type != EventThinkingEnd || events[4].Type != EventDone {
		t.Fatalf("non-string text should fall through to reasoningContent like upstream, got %#v", events)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Type != ContentThinking || message.Content[0].Thinking != "plan" {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
}

func TestConsumeBedrockEventStreamKeepsEmptyTextDeltaLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":""}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 5 || events[0].Type != EventStart || events[1].Type != EventTextStart || events[2].Type != EventTextDelta || events[3].Type != EventTextEnd || events[4].Type != EventDone {
		t.Fatalf("Bedrock empty text lifecycle mismatch: %#v", events)
	}
	if events[2].Delta != "" || events[3].Content != "" || events[4].Message == nil || len(events[4].Message.Content) != 1 || events[4].Message.Content[0].Type != ContentText || events[4].Message.Text() != "" {
		t.Fatalf("Bedrock empty text content mismatch: %#v", events)
	}
}

func TestConsumeBedrockEventStreamRepeatsTextStartAfterEmptyDeltaLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":""}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"hi"}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 7 || events[0].Type != EventStart || events[1].Type != EventTextStart || events[2].Type != EventTextDelta || events[3].Type != EventTextStart || events[4].Type != EventTextDelta || events[5].Type != EventTextEnd || events[6].Type != EventDone {
		t.Fatalf("Bedrock empty-then-text lifecycle mismatch: %#v", events)
	}
	if events[2].Delta != "" || events[4].Delta != "hi" || events[5].Content != "hi" || events[6].Message == nil || events[6].Message.Text() != "hi" {
		t.Fatalf("Bedrock empty-then-text content mismatch: %#v", events)
	}
}

func TestConsumeBedrockEventStreamKeepsEmptyReasoningDeltaLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"reasoningContent":{"text":""}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 5 || events[0].Type != EventStart || events[1].Type != EventThinkingStart || events[2].Type != EventThinkingDelta || events[3].Type != EventThinkingEnd || events[4].Type != EventDone {
		t.Fatalf("Bedrock empty reasoning lifecycle mismatch: %#v", events)
	}
	if events[2].Delta != "" || events[3].Content != "" || events[4].Message == nil || len(events[4].Message.Content) != 1 || events[4].Message.Content[0].Type != ContentThinking || events[4].Message.Content[0].Thinking != "" {
		t.Fatalf("Bedrock empty reasoning content mismatch: %#v", events)
	}
}

func TestConsumeBedrockEventStreamRepeatsThinkingStartAfterEmptyDeltaLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"reasoningContent":{"text":""}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"reasoningContent":{"text":"plan"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 7 || events[0].Type != EventStart || events[1].Type != EventThinkingStart || events[2].Type != EventThinkingDelta || events[3].Type != EventThinkingStart || events[4].Type != EventThinkingDelta || events[5].Type != EventThinkingEnd || events[6].Type != EventDone {
		t.Fatalf("Bedrock empty-then-thinking lifecycle mismatch: %#v", events)
	}
	if events[2].Delta != "" || events[4].Delta != "plan" || events[5].Content != "plan" || events[6].Message == nil || events[6].Message.Content[0].Thinking != "plan" {
		t.Fatalf("Bedrock empty-then-thinking content mismatch: %#v", events)
	}
}

func TestConsumeBedrockEventStreamDoesNotWriteTextIntoThinkingBlockLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"reasoningContent":{"text":"plan"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"hello"}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 6 || events[0].Type != EventStart || events[1].Type != EventThinkingStart || events[2].Type != EventThinkingDelta || events[3].Type != EventTextDelta || events[4].Type != EventThinkingEnd || events[5].Type != EventDone {
		t.Fatalf("Bedrock thinking-then-text lifecycle mismatch: %#v", events)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Type != ContentThinking || message.Content[0].Thinking != "plan" || message.Content[0].Text != "" || message.Text() != "" {
		t.Fatalf("text delta should not mutate thinking block like upstream: %#v ok=%v", message, ok)
	}
}

func TestConsumeBedrockEventStreamDoesNotWriteThinkingIntoTextBlockLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"hello"}}`)))
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"reasoningContent":{"text":"plan"}}}`)))
	frames.Write(bedrockEventFrame("contentBlockStop", []byte(`{"contentBlockIndex":0}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	events := stream.Events()
	if len(events) != 6 || events[0].Type != EventStart || events[1].Type != EventTextStart || events[2].Type != EventTextDelta || events[3].Type != EventThinkingDelta || events[4].Type != EventTextEnd || events[5].Type != EventDone {
		t.Fatalf("Bedrock text-then-thinking lifecycle mismatch: %#v", events)
	}
	message, ok := stream.Result()
	if !ok || len(message.Content) != 1 || message.Content[0].Type != ContentText || message.Content[0].Text != "hello" || message.Content[0].Thinking != "" || message.Text() != "hello" {
		t.Fatalf("thinking delta should not mutate text block like upstream: %#v ok=%v", message, ok)
	}
}

func TestConsumeBedrockEventStreamKeepsMetadataAfterMessageStopLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"contentBlockIndex":0,"delta":{"text":"hello"}}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))
	frames.Write(bedrockEventFrame("metadata", []byte(`{"usage":{"inputTokens":3,"outputTokens":4,"cacheReadInputTokens":2,"cacheWriteInputTokens":1}}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil || message.Usage.InputTokens != 3 || message.Usage.OutputTokens != 4 || message.Usage.TotalTokenCount != 10 {
		t.Fatalf("message should include usage after messageStop: %#v ok=%v", message, ok)
	}
}

func TestConsumeBedrockEventStreamMapsStopReasons(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(bedrockEventFrame("messageStop", []byte(`{"stopReason":"max_tokens"}`))), stream); err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonMaxTokens {
		t.Fatalf("max tokens mismatch: %#v ok=%v", message, ok)
	}

	stream = NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(bedrockEventFrame("messageStop", []byte(`{"stopReason":"stop_sequence"}`))), stream); err != nil {
		t.Fatal(err)
	}
	message, ok = stream.Result()
	if !ok || message.StopReason != StopReasonEndTurn {
		t.Fatalf("stop sequence mismatch: %#v ok=%v", message, ok)
	}
}

func TestBuildBedrockConverseStreamURL(t *testing.T) {
	got := BuildBedrockConverseStreamURL("https://bedrock-runtime.us-east-1.amazonaws.com/", "anthropic.claude-3")
	want := "https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude-3/converse-stream"
	if got != want {
		t.Fatalf("url mismatch: %s", got)
	}
}

func TestConsumeBedrockEventStreamIgnoresNegativeUsageLikeUpstream(t *testing.T) {
	var frames bytes.Buffer
	frames.Write(bedrockEventFrame("metadata", []byte(`{"usage":{"inputTokens":-1,"outputTokens":2,"cacheReadInputTokens":1.5,"cacheWriteInputTokens":4}}`)))
	frames.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))

	stream := NewAssistantMessageEventStream()
	if err := ConsumeBedrockEventStream(bytes.NewReader(frames.Bytes()), stream); err != nil {
		t.Fatal(err)
	}
	message, ok := stream.Result()
	if !ok || message.Usage == nil {
		t.Fatalf("expected completed message with usage: %#v ok=%v", message, ok)
	}
	if message.Usage.InputTokens != 0 || message.Usage.OutputTokens != 2 || message.Usage.CacheReadTokens != 0 || message.Usage.CacheWriteTokens != 4 || message.Usage.TotalTokens() != 6 {
		t.Fatalf("non-u64 usage fields should be ignored like upstream as_u64: %#v", message.Usage)
	}
}

func TestBuildBedrockRequestBody(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{Messages: []Message{
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}, {Type: ContentImage, MimeType: "image/png", Data: "abc"}}},
		{Role: RoleAssistant, Content: []ContentBlock{{Type: ContentText, Text: "ok"}, {Type: ContentToolCall, ToolCall: &ToolCall{ID: "tu_1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}}},
		{Role: RoleTool, ToolCallID: "tu_1", Content: []ContentBlock{{Type: ContentText, Text: "done"}}},
	}, Tools: []Tool{{Name: "read", Description: "read files", Parameters: map[string]any{"type": "object"}}}}, StreamOptions{MaxTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]map[string]any)
	if messages[0]["role"] != "user" || messages[0]["content"].([]map[string]any)[0]["text"] != "hi" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
	if messages[1]["content"].([]map[string]any)[1]["toolUse"] == nil {
		t.Fatalf("tool use missing: %#v", messages[1])
	}
	if messages[2]["content"].([]map[string]any)[0]["toolResult"] == nil {
		t.Fatalf("tool result missing: %#v", messages[2])
	}
	if body["inferenceConfig"].(map[string]any)["maxTokens"] != 512 {
		t.Fatalf("inference config mismatch: %#v", body)
	}
	if body["toolConfig"].(map[string]any)["tools"] == nil {
		t.Fatalf("tool config mismatch: %#v", body)
	}
}

func TestBuildBedrockRequestBodyIgnoresRoleSystemMessagesLikeUpstream(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{Messages: []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: ContentText, Text: "sys"}}},
		{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}},
	}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["system"]; ok {
		t.Fatalf("RoleSystem messages should not become Bedrock system like upstream: %#v", body)
	}
	messages := body["messages"].([]map[string]any)
	if len(messages) != 1 || messages[0]["role"] != "user" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
}

func TestBuildBedrockRequestBodyDeduplicatesToolCallBlocks(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{Messages: []Message{{
		Role:      RoleAssistant,
		Content:   []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "tu_1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}},
		ToolCalls: []ToolCall{{ID: "tu_1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
	}}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("tool calls should be deduplicated: %#v", content)
	}
}

func TestBuildBedrockRequestBodyDefaultsNilToolCallArgumentsLikeUpstream(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{Messages: []Message{{
		Role:    RoleAssistant,
		Content: []ContentBlock{{Type: ContentToolCall, ToolCall: &ToolCall{ID: "tu_1", Name: "read"}}},
	}}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	toolUse := content[0]["toolUse"].(map[string]any)
	input, ok := toolUse["input"].(map[string]any)
	if !ok || len(input) != 0 {
		t.Fatalf("nil tool call arguments should serialize as empty object like upstream: %#v", toolUse)
	}
}

func TestBuildBedrockRequestBodyIgnoresLegacyAssistantToolCallsLikeUpstream(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{Messages: []Message{{
		Role:      RoleAssistant,
		ToolCalls: []ToolCall{{ID: "tu_1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
	}}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]map[string]any)
	if len(messages) != 0 {
		t.Fatalf("legacy assistant tool calls should be ignored like upstream: %#v", messages)
	}
}

func TestBuildBedrockRequestBodyIgnoresAssistantImagesLikeUpstream(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{Messages: []Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{
			{Type: ContentImage, MimeType: "image/png", Data: "abc"},
			{Type: ContentText, Text: "ok"},
		},
	}}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	if len(content) != 1 || content[0]["text"] != "ok" {
		t.Fatalf("assistant image blocks should be ignored like upstream: %#v", messages)
	}
}

func TestBuildBedrockRequestBodyIgnoresUserToolCallsLikeUpstream(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{Messages: []Message{{
		Role: RoleUser,
		Content: []ContentBlock{
			{Type: ContentToolCall, ToolCall: &ToolCall{ID: "tu_1", Name: "read", Arguments: map[string]any{"path": "README.md"}}},
			{Type: ContentText, Text: "hi"},
		},
	}}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	if len(content) != 1 || content[0]["text"] != "hi" {
		t.Fatalf("user tool call blocks should be ignored like upstream: %#v", messages)
	}
}

func TestBuildBedrockRequestBodyPreservesEmptyImageFormatLikeUpstream(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{Messages: []Message{{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentImage, MimeType: "image/", Data: "abc"}},
	}}}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]map[string]any)
	content := messages[0]["content"].([]map[string]any)
	image := content[0]["image"].(map[string]any)
	if image["format"] != "" {
		t.Fatalf("image/ mime type should serialize to empty format like upstream: %#v", image)
	}
}

func TestBuildBedrockRequestBodyUsesContextSystemPrompt(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{
		SystemPrompt: "sys",
		Messages:     []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if body["system"].([]map[string]any)[0]["text"] != "sys" {
		t.Fatalf("system mismatch: %#v", body)
	}
	messages := body["messages"].([]map[string]any)
	if len(messages) != 1 || messages[0]["role"] != "user" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
}

func TestBuildBedrockRequestBodyPreservesExplicitEmptySystemPromptLikeUpstream(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{
		HasSystemPrompt: true,
		Messages:        []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}},
	}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	system, ok := body["system"].([]map[string]any)
	if !ok || len(system) != 1 || system[0]["text"] != "" {
		t.Fatalf("explicit empty system prompt should be preserved like upstream Some(empty): %#v", body)
	}
}

func TestBuildBedrockRequestBodyDefaultsMaxTokens(t *testing.T) {
	body, err := BuildBedrockRequestBody(Context{}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if body["inferenceConfig"].(map[string]any)["maxTokens"] != 4096 {
		t.Fatalf("inference config mismatch: %#v", body["inferenceConfig"])
	}
}

func TestConvertMessagesForBedrockPreservesToolResultTextBlocks(t *testing.T) {
	messages := ConvertMessagesForBedrock([]Message{{
		Role:       RoleTool,
		ToolCallID: "tu_1",
		Content: []ContentBlock{
			{Type: ContentText, Text: "one"},
			{Type: ContentText, Text: "two"},
		},
	}})
	toolResult := messages[0]["content"].([]map[string]any)[0]["toolResult"].(map[string]any)
	content := toolResult["content"].([]map[string]any)
	if len(content) != 2 || content[0]["text"] != "one" || content[1]["text"] != "two" {
		t.Fatalf("tool result content mismatch: %#v", content)
	}
}

func TestConvertMessagesForBedrockUsesExplicitToolResultError(t *testing.T) {
	messages := ConvertMessagesForBedrock([]Message{{
		Role:       RoleTool,
		ToolCallID: "tu_1",
		Content:    []ContentBlock{{Type: ContentText, Text: "failed"}},
		IsError:    true,
	}})
	toolResult := messages[0]["content"].([]map[string]any)[0]["toolResult"].(map[string]any)
	if toolResult["status"] != "error" {
		t.Fatalf("tool result mismatch: %#v", toolResult)
	}
}

func TestConvertMessagesForBedrockToolResultStatusIgnoresStopReasonLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForBedrock([]Message{{
		Role:       RoleTool,
		ToolCallID: "tu_1",
		Content:    []ContentBlock{{Type: ContentText, Text: "failed"}},
		StopReason: StopReasonError,
	}})
	toolResult := messages[0]["content"].([]map[string]any)[0]["toolResult"].(map[string]any)
	if toolResult["status"] != "success" {
		t.Fatalf("tool result status should use explicit IsError only like upstream: %#v", toolResult)
	}
}

func TestConvertMessagesForBedrockIgnoresToolResultImageBlocksLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForBedrock([]Message{{
		Role:       RoleTool,
		ToolCallID: "tu_1",
		Content: []ContentBlock{
			{Type: ContentImage, MimeType: "image/png", Data: "abc"},
		},
	}})
	toolResult := messages[0]["content"].([]map[string]any)[0]["toolResult"].(map[string]any)
	content := toolResult["content"].([]map[string]any)
	if len(content) != 0 {
		t.Fatalf("tool result image blocks should be ignored like upstream: %#v", content)
	}
}

func TestConvertMessagesForBedrockDefaultsNonImageMimeFormatLikeUpstream(t *testing.T) {
	messages := ConvertMessagesForBedrock([]Message{{
		Role:    RoleUser,
		Content: []ContentBlock{{Type: ContentImage, MimeType: "application/octet-stream", Data: "abc"}},
	}})
	image := messages[0]["content"].([]map[string]any)[0]["image"].(map[string]any)
	if image["format"] != "png" {
		t.Fatalf("non-image mime should default to png like upstream: %#v", image)
	}
}

func TestBedrockProviderRequestSkeleton(t *testing.T) {
	var body map[string]any
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model/anthropic.claude-3/converse-stream" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer bedrock-token" || r.Header.Get("Accept") != "application/vnd.amazon.eventstream" {
			t.Fatalf("headers mismatch auth=%q accept=%q", r.Header.Get("Authorization"), r.Header.Get("Accept"))
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		rawBody = string(data)
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		var stream bytes.Buffer
		stream.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"text":"hel"}}`)))
		stream.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"text":"lo"}}`)))
		stream.Write(bedrockEventFrame("metadata", []byte(`{"usage":{"inputTokens":3,"outputTokens":4,"cacheReadInputTokens":2,"cacheWriteInputTokens":1}}`)))
		stream.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))
		_, _ = w.Write(stream.Bytes())
	}))
	defer server.Close()

	provider := NewBedrockProvider(WithBedrockHTTPClient(server.Client()))
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "<tag>&value"}}}}}, StreamOptions{APIKey: "bedrock-token"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "hello" || message.StopReason != StopReasonEndTurn || message.Usage == nil || message.Usage.InputTokens != 3 || message.Usage.OutputTokens != 4 || message.Usage.CacheReadTokens != 2 || message.Usage.CacheWriteTokens != 1 || message.Usage.TotalTokenCount != 10 || !message.Usage.HasTotalTokens || message.Usage.TotalTokens() != 10 {
		t.Fatalf("message mismatch: %#v", message)
	}
	if body["messages"] == nil {
		t.Fatalf("body mismatch: %#v", body)
	}
	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("request body should not HTML-escape JSON strings like upstream serde_json: %s", rawBody)
	}
	if !strings.Contains(rawBody, `"text":"<tag>&value"`) {
		t.Fatalf("request body missing unescaped content: %s", rawBody)
	}
}

func TestBedrockProviderStreamSimpleUsesOnlyBaseOptionsLikeUpstream(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))
	}))
	defer server.Close()

	provider := NewBedrockProvider(WithBedrockHTTPClient(server.Client()))
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream, BaseURL: server.URL}
	_, ok := provider.StreamSimple(context.Background(), model, Context{}, SimpleStreamOptions{
		Base: StreamOptions{APIKey: "bedrock-token", MaxTokens: 128},
	}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if body["inferenceConfig"].(map[string]any)["maxTokens"] != float64(128) {
		t.Fatalf("Bedrock StreamSimple should ignore non-base simple fields like upstream: %#v", body)
	}
}

func TestBedrockProviderHTTPErrorMessageLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad bedrock", http.StatusBadRequest)
	}))
	defer server.Close()

	provider := NewBedrockProvider(WithBedrockHTTPClient(server.Client()))
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream, BaseURL: server.URL}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "bedrock-token"})
	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected error message")
	}
	if message.ErrorMessage != "Bedrock API error (400 Bad Request): bad bedrock" {
		t.Fatalf("error mismatch: %#v", message)
	}
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "anthropic.claude-3" || events[0].Message.Provider != Provider("bedrock") || events[0].Message.API != ApiBedrockConverseStream || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "Bedrock API error (400 Bad Request): bad bedrock" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("HTTP error should carry provider-aware upstream message: %#v", events)
	}
}

func TestBedrockProviderHTTPErrorBodyIsNotTruncatedLikeUpstream(t *testing.T) {
	body := strings.Repeat("x", 5000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	provider := NewBedrockProvider(WithBedrockHTTPClient(server.Client()))
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "bedrock-token"}).Result()
	if !ok || message.StopReason != StopReasonError {
		t.Fatalf("expected HTTP error, got %#v ok=%v", message, ok)
	}
	if !strings.HasSuffix(message.ErrorMessage, body) {
		t.Fatalf("Bedrock HTTP error body should not be truncated like upstream, got length %d", len(message.ErrorMessage))
	}
}

func TestBedrockProviderSendErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewBedrockProvider(WithBedrockHTTPClient(&http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}))
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream, BaseURL: "https://bedrock.invalid"}
	maxRetries := 0
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "bedrock-token", MaxRetries: &maxRetries})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || message.ErrorMessage != "http error: Post \"https://bedrock.invalid/model/anthropic.claude-3/converse-stream\": dial failed" {
		t.Fatalf("send error mismatch: %#v ok=%v", message, ok)
	}
}

func TestBedrockProviderNewRequestErrorIncludesHTTPErrorPrefixLikeUpstream(t *testing.T) {
	provider := NewBedrockProvider()
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream, BaseURL: "://bad-url"}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "bedrock-token"})
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonError || !strings.HasPrefix(message.ErrorMessage, "http error: ") {
		t.Fatalf("request build error mismatch: %#v ok=%v", message, ok)
	}
}

func TestBedrockProviderMissingBaseURLErrorCarriesModelLikeUpstream(t *testing.T) {
	provider := NewBedrockProvider()
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream}
	stream := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "bedrock-token"})
	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].Error != "" || events[0].Message == nil || events[0].Message.Model != "anthropic.claude-3" || events[0].Message.Provider != Provider("bedrock") || events[0].Message.API != ApiBedrockConverseStream || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "Bedrock base URL is not set" || events[0].Message.Timestamp == 0 || events[0].Message.Usage == nil {
		t.Fatalf("missing base URL should carry provider-aware upstream message: %#v", events)
	}
}

func TestBedrockProviderUsesSigV4WhenBearerTokenMissing(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_SESSION_TOKEN", "session-token")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model/anthropic.claude-3/converse-stream" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIATEST/") || r.Header.Get("x-amz-date") == "" || r.Header.Get("x-amz-content-sha256") == "" || r.Header.Get("x-amz-security-token") != "session-token" {
			t.Fatalf("sigv4 headers missing auth=%q date=%q hash=%q token=%q", r.Header.Get("Authorization"), r.Header.Get("x-amz-date"), r.Header.Get("x-amz-content-sha256"), r.Header.Get("x-amz-security-token"))
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"text":"signed"}}`)))
		_, _ = w.Write(bedrockEventFrame("messageStop", []byte(`{"stopReason":"end_turn"}`)))
	}))
	defer server.Close()

	provider := NewBedrockProvider(WithBedrockHTTPClient(server.Client()))
	model := Model{ID: "anthropic.claude-3", Provider: Provider("bedrock"), API: ApiBedrockConverseStream, BaseURL: server.URL}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}}}, StreamOptions{}).Result()
	if !ok || message.Text() != "signed" {
		t.Fatalf("signed stream mismatch message=%#v ok=%v", message, ok)
	}
}

func TestAWSRegionResolution(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	if got := AWSRegion("https://bedrock-runtime.eu-west-1.amazonaws.com"); got != "eu-west-1" {
		t.Fatalf("region mismatch: %s", got)
	}
	t.Setenv("AWS_REGION", "ap-southeast-1")
	if got := AWSRegion("https://bedrock-runtime.eu-west-1.amazonaws.com"); got != "ap-southeast-1" {
		t.Fatalf("env region mismatch: %s", got)
	}
}

func TestBedrockProviderRegisteredBuiltin(t *testing.T) {
	ClearAPIProviders()
	t.Cleanup(ClearAPIProviders)
	RegisterBuiltinProviders()
	if _, ok := GetAPIProvider(ApiBedrockConverseStream); !ok {
		t.Fatal("bedrock provider was not registered")
	}
}

func TestAmazonBedrockProviderAliasMatchesUpstreamName(t *testing.T) {
	provider := AmazonBedrockProvider{}
	if provider.API() != ApiBedrockConverseStream {
		t.Fatalf("unexpected API: %s", provider.API())
	}
}

func TestBedrockRegisterCompatibilityPlaceholder(t *testing.T) {
	Register()
}

func TestBedrockCredsFromEnvMatchesUpstream(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_SESSION_TOKEN", "session-token")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-1")

	creds, ok := (BedrockCreds{}).FromEnv()
	if !ok {
		t.Fatal("expected credentials")
	}
	if creds.AccessKey != "AKIATEST" || creds.SecretKey != "secret" || creds.SessionToken != "session-token" || creds.Region != "eu-west-1" {
		t.Fatalf("credentials mismatch: %#v", creds)
	}
}

func TestBedrockCredsFromEnvReturnsNoneWithoutKeysLikeUpstream(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	if creds, ok := BedrockCredsFromEnv(); ok {
		t.Fatalf("expected no creds without required keys, got %#v", creds)
	}
	if creds, ok := (BedrockCreds{}).FromEnv(); ok {
		t.Fatalf("expected alias no creds without required keys, got %#v", creds)
	}
}

func TestInvokeBedrockPostsSignedJSON(t *testing.T) {
	var rawBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model/anthropic.claude-3/invoke" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("request mismatch method=%s content-type=%s", r.Method, r.Header.Get("Content-Type"))
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIATEST/") || r.Header.Get("x-amz-date") == "" || r.Header.Get("x-amz-content-sha256") == "" {
			t.Fatalf("sigv4 headers missing auth=%q date=%q hash=%q", r.Header.Get("Authorization"), r.Header.Get("x-amz-date"), r.Header.Get("x-amz-content-sha256"))
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		rawBody = string(data)
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		if body["prompt"] != "<tag>&value" {
			t.Fatalf("body mismatch: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"completion":"ok"}`))
	}))
	defer server.Close()

	got, err := InvokeBedrock(context.Background(), server.Client(), server.URL, BedrockCreds{AccessKey: "AKIATEST", SecretKey: "secret", Region: "us-east-1"}, "anthropic.claude-3", map[string]any{"prompt": "<tag>&value"})
	if err != nil {
		t.Fatal(err)
	}
	if got["completion"] != "ok" {
		t.Fatalf("response mismatch: %#v", got)
	}
	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("request body should not HTML-escape JSON strings like upstream serde_json: %s", rawBody)
	}
	if !strings.Contains(rawBody, `"prompt":"<tag>&value"`) {
		t.Fatalf("request body missing unescaped prompt: %s", rawBody)
	}
}

func TestInvokeBedrockStatusErrorTruncatesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("x", 600)))
	}))
	defer server.Close()

	_, err := InvokeBedrock(context.Background(), server.Client(), server.URL, BedrockCreds{AccessKey: "AKIATEST", SecretKey: "secret", Region: "us-east-1"}, "m", map[string]any{})
	if err == nil {
		t.Fatal("expected status error")
	}
	if got := err.Error(); !strings.HasPrefix(got, "HTTP 400: ") || len(strings.TrimPrefix(got, "HTTP 400: ")) != 500 {
		t.Fatalf("status error mismatch: %q", got)
	}
	var bedrockErr BedrockError
	if !errors.As(err, &bedrockErr) || bedrockErr.Kind != BedrockErrorExchange {
		t.Fatalf("expected BedrockErrorExchange, got %#v", err)
	}
}

func TestInvokeBedrockUpstreamWrapperUsesExistingInvoke(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"completion":"ok"}`))
	}))
	defer server.Close()

	got, err := Invoke(context.Background(), server.Client(), server.URL, BedrockCreds{AccessKey: "AKIATEST", SecretKey: "secret", Region: "us-east-1"}, "anthropic.claude-3", map[string]any{"prompt": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got["completion"] != "ok" {
		t.Fatalf("response mismatch: %#v", got)
	}
}

func TestBedrockAnthropicConverterIngestsBase64AnthropicEvents(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	alias := NewConverter()
	var _ Converter = *alias
	if _, err := alias.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"ping"}`)}); err != nil {
		t.Fatalf("upstream Converter alias should ingest ping: %v", err)
	}
	start := base64BedrockAnthropicPayload(`{"type":"message_start","message":{"id":"msg_1","role":"assistant","model":"claude"}}`)
	events, err := converter.Ingest(BedrockEventStreamMessage{Payload: start})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventStart || events[0].Partial.ResponseID != "msg_1" || events[0].Partial.Model != "claude" {
		t.Fatalf("start event mismatch: %#v", events)
	}
	if events[0].Partial.Role != AssistantRoleAssistant {
		t.Fatalf("message_start partial should default assistant role like upstream: %#v", events[0].Partial)
	}

	block := base64BedrockAnthropicPayload(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	events, err = converter.Ingest(BedrockEventStreamMessage{Payload: block})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventTextStart || events[0].ContentIndex != 0 {
		t.Fatalf("text start mismatch: %#v", events)
	}

	delta := base64BedrockAnthropicPayload(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)
	events, err = converter.Ingest(BedrockEventStreamMessage{Payload: delta})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventTextDelta || events[0].Delta != "hi" || events[0].Partial.Text() != "hi" {
		t.Fatalf("text delta mismatch: %#v", events)
	}

	stop := base64BedrockAnthropicPayload(`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":3}}`)
	events, err = converter.Ingest(BedrockEventStreamMessage{Payload: stop})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("message_delta should only update state, got %#v", events)
	}
	events, err = converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"message_stop"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventDone || events[0].DoneReason != DoneReasonLength || events[0].Partial.StopReason != StopReasonMaxTokens || events[0].Partial.Usage.OutputTokens != 3 {
		t.Fatalf("done event mismatch: %#v", events)
	}
}

func TestBedrockAnthropicConverterErrorsOnMissingBytes(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	_, err := converter.Ingest(BedrockEventStreamMessage{Payload: []byte(`{}`)})
	if err == nil || err.Error() != "bedrock chunk missing `bytes`" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInvokeBedrockStreamPostsSignedJSONAndParsesFrames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/model/anthropic.claude-3/invoke-with-response-stream" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/vnd.amazon.eventstream" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("headers mismatch accept=%q content-type=%q", r.Header.Get("Accept"), r.Header.Get("Content-Type"))
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIATEST/") || r.Header.Get("x-amz-date") == "" || r.Header.Get("x-amz-content-sha256") == "" {
			t.Fatalf("sigv4 headers missing auth=%q date=%q hash=%q", r.Header.Get("Authorization"), r.Header.Get("x-amz-date"), r.Header.Get("x-amz-content-sha256"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["prompt"] != "hi" {
			t.Fatalf("body mismatch: %#v", body)
		}
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(bedrockEventFrameWithHeader(":event-type", "chunk", []byte(`{"bytes":"aGk="}`)))
		_, _ = w.Write(bedrockEventFrameWithHeader(":event-type", "done", []byte(`{}`)))
	}))
	defer server.Close()

	messages, err := InvokeBedrockStream(context.Background(), server.Client(), server.URL, BedrockCreds{AccessKey: "AKIATEST", SecretKey: "secret", Region: "us-east-1"}, "anthropic.claude-3", map[string]any{"prompt": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].EventType() != "chunk" || string(messages[0].Payload) != `{"bytes":"aGk="}` || messages[1].EventType() != "done" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
}

func TestInvokeBedrockStreamStatusErrorTruncatesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(strings.Repeat("x", 600)))
	}))
	defer server.Close()

	_, err := InvokeBedrockStream(context.Background(), server.Client(), server.URL, BedrockCreds{AccessKey: "AKIATEST", SecretKey: "secret", Region: "us-east-1"}, "m", map[string]any{})
	if err == nil {
		t.Fatal("expected status error")
	}
	if got := err.Error(); !strings.HasPrefix(got, "HTTP 503: ") || len(strings.TrimPrefix(got, "HTTP 503: ")) != 500 {
		t.Fatalf("status error mismatch: %q", got)
	}
}

func TestInvokeStreamUpstreamWrapperUsesExistingStreamInvoke(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(bedrockEventFrameWithHeader(":event-type", "done", []byte(`{}`)))
	}))
	defer server.Close()

	messages, err := InvokeStream(context.Background(), server.Client(), server.URL, BedrockCreds{AccessKey: "AKIATEST", SecretKey: "secret", Region: "us-east-1"}, "anthropic.claude-3", map[string]any{"prompt": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].EventType() != "done" {
		t.Fatalf("messages mismatch: %#v", messages)
	}
}

func TestBedrockAnthropicConverterStopsOnlyOnMessageStop(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)})
	if err != nil {
		t.Fatal(err)
	}

	events, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"input_tokens":2,"output_tokens":3,"cache_read_input_tokens":4,"cache_creation_input_tokens":5}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("message_delta should only update state, got %#v", events)
	}

	events, err = converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_stop","index":0}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventTextEnd || events[0].Content != "hi" {
		t.Fatalf("text end mismatch: %#v", events)
	}

	events, err = converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"message_stop"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventDone || events[0].DoneReason != DoneReasonLength || events[0].Message.StopReason != StopReasonMaxTokens {
		t.Fatalf("done mismatch: %#v", events)
	}
	if events[0].Message.Usage.InputTokens != 2 || events[0].Message.Usage.OutputTokens != 3 || events[0].Message.Usage.CacheReadTokens != 4 || events[0].Message.Usage.CacheWriteTokens != 5 {
		t.Fatalf("usage mismatch: %#v", events[0].Message.Usage)
	}
	if events[0].Message.Usage.TotalTokens() != 5 {
		t.Fatalf("bedrock anthropic total tokens should match upstream input+output only, got %#v", events[0].Message.Usage)
	}
}

func TestBedrockAnthropicConverterIgnoresNonU64UsageLikeUpstream(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":-1,"output_tokens":2,"cache_read_input_tokens":1.5,"cache_creation_input_tokens":4}}`)})
	if err != nil {
		t.Fatal(err)
	}
	events, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"message_stop"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Message == nil || events[0].Message.Usage == nil {
		t.Fatalf("expected done message with usage: %#v", events)
	}
	usage := events[0].Message.Usage
	if usage.InputTokens != 0 || usage.OutputTokens != 2 || usage.CacheReadTokens != 0 || usage.CacheWriteTokens != 4 || usage.TotalTokens() != 2 {
		t.Fatalf("non-u64 usage fields should be ignored like upstream u64 options: %#v", usage)
	}
}

func TestBedrockAnthropicConverterKeepsStopReasonWhenMessageDeltaOmitsItLikeUpstream(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"message_delta","delta":{},"usage":{"input_tokens":2}}`)})
	if err != nil {
		t.Fatal(err)
	}
	events, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"message_stop"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].DoneReason != DoneReasonToolCalls || events[0].Message.StopReason != StopReasonToolCalls {
		t.Fatalf("message_delta without stop_reason should preserve previous stop reason like upstream: %#v", events)
	}
}

func TestBedrockAnthropicConverterDeltaDoesNotRewriteMissingBlockTypeLikeUpstream(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	events, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"plan"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventThinkingDelta || events[0].Partial == nil || len(events[0].Partial.Content) != 2 {
		t.Fatalf("thinking delta mismatch: %#v", events)
	}
	if events[0].Partial.Content[1].Type != ContentText || events[0].Partial.Content[1].Thinking != "" {
		t.Fatalf("missing delta target should stay padded text like upstream: %#v", events[0].Partial.Content)
	}
}

func TestBedrockAnthropicConverterEndsThinkingAndToolCallBlocks(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	events, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_stop","index":0}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventThinkingEnd || events[0].Content != "plan" {
		t.Fatalf("thinking end mismatch: %#v", events)
	}

	_, err = converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	events, err = converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_stop","index":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventToolCallEnd || events[0].ToolCall == nil || events[0].ToolCall.ID != "toolu_1" || events[0].ToolCall.Name != "read" {
		t.Fatalf("tool end mismatch: %#v", events)
	}
}

func TestBedrockAnthropicConverterErrorEventCarriesMessageState(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	events, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"error","error":{"message":"boom"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != EventError || events[0].ErrorReason != ErrorReasonProvider || events[0].Error != "" {
		t.Fatalf("error event mismatch: %#v", events)
	}
	if events[0].Message == nil || events[0].Message.StopReason != StopReasonError || events[0].Message.ErrorMessage != "boom" {
		t.Fatalf("error message mismatch: %#v", events[0].Message)
	}
}

func TestBedrockAnthropicConverterPingIsSilent(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	events, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"ping"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("ping should be silent, got %#v", events)
	}
}

func TestBedrockAnthropicConverterErrorsOnUnknownEventLikeUpstream(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"unknown_event"}`)})
	if err == nil || !strings.Contains(err.Error(), "anthropic event parse:") {
		t.Fatalf("expected upstream-style parse error for unknown event, got %v", err)
	}
}

func TestBedrockAnthropicConverterEmptyBytesParseErrorMatchesUpstream(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	_, err := converter.Ingest(BedrockEventStreamMessage{Payload: []byte(`{"bytes":""}`)})
	if err == nil || !strings.Contains(err.Error(), "anthropic event parse:") {
		t.Fatalf("expected upstream-style anthropic parse error, got %v", err)
	}
}

func TestBedrockAnthropicConverterErrorsOnMissingMessageStartBodyLikeUpstream(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"message_start"}`)})
	if err == nil || !strings.Contains(err.Error(), "anthropic event parse:") || !strings.Contains(err.Error(), "message") {
		t.Fatalf("expected missing message parse error, got %v", err)
	}
}

func TestBedrockAnthropicConverterErrorsOnMissingRequiredBodiesLikeUpstream(t *testing.T) {
	for _, test := range []struct {
		event string
		field string
	}{
		{`{"type":"content_block_start","index":0}`, "content_block"},
		{`{"type":"content_block_delta","index":0}`, "delta"},
		{`{"type":"message_delta"}`, "delta"},
		{`{"type":"error"}`, "error"},
	} {
		converter := NewBedrockAnthropicConverter()
		_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(test.event)})
		if err == nil || !strings.Contains(err.Error(), "anthropic event parse:") || !strings.Contains(err.Error(), test.field) {
			t.Fatalf("expected missing %s parse error for %s, got %v", test.field, test.event, err)
		}
	}
}

func TestBedrockAnthropicConverterErrorsOnInvalidNestedTypeLikeUpstream(t *testing.T) {
	for _, test := range []struct {
		event string
		field string
	}{
		{`{"type":"content_block_start","index":0,"content_block":{}}`, "content_block.type"},
		{`{"type":"content_block_start","index":0,"content_block":{"type":1}}`, "content_block.type"},
		{`{"type":"content_block_delta","index":0,"delta":{}}`, "delta.type"},
		{`{"type":"content_block_delta","index":0,"delta":{"type":1}}`, "delta.type"},
	} {
		converter := NewBedrockAnthropicConverter()
		_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(test.event)})
		if err == nil || !strings.Contains(err.Error(), "anthropic event parse:") || !strings.Contains(err.Error(), test.field) {
			t.Fatalf("expected invalid %s parse error for %s, got %v", test.field, test.event, err)
		}
	}
}

func TestBedrockAnthropicConverterErrorsOnNegativeIndexLikeUpstream(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_start","index":-1,"content_block":{"type":"text","text":"hi"}}`)})
	if err == nil || !strings.Contains(err.Error(), "anthropic event parse:") || !strings.Contains(err.Error(), "negative index") {
		t.Fatalf("expected negative index parse error, got %v", err)
	}
}

func TestBedrockAnthropicConverterErrorsOnInvalidIndexLikeUpstream(t *testing.T) {
	for _, event := range []string{
		`{"type":"content_block_start","content_block":{"type":"text","text":"hi"}}`,
		`{"type":"content_block_delta","index":"0","delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"content_block_stop","index":1.5}`,
	} {
		converter := NewBedrockAnthropicConverter()
		_, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(event)})
		if err == nil || !strings.Contains(err.Error(), "anthropic event parse:") || !strings.Contains(err.Error(), "invalid index") {
			t.Fatalf("expected invalid index parse error for %s, got %v", event, err)
		}
	}
}

func TestBedrockAnthropicIndexAcceptsJSONNumber(t *testing.T) {
	index, err := bedrockAnthropicIndex(map[string]any{"index": json.Number("2")})
	if err != nil {
		t.Fatal(err)
	}
	if index != 2 {
		t.Fatalf("index mismatch: %d", index)
	}
}

func TestBedrockNumberValueAcceptsJSONNumber(t *testing.T) {
	if value := numberValue(json.Number("3")); value != 3 {
		t.Fatalf("number mismatch: %v", value)
	}
}

func TestBedrockAnthropicConverterIgnoresUnknownContentSubtypesLikeUpstream(t *testing.T) {
	converter := NewBedrockAnthropicConverter()
	events, err := converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_start","index":0,"content_block":{"type":"unknown_block"}}`)})
	if err != nil || len(events) != 0 {
		t.Fatalf("unknown content block subtype should be ignored like upstream: events=%#v err=%v", events, err)
	}
	events, err = converter.Ingest(BedrockEventStreamMessage{Payload: base64BedrockAnthropicPayload(`{"type":"content_block_delta","index":0,"delta":{"type":"unknown_delta"}}`)})
	if err != nil || len(events) != 0 {
		t.Fatalf("unknown content delta subtype should be ignored like upstream: events=%#v err=%v", events, err)
	}
}
