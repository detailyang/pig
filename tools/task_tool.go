package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type TaskRequest struct {
	SubagentType string
	Description  string
	Prompt       string
}

type TaskResult struct {
	Text  string
	Usage *ai.Usage
}

type TaskRunner interface {
	RunTask(ctx context.Context, request TaskRequest) (TaskResult, error)
}

type TaskRunnerFunc func(ctx context.Context, request TaskRequest) (TaskResult, error)

func (fn TaskRunnerFunc) RunTask(ctx context.Context, request TaskRequest) (TaskResult, error) {
	return fn(ctx, request)
}

type SubagentToolsFn func() []agent.Tool

type TaskToolOptions struct {
	Runner        TaskRunner
	SubagentTypes []string
	MaxDepth      int
	SubagentTools SubagentToolsFn
}

type TaskTool struct {
	Runner        TaskRunner
	SubagentTypes []string
	MaxDepth      int
	subagentTools SubagentToolsFn
}

func (TaskTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}

func NewTaskTool(options TaskToolOptions) TaskTool {
	subagentTypes := append([]string(nil), options.SubagentTypes...)
	if len(subagentTypes) == 0 {
		subagentTypes = []string{"general"}
	}
	maxDepth := options.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	return TaskTool{Runner: options.Runner, SubagentTypes: subagentTypes, MaxDepth: maxDepth, subagentTools: options.SubagentTools}
}

func (tool TaskTool) SubagentTools() []agent.Tool {
	if tool.subagentTools == nil {
		return nil
	}
	return tool.subagentTools()
}

func NewSubagentTaskTool(options agent.SubagentRunnerOptions) TaskTool {
	if len(options.Tools) == 0 {
		options.Tools = SubagentReadOnlyTools()
	}
	runner := agent.NewSubagentRunner(options)
	return NewTaskTool(TaskToolOptions{Runner: subagentTaskRunner{Runner: runner}, SubagentTypes: subagentTypesFromSpecs(options.Specs)})
}

func SubagentReadOnlyTools() []agent.Tool {
	return []agent.Tool{ReadTool{}, LSTool{}, GrepTool{}, FindTool{}, WebFetchTool{}, GitTool{}}
}

type subagentTaskRunner struct {
	Runner *agent.SubagentRunner
}

func (runner subagentTaskRunner) RunTask(ctx context.Context, request TaskRequest) (TaskResult, error) {
	result, err := runner.Runner.RunTask(ctx, agent.SubagentTaskRequest{SubagentType: request.SubagentType, Description: request.Description, Prompt: request.Prompt})
	if err != nil {
		return TaskResult{}, err
	}
	return TaskResult{Text: result.Text, Usage: result.Usage}, nil
}

func (TaskTool) Name() string { return "task" }
func (TaskTool) Description() string {
	return "Delegate a self-contained research task to a fresh sub-agent. The subagent gets its own context window and tool set; this tool returns a single text result from the subagent. Use this when you need to inspect a large surface area (search, file reads) without polluting the main conversation."
}
func (tool TaskTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subagent_type": map[string]any{"type": "string", "enum": tool.SubagentTypes, "description": "Which subagent kind to spawn. v1 ships only 'general'.", "default": "general"},
			"description":   map[string]any{"type": "string", "description": "Short label for the task (visible in UI logs)."},
			"prompt":        map[string]any{"type": "string", "description": "Full prompt the subagent will receive as its user message."},
		},
		"required":             []string{"prompt"},
		"additionalProperties": false,
	}
}
func (tool TaskTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	subagentType := optionalTaskStringArg(call, "subagent_type", "general")
	if !utf8.ValidString(subagentType) {
		return agent.ToolResult{}, fmt.Errorf("subagent_type must be valid UTF-8")
	}
	if !containsSubagentType(tool.SubagentTypes, subagentType) {
		return agent.ToolResult{}, fmt.Errorf("unknown subagent_type: %s (allowed: %s)", subagentType, strings.Join(tool.SubagentTypes, ", "))
	}
	prompt, err := requiredToolArg(call, "prompt")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(prompt) {
		return agent.ToolResult{}, fmt.Errorf("prompt must be valid UTF-8")
	}
	description := optionalStringArg(call, "description", "")
	if !utf8.ValidString(description) {
		return agent.ToolResult{}, fmt.Errorf("description must be valid UTF-8")
	}
	if tool.Runner == nil {
		return agent.ToolResult{}, fmt.Errorf("task tool not configured: provide a TaskRunner")
	}
	depth := agent.SubagentDepth(ctx)
	if depth >= tool.MaxDepth {
		return agent.ToolResult{}, fmt.Errorf("subagent depth %d reached max depth %d", depth, tool.MaxDepth)
	}
	runCtx := agent.ContextWithSubagentDepth(ctx, depth+1)
	result, err := tool.Runner.RunTask(runCtx, TaskRequest{SubagentType: subagentType, Description: description, Prompt: prompt})
	if ctx.Err() != nil {
		return agent.ToolResult{}, fmt.Errorf("cancelled")
	}
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("subagent failed: %w", err)
	}
	text := result.Text
	if text == "" {
		text = "(subagent produced no text output)"
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: text, Details: map[string]any{"subagent_type": subagentType, "description": description, "chars": len(text)}}, nil
}

func optionalTaskStringArg(call ai.ToolCall, key string, fallback string) string {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback
	}
	text, ok := value.(string)
	if !ok {
		return fallback
	}
	return text
}

func subagentTypesFromSpecs(specs []agent.SubagentSpec) []string {
	types := []string{"general"}
	for _, spec := range specs {
		if spec.Name != "" {
			types = append(types, spec.Name)
		}
	}
	return types
}

func containsSubagentType(types []string, value string) bool {
	for _, item := range types {
		if item == value {
			return true
		}
	}
	return false
}
