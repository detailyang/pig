package ai

import (
	"testing"
	"time"
)

func TestAssistantMessageEventStreamResultWaitsForDone(t *testing.T) {
	stream, sender := NewAssistantMessageEventStreamWithSender()
	result := make(chan bool, 1)
	go func() {
		_, ok := stream.Result()
		result <- ok
	}()
	select {
	case ok := <-result:
		t.Fatalf("result returned before terminal event: ok=%v", ok)
	case <-time.After(20 * time.Millisecond):
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		sender.Push(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonStop})
	}()
	select {
	case ok := <-result:
		if !ok {
			t.Fatalf("result should complete after terminal event")
		}
	case <-time.After(time.Second):
		t.Fatal("result did not return after terminal event")
	}
	<-done
}

func TestAssistantMessageEventSenderPushResolvesResultAndQueuesEvents(t *testing.T) {
	stream, sender := CreateAssistantMessageEventStreamWithSender()
	sender.Push(AssistantMessageEvent{Type: EventTextDelta, Delta: "hel"})
	sender.Push(AssistantMessageEvent{Type: EventTextDelta, Delta: "lo"})
	sender.Push(AssistantMessageEvent{Type: EventDone, DoneReason: DoneReasonStop})

	message, ok := stream.Result()
	if !ok {
		t.Fatal("expected completed result")
	}
	if message.Text() != "hello" || message.StopReason != StopReasonEndTurn {
		t.Fatalf("result mismatch: %#v", message)
	}
	events := stream.Events()
	if len(events) != 3 || events[0].Delta != "hel" || events[2].Type != EventDone {
		t.Fatalf("events should be queued in order: %#v", events)
	}
}

func TestAssistantMessageEventSenderClosedAndClose(t *testing.T) {
	stream, sender := NewAssistantMessageEventStreamWithSender()
	if sender.IsClosed() {
		t.Fatal("fresh sender should be open")
	}
	sender.Close(DoneReasonAbort)
	if !sender.IsClosed() {
		t.Fatal("sender should report closed after close")
	}
	message, ok := stream.Result()
	if !ok || message.StopReason != StopReasonAborted {
		t.Fatalf("close should emit abort result: %#v ok=%v", message, ok)
	}
	sender.Push(AssistantMessageEvent{Type: EventTextDelta, Delta: "ignored"})
	if got := len(stream.Events()); got != 1 {
		t.Fatalf("push after close should be ignored, events=%d", got)
	}
}
