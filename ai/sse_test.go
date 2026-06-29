package ai

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSSEMatchesUpstreamEventAndDataFields(t *testing.T) {
	events, err := ParseSSE(strings.NewReader("event: response.delta\ndata: {\"delta\":\"hi\"}\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []SSEEvent{{Event: "response.delta", Data: `{"delta":"hi"}`}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v want %#v", events, want)
	}
	stream := NewSseStream(strings.NewReader("event: response.delta\ndata: {\"delta\":\"hi\"}\n\n"))
	event, ok, err := stream.Next()
	if err != nil || !ok || event.Event != "response.delta" || event.Data != `{"delta":"hi"}` {
		t.Fatalf("stream event = %#v ok=%v err=%v", event, ok, err)
	}
	if event, ok, err = stream.Next(); err != nil || ok || event.Event != "" || event.Data != "" {
		t.Fatalf("stream should be exhausted, event=%#v ok=%v err=%v", event, ok, err)
	}
}

func TestParseSSEDropsCommentsAndJoinsDataLines(t *testing.T) {
	events, err := ParseSSE(strings.NewReader(": keepalive\ndata: hello\ndata: world\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []SSEEvent{{Data: "hello\nworld"}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v want %#v", events, want)
	}
}

func TestParseSSEFlushesFinalEventAtEOF(t *testing.T) {
	events, err := ParseSSE(strings.NewReader("event: done\ndata: ok"))
	if err != nil {
		t.Fatal(err)
	}
	want := []SSEEvent{{Event: "done", Data: "ok"}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v want %#v", events, want)
	}
}

func TestParseSSEFieldWithoutColonUsesEmptyValueLikeUpstream(t *testing.T) {
	events, err := ParseSSE(strings.NewReader("event\ndata: ok\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []SSEEvent{{Data: "ok"}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v want %#v", events, want)
	}
}

func TestParseSSETrimsCRLFLinesLikeUpstream(t *testing.T) {
	events, err := ParseSSE(strings.NewReader("event: done\r\ndata: ok\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []SSEEvent{{Event: "done", Data: "ok"}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v want %#v", events, want)
	}
}

func TestParseSSEDoesNotEmitEmptyDataOnlyEventLikeUpstream(t *testing.T) {
	events, err := ParseSSE(strings.NewReader("data:\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("empty data-only event should not flush like upstream, got %#v", events)
	}
}

func TestParseSSEEmitsEventFieldWithoutColonLikeUpstream(t *testing.T) {
	events, err := ParseSSE(strings.NewReader("event\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []SSEEvent{{Event: "", Data: ""}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v want %#v", events, want)
	}
}
