package ai

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

type AWSEventStreamMessage = BedrockEventStreamMessage

type EventStreamMessage = BedrockEventStreamMessage

type EventStreamErrorKind string

const (
	EventStreamErrorShort       EventStreamErrorKind = "short"
	EventStreamErrorPreludeCrc  EventStreamErrorKind = "prelude_crc"
	EventStreamErrorMessageCrc  EventStreamErrorKind = "message_crc"
	EventStreamErrorHeaderLen   EventStreamErrorKind = "header_len"
	EventStreamErrorHeaderValue EventStreamErrorKind = "header_value"
	EventStreamErrorHeader      EventStreamErrorKind = "header"
)

type EventStreamError struct {
	Kind     EventStreamErrorKind
	Need     int
	Have     int
	Expected uint32
	Got      uint32
	Total    int
	Headers  int
	Value    byte
	Header   string
}

func (err EventStreamError) Error() string {
	switch err.Kind {
	case EventStreamErrorShort:
		return fmt.Sprintf("frame too short: need %d bytes, have %d", err.Need, err.Have)
	case EventStreamErrorPreludeCrc:
		return fmt.Sprintf("prelude CRC mismatch (expected %#x, got %#x)", err.Expected, err.Got)
	case EventStreamErrorMessageCrc:
		return fmt.Sprintf("message CRC mismatch (expected %#x, got %#x)", err.Expected, err.Got)
	case EventStreamErrorHeaderLen:
		return fmt.Sprintf("invalid header length (total %d, headers %d)", err.Total, err.Headers)
	case EventStreamErrorHeaderValue:
		return fmt.Sprintf("unsupported header value type %d", err.Value)
	case EventStreamErrorHeader:
		return fmt.Sprintf("malformed header: %s", err.Header)
	default:
		return "event stream error"
	}
}

type AwsEventStream struct {
	buf []byte
}

func NewAwsEventStream() *AwsEventStream {
	return &AwsEventStream{}
}

func (stream *AwsEventStream) Push(data []byte) (EventStreamMessage, bool, error) {
	stream.buf = append(stream.buf, data...)
	return stream.Next()
}

func (stream *AwsEventStream) Next() (EventStreamMessage, bool, error) {
	message, consumed, err := ParseAWSEventStreamMessage(stream.buf)
	if err != nil {
		if isIncompleteAWSEventStreamFrame(stream.buf) {
			return EventStreamMessage{}, false, nil
		}
		return EventStreamMessage{}, false, err
	}
	stream.buf = stream.buf[consumed:]
	return EventStreamMessage{EventType: message.EventType(), ExceptionType: message.headerString(":exception-type"), Payload: message.Payload}, true, nil
}

func (stream *AwsEventStream) Buffered() int { return len(stream.buf) }

type AWSEventMessage struct {
	Headers map[string]AWSEventStreamHeaderValue
	Payload []byte
}

type EventMessage = AWSEventMessage

func (message AWSEventMessage) MessageType() string { return message.headerString(":message-type") }
func (message AWSEventMessage) EventType() string   { return message.headerString(":event-type") }
func (message AWSEventMessage) ContentType() string { return message.headerString(":content-type") }

func (message AWSEventMessage) headerString(name string) string {
	if value, ok := message.Headers[name].(AWSEventStreamStringHeader); ok {
		return string(value)
	}
	return ""
}

type AWSEventStreamHeaderValue any

type HeaderValue = AWSEventStreamHeaderValue

type AWSEventStreamStringHeader string
type AWSEventStreamBytesHeader []byte
type AWSEventStreamBoolHeader bool
type AWSEventStreamIntHeader int64

type HeaderValueOtherValue struct {
	ValueType uint8
	Raw       []byte
}

func HeaderValueString(value string) HeaderValue {
	return AWSEventStreamStringHeader(value)
}

func HeaderValueBytes(value []byte) HeaderValue {
	return AWSEventStreamBytesHeader(append([]byte(nil), value...))
}

func HeaderValueBool(value bool) HeaderValue {
	return AWSEventStreamBoolHeader(value)
}

func HeaderValueInt(value int64) HeaderValue {
	return AWSEventStreamIntHeader(value)
}

func HeaderValueOther(valueType uint8, raw []byte) HeaderValueOtherValue {
	return HeaderValueOtherValue{ValueType: valueType, Raw: append([]byte(nil), raw...)}
}

func DecodeAWSEventStreamFrame(data []byte) (AWSEventStreamMessage, []byte, bool) {
	message, consumed, err := ParseAWSEventStreamMessage(data)
	if err != nil {
		return AWSEventStreamMessage{}, data, false
	}
	return AWSEventStreamMessage{EventType: message.EventType(), ExceptionType: message.headerString(":exception-type"), Payload: message.Payload}, data[consumed:], true
}

func ParseEventMessage(data []byte) (EventMessage, int, error) {
	return ParseAWSEventStreamMessage(data)
}

func ParseMessage(data []byte) (EventMessage, int, error) {
	return ParseEventMessage(data)
}

func ParseAWSEventStreamMessage(data []byte) (AWSEventMessage, int, error) {
	if len(data) < 12 {
		return AWSEventMessage{}, 0, EventStreamError{Kind: EventStreamErrorShort, Need: 12, Have: len(data)}
	}
	totalLen := int(binary.BigEndian.Uint32(data[0:4]))
	headersLen := int(binary.BigEndian.Uint32(data[4:8]))
	preludeCRC := binary.BigEndian.Uint32(data[8:12])
	if got := AWSEventStreamCRC32(data[:8]); got != preludeCRC {
		return AWSEventMessage{}, 0, EventStreamError{Kind: EventStreamErrorPreludeCrc, Expected: preludeCRC, Got: got}
	}
	if len(data) < totalLen {
		return AWSEventMessage{}, 0, EventStreamError{Kind: EventStreamErrorShort, Need: totalLen, Have: len(data)}
	}
	if totalLen < 16 || headersLen < 0 || 12+headersLen+4 > totalLen {
		return AWSEventMessage{}, 0, EventStreamError{Kind: EventStreamErrorHeaderLen, Total: totalLen, Headers: headersLen}
	}
	messageCRC := binary.BigEndian.Uint32(data[totalLen-4 : totalLen])
	if got := AWSEventStreamCRC32(data[:totalLen-4]); got != messageCRC {
		return AWSEventMessage{}, 0, EventStreamError{Kind: EventStreamErrorMessageCrc, Expected: messageCRC, Got: got}
	}
	headers, err := parseAWSEventStreamHeaders(data[12 : 12+headersLen])
	if err != nil {
		return AWSEventMessage{}, 0, err
	}
	return AWSEventMessage{Headers: headers, Payload: append([]byte(nil), data[12+headersLen:totalLen-4]...)}, totalLen, nil
}

func isIncompleteAWSEventStreamFrame(data []byte) bool {
	if len(data) < 12 {
		return true
	}
	totalLen := int(binary.BigEndian.Uint32(data[0:4]))
	return totalLen >= 16 && len(data) < totalLen
}

func parseAWSEventStreamHeaders(data []byte) (map[string]AWSEventStreamHeaderValue, error) {
	headers := map[string]AWSEventStreamHeaderValue{}
	for len(data) > 0 {
		nameLen := int(data[0])
		data = data[1:]
		if len(data) < nameLen+1 {
			return nil, EventStreamError{Kind: EventStreamErrorHeader, Header: "name truncated"}
		}
		if !utf8.Valid(data[:nameLen]) {
			return nil, EventStreamError{Kind: EventStreamErrorHeader, Header: "name not utf-8"}
		}
		name := string(data[:nameLen])
		valueType := data[nameLen]
		data = data[nameLen+1:]
		var value AWSEventStreamHeaderValue
		var err error
		value, data, err = parseAWSEventStreamHeaderValue(valueType, data)
		if err != nil {
			return nil, err
		}
		headers[name] = value
	}
	return headers, nil
}

func parseAWSEventStreamHeaderValue(valueType byte, data []byte) (AWSEventStreamHeaderValue, []byte, error) {
	switch valueType {
	case 7:
		if len(data) < 2 {
			return nil, nil, EventStreamError{Kind: EventStreamErrorHeader, Header: "string length missing"}
		}
		valueLen := int(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
		if len(data) < valueLen {
			return nil, nil, EventStreamError{Kind: EventStreamErrorHeader, Header: "string body truncated"}
		}
		if !utf8.Valid(data[:valueLen]) {
			return nil, nil, EventStreamError{Kind: EventStreamErrorHeader, Header: "string not utf-8"}
		}
		return AWSEventStreamStringHeader(string(data[:valueLen])), data[valueLen:], nil
	case 6:
		if len(data) < 2 {
			return nil, nil, EventStreamError{Kind: EventStreamErrorHeader, Header: "bytes length missing"}
		}
		valueLen := int(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
		if len(data) < valueLen {
			return nil, nil, EventStreamError{Kind: EventStreamErrorHeader, Header: "bytes body truncated"}
		}
		return AWSEventStreamBytesHeader(append([]byte(nil), data[:valueLen]...)), data[valueLen:], nil
	case 0:
		return AWSEventStreamBoolHeader(true), data, nil
	case 1:
		return AWSEventStreamBoolHeader(false), data, nil
	case 4:
		if len(data) < 4 {
			return nil, nil, EventStreamError{Kind: EventStreamErrorHeader, Header: "int32 truncated"}
		}
		return AWSEventStreamIntHeader(int64(int32(binary.BigEndian.Uint32(data[:4])))), data[4:], nil
	case 5:
		if len(data) < 8 {
			return nil, nil, EventStreamError{Kind: EventStreamErrorHeader, Header: "int64 truncated"}
		}
		return AWSEventStreamIntHeader(int64(binary.BigEndian.Uint64(data[:8]))), data[8:], nil
	default:
		return nil, nil, EventStreamError{Kind: EventStreamErrorHeaderValue, Value: valueType}
	}
}

func AWSEventStreamCRC32(data []byte) uint32 {
	var crc uint32 = 0xffffffff
	for _, value := range data {
		crc = awsEventStreamCRCTable[(crc^uint32(value))&0xff] ^ (crc >> 8)
	}
	return crc ^ 0xffffffff
}

func CRC32(data []byte) uint32 {
	return AWSEventStreamCRC32(data)
}

func Crc32(data []byte) uint32 {
	return CRC32(data)
}

var awsEventStreamCRCTable = func() [256]uint32 {
	var table [256]uint32
	for index := range table {
		crc := uint32(index)
		for range 8 {
			if crc&1 != 0 {
				crc = 0xedb88320 ^ (crc >> 1)
			} else {
				crc >>= 1
			}
		}
		table[index] = crc
	}
	return table
}()
