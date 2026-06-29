package tools

import "github.com/detailyang/pig/mcp"

type McpAgentTool = mcp.McpAgentTool

func NewMcpAgentTool(client mcp.ToolCaller, tool *mcp.McpTool) *McpAgentTool {
	return mcp.NewMcpAgentTool(client, tool)
}
