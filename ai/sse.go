package ai

import (
	"bufio"
	"io"
	"strings"
)

type SSEEvent struct {
	Event string
	Data  string
}

type SseEvent = SSEEvent

type SseStream struct {
	events []SseEvent
	index  int
	err    error
}

func NewSseStream(reader io.Reader) *SseStream {
	events, err := ParseSSE(reader)
	return &SseStream{events: events, err: err}
}

func (stream *SseStream) Next() (SseEvent, bool, error) {
	if stream.err != nil {
		err := stream.err
		stream.err = nil
		return SseEvent{}, false, err
	}
	if stream.index >= len(stream.events) {
		return SseEvent{}, false, nil
	}
	event := stream.events[stream.index]
	stream.index++
	return event, true, nil
}

func ParseSSE(reader io.Reader) ([]SSEEvent, error) {
	var events []SSEEvent
	err := ConsumeSSE(reader, func(event SSEEvent) bool {
		events = append(events, event)
		return true
	})
	return events, err
}

func ConsumeSSE(reader io.Reader, handle func(SSEEvent) bool) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	current := SSEEvent{}
	hasEvent := false
	eventPresent := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if hasEvent || eventPresent {
				if !handle(current) {
					return nil
				}
				current = SSEEvent{}
				hasEvent = false
				eventPresent = false
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			current.Event = value
			eventPresent = true
			hasEvent = true
		case "data":
			if current.Data != "" {
				current.Data += "\n"
			}
			current.Data += value
			if value != "" {
				hasEvent = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if hasEvent || eventPresent {
		handle(current)
	}
	return nil
}
