package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/detailyang/pig/ai"
)

var testSubagentModel = ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("fake")}

func TestSubagentRunnerRunsFreshAgentAndReturnsText(t *testing.T) {
	var gotMessages []ai.Message
	runner := NewSubagentRunner(SubagentRunnerOptions{
		Model: testSubagentModel,
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			gotMessages = append([]ai.Message(nil), messages...)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "answer"}})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	result, err := runner.RunTask(context.Background(), SubagentTaskRequest{SubagentType: "general", Description: "scan", Prompt: "inspect repo"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "answer" {
		t.Fatalf("result mismatch: %#v", result)
	}
	if len(gotMessages) != 2 || gotMessages[0].Role != ai.RoleSystem || gotMessages[1].Role != ai.RoleUser || gotMessages[0].Content[0].Text == "" || gotMessages[1].Content[0].Text != "inspect repo" {
		t.Fatalf("messages mismatch: %#v", gotMessages)
	}
}

func TestTaskToolUsesSubagentToolsFnLikeUpstream(t *testing.T) {
	calledFactory := 0
	var gotTools []ai.Tool
	tool := NewTaskTool(testSubagentModel, func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		gotTools = append([]ai.Tool(nil), tools...)
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "done"}})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}, SubagentToolsFn(func() []Tool {
		calledFactory++
		return []Tool{fakeTool{name: "read"}}
	}))
	var _ Tool = tool
	var _ ToolDefinition = tool
	var _ ToolExecutionModeOverride = tool

	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"description": "scan", "prompt": "inspect"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "done" || calledFactory != 1 {
		t.Fatalf("task result/factory mismatch result=%#v called=%d", result, calledFactory)
	}
	if len(gotTools) != 1 || gotTools[0].Name != "read" {
		t.Fatalf("subagent tools mismatch: %#v", gotTools)
	}
}

func TestTaskToolRejectsUnknownSubagentType(t *testing.T) {
	tool := NewTaskTool(testSubagentModel, nil, nil)
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"subagent_type": "special", "prompt": "inspect"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "unknown subagent_type: special") {
		t.Fatalf("expected unknown type error, got %v", err)
	}
}

func TestSubagentRunnerJoinsAssistantTextBlocksLikeUpstream(t *testing.T) {
	runner := NewSubagentRunner(SubagentRunnerOptions{
		Model: testSubagentModel,
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "first"}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "second"}})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	result, err := runner.RunTask(context.Background(), SubagentTaskRequest{Prompt: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "first\nsecond" {
		t.Fatalf("assistant text block join mismatch: %#v", result)
	}
}

func TestSubagentRunnerKeepsSystemPromptOutOfTranscriptTransform(t *testing.T) {
	var transformed []ai.Message
	runner := NewSubagentRunner(SubagentRunnerOptions{
		Model:        testSubagentModel,
		SystemPrompt: "subagent system",
		Config: Config{TransformContext: func(ctx context.Context, messages []ai.Message) ([]ai.Message, error) {
			transformed = append([]ai.Message(nil), messages...)
			return messages, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "answer"}})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	if _, err := runner.RunTask(context.Background(), SubagentTaskRequest{Prompt: "inspect"}); err != nil {
		t.Fatal(err)
	}
	if len(transformed) != 1 || transformed[0].Role != ai.RoleUser || transformed[0].Content[0].Text != "inspect" {
		t.Fatalf("system prompt leaked into transform transcript: %#v", transformed)
	}
}

func TestSubagentRunnerRollsUpUsage(t *testing.T) {
	runner := NewSubagentRunner(SubagentRunnerOptions{
		Model: testSubagentModel,
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "answer"}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventUsage, Usage: &ai.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 2, Cost: &ai.UsageCost{Total: 0.25}}})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	result, err := runner.RunTask(context.Background(), SubagentTaskRequest{Prompt: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage == nil || result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 5 || result.Usage.CacheReadTokens != 2 || result.Usage.Cost == nil || result.Usage.Cost.Total != 0.25 {
		t.Fatalf("usage mismatch: %#v", result.Usage)
	}
}

func TestRollupUsageSumsAssistantMessages(t *testing.T) {
	messages := []Message{
		NewUserMessage("x"),
		assistantWithUsage("a", &ai.Usage{InputTokens: 1, OutputTokens: 2, Cost: &ai.UsageCost{Input: 0.125, Total: 0.25}}),
		assistantWithUsage("b", &ai.Usage{InputTokens: 3, OutputTokens: 4, CacheWriteTokens: 5, Cost: &ai.UsageCost{Output: 0.5, Total: 0.5}}),
	}
	usage := rollupUsage(messages)
	if usage == nil || usage.InputTokens != 4 || usage.OutputTokens != 6 || usage.CacheWriteTokens != 5 || usage.Cost == nil || usage.Cost.Input != 0.125 || usage.Cost.Output != 0.5 || usage.Cost.Total != 0.75 {
		t.Fatalf("usage mismatch: %#v", usage)
	}
}

func TestSubagentRunnerPassesToolsAndSurfacesErrors(t *testing.T) {
	runner := NewSubagentRunner(SubagentRunnerOptions{
		Model: testSubagentModel,
		Tools: []Tool{fakeTool{name: "fake"}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			if len(tools) != 1 || tools[0].Name != "fake" {
				t.Fatalf("tools mismatch: %#v", tools)
			}
			return nil, errors.New("stream failed")
		},
	})
	if _, err := runner.RunTask(context.Background(), SubagentTaskRequest{Prompt: "x"}); err == nil || err.Error() != "stream failed" {
		t.Fatalf("expected stream error, got %v", err)
	}
}

func TestSubagentRunnerSupportsConcurrentTasks(t *testing.T) {
	start := make(chan struct{})
	finish := make(chan struct{})
	var started sync.WaitGroup
	started.Add(2)
	runner := NewSubagentRunner(SubagentRunnerOptions{
		Model: testSubagentModel,
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			started.Done()
			<-start
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: messages[len(messages)-1].Content[0].Text}})
			stream.Close(ai.DoneReasonStop)
			finish <- struct{}{}
			return stream, nil
		},
	})
	errs := make(chan error, 2)
	results := make(chan string, 2)
	var done sync.WaitGroup
	done.Add(2)
	for _, prompt := range []string{"first", "second"} {
		prompt := prompt
		go func() {
			defer done.Done()
			result, err := runner.RunTask(context.Background(), SubagentTaskRequest{Prompt: prompt})
			if err != nil {
				errs <- err
				return
			}
			results <- result.Text
		}()
	}
	started.Wait()
	close(start)
	<-finish
	<-finish
	done.Wait()
	close(errs)
	close(results)
	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}
	seen := map[string]bool{}
	for result := range results {
		seen[result] = true
	}
	if !seen["first"] || !seen["second"] {
		t.Fatalf("results mismatch: %#v", seen)
	}
}

func TestLoadSubagentSpecsFromDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reviewer.toml"), []byte("name = \"reviewer\"\ndescription = \"Review code\"\nsystem_prompt = \"Review carefully.\"\ntools = [\"read\", \"grep\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specs, err := LoadSubagentSpecsDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Name != "reviewer" || specs[0].Description != "Review code" || specs[0].SystemPrompt != "Review carefully." {
		t.Fatalf("spec mismatch: %#v", specs)
	}
	if len(specs[0].Tools) != 2 || specs[0].Tools[0] != "read" || specs[0].Tools[1] != "grep" {
		t.Fatalf("tools mismatch: %#v", specs[0].Tools)
	}
}

func TestSubagentRunnerUsesUserDefinedSpec(t *testing.T) {
	var gotMessages []ai.Message
	var gotTools []ai.Tool
	runner := NewSubagentRunner(SubagentRunnerOptions{
		Model: testSubagentModel,
		Tools: []Tool{fakeTool{name: "read"}, fakeTool{name: "grep"}, fakeTool{name: "write"}},
		Specs: []SubagentSpec{{Name: "reviewer", SystemPrompt: "Review carefully.", Tools: []string{"read", "grep"}}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			gotMessages = append([]ai.Message(nil), messages...)
			gotTools = append([]ai.Tool(nil), tools...)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "reviewed"}})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	result, err := runner.RunTask(context.Background(), SubagentTaskRequest{SubagentType: "reviewer", Description: "scan", Prompt: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "reviewed" {
		t.Fatalf("result mismatch: %#v", result)
	}
	if len(gotMessages) != 2 || gotMessages[0].Role != ai.RoleSystem || gotMessages[0].Content[0].Text != "Review carefully." || gotMessages[1].Role != ai.RoleUser {
		t.Fatalf("messages mismatch: %#v", gotMessages)
	}
	if len(gotTools) != 2 || gotTools[0].Name != "read" || gotTools[1].Name != "grep" {
		t.Fatalf("tools mismatch: %#v", gotTools)
	}
}

func TestSubagentRunnerIncrementsDepthForInnerAgent(t *testing.T) {
	var gotDepth int
	runner := NewSubagentRunner(SubagentRunnerOptions{
		Model: testSubagentModel,
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			gotDepth = SubagentDepth(ctx)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "ok"}})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	if _, err := runner.RunTask(context.Background(), SubagentTaskRequest{Prompt: "inspect"}); err != nil {
		t.Fatal(err)
	}
	if gotDepth != 1 {
		t.Fatalf("depth mismatch: %d", gotDepth)
	}
}

type fakeTool struct{ name string }

func (tool fakeTool) Name() string        { return tool.name }
func (tool fakeTool) Description() string { return "fake" }
func (tool fakeTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{}, nil
}

func assistantWithUsage(text string, usage *ai.Usage) Message {
	message := NewAssistantMessage(text)
	message.LLM.Usage = usage
	return message
}
