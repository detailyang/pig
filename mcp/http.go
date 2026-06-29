package mcp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const DefaultHTTPTransportBodyCap = 1024 * 1024

const defaultHTTPTransportTimeout = 30 * time.Second

type HTTPTransportOptions struct {
	EndpointURL           string
	Client                *http.Client
	BodyCap               int64
	Headers               map[string]string
	Auth                  HttpMcpAuth
	UserAgent             string
	BufferSize            int
	RequestTimeout        time.Duration
	SSEIdleTimeout        time.Duration
	ReconnectPolicy       ReconnectPolicy
	ReconnectInitialDelay time.Duration
	ReconnectMaxDelay     time.Duration
	ReconnectMaxAttempts  int
}

type HttpMcpTransportOptions = HTTPTransportOptions

func NewHTTPMCPTransportOptions(endpoint string) HTTPTransportOptions {
	return HTTPTransportOptions{EndpointURL: endpoint}
}

type HttpMcpAuth struct {
	bearerToken string
}

var HttpMcpAuthNone HttpMcpAuth

func HttpMcpAuthBearer(token string) HttpMcpAuth {
	return HTTPMCPBearerAuth(token)
}

func HTTPMCPBearerAuth(token string) HttpMcpAuth {
	return HttpMcpAuth{bearerToken: token}
}

func (auth HttpMcpAuth) String() string {
	if auth.bearerToken == "" {
		return "None"
	}
	return "Bearer { token: <redacted> }"
}

func (auth HttpMcpAuth) GoString() string {
	return auth.String()
}

func (auth HttpMcpAuth) HeaderValue() string {
	if auth.bearerToken == "" {
		return ""
	}
	return "Bearer " + auth.bearerToken
}

func (options HTTPTransportOptions) Bearer(token string) HTTPTransportOptions {
	options.Auth = HTTPMCPBearerAuth(token)
	return options
}

type HTTPTransport struct {
	Endpoint       string
	Client         *http.Client
	BodyCap        int64
	Headers        map[string]string
	UserAgent      string
	RequestTimeout time.Duration
	SSEIdleTimeout time.Duration
	reconnect      ReconnectPolicy
	authMu         sync.RWMutex
	sessionMu      sync.RWMutex
	sessionID      string
	responses      chan string
	errors         chan error
	lifetime       context.Context
	cancel         context.CancelFunc
	closed         chan struct{}
	closeOnce      sync.Once
	sseOnce        sync.Once
}

type HttpMcpTransport = HTTPTransport

type ReconnectPolicy struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
}

func NewHTTPTransport(endpoint string, options HTTPTransportOptions) *HTTPTransport {
	if endpoint == "" {
		endpoint = options.EndpointURL
	}
	requestTimeout := options.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultHTTPTransportTimeout
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	bodyCap := options.BodyCap
	if bodyCap <= 0 {
		bodyCap = DefaultHTTPTransportBodyCap
	}
	bufferSize := options.BufferSize
	if bufferSize <= 0 {
		bufferSize = 256
	}
	userAgent := options.UserAgent
	if userAgent == "" {
		userAgent = "pie-mcp/0.75.0 (mcp-streamable-http/2025-03-26)"
	}
	sseIdleTimeout := options.SSEIdleTimeout
	if sseIdleTimeout <= 0 {
		sseIdleTimeout = 60 * time.Second
	}
	reconnect := options.ReconnectPolicy
	if options.ReconnectInitialDelay > 0 {
		reconnect.InitialDelay = options.ReconnectInitialDelay
	}
	if options.ReconnectMaxDelay > 0 {
		reconnect.MaxDelay = options.ReconnectMaxDelay
	}
	if options.ReconnectMaxAttempts > 0 {
		reconnect.MaxAttempts = options.ReconnectMaxAttempts
	}
	if reconnect.InitialDelay <= 0 {
		reconnect.InitialDelay = 500 * time.Millisecond
	}
	if reconnect.MaxDelay <= 0 {
		reconnect.MaxDelay = 30 * time.Second
	}
	headers := map[string]string{}
	for key, value := range options.Headers {
		headers[key] = value
	}
	if authorization := options.Auth.HeaderValue(); authorization != "" {
		headers["Authorization"] = authorization
	}
	lifetime, cancel := context.WithCancel(context.Background())
	return &HTTPTransport{Endpoint: endpoint, Client: client, BodyCap: bodyCap, Headers: headers, UserAgent: userAgent, RequestTimeout: requestTimeout, SSEIdleTimeout: sseIdleTimeout, reconnect: reconnect, responses: make(chan string, bufferSize), errors: make(chan error, bufferSize), lifetime: lifetime, cancel: cancel, closed: make(chan struct{})}
}

func ConnectHTTPTransport(options HTTPTransportOptions) (*HTTPTransport, error) {
	endpoint, err := url.Parse(options.EndpointURL)
	if err != nil {
		return nil, TransportError{Message: "invalid MCP HTTP endpoint: " + err.Error()}
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "127.0.0.1" {
		return nil, TransportError{Message: "streamable_http endpoint must be https, except 127.0.0.1 test fixtures"}
	}
	userAgent := options.UserAgent
	if userAgent == "" {
		userAgent = "pie-mcp/0.75.0 (mcp-streamable-http/2025-03-26)"
	}
	if !validHTTPHeaderValue(userAgent) {
		return nil, TransportError{Message: "invalid streamable_http user agent"}
	}
	transport := NewHTTPTransport(options.EndpointURL, options)
	transport.StartSSE(context.Background())
	return transport, nil
}

func Connect(options HttpMcpTransportOptions) (*HttpMcpTransport, error) {
	return ConnectHTTPTransport(options)
}

func validHTTPHeaderValue(value string) bool {
	for index := 0; index < len(value); index++ {
		char := value[index]
		if char == '\t' {
			continue
		}
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

func (transport *HTTPTransport) SetAuth(auth HttpMcpAuth) {
	transport.authMu.Lock()
	defer transport.authMu.Unlock()
	if authorization := auth.HeaderValue(); authorization != "" {
		transport.Headers["Authorization"] = authorization
		return
	}
	delete(transport.Headers, "Authorization")
}

func (transport *HTTPTransport) GoString() string {
	if transport == nil {
		return "(*mcp.HTTPTransport)(nil)"
	}
	headers := transport.copyHeaders()
	for key, value := range headers {
		headers[key] = value
		if strings.EqualFold(key, "authorization") && strings.HasPrefix(strings.ToLower(value), "bearer ") {
			headers[key] = "Bearer <redacted>"
		}
	}
	return fmt.Sprintf("&mcp.HTTPTransport{Endpoint:%q, BodyCap:%d, Headers:%#v, UserAgent:%q, SSEIdleTimeout:%s}", transport.Endpoint, transport.BodyCap, headers, transport.UserAgent, transport.SSEIdleTimeout)
}

func (transport *HTTPTransport) SendLine(ctx context.Context, line string) error {
	select {
	case <-transport.closed:
		return io.ErrClosedPipe
	default:
	}
	if int64(len(line)) > transport.BodyCap {
		return TransportError{Message: "MCP HTTP request exceeded body cap"}
	}
	requestCtx := transport.lifetime
	var requestCancel context.CancelFunc
	if transport.RequestTimeout > 0 {
		requestCtx, requestCancel = context.WithTimeout(requestCtx, transport.RequestTimeout)
		defer func() {
			if requestCancel != nil {
				requestCancel()
			}
		}()
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, transport.Endpoint, strings.NewReader(line))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", transport.UserAgent)
	transport.applySession(req)
	for key, value := range transport.copyHeaders() {
		req.Header.Set(key, value)
	}
	resp, err := transport.Client.Do(req)
	if err != nil {
		return TransportError{Message: err.Error()}
	}
	transport.captureSession(resp)
	postSSE := strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	if postSSE {
		requestCancel = nil
	}
	if err := transport.enqueueResponse(ctx, resp); err != nil {
		return err
	}
	return nil
}

func (transport *HTTPTransport) SessionID() string {
	transport.sessionMu.RLock()
	defer transport.sessionMu.RUnlock()
	return transport.sessionID
}

func (transport *HTTPTransport) SetSessionID(sessionID string) {
	transport.sessionMu.Lock()
	defer transport.sessionMu.Unlock()
	transport.sessionID = sessionID
}

func (transport *HTTPTransport) RecvLine(ctx context.Context) (string, bool, error) {
	select {
	case line := <-transport.responses:
		return line, true, nil
	case err := <-transport.errors:
		return "", false, err
	case <-ctx.Done():
		return "", false, ctx.Err()
	case <-transport.closed:
		return "", false, nil
	}
}

func (transport *HTTPTransport) Close() error {
	transport.closeOnce.Do(func() {
		transport.cancel()
		close(transport.closed)
	})
	return nil
}

func (transport *HTTPTransport) StartSSE(ctx context.Context) {
	transport.sseOnce.Do(func() {
		go func() { _ = transport.RunSSE(ctx) }()
	})
}

func (transport *HTTPTransport) RunSSE(ctx context.Context) error {
	delay := transport.reconnect.InitialDelay
	attempts := 0
	lastEventID := ""
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-transport.closed:
			return nil
		default:
		}
		err := transport.runSSEOnce(ctx, &lastEventID)
		if err == nil {
			delay = transport.reconnect.InitialDelay
			attempts = 0
		} else {
			attempts++
			if transport.reconnect.MaxAttempts > 0 && attempts >= transport.reconnect.MaxAttempts {
				return transport.pushError(ctx, TransportError{Message: "MCP HTTP SSE reconnect attempts exhausted"})
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-transport.closed:
			return nil
		case <-time.After(delay):
		}
		delay *= 2
		if delay > transport.reconnect.MaxDelay {
			delay = transport.reconnect.MaxDelay
		}
	}
}

func (transport *HTTPTransport) pushError(ctx context.Context, err error) error {
	select {
	case transport.errors <- err:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-transport.closed:
		return io.ErrClosedPipe
	}
}

func (transport *HTTPTransport) runSSEOnce(ctx context.Context, lastEventID *string) error {
	requestCtx := ctx
	var requestCancel context.CancelFunc
	if transport.SSEIdleTimeout > 0 {
		requestCtx, requestCancel = context.WithTimeout(ctx, transport.SSEIdleTimeout)
		defer func() {
			if requestCancel != nil {
				requestCancel()
			}
		}()
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, transport.Endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", transport.UserAgent)
	transport.applySession(req)
	if *lastEventID != "" {
		req.Header.Set("Last-Event-ID", *lastEventID)
	}
	for key, value := range transport.copyHeaders() {
		req.Header.Set(key, value)
	}
	resp, err := transport.Client.Do(req)
	if err != nil {
		return TransportError{Message: err.Error()}
	}
	requestCancel = nil
	defer resp.Body.Close()
	transport.captureSession(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TransportError{Message: fmt.Sprintf("MCP HTTP SSE status %s; response body redacted", resp.Status)}
	}
	if contentType := strings.ToLower(resp.Header.Get("Content-Type")); !strings.HasPrefix(contentType, "text/event-stream") {
		return TransportError{Message: "MCP HTTP SSE response was not text/event-stream"}
	}
	return transport.readSSE(ctx, resp.Body, lastEventID, transport.SSEIdleTimeout)
}

func (transport *HTTPTransport) copyHeaders() map[string]string {
	transport.authMu.RLock()
	defer transport.authMu.RUnlock()
	headers := make(map[string]string, len(transport.Headers))
	for key, value := range transport.Headers {
		headers[key] = value
	}
	return headers
}

func (transport *HTTPTransport) applySession(req *http.Request) {
	if sessionID := transport.SessionID(); sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
}

func (transport *HTTPTransport) captureSession(resp *http.Response) {
	if sessionID := strings.TrimSpace(resp.Header.Get("Mcp-Session-Id")); sessionID != "" {
		transport.SetSessionID(sessionID)
	}
}

func (transport *HTTPTransport) enqueueResponse(ctx context.Context, resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return TransportError{Message: fmt.Sprintf("MCP HTTP status %s; response body redacted", resp.Status)}
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "text/event-stream") {
		go func() {
			defer resp.Body.Close()
			_ = transport.enqueueSSE(transport.lifetime, resp.Body)
		}()
		return nil
	}
	defer resp.Body.Close()
	text, err := readCappedText(resp.Body, transport.BodyCap)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return transport.pushResponse(ctx, text)
}

func (transport *HTTPTransport) enqueueSSE(ctx context.Context, body io.Reader) error {
	lastEventID := ""
	return transport.readSSE(ctx, body, &lastEventID, 0)
}

func (transport *HTTPTransport) readSSE(ctx context.Context, body io.Reader, lastEventID *string, idleTimeout time.Duration) error {
	reader := body
	if idleTimeout > 0 {
		reader = &idleTimeoutReader{reader: body, timeout: idleTimeout}
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), int(transport.BodyCap+1))
	var data []string
	eventID := ""
	var frameBytes int64
	flush := func() error {
		frameBytes = 0
		if eventID != "" {
			*lastEventID = eventID
			eventID = ""
		}
		if len(data) == 0 {
			return nil
		}
		line := strings.Join(data, "\n")
		data = nil
		return transport.pushResponse(ctx, line)
	}
	for scanner.Scan() {
		bytes := scanner.Bytes()
		if !utf8.Valid(bytes) {
			return TransportError{Message: "utf8: invalid SSE frame"}
		}
		line := string(bytes)
		frameBytes += int64(len(line)) + 1
		if frameBytes > transport.BodyCap {
			return TransportError{Message: fmt.Sprintf("body too large: exceeds %d bytes", transport.BodyCap)}
		}
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "id:") {
			eventID = strings.TrimLeft(strings.TrimPrefix(line, "id:"), " \t\n\r")
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimLeft(strings.TrimPrefix(line, "data:"), " \t\n\r"))
		}
	}
	if err := scanner.Err(); err != nil {
		return TransportError{Message: err.Error()}
	}
	return nil
}

type idleTimeoutReader struct {
	reader  io.Reader
	timeout time.Duration
}

func (reader *idleTimeoutReader) Read(buffer []byte) (int, error) {
	type result struct {
		n   int
		err error
	}
	resultCh := make(chan result, 1)
	go func() {
		n, err := reader.reader.Read(buffer)
		resultCh <- result{n: n, err: err}
	}()
	select {
	case result := <-resultCh:
		return result.n, result.err
	case <-time.After(reader.timeout):
		return 0, net.Error(&timeoutError{timeout: reader.timeout})
	}
}

type timeoutError struct {
	timeout time.Duration
}

func (err *timeoutError) Error() string   { return fmt.Sprintf("timeout after %s", err.timeout) }
func (err *timeoutError) Timeout() bool   { return true }
func (err *timeoutError) Temporary() bool { return true }

func (transport *HTTPTransport) pushResponse(ctx context.Context, line string) error {
	select {
	case transport.responses <- line:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-transport.closed:
		return io.ErrClosedPipe
	}
}

func readCappedText(reader io.Reader, capBytes int64) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, capBytes+1))
	if err != nil {
		return "", TransportError{Message: err.Error()}
	}
	if int64(len(data)) > capBytes {
		return "", TransportError{Message: fmt.Sprintf("body too large: exceeds %d bytes", capBytes)}
	}
	return string(data), nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
