package ai

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeAWSEventStreamFrameMatchesUpstreamMessageShape(t *testing.T) {
	frame := bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"text":"hi"}}`))
	message, rest, ok := DecodeAWSEventStreamFrame(frame)
	if !ok || len(rest) != 0 {
		t.Fatalf("decode mismatch ok=%v rest=%d", ok, len(rest))
	}
	if message.EventType != "contentBlockDelta" || message.ExceptionType != "" || string(message.Payload) != `{"delta":{"text":"hi"}}` {
		t.Fatalf("message mismatch: %#v", message)
	}
}

func TestDecodeAWSEventStreamFrameWaitsForPartialFrame(t *testing.T) {
	frame := bedrockEventFrame("messageStop", []byte(`{}`))
	_, rest, ok := DecodeAWSEventStreamFrame(frame[:8])
	if ok || len(rest) != 8 {
		t.Fatalf("partial decode mismatch ok=%v rest=%d", ok, len(rest))
	}
}

func TestDecodeAWSEventStreamFrameRejectsZeroCRCLikeUpstream(t *testing.T) {
	frame := awsEventStreamFrameWithZeroCRC("contentBlockDelta", []byte(`{"delta":{"text":"hi"}}`))
	message, rest, ok := DecodeAWSEventStreamFrame(frame)
	if ok || len(rest) != len(frame) || message.EventType != "" {
		t.Fatalf("zero CRC should fail and leave input untouched ok=%v rest=%d message=%#v", ok, len(rest), message)
	}
}

func TestDecodeAWSEventStreamFrameRejectsBadMessageCRCLikeUpstream(t *testing.T) {
	frame := bedrockEventFrame("messageStop", []byte(`{}`))
	frame[len(frame)-1] ^= 0xff
	message, rest, ok := DecodeAWSEventStreamFrame(frame)
	if ok || len(rest) != len(frame) || message.EventType != "" {
		t.Fatalf("bad CRC should fail and leave input untouched ok=%v rest=%d message=%#v", ok, len(rest), message)
	}
}

func TestDecodeAWSEventStreamFrameSkipsNonStringHeaders(t *testing.T) {
	payload := []byte(`{}`)
	headers := []byte{byte(len("ignored-bool"))}
	headers = append(headers, []byte("ignored-bool")...)
	headers = append(headers, 0)
	headers = append(headers, byte(len(":event-type")))
	headers = append(headers, []byte(":event-type")...)
	headers = append(headers, 7, 0, byte(len("messageStop")))
	headers = append(headers, []byte("messageStop")...)
	frame := bedrockEventFrameWithHeadersAndCRC(headers, payload)

	message, _, ok := DecodeAWSEventStreamFrame(frame)
	if !ok || message.EventType != "messageStop" {
		t.Fatalf("message mismatch ok=%v message=%#v", ok, message)
	}
}

func TestParseAWSEventStreamMessageMatchesUpstreamTypedHeaders(t *testing.T) {
	headers := append([]byte{}, awsEventStreamStringHeader(":event-type", "chunk")...)
	headers = append(headers, awsEventStreamBoolHeader("flag", true)...)
	headers = append(headers, awsEventStreamIntHeader("count", 7)...)
	frame := bedrockEventFrameWithHeadersAndCRC(headers, []byte(`{"ok":true}`))

	message, consumed, err := ParseAWSEventStreamMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != len(frame) || message.EventType() != "chunk" || string(message.Payload) != `{"ok":true}` {
		t.Fatalf("message mismatch consumed=%d message=%#v", consumed, message)
	}
	if value, ok := message.Headers["flag"].(AWSEventStreamBoolHeader); !ok || !bool(value) {
		t.Fatalf("bool header mismatch: %#v", message.Headers["flag"])
	}
	if value, ok := message.Headers["count"].(AWSEventStreamIntHeader); !ok || int64(value) != 7 {
		t.Fatalf("int header mismatch: %#v", message.Headers["count"])
	}
}

func TestEventMessageHeaderAccessorsMatchUpstream(t *testing.T) {
	headers := append([]byte{}, awsEventStreamStringHeader(":message-type", "event")...)
	headers = append(headers, awsEventStreamStringHeader(":event-type", "chunk")...)
	headers = append(headers, awsEventStreamStringHeader(":content-type", "application/json")...)
	frame := bedrockEventFrameWithHeadersAndCRC(headers, []byte(`{"ok":true}`))

	message, consumed, err := ParseEventMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != len(frame) || message.MessageType() != "event" || message.EventType() != "chunk" || message.ContentType() != "application/json" {
		t.Fatalf("header accessor mismatch consumed=%d message=%#v", consumed, message)
	}
}

func TestParseEventMessageMatchesUpstreamPublicName(t *testing.T) {
	frame := bedrockEventFrameWithHeader(":event-type", "chunk", []byte(`{"ok":true}`))

	message, consumed, err := ParseEventMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != len(frame) || message.EventType() != "chunk" || string(message.Payload) != `{"ok":true}` {
		t.Fatalf("message mismatch consumed=%d message=%#v", consumed, message)
	}

	var _ EventMessage = message
	var _ HeaderValue = message.Headers[":event-type"]
}

func TestHeaderValueUpstreamVariantConstructors(t *testing.T) {
	if value, ok := HeaderValueString("hello").(AWSEventStreamStringHeader); !ok || string(value) != "hello" {
		t.Fatalf("string header mismatch: %#v", value)
	}
	if value, ok := HeaderValueBytes([]byte("abc")).(AWSEventStreamBytesHeader); !ok || string(value) != "abc" {
		t.Fatalf("bytes header mismatch: %#v", value)
	}
	if value, ok := HeaderValueBool(true).(AWSEventStreamBoolHeader); !ok || bool(value) != true {
		t.Fatalf("bool header mismatch: %#v", value)
	}
	if value, ok := HeaderValueInt(42).(AWSEventStreamIntHeader); !ok || int64(value) != 42 {
		t.Fatalf("int header mismatch: %#v", value)
	}
	other := HeaderValueOther(99, []byte("raw"))
	if other.ValueType != 99 || string(other.Raw) != "raw" {
		t.Fatalf("other header mismatch: %#v", other)
	}
}

func TestParseMessageAndCRC32MatchUpstreamPublicNames(t *testing.T) {
	frame := bedrockEventFrameWithHeader(":event-type", "chunk", []byte(`{"ok":true}`))
	message, consumed, err := ParseMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != len(frame) || message.EventType() != "chunk" || string(message.Payload) != `{"ok":true}` {
		t.Fatalf("message mismatch consumed=%d message=%#v", consumed, message)
	}
	if got := CRC32([]byte("123456789")); got != 0xcbf43926 {
		t.Fatalf("crc mismatch: %#x", got)
	}
	if got := Crc32([]byte("123456789")); got != 0xcbf43926 {
		t.Fatalf("Crc32 alias mismatch: %#x", got)
	}
}

func TestEventStreamMessageMatchesUpstreamPublicShape(t *testing.T) {
	message := EventStreamMessage{Payload: []byte("payload")}
	message.EventType = "contentBlockDelta"
	message.ExceptionType = ""

	if message.EventType != "contentBlockDelta" || message.ExceptionType != "" || string(message.Payload) != "payload" {
		t.Fatalf("message mismatch: %#v", message)
	}
}

func TestAwsEventStreamDecodesBufferedFramesLikeUpstream(t *testing.T) {
	first := bedrockEventFrame("contentBlockDelta", []byte(`{"delta":{"text":"hi"}}`))
	second := bedrockEventFrame("messageStop", []byte(`{}`))
	stream := NewAwsEventStream()

	if message, ok, err := stream.Push(first[:7]); err != nil || ok || message.EventType != "" {
		t.Fatalf("partial first push = message=%#v ok=%v err=%v", message, ok, err)
	}
	message, ok, err := stream.Push(append(first[7:], second...))
	if err != nil || !ok || message.EventType != "contentBlockDelta" || string(message.Payload) != `{"delta":{"text":"hi"}}` {
		t.Fatalf("first message = %#v ok=%v err=%v", message, ok, err)
	}
	message, ok, err = stream.Next()
	if err != nil || !ok || message.EventType != "messageStop" || string(message.Payload) != `{}` {
		t.Fatalf("second message = %#v ok=%v err=%v", message, ok, err)
	}
	if message, ok, err = stream.Next(); err != nil || ok || message.EventType != "" {
		t.Fatalf("empty next = %#v ok=%v err=%v", message, ok, err)
	}
}

func TestParseAWSEventStreamMessageCopiesPayloadLikeUpstream(t *testing.T) {
	frame := bedrockEventFrameWithHeader(":event-type", "chunk", []byte("payload"))

	message, _, err := ParseAWSEventStreamMessage(frame)
	if err != nil {
		t.Fatal(err)
	}

	frame[len(frame)-5] = 'X'
	if string(message.Payload) != "payload" {
		t.Fatalf("payload should not alias input frame, got %q", message.Payload)
	}
}

func TestParseAWSEventStreamMessageRejectsBadPreludeCRCLikeUpstream(t *testing.T) {
	frame := bedrockEventFrameWithHeader(":event-type", "chunk", []byte("x"))
	frame[8] ^= 0xff
	_, _, err := ParseAWSEventStreamMessage(frame)
	if err == nil || !strings.Contains(err.Error(), "prelude CRC mismatch") {
		t.Fatalf("expected prelude CRC mismatch, got %v", err)
	}
	var streamErr EventStreamError
	if !errors.As(err, &streamErr) || streamErr.Kind != EventStreamErrorPreludeCrc {
		t.Fatalf("expected EventStreamErrorPreludeCrc, got %#v", err)
	}
}

func TestParseAWSEventStreamMessageRejectsNonUTF8HeaderNameLikeUpstream(t *testing.T) {
	headers := []byte{1, 0xff, 7, 0, 2, 'o', 'k'}
	frame := bedrockEventFrameWithHeadersAndCRC(headers, []byte(`{}`))
	_, _, err := ParseAWSEventStreamMessage(frame)
	if err == nil || !strings.Contains(err.Error(), "name not utf-8") {
		t.Fatalf("expected non-utf8 name error, got %v", err)
	}
}

func TestParseAWSEventStreamMessageRejectsNonUTF8StringHeaderLikeUpstream(t *testing.T) {
	headers := []byte{byte(len(":event-type"))}
	headers = append(headers, []byte(":event-type")...)
	headers = append(headers, 7, 0, 1, 0xff)
	frame := bedrockEventFrameWithHeadersAndCRC(headers, []byte(`{}`))
	_, _, err := ParseAWSEventStreamMessage(frame)
	if err == nil || !strings.Contains(err.Error(), "string not utf-8") {
		t.Fatalf("expected non-utf8 string error, got %v", err)
	}
}

func TestParseAWSEventStreamMessageRejectsUnsupportedHeaderValueWithTypedError(t *testing.T) {
	headers := []byte{byte(len("bad"))}
	headers = append(headers, []byte("bad")...)
	headers = append(headers, 99)
	frame := bedrockEventFrameWithHeadersAndCRC(headers, []byte(`{}`))

	_, _, err := ParseAWSEventStreamMessage(frame)
	if err == nil || !strings.Contains(err.Error(), "unsupported header value type 99") {
		t.Fatalf("expected unsupported header value error, got %v", err)
	}
	var streamErr EventStreamError
	if !errors.As(err, &streamErr) || streamErr.Kind != EventStreamErrorHeaderValue || streamErr.Value != 99 {
		t.Fatalf("expected EventStreamErrorHeaderValue, got %#v", err)
	}
}

func TestAWSEventStreamCRC32ReferenceVector(t *testing.T) {
	if got := AWSEventStreamCRC32([]byte("123456789")); got != 0xcbf43926 {
		t.Fatalf("crc mismatch: %#x", got)
	}
}

func awsEventStreamFrameWithZeroCRC(eventType string, payload []byte) []byte {
	name := ":event-type"
	headers := []byte{byte(len(name))}
	headers = append(headers, []byte(name)...)
	headers = append(headers, 7, byte(len(eventType)>>8), byte(len(eventType)))
	headers = append(headers, []byte(eventType)...)

	totalLen := 12 + len(headers) + len(payload) + 4
	frame := []byte{byte(totalLen >> 24), byte(totalLen >> 16), byte(totalLen >> 8), byte(totalLen)}
	frame = append(frame, byte(len(headers)>>24), byte(len(headers)>>16), byte(len(headers)>>8), byte(len(headers)))
	frame = append(frame, 0, 0, 0, 0)
	frame = append(frame, headers...)
	frame = append(frame, payload...)
	frame = append(frame, 0, 0, 0, 0)
	return frame
}
