package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const cancelNotifySendBudget = 200 * time.Millisecond

const clientVersion = "0.75.0"

type Client struct {
	transport   Transport
	nextID      atomic.Uint64
	initialized atomic.Bool
	timeout     time.Duration
	catalogMu   sync.Mutex
	catalog     []Tool
	notifyMu    sync.Mutex
	notifyCond  *sync.Cond
	notifyQueue []ServerNotification
	notifyOut   chan ServerNotification
	notifyClose sync.Once
	notifyTaken bool
	closed      chan struct{}
	pumpOnce    sync.Once
	inflightMu  sync.Mutex
	inflight    map[uint64]chan rpcResponse
}

type McpClient = Client

type ClientCapabilities = map[string]any

func NewClient(transport Transport) *Client {
	client := &Client{transport: transport, timeout: 30 * time.Second, notifyOut: make(chan ServerNotification), closed: make(chan struct{}), inflight: map[uint64]chan rpcResponse{}}
	client.notifyCond = sync.NewCond(&client.notifyMu)
	client.nextID.Store(1)
	client.startPump()
	go client.dispatchNotifications()
	return client
}

func (client *Client) WithTimeout(timeout time.Duration) *Client {
	client.timeout = timeout
	return client
}

func (client *Client) Initialize(ctx context.Context, clientName string) (InitializeResult, error) {
	params := InitializeParams{ProtocolVersion: ProtocolVersion, Capabilities: ClientCapabilitiesSpec{}, ClientInfo: ClientInfo{Name: clientName, Version: clientVersion}}
	var result InitializeResult
	if err := client.request(ctx, "initialize", params, &result, false); err != nil {
		return InitializeResult{}, err
	}
	line, err := marshalNotification("notifications/initialized", nil)
	if err != nil {
		return InitializeResult{}, err
	}
	if err := client.transport.SendLine(ctx, line); err != nil {
		return InitializeResult{}, err
	}
	client.initialized.Store(true)
	return result, nil
}

func (client *Client) IsInitialized() bool { return client.initialized.Load() }

func (client *Client) ToolsList(ctx context.Context) ([]Tool, error) {
	if !client.IsInitialized() {
		return nil, ErrNotInitialized
	}
	var result ToolsListResult
	if err := client.request(ctx, "tools/list", nil, &result, false); err != nil {
		return nil, err
	}
	client.catalogMu.Lock()
	client.catalog = append([]Tool(nil), result.Tools...)
	client.catalogMu.Unlock()
	return result.Tools, nil
}

func (client *Client) Catalog() []Tool {
	client.catalogMu.Lock()
	defer client.catalogMu.Unlock()
	return append([]Tool(nil), client.catalog...)
}

func (client *Client) ToolsCall(ctx context.Context, name string, arguments any) (ToolCallResult, error) {
	if !client.IsInitialized() {
		return ToolCallResult{}, ErrNotInitialized
	}
	params := ToolsCallParams{Name: name, Arguments: arguments}
	var result ToolCallResult
	if err := client.request(ctx, "tools/call", params, &result, true); err != nil {
		return ToolCallResult{}, err
	}
	return result, nil
}

func (client *Client) TakeNotification(ctx context.Context) (ServerNotification, bool) {
	client.startPump()
	select {
	case notification, ok := <-client.notifyOut:
		return notification, ok
	default:
	}
	select {
	case notification, ok := <-client.notifyOut:
		return notification, ok
	case <-ctx.Done():
		return ServerNotification{}, false
	case <-client.closed:
		return ServerNotification{}, false
	}
}

func (client *Client) TakeNotifications() (<-chan ServerNotification, bool) {
	client.notifyMu.Lock()
	defer client.notifyMu.Unlock()
	if client.notifyTaken {
		return nil, false
	}
	client.notifyTaken = true
	client.startPump()
	return client.notifyOut, true
}

func (client *Client) Close() error {
	client.initialized.Store(false)
	select {
	case <-client.closed:
	default:
		close(client.closed)
		client.notifyCond.Broadcast()
	}
	return client.transport.Close()
}

func (client *Client) request(ctx context.Context, method string, params any, out any, notifyCancel bool) error {
	id := client.nextID.Add(1) - 1
	line, err := MarshalRequest(id, method, params)
	if err != nil {
		return ProtocolError{Message: err.Error()}
	}
	requestCtx := ctx
	cancel := func() {}
	if client.timeout > 0 {
		requestCtx, cancel = context.WithTimeout(ctx, client.timeout)
	}
	defer cancel()
	responseCh := make(chan rpcResponse, 1)
	client.addInflight(id, responseCh)
	defer client.removeInflight(id)
	client.startPump()
	sendCtx := requestCtx
	if notifyCancel {
		sendCtx = context.Background()
		if client.timeout > 0 {
			var sendCancel context.CancelFunc
			sendCtx, sendCancel = context.WithTimeout(sendCtx, client.timeout)
			defer sendCancel()
		}
	}
	if err := client.transport.SendLine(sendCtx, line); err != nil {
		return TransportError{Message: err.Error()}
	}
	for {
		select {
		case response := <-responseCh:
			return decodeRPCResponse(response, out)
		default:
		}
		select {
		case response := <-responseCh:
			return decodeRPCResponse(response, out)
		case <-requestCtx.Done():
			if requestCtx.Err() == context.DeadlineExceeded {
				return TimeoutError{Seconds: int64(client.timeout.Seconds())}
			}
			if notifyCancel && errors.Is(requestCtx.Err(), context.Canceled) {
				client.sendCancelledNotification(id)
				return ErrCancelled
			}
			return TransportError{Message: requestCtx.Err().Error()}
		case <-client.closed:
			return TransportError{Message: "transport closed"}
		}
	}
}

func decodeRPCResponse(response rpcResponse, out any) error {
	if response.Error != nil {
		return ServerError{Code: response.Error.Code, Message: response.Error.Message}
	}
	if len(response.Result) == 0 {
		return ProtocolError{Message: "response had neither result nor error"}
	}
	if err := json.Unmarshal(response.Result, out); err != nil {
		return ProtocolError{Message: "json: " + err.Error()}
	}
	return nil
}

func (client *Client) sendCancelledNotification(id uint64) {
	line, err := marshalNotification("notifications/cancelled", CancelledNotificationParams{RequestID: id})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cancelNotifySendBudget)
	defer cancel()
	_ = client.transport.SendLine(ctx, line)
}

func (client *Client) startPump() {
	client.pumpOnce.Do(func() { go client.recvPump() })
}

func (client *Client) recvPump() {
	for {
		line, ok, err := client.transport.RecvLine(context.Background())
		if err != nil || !ok {
			client.drainInflight(RPCError{Code: -32000, Message: "transport closed"})
			client.closeInflight()
			return
		}
		var response rpcResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			continue
		}
		if response.ID == nil {
			client.enqueueNotification(response)
			continue
		}
		if response.malformedError {
			response.Error = &RPCError{Code: -32603, Message: "malformed error frame"}
		}
		client.deliverResponse(*response.ID, response)
	}
}

func (client *Client) addInflight(id uint64, responseCh chan rpcResponse) {
	client.inflightMu.Lock()
	defer client.inflightMu.Unlock()
	client.inflight[id] = responseCh
}

func (client *Client) removeInflight(id uint64) {
	client.inflightMu.Lock()
	defer client.inflightMu.Unlock()
	delete(client.inflight, id)
}

func (client *Client) deliverResponse(id uint64, response rpcResponse) {
	client.inflightMu.Lock()
	responseCh := client.inflight[id]
	client.inflightMu.Unlock()
	if responseCh == nil {
		return
	}
	select {
	case responseCh <- response:
	case <-client.closed:
	}
}

func (client *Client) closeInflight() {
	select {
	case <-client.closed:
	default:
		close(client.closed)
	}
}

func (client *Client) drainInflight(err RPCError) {
	client.inflightMu.Lock()
	pending := make([]chan rpcResponse, 0, len(client.inflight))
	for id, responseCh := range client.inflight {
		pending = append(pending, responseCh)
		delete(client.inflight, id)
	}
	client.inflightMu.Unlock()
	for _, responseCh := range pending {
		select {
		case responseCh <- rpcResponse{Error: &err}:
		default:
		}
	}
}

func (client *Client) enqueueNotification(response rpcResponse) {
	if response.Method == "" {
		return
	}
	var params any
	if len(response.Params) > 0 && string(bytes.TrimSpace(response.Params)) != "null" {
		params = append(json.RawMessage(nil), response.Params...)
	}
	client.notifyMu.Lock()
	client.notifyQueue = append(client.notifyQueue, ServerNotification{Method: response.Method, Params: params})
	client.notifyCond.Signal()
	client.notifyMu.Unlock()
}

func (client *Client) dispatchNotifications() {
	defer client.notifyClose.Do(func() { close(client.notifyOut) })
	for {
		client.notifyMu.Lock()
		for len(client.notifyQueue) == 0 {
			select {
			case <-client.closed:
				client.notifyMu.Unlock()
				return
			default:
			}
			client.notifyCond.Wait()
		}
		notification := client.notifyQueue[0]
		copy(client.notifyQueue, client.notifyQueue[1:])
		client.notifyQueue = client.notifyQueue[:len(client.notifyQueue)-1]
		client.notifyMu.Unlock()
		client.notifyOut <- notification
	}
}
