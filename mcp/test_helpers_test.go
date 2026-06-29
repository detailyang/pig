package mcp

import "github.com/detailyang/pig/ai"

func testToolCall(name string, arguments map[string]any) ai.ToolCall {
	return ai.ToolCall{ID: "call-1", Name: name, Arguments: arguments}
}
