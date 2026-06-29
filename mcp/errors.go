package mcp

import (
	"errors"
	"fmt"
)

var ErrNotInitialized = errors.New("client is not initialized; call `initialize` before issuing requests")
var ErrCancelled = errors.New("request cancelled before the server responded")

var McpErrorNotInitialized error = ErrNotInitialized
var McpErrorCancelled error = ErrCancelled

type McpError = error

type TransportError struct{ Message string }

func (err TransportError) Error() string { return "transport error: " + err.Message }

type ProtocolError struct{ Message string }

func (err ProtocolError) Error() string { return "protocol error: " + err.Message }

func McpErrorProtocol(message string) error { return ProtocolError{Message: message} }

type ServerError struct {
	Code    int64
	Message string
}

func (err ServerError) Error() string {
	return fmt.Sprintf("server returned error %d: %s", err.Code, err.Message)
}

type TimeoutError struct{ Seconds int64 }

func (err TimeoutError) Error() string {
	return fmt.Sprintf("request timed out after %ds", err.Seconds)
}

func McpErrorTimeout(seconds int64) error { return TimeoutError{Seconds: seconds} }

func McpErrorOther(message string) error { return errors.New(message) }

func IsNotInitialized(err error) bool { return errors.Is(err, ErrNotInitialized) }
func IsServerError(err error) bool    { var target ServerError; return errors.As(err, &target) }
