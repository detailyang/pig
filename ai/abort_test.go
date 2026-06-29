package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAbortErrorMatchesUpstreamMessage(t *testing.T) {
	if got, want := (AbortError{}).Error(), "aborted"; got != want {
		t.Fatalf("AbortError = %q want %q", got, want)
	}
}

func TestPushAbortedEmitsAbortedErrorMessage(t *testing.T) {
	stream := NewAssistantMessageEventStream()
	model := Model{ID: "model-1", Provider: Provider("anthropic"), API: ApiAnthropic}

	PushAborted(stream, model)

	events := stream.Events()
	if len(events) != 1 || events[0].Type != EventError || events[0].ErrorReason != ErrorReasonAborted || events[0].Message == nil {
		t.Fatalf("events = %#v", events)
	}
	message := events[0].Message
	if message.Role != AssistantRoleAssistant || message.Model != "model-1" || message.Provider != Provider("anthropic") || message.API != ApiAnthropic || message.StopReason != StopReasonAborted || message.ErrorMessage != "aborted" || message.Timestamp == 0 {
		t.Fatalf("message = %#v", message)
	}

	result, ok := stream.Result()
	if !ok || result.StopReason != StopReasonAborted || result.ErrorMessage != "aborted" || result.Timestamp == 0 {
		t.Fatalf("result = %#v ok=%v", result, ok)
	}
}

func TestNextOrAbortReturnsAbortedWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := NextOrAbort(ctx, func() (string, bool, error) {
		t.Fatal("next should not be called after cancellation")
		return "", false, nil
	})

	if result.Kind != AbortableNextAborted {
		t.Fatalf("result = %#v", result)
	}
}

func TestNextOrAbortReturnsItemAndEOF(t *testing.T) {
	item := NextOrAbort(context.Background(), func() (string, bool, error) {
		return "value", true, nil
	})
	if item.Kind != AbortableNextItem || item.Value != "value" {
		t.Fatalf("item = %#v", item)
	}

	eof := NextOrAbort(context.Background(), func() (string, bool, error) {
		return "", false, nil
	})
	if eof.Kind != AbortableNextEOF {
		t.Fatalf("eof = %#v", eof)
	}
}

func TestSleepOrAbortReturnsAbortErrorWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := SleepOrAbort(ctx, time.Second); err == nil || err.Error() != "aborted" {
		t.Fatalf("expected AbortError, got %v", err)
	}
}

func TestResponseTextOrAbortReadsBody(t *testing.T) {
	response := &http.Response{Body: io.NopCloser(strings.NewReader("hello"))}
	text, err := ResponseTextOrAbort(context.Background(), response)
	if err != nil || text != "hello" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestDrainBytesOrAbortConsumesBody(t *testing.T) {
	response := &http.Response{Body: io.NopCloser(strings.NewReader("hello"))}
	if err := DrainBytesOrAbort(context.Background(), response); err != nil {
		t.Fatal(err)
	}
}

func TestSendOrAbortReturnsAbortedTypedError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := SendOrAbort(ctx, func(context.Context) (*http.Response, error) {
		t.Fatal("send should not be called after cancellation")
		return nil, nil
	})
	var abortErr AbortErrorOrReqwest
	if !errors.As(err, &abortErr) || abortErr.Kind != AbortErrorOrReqwestAborted {
		t.Fatalf("expected AbortErrorOrReqwestAborted, got %#v", err)
	}
}

func TestSendOrAbortCallsSender(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusOK}
	got, err := SendOrAbort(context.Background(), func(context.Context) (*http.Response, error) {
		return response, nil
	})
	if err != nil || got != response {
		t.Fatalf("response=%#v err=%v", got, err)
	}
}
