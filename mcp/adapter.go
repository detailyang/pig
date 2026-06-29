package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type ToolCaller interface {
	ToolsCall(ctx context.Context, name string, arguments any) (ToolCallResult, error)
}

type AgentToolAdapter struct {
	client ToolCaller
	tool   Tool
}

type McpAgentTool = AgentToolAdapter

func NewAgentToolAdapter(client ToolCaller, tool Tool) *AgentToolAdapter {
	return &AgentToolAdapter{client: client, tool: tool}
}

func NewMcpAgentTool(client ToolCaller, tool *McpTool) *McpAgentTool {
	if tool == nil {
		return NewAgentToolAdapter(client, Tool{})
	}
	return NewAgentToolAdapter(client, *tool)
}

func AgentToolsFromCatalog(client ToolCaller, catalog []Tool) []*AgentToolAdapter {
	adapters := make([]*AgentToolAdapter, 0, len(catalog))
	for _, tool := range catalog {
		adapters = append(adapters, NewAgentToolAdapter(client, tool))
	}
	return adapters
}

func (adapter *AgentToolAdapter) Name() string { return adapter.tool.Name }

func (adapter *AgentToolAdapter) Description() string {
	if adapter.tool.Description == nil {
		return ""
	}
	return *adapter.tool.Description
}

func (adapter *AgentToolAdapter) Parameters() any { return adapter.tool.InputSchema }

func (adapter *AgentToolAdapter) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}

func (adapter *AgentToolAdapter) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	result, err := adapter.client.ToolsCall(ctx, adapter.tool.Name, call.Arguments)
	if err != nil {
		if errors.Is(err, ErrCancelled) {
			return agent.ToolResult{}, fmt.Errorf("cancelled")
		}
		return agent.ToolResult{}, fmt.Errorf("mcp call: %w", err)
	}
	content := RenderToolContent(result.Content)
	if result.IsError {
		return agent.ToolResult{}, fmt.Errorf("%s", strings.TrimSpace(RenderTextToolContent(result.Content)))
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: content, ContentBlocks: ToolContentBlocks(result.Content), Details: map[string]any{"name": adapter.tool.Name, "isError": false}}, nil
}

func ToolContentBlocks(blocks []ToolContent) []ai.ContentBlock {
	out := make([]ai.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			out = append(out, ai.ContentBlock{Type: ai.ContentText, Text: block.Text})
		case "image":
			out = append(out, ai.ContentBlock{Type: ai.ContentImage, Data: block.Data, MimeType: block.MimeType})
		case "resource":
			data, err := marshalJSONNoHTMLEscape(block.Resource)
			if err != nil {
				out = append(out, ai.ContentBlock{Type: ai.ContentText, Text: "<resource>(resource)</resource>"})
			} else {
				out = append(out, ai.ContentBlock{Type: ai.ContentText, Text: "<resource>" + string(data) + "</resource>"})
			}
		}
	}
	return out
}

func RenderTextToolContent(blocks []ToolContent) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "resource":
			data, err := marshalJSONNoHTMLEscape(block.Resource)
			if err != nil {
				parts = append(parts, "<resource>(resource)</resource>")
			} else {
				parts = append(parts, "<resource>"+string(data)+"</resource>")
			}
		}
	}
	return strings.Join(parts, "\n")
}

func RenderToolContent(blocks []ToolContent) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "image":
			parts = append(parts, fmt.Sprintf("<image mimeType=\"%s\">%s</image>", block.MimeType, block.Data))
		case "resource":
			data, err := marshalJSONNoHTMLEscape(block.Resource)
			if err != nil {
				parts = append(parts, "<resource>(resource)</resource>")
			} else {
				parts = append(parts, "<resource>"+string(data)+"</resource>")
			}
		default:
			data, err := json.Marshal(block)
			if err == nil {
				parts = append(parts, string(data))
			}
		}
	}
	return strings.Join(parts, "\n")
}

func marshalJSONNoHTMLEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
