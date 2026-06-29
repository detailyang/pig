package tools

import (
	"context"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/mcp"
)

type mcpCompatClient struct{}

func (mcpCompatClient) ToolsCall(context.Context, string, any) (mcp.ToolCallResult, error) {
	return mcp.ToolCallResult{Content: []mcp.ToolContent{{Type: "text", Text: "ok"}}}, nil
}

func TestMcpAdapterUpstreamToolsPackageAliases(t *testing.T) {
	description := "Echo text"
	tool := NewMcpAgentTool(mcpCompatClient{}, &mcp.McpTool{Name: "echo", Description: &description, InputSchema: map[string]any{"type": "object"}})
	var _ agent.Tool = tool
	var _ agent.ToolExecutionModeOverride = tool
	var _ *McpAgentTool = tool
	if tool.Name() != "echo" || tool.Description() != "Echo text" || tool.ExecutionMode() != agent.ToolExecutionParallel {
		t.Fatalf("adapter metadata mismatch")
	}
	result, err := tool.Execute(context.Background(), ai.ToolCall{ID: "call", Name: "echo", Arguments: map[string]any{}}, nil)
	if err != nil || result.Content != "ok" {
		t.Fatalf("adapter execute mismatch result=%#v err=%v", result, err)
	}
}

func TestTruncateUpstreamDefaultConstants(t *testing.T) {
	if DEFAULT_MAX_LINES != DefaultMaxLines || DEFAULT_MAX_BYTES != DefaultMaxBytes {
		t.Fatalf("default truncate constants mismatch")
	}
}
