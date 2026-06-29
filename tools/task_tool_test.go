package tools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

func TestTaskToolDefinitionMatchesUpstream(t *testing.T) {
	tool := NewTaskTool(TaskToolOptions{})
	if got, want := tool.Description(), "Delegate a self-contained research task to a fresh sub-agent. The subagent gets its own context window and tool set; this tool returns a single text result from the subagent. Use this when you need to inspect a large surface area (search, file reads) without polluting the main conversation."; got != want {
		t.Fatalf("description mismatch:\n got: %q\nwant: %q", got, want)
	}

	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subagent_type": map[string]any{
				"type":        "string",
				"enum":        []string{"general"},
				"description": "Which subagent kind to spawn. v1 ships only 'general'.",
				"default":     "general",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Short label for the task (visible in UI logs).",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "Full prompt the subagent will receive as its user message.",
			},
		},
		"required":             []string{"prompt"},
		"additionalProperties": false,
	}
	if got := tool.Parameters(); !reflect.DeepEqual(got, want) {
		t.Fatalf("parameters mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestTaskToolDelegatesToRunner(t *testing.T) {
	var got TaskRequest
	var subagentTools SubagentToolsFn = func() []agent.Tool { return []agent.Tool{ReadTool{}} }
	tool := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		got = request
		return TaskResult{Text: "subagent answer", Usage: &ai.Usage{InputTokens: 7, OutputTokens: 3}}, nil
	}), SubagentTools: subagentTools})
	if len(tool.SubagentTools()) != 1 {
		t.Fatalf("subagent tools factory mismatch")
	}
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "inspect repo", "description": "repo scan"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.SubagentType != "general" || got.Prompt != "inspect repo" || got.Description != "repo scan" {
		t.Fatalf("request mismatch: %#v", got)
	}
	if result.Content != "subagent answer" {
		t.Fatalf("result mismatch: %q", result.Content)
	}
	if result.Details["subagent_type"] != "general" || result.Details["description"] != "repo scan" || result.Details["chars"] != len(result.Content) {
		t.Fatalf("details mismatch: %#v", result.Details)
	}
}

func TestTaskToolAllowsEmptyPrompt(t *testing.T) {
	var got TaskRequest
	tool := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		got = request
		return TaskResult{Text: "ok"}, nil
	})})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != "" || result.Content != "ok" {
		t.Fatalf("empty prompt mismatch request=%#v result=%#v", got, result)
	}
	if result.Details["description"] != "" {
		t.Fatalf("empty prompt details mismatch: %#v", result.Details)
	}
}

func TestTaskToolMissingPromptMatchesUpstream(t *testing.T) {
	tool := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		return TaskResult{Text: "unused"}, nil
	})})
	for _, arguments := range []map[string]any{
		{},
		{"prompt": 123},
	} {
		_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: arguments}, nil)
		if err == nil || err.Error() != "missing required arg: prompt" {
			t.Fatalf("expected upstream missing prompt error for %#v, got %v", arguments, err)
		}
	}
}

func TestTaskToolRejectsInvalidUTF8Arguments(t *testing.T) {
	tool := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		return TaskResult{Text: "unused"}, nil
	})})

	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "prompt must be valid UTF-8" {
		t.Fatalf("expected invalid prompt error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "x", "description": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "description must be valid UTF-8" {
		t.Fatalf("expected invalid description error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "x", "subagent_type": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "subagent_type must be valid UTF-8" {
		t.Fatalf("expected invalid subagent_type error, got %v", err)
	}
}

func TestTaskToolValidatesSubagentTypeBeforePromptLikeUpstream(t *testing.T) {
	tool := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		return TaskResult{Text: "unused"}, nil
	})})
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"subagent_type": "specialist"}}, nil)
	if err == nil || err.Error() != "unknown subagent_type: specialist (allowed: general)" {
		t.Fatalf("expected upstream subagent_type validation to happen before prompt validation, got %v", err)
	}
}

func TestTaskToolRejectsUnknownTypeAndMissingRunner(t *testing.T) {
	tool := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		return TaskResult{}, nil
	})})
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "x", "subagent_type": "specialist"}}, nil); err == nil || !strings.Contains(err.Error(), "unknown subagent_type") {
		t.Fatalf("expected subagent type error, got %v", err)
	}
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "x", "subagent_type": ""}}, nil); err == nil || !strings.Contains(err.Error(), "unknown subagent_type") {
		t.Fatalf("expected empty subagent type error, got %v", err)
	}
	if _, err := NewTaskTool(TaskToolOptions{}).Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "x"}}, nil); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected missing runner error, got %v", err)
	}
}

func TestTaskToolWrapsEmptyAndRunnerErrors(t *testing.T) {
	empty := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		return TaskResult{}, nil
	})})
	result, err := empty.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "produced no text") {
		t.Fatalf("empty result mismatch: %q", result.Content)
	}
	if result.Details["chars"] != len(result.Content) || result.Details["subagent_type"] != "general" {
		t.Fatalf("empty result details mismatch: %#v", result.Details)
	}
	failed := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		return TaskResult{}, errors.New("boom")
	})})
	if _, err := failed.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "x"}}, nil); err == nil || !strings.Contains(err.Error(), "subagent failed") {
		t.Fatalf("expected runner error, got %v", err)
	}
}

func TestTaskToolReportsCancelledAfterRunnerReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tool := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		cancel()
		return TaskResult{Text: "should not surface"}, nil
	})})
	_, err := tool.Execute(ctx, ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "x"}}, nil)
	if err == nil || err.Error() != "cancelled" {
		t.Fatalf("expected cancelled error after runner returns, got %v", err)
	}
}

func TestNewSubagentTaskToolWiresAgentRunner(t *testing.T) {
	var gotTools []ai.Tool
	var gotDepth int
	tool := NewSubagentTaskTool(agent.SubagentRunnerOptions{
		Model: ai.Model{ID: "test-model", Provider: ai.Provider("test")},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			gotTools = append([]ai.Tool(nil), tools...)
			gotDepth = agent.SubagentDepth(ctx)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "subagent answer"}})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "inspect repo"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "subagent answer" {
		t.Fatalf("result mismatch: %#v", result)
	}
	if gotDepth != 1 {
		t.Fatalf("depth mismatch: %d", gotDepth)
	}
	if len(gotTools) == 0 || hasToolSpec(gotTools, "write") || hasToolSpec(gotTools, "edit") || hasToolSpec(gotTools, "bash") || hasToolSpec(gotTools, "task") {
		t.Fatalf("subagent tools should be read-only, got %#v", gotTools)
	}
	for _, name := range []string{"read", "ls", "grep", "find", "web_fetch", "git"} {
		if !hasToolSpec(gotTools, name) {
			t.Fatalf("missing subagent tool %q in %#v", name, gotTools)
		}
	}
}

func TestNewSubagentTaskToolIncludesSpecTypesInSchema(t *testing.T) {
	tool := NewSubagentTaskTool(agent.SubagentRunnerOptions{Specs: []agent.SubagentSpec{{Name: "reviewer"}}})
	params := tool.Parameters()
	properties := params["properties"].(map[string]any)
	subagentType := properties["subagent_type"].(map[string]any)
	enum := subagentType["enum"].([]string)
	if len(enum) != 2 || enum[0] != "general" || enum[1] != "reviewer" {
		t.Fatalf("enum mismatch: %#v", enum)
	}
}

func TestNewSubagentTaskToolRunsUserDefinedSpecType(t *testing.T) {
	tool := NewSubagentTaskTool(agent.SubagentRunnerOptions{
		Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Specs: []agent.SubagentSpec{{Name: "reviewer", SystemPrompt: "Review only."}},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			if messages[0].Content[0].Text != "Review only." {
				t.Fatalf("system prompt mismatch: %#v", messages)
			}
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "ok"}})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "task", Arguments: map[string]any{"subagent_type": "reviewer", "prompt": "inspect"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "ok" {
		t.Fatalf("result mismatch: %#v", result)
	}
}

func TestNewSubagentTaskToolParentCancelUnblocksStalledSubagentLikeUpstream(t *testing.T) {
	tool := NewSubagentTaskTool(agent.SubagentRunnerOptions{
		Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			return ai.NewAssistantMessageEventStream().MarkLive(), nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tool.Execute(ctx, ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "x"}}, nil)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "cancel") {
			t.Fatalf("expected cancellation error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parent cancel should unblock stalled subagent like upstream")
	}
}

func TestTaskToolRejectsRecursiveSubagentByDefault(t *testing.T) {
	tool := NewTaskTool(TaskToolOptions{Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		return TaskResult{Text: "nested"}, nil
	})})
	ctx := agent.ContextWithSubagentDepth(context.Background(), 1)
	_, err := tool.Execute(ctx, ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "nested"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "subagent depth") {
		t.Fatalf("expected depth cap error, got %v", err)
	}
}

func TestTaskToolAllowsConfiguredDepth(t *testing.T) {
	var gotDepth int
	tool := NewTaskTool(TaskToolOptions{MaxDepth: 2, Runner: TaskRunnerFunc(func(ctx context.Context, request TaskRequest) (TaskResult, error) {
		gotDepth = agent.SubagentDepth(ctx)
		return TaskResult{Text: "nested"}, nil
	})})
	ctx := agent.ContextWithSubagentDepth(context.Background(), 1)
	result, err := tool.Execute(ctx, ai.ToolCall{Name: "task", Arguments: map[string]any{"prompt": "nested"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "nested" || gotDepth != 2 {
		t.Fatalf("depth/result mismatch depth=%d result=%#v", gotDepth, result)
	}
}

func TestSubagentReadOnlyToolsAreFreshCopies(t *testing.T) {
	first := SubagentReadOnlyTools()
	second := SubagentReadOnlyTools()
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("expected read-only tools")
	}
	first[0] = BashTool{}
	if second[0].Name() == "bash" {
		t.Fatalf("subagent tools reused mutable slice: %#v", second)
	}
	for _, tool := range second {
		if tool.Name() == "write" || tool.Name() == "edit" || tool.Name() == "bash" || tool.Name() == "task" {
			t.Fatalf("subagent read-only tools included mutating/control tool %q", tool.Name())
		}
	}
}

func hasToolSpec(tools []ai.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}
