package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/detailyang/pig/ai"
)

type SubagentTaskRequest struct {
	SubagentType string
	Description  string
	Prompt       string
}

type SubagentTaskResult struct {
	Text  string
	Usage *ai.Usage
}

type SubagentRunnerOptions struct {
	Model        ai.Model
	Tools        []Tool
	Specs        []SubagentSpec
	Stream       StreamFunc
	SystemPrompt string
	Config       Config
}

type SubagentRunner struct {
	options SubagentRunnerOptions
}

func NewSubagentRunner(options SubagentRunnerOptions) *SubagentRunner {
	options.Tools = append([]Tool(nil), options.Tools...)
	options.Specs = append([]SubagentSpec(nil), options.Specs...)
	return &SubagentRunner{options: options}
}

func (runner *SubagentRunner) RunTask(ctx context.Context, request SubagentTaskRequest) (SubagentTaskResult, error) {
	if request.SubagentType == "" {
		request.SubagentType = "general"
	}
	spec, ok := runner.resolveSpec(request.SubagentType)
	if !ok {
		return SubagentTaskResult{}, fmt.Errorf("unknown subagent_type: %s (allowed: %s)", request.SubagentType, runner.allowedTypes())
	}
	systemPrompt := spec.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = runner.options.SystemPrompt
	}
	if systemPrompt == "" {
		systemPrompt = fmt.Sprintf("You are a research subagent dispatched by a coding agent.\nDescription of your task: %s\nStay focused on the prompt; return a concise final answer.", request.Description)
	}
	messages := []Message{NewUserMessage(request.Prompt)}
	subagent := New(Options{Model: runner.options.Model, SystemPrompt: systemPrompt, Tools: runner.toolsForSpec(spec), Stream: runner.options.Stream, Config: runner.options.Config})
	runCtx := ctx
	if SubagentDepth(runCtx) == 0 {
		runCtx = ContextWithIncrementedSubagentDepth(runCtx)
	}
	state, err := subagent.Run(runCtx, messages)
	if err != nil {
		return SubagentTaskResult{}, err
	}
	return SubagentTaskResult{Text: lastAssistantText(state.Messages), Usage: rollupUsage(state.Messages)}, nil
}

func rollupUsage(messages []Message) *ai.Usage {
	usage := ai.Usage{}
	var hasUsage bool
	for _, message := range messages {
		if message.Kind != MessageKindLLM || message.LLM == nil || message.LLM.Usage == nil {
			continue
		}
		hasUsage = true
		usage.InputTokens += message.LLM.Usage.InputTokens
		usage.OutputTokens += message.LLM.Usage.OutputTokens
		usage.CacheReadTokens += message.LLM.Usage.CacheReadTokens
		usage.CacheWriteTokens += message.LLM.Usage.CacheWriteTokens
		if message.LLM.Usage.Cost != nil {
			if usage.Cost == nil {
				usage.Cost = &ai.UsageCost{}
			}
			usage.Cost.Input += message.LLM.Usage.Cost.Input
			usage.Cost.Output += message.LLM.Usage.Cost.Output
			usage.Cost.CacheRead += message.LLM.Usage.Cost.CacheRead
			usage.Cost.CacheWrite += message.LLM.Usage.Cost.CacheWrite
			usage.Cost.Total += message.LLM.Usage.Cost.Total
		}
	}
	if !hasUsage {
		return nil
	}
	return &usage
}

func (runner *SubagentRunner) resolveSpec(name string) (SubagentSpec, bool) {
	if name == "general" {
		return SubagentSpec{Name: "general"}, true
	}
	for _, spec := range runner.options.Specs {
		if spec.Name == name {
			return spec, true
		}
	}
	return SubagentSpec{}, false
}

func (runner *SubagentRunner) allowedTypes() string {
	names := []string{"general"}
	for _, spec := range runner.options.Specs {
		names = append(names, spec.Name)
	}
	return strings.Join(names, ", ")
}

func (runner *SubagentRunner) toolsForSpec(spec SubagentSpec) []Tool {
	if len(spec.Tools) == 0 {
		return runner.options.Tools
	}
	allowed := map[string]bool{}
	for _, name := range spec.Tools {
		allowed[name] = true
	}
	var tools []Tool
	for _, tool := range runner.options.Tools {
		if allowed[tool.Name()] {
			tools = append(tools, tool)
		}
	}
	return tools
}

func lastAssistantText(messages []Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Kind != MessageKindLLM || message.LLM == nil || message.LLM.Role != ai.RoleAssistant {
			continue
		}
		var parts []string
		for _, block := range message.LLM.Content {
			if block.Type == ai.ContentText {
				parts = append(parts, block.Text)
			}
		}
		text := strings.Join(parts, "\n")
		if text != "" {
			return text
		}
	}
	return ""
}
