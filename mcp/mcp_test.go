package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type pipeTransport struct {
	in     chan string
	out    chan string
	closed chan struct{}
}

type blockedSendTransport struct {
	sendStarted chan struct{}
	sendRelease chan struct{}
	closed      chan struct{}
	once        sync.Once
}

type observingRecvTransport struct {
	recvStarted chan struct{}
	closed      chan struct{}
	once        sync.Once
}

func newBlockedSendTransport() *blockedSendTransport {
	return &blockedSendTransport{sendStarted: make(chan struct{}), sendRelease: make(chan struct{}), closed: make(chan struct{})}
}

func newObservingRecvTransport() *observingRecvTransport {
	return &observingRecvTransport{recvStarted: make(chan struct{}), closed: make(chan struct{})}
}

func TestMcpErrorVariantConstructorsMatchUpstream(t *testing.T) {
	if PROTOCOL_VERSION != ProtocolVersion {
		t.Fatalf("protocol version alias mismatch: %q", PROTOCOL_VERSION)
	}
	if PROTOCOL_VERSION != "2025-03-26" {
		t.Fatalf("protocol version value mismatch: %q", PROTOCOL_VERSION)
	}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "protocol", err: McpErrorProtocol("bad frame"), want: "protocol error: bad frame"},
		{name: "timeout", err: McpErrorTimeout(7), want: "request timed out after 7s"},
		{name: "not initialized", err: McpErrorNotInitialized, want: "client is not initialized; call `initialize` before issuing requests"},
		{name: "cancelled", err: McpErrorCancelled, want: "request cancelled before the server responded"},
		{name: "other", err: McpErrorOther("boom"), want: "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.want {
				t.Fatalf("error mismatch: got %q want %q", tt.err.Error(), tt.want)
			}
		})
	}
	if !errors.Is(McpErrorNotInitialized, ErrNotInitialized) || !errors.Is(McpErrorCancelled, ErrCancelled) {
		t.Fatalf("sentinel variants should preserve errors.Is behavior")
	}
}

func TestMCPWireJSONDoesNotHTMLEscapeLikeUpstreamSerde(t *testing.T) {
	request, err := MarshalRequest(1, "tools/call", ToolsCallParams{Name: "echo", Arguments: map[string]any{"text": "<tag>&value"}, ArgumentsPresent: true})
	if err != nil {
		t.Fatal(err)
	}
	notification, err := MarshalNotification("notifications/progress", map[string]any{"text": "<tag>&value"})
	if err != nil {
		t.Fatal(err)
	}
	combined := request + notification
	if strings.Contains(combined, `\u003c`) || strings.Contains(combined, `\u003e`) || strings.Contains(combined, `\u0026`) {
		t.Fatalf("MCP wire JSON should not HTML-escape like upstream serde_json:\nrequest=%s\nnotification=%s", request, notification)
	}
	if !strings.Contains(request, `"text":"<tag>&value"`) {
		t.Fatalf("request missing unescaped argument: %s", request)
	}
	if !strings.Contains(notification, `"text":"<tag>&value"`) {
		t.Fatalf("notification missing unescaped params: %s", notification)
	}
}

func (transport *blockedSendTransport) SendLine(ctx context.Context, line string) error {
	transport.once.Do(func() { close(transport.sendStarted) })
	select {
	case <-transport.sendRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-transport.closed:
		return io.EOF
	}
}

func (transport *blockedSendTransport) RecvLine(ctx context.Context) (string, bool, error) {
	select {
	case <-ctx.Done():
		return "", false, ctx.Err()
	case <-transport.closed:
		return "", false, nil
	}
}

func (transport *blockedSendTransport) Close() error {
	select {
	case <-transport.closed:
	default:
		close(transport.closed)
	}
	return nil
}

func (transport *blockedSendTransport) releaseSend() {
	close(transport.sendRelease)
}

func (transport *observingRecvTransport) SendLine(ctx context.Context, line string) error { return nil }

func (transport *observingRecvTransport) RecvLine(ctx context.Context) (string, bool, error) {
	transport.once.Do(func() { close(transport.recvStarted) })
	select {
	case <-ctx.Done():
		return "", false, ctx.Err()
	case <-transport.closed:
		return "", false, nil
	}
}

func (transport *observingRecvTransport) Close() error {
	select {
	case <-transport.closed:
	default:
		close(transport.closed)
	}
	return nil
}

func newPipePair() (*pipeTransport, *pipeTransport) {
	aToB := make(chan string, 32)
	bToA := make(chan string, 32)
	return &pipeTransport{in: bToA, out: aToB, closed: make(chan struct{})}, &pipeTransport{in: aToB, out: bToA, closed: make(chan struct{})}
}

func (transport *pipeTransport) SendLine(ctx context.Context, line string) error {
	select {
	case transport.out <- line:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-transport.closed:
		return io.EOF
	}
}

func (transport *pipeTransport) RecvLine(ctx context.Context) (string, bool, error) {
	select {
	case line := <-transport.in:
		return line, true, nil
	case <-ctx.Done():
		return "", false, ctx.Err()
	case <-transport.closed:
		return "", false, nil
	}
}

func (transport *pipeTransport) Close() error {
	select {
	case <-transport.closed:
	default:
		close(transport.closed)
	}
	return nil
}

func TestClientInitializeListCallAndNotifications(t *testing.T) {
	clientSide, serverSide := newPipePair()
	server := newMockServer(t, serverSide)
	go server.serve()

	client := NewClient(clientSide).WithTimeout(time.Second)
	init, err := client.Initialize(context.Background(), "pig-test")
	if err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != ProtocolVersion || init.ServerInfo.Name != "mock" || !client.IsInitialized() {
		t.Fatalf("init mismatch: %#v initialized=%v", init, client.IsInitialized())
	}
	tools, err := client.ToolsList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" || len(client.Catalog()) != 1 {
		t.Fatalf("tools mismatch: %#v", tools)
	}
	result, err := client.ToolsCall(context.Background(), "echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hi" || result.IsError {
		t.Fatalf("call mismatch: %#v", result)
	}
	server.sendNotification("notifications/tools/listChanged", map[string]any{"reason": "test"})
	notification, ok := client.TakeNotification(context.Background())
	if !ok || notification.Method != "notifications/tools/listChanged" {
		t.Fatalf("notification mismatch: %#v ok=%v", notification, ok)
	}
}

func TestClientInitializeUsesPackageVersionLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)

	done := make(chan error, 1)
	go func() {
		_, err := client.Initialize(context.Background(), "pig-test")
		done <- err
	}()

	line, ok, err := serverSide.RecvLine(context.Background())
	if err != nil || !ok {
		t.Fatalf("expected initialize request, ok=%v err=%v", ok, err)
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(line), &request); err != nil {
		t.Fatal(err)
	}
	params := request["params"].(map[string]any)
	clientInfo := params["clientInfo"].(map[string]any)
	if clientInfo["version"] != "0.75.0" {
		t.Fatalf("initialize client version should match upstream package version, got %#v", clientInfo)
	}
	responseLine, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "mock", "version": "1.0"}}})
	if err := serverSide.SendLine(context.Background(), string(responseLine)); err != nil {
		t.Fatal(err)
	}
	line, ok, err = serverSide.RecvLine(context.Background())
	if err != nil || !ok {
		t.Fatalf("expected initialized notification, ok=%v err=%v", ok, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestClientRequiresInitializeAndSurfacesServerErrors(t *testing.T) {
	clientSide, serverSide := newPipePair()
	server := newMockServer(t, serverSide)
	go server.serve()
	client := NewClient(clientSide).WithTimeout(time.Second)
	if _, err := client.ToolsList(context.Background()); !IsNotInitialized(err) || err.Error() != "client is not initialized; call `initialize` before issuing requests" {
		t.Fatalf("expected not initialized, got %v", err)
	}
	if _, err := client.Initialize(context.Background(), "pig-test"); err != nil {
		t.Fatal(err)
	}
	_, err := client.ToolsCall(context.Background(), "missing", nil)
	if !IsServerError(err) {
		t.Fatalf("expected server error, got %v", err)
	}
}

func TestClientMalformedErrorFrameMatchesUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(20 * time.Millisecond)
	client.initialized.Store(true)

	done := make(chan error, 1)
	go func() {
		_, err := client.ToolsCall(context.Background(), "echo", map[string]any{"text": "bad"})
		done <- err
	}()
	requestLine, ok, err := serverSide.RecvLine(context.Background())
	if err != nil || !ok {
		t.Fatalf("missing request line ok=%v err=%v", ok, err)
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(requestLine), &request); err != nil {
		t.Fatal(err)
	}
	response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request["id"], "error": "not an object"})
	if err := serverSide.SendLine(context.Background(), string(response)); err != nil {
		t.Fatal(err)
	}
	err = <-done
	var serverErr ServerError
	if !errors.As(err, &serverErr) || serverErr.Code != -32603 || serverErr.Message != "malformed error frame" {
		t.Fatalf("malformed error should surface like upstream, got %#v (%v)", err, err)
	}
}

func TestClientResultNullIsDecodedAsResultLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(20 * time.Millisecond)
	client.initialized.Store(true)

	done := make(chan error, 1)
	go func() {
		_, err := client.ToolsList(context.Background())
		done <- err
	}()
	requestLine, ok, err := serverSide.RecvLine(context.Background())
	if err != nil || !ok {
		t.Fatalf("missing request line ok=%v err=%v", ok, err)
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(requestLine), &request); err != nil {
		t.Fatal(err)
	}
	response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": nil})
	if err := serverSide.SendLine(context.Background(), string(response)); err != nil {
		t.Fatal(err)
	}
	err = <-done
	var protocolErr ProtocolError
	if !errors.As(err, &protocolErr) || strings.Contains(protocolErr.Message, "neither result nor error") {
		t.Fatalf("result:null should decode as result and fail target type like upstream, got %#v (%v)", err, err)
	}
}

func TestClientHandlesConcurrentRequestsWithOutOfOrderResponses(t *testing.T) {
	clientSide, serverSide := newPipePair()
	server := newConcurrentMockServer(t, serverSide)
	go server.serve()

	client := NewClient(clientSide).WithTimeout(time.Second)
	if _, err := client.Initialize(context.Background(), "pig-test"); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	wait.Add(2)
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for _, text := range []string{"first", "second"} {
		text := text
		go func() {
			defer wait.Done()
			result, err := client.ToolsCall(context.Background(), "echo", map[string]any{"text": text})
			if err != nil {
				errs <- err
				return
			}
			if len(result.Content) != 1 {
				errs <- io.ErrUnexpectedEOF
				return
			}
			results <- result.Content[0].Text
		}()
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("unexpected call error: %v", err)
	}
	seen := map[string]bool{}
	for result := range results {
		seen[result] = true
	}
	if !seen["first"] || !seen["second"] {
		t.Fatalf("missing results: %#v", seen)
	}
}

func TestClientKeepsNotificationsWhileRequestsAreInFlight(t *testing.T) {
	clientSide, serverSide := newPipePair()
	server := newConcurrentMockServer(t, serverSide)
	go server.serve()

	client := NewClient(clientSide).WithTimeout(time.Second)
	if _, err := client.Initialize(context.Background(), "pig-test"); err != nil {
		t.Fatal(err)
	}
	result, err := client.ToolsCall(context.Background(), "echo", map[string]any{"text": "notice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "notice" {
		t.Fatalf("call mismatch: %#v", result)
	}
	notification, ok := client.TakeNotification(context.Background())
	if !ok || notification.Method != "notifications/progress" {
		t.Fatalf("notification mismatch: %#v ok=%v", notification, ok)
	}
}

func TestClientNewStartsReadPumpLikeUpstream(t *testing.T) {
	transport := newObservingRecvTransport()
	client := NewClient(transport)
	defer client.Close()
	select {
	case <-transport.recvStarted:
	case <-time.After(time.Second):
		t.Fatal("NewClient should start read pump immediately like upstream")
	}
}

func TestClientNotificationMissingParamsDefaultsToJSONNullLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/ping"})
	if err := serverSide.SendLine(context.Background(), string(line)); err != nil {
		t.Fatal(err)
	}
	notification, ok := client.TakeNotification(context.Background())
	if !ok || notification.Method != "notifications/ping" {
		t.Fatalf("notification mismatch: %#v ok=%v", notification, ok)
	}
	if notification.Params != nil {
		t.Fatalf("missing params should surface as JSON null/nil like upstream, got %#v", notification.Params)
	}
	data, err := json.Marshal(notification.Params)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "null" {
		t.Fatalf("missing params should remarshal as null like upstream, got %s", data)
	}
}

func TestClientNotificationParamsPreserveSomeNullLikeUpstreamOption(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	line := `{"jsonrpc":"2.0","method":"notifications/null","params":null}`
	if err := serverSide.SendLine(context.Background(), line); err != nil {
		t.Fatal(err)
	}
	notification, ok := client.TakeNotification(context.Background())
	if !ok || notification.Method != "notifications/null" {
		t.Fatalf("notification mismatch: %#v ok=%v", notification, ok)
	}
	if notification.Params != nil {
		t.Fatalf("params:null should surface as JSON null/nil like upstream, got %#v", notification.Params)
	}
}

func TestClientNotificationParamsPreserveJSONValueLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	line := `{"jsonrpc":"2.0","method":"notifications/big","params":{"id":9007199254740993}}`
	if err := serverSide.SendLine(context.Background(), line); err != nil {
		t.Fatal(err)
	}
	notification, ok := client.TakeNotification(context.Background())
	if !ok || notification.Method != "notifications/big" {
		t.Fatalf("notification mismatch: %#v ok=%v", notification, ok)
	}
	data, err := json.Marshal(notification.Params)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"id":9007199254740993}` {
		t.Fatalf("notification params should preserve JSON value like upstream, got %s", data)
	}
}

func TestClientTakeNotificationsCanOnlyBeTakenOnceLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/once", "params": map[string]any{"ok": true}})
	if err := serverSide.SendLine(context.Background(), string(line)); err != nil {
		t.Fatal(err)
	}
	notifications, ok := client.TakeNotifications()
	if !ok {
		t.Fatal("expected first TakeNotifications to succeed")
	}
	if _, ok := client.TakeNotifications(); ok {
		t.Fatal("second TakeNotifications should fail like upstream take_notifications")
	}
	select {
	case notification := <-notifications:
		if notification.Method != "notifications/once" {
			t.Fatalf("notification mismatch: %#v", notification)
		}
	case <-time.After(time.Second):
		t.Fatal("missing buffered notification")
	}
}

func TestClientTakeNotificationsChannelClosesOnClientCloseLikeUpstream(t *testing.T) {
	clientSide, _ := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	notifications, ok := client.TakeNotifications()
	if !ok {
		t.Fatal("expected first TakeNotifications to succeed")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-notifications:
		if ok {
			t.Fatal("notification channel should be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("notification channel did not close")
	}
}

func TestClientTakeNotificationReturnsFalseAfterCloseLikeUpstream(t *testing.T) {
	clientSide, _ := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	notification, ok := client.TakeNotification(ctx)
	if ok || notification.Method != "" {
		t.Fatalf("TakeNotification after close should return false, got %#v ok=%v", notification, ok)
	}
}

func TestClientTakeNotificationPrefersBufferedNotificationOverClosedSignal(t *testing.T) {
	clientSide, _ := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	client.enqueueNotification(rpcResponse{Method: "notifications/prefer", Params: json.RawMessage(`{"ok":true}`)})
	time.Sleep(10 * time.Millisecond)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for index := 0; index < 100; index++ {
		notification, ok := client.TakeNotification(ctx)
		if ok && notification.Method == "notifications/prefer" {
			return
		}
		if !ok {
			t.Fatalf("TakeNotification should prefer buffered notification over close, attempt=%d got %#v ok=%v", index, notification, ok)
		}
	}
	t.Fatal("buffered notification was not returned")
}

func TestClientTakeNotificationsDrainsQueuedNotificationAfterCloseLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	notifications, ok := client.TakeNotifications()
	if !ok {
		t.Fatal("expected first TakeNotifications to succeed")
	}
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/drain", "params": map[string]any{"ok": true}})
	if err := serverSide.SendLine(context.Background(), string(line)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var notification ServerNotification
	select {
	case notification = <-notifications:
	case <-ctx.Done():
		t.Fatal("notification was not delivered")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if notification.Method != "notifications/drain" {
		t.Fatalf("queued notification should drain after close: %#v", notification)
	}
}

func TestClientTakeNotificationsDrainsBufferedNotificationAfterCloseLikeUpstream(t *testing.T) {
	clientSide, _ := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	notifications, ok := client.TakeNotifications()
	if !ok {
		t.Fatal("expected first TakeNotifications to succeed")
	}
	client.enqueueNotification(rpcResponse{Method: "notifications/drain-buffer", Params: json.RawMessage(`{"ok":true}`)})
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case notification, ok := <-notifications:
		if !ok || notification.Method != "notifications/drain-buffer" {
			t.Fatalf("buffered notification should drain after close: %#v ok=%v", notification, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("buffered notification did not drain after close")
	}
	select {
	case _, ok := <-notifications:
		if ok {
			t.Fatal("notification channel should close after draining buffered notification")
		}
	case <-time.After(time.Second):
		t.Fatal("notification channel did not close after draining")
	}
}

func TestClientCloseWithBufferedNotificationAndNoConsumerDoesNotBlockLaterTakeNotifications(t *testing.T) {
	clientSide, _ := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	client.enqueueNotification(rpcResponse{Method: "notifications/later", Params: json.RawMessage(`{"ok":true}`)})
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	notifications, ok := client.TakeNotifications()
	if !ok {
		t.Fatal("expected first TakeNotifications to succeed")
	}
	select {
	case notification, ok := <-notifications:
		if !ok || notification.Method != "notifications/later" {
			t.Fatalf("buffered notification mismatch: %#v ok=%v", notification, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("buffered notification did not drain")
	}
	select {
	case _, ok := <-notifications:
		if ok {
			t.Fatal("notification channel should close after drain")
		}
	case <-time.After(time.Second):
		t.Fatal("notification channel did not close after drain")
	}
}

func TestClientNotificationDispatcherDoesNotBlockBeforeTakeNotifications(t *testing.T) {
	clientSide, _ := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	client.enqueueNotification(rpcResponse{Method: "notifications/buffered", Params: json.RawMessage(`{"ok":true}`)})
	time.Sleep(10 * time.Millisecond)
	notifications, ok := client.TakeNotifications()
	if !ok {
		t.Fatal("expected first TakeNotifications to succeed")
	}
	select {
	case notification, ok := <-notifications:
		if !ok || notification.Method != "notifications/buffered" {
			t.Fatalf("buffered notification mismatch: %#v ok=%v", notification, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher blocked before TakeNotifications")
	}
	client.Close()
}

func TestClientNotificationsAreNotDroppedLikeUpstreamUnboundedChannel(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	defer client.Close()

	for index := 0; index < 128; index++ {
		line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress", "params": map[string]any{"index": index}})
		if err := serverSide.SendLine(context.Background(), string(line)); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for index := 0; index < 128; index++ {
		notification, ok := client.TakeNotification(ctx)
		if !ok || notification.Method != "notifications/progress" {
			t.Fatalf("missing notification %d: %#v ok=%v", index, notification, ok)
		}
	}
}

func TestClientNotificationPumpDoesNotBlockWhenNotificationsAreNotConsumedLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	defer client.Close()

	client.addInflight(1, make(chan rpcResponse, 1))
	defer client.removeInflight(1)
	sendDone := make(chan error, 1)
	go func() {
		for index := 0; index < 1100; index++ {
			line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/progress", "params": map[string]any{"index": index}})
			if err := serverSide.SendLine(context.Background(), string(line)); err != nil {
				sendDone <- err
				return
			}
		}
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"ok": true}})
		sendDone <- serverSide.SendLine(context.Background(), string(response))
	}()
	select {
	case got := <-client.inflight[1]:
		if string(got.Result) != `{"ok":true}` {
			t.Fatalf("response mismatch: %#v", got)
		}
	case err := <-sendDone:
		if err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-client.inflight[1]:
			if string(got.Result) != `{"ok":true}` {
				t.Fatalf("response mismatch: %#v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("notification pump blocked before delivering later response")
		}
	case <-time.After(time.Second):
		t.Fatal("notification pump blocked before delivering later response")
	}
}

func TestClientToolsCallSendsCancelledNotificationLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	initDone := make(chan struct{})
	go func() {
		defer close(initDone)
		line, ok, err := serverSide.RecvLine(context.Background())
		if err != nil || !ok {
			return
		}
		var request map[string]any
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			return
		}
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "mock", "version": "1.0"}}})
		_ = serverSide.SendLine(context.Background(), string(response))
		_, _, _ = serverSide.RecvLine(context.Background())
	}()
	if _, err := client.Initialize(context.Background(), "pig-test"); err != nil {
		t.Fatal(err)
	}
	<-initDone
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.ToolsCall(ctx, "echo", map[string]any{"text": "cancel"})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected cancelled error, got %v", err)
	}
	requestLine, ok, err := serverSide.RecvLine(context.Background())
	if err != nil || !ok {
		t.Fatalf("missing tools/call request line ok=%v err=%v", ok, err)
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(requestLine), &request); err != nil {
		t.Fatal(err)
	}
	if request["method"] != "tools/call" || request["id"] == nil {
		t.Fatalf("unexpected tools/call request: %s", requestLine)
	}
	line, ok, err := serverSide.RecvLine(context.Background())
	if err != nil || !ok {
		t.Fatalf("missing cancelled notification line ok=%v err=%v", ok, err)
	}
	var notification map[string]any
	if err := json.Unmarshal([]byte(line), &notification); err != nil {
		t.Fatal(err)
	}
	if notification["method"] != "notifications/cancelled" {
		t.Fatalf("unexpected notification: %s", line)
	}
	params, ok := notification["params"].(map[string]any)
	if !ok || params["requestId"] == nil {
		t.Fatalf("cancelled params mismatch: %s", line)
	}
	if _, ok := params["reason"]; ok {
		t.Fatalf("cancelled notification should omit reason like upstream: %s", line)
	}
}

func TestClientToolsCallSendIgnoresCancellationLikeUpstream(t *testing.T) {
	transport := newBlockedSendTransport()
	client := NewClient(transport).WithTimeout(time.Second)
	client.initialized.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.ToolsCall(ctx, "echo", map[string]any{"text": "cancel"})
		done <- err
	}()

	<-transport.sendStarted
	cancel()
	select {
	case err := <-done:
		t.Fatalf("request returned before send completed, unlike upstream: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	transport.releaseSend()
	if err := <-done; !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected cancelled after send completed, got %v", err)
	}
}

func TestClientToolsCallPrefersReadyResponseOverCancellationLikeUpstream(t *testing.T) {
	resultJSON := json.RawMessage(`{"content":[{"type":"text","text":"ok"}],"isError":false}`)
	for attempt := 0; attempt < 100; attempt++ {
		transport := newBlockedSendTransport()
		client := NewClient(transport).WithTimeout(time.Second)
		client.initialized.Store(true)

		ctx, cancel := context.WithCancel(context.Background())
		resultCh := make(chan ToolCallResult, 1)
		errCh := make(chan error, 1)
		go func() {
			var result ToolCallResult
			err := client.request(ctx, "tools/call", ToolsCallParams{Name: "echo", Arguments: map[string]any{"text": "ok"}}, &result, true)
			resultCh <- result
			errCh <- err
		}()
		<-transport.sendStarted
		client.inflightMu.Lock()
		responseCh := client.inflight[1]
		client.inflightMu.Unlock()
		if responseCh == nil {
			t.Fatalf("missing inflight channel on attempt %d", attempt)
		}
		responseCh <- rpcResponse{Result: resultJSON}
		cancel()
		transport.releaseSend()
		result := <-resultCh
		err := <-errCh
		if err != nil {
			t.Fatalf("ready response should win over cancellation like upstream on attempt %d, got %v", attempt, err)
		}
		if len(result.Content) != 1 || result.Content[0].Text != "ok" {
			t.Fatalf("response mismatch on attempt %d: %#v", attempt, result)
		}
	}
}

func TestClientLateResponseAfterTimeoutIsDiscardedLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Millisecond)
	client.initialized.Store(true)

	_, err := client.ToolsCall(context.Background(), "echo", map[string]any{"text": "slow"})
	var timeoutErr TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected timeout error, got %v", err)
	}
	requestLine, ok, err := serverSide.RecvLine(context.Background())
	if err != nil || !ok {
		t.Fatalf("missing request line ok=%v err=%v", ok, err)
	}
	var request map[string]any
	if err := json.Unmarshal([]byte(requestLine), &request); err != nil {
		t.Fatal(err)
	}
	response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": map[string]any{"content": []map[string]any{{"type": "text", "text": "late"}}, "isError": false}})
	if err := serverSide.SendLine(context.Background(), string(response)); err != nil {
		t.Fatal(err)
	}
	notificationCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if notification, ok := client.TakeNotification(notificationCtx); ok {
		t.Fatalf("late response should be discarded, not delivered as notification: %#v", notification)
	}
}

func TestClientTransportCloseDrainsInflightLikeUpstream(t *testing.T) {
	clientSide, serverSide := newPipePair()
	client := NewClient(clientSide).WithTimeout(time.Second)
	client.initialized.Store(true)

	done := make(chan error, 1)
	go func() {
		_, err := client.ToolsCall(context.Background(), "echo", map[string]any{"text": "close"})
		done <- err
	}()
	if _, ok, err := serverSide.RecvLine(context.Background()); err != nil || !ok {
		t.Fatalf("missing request line ok=%v err=%v", ok, err)
	}
	clientSide.Close()
	err := <-done
	var serverErr ServerError
	if !errors.As(err, &serverErr) || serverErr.Code != -32000 || serverErr.Message != "transport closed" {
		t.Fatalf("transport close should drain inflight like upstream, got %#v (%v)", err, err)
	}
}

func TestClientUsesSingleReceivePumpForConcurrentRequests(t *testing.T) {
	transport := newSingleReaderTransport()
	client := NewClient(transport).WithTimeout(time.Second)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		serverSingleReaderTransport(t, transport)
	}()
	if _, err := client.Initialize(context.Background(), "pig-test"); err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	wait.Add(2)
	errs := make(chan error, 2)
	for _, text := range []string{"first", "second"} {
		text := text
		go func() {
			defer wait.Done()
			_, err := client.ToolsCall(context.Background(), "echo", map[string]any{"text": text})
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent call failed: %v", err)
		}
	}
	transport.Close()
	<-serverDone
}

func TestStdioTransportLineIO(t *testing.T) {
	readerToTransport, writerToTransport := io.Pipe()
	readerFromTransport, writerFromTransport := io.Pipe()
	transport := NewStdioTransport(readerToTransport, writerFromTransport, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { _, _ = writerToTransport.Write([]byte("hello\n")) }()
	line, ok, err := transport.RecvLine(ctx)
	if err != nil || !ok || line != "hello" {
		t.Fatalf("recv mismatch line=%q ok=%v err=%v", line, ok, err)
	}
	gotCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		got, err := bufio.NewReader(readerFromTransport).ReadString('\n')
		gotCh <- got
		errCh <- err
	}()
	if err := transport.SendLine(ctx, "world"); err != nil {
		t.Fatal(err)
	}
	got := <-gotCh
	err = <-errCh
	if err != nil || got != "world\n" {
		t.Fatalf("send mismatch got=%q err=%v", got, err)
	}
}

type mockServer struct {
	t         *testing.T
	transport *pipeTransport
	mu        sync.Mutex
}

type concurrentMockServer struct {
	t         *testing.T
	transport *pipeTransport
}

func newConcurrentMockServer(t *testing.T, transport *pipeTransport) *concurrentMockServer {
	return &concurrentMockServer{t: t, transport: transport}
}

func (server *concurrentMockServer) serve() {
	pending := make([]map[string]any, 0, 2)
	for {
		line, ok, err := server.transport.RecvLine(context.Background())
		if err != nil || !ok {
			return
		}
		var request map[string]any
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			server.t.Errorf("bad json: %v", err)
			return
		}
		if _, hasID := request["id"]; !hasID {
			continue
		}
		switch request["method"] {
		case "initialize":
			server.respond(request["id"], map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "mock", "version": "1.0"}})
		case "tools/call":
			server.sendNotification("notifications/progress", map[string]any{"request": request["id"]})
			pending = append(pending, request)
			if len(pending) == 1 {
				go server.respondAfterDelay(request, 10*time.Millisecond)
				continue
			}
			for i := len(pending) - 1; i >= 0; i-- {
				server.respondEcho(pending[i])
			}
			pending = pending[:0]
		}
	}
}

func (server *concurrentMockServer) respondAfterDelay(request map[string]any, delay time.Duration) {
	time.Sleep(delay)
	server.respondEcho(request)
}

func (server *concurrentMockServer) respondEcho(request map[string]any) {
	params := request["params"].(map[string]any)
	args, _ := params["arguments"].(map[string]any)
	server.respond(request["id"], map[string]any{"content": []map[string]any{{"type": "text", "text": args["text"]}}, "isError": false})
}

func (server *concurrentMockServer) respond(id any, result any) {
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_ = server.transport.SendLine(context.Background(), string(line))
}

func (server *concurrentMockServer) sendNotification(method string, params any) {
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	_ = server.transport.SendLine(context.Background(), string(line))
}

type singleReaderTransport struct {
	clientIn chan string
	serverIn chan string
	closed   chan struct{}
	active   int32
}

func newSingleReaderTransport() *singleReaderTransport {
	return &singleReaderTransport{clientIn: make(chan string, 32), serverIn: make(chan string, 32), closed: make(chan struct{})}
}

func (transport *singleReaderTransport) SendLine(ctx context.Context, line string) error {
	select {
	case transport.serverIn <- line:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-transport.closed:
		return io.EOF
	}
}

func (transport *singleReaderTransport) RecvLine(ctx context.Context) (string, bool, error) {
	if !atomic.CompareAndSwapInt32(&transport.active, 0, 1) {
		return "", false, errConcurrentRecv
	}
	defer atomic.StoreInt32(&transport.active, 0)
	select {
	case line := <-transport.clientIn:
		return line, true, nil
	case <-ctx.Done():
		return "", false, ctx.Err()
	case <-transport.closed:
		return "", false, nil
	}
}

func (transport *singleReaderTransport) Close() error {
	select {
	case <-transport.closed:
	default:
		close(transport.closed)
	}
	return nil
}

var errConcurrentRecv = errors.New("concurrent recv")

func serverSingleReaderTransport(t *testing.T, transport *singleReaderTransport) {
	pending := make([]map[string]any, 0, 2)
	for {
		select {
		case line := <-transport.serverIn:
			var request map[string]any
			if err := json.Unmarshal([]byte(line), &request); err != nil {
				t.Errorf("bad json: %v", err)
				return
			}
			if _, hasID := request["id"]; !hasID {
				continue
			}
			switch request["method"] {
			case "initialize":
				respondSingleReaderTransport(transport, request["id"], map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "mock", "version": "1.0"}})
			case "tools/call":
				pending = append(pending, request)
				if len(pending) < 2 {
					continue
				}
				respondSingleReaderTransportEcho(transport, pending[1])
				respondSingleReaderTransportEcho(transport, pending[0])
			}
		case <-transport.closed:
			return
		}
	}
}

func respondSingleReaderTransportEcho(transport *singleReaderTransport, request map[string]any) {
	params := request["params"].(map[string]any)
	args, _ := params["arguments"].(map[string]any)
	respondSingleReaderTransport(transport, request["id"], map[string]any{"content": []map[string]any{{"type": "text", "text": args["text"]}}, "isError": false})
}

func respondSingleReaderTransport(transport *singleReaderTransport, id any, result any) {
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	transport.clientIn <- string(line)
}

func newMockServer(t *testing.T, transport *pipeTransport) *mockServer {
	return &mockServer{t: t, transport: transport}
}

func (server *mockServer) serve() {
	for {
		line, ok, err := server.transport.RecvLine(context.Background())
		if err != nil || !ok {
			return
		}
		var request map[string]any
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			server.t.Errorf("bad json: %v", err)
			return
		}
		if _, hasID := request["id"]; !hasID {
			continue
		}
		id := request["id"]
		switch request["method"] {
		case "initialize":
			server.respond(id, map[string]any{"protocolVersion": ProtocolVersion, "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "mock", "version": "1.0"}})
		case "tools/list":
			server.respond(id, map[string]any{"tools": []map[string]any{{"name": "echo", "description": "Echo text", "inputSchema": map[string]any{"type": "object"}}}})
		case "tools/call":
			params := request["params"].(map[string]any)
			if params["name"] != "echo" {
				server.respondError(id, -32602, "unknown tool")
				continue
			}
			args, _ := params["arguments"].(map[string]any)
			server.respond(id, map[string]any{"content": []map[string]any{{"type": "text", "text": args["text"]}}, "isError": false})
		}
	}
}

func (server *mockServer) respond(id any, result any) {
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_ = server.transport.SendLine(context.Background(), string(line))
}

func (server *mockServer) respondError(id any, code int, message string) {
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
	_ = server.transport.SendLine(context.Background(), string(line))
}

func (server *mockServer) sendNotification(method string, params any) {
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	_ = server.transport.SendLine(context.Background(), string(line))
}

func TestMakeRequestJSON(t *testing.T) {
	line, err := MarshalRequest(7, "tools/list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, `"jsonrpc":"2.0"`) || !strings.Contains(line, `"id":7`) {
		t.Fatalf("request json mismatch: %s", line)
	}
}

func TestMakeNotificationJSON(t *testing.T) {
	line, err := MarshalNotification("notifications/cancelled", CancelledNotificationParams{RequestID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, `"jsonrpc":"2.0"`) || !strings.Contains(line, `"method":"notifications/cancelled"`) || strings.Contains(line, `"id"`) {
		t.Fatalf("notification json mismatch: %s", line)
	}
}

func TestMCPUpstreamProtocolAliases(t *testing.T) {
	var capabilities ClientCapabilitiesSpec = ClientCapabilitiesSpec{Roots: map[string]any{}}
	var tool McpTool = Tool{Name: "read"}
	var result McpToolCallResult = ToolCallResult{Content: []ToolContent{{Type: "text", Text: "ok"}}}
	var client *McpClient = NewClient(&pipeTransport{in: make(chan string), out: make(chan string), closed: make(chan struct{})})
	var clientCapabilities ClientCapabilities
	var notification McpServerNotification = ServerNotification{Method: "notifications/ping"}
	var rpcError RpcError = RPCError{Code: -32000, Message: "bad"}
	_ = clientCapabilities
	if capabilities.Roots == nil || tool.Name != "read" || len(result.Content) != 1 || client == nil || notification.Method == "" || rpcError.Code != -32000 {
		t.Fatalf("protocol aliases mismatch: %#v %#v %#v %#v %#v %#v %#v", capabilities, tool, result, client, clientCapabilities, notification, rpcError)
	}
}

func TestMCPUpstreamRPCEnvelopeTypes(t *testing.T) {
	requestData, err := json.Marshal(RpcRequest{JSONRPC: "2.0", ID: 7, Method: "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	if string(requestData) != `{"jsonrpc":"2.0","id":7,"method":"tools/list"}` {
		t.Fatalf("rpc request mismatch: %s", requestData)
	}

	notificationData, err := json.Marshal(RpcNotification{JSONRPC: "2.0", Method: "notifications/ping"})
	if err != nil {
		t.Fatal(err)
	}
	if string(notificationData) != `{"jsonrpc":"2.0","method":"notifications/ping"}` {
		t.Fatalf("rpc notification mismatch: %s", notificationData)
	}

	var response RpcResponse
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.JSONRPC == nil || *response.JSONRPC != "2.0" || response.ID == nil || *response.ID != 7 || len(response.Result) == 0 || response.Error != nil {
		t.Fatalf("rpc response mismatch: %#v", response)
	}
}

func TestRPCResponseJSONRPCIsOptionalLikeUpstream(t *testing.T) {
	var missing RpcResponse
	if err := json.Unmarshal([]byte(`{"id":7,"result":{"ok":true}}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.JSONRPC != nil {
		t.Fatalf("missing jsonrpc should decode as nil like upstream Option<String>, got %#v", missing.JSONRPC)
	}

	var empty RpcResponse
	if err := json.Unmarshal([]byte(`{"jsonrpc":"","id":7,"result":{"ok":true}}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.JSONRPC == nil || *empty.JSONRPC != "" {
		t.Fatalf("present empty jsonrpc should remain distinguishable, got %#v", empty.JSONRPC)
	}
}

func TestMakeRequestAndNotificationReturnRPCEnvelopesLikeUpstream(t *testing.T) {
	request := MakeRequest(7, "tools/call", map[string]any{"name": "read"})
	requestData, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(requestData) != `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"read"}}` {
		t.Fatalf("make request mismatch: %s", requestData)
	}

	notification := MakeNotification("notifications/ping", nil)
	notificationData, err := json.Marshal(notification)
	if err != nil {
		t.Fatal(err)
	}
	if string(notificationData) != `{"jsonrpc":"2.0","method":"notifications/ping"}` {
		t.Fatalf("make notification mismatch: %s", notificationData)
	}
}

func TestRPCEnvelopeParamsPreserveSomeNullLikeUpstreamOption(t *testing.T) {
	requestData, err := json.Marshal(RpcRequest{JSONRPC: "2.0", ID: 7, Method: "tools/list", ParamsPresent: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(requestData) != `{"jsonrpc":"2.0","id":7,"method":"tools/list","params":null}` {
		t.Fatalf("request Some(null) params should serialize like upstream Option<P>, got %s", requestData)
	}

	notificationData, err := json.Marshal(RpcNotification{JSONRPC: "2.0", Method: "notifications/ping", ParamsPresent: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(notificationData) != `{"jsonrpc":"2.0","method":"notifications/ping","params":null}` {
		t.Fatalf("notification Some(null) params should serialize like upstream Option<P>, got %s", notificationData)
	}
}

func TestMCPUpstreamErrorAlias(t *testing.T) {
	var err McpError = ServerError{Code: -32000, Message: "bad"}
	if !IsServerError(err) || err.Error() != "server returned error -32000: bad" {
		t.Fatalf("mcp error alias mismatch: %v", err)
	}
}

func TestToolInputSchemaJSONMatchesUpstreamDefaultValue(t *testing.T) {
	data, err := json.Marshal(Tool{Name: "read"})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["inputSchema"]; !ok || object["inputSchema"] != nil {
		t.Fatalf("inputSchema should serialize as upstream null default, got %s", data)
	}
	var tool Tool
	if err := json.Unmarshal([]byte(`{"name":"read"}`), &tool); err != nil {
		t.Fatal(err)
	}
	if tool.InputSchema != nil {
		t.Fatalf("missing inputSchema should decode to nil default, got %#v", tool.InputSchema)
	}
}

func TestToolDescriptionPreservesSomeEmptyStringLikeUpstreamOption(t *testing.T) {
	description := ""
	data, err := json.Marshal(Tool{Name: "read", Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if value, ok := object["description"]; !ok || value != "" {
		t.Fatalf("Some(empty string) description should serialize like upstream Option<String>, got %s", data)
	}

	var missing Tool
	if err := json.Unmarshal([]byte(`{"name":"read","inputSchema":null}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Description != nil {
		t.Fatalf("missing description should decode to nil Option, got %#v", missing.Description)
	}
	var present Tool
	if err := json.Unmarshal([]byte(`{"name":"read","description":"","inputSchema":null}`), &present); err != nil {
		t.Fatal(err)
	}
	if present.Description == nil || *present.Description != "" {
		t.Fatalf("present empty description should decode to Some(empty), got %#v", present.Description)
	}
}

func TestToolInputSchemaPreservesArbitraryJSONValueLikeUpstream(t *testing.T) {
	var arraySchema Tool
	if err := json.Unmarshal([]byte(`{"name":"read","inputSchema":["flag",{"ticket":9007199254740993}]}`), &arraySchema); err != nil {
		t.Fatalf("array inputSchema should decode like upstream serde_json::Value: %v", err)
	}
	data, err := json.Marshal(arraySchema.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ticket":9007199254740993}` && !strings.Contains(string(data), `9007199254740993`) {
		t.Fatalf("inputSchema should preserve arbitrary JSON value like upstream, got %s", data)
	}
}

func TestToolRequiresNameLikeUpstream(t *testing.T) {
	for _, input := range []string{`{"inputSchema":{}}`, `{"name":null,"inputSchema":{}}`} {
		t.Run(input, func(t *testing.T) {
			var tool Tool
			if err := json.Unmarshal([]byte(input), &tool); err == nil {
				t.Fatalf("missing or null tool name should fail like upstream String, got %#v", tool)
			}
		})
	}
}

func TestToolsCallParamsArgumentsPreserveRawJSONNumbersLikeUpstream(t *testing.T) {
	var params ToolsCallParams
	if err := json.Unmarshal([]byte(`{"name":"read","arguments":{"ticket":9007199254740993}}`), &params); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(params.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ticket":9007199254740993}` {
		t.Fatalf("tools/call arguments should preserve JSON value like upstream, got %s", data)
	}
}

func TestToolsCallParamsArgumentsPreserveSomeNullLikeUpstreamOption(t *testing.T) {
	data, err := json.Marshal(ToolsCallParams{Name: "read", ArgumentsPresent: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"name":"read","arguments":null}` {
		t.Fatalf("Some(null) arguments should serialize like upstream Option<Value>, got %s", data)
	}

	var missing ToolsCallParams
	if err := json.Unmarshal([]byte(`{"name":"read"}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.ArgumentsPresent {
		t.Fatalf("missing arguments should decode as None, got %#v", missing)
	}
	var presentNull ToolsCallParams
	if err := json.Unmarshal([]byte(`{"name":"read","arguments":null}`), &presentNull); err != nil {
		t.Fatal(err)
	}
	if !presentNull.ArgumentsPresent || presentNull.Arguments != nil {
		t.Fatalf("arguments:null should decode as Some(null), got %#v", presentNull)
	}
}

func TestToolsCallParamsNilArgumentsOmitsFieldLikeUpstreamNone(t *testing.T) {
	data, err := json.Marshal(ToolsCallParams{Name: "read"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"name":"read"}` {
		t.Fatalf("nil arguments without presence should serialize as None, got %s", data)
	}
}

func TestToolsCallParamsRequiresNameLikeUpstream(t *testing.T) {
	for _, input := range []string{`{"arguments":{}}`, `{"name":null}`} {
		t.Run(input, func(t *testing.T) {
			var params ToolsCallParams
			if err := json.Unmarshal([]byte(input), &params); err == nil {
				t.Fatalf("missing or null name should fail like upstream String, got %#v", params)
			}
		})
	}
}

func TestRPCErrorDataPreservesRawJSONNumbersLikeUpstream(t *testing.T) {
	var response rpcResponse
	if err := json.Unmarshal([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"bad","data":{"ticket":9007199254740993}}}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil {
		t.Fatal("expected rpc error")
	}
	data, err := json.Marshal(response.Error.Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ticket":9007199254740993}` {
		t.Fatalf("rpc error data should preserve JSON value like upstream, got %s", data)
	}
}

func TestRPCErrorRequiresCodeAndMessageLikeUpstream(t *testing.T) {
	for _, input := range []string{
		`{"code":-32000}`,
		`{"message":"bad"}`,
		`{"code":null,"message":"bad"}`,
		`{"code":-32000,"message":null}`,
	} {
		t.Run(input, func(t *testing.T) {
			var rpcError RPCError
			if err := json.Unmarshal([]byte(input), &rpcError); err == nil {
				t.Fatalf("invalid rpc error should fail like upstream RpcError, got %#v", rpcError)
			}
		})
	}
}

func TestInitializeParamsCapabilitiesDefaultsToEmptyObjectLikeUpstream(t *testing.T) {
	data, err := json.Marshal(InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      ClientInfo{Name: "pig", Version: "0.0.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	capabilities, ok := object["capabilities"].(map[string]any)
	if !ok || len(capabilities) != 0 {
		t.Fatalf("nil capabilities should serialize as empty object like upstream ClientCapabilitiesSpec, got %s", data)
	}
}

func TestInitializeParamsCapabilitiesPreserveRawJSONNumbersLikeUpstream(t *testing.T) {
	var params InitializeParams
	if err := json.Unmarshal([]byte(`{"protocolVersion":"2025-03-26","capabilities":{"roots":{"ticket":9007199254740993}},"clientInfo":{"name":"pig","version":"0.0.0"}}`), &params); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(params.Capabilities.Roots)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ticket":9007199254740993}` {
		t.Fatalf("initialize capabilities roots should preserve JSON value like upstream, got %s", data)
	}
}

func TestClientCapabilitiesSpecOnlySerializesKnownFieldsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ClientCapabilitiesSpec{Roots: map[string]any{"listChanged": true}})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"roots":{"listChanged":true}}` {
		t.Fatalf("client capabilities should serialize only known non-nil fields like upstream struct, got %s", data)
	}
}

func TestClientCapabilitiesSpecNullFieldsDecodeAsNoneLikeUpstream(t *testing.T) {
	var params InitializeParams
	if err := json.Unmarshal([]byte(`{"protocolVersion":"2025-03-26","capabilities":{"roots":null,"sampling":null},"clientInfo":{"name":"pig","version":"0.0.0"}}`), &params); err != nil {
		t.Fatal(err)
	}
	if params.Capabilities.Roots != nil || params.Capabilities.Sampling != nil {
		t.Fatalf("null capabilities fields should decode as None, got %#v", params.Capabilities)
	}
	data, err := json.Marshal(params.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{}` {
		t.Fatalf("None capabilities fields should be omitted like upstream, got %s", data)
	}
}

func TestClientCapabilitiesSpecPreservesSomeNullLikeUpstreamOption(t *testing.T) {
	data, err := json.Marshal(ClientCapabilitiesSpec{RootsPresent: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"roots":null}` {
		t.Fatalf("Some(null) roots should serialize like upstream Option<Value>, got %s", data)
	}
}

func TestInitializeParamsCapabilitiesRequiredLikeUpstream(t *testing.T) {
	for _, input := range []string{
		`{"protocolVersion":"2025-03-26","clientInfo":{"name":"pig","version":"0.0.0"}}`,
		`{"protocolVersion":"2025-03-26","capabilities":null,"clientInfo":{"name":"pig","version":"0.0.0"}}`,
	} {
		t.Run(input, func(t *testing.T) {
			var params InitializeParams
			if err := json.Unmarshal([]byte(input), &params); err == nil {
				t.Fatalf("invalid capabilities should fail like upstream ClientCapabilitiesSpec, got %#v", params)
			}
		})
	}
}

func TestInitializeParamsRequiresProtocolVersionAndClientInfoLikeUpstream(t *testing.T) {
	for _, input := range []string{
		`{"capabilities":{},"clientInfo":{"name":"pig","version":"0.0.0"}}`,
		`{"protocolVersion":null,"capabilities":{},"clientInfo":{"name":"pig","version":"0.0.0"}}`,
		`{"protocolVersion":"2025-03-26","capabilities":{}}`,
		`{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":null}`,
		`{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"version":"0.0.0"}}`,
		`{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"pig"}}`,
	} {
		t.Run(input, func(t *testing.T) {
			var params InitializeParams
			if err := json.Unmarshal([]byte(input), &params); err == nil {
				t.Fatalf("invalid initialize params should fail like upstream InitializeParams, got %#v", params)
			}
		})
	}
}

func TestInitializeResultCapabilitiesUsesJSONValueSemanticsLikeUpstream(t *testing.T) {
	var withArray InitializeResult
	if err := json.Unmarshal([]byte(`{"protocolVersion":"2025-03-26","capabilities":["x"],"serverInfo":{"name":"mock","version":"1.0"}}`), &withArray); err != nil {
		t.Fatalf("array capabilities should decode like upstream serde_json::Value: %v", err)
	}
	if capabilities, ok := withArray.Capabilities.([]any); !ok || len(capabilities) != 1 || capabilities[0] != "x" {
		t.Fatalf("array capabilities mismatch: %#v", withArray.Capabilities)
	}
	var withNumber InitializeResult
	if err := json.Unmarshal([]byte(`{"protocolVersion":"2025-03-26","capabilities":{"ticket":9007199254740993},"serverInfo":{"name":"mock","version":"1.0"}}`), &withNumber); err != nil {
		t.Fatalf("number capabilities should decode like upstream serde_json::Value: %v", err)
	}
	data, err := json.Marshal(withNumber.Capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ticket":9007199254740993}` {
		t.Fatalf("capabilities should preserve JSON value like upstream, got %s", data)
	}
	var withNull InitializeResult
	if err := json.Unmarshal([]byte(`{"protocolVersion":"2025-03-26","capabilities":null,"serverInfo":{"name":"mock","version":"1.0"}}`), &withNull); err != nil {
		t.Fatalf("null capabilities should decode like upstream serde_json::Value: %v", err)
	}
	if withNull.Capabilities != nil {
		t.Fatalf("null capabilities should decode to nil JSON value, got %#v", withNull.Capabilities)
	}
	var missing InitializeResult
	if err := json.Unmarshal([]byte(`{"protocolVersion":"2025-03-26","serverInfo":{"name":"mock","version":"1.0"}}`), &missing); err == nil {
		t.Fatalf("missing capabilities should fail like upstream required serde_json::Value, got %#v", missing)
	}
}

func TestInitializeResultRequiresServerInfoLikeUpstream(t *testing.T) {
	for _, input := range []string{
		`{"protocolVersion":"2025-03-26","capabilities":{}}`,
		`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":null}`,
		`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"version":"1.0"}}`,
		`{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"mock"}}`,
	} {
		t.Run(input, func(t *testing.T) {
			var result InitializeResult
			if err := json.Unmarshal([]byte(input), &result); err == nil {
				t.Fatalf("invalid serverInfo should fail like upstream ServerInfo, got %#v", result)
			}
		})
	}
}

func TestInitializeResultRequiresProtocolVersionLikeUpstream(t *testing.T) {
	for _, input := range []string{
		`{"capabilities":{},"serverInfo":{"name":"mock","version":"1.0"}}`,
		`{"protocolVersion":null,"capabilities":{},"serverInfo":{"name":"mock","version":"1.0"}}`,
	} {
		t.Run(input, func(t *testing.T) {
			var result InitializeResult
			if err := json.Unmarshal([]byte(input), &result); err == nil {
				t.Fatalf("invalid protocolVersion should fail like upstream InitializeResult, got %#v", result)
			}
		})
	}
}

func TestCancelledNotificationParamsOmitsEmptyReasonLikeUpstream(t *testing.T) {
	data, err := json.Marshal(CancelledNotificationParams{RequestID: 7})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["requestId"] != float64(7) {
		t.Fatalf("requestId mismatch: %s", data)
	}
	if _, ok := object["reason"]; ok {
		t.Fatalf("empty reason should be omitted like upstream Option::None, got %s", data)
	}
}

func TestCancelledNotificationParamsPreservesSomeEmptyReasonLikeUpstreamOption(t *testing.T) {
	reason := ""
	data, err := json.Marshal(CancelledNotificationParams{RequestID: 7, Reason: &reason})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if value, ok := object["reason"]; !ok || value != "" {
		t.Fatalf("Some(empty string) reason should serialize like upstream Option<String>, got %s", data)
	}
}

func TestToolsListResultToolsUsesVecSemanticsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ToolsListResult{})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	tools, ok := object["tools"].([]any)
	if !ok || len(tools) != 0 {
		t.Fatalf("nil tools should marshal as empty array like upstream Vec, got %s", data)
	}
	var result ToolsListResult
	if err := json.Unmarshal([]byte(`{"tools":null}`), &result); err == nil {
		t.Fatalf("tools:null should fail like upstream Vec, got %#v", result)
	}
}

func TestToolCallResultIncludesFalseIsErrorLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ToolCallResult{Content: []ToolContent{}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if value, ok := object["isError"]; !ok || value != false {
		t.Fatalf("isError false should serialize like upstream, got %s", data)
	}
	var missing ToolCallResult
	if err := json.Unmarshal([]byte(`{"content":[]}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.IsError {
		t.Fatalf("missing isError should default to false like upstream, got %#v", missing)
	}
}

func TestToolCallResultRejectsNullIsErrorLikeUpstream(t *testing.T) {
	var result ToolCallResult
	if err := json.Unmarshal([]byte(`{"content":[],"isError":null}`), &result); err == nil {
		t.Fatalf("isError:null should fail like upstream bool, got %#v", result)
	}
}

func TestToolCallResultContentUsesVecSemanticsLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ToolCallResult{})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	content, ok := object["content"].([]any)
	if !ok || len(content) != 0 {
		t.Fatalf("nil content should marshal as empty array like upstream Vec, got %s", data)
	}
	for _, input := range []string{`{"isError":false}`, `{"content":null,"isError":false}`} {
		t.Run(input, func(t *testing.T) {
			var result ToolCallResult
			if err := json.Unmarshal([]byte(input), &result); err == nil {
				t.Fatalf("invalid content should fail like upstream Vec, got %#v", result)
			}
		})
	}
}

func TestToolContentMarshalMatchesUpstreamTaggedEnum(t *testing.T) {
	tests := []struct {
		name    string
		content ToolContent
		fields  map[string]any
	}{
		{name: "text", content: ToolContent{Type: ToolContentText}, fields: map[string]any{"type": "text", "text": ""}},
		{name: "image", content: ToolContent{Type: ToolContentImage}, fields: map[string]any{"type": "image", "data": "", "mimeType": ""}},
		{name: "resource", content: ToolContent{Type: ToolContentResource}, fields: map[string]any{"type": "resource", "resource": nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.content)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			if err := json.Unmarshal(data, &object); err != nil {
				t.Fatal(err)
			}
			if len(object) != len(tt.fields) {
				t.Fatalf("%s content should only include active variant fields, got %s", tt.name, data)
			}
			for key, want := range tt.fields {
				if object[key] != want {
					t.Fatalf("%s field %s mismatch: got %#v want %#v in %s", tt.name, key, object[key], want, data)
				}
			}
		})
	}
}

func TestToolContentMarshalRejectsUnknownTypeLikeUpstreamTaggedEnum(t *testing.T) {
	for _, content := range []ToolContent{
		{},
		{Type: "audio", Data: "abc", MimeType: "audio/wav"},
	} {
		if data, err := json.Marshal(content); err == nil {
			t.Fatalf("unknown tool content type should fail like upstream enum, got %s", data)
		}
	}
}

func TestToolContentUnmarshalMatchesUpstreamTaggedEnum(t *testing.T) {
	missingRequired := []string{
		`{"text":"ok"}`,
		`{"type":null,"text":"ok"}`,
		`{"type":"text"}`,
		`{"type":"image","data":"abc"}`,
		`{"type":"image","mimeType":"image/png"}`,
		`{"type":"resource"}`,
	}
	for _, input := range missingRequired {
		t.Run(input, func(t *testing.T) {
			var content ToolContent
			if err := json.Unmarshal([]byte(input), &content); err == nil {
				t.Fatalf("missing required variant field should fail like upstream: %#v", content)
			}
		})
	}
	var text ToolContent
	if err := json.Unmarshal([]byte(`{"type":"text","text":"ok","data":"inactive"}`), &text); err != nil {
		t.Fatal(err)
	}
	if text.Type != "text" || text.Text != "ok" || text.Data != "" || text.MimeType != "" || text.Resource != nil {
		t.Fatalf("inactive fields should be dropped like upstream enum, got %#v", text)
	}
}

func TestToolContentResourcePreservesRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var content ToolContent
	if err := json.Unmarshal([]byte(`{"type":"resource","resource":{"ticket":9007199254740993}}`), &content); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(content.Resource)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ticket":9007199254740993}` {
		t.Fatalf("resource content should preserve JSON value like upstream, got %s", data)
	}
}
