package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPTransportPostsJSONAndReceivesJSONLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method mismatch: %s", r.Method)
		}
		if got := r.Header.Get("Accept"); got != "application/json, text/event-stream" {
			t.Fatalf("accept mismatch: %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["method"] != "ping" {
			t.Fatalf("request mismatch: %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	line, ok, err := transport.RecvLine(ctx)
	if err != nil || !ok || !strings.Contains(line, `"ok":true`) {
		t.Fatalf("recv mismatch line=%q ok=%v err=%v", line, ok, err)
	}
}

func TestHTTPTransportDefaultBodyCapMatchesUpstream(t *testing.T) {
	if DefaultHTTPTransportBodyCap != 1024*1024 {
		t.Fatalf("default HTTP body cap should match upstream 1MiB, got %d", DefaultHTTPTransportBodyCap)
	}
	transport := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{})
	if transport.BodyCap != DefaultHTTPTransportBodyCap {
		t.Fatalf("transport default body cap mismatch: %d", transport.BodyCap)
	}
}

func TestHTTPTransportDefaultResponseBufferMatchesUpstream(t *testing.T) {
	transport := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{})
	if cap(transport.responses) != 256 || cap(transport.errors) != 256 {
		t.Fatalf("default response buffer mismatch: responses=%d errors=%d", cap(transport.responses), cap(transport.errors))
	}
}

func TestHTTPTransportDefaultReconnectPolicyMatchesUpstream(t *testing.T) {
	transport := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{})
	if transport.reconnect.InitialDelay != 500*time.Millisecond || transport.reconnect.MaxDelay != 30*time.Second || transport.reconnect.MaxAttempts != 0 {
		t.Fatalf("default reconnect policy mismatch: %#v", transport.reconnect)
	}
}

func TestHTTPTransportReconnectPolicyOptionMatchesUpstream(t *testing.T) {
	transport := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{ReconnectPolicy: ReconnectPolicy{InitialDelay: time.Second, MaxDelay: 2 * time.Second, MaxAttempts: 3}})
	if transport.reconnect.InitialDelay != time.Second || transport.reconnect.MaxDelay != 2*time.Second || transport.reconnect.MaxAttempts != 3 {
		t.Fatalf("reconnect policy option mismatch: %#v", transport.reconnect)
	}

	legacy := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{ReconnectPolicy: ReconnectPolicy{InitialDelay: time.Second, MaxDelay: 2 * time.Second, MaxAttempts: 3}, ReconnectInitialDelay: 3 * time.Second, ReconnectMaxDelay: 4 * time.Second, ReconnectMaxAttempts: 5})
	if legacy.reconnect.InitialDelay != 3*time.Second || legacy.reconnect.MaxDelay != 4*time.Second || legacy.reconnect.MaxAttempts != 5 {
		t.Fatalf("legacy reconnect options should override struct option: %#v", legacy.reconnect)
	}
}

func TestHTTPTransportDefaultSSEIdleTimeoutMatchesUpstream(t *testing.T) {
	transport := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{})
	if transport.SSEIdleTimeout != 60*time.Second {
		t.Fatalf("default SSE idle timeout mismatch: %s", transport.SSEIdleTimeout)
	}
}

func TestHTTPTransportDefaultUserAgentMatchesUpstream(t *testing.T) {
	transport := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{})
	if transport.UserAgent != "pie-mcp/0.75.0 (mcp-streamable-http/2025-03-26)" {
		t.Fatalf("default user agent mismatch: %q", transport.UserAgent)
	}
}

func TestHTTPTransportRequestTimeoutMatchesUpstream(t *testing.T) {
	defaultTransport := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{})
	if defaultTransport.RequestTimeout != 30*time.Second {
		t.Fatalf("default request timeout mismatch: %s", defaultTransport.RequestTimeout)
	}

	customTransport := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{RequestTimeout: 5 * time.Second})
	if customTransport.RequestTimeout != 5*time.Second || customTransport.Client.Timeout != 5*time.Second {
		t.Fatalf("custom request timeout mismatch: timeout=%s client=%s", customTransport.RequestTimeout, customTransport.Client.Timeout)
	}
}

func TestHTTPTransportRequestTimeoutAppliesWithCustomClientLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{Client: server.Client(), RequestTimeout: time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected request timeout with custom client, got %v", err)
	}
}

func TestHTTPTransportUpstreamTypeAliases(t *testing.T) {
	var options HttpMcpTransportOptions = HTTPTransportOptions{BodyCap: 42}
	transport := NewHTTPTransport("https://example.test/mcp", options)
	var httpTransport *HttpMcpTransport = transport
	var none HttpMcpAuth = HttpMcpAuthNone
	var auth HttpMcpAuth = HttpMcpAuthBearer("token")
	if httpTransport.BodyCap != 42 || none.HeaderValue() != "" || auth.HeaderValue() != "Bearer token" {
		t.Fatalf("HTTP transport alias mismatch: %#v", httpTransport)
	}
}

func TestHTTPTransportBearerAuthBuilderMatchesUpstream(t *testing.T) {
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			seen <- r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer server.Close()

	options := NewHTTPMCPTransportOptions(server.URL).Bearer("secret")
	if options.EndpointURL != server.URL {
		t.Fatalf("endpoint option mismatch: %q", options.EndpointURL)
	}
	transport := NewHTTPTransport("", options)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "Bearer secret" {
		t.Fatalf("authorization header mismatch: %q", got)
	}
}

func TestHTTPTransportConnectAndSetAuthMatchUpstream(t *testing.T) {
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			select {
			case seen <- r.Header.Get("Authorization"):
			default:
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer server.Close()

	transport, err := ConnectHTTPTransport(NewHTTPMCPTransportOptions(server.URL).Bearer("old"))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	transport.SetAuth(HTTPMCPBearerAuth("new"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "Bearer new" {
		t.Fatalf("authorization header mismatch: %q", got)
	}
}

func TestHTTPTransportConnectAliasMatchesUpstream(t *testing.T) {
	transport, err := Connect(HttpMcpTransportOptions{EndpointURL: "https://example.test/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	if transport.Endpoint != "https://example.test/mcp" || transport.UserAgent == "" {
		t.Fatalf("connect alias mismatch: %#v", transport)
	}
}

func TestHTTPTransportConnectRejectsInvalidUserAgentLikeUpstream(t *testing.T) {
	_, err := ConnectHTTPTransport(HTTPTransportOptions{EndpointURL: "https://example.test/mcp", UserAgent: "bad\nagent"})
	if err == nil || !strings.Contains(err.Error(), "invalid streamable_http user agent") {
		t.Fatalf("expected invalid user agent error, got %v", err)
	}
}

func TestHTTPTransportConnectStartsSSELikeUpstream(t *testing.T) {
	seen := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method mismatch: %s", r.Method)
		}
		seen <- struct{}{}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/ping\"}\n\n"))
	}))
	defer server.Close()

	transport, err := ConnectHTTPTransport(HTTPTransportOptions{EndpointURL: server.URL, SSEIdleTimeout: 10 * time.Millisecond, ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("ConnectHTTPTransport did not start SSE loop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	line, ok, err := transport.RecvLine(ctx)
	if err != nil || !ok || !strings.Contains(line, "notifications/ping") {
		t.Fatalf("SSE notification mismatch line=%q ok=%v err=%v", line, ok, err)
	}
}

func TestHTTPTransportStartSSEOnlyStartsOnce(t *testing.T) {
	requests := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/ping\"}\n\n"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{SSEIdleTimeout: 10 * time.Millisecond, ReconnectInitialDelay: time.Hour, ReconnectMaxDelay: time.Hour})
	defer transport.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	transport.StartSSE(ctx)
	transport.StartSSE(ctx)
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("StartSSE did not start loop")
	}
	select {
	case <-requests:
		t.Fatal("StartSSE started more than one loop")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestHTTPTransportDebugRedactsBearerTokenLikeUpstream(t *testing.T) {
	transport := NewHTTPTransport("https://example.test/mcp", HTTPTransportOptions{Headers: map[string]string{"Authorization": "Bearer hub_agent_secret"}})
	text := fmt.Sprintf("%#v", transport)
	if strings.Contains(text, "hub_agent_secret") || !strings.Contains(text, "<redacted>") {
		t.Fatalf("debug output should redact bearer token like upstream: %s", text)
	}
}

func TestHTTPMCPAuthDebugRedactsBearerTokenLikeUpstream(t *testing.T) {
	text := fmt.Sprintf("%#v", HTTPMCPBearerAuth("hub_agent_secret"))
	if strings.Contains(text, "hub_agent_secret") || !strings.Contains(text, "<redacted>") {
		t.Fatalf("auth debug output should redact bearer token like upstream: %s", text)
	}
}

func TestHTTPTransportReceivesSSEResponseLines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"pong\":true}}\n\n"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":2,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	line, ok, err := transport.RecvLine(ctx)
	if err != nil || !ok || !strings.Contains(line, `"pong":true`) {
		t.Fatalf("sse recv mismatch line=%q ok=%v err=%v", line, ok, err)
	}
}

func TestHTTPTransportPostsRequestsAndReceivesSSENotificationsLikeUpstream(t *testing.T) {
	var seenMu sync.Mutex
	seenAuth := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMu.Lock()
		seenAuth = append(seenAuth, r.Header.Get("Authorization"))
		seenMu.Unlock()
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPost:
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			id, hasID := request["id"]
			if !hasID {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			var result string
			switch request["method"] {
			case "initialize":
				result = `{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"fixture-hub","version":"0.1.0"}}`
			case "tools/list":
				result = `{"tools":[{"name":"send_notification","description":"fixture","inputSchema":{"type":"object"}}]}`
			default:
				t.Fatalf("unexpected method %v", request["method"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%v,"result":%s}`, id, result)
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("id: fixture-1\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/agent_message\",\"params\":{\"_meta\":{\"pie_summary\":\"fixture\",\"pie_dedup_key\":\"fixture-1\"}}}\n\n"))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	transport, err := ConnectHTTPTransport(NewHTTPMCPTransportOptions(server.URL).Bearer("fixture-token"))
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	client := NewClient(transport).WithTimeout(time.Second)

	init, err := client.Initialize(context.Background(), "pig-test")
	if err != nil {
		t.Fatal(err)
	}
	if init.ServerInfo.Name != "fixture-hub" {
		t.Fatalf("server info mismatch: %#v", init.ServerInfo)
	}
	notifications, ok := client.TakeNotifications()
	if !ok {
		t.Fatal("notification receiver should be available")
	}
	tools, err := client.ToolsList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "send_notification" {
		t.Fatalf("tools mismatch: %#v", tools)
	}
	select {
	case notification := <-notifications:
		params, ok := notification.Params.(json.RawMessage)
		if !ok {
			t.Fatalf("notification params type mismatch: %#v", notification.Params)
		}
		var decoded map[string]any
		if err := json.Unmarshal(params, &decoded); err != nil {
			t.Fatal(err)
		}
		meta := decoded["_meta"].(map[string]any)
		if notification.Method != "notifications/agent_message" || meta["pie_summary"] != "fixture" {
			t.Fatalf("notification mismatch: %#v", notification)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification should arrive")
	}

	seenMu.Lock()
	defer seenMu.Unlock()
	if len(seenAuth) == 0 {
		t.Fatal("expected fixture to see authorized requests")
	}
	for _, header := range seenAuth {
		if header != "Bearer fixture-token" {
			t.Fatalf("every request must use bearer auth, saw %#v", seenAuth)
		}
	}
}

func TestHTTPTransportPostSSEIsReadInBackgroundLikeUpstream(t *testing.T) {
	gate := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"ok\":true}\n\n"))
		flusher.Flush()
		<-gate
	}))
	defer server.Close()
	defer close(gate)

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{})
	sendCtx, sendCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer sendCancel()
	if err := transport.SendLine(sendCtx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatalf("SendLine should return after spawning POST SSE reader like upstream, got %v", err)
	}
	recvCtx, recvCancel := context.WithTimeout(context.Background(), time.Second)
	defer recvCancel()
	line, ok, err := transport.RecvLine(recvCtx)
	if err != nil || !ok || !strings.Contains(line, `"ok":true`) {
		t.Fatalf("background POST SSE recv mismatch line=%q ok=%v err=%v", line, ok, err)
	}
}

func TestHTTPTransportPostSSEOutlivesSendContextLikeUpstream(t *testing.T) {
	second := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"first\":true}\n\n"))
		flusher.Flush()
		<-second
		_, _ = w.Write([]byte("data: {\"second\":true}\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{})
	sendCtx, sendCancel := context.WithCancel(context.Background())
	if err := transport.SendLine(sendCtx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	sendCancel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	line, ok, err := transport.RecvLine(ctx)
	if err != nil || !ok || !strings.Contains(line, `"first":true`) {
		t.Fatalf("first POST SSE recv mismatch line=%q ok=%v err=%v", line, ok, err)
	}
	close(second)
	line, ok, err = transport.RecvLine(ctx)
	if err != nil || !ok || !strings.Contains(line, `"second":true`) {
		t.Fatalf("POST SSE reader should outlive send context like upstream, line=%q ok=%v err=%v", line, ok, err)
	}
}

func TestHTTPTransportSurfacesStatusAndBodyCap(t *testing.T) {
	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer statusServer.Close()
	transport := NewHTTPTransport(statusServer.URL, HTTPTransportOptions{})
	if err := transport.SendLine(context.Background(), `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected status error, got %v", err)
	}

	largeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", DefaultHTTPTransportBodyCap+1)))
	}))
	defer largeServer.Close()
	transport = NewHTTPTransport(largeServer.URL, HTTPTransportOptions{})
	if err := transport.SendLine(context.Background(), `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err == nil || !strings.Contains(err.Error(), "body too large") {
		t.Fatalf("expected body cap error, got %v", err)
	}
}

func TestHTTPTransportSSEStatusErrorMatchesUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sse secret should not leak", http.StatusBadGateway)
	}))
	defer server.Close()
	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.RunSSE(ctx) }()

	_, ok, err := transport.RecvLine(ctx)
	if err == nil || ok || !strings.Contains(err.Error(), "MCP HTTP SSE reconnect attempts exhausted") || strings.Contains(err.Error(), "sse secret") {
		t.Fatalf("expected redacted upstream SSE status exhaustion, ok=%v err=%v", ok, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSSE should stop cleanly after reporting SSE status exhaustion, got %v", err)
	}
}

func TestHTTPTransportRejectsOversizeRequestBeforePostLikeUpstream(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{BodyCap: 5})
	err := transport.SendLine(context.Background(), "123456")
	if err == nil || !strings.Contains(err.Error(), "request exceeded body cap") {
		t.Fatalf("oversize request should fail like upstream, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("oversize request should be rejected before POST, calls=%d", calls.Load())
	}
}

func TestHTTPTransportRunSSEReconnectsWithLastEventID(t *testing.T) {
	var calls atomic.Int32
	lastEventIDs := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method mismatch: %s", r.Method)
		}
		lastEventIDs <- r.Header.Get("Last-Event-ID")
		w.Header().Set("Content-Type", "text/event-stream")
		call := calls.Add(1)
		if call == 1 {
			_, _ = w.Write([]byte("id: first\nevent: message\ndata: {\"one\":true}\n\n"))
			return
		}
		_, _ = w.Write([]byte("id: second\nevent: message\ndata: {\"two\":true}\n\n"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 2})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.RunSSE(ctx) }()

	line, ok, err := transport.RecvLine(ctx)
	if err != nil || !ok || !strings.Contains(line, `"one":true`) {
		t.Fatalf("first recv mismatch line=%q ok=%v err=%v", line, ok, err)
	}
	line, ok, err = transport.RecvLine(ctx)
	if err != nil || !ok || !strings.Contains(line, `"two":true`) {
		t.Fatalf("second recv mismatch line=%q ok=%v err=%v", line, ok, err)
	}
	first := <-lastEventIDs
	second := <-lastEventIDs
	if first != "" || second != "first" {
		t.Fatalf("last event ids mismatch first=%q second=%q", first, second)
	}
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatalf("run sse error: %v", err)
	}
}

func TestHTTPTransportRunSSEExhaustionReportsQueueErrorLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.RunSSE(ctx) }()

	_, ok, err := transport.RecvLine(ctx)
	if err == nil || ok || !strings.Contains(err.Error(), "MCP HTTP SSE reconnect attempts exhausted") {
		t.Fatalf("expected upstream queue exhaustion error, ok=%v err=%v", ok, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSSE should stop cleanly after reporting exhaustion like upstream, got %v", err)
	}
}

func TestHTTPTransportRunSSEIdleTimeoutExhaustsLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(": connected\n\n"))
		flusher.Flush()
		time.Sleep(time.Second)
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{SSEIdleTimeout: 10 * time.Millisecond, ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.RunSSE(ctx) }()

	_, ok, err := transport.RecvLine(ctx)
	if err == nil || ok || !strings.Contains(err.Error(), "MCP HTTP SSE reconnect attempts exhausted") {
		t.Fatalf("expected idle timeout exhaustion error, ok=%v err=%v", ok, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSSE should stop cleanly after idle timeout exhaustion, got %v", err)
	}
}

func TestHTTPTransportRunSSEHandshakeTimeoutExhaustsLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{SSEIdleTimeout: 10 * time.Millisecond, ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.RunSSE(ctx) }()

	_, ok, err := transport.RecvLine(ctx)
	if err == nil || ok || !strings.Contains(err.Error(), "MCP HTTP SSE reconnect attempts exhausted") {
		t.Fatalf("expected handshake timeout exhaustion, ok=%v err=%v", ok, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSSE should stop cleanly after handshake timeout exhaustion, got %v", err)
	}
}

func TestHTTPTransportRunSSEUpdatesLastEventIDWithoutDataLikeUpstream(t *testing.T) {
	var calls atomic.Int32
	lastEventIDs := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastEventIDs <- r.Header.Get("Last-Event-ID")
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte("id: first\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"ok\":true}\n\n"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 2})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.RunSSE(ctx) }()

	line, ok, err := transport.RecvLine(ctx)
	if err != nil || !ok || !strings.Contains(line, `"ok":true`) {
		t.Fatalf("recv mismatch line=%q ok=%v err=%v", line, ok, err)
	}
	first := <-lastEventIDs
	second := <-lastEventIDs
	if first != "" || second != "first" {
		t.Fatalf("id-only event should update Last-Event-ID like upstream, first=%q second=%q", first, second)
	}
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		t.Fatalf("run sse error: %v", err)
	}
}

func TestHTTPTransportSSEBodyCapAppliesPerFrameLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"n\":1}\n\n"))
		_, _ = w.Write([]byte("data: {\"n\":2}\n\n"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{BodyCap: 64})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		line, ok, err := transport.RecvLine(ctx)
		if err != nil || !ok || !strings.Contains(line, fmt.Sprintf(`"n":%d`, i)) {
			t.Fatalf("recv %d mismatch line=%q ok=%v err=%v", i, line, ok, err)
		}
	}
}

func TestHTTPTransportSSERejectsFrameOverBodyCapLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + strings.Repeat("x", 64) + "\n\n"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{BodyCap: 50, ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.RunSSE(ctx) }()
	_, ok, err := transport.RecvLine(ctx)
	if err == nil || ok || !strings.Contains(err.Error(), "MCP HTTP SSE reconnect attempts exhausted") {
		t.Fatalf("oversize SSE frame should exhaust like upstream, ok=%v err=%v", ok, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSSE should stop cleanly after reporting exhaustion, got %v", err)
	}
}

func TestHTTPTransportSSERejectsInvalidUTF8LikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte{'d', 'a', 't', 'a', ':', ' ', 0xff, '\n', '\n'})
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- transport.RunSSE(ctx) }()
	_, ok, err := transport.RecvLine(ctx)
	if err == nil || ok || !strings.Contains(err.Error(), "MCP HTTP SSE reconnect attempts exhausted") {
		t.Fatalf("invalid UTF-8 SSE frame should exhaust like upstream, ok=%v err=%v", ok, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("RunSSE should stop cleanly after reporting exhaustion, got %v", err)
	}
}

func TestHTTPTransportSSEAllowsFrameUpToBodyCapLikeUpstream(t *testing.T) {
	payload := strings.Repeat("x", 1024*1024+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + payload + "\n\n"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{BodyCap: int64(len(payload) + len("data: \n\n"))})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	line, ok, err := transport.RecvLine(ctx)
	if err != nil || !ok || len(line) != len(payload) {
		t.Fatalf("large SSE frame mismatch len=%d ok=%v err=%v", len(line), ok, err)
	}
}

func TestHTTPTransportSSEIgnoresIncompleteFrameAtEOFLIkeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"partial\":true}"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	recvCtx, recvCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer recvCancel()
	line, ok, err := transport.RecvLine(recvCtx)
	if err == nil || err != context.DeadlineExceeded || ok || line != "" {
		t.Fatalf("incomplete SSE frame should not be emitted like upstream, line=%q ok=%v err=%v", line, ok, err)
	}
}

func TestHTTPTransportSSEPreservesTrailingSpacesLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data:  value  \n\n"))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	line, ok, err := transport.RecvLine(ctx)
	if err != nil || !ok || line != "value  " {
		t.Fatalf("SSE data should trim leading spaces only like upstream, line=%q ok=%v err=%v", line, ok, err)
	}
}

func TestHTTPTransportPropagatesSessionIDToPostAndSSE(t *testing.T) {
	seenSessionOnPost := make(chan string, 1)
	seenSessionOnSSE := make(chan string, 1)
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			call := posts.Add(1)
			if call == 1 {
				w.Header().Set("Mcp-Session-Id", "session-123")
			} else {
				seenSessionOnPost <- r.Header.Get("Mcp-Session-Id")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
		case http.MethodGet:
			seenSessionOnSSE <- r.Header.Get("Mcp-Session-Id")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message\ndata: {\"ok\":true}\n\n"))
		}
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{ReconnectInitialDelay: time.Millisecond, ReconnectMaxDelay: time.Millisecond, ReconnectMaxAttempts: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":1,"method":"first"}`); err != nil {
		t.Fatal(err)
	}
	if got := transport.SessionID(); got != "session-123" {
		t.Fatalf("session id mismatch: %q", got)
	}
	if err := transport.SendLine(ctx, `{"jsonrpc":"2.0","id":2,"method":"second"}`); err != nil {
		t.Fatal(err)
	}
	if got := <-seenSessionOnPost; got != "session-123" {
		t.Fatalf("post session header mismatch: %q", got)
	}
	done := make(chan error, 1)
	go func() { done <- transport.RunSSE(ctx) }()
	if _, ok, err := transport.RecvLine(ctx); err != nil || !ok {
		t.Fatalf("sse recv mismatch ok=%v err=%v", ok, err)
	}
	if got := <-seenSessionOnSSE; got != "session-123" {
		t.Fatalf("sse session header mismatch: %q", got)
	}
	cancel()
	<-done
}

func TestHTTPTransportSetSessionID(t *testing.T) {
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Mcp-Session-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	defer server.Close()
	transport := NewHTTPTransport(server.URL, HTTPTransportOptions{})
	transport.SetSessionID("restored")
	if transport.SessionID() != "restored" {
		t.Fatalf("session id mismatch: %q", transport.SessionID())
	}
	if err := transport.SendLine(context.Background(), `{"jsonrpc":"2.0","id":1,"method":"ping"}`); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got != "restored" {
		t.Fatalf("session header mismatch: %q", got)
	}
}
