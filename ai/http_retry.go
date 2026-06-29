package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxRetries      = 2
	defaultBaseDelayMS     = 500
	defaultMaxRetryDelayMS = 60_000
)

type RetrySendErrorKind string

const (
	RetrySendErrorStatus       RetrySendErrorKind = "status"
	RetrySendErrorRequest      RetrySendErrorKind = "request"
	RetrySendErrorAborted      RetrySendErrorKind = "aborted"
	RetrySendErrorDelayTooLong RetrySendErrorKind = "delay_too_long"
)

type RetrySendError struct {
	Kind        RetrySendErrorKind
	Message     string
	Err         error
	RequestedMS int
	CapMS       int
}

func (err RetrySendError) Error() string {
	if err.Message != "" {
		return err.Message
	}
	if err.Err != nil {
		return err.Err.Error()
	}
	return "retry send error"
}

func (err RetrySendError) Unwrap() error { return err.Err }

func (err RetrySendError) IsAborted() bool { return err.Kind == RetrySendErrorAborted }

func RetrySendErrorReqwest(err error) RetrySendError {
	return RetrySendError{Kind: RetrySendErrorRequest, Err: err}
}

func sendWithRetry(client *http.Client, request *http.Request, body []byte, options StreamOptions) (*http.Response, error) {
	var cancels []context.CancelFunc
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()
	maxRetries := options.MaxRetries
	if maxRetries == nil {
		defaultRetries := defaultMaxRetries
		maxRetries = &defaultRetries
	}
	maxRetryDelayMS := options.MaxRetryDelayMS
	if maxRetryDelayMS == nil {
		defaultRetryDelayMS := defaultMaxRetryDelayMS
		maxRetryDelayMS = &defaultRetryDelayMS
	}
	if options.TimeoutMS > 0 {
		ctx, timeoutCancel := context.WithTimeout(request.Context(), time.Duration(options.TimeoutMS)*time.Millisecond)
		cancels = append(cancels, timeoutCancel)
		request = request.WithContext(ctx)
	}
	if options.Abort != nil {
		ctx, abortCancel := context.WithCancel(request.Context())
		cancels = append(cancels, abortCancel)
		go func() {
			select {
			case <-options.Abort:
				abortCancel()
			case <-ctx.Done():
			}
		}()
		request = request.WithContext(ctx)
	}
	for attempt := 0; ; attempt++ {
		attemptRequest := request.Clone(request.Context())
		if body != nil {
			attemptRequest.Body = io.NopCloser(bytes.NewReader(body))
			attemptRequest.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(body)), nil
			}
		}
		response, err := client.Do(attemptRequest)
		if !shouldRetryResponse(response, err) || attempt >= *maxRetries {
			if response != nil && response.Body != nil && err == nil {
				response.Body = &cancelOnCloseReadCloser{ReadCloser: response.Body, cancels: cancels}
				cancels = nil
			}
			return response, err
		}
		if response != nil && response.Body != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		delayMS, err := retryDelayMS(response, attempt, *maxRetryDelayMS)
		if err != nil {
			return nil, err
		}
		select {
		case <-attemptRequest.Context().Done():
			return nil, attemptRequest.Context().Err()
		case <-time.After(time.Duration(delayMS) * time.Millisecond):
		}
	}
}

func SendWithRetry(client *http.Client, request *http.Request, body []byte, options StreamOptions) (*http.Response, error) {
	return sendWithRetry(client, request, body, options)
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancels []context.CancelFunc
}

func (reader *cancelOnCloseReadCloser) Close() error {
	err := reader.ReadCloser.Close()
	for _, cancel := range reader.cancels {
		cancel()
	}
	reader.cancels = nil
	return err
}

func shouldRetryResponse(response *http.Response, err error) bool {
	if err != nil {
		return shouldRetryError(err)
	}
	if response == nil {
		return false
	}
	return shouldRetryStatus(response.StatusCode)
}

func shouldRetryError(err error) bool {
	if err == nil || IsCanceledError(err) {
		return false
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		if urlError.Timeout() {
			return true
		}
		return shouldRetryError(urlError.Err)
	}
	var netError net.Error
	if errors.As(err, &netError) {
		return netError.Timeout() || netError.Temporary()
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "connection refused") || strings.Contains(text, "connection reset") || strings.Contains(text, "connection aborted") || strings.Contains(text, "no such host") || strings.Contains(text, "network is unreachable") || strings.Contains(text, "i/o timeout")
}

func shouldRetryStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusConflict || status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= 500
}

func retryDelayMS(response *http.Response, attempt int, maxRetryDelayMS int) (int, error) {
	capMS := max(maxRetryDelayMS, 1)
	if response != nil {
		if value := response.Header.Get("Retry-After"); value != "" {
			if seconds, err := strconv.Atoi(value); err == nil {
				delayMS := seconds * 1000
				if delayMS > maxRetryDelayMS && maxRetryDelayMS > 0 {
					return 0, RetrySendError{Kind: RetrySendErrorDelayTooLong, Message: fmt.Sprintf("server requested %dms wait, exceeds cap %dms", delayMS, maxRetryDelayMS), RequestedMS: delayMS, CapMS: maxRetryDelayMS}
				}
				return min(delayMS, capMS), nil
			}
		}
	}
	delayMS := defaultBaseDelayMS * (1 << min(attempt, 6))
	delayMS += rand.Intn(100) + 1
	if delayMS > capMS {
		return capMS, nil
	}
	return delayMS, nil
}
