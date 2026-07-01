package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRetryableStatusMatchesUpstream(t *testing.T) {
	retryable := []int{
		http.StatusRequestTimeout,
		http.StatusConflict,
		http.StatusTooEarly,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	}
	for _, status := range retryable {
		if !shouldRetryStatus(status) {
			t.Fatalf("status %d should be retryable", status)
		}
	}
	nonRetryable := []int{http.StatusOK, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden}
	for _, status := range nonRetryable {
		if shouldRetryStatus(status) {
			t.Fatalf("status %d should not be retryable", status)
		}
	}
}

func TestRetrySendErrorIsAbortedMatchesUpstream(t *testing.T) {
	if !(RetrySendError{Kind: RetrySendErrorAborted}).IsAborted() {
		t.Fatal("aborted retry error should report IsAborted")
	}
	if (RetrySendError{Kind: RetrySendErrorRequest}).IsAborted() {
		t.Fatal("request retry error should not report IsAborted")
	}
}

func TestRetryDelayErrorsWhenRetryAfterExceedsCapLikeUpstream(t *testing.T) {
	response := &http.Response{Header: http.Header{"Retry-After": []string{"5"}}}
	if _, err := retryDelayMS(response, 0, 1000); err == nil {
		t.Fatal("expected retry-after cap error like upstream send_with_retry")
	} else {
		var retryErr RetrySendError
		if !errors.As(err, &retryErr) || retryErr.Kind != RetrySendErrorDelayTooLong {
			t.Fatalf("expected RetrySendErrorDelayTooLong, got %#v", err)
		}
	}

	delayMS, err := retryDelayMS(response, 0, 6000)
	if err != nil {
		t.Fatal(err)
	}
	if delayMS != 5000 {
		t.Fatalf("delay mismatch: %d", delayMS)
	}
}

func TestSendWithRetryUpstreamWrapperUsesExistingRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := SendWithRetry(server.Client(), request, nil, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
}

func TestRetryDelayZeroCapMatchesUpstreamMinimumCap(t *testing.T) {
	delayMS, err := retryDelayMS(nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if delayMS != 1 {
		t.Fatalf("zero cap should clamp backoff delay to upstream minimum 1ms, got %d", delayMS)
	}

	response := &http.Response{Header: http.Header{"Retry-After": []string{"5"}}}
	delayMS, err = retryDelayMS(response, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if delayMS != 1 {
		t.Fatalf("zero cap should clamp retry-after delay to upstream minimum 1ms, got %d", delayMS)
	}
}

func TestSendWithRetryHonorsStreamOptionsAbortLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	abort := make(chan struct{})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	close(abort)

	_, err = sendWithRetry(server.Client(), request, nil, StreamOptions{Abort: abort})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sendWithRetry should return context.Canceled after Abort closes, got %v", err)
	}
}

func TestSendWithRetryDoesNotRetryNonNetworkErrorsLikeUpstream(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("certificate rejected")
	})}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	maxRetries := 2
	_, err = sendWithRetry(client, request, nil, StreamOptions{MaxRetries: &maxRetries})
	if err == nil || !strings.Contains(err.Error(), "certificate rejected") {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("non-network errors should not retry like upstream, attempts=%d", attempts)
	}
}

func TestSendWithRetryRetriesConnectErrorsLikeUpstream(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("dial tcp 127.0.0.1:9: connect: connection refused")
	})}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	maxRetries := 2
	maxRetryDelayMS := 1
	_, err = sendWithRetry(client, request, nil, StreamOptions{MaxRetries: &maxRetries, MaxRetryDelayMS: &maxRetryDelayMS})
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("connect errors should retry like upstream, attempts=%d", attempts)
	}
}

func TestSendWithRetryRetriesClosedNetworkConnection(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripErrorFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("readfrom tcp 172.23.145.126:53177->10.88.128.112:80: write tcp 172.23.145.126:53177->10.88.128.112:80: use of closed network connection")
	})}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	maxRetries := 2
	maxRetryDelayMS := 1
	_, err = SendWithRetry(client, request, nil, StreamOptions{MaxRetries: &maxRetries, MaxRetryDelayMS: &maxRetryDelayMS})
	if err == nil || !strings.Contains(err.Error(), "use of closed network connection") {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("closed network connection errors should retry, attempts=%d", attempts)
	}
}

func TestSendWithRetryKeepsStreamAliveUntilAbortLikeUpstream(t *testing.T) {
	abort := make(chan struct{})
	writeDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		_, _ = w.Write([]byte("first\n"))
		flusher.Flush()
		<-r.Context().Done()
		_, _ = w.Write([]byte("done\n"))
		close(writeDone)
	}))
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := sendWithRetry(server.Client(), request, nil, StreamOptions{Abort: abort})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	buffer := make([]byte, len("first\n"))
	if _, err := io.ReadFull(response.Body, buffer); err != nil {
		t.Fatalf("response body should remain readable after sendWithRetry returns: %v", err)
	}
	select {
	case <-writeDone:
		t.Fatal("stream context should not be canceled before Abort closes")
	case <-time.After(20 * time.Millisecond):
	}

	close(abort)
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("Abort should cancel in-flight stream consumption")
	}
}
