package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const ProtocolVersion = "2025-03-26"

const PROTOCOL_VERSION = ProtocolVersion

type ClientCapabilitiesSpec struct {
	Roots           any  `json:"roots,omitempty"`
	Sampling        any  `json:"sampling,omitempty"`
	RootsPresent    bool `json:"-"`
	SamplingPresent bool `json:"-"`
}

func (capabilities ClientCapabilitiesSpec) MarshalJSON() ([]byte, error) {
	object := map[string]any{}
	if capabilities.RootsPresent || capabilities.Roots != nil {
		object["roots"] = capabilities.Roots
	}
	if capabilities.SamplingPresent || capabilities.Sampling != nil {
		object["sampling"] = capabilities.Sampling
	}
	return marshalJSONNoHTMLEscape(object)
}

func (capabilities *ClientCapabilitiesSpec) UnmarshalJSON(data []byte) error {
	type wire struct {
		Roots    any `json:"roots,omitempty"`
		Sampling any `json:"sampling,omitempty"`
	}
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*capabilities = ClientCapabilitiesSpec{Roots: decoded.Roots, Sampling: decoded.Sampling}
	return nil
}

type InitializeParams struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    ClientCapabilitiesSpec `json:"capabilities"`
	ClientInfo      ClientInfo             `json:"clientInfo"`
}

func (params InitializeParams) MarshalJSON() ([]byte, error) {
	type wire InitializeParams
	return marshalJSONNoHTMLEscape(wire(params))
}

func (params *InitializeParams) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if err := requireJSONField(object, "protocolVersion"); err != nil {
		return err
	}
	if err := requireJSONField(object, "capabilities"); err != nil {
		return err
	}
	if err := requireJSONField(object, "clientInfo"); err != nil {
		return err
	}
	if err := requireJSONObjectFields(object["clientInfo"], "clientInfo", "name", "version"); err != nil {
		return err
	}
	type wire InitializeParams
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*params = InitializeParams(decoded)
	return nil
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    any        `json:"capabilities"`
	ServerInfo      ServerInfo `json:"serverInfo"`
}

func (result *InitializeResult) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if err := requireJSONField(object, "protocolVersion"); err != nil {
		return err
	}
	if _, ok := object["capabilities"]; !ok {
		return fmt.Errorf("missing required field %q", "capabilities")
	}
	if err := requireJSONField(object, "serverInfo"); err != nil {
		return err
	}
	if err := requireJSONObjectFields(object["serverInfo"], "serverInfo", "name", "version"); err != nil {
		return err
	}
	type wire InitializeResult
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*result = InitializeResult(decoded)
	return nil
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Tool struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	InputSchema any     `json:"inputSchema"`
}

type McpTool = Tool

func (tool *Tool) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if err := requireJSONField(object, "name"); err != nil {
		return err
	}
	type wire Tool
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*tool = Tool(decoded)
	return nil
}

type ToolsListResult struct {
	Tools      []Tool  `json:"tools"`
	NextCursor *string `json:"nextCursor,omitempty"`
}

func (result ToolsListResult) MarshalJSON() ([]byte, error) {
	tools := result.Tools
	if tools == nil {
		tools = []Tool{}
	}
	type wire ToolsListResult
	return marshalJSONNoHTMLEscape(struct {
		wire
		Tools []Tool `json:"tools"`
	}{wire: wire(result), Tools: tools})
}

func (result *ToolsListResult) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if err := requireJSONField(object, "tools"); err != nil {
		return err
	}
	type wire ToolsListResult
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*result = ToolsListResult(decoded)
	return nil
}

type ToolsCallParams struct {
	Name             string `json:"name"`
	Arguments        any    `json:"arguments,omitempty"`
	ArgumentsPresent bool   `json:"-"`
}

func (params ToolsCallParams) MarshalJSON() ([]byte, error) {
	type wire struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments,omitempty"`
	}
	if params.ArgumentsPresent {
		return marshalJSONNoHTMLEscape(struct {
			Name      string `json:"name"`
			Arguments any    `json:"arguments"`
		}{Name: params.Name, Arguments: params.Arguments})
	}
	return marshalJSONNoHTMLEscape(wire{Name: params.Name, Arguments: params.Arguments})
}

func (params *ToolsCallParams) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if err := requireJSONField(object, "name"); err != nil {
		return err
	}
	type wire struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments,omitempty"`
	}
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	_, hasArguments := object["arguments"]
	*params = ToolsCallParams{Name: decoded.Name, Arguments: decoded.Arguments, ArgumentsPresent: hasArguments}
	return nil
}

type CancelledNotificationParams struct {
	RequestID uint64  `json:"requestId"`
	Reason    *string `json:"reason,omitempty"`
}

type ToolCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError"`
}

type McpToolCallResult = ToolCallResult

func (result ToolCallResult) MarshalJSON() ([]byte, error) {
	content := result.Content
	if content == nil {
		content = []ToolContent{}
	}
	type wire ToolCallResult
	return marshalJSONNoHTMLEscape(struct {
		wire
		Content []ToolContent `json:"content"`
	}{wire: wire(result), Content: content})
}

func (result *ToolCallResult) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if err := requireJSONField(object, "content"); err != nil {
		return err
	}
	if raw, ok := object["isError"]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("field %q must not be null", "isError")
	}
	type wire ToolCallResult
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*result = ToolCallResult(decoded)
	return nil
}

type ToolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Resource any    `json:"resource,omitempty"`
}

const (
	ToolContentText     = "text"
	ToolContentImage    = "image"
	ToolContentResource = "resource"
)

func (content ToolContent) MarshalJSON() ([]byte, error) {
	switch content.Type {
	case ToolContentText:
		return marshalJSONNoHTMLEscape(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: content.Type, Text: content.Text})
	case ToolContentImage:
		return marshalJSONNoHTMLEscape(struct {
			Type     string `json:"type"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		}{Type: content.Type, Data: content.Data, MimeType: content.MimeType})
	case ToolContentResource:
		return marshalJSONNoHTMLEscape(struct {
			Type     string `json:"type"`
			Resource any    `json:"resource"`
		}{Type: content.Type, Resource: content.Resource})
	default:
		return nil, fmt.Errorf("unknown tool content type %q", content.Type)
	}
}

func (content *ToolContent) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	var kind struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &kind); err != nil {
		return err
	}
	switch kind.Type {
	case ToolContentText:
		if err := requireJSONField(object, "text"); err != nil {
			return err
		}
		var wire struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return err
		}
		*content = ToolContent{Type: kind.Type, Text: wire.Text}
	case ToolContentImage:
		for _, field := range []string{"data", "mimeType"} {
			if err := requireJSONField(object, field); err != nil {
				return err
			}
		}
		var wire struct {
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			return err
		}
		*content = ToolContent{Type: kind.Type, Data: wire.Data, MimeType: wire.MimeType}
	case ToolContentResource:
		if err := requireJSONField(object, "resource"); err != nil {
			return err
		}
		var wire struct {
			Resource any `json:"resource"`
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&wire); err != nil {
			return err
		}
		*content = ToolContent{Type: kind.Type, Resource: wire.Resource}
	default:
		return fmt.Errorf("unknown tool content type %q", kind.Type)
	}
	return nil
}

func requireJSONField(object map[string]json.RawMessage, field string) error {
	raw, ok := object[field]
	if !ok {
		return fmt.Errorf("missing required field %q", field)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("field %q must not be null", field)
	}
	return nil
}

func requireJSONObjectFields(data json.RawMessage, parent string, fields ...string) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("field %q must not be null", parent)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range fields {
		if err := requireJSONField(object, field); err != nil {
			return fmt.Errorf("%s.%w", parent, err)
		}
	}
	return nil
}

type ServerNotification struct {
	Method string
	Params any
}

type McpServerNotification = ServerNotification

type RpcRequest struct {
	JSONRPC       string `json:"jsonrpc"`
	ID            uint64 `json:"id"`
	Method        string `json:"method"`
	Params        any    `json:"params,omitempty"`
	ParamsPresent bool   `json:"-"`
}

type rpcRequest = RpcRequest

func (request RpcRequest) MarshalJSON() ([]byte, error) {
	type wire struct {
		JSONRPC string `json:"jsonrpc"`
		ID      uint64 `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}
	if request.ParamsPresent {
		return marshalJSONNoHTMLEscape(struct {
			JSONRPC string `json:"jsonrpc"`
			ID      uint64 `json:"id"`
			Method  string `json:"method"`
			Params  any    `json:"params"`
		}{JSONRPC: request.JSONRPC, ID: request.ID, Method: request.Method, Params: request.Params})
	}
	return marshalJSONNoHTMLEscape(wire{JSONRPC: request.JSONRPC, ID: request.ID, Method: request.Method, Params: request.Params})
}

type RpcNotification struct {
	JSONRPC       string `json:"jsonrpc"`
	Method        string `json:"method"`
	Params        any    `json:"params,omitempty"`
	ParamsPresent bool   `json:"-"`
}

type rpcNotification = RpcNotification

func (notification RpcNotification) MarshalJSON() ([]byte, error) {
	type wire struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}
	if notification.ParamsPresent {
		return marshalJSONNoHTMLEscape(struct {
			JSONRPC string `json:"jsonrpc"`
			Method  string `json:"method"`
			Params  any    `json:"params"`
		}{JSONRPC: notification.JSONRPC, Method: notification.Method, Params: notification.Params})
	}
	return marshalJSONNoHTMLEscape(wire{JSONRPC: notification.JSONRPC, Method: notification.Method, Params: notification.Params})
}

type RpcResponse struct {
	JSONRPC        *string         `json:"jsonrpc,omitempty"`
	ID             *uint64         `json:"id,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          *RPCError       `json:"error,omitempty"`
	Method         string          `json:"method,omitempty"`
	Params         json.RawMessage `json:"params,omitempty"`
	malformedError bool
}

type rpcResponse = RpcResponse

func (response *RpcResponse) UnmarshalJSON(data []byte) error {
	var decoded struct {
		JSONRPC *string         `json:"jsonrpc,omitempty"`
		ID      *uint64         `json:"id,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   json.RawMessage `json:"error,omitempty"`
		Method  string          `json:"method,omitempty"`
		Params  json.RawMessage `json:"params,omitempty"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*response = RpcResponse{JSONRPC: decoded.JSONRPC, ID: decoded.ID, Result: decoded.Result, Method: decoded.Method, Params: decoded.Params}
	if len(decoded.Error) > 0 {
		var rpcError RPCError
		if err := json.Unmarshal(decoded.Error, &rpcError); err != nil {
			response.malformedError = true
			return nil
		}
		response.Error = &rpcError
	}
	return nil
}

type RPCError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type RpcError = RPCError

func (rpcError *RPCError) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"code", "message"} {
		if err := requireJSONField(object, field); err != nil {
			return err
		}
	}
	type wire RPCError
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*rpcError = RPCError(decoded)
	return nil
}

func MakeRequest(id uint64, method string, params any) RpcRequest {
	return RpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
}

func MakeNotification(method string, params any) RpcNotification {
	return RpcNotification{JSONRPC: "2.0", Method: method, Params: params}
}

func MarshalRequest(id uint64, method string, params any) (string, error) {
	data, err := marshalJSONNoHTMLEscape(MakeRequest(id, method, params))
	return string(data), err
}

func MarshalNotification(method string, params any) (string, error) {
	return marshalNotification(method, params)
}

func marshalNotification(method string, params any) (string, error) {
	data, err := marshalJSONNoHTMLEscape(MakeNotification(method, params))
	return string(data), err
}
