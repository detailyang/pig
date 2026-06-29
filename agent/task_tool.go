package agent

import (
	"context"

	"github.com/detailyang/pig/ai"
)

type SubagentToolsFn func() []Tool

type TaskTool struct {
	model         ai.Model
	stream        StreamFunc
	subagentTools SubagentToolsFn
}

func NewTaskTool(model ai.Model, stream StreamFunc, subagentTools SubagentToolsFn) *TaskTool {
	return &TaskTool{model: model, stream: stream, subagentTools: subagentTools}
}

func (tool *TaskTool) Name() string { return "task" }

func (tool *TaskTool) Description() string {
	return "Launch a subagent to perform an independent research task and return its final answer."
}

func (tool *TaskTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"description":   map[string]any{"type": "string", "description": "Short task description."},
			"prompt":        map[string]any{"type": "string", "description": "Detailed prompt for the subagent."},
			"subagent_type": map[string]any{"type": "string", "description": "Subagent type to use.", "enum": []string{"general"}},
		},
		"required":             []string{"prompt"},
		"additionalProperties": false,
	}
}

func (tool *TaskTool) ExecutionMode() ToolExecutionMode { return ToolExecutionParallel }

func (tool *TaskTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	prompt, err := requiredStringToolArg(call, "prompt")
	if err != nil {
		return ToolResult{}, err
	}
	description, _ := call.Arguments["description"].(string)
	subagentType, _ := call.Arguments["subagent_type"].(string)
	runner := NewSubagentRunner(SubagentRunnerOptions{Model: tool.model, Tools: tool.tools(), Stream: tool.stream})
	result, err := runner.RunTask(ctx, SubagentTaskRequest{SubagentType: subagentType, Description: description, Prompt: prompt})
	if err != nil {
		return ToolResult{}, err
	}
	details := map[string]any{"subagent_type": "general"}
	if subagentType != "" {
		details["subagent_type"] = subagentType
	}
	if description != "" {
		details["description"] = description
	}
	if result.Usage != nil {
		details["usage"] = result.Usage
	}
	return ToolResult{CallID: call.ID, Name: call.Name, Content: result.Text, ContentBlocks: []ai.ContentBlock{{Type: ai.ContentText, Text: result.Text}}, Details: details}, nil
}

func (tool *TaskTool) tools() []Tool {
	if tool.subagentTools == nil {
		return nil
	}
	return tool.subagentTools()
}

func NewDefaultSubagentToolsFn(tools []Tool) SubagentToolsFn {
	copied := append([]Tool(nil), tools...)
	return func() []Tool { return append([]Tool(nil), copied...) }
}
