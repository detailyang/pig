package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type AbortError struct{}

func (AbortError) Error() string { return "aborted" }

type AbortErrorOrReqwestKind string

const (
	AbortErrorOrReqwestAborted AbortErrorOrReqwestKind = "aborted"
	AbortErrorOrReqwestRequest AbortErrorOrReqwestKind = "request"
)

type AbortErrorOrReqwest struct {
	Kind AbortErrorOrReqwestKind
	Err  error
}

func (err AbortErrorOrReqwest) Error() string {
	if err.Kind == AbortErrorOrReqwestAborted {
		return "aborted"
	}
	if err.Err != nil {
		return err.Err.Error()
	}
	return "abort request error"
}

func (err AbortErrorOrReqwest) Unwrap() error { return err.Err }

func AbortErrorOrReqwestReqwest(err error) AbortErrorOrReqwest {
	return AbortErrorOrReqwest{Kind: AbortErrorOrReqwestRequest, Err: err}
}

type AbortableNextKind string

const (
	AbortableNextItem    AbortableNextKind = "item"
	AbortableNextEOF     AbortableNextKind = "eof"
	AbortableNextAborted AbortableNextKind = "aborted"
)

type AbortableNext[T any] struct {
	Kind  AbortableNextKind
	Value T
	Err   error
}

func NextOrAbort[T any](ctx context.Context, next func() (T, bool, error)) AbortableNext[T] {
	select {
	case <-ctx.Done():
		return AbortableNext[T]{Kind: AbortableNextAborted, Err: AbortError{}}
	default:
	}
	value, ok, err := next()
	if err != nil {
		return AbortableNext[T]{Kind: AbortableNextItem, Value: value, Err: err}
	}
	if !ok {
		var zero T
		return AbortableNext[T]{Kind: AbortableNextEOF, Value: zero}
	}
	return AbortableNext[T]{Kind: AbortableNextItem, Value: value}
}

func SendOrAbort(ctx context.Context, send func(context.Context) (*http.Response, error)) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, AbortErrorOrReqwest{Kind: AbortErrorOrReqwestAborted, Err: AbortError{}}
	default:
	}
	response, err := send(ctx)
	if err != nil {
		if IsCanceledError(err) {
			return nil, AbortErrorOrReqwest{Kind: AbortErrorOrReqwestAborted, Err: AbortError{}}
		}
		return nil, AbortErrorOrReqwest{Kind: AbortErrorOrReqwestRequest, Err: err}
	}
	return response, nil
}

func SleepOrAbort(ctx context.Context, duration time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return AbortError{}
	case <-timer.C:
		return nil
	}
}

func DrainBytesOrAbort(ctx context.Context, response *http.Response) error {
	_, err := ResponseTextOrAbort(ctx, response)
	return err
}

func ResponseTextOrAbort(ctx context.Context, response *http.Response) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", AbortError{}
	default:
	}
	if response == nil || response.Body == nil {
		return "", nil
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", AbortError{}
	default:
	}
	return string(data), nil
}

func PushAborted(stream *AssistantMessageEventStream, model Model) {
	stream.Emit(AssistantMessageEvent{
		Type:        EventError,
		ErrorReason: ErrorReasonAborted,
		Message: &AssistantMessage{
			Role:         AssistantRoleAssistant,
			Content:      []ContentBlock{},
			API:          model.API,
			Provider:     model.Provider,
			Model:        model.ID,
			Usage:        &Usage{},
			StopReason:   StopReasonAborted,
			ErrorMessage: "aborted",
			Timestamp:    time.Now().UnixMilli(),
		},
	})
}

func AbortedStreamIfCanceled(model Model, err error) (*AssistantMessageEventStream, bool) {
	if !IsCanceledError(err) {
		return nil, false
	}
	stream := NewAssistantMessageEventStream()
	PushAborted(stream, model)
	return stream, true
}

func IsCanceledError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || strings.Contains(err.Error(), context.Canceled.Error())
}

func EmitErrorOrAborted(stream *AssistantMessageEventStream, model Model, err error, emit func(string)) {
	if IsCanceledError(err) {
		PushAborted(stream, model)
		return
	}
	emit(err.Error())
}
