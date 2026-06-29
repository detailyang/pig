package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/detailyang/pig/ai"
)

type recordingTool struct {
	calls []ai.ToolCall
}

func (tool *recordingTool) Name() string        { return "read" }
func (tool *recordingTool) Description() string { return "read files" }
func (tool *recordingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	tool.calls = append(tool.calls, call)
	return ToolResult{Content: "ok:" + call.Arguments["path"].(string)}, nil
}

type preparingTool struct {
	calls []ai.ToolCall
}

func (tool *preparingTool) Name() string        { return "prepare" }
func (tool *preparingTool) Description() string { return "prepare args" }
func (tool *preparingTool) PrepareArguments(arguments map[string]any) map[string]any {
	prepared := make(map[string]any, len(arguments)+1)
	for key, value := range arguments {
		prepared[key] = value
	}
	prepared["path"] = "prepared.md"
	return prepared
}
func (tool *preparingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	tool.calls = append(tool.calls, call)
	return ToolResult{Content: call.Arguments["path"].(string)}, nil
}

type denyingTool struct {
	calls int
}

func (tool *denyingTool) Name() string        { return "deny" }
func (tool *denyingTool) Description() string { return "deny tool" }
func (tool *denyingTool) PermissionClassification(arguments map[string]any) PermissionClassification {
	return PermissionDeny
}
func (tool *denyingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	tool.calls++
	return ToolResult{Content: "should not run"}, nil
}

type askingTool struct {
	calls []ai.ToolCall
}

func (tool *askingTool) Name() string        { return "ask" }
func (tool *askingTool) Description() string { return "ask tool" }
func (tool *askingTool) PermissionClassification(arguments map[string]any) PermissionClassification {
	return PermissionAsk
}
func (tool *askingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	tool.calls = append(tool.calls, call)
	return ToolResult{Content: "allowed"}, nil
}

type askingReasonTool struct{}

func (tool askingReasonTool) Name() string        { return "ask_reason" }
func (tool askingReasonTool) Description() string { return "requires reasoned prompt" }
func (tool askingReasonTool) PermissionClassification(arguments map[string]any) PermissionClassification {
	return PermissionAsk
}
func (tool askingReasonTool) PermissionReason(arguments map[string]any) string {
	return "custom prompt reason"
}
func (tool askingReasonTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{Content: "allowed"}, nil
}

type denyingReasonTool struct{}

func (tool denyingReasonTool) Name() string        { return "deny_reason" }
func (tool denyingReasonTool) Description() string { return "denies with reason" }
func (tool denyingReasonTool) PermissionClassification(arguments map[string]any) PermissionClassification {
	return PermissionDeny
}
func (tool denyingReasonTool) PermissionReason(arguments map[string]any) string {
	return "custom deny reason"
}
func (tool denyingReasonTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{Content: "should not run"}, nil
}

type promptingTool struct{}

func (tool promptingTool) Name() string        { return "prompt" }
func (tool promptingTool) Description() string { return "prompt tool" }
func (tool promptingTool) PermissionClassification(arguments map[string]any) PermissionClassification {
	return PermissionPrompt
}
func (tool promptingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{Content: "prompt allowed"}, nil
}

type blockingTool struct{}

func (tool blockingTool) Name() string        { return "block" }
func (tool blockingTool) Description() string { return "block tool" }
func (tool blockingTool) PermissionClassification(arguments map[string]any) PermissionClassification {
	return PermissionBlock
}
func (tool blockingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{Content: "should not run"}, nil
}

type unknownPermissionTool struct {
	calls int
}

func (tool *unknownPermissionTool) Name() string        { return "unknown_permission" }
func (tool *unknownPermissionTool) Description() string { return "unknown permission tool" }
func (tool *unknownPermissionTool) PermissionClassification(arguments map[string]any) PermissionClassification {
	return PermissionClassification("future")
}
func (tool *unknownPermissionTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	tool.calls++
	return ToolResult{Content: "should not run"}, nil
}

type failingToolWithDetails struct{}

func (tool failingToolWithDetails) Name() string        { return "read" }
func (tool failingToolWithDetails) Description() string { return "read files" }
func (tool failingToolWithDetails) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{Details: map[string]any{"exit_code": float64(2)}}, context.Canceled
}

type failingToolWithContent struct{}

func (tool failingToolWithContent) Name() string        { return "read" }
func (tool failingToolWithContent) Description() string { return "read files" }
func (tool failingToolWithContent) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{Content: "partial output"}, context.Canceled
}

type terminatingTool struct{}

func (tool terminatingTool) Name() string        { return "finish" }
func (tool terminatingTool) Description() string { return "finish task" }
func (tool terminatingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{Content: "finished", Terminate: boolPtr(true)}, nil
}

type updatingTool struct{}

func (tool updatingTool) Name() string        { return "update" }
func (tool updatingTool) Description() string { return "update progress" }
func (tool updatingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	update(ToolResult{Content: "partial", Details: map[string]any{"pct": float64(50)}})
	return ToolResult{Content: "done"}, nil
}

type blockUpdatingTool struct{}

func (tool blockUpdatingTool) Name() string        { return "block_update" }
func (tool blockUpdatingTool) Description() string { return "update with content blocks" }
func (tool blockUpdatingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	update(ToolResult{ContentBlocks: []ai.ContentBlock{{Type: ai.ContentThinking, Thinking: "plan"}, {Type: ai.ContentText, Text: "partial"}, {Type: ai.ContentImage, Data: "aW1n", MimeType: "image/png"}}, DetailsValue: []any{"trace"}})
	return ToolResult{Content: "done"}, nil
}

type blockResultTool struct{}

func (tool blockResultTool) Name() string        { return "block_result" }
func (tool blockResultTool) Description() string { return "return content blocks" }
func (tool blockResultTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{ContentBlocks: []ai.ContentBlock{{Type: ai.ContentThinking, Thinking: "plan"}, {Type: ai.ContentText, Text: "done"}, {Type: ai.ContentImage, Data: "aW1n", MimeType: "image/png"}}, DetailsValue: []any{"trace"}}, nil
}

type preparingUpdatingTool struct{}

func (tool preparingUpdatingTool) Name() string        { return "prepare_update" }
func (tool preparingUpdatingTool) Description() string { return "prepare and update" }
func (tool preparingUpdatingTool) PrepareArguments(arguments map[string]any) map[string]any {
	return map[string]any{"path": "prepared.md"}
}
func (tool preparingUpdatingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	update(ToolResult{Content: call.Arguments["path"].(string)})
	return ToolResult{Content: "done"}, nil
}

type arbitraryPreparingTool struct {
	calls []ai.ToolCall
}

func (tool *arbitraryPreparingTool) Name() string        { return "prepare_any" }
func (tool *arbitraryPreparingTool) Description() string { return "prepare arbitrary JSON" }
func (tool *arbitraryPreparingTool) PrepareArgumentsValue(arguments any) any {
	return []any{"flag", map[string]any{"ticket": json.Number("9007199254740993")}}
}
func (tool *arbitraryPreparingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	tool.calls = append(tool.calls, call)
	return ToolResult{Content: "done"}, nil
}

type arbitraryClassifyingTool struct {
	calls []ai.ToolCall
}

func (tool *arbitraryClassifyingTool) Name() string        { return "classify_any" }
func (tool *arbitraryClassifyingTool) Description() string { return "classify arbitrary JSON" }
func (tool *arbitraryClassifyingTool) PrepareArgumentsValue(arguments any) any {
	return []any{"deny"}
}
func (tool *arbitraryClassifyingTool) PermissionClassificationValue(arguments any) PermissionClassification {
	if args, ok := arguments.([]any); ok && len(args) == 1 && args[0] == "deny" {
		return PermissionBlock
	}
	return PermissionAllow
}
func (tool *arbitraryClassifyingTool) PermissionReasonValue(arguments any) string {
	if args, ok := arguments.([]any); ok && len(args) == 1 && args[0] == "deny" {
		return "blocked arbitrary args"
	}
	return ""
}
func (tool *arbitraryClassifyingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	tool.calls = append(tool.calls, call)
	return ToolResult{Content: "done"}, nil
}

type arbitraryPromptingTool struct{}

func (tool arbitraryPromptingTool) Name() string        { return "prompt_any" }
func (tool arbitraryPromptingTool) Description() string { return "prompt arbitrary JSON" }
func (tool arbitraryPromptingTool) PrepareArgumentsValue(arguments any) any {
	return []any{"prompt", map[string]any{"ticket": json.Number("9007199254740993")}}
}
func (tool arbitraryPromptingTool) PermissionClassificationValue(arguments any) PermissionClassification {
	return PermissionPrompt
}
func (tool arbitraryPromptingTool) PermissionReasonValue(arguments any) string {
	return "prompt arbitrary args"
}
func (tool arbitraryPromptingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	return ToolResult{Content: "prompt allowed"}, nil
}

type sleepingTool struct {
	name       string
	started    chan string
	release    <-chan struct{}
	sequential bool
}

func (tool sleepingTool) Name() string        { return tool.name }
func (tool sleepingTool) Description() string { return "sleep" }
func (tool sleepingTool) ExecutionMode() ToolExecutionMode {
	if tool.sequential {
		return ToolExecutionSequential
	}
	return ""
}
func (tool sleepingTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	tool.started <- tool.name
	<-tool.release
	return ToolResult{Content: tool.name}, nil
}

type orderedTool struct {
	name   string
	events *[]string
}

func (tool orderedTool) Name() string        { return tool.name }
func (tool orderedTool) Description() string { return tool.name }
func (tool orderedTool) Execute(ctx context.Context, call ai.ToolCall, update ToolUpdateFunc) (ToolResult, error) {
	*tool.events = append(*tool.events, "execute:"+call.Name)
	return ToolResult{Content: call.Name}, nil
}

type panickingTool struct{}

func (tool panickingTool) Name() string        { return "panic" }
func (tool panickingTool) Description() string { return "panic" }
func (tool panickingTool) Execute(context.Context, ai.ToolCall, ToolUpdateFunc) (ToolResult, error) {
	panic("boom")
}

func TestDefaultConvertToLLMDropsCustomMessages(t *testing.T) {
	messages := []Message{
		NewUserMessage("hello"),
		{Kind: MessageKindCustom, Custom: &CustomMessage{Role: "compaction_summary", Payload: map[string]any{"summary": "old"}}},
		NewAssistantMessage("world"),
	}

	got := DefaultConvertToLLM(messages)
	want := []ai.Message{
		{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hello"}}},
		{Role: ai.RoleAssistant, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "world"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("converted messages mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestAgentConvertToLLMUsesConfiguredConverter(t *testing.T) {
	ag := New(Options{Config: Config{ConvertToLLM: func(messages []Message) []ai.Message {
		return []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: messages[0].LLM.Content[0].Text + " converted"}}}}
	}}})
	got := ag.ConvertToLLM([]Message{NewUserMessage("hello")})
	if len(got) != 1 || got[0].Content[0].Text != "hello converted" {
		t.Fatalf("configured converter mismatch: %#v", got)
	}
}

func TestDefaultConvertToLLMPreservesToolResultMetadata(t *testing.T) {
	got := DefaultConvertToLLM([]Message{NewToolResultMessage(ToolResult{CallID: "call-1", Name: "read", Content: "failed", Error: "denied", Details: map[string]any{"exit_code": float64(1)}})})
	if len(got) != 1 || got[0].Role != ai.RoleTool || got[0].ToolCallID != "call-1" || got[0].ToolName != "read" || got[0].Name != "read" || !got[0].IsError || got[0].Details["exit_code"] != float64(1) {
		t.Fatalf("tool result metadata mismatch: %#v", got)
	}
}

func TestDefaultConvertToLLMFiltersToolResultContentLikeUpstream(t *testing.T) {
	got := DefaultConvertToLLM([]Message{NewToolResultMessage(ToolResult{CallID: "call-1", Name: "read", ContentBlocks: []ai.ContentBlock{
		{Type: ai.ContentThinking, Thinking: "plan"},
		{Type: ai.ContentText, Text: "ok"},
		{Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "nested", Name: "bad"}},
		{Type: ai.ContentImage, Data: "aW1n", MimeType: "image/png"},
	}})})
	if len(got) != 1 {
		t.Fatalf("converted messages mismatch: %#v", got)
	}
	if len(got[0].Content) != 2 || got[0].Content[0].Type != ai.ContentText || got[0].Content[1].Type != ai.ContentImage {
		t.Fatalf("tool result content should keep only upstream user blocks: %#v", got[0].Content)
	}
}

func TestAgentRunExecutesToolCallsAndPublishesEvents(t *testing.T) {
	tool := &recordingTool{}
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(tool.calls))
	}
	if len(state.Messages) != 3 {
		t.Fatalf("expected user, assistant, tool result messages, got %#v", state.Messages)
	}
	if state.Messages[2].Kind != MessageKindToolResult || state.Messages[2].ToolResult.Content != "ok:README.md" {
		t.Fatalf("tool result not appended: %#v", state.Messages[2])
	}
	if !hasEvent(events, EventTypeToolCall) || !hasEvent(events, EventTypeToolResult) || !hasEvent(events, EventTypeDone) {
		t.Fatalf("missing lifecycle events: %#v", events)
	}
}

func TestRunAgentLoopDelegatesToAgentRun(t *testing.T) {
	var seen []ai.Message
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			seen = append([]ai.Message(nil), llm...)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	state, err := RunAgentLoop(context.Background(), agent, []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Role != ai.RoleUser {
		t.Fatalf("run loop did not send new user message to model: %#v", seen)
	}
	if len(state.Messages) != 2 || state.Messages[0].Kind != MessageKindLLM || state.Messages[0].LLM.Role != ai.RoleUser || state.Messages[1].Kind != MessageKindLLM || state.Messages[1].LLM.Role != ai.RoleAssistant {
		t.Fatalf("run loop state mismatch: %#v", state.Messages)
	}
}

func TestRunAgentLoopContinueDelegatesToAgentContinue(t *testing.T) {
	var seen []ai.Message
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			seen = append([]ai.Message(nil), llm...)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "again"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	agent.ReplaceState(State{Messages: []Message{NewUserMessage("existing")}})

	state, err := RunAgentLoopContinue(context.Background(), agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Role != ai.RoleUser || seen[0].Content[0].Text != "existing" {
		t.Fatalf("continue loop did not reuse existing transcript: %#v", seen)
	}
	if len(state.Messages) != 2 || state.Messages[0].Kind != MessageKindLLM || state.Messages[0].LLM.Role != ai.RoleUser || state.Messages[1].Kind != MessageKindLLM || state.Messages[1].LLM.Role != ai.RoleAssistant {
		t.Fatalf("continue loop state mismatch: %#v", state.Messages)
	}
}

func TestAgentSubscribeReturnsUnsubscribe(t *testing.T) {
	var firstEvents []Event
	var secondEvents []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	unsubscribe := agent.Subscribe(func(event Event) { firstEvents = append(firstEvents, event) })
	agent.Subscribe(func(event Event) { secondEvents = append(secondEvents, event) })
	unsubscribe()
	unsubscribe()

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if len(firstEvents) != 0 || len(secondEvents) == 0 {
		t.Fatalf("unsubscribe mismatch: first=%#v second=%#v", firstEvents, secondEvents)
	}
}

func TestAgentSubscribeWithActiveDoneReceivesRunDoneChannel(t *testing.T) {
	var observed []<-chan struct{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	agent.SubscribeWithActiveDone(func(event Event, activeDone <-chan struct{}) {
		if event.Type == EventTypeStart || event.Type == EventTypeDone {
			observed = append(observed, activeDone)
		}
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 || observed[0] == nil || observed[1] == nil || observed[0] != observed[1] {
		t.Fatalf("active done observations mismatch: %#v", observed)
	}
	select {
	case <-observed[0]:
	default:
		t.Fatal("expected active done to close after run")
	}
}

func TestAgentStateSnapshotAndIsStreaming(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	agent := New(Options{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SystemPrompt: "sys",
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			close(started)
			<-release
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	initial := agent.State()
	if initial.Running || initial.SystemPrompt != "sys" || initial.Model == nil || initial.Model.ID != "fake" {
		t.Fatalf("initial agent state mismatch: %#v", initial)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
			t.Errorf("run failed: %v", err)
		}
	}()
	<-started
	if !agent.IsStreaming() {
		t.Fatal("expected agent to report streaming during run")
	}
	running := agent.State()
	if !running.Running || len(running.Messages) != 1 || running.Messages[0].LLM.Content[0].Text != "start" {
		t.Fatalf("running state mismatch: %#v", running)
	}
	close(release)
	<-done
	final := agent.State()
	if final.Running || len(final.Messages) != 2 || final.Messages[1].LLM.Content[0].Text != "ok" {
		t.Fatalf("final state mismatch: %#v", final)
	}
}

func TestAgentNewUsesInitialState(t *testing.T) {
	initial := State{
		SystemPrompt: "initial system",
		Model:        &ai.Model{ID: "initial", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Messages:     []Message{NewUserMessage("existing")},
	}
	var got []ai.Message
	agent := New(Options{
		InitialState: &initial,
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			got = append([]ai.Message(nil), llm...)
			if model.ID != "initial" {
				t.Fatalf("model mismatch: %#v", model)
			}
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	state := agent.State()
	if state.SystemPrompt != "initial system" || state.Model == nil || state.Model.ID != "initial" || len(state.Messages) != 1 {
		t.Fatalf("initial state mismatch: %#v", state)
	}
	if _, err := agent.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Role != ai.RoleSystem || got[0].Content[0].Text != "initial system" || got[1].Role != ai.RoleUser || got[1].Content[0].Text != "existing" {
		t.Fatalf("initial state transcript mismatch: %#v", got)
	}
}

func TestAgentRunRequiresModel(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	initial := agent.State()
	if initial.Model != nil {
		t.Fatalf("default state should not have a model: %#v", initial.Model)
	}
	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err == nil || err.Error() != "Agent has no model set; assign state.model first" {
		t.Fatalf("expected missing model error, got state=%#v err=%v", state, err)
	}
	if streamCalls != 0 || state.ErrorMessage != "Agent has no model set; assign state.model first" || state.Running {
		t.Fatalf("missing model state mismatch: streams=%d state=%#v", streamCalls, state)
	}
}

func TestAgentContinueRequiresExistingMessages(t *testing.T) {
	agent := New(Options{Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")}})
	state, err := agent.Continue(context.Background())
	if err == nil || err.Error() != "No messages to continue from" {
		t.Fatalf("expected empty continue error, got state=%#v err=%v", state, err)
	}
	if state.ErrorMessage != "" || state.Running {
		t.Fatalf("continue error state mismatch: %#v", state)
	}
	if current := agent.State(); current.ErrorMessage != "" || current.Running {
		t.Fatalf("empty continue should not mutate agent state like upstream: %#v", current)
	}
}

func TestAgentPromptWrappersMatchUpstreamFacade(t *testing.T) {
	var got []ai.Message
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			got = append([]ai.Message(nil), llm...)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	state, err := agent.Prompt(context.Background(), NewUserMessage("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Role != ai.RoleUser || got[0].Content[0].Text != "hello" {
		t.Fatalf("prompt wrapper transcript mismatch: %#v", got)
	}
	if len(state.Messages) != 2 || state.Messages[1].LLM.Content[0].Text != "ok" {
		t.Fatalf("prompt wrapper state mismatch: %#v", state)
	}

	got = nil
	state, err = agent.PromptMany(context.Background(), []Message{NewUserMessage("a"), NewUserMessage("b")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0].Content[0].Text != "hello" || got[1].Content[0].Text != "ok" || got[2].Content[0].Text != "a" || got[3].Content[0].Text != "b" {
		t.Fatalf("prompt_many wrapper transcript mismatch: %#v", got)
	}
	if len(state.Messages) != 5 {
		t.Fatalf("prompt_many wrapper state mismatch: %#v", state)
	}
}

func TestAgentContinueUsesExistingTranscriptWithoutRepublishingInputs(t *testing.T) {
	streamCalls := 0
	var streamLengths []int
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			streamLengths = append(streamLengths, len(llm))
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("turn-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	first, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Messages) != 2 {
		t.Fatalf("first run state mismatch: %#v", first)
	}
	events = nil
	continued, err := agent.Continue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(streamLengths, []int{1, 2}) || len(continued.Messages) != 3 || continued.Messages[2].LLM.Content[0].Text != "turn-2" {
		t.Fatalf("continue transcript mismatch: lengths=%#v state=%#v", streamLengths, continued)
	}
	if eventTypes(events)[0] != EventTypeStart || hasInitialUserMessageEvent(events) {
		t.Fatalf("continue republished existing inputs: %#v", eventTypes(events))
	}
}

func TestAgentRunRejectsConcurrentRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			if streamCalls == 1 {
				close(started)
				<-release
			}
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- err
	}()
	<-started
	state, err := agent.Run(context.Background(), []Message{NewUserMessage("second")})
	if err == nil || err.Error() != "agent is already streaming" {
		close(release)
		t.Fatalf("expected already streaming error, state=%#v err=%v", state, err)
	}
	if state.ErrorMessage != "" || !state.Running || streamCalls != 1 {
		close(release)
		t.Fatalf("concurrent run guard mismatch: state=%#v streams=%d", state, streamCalls)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAgentContinueRejectsConcurrentRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			close(started)
			<-release
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- err
	}()
	<-started
	state, err := agent.Continue(context.Background())
	if err == nil || err.Error() != "agent is already streaming" || state.ErrorMessage != "" {
		close(release)
		t.Fatalf("expected already streaming continue error, state=%#v err=%v", state, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAgentAbortCancelsActiveRun(t *testing.T) {
	started := make(chan struct{})
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	done := make(chan struct {
		state State
		err   error
	}, 1)
	go func() {
		state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- struct {
			state State
			err   error
		}{state: state, err: err}
	}()
	<-started
	if !agent.IsStreaming() {
		t.Fatal("expected active run before abort")
	}
	agent.Abort()
	agent.Abort()
	result := <-done
	if result.err == nil || result.err.Error() != "aborted" {
		t.Fatalf("expected canceled run, got state=%#v err=%v", result.state, result.err)
	}
	if agent.IsStreaming() || result.state.Running || result.state.ErrorMessage != "aborted" {
		t.Fatalf("abort state mismatch: returned=%#v current=%#v", result.state, agent.State())
	}
}

func TestAgentActiveDoneTracksActiveRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			close(started)
			<-release
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- err
	}()
	<-started
	activeDone := agent.ActiveDone()
	if activeDone == nil {
		close(release)
		t.Fatal("expected active done channel during run")
	}
	if activeToken := agent.ActiveToken(); activeToken != activeDone {
		close(release)
		t.Fatalf("active token should match active done channel, got %#v want %#v", activeToken, activeDone)
	}
	select {
	case <-activeDone:
		close(release)
		t.Fatal("active done channel closed before run ended")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if agent.ActiveDone() != nil {
		t.Fatalf("expected nil active done after run, got %#v", agent.ActiveDone())
	}
	if agent.ActiveToken() != nil {
		t.Fatalf("expected nil active token after run, got %#v", agent.ActiveToken())
	}
}

func TestAgentWaitIdleReturnsWhenActiveRunEnds(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			close(started)
			<-release
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	runDone := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		runDone <- err
	}()
	<-started
	waitDone := make(chan error, 1)
	go func() { waitDone <- agent.WaitIdle(context.Background()) }()
	select {
	case err := <-waitDone:
		close(release)
		t.Fatalf("wait idle returned before release: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	if err := <-waitDone; err != nil {
		t.Fatal(err)
	}
	if err := agent.WaitIdle(context.Background()); err != nil {
		t.Fatalf("idle wait should return immediately: %v", err)
	}
}

func TestAgentWaitIdleHonorsContextCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			close(started)
			<-release
			stream := ai.NewAssistantMessageEventStream()
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	runDone := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		runDone <- err
	}()
	<-started
	waitCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := agent.WaitIdle(waitCtx); !errors.Is(err, context.Canceled) {
		close(release)
		t.Fatalf("expected canceled wait, got %v", err)
	}
	close(release)
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
}

func TestAgentStateTracksPendingToolCalls(t *testing.T) {
	started := make(chan string, 1)
	release := make(chan struct{})
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{sleepingTool{name: "sleep", started: started, release: release}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "sleep", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("sleep")})
		done <- err
	}()
	if got := <-started; got != "sleep" {
		close(release)
		t.Fatalf("unexpected started tool: %s", got)
	}
	if pending := agent.State().PendingToolCalls; !reflect.DeepEqual(pending, []string{"call-1"}) {
		close(release)
		t.Fatalf("pending tool calls mismatch: %#v", pending)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if pending := agent.State().PendingToolCalls; len(pending) != 0 {
		t.Fatalf("expected pending tool calls to clear, got %#v", pending)
	}
}

func TestAgentStateTracksStreamingMessage(t *testing.T) {
	var streaming []Message
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "hel"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "lo"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) {
		if event.Type == EventTypeMessageUpdate {
			state := agent.State()
			if state.StreamingMessage != nil {
				streaming = append(streaming, *state.StreamingMessage)
			}
		}
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if len(streaming) != 2 || streaming[0].LLM.Content[0].Text != "hel" || streaming[1].LLM.Content[0].Text != "hello" {
		t.Fatalf("streaming messages mismatch: %#v", streaming)
	}
	if agent.State().StreamingMessage != nil {
		t.Fatalf("expected streaming message to clear, got %#v", agent.State().StreamingMessage)
	}
}

func TestAgentPublishesThinkingDeltaBeforeStreamCloses(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream().MarkLive()
	ready := make(chan struct{})
	update := make(chan ai.AssistantMessageEvent, 1)
	done := make(chan error, 1)
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			close(ready)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) {
		if event.Type == EventTypeMessageUpdate && event.AssistantMessageEvent != nil && event.AssistantMessageEvent.Type == ai.EventThinkingDelta {
			update <- *event.AssistantMessageEvent
		}
	})
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- err
	}()
	<-ready
	stream.Emit(ai.AssistantMessageEvent{Type: ai.EventThinkingDelta, Delta: "thinking"})
	select {
	case event := <-update:
		if event.Delta != "thinking" {
			t.Fatalf("thinking delta mismatch: %#v", event)
		}
	case err := <-done:
		t.Fatalf("run finished before live update: %v", err)
	case <-time.After(time.Second):
		t.Fatal("did not receive thinking delta before stream close")
	}
	stream.Close(ai.DoneReasonStop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAgentLiveStreamWithInitialEventWaitsForLaterDone(t *testing.T) {
	stream := ai.NewAssistantMessageEventStream().MarkLive()
	stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "hel"})
	ready := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			close(ready)
			return stream, nil
		},
	})
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- err
	}()
	<-ready
	select {
	case err := <-done:
		t.Fatalf("live stream ended before later terminal event: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "lo"})
	stream.Close(ai.DoneReasonStop)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	messages := agent.State().Messages
	if len(messages) != 2 || len(messages[1].LLM.Content) != 1 || messages[1].LLM.Content[0].Text != "hello" {
		t.Fatalf("live stream should include later delta before done: %#v", messages)
	}
}

func TestAgentStateExposesUpstreamIsStreamingAlias(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			close(started)
			<-release
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	done := make(chan struct {
		state State
		err   error
	}, 1)
	go func() {
		state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- struct {
			state State
			err   error
		}{state: state, err: err}
	}()
	<-started
	running := agent.State()
	if !running.Running || !running.IsStreaming || !agent.IsStreaming() {
		close(release)
		t.Fatalf("running state should expose both names: %#v", running)
	}
	close(release)
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.state.Running || result.state.IsStreaming || agent.State().IsStreaming {
		t.Fatalf("final state should clear both names: final=%#v current=%#v", result.state, agent.State())
	}
}

func TestAgentRunPublishesTurnLifecycleEvents(t *testing.T) {
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("hello")}); err != nil {
		t.Fatal(err)
	}
	types := eventTypes(events)
	want := []EventType{EventTypeStart, EventTypeMessageStart, EventTypeMessageEnd, EventTypeTurnStart, EventTypeMessageStart, EventTypeMessageUpdate, EventTypeAssistant, EventTypeMessageEnd, EventTypeTurnEnd, EventTypeDone}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("turn lifecycle events mismatch:\n got: %#v\nwant: %#v", types, want)
	}
	turnEnd := findEvent(events, EventTypeTurnEnd)
	if turnEnd == nil || turnEnd.Message == nil || turnEnd.Message.Kind != MessageKindLLM || len(turnEnd.ToolResults) != 0 {
		t.Fatalf("turn end payload mismatch: %#v", turnEnd)
	}
	done := findEvent(events, EventTypeDone)
	if done == nil || len(done.Messages) != 2 || done.Messages[0].LLM.Content[0].Text != "hello" || done.Messages[1].LLM.Content[0].Text != "ok" {
		t.Fatalf("done payload mismatch: %#v", done)
	}
}

func TestAgentRunPublishesMessageLifecycleEvents(t *testing.T) {
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "hel"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "lo"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}

	types := eventTypes(events)
	want := []EventType{
		EventTypeStart,
		EventTypeMessageStart,
		EventTypeMessageEnd,
		EventTypeTurnStart,
		EventTypeMessageStart,
		EventTypeMessageUpdate,
		EventTypeMessageUpdate,
		EventTypeAssistant,
		EventTypeMessageEnd,
		EventTypeTurnEnd,
		EventTypeDone,
	}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("message lifecycle events mismatch:\n got: %#v\nwant: %#v", types, want)
	}

	updates := filterEvents(events, EventTypeMessageUpdate)
	if len(updates) != 2 || updates[0].AssistantMessageEvent == nil || updates[0].AssistantMessageEvent.Delta != "hel" || updates[1].Message.LLM.Content[0].Text != "hello" {
		t.Fatalf("message update payload mismatch: %#v", updates)
	}
	ends := filterEvents(events, EventTypeMessageEnd)
	if len(ends) != 2 || ends[0].Message.LLM.Role != ai.RoleUser || ends[1].Message.LLM.Content[0].Text != "hello" {
		t.Fatalf("message end payload mismatch: %#v", ends)
	}
}

func TestAgentRunTurnEndIncludesToolResults(t *testing.T) {
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")}); err != nil {
		t.Fatal(err)
	}
	turnEnd := findEvent(events, EventTypeTurnEnd)
	if turnEnd == nil || len(turnEnd.ToolResults) != 1 || turnEnd.ToolResults[0].Content != "ok:README.md" {
		t.Fatalf("turn end tool results mismatch: %#v", turnEnd)
	}
}

func TestAgentRunShouldStopHookReceivesAssistantToolResultsAndContext(t *testing.T) {
	var stopCtx ShouldStopAfterTurnContext
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				stopCtx = turn
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 1 || len(state.Messages) != 3 {
		t.Fatalf("unexpected run shape: streams=%d state=%#v", streamCalls, state.Messages)
	}
	if stopCtx.Message.StopReason != ai.StopReasonToolCalls || len(stopCtx.ToolResults) != 1 || stopCtx.ToolResults[0].Content != "ok:README.md" {
		t.Fatalf("should-stop hook assistant/results mismatch: %#v", stopCtx)
	}
	if len(stopCtx.AgentContext.Messages) != 3 || len(stopCtx.NewMessages) != 3 || stopCtx.NewMessages[2].ToolResult.Content != "ok:README.md" {
		t.Fatalf("should-stop hook context/new messages mismatch: %#v", stopCtx)
	}
}

func TestAgentRunPrepareNextTurnHookReceivesAssistantAndContext(t *testing.T) {
	var prepareCtx PrepareNextTurnContext
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				prepareCtx = turn
				if streamCalls == 1 {
					return &AgentLoopTurnUpdate{Messages: []Message{NewUserMessage("continue")}}, nil
				}
				return nil, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("turn-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 2 || len(state.Messages) != 4 {
		t.Fatalf("unexpected run shape: streams=%d state=%#v", streamCalls, state.Messages)
	}
	if prepareCtx.Message.StopReason != ai.StopReasonEndTurn || len(prepareCtx.ToolResults) != 0 {
		t.Fatalf("prepare hook assistant/results mismatch: %#v", prepareCtx)
	}
	if len(prepareCtx.AgentContext.Messages) != 2 || len(prepareCtx.NewMessages) != 2 || prepareCtx.NewMessages[1].LLM.Content[0].Text != "turn-1" {
		t.Fatalf("prepare hook context/new messages mismatch: %#v", prepareCtx)
	}
}

func TestAgentRunPublishesToolExecutionEndBeforeToolResult(t *testing.T) {
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")}); err != nil {
		t.Fatal(err)
	}
	endIndex := eventIndex(events, EventTypeToolExecutionEnd)
	resultIndex := eventIndex(events, EventTypeToolResult)
	if endIndex < 0 || resultIndex < 0 || endIndex > resultIndex {
		t.Fatalf("tool execution end should be before tool result: %#v", eventTypes(events))
	}
	end := events[endIndex]
	if end.ToolCall == nil || end.ToolCall.ID != "call-1" || end.ToolCall.Name != "read" || end.ToolResult == nil || end.ToolResult.Content != "ok:README.md" || end.IsError {
		t.Fatalf("tool execution end payload mismatch: %#v", end)
	}
}

func TestAgentRunPublishesToolExecutionStartWithPreparedArguments(t *testing.T) {
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&preparingTool{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "prepare", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("prepare it")}); err != nil {
		t.Fatal(err)
	}
	startIndex := eventIndex(events, EventTypeToolExecutionStart)
	callIndex := eventIndex(events, EventTypeToolCall)
	if startIndex < 0 || callIndex < 0 || startIndex > callIndex {
		t.Fatalf("tool execution start should be before tool call: %#v", eventTypes(events))
	}
	start := events[startIndex]
	startArgs, _ := start.ToolArgs.(map[string]any)
	if start.ToolCall == nil || start.ToolCall.ID != "call-1" || start.ToolCall.Name != "prepare" || start.ToolCall.Arguments["path"] != "prepared.md" || startArgs["path"] != "prepared.md" {
		t.Fatalf("tool execution start payload mismatch: %#v", start)
	}
}

func TestAgentRunExecutesToolCallsFromAssistantContent(t *testing.T) {
	tool := &recordingTool{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 1 || len(state.Messages) != 3 || state.Messages[2].ToolResult == nil || state.Messages[2].ToolResult.Content != "ok:README.md" {
		t.Fatalf("assistant content tool call should execute: calls=%#v state=%#v", tool.calls, state.Messages)
	}
}

func TestAgentRunToolHooksReceiveAssistantArgsAndContext(t *testing.T) {
	var before BeforeToolCallContext
	var after AfterToolCallContext
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&preparingTool{}},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 1, nil
			},
			BeforeToolCall: func(ctx context.Context, call BeforeToolCallContext) (BeforeToolCallResult, error) {
				before = call
				return BeforeToolCallResult{}, nil
			},
			AfterToolCall: func(ctx context.Context, call AfterToolCallContext) (AfterToolCallResult, error) {
				after = call
				return AfterToolCallResult{}, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "prepare", Arguments: map[string]any{"raw": "value"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if before.AssistantMessage.StopReason != ai.StopReasonToolCalls || after.AssistantMessage.StopReason != ai.StopReasonToolCalls {
		t.Fatalf("assistant message missing from hooks: before=%#v after=%#v", before.AssistantMessage, after.AssistantMessage)
	}
	beforeArgs, _ := before.Args.(map[string]any)
	afterArgs, _ := after.Args.(map[string]any)
	if beforeArgs["path"] != "prepared.md" || afterArgs["path"] != "prepared.md" || before.Call.Arguments["path"] != "prepared.md" {
		t.Fatalf("prepared args missing from hooks: before=%#v after=%#v", before, after)
	}
	if len(before.AgentContext.Messages) != 2 || before.AgentContext.Messages[0].LLM.Content[0].Text != "start" || len(before.AgentContext.Tools) != 1 {
		t.Fatalf("before hook context mismatch: %#v", before.AgentContext)
	}
	if len(after.AgentContext.Messages) != 2 || after.Result.Content != "prepared.md" || !reflect.DeepEqual(before.AgentContext.Messages, after.AgentContext.Messages) {
		t.Fatalf("after hook context mismatch: %#v", after)
	}
}

func TestAgentRunRunsAllBeforeHooksBeforeSequentialToolExecution(t *testing.T) {
	events := []string{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{orderedTool{name: "first", events: &events}, orderedTool{name: "second", events: &events}},
		Config: Config{
			ToolExecution: ToolExecutionSequential,
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				events = append(events, "before:"+before.Call.Name)
				return BeforeToolCallResult{}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "first", Arguments: map[string]any{}}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-2", Name: "second", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"before:first", "before:second", "execute:first", "execute:second"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("tool preflight order mismatch: got %#v want %#v", events, want)
	}
}

func TestAgentRunAfterHookCanPatchBeforeHookError(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{}, fmt.Errorf("before boom")
			},
			AfterToolCall: func(ctx context.Context, after AfterToolCallContext) (AfterToolCallResult, error) {
				if after.Result.Error != "before boom" || after.Call.Name != "read" {
					t.Fatalf("after hook saw wrong before error: %#v", after)
				}
				return AfterToolCallResult{Content: testStringPtr("patched before error"), IsError: boolPtr(false)}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "patched before error" || result.Error != "" {
		t.Fatalf("patched before error mismatch: %#v", result)
	}
}

func TestAgentRunToolExecutionEndMarksErrors(t *testing.T) {
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{failingToolWithDetails{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")}); err != nil {
		t.Fatal(err)
	}
	end := findEvent(events, EventTypeToolExecutionEnd)
	if end == nil || !end.IsError || end.ToolResult == nil || end.ToolResult.Error != context.Canceled.Error() {
		t.Fatalf("tool execution error end mismatch: %#v", end)
	}
}

func TestAgentRunPreparesToolArgumentsBeforeHookAndExecute(t *testing.T) {
	tool := &preparingTool{}
	var hookCall ai.ToolCall
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				hookCall = before.Call
				return BeforeToolCallResult{}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "prepare", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("prepare it")})
	if err != nil {
		t.Fatal(err)
	}
	if hookCall.Arguments["path"] != "prepared.md" {
		t.Fatalf("before hook saw raw arguments: %#v", hookCall.Arguments)
	}
	if len(tool.calls) != 1 || tool.calls[0].Arguments["path"] != "prepared.md" {
		t.Fatalf("execute saw raw arguments: %#v", tool.calls)
	}
	if got := state.Messages[2].ToolResult.Content; got != "prepared.md" {
		t.Fatalf("tool result content mismatch: %s", got)
	}
}

func TestAgentRunArbitraryPreparedArgsMatchUpstreamValueSemantics(t *testing.T) {
	tool := &arbitraryPreparingTool{}
	var startArgs any
	var hookCall ai.ToolCall
	var hookArgs any
	var afterArgs any
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				hookCall = before.Call
				hookArgs = before.Args
				return BeforeToolCallResult{}, nil
			},
			AfterToolCall: func(ctx context.Context, after AfterToolCallContext) (AfterToolCallResult, error) {
				afterArgs = after.Args
				return AfterToolCallResult{}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "prepare_any", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) {
		if event.Type == EventTypeToolExecutionStart {
			startArgs = event.ToolArgs
		}
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("prepare it")}); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(startArgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("start event should preserve arbitrary prepared args like upstream: %s", encoded)
	}
	if len(hookCall.Arguments) != 0 || len(tool.calls) != 1 || len(tool.calls[0].Arguments) != 0 {
		t.Fatalf("non-object prepared args should clear tool call argument map like upstream: hook=%#v calls=%#v", hookCall, tool.calls)
	}
	encodedHookArgs, err := json.Marshal(hookArgs)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedHookArgs) != `["flag",{"ticket":9007199254740993}]` {
		t.Fatalf("before hook args should expose prepared arbitrary JSON value like upstream: %s", encodedHookArgs)
	}
	encodedAfterArgs, err := json.Marshal(afterArgs)
	if err != nil {
		t.Fatal(err)
	}
	if string(encodedAfterArgs) != `["flag",{"ticket":9007199254740993}]` {
		t.Fatalf("after hook args should expose prepared arbitrary JSON value like upstream: %s", encodedAfterArgs)
	}
}

func TestAgentRunValuePermissionClassifierSeesPreparedJSONValueLikeUpstream(t *testing.T) {
	tool := &arbitraryClassifyingTool{}
	hookCalled := false
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				hookCalled = true
				return BeforeToolCallResult{}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "classify_any", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("classify it")})
	if err != nil {
		t.Fatal(err)
	}
	if hookCalled || len(tool.calls) != 0 {
		t.Fatalf("blocked value-classified tool should skip hook and execute: hook=%v calls=%#v", hookCalled, tool.calls)
	}
	if got := state.Messages[2].ToolResult; got == nil || !got.IsError || got.Content != "blocked arbitrary args" {
		t.Fatalf("value classifier should block like upstream, got %#v", got)
	}
}

func TestAgentRunToolPermissionDenySkipsHookAndExecute(t *testing.T) {
	tool := &denyingTool{}
	hookCalled := false
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				hookCalled = true
				return BeforeToolCallResult{}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "deny", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("deny it")})
	if err != nil {
		t.Fatal(err)
	}
	if hookCalled {
		t.Fatal("before hook should not run for denied tool")
	}
	if tool.calls != 0 {
		t.Fatalf("denied tool executed %d times", tool.calls)
	}
	result := state.Messages[2].ToolResult
	if result.Error == "" || result.CallID != "1" || result.Name != "deny" {
		t.Fatalf("denied result mismatch: %#v", result)
	}
}

func TestAgentRunPermissionDenyUsesClassifierReasonLikeUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{denyingReasonTool{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "deny_reason", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("deny it")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "custom deny reason" || result.Error != "custom deny reason" {
		t.Fatalf("denied result should use classifier reason: %#v", result)
	}
}

func TestAfterToolCallCanPatchPermissionDeniedResult(t *testing.T) {
	tool := &denyingTool{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				if turn.Result.Error != "tool call denied" || turn.Call.ID != "1" || turn.Call.Name != "deny" {
					t.Fatalf("after hook saw wrong denied context: %#v", turn)
				}
				return AfterToolCallResult{Content: testStringPtr("patched deny"), IsError: boolPtr(false)}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "deny", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("deny it")})
	if err != nil {
		t.Fatal(err)
	}
	if tool.calls != 0 {
		t.Fatalf("denied tool executed %d times", tool.calls)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "patched deny" || result.Error != "" {
		t.Fatalf("patched deny result mismatch: %#v", result)
	}
}

func TestAgentRunToolPermissionAskFailsClosedWithoutPromptHook(t *testing.T) {
	tool := &askingTool{}
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "ask", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 0 {
		t.Fatalf("ask tool executed without prompt hook: %#v", tool.calls)
	}
	result := state.Messages[2].ToolResult
	if result.Error == "" || result.CallID != "1" || result.Name != "ask" {
		t.Fatalf("ask fail-closed result mismatch: %#v", result)
	}
	if result.Error != "control-plane prompt required but no on_control_plane_prompt hook configured (fail-closed deny — see issue #110 design v0.2)" {
		t.Fatalf("fail-closed reason mismatch: %q", result.Error)
	}
	event := findEvent(events, EventTypeControlPlanePromptResolved)
	if event == nil || event.ControlPlanePromptDecision != ControlPlaneDeny || event.ControlPlanePromptReason != result.Error {
		t.Fatalf("fail-closed prompt event mismatch: %#v", events)
	}
}

func TestAgentRunToolPermissionAskUsesPromptHookDecision(t *testing.T) {
	tool := &askingTool{}
	var prompt ControlPlanePromptRequest
	var events []Event
	beforeCalled := false
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				beforeCalled = true
				return BeforeToolCallResult{}, nil
			},
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				prompt = request
				return ControlPlaneAllow, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "ask", Arguments: map[string]any{"b": float64(2), "a": "one"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 1 {
		t.Fatalf("ask tool was not executed after allow: %#v", tool.calls)
	}
	if !beforeCalled {
		t.Fatal("before hook should run before prompt resolution")
	}
	if prompt.ToolCallID != "1" || prompt.ToolName != "ask" || prompt.ArgsHash == "" || prompt.Label == "" || prompt.Payload == nil {
		t.Fatalf("prompt request mismatch: %#v", prompt)
	}
	promptPayload, _ := prompt.Payload.(map[string]any)
	if !reflect.DeepEqual(promptPayload["args_keys"], []string{"a", "b"}) || promptPayload["args_hash"] != prompt.ArgsHash || promptPayload["tool_name"] != "ask" {
		t.Fatalf("default prompt payload mismatch: %#v", prompt.Payload)
	}
	if _, ok := promptPayload["arguments"]; ok {
		t.Fatalf("default prompt payload should not include raw arguments: %#v", prompt.Payload)
	}
	event := findEvent(events, EventTypeControlPlanePromptResolved)
	if event == nil || event.ControlPlanePromptDecision != ControlPlaneAllow || event.ControlPlanePrompt == nil || event.ControlPlanePrompt.ArgsHash != prompt.ArgsHash {
		t.Fatalf("missing allow prompt event: %#v", events)
	}
	if state.Messages[2].ToolResult.Content != "allowed" {
		t.Fatalf("allowed result mismatch: %#v", state.Messages[2].ToolResult)
	}
}

func TestDefaultPromptPayloadBoundsArgumentKeys(t *testing.T) {
	arguments := make(map[string]any)
	for index := 0; index < 40; index++ {
		arguments[fmt.Sprintf("key-%02d", index)] = index
	}
	longKey := strings.Repeat("长", 70)
	arguments[longKey] = true

	payload := defaultPromptPayload(ai.ToolCall{Name: "ask", Arguments: arguments})
	keys, ok := payload["args_keys"].([]string)
	if !ok {
		t.Fatalf("args_keys type mismatch: %#v", payload["args_keys"])
	}
	if len(keys) != 32 {
		t.Fatalf("expected 32 bounded keys, got %d: %#v", len(keys), keys)
	}
	for index, key := range keys {
		want := fmt.Sprintf("key-%02d", index)
		if key != want {
			t.Fatalf("expected sorted first 32 keys, got index %d = %q want %q in %#v", index, key, want, keys)
		}
	}
	for _, key := range keys {
		if len([]rune(key)) > 65 {
			t.Fatalf("key was not truncated: %q", key)
		}
	}
}

func TestHashToolArgumentsDoesNotHTMLEscapeLikeSerdeJSON(t *testing.T) {
	got := hashToolArguments(map[string]any{"url": "https://example.test?a=1&b=<tag>"})
	if got != "e95c4d5a4ee74889854144cb943baba8876d0f3e02e5b7d4e0e10e3d1704638b" {
		t.Fatalf("args hash should match serde_json without HTML escaping, got %s", got)
	}
}

func TestAgentRunBeforeHookCanDenyPermissionAsk(t *testing.T) {
	tool := &askingTool{}
	promptCalled := false
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{Skip: true, Result: &ToolResult{Content: "blocked by hook", Error: "blocked by hook"}}, nil
			},
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				promptCalled = true
				return ControlPlaneAllow, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "ask", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")})
	if err != nil {
		t.Fatal(err)
	}
	if promptCalled {
		t.Fatal("prompt hook should not run after before hook blocks")
	}
	if len(tool.calls) != 0 {
		t.Fatalf("blocked ask tool executed: %#v", tool.calls)
	}
	if state.Messages[2].ToolResult.Error != "blocked by hook" {
		t.Fatalf("blocked result mismatch: %#v", state.Messages[2].ToolResult)
	}
}

func TestBeforeToolCallSkipResultNormalizesContentBlocksLikeUpstream(t *testing.T) {
	var after AfterToolCallContext
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{Skip: true, Result: &ToolResult{ContentBlocks: []ai.ContentBlock{{Type: ai.ContentThinking, Thinking: "plan"}, {Type: ai.ContentText, Text: "skipped"}, {Type: ai.ContentImage, Data: "aW1n", MimeType: "image/png"}}, DetailsValue: []any{"trace"}}}, nil
			},
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				after = turn
				return AfterToolCallResult{}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	if after.Result.CallID != "call-1" || after.Result.Name != "read" || after.Result.Content != "skipped" || len(after.Result.ContentBlocks) != 2 || after.Result.Details != nil || len(after.Result.DetailsValue.([]any)) != 1 {
		t.Fatalf("after hook should receive normalized skipped result: %#v", after.Result)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "skipped" || len(result.ContentBlocks) != 2 || result.Details != nil {
		t.Fatalf("skipped result should stay normalized: %#v", result)
	}
}

func TestAgentRunBeforeHookBlockWinsOverSkipResult(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{Block: true, Reason: "blocked", Skip: true, Result: &ToolResult{Content: "skipped"}}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "blocked" || result.Error != "blocked" || !result.IsError {
		t.Fatalf("block should win over skip result: %#v", result)
	}
}

func TestAgentRunBeforeHookBlockWinsOverPrompt(t *testing.T) {
	tool := &askingTool{}
	promptCalled := false
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{
					Block:  true,
					Reason: "blocked reason",
					Prompt: &ControlPlanePromptRequest{Label: "should not prompt"},
				}, nil
			},
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				promptCalled = true
				return ControlPlaneAllow, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "ask", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")})
	if err != nil {
		t.Fatal(err)
	}
	if promptCalled {
		t.Fatal("prompt hook should not run when before hook blocks")
	}
	if len(tool.calls) != 0 {
		t.Fatalf("blocked tool executed: %#v", tool.calls)
	}
	result := state.Messages[2].ToolResult
	if result.CallID != "1" || result.Name != "ask" || result.Error != "blocked reason" || result.Content != "blocked reason" {
		t.Fatalf("blocked result mismatch: %#v", result)
	}
}

func TestAgentRunBeforeHookBlockUsesDefaultReason(t *testing.T) {
	tool := &recordingTool{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{Block: true}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 0 {
		t.Fatalf("blocked tool executed: %#v", tool.calls)
	}
	if got := state.Messages[2].ToolResult.Error; got != "tool call blocked by before_tool_call hook" {
		t.Fatalf("default block reason mismatch: %q", got)
	}
}

func TestAgentRunClassifierPromptKeepsReasonWhenHookSuppliesPrompt(t *testing.T) {
	tool := &askingTool{}
	var prompt ControlPlanePromptRequest
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{Prompt: &ControlPlanePromptRequest{
					ToolCallID: "forged",
					ToolName:   "forged",
					ArgsHash:   "forged",
					Label:      "custom label",
					Payload:    map[string]any{"custom": true},
					Reason:     "hook reason",
				}}, nil
			},
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				prompt = request
				return ControlPlaneAllow, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "ask", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	_, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.ToolCallID != "1" || prompt.ToolName != "ask" || prompt.ArgsHash == "" || prompt.ArgsHash == "forged" {
		t.Fatalf("prompt binding mismatch: %#v", prompt)
	}
	promptPayload, _ := prompt.Payload.(map[string]any)
	if prompt.Label != "custom label" || promptPayload["custom"] != true {
		t.Fatalf("prompt enrichment mismatch: %#v", prompt)
	}
	if prompt.Reason != "tool requires confirmation" {
		t.Fatalf("classifier prompt reason should win, got %q", prompt.Reason)
	}
}

func TestAgentRunPermissionPromptUsesClassifierReasonLikeUpstream(t *testing.T) {
	var prompt ControlPlanePromptRequest
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{askingReasonTool{}},
		Config: Config{
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				prompt = request
				return ControlPlaneAllow, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "ask_reason", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")}); err != nil {
		t.Fatal(err)
	}
	if prompt.Reason != "custom prompt reason" {
		t.Fatalf("prompt should use classifier reason, got %#v", prompt)
	}
}

func TestAgentRunPermissionPromptUsesPreparedJSONValueLikeUpstream(t *testing.T) {
	var prompt ControlPlanePromptRequest
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{arbitraryPromptingTool{}},
		Config: Config{
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				prompt = request
				return ControlPlaneAllow, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "prompt_any", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("prompt it")})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := prompt.Payload.(map[string]any)
	if prompt.Reason != "prompt arbitrary args" || payload["args_hash"] == hashToolArguments(map[string]any{}) || state.Messages[2].ToolResult.Content != "prompt allowed" {
		t.Fatalf("prompt should use prepared arbitrary args like upstream prompt=%#v result=%#v", prompt, state.Messages[2].ToolResult)
	}
}

func TestAgentRunPermissionPromptNameMatchesUpstream(t *testing.T) {
	var prompt ControlPlanePromptRequest
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{promptingTool{}},
		Config: Config{
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				prompt = request
				return ControlPlaneAllow, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "prompt", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("prompt it")})
	if err != nil {
		t.Fatal(err)
	}
	if prompt.ToolName != "prompt" || state.Messages[2].ToolResult.Content != "prompt allowed" {
		t.Fatalf("permission prompt mismatch prompt=%#v result=%#v", prompt, state.Messages[2].ToolResult)
	}
}

func TestAgentRunPermissionBlockNameMatchesUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{blockingTool{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "block", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("block it")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Error == "" || result.Content == "should not run" {
		t.Fatalf("permission block mismatch: %#v", result)
	}
}

func TestAgentRunUnknownPermissionClassificationFailsClosedLikeUpstreamEnum(t *testing.T) {
	tool := &unknownPermissionTool{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "unknown_permission", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("run unknown permission")})
	if err != nil {
		t.Fatal(err)
	}
	if tool.calls != 0 {
		t.Fatalf("unknown permission classification should not execute tool, calls=%d", tool.calls)
	}
	result := state.Messages[2].ToolResult
	if result.Error != "tool call denied" || result.Content != "tool call denied" {
		t.Fatalf("unknown permission should fail closed like upstream enum: %#v", result)
	}
}

func TestAgentRunBeforeHookCanRaisePromptForAllowedTool(t *testing.T) {
	tool := &recordingTool{}
	var prompt ControlPlanePromptRequest
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{Prompt: &ControlPlanePromptRequest{
					ToolCallID: "forged",
					ToolName:   "forged",
					ArgsHash:   "forged",
					Label:      "review read",
					Payload:    map[string]any{"path": before.Call.Arguments["path"]},
					Reason:     "hook raised prompt",
				}}, nil
			},
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				prompt = request
				return ControlPlaneAllow, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 1 || state.Messages[2].ToolResult.Content != "ok:README.md" {
		t.Fatalf("allowed prompted tool mismatch: calls=%#v state=%#v", tool.calls, state.Messages[2].ToolResult)
	}
	if prompt.ToolCallID != "1" || prompt.ToolName != "read" || prompt.ArgsHash == "" || prompt.ArgsHash == "forged" {
		t.Fatalf("prompt binding mismatch: %#v", prompt)
	}
	promptPayload, _ := prompt.Payload.(map[string]any)
	if prompt.Label != "review read" || promptPayload["path"] != "README.md" || prompt.Reason != "hook raised prompt" {
		t.Fatalf("hook prompt fields mismatch: %#v", prompt)
	}
}

func TestAgentRunBeforeHookPromptBindsPreparedJSONValueLikeUpstream(t *testing.T) {
	tool := &arbitraryPreparingTool{}
	var prompt ControlPlanePromptRequest
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{Prompt: &ControlPlanePromptRequest{
					ToolCallID: "forged",
					ToolName:   "forged",
					ArgsHash:   "forged",
					Label:      "review arbitrary",
					Payload:    []any{"hook"},
					Reason:     "hook raised prompt",
				}}, nil
			},
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				prompt = request
				return ControlPlaneAllow, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{
				ID: "1", Name: "prepare_any", Arguments: map[string]any{"path": "raw.md"},
			}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("review it")}); err != nil {
		t.Fatal(err)
	}
	if prompt.ToolCallID != "1" || prompt.ToolName != "prepare_any" || prompt.ArgsHash == "forged" || prompt.ArgsHash == hashToolArguments(map[string]any{}) {
		t.Fatalf("hook prompt should bind prepared arbitrary args like upstream: %#v", prompt)
	}
}

func TestAgentRunControlPlanePromptDenySkipsTool(t *testing.T) {
	tool := &askingTool{}
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			OnControlPlanePrompt: DenyControlPlanePromptHook("no ui"),
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "ask", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 0 {
		t.Fatalf("denied ask tool executed: %#v", tool.calls)
	}
	if got := state.Messages[2].ToolResult.Error; got != "no ui" {
		t.Fatalf("deny result mismatch: %q", got)
	}
	event := findEvent(events, EventTypeControlPlanePromptResolved)
	if event == nil || event.ControlPlanePromptDecision != ControlPlaneDeny || event.ControlPlanePromptReason != "no ui" {
		t.Fatalf("missing deny prompt reason event: %#v", events)
	}
}

func TestAgentRunControlPlanePromptTimeoutSkipsTool(t *testing.T) {
	tool := &askingTool{}
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				return ControlPlaneTimeout, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "ask", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 0 {
		t.Fatalf("timed out ask tool executed: %#v", tool.calls)
	}
	if got := state.Messages[2].ToolResult.Error; got != "control-plane prompt timed out — tool call denied" {
		t.Fatalf("timeout result mismatch: %q", got)
	}
	event := findEvent(events, EventTypeControlPlanePromptResolved)
	if event == nil || event.ControlPlanePrompt == nil || event.ControlPlanePromptDecision != ControlPlaneTimeout {
		t.Fatalf("missing timeout prompt event: %#v", events)
	}
	if event.ControlPlanePrompt.ToolCallID != "1" || event.ControlPlanePrompt.ToolName != "ask" || event.ControlPlanePrompt.ArgsHash == "" {
		t.Fatalf("prompt event request mismatch: %#v", event.ControlPlanePrompt)
	}
}

func TestAgentRunControlPlanePromptUnknownDecisionFailsClosedLikeUpstreamEnum(t *testing.T) {
	tool := &askingTool{}
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				setControlPlanePromptReason(ctx, "invalid decision")
				return ControlPlanePromptDecision("future"), nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "1", Name: "ask", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 0 {
		t.Fatalf("unknown decision ask tool executed: %#v", tool.calls)
	}
	if got := state.Messages[2].ToolResult.Error; got != "invalid decision" {
		t.Fatalf("unknown decision result mismatch: %q", got)
	}
	event := findEvent(events, EventTypeControlPlanePromptResolved)
	if event == nil || event.ControlPlanePromptDecision != ControlPlaneDeny || event.ControlPlanePromptReason != "invalid decision" {
		t.Fatalf("unknown prompt decision should audit as fail-closed deny like upstream enum: %#v", event)
	}
}

func TestAgentRunPreservesAssistantMetadata(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventMetadata, ResponseID: "resp-1", ResponseModel: "served"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "ok"}})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	state, err := agent.Run(context.Background(), []Message{NewUserMessage("hello")})
	if err != nil {
		t.Fatal(err)
	}
	stored := state.Messages[1].LLM
	if stored.ResponseID != "resp-1" || stored.ResponseModel != "served" || stored.Timestamp == 0 {
		t.Fatalf("assistant metadata mismatch: %#v", stored)
	}
}

func TestAgentRunErroredToolResultUsesErrorOnlyLikeUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{failingToolWithDetails{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	state, err := agent.Run(context.Background(), []Message{NewUserMessage("hello")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != context.Canceled.Error() || result.Error != context.Canceled.Error() || result.Details != nil {
		t.Fatalf("tool result mismatch: %#v", result)
	}
}

func TestAgentRunContinuesAfterToolCalls(t *testing.T) {
	tool := &recordingTool{}
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			if streamCalls == 1 {
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
				stream.Close(ai.DoneReasonToolCalls)
				return stream, nil
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "done"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 2 || len(tool.calls) != 1 {
		t.Fatalf("expected two stream calls and one tool call, streams=%d tools=%d", streamCalls, len(tool.calls))
	}
	if state.Messages[len(state.Messages)-1].LLM.Content[0].Text != "done" {
		t.Fatalf("final assistant mismatch: %#v", state.Messages)
	}
}

func TestAgentRunPrepareNextTurnCanAppendMessages(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "turn"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				if streamCalls == 1 {
					return &AgentLoopTurnUpdate{Messages: []Message{NewUserMessage("continue")}}, nil
				}
				return nil, nil
			},
		},
	})
	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 2 {
		t.Fatalf("expected prepare_next_turn to trigger second stream, got %d", streamCalls)
	}
	if len(state.Messages) != 4 || state.Messages[2].LLM.Content[0].Text != "continue" {
		t.Fatalf("next turn message mismatch: %#v", state.Messages)
	}
}

func TestAgentRunPrepareNextTurnCanUpdateModelAndThinkingLevel(t *testing.T) {
	streamCalls := 0
	seenModels := make([]string, 0, 2)
	seenThinking := make([]ai.ThinkingLevel, 0, 2)
	nextModel := ai.Model{ID: "next", Provider: ai.Provider("test"), API: ai.Api("fake")}
	nextThinking := ai.ThinkingHigh
	agent := New(Options{
		Model: ai.Model{ID: "initial", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			seenModels = append(seenModels, model.ID)
			seenThinking = append(seenThinking, options.ThinkingLevel)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("turn-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				if streamCalls == 1 {
					return &AgentLoopTurnUpdate{Messages: []Message{NewUserMessage("next")}, Model: &nextModel, ThinkingLevel: &nextThinking}, nil
				}
				return nil, nil
			},
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seenModels, []string{"initial", "next"}) || !reflect.DeepEqual(seenThinking, []ai.ThinkingLevel{"", ai.ThinkingHigh}) {
		t.Fatalf("turn update not applied: models=%#v thinking=%#v", seenModels, seenThinking)
	}
	if len(state.Messages) != 4 || state.Messages[2].LLM.Content[0].Text != "next" {
		t.Fatalf("turn update messages mismatch: %#v", state.Messages)
	}
}

func TestAgentRunStateTracksModelThinkingAndTools(t *testing.T) {
	streamCalls := 0
	var firstTurn PrepareNextTurnContext
	nextModel := ai.Model{ID: "next", Provider: ai.Provider("test"), API: ai.Api("fake")}
	nextThinking := ai.ThinkingHigh
	nextTool := &recordingTool{}
	agent := New(Options{
		Model: ai.Model{ID: "initial", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{fakeTool{name: "old"}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "turn"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				if streamCalls == 1 {
					firstTurn = turn
					return &AgentLoopTurnUpdate{Messages: []Message{NewUserMessage("next")}, Model: &nextModel, ThinkingLevel: &nextThinking, Context: &AgentContext{Tools: []Tool{nextTool}}}, nil
				}
				return nil, nil
			},
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if firstTurn.State.Model == nil || firstTurn.State.Model.ID != "initial" || len(firstTurn.State.Tools) != 1 || firstTurn.State.Tools[0].Name() != "old" || firstTurn.AgentContext.State.Model == nil || firstTurn.AgentContext.State.Model.ID != "initial" {
		t.Fatalf("initial state snapshot mismatch: %#v", firstTurn)
	}
	if state.Model == nil || state.Model.ID != "next" || state.ThinkingLevel == nil || *state.ThinkingLevel != ai.ThinkingHigh || len(state.Tools) != 1 || state.Tools[0].Name() != "read" {
		t.Fatalf("final state runtime fields mismatch: %#v", state)
	}
}

func TestAgentRunStateCapturesStreamErrorMessage(t *testing.T) {
	boom := fmt.Errorf("stream boom")
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			return nil, boom
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if !errors.Is(err, boom) {
		t.Fatalf("expected stream error, got %v", err)
	}
	if state.Running || state.ErrorMessage != "stream boom" {
		t.Fatalf("state did not capture stream error: %#v", state)
	}
}

func TestAgentRunStateCapturesStreamErrorEventMessage(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "partial"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventError, Error: "provider boom"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err == nil || err.Error() != "provider boom" {
		t.Fatalf("expected stream error event, got state=%#v err=%v", state, err)
	}
	if state.Running || state.ErrorMessage != "provider boom" {
		t.Fatalf("state did not capture stream error event: %#v", state)
	}
}

func TestAgentRunStateCapturesStreamErrorEventMessageFromAssistantMessage(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventError, Message: &ai.AssistantMessage{ErrorMessage: "HTTP 401 Unauthorized: missing api key"}})
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err == nil || err.Error() != "HTTP 401 Unauthorized: missing api key" {
		t.Fatalf("expected assistant error message, got state=%#v err=%v", state, err)
	}
	if state.Running || state.ErrorMessage != "HTTP 401 Unauthorized: missing api key" {
		t.Fatalf("state did not capture assistant error message: %#v", state)
	}
}

func TestAgentRunStreamErrorEventStopsConsumingLaterEvents(t *testing.T) {
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "before"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventError, Error: "provider boom"})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "after"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })

	_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err == nil || err.Error() != "provider boom" {
		t.Fatalf("expected stream error event, got %v", err)
	}
	updates := filterEvents(events, EventTypeMessageUpdate)
	if len(updates) != 1 || updates[0].Message == nil || updates[0].Message.LLM == nil || len(updates[0].Message.LLM.Content) != 1 || updates[0].Message.LLM.Content[0].Text != "before" {
		t.Fatalf("stream should stop at error event, updates=%#v", updates)
	}
}

func TestAgentRunAcceptsPartialStreamWithoutDoneLikeUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "partial"})
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 2 || state.Messages[1].LLM.Content[0].Text != "partial" {
		t.Fatalf("partial stream result mismatch: %#v", state.Messages)
	}
}

func TestAgentRunEmptyStreamReportsNoMessageLikeUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			return ai.NewAssistantMessageEventStream(), nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err == nil || err.Error() != "LLM stream produced no message" {
		t.Fatalf("expected upstream empty stream error, state=%#v err=%v", state, err)
	}
	if state.ErrorMessage != "LLM stream produced no message" {
		t.Fatalf("state error mismatch: %#v", state)
	}
}

func TestAgentRunStateCapturesHookErrorMessage(t *testing.T) {
	boom := fmt.Errorf("hook boom")
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Config: Config{ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return false, boom
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if !errors.Is(err, boom) {
		t.Fatalf("expected hook error, got %v", err)
	}
	if state.Running || state.ErrorMessage != "hook boom" {
		t.Fatalf("state did not capture hook error: %#v", state)
	}
}

func TestAgentRunPassesConfiguredStreamOptions(t *testing.T) {
	temperature := 0.2
	maxRetries := 3
	var seenOptions ai.SimpleStreamOptions
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		StreamOptions: ai.SimpleStreamOptions{Base: ai.StreamOptions{
			APIKey:         "api-key",
			MaxTokens:      123,
			Temperature:    &temperature,
			Transport:      ai.TransportHTTP,
			CacheRetention: ai.CacheLong,
			SessionID:      "session-1",
			Headers:        map[string]string{"X-Test": "yes"},
			TimeoutMS:      5000,
			MaxRetries:     &maxRetries,
			Metadata:       map[string]any{"trace": "abc"},
			ProviderExtras: map[string]any{"extra": "value"},
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			seenOptions = options
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if seenOptions.Base.APIKey != "api-key" || seenOptions.Base.MaxTokens != 123 || seenOptions.Base.Temperature != &temperature || seenOptions.Base.Transport != ai.TransportHTTP || seenOptions.Base.CacheRetention != ai.CacheLong || seenOptions.Base.SessionID != "session-1" || seenOptions.Base.Headers["X-Test"] != "yes" || seenOptions.Base.TimeoutMS != 5000 || seenOptions.Base.MaxRetries != &maxRetries || seenOptions.Base.Metadata["trace"] != "abc" || seenOptions.Base.ProviderExtras["extra"] != "value" {
		t.Fatalf("stream options mismatch: %#v", seenOptions)
	}
}

func TestAgentRunPassesOptionSessionIDToStream(t *testing.T) {
	var seenOptions ai.SimpleStreamOptions
	agent := New(Options{
		Model:     ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SessionID: "agent-session",
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			seenOptions = options
			stream := ai.NewAssistantMessageEventStream()
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if seenOptions.Base.SessionID != "agent-session" {
		t.Fatalf("session id mismatch: %#v", seenOptions)
	}
}

func TestAgentRunConfiguredStreamSessionIDOverridesOptionSessionID(t *testing.T) {
	var seenOptions ai.SimpleStreamOptions
	agent := New(Options{
		Model:         ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SessionID:     "agent-session",
		StreamOptions: ai.SimpleStreamOptions{Base: ai.StreamOptions{SessionID: "stream-session"}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			seenOptions = options
			stream := ai.NewAssistantMessageEventStream()
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if seenOptions.Base.SessionID != "stream-session" {
		t.Fatalf("session id override mismatch: %#v", seenOptions)
	}
}

func TestAgentRunUsesConfigSimpleOptionsLikeUpstreamAgentLoopConfig(t *testing.T) {
	var seenOptions ai.SimpleStreamOptions
	agent := New(Options{
		Model:  ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Config: Config{SimpleOptions: ai.SimpleStreamOptions{Base: ai.StreamOptions{SessionID: "config-session"}, Reasoning: ai.ThinkingHigh}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			seenOptions = options
			stream := ai.NewAssistantMessageEventStream()
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if seenOptions.Base.SessionID != "config-session" || seenOptions.Reasoning != ai.ThinkingHigh {
		t.Fatalf("config simple options mismatch: %#v", seenOptions)
	}
}

func TestAgentRunGetAPIKeyOverridesConfiguredStreamOptionPerCall(t *testing.T) {
	streamCalls := 0
	seenKeys := make([]string, 0, 2)
	seenProviders := make([]ai.Provider, 0, 2)
	nextModel := ai.Model{ID: "next", Provider: ai.Provider("next-provider"), API: ai.Api("fake")}
	agent := New(Options{
		Model:         ai.Model{ID: "fake", Provider: ai.Provider("initial-provider"), API: ai.Api("fake")},
		StreamOptions: ai.SimpleStreamOptions{Base: ai.StreamOptions{APIKey: "fallback-key"}},
		Config: Config{
			GetAPIKey: func(ctx context.Context, provider ai.Provider) (string, bool) {
				seenProviders = append(seenProviders, provider)
				return "dynamic-" + string(provider), true
			},
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				if streamCalls == 1 {
					return &AgentLoopTurnUpdate{Messages: []Message{NewUserMessage("continue")}, Model: &nextModel}, nil
				}
				return nil, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			seenKeys = append(seenKeys, options.Base.APIKey)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seenProviders, []ai.Provider{"initial-provider", "next-provider"}) || !reflect.DeepEqual(seenKeys, []string{"dynamic-initial-provider", "dynamic-next-provider"}) {
		t.Fatalf("dynamic api key mismatch: providers=%#v keys=%#v", seenProviders, seenKeys)
	}
}

func TestAgentRunGetAPIKeyCanKeepConfiguredFallback(t *testing.T) {
	var seenKey string
	agent := New(Options{
		Model:         ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		StreamOptions: ai.SimpleStreamOptions{Base: ai.StreamOptions{APIKey: "fallback-key"}},
		Config: Config{GetAPIKey: func(ctx context.Context, provider ai.Provider) (string, bool) {
			return "", false
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			seenKey = options.Base.APIKey
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if seenKey != "fallback-key" {
		t.Fatalf("expected fallback api key, got %q", seenKey)
	}
}

func TestAgentRunTurnUpdateThinkingLevelOverridesConfiguredStreamOptions(t *testing.T) {
	streamCalls := 0
	seenThinking := make([]ai.ThinkingLevel, 0, 2)
	nextThinking := ai.ThinkingHigh
	agent := New(Options{
		Model:         ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		StreamOptions: ai.SimpleStreamOptions{ThinkingLevel: ai.ThinkingLow},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			seenThinking = append(seenThinking, options.ThinkingLevel)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				if streamCalls == 1 {
					return &AgentLoopTurnUpdate{Messages: []Message{NewUserMessage("continue")}, ThinkingLevel: &nextThinking}, nil
				}
				return nil, nil
			},
		},
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seenThinking, []ai.ThinkingLevel{ai.ThinkingLow, ai.ThinkingHigh}) {
		t.Fatalf("thinking options mismatch: %#v", seenThinking)
	}
}

func TestAgentRunSystemPromptCanBeSetAndUpdated(t *testing.T) {
	streamCalls := 0
	seenSystemPrompts := make([]string, 0, 2)
	nextSystemPrompt := "next system"
	agent := New(Options{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SystemPrompt: "initial system",
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			if len(llm) == 0 || llm[0].Role != ai.RoleSystem {
				t.Fatalf("system prompt missing from LLM messages: %#v", llm)
			}
			seenSystemPrompts = append(seenSystemPrompts, llm[0].Content[0].Text)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "turn"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				if streamCalls == 1 {
					if turn.AgentContext.SystemPrompt != "initial system" || turn.State.SystemPrompt != "initial system" {
						t.Fatalf("system prompt missing from turn context: %#v", turn)
					}
					return &AgentLoopTurnUpdate{Messages: []Message{NewUserMessage("continue")}, SystemPrompt: &nextSystemPrompt}, nil
				}
				return nil, nil
			},
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if state.SystemPrompt != "next system" || !reflect.DeepEqual(seenSystemPrompts, []string{"initial system", "next system"}) {
		t.Fatalf("system prompt update mismatch: state=%#v seen=%#v", state, seenSystemPrompts)
	}
}

func TestAgentRunTransformContextDoesNotSeeRuntimeSystemPrompt(t *testing.T) {
	var transformRoles []ai.Role
	var streamRoles []ai.Role
	agent := New(Options{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SystemPrompt: "runtime system",
		Config: Config{
			TransformContext: func(ctx context.Context, messages []ai.Message) ([]ai.Message, error) {
				for _, message := range messages {
					transformRoles = append(transformRoles, message.Role)
				}
				return messages, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			for _, message := range llm {
				streamRoles = append(streamRoles, message.Role)
			}
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(transformRoles, []ai.Role{ai.RoleUser}) || !reflect.DeepEqual(streamRoles, []ai.Role{ai.RoleSystem, ai.RoleUser}) {
		t.Fatalf("transform/system order mismatch: transform=%#v stream=%#v", transformRoles, streamRoles)
	}
}

func TestAgentRunTransformAgentContextRunsBeforeConvertToLLM(t *testing.T) {
	var converted []Message
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Config: Config{
			TransformAgentContext: func(ctx context.Context, messages []Message) ([]Message, error) {
				out := append([]Message(nil), messages...)
				out = append(out, NewUserMessage("injected"))
				return out, nil
			},
			ConvertToLLM: func(messages []Message) []ai.Message {
				converted = append([]Message(nil), messages...)
				return DefaultConvertToLLM(messages)
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if len(converted) != 2 || converted[0].LLM.Content[0].Text != "start" || converted[1].LLM.Content[0].Text != "injected" {
		t.Fatalf("converted messages mismatch: %#v", converted)
	}
}

func TestAgentRunContextUpdateCanClearSystemPrompt(t *testing.T) {
	streamCalls := 0
	sawSystem := make([]bool, 0, 2)
	agent := New(Options{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SystemPrompt: "initial system",
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			sawSystem = append(sawSystem, len(llm) > 0 && llm[0].Role == ai.RoleSystem)
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "turn"})
			if streamCalls == 1 {
				stream.Close(ai.DoneReasonToolCalls)
				return stream, nil
			}
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				if streamCalls == 1 {
					return &AgentLoopTurnUpdate{Context: &AgentContext{SystemPrompt: "", HasSystemPrompt: true, Messages: []Message{NewUserMessage("continue")}}}, nil
				}
				return nil, nil
			},
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if state.SystemPrompt != "" || !reflect.DeepEqual(sawSystem, []bool{true, false}) {
		t.Fatalf("system prompt clear mismatch: state=%#v saw=%#v", state, sawSystem)
	}
}

func TestAgentRunPrepareNextTurnCanReplaceContextMessagesAndTools(t *testing.T) {
	streamCalls := 0
	seenPromptCounts := make([]int, 0, 2)
	seenToolNames := make([][]string, 0, 2)
	replacementTool := &recordingTool{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{fakeTool{name: "old"}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			seenPromptCounts = append(seenPromptCounts, len(llm))
			names := make([]string, 0, len(tools))
			for _, tool := range tools {
				names = append(names, tool.Name)
			}
			seenToolNames = append(seenToolNames, names)
			stream := ai.NewAssistantMessageEventStream()
			if streamCalls == 2 {
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
				stream.Close(ai.DoneReasonToolCalls)
				return stream, nil
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "turn"})
			if streamCalls == 1 {
				stream.Close(ai.DoneReasonToolCalls)
				return stream, nil
			}
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				if streamCalls == 1 {
					return &AgentLoopTurnUpdate{Context: &AgentContext{Messages: []Message{NewUserMessage("replacement")}, Tools: []Tool{replacementTool}}}, nil
				}
				return nil, nil
			},
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seenPromptCounts, []int{1, 1}) || !reflect.DeepEqual(seenToolNames, [][]string{{"old"}, {"read"}}) {
		t.Fatalf("context update not applied: prompts=%#v tools=%#v", seenPromptCounts, seenToolNames)
	}
	if len(replacementTool.calls) != 1 {
		t.Fatalf("replacement tool was not used: %#v", replacementTool.calls)
	}
	if len(state.Messages) != 3 || state.Messages[0].LLM.Content[0].Text != "replacement" {
		t.Fatalf("context messages were not replaced: %#v", state.Messages)
	}
}

func TestAgentRunPrepareNextTurnContextReplacesNilMessagesAndToolsWithEmpty(t *testing.T) {
	streamCalls := 0
	seenPromptCounts := make([]int, 0, 2)
	seenToolCounts := make([]int, 0, 2)
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{fakeTool{name: "old"}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			seenPromptCounts = append(seenPromptCounts, len(llm))
			seenToolCounts = append(seenToolCounts, len(tools))
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "turn"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				if streamCalls == 1 {
					return &AgentLoopTurnUpdate{Context: &AgentContext{}}, nil
				}
				return nil, nil
			},
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seenPromptCounts, []int{1}) || !reflect.DeepEqual(seenToolCounts, []int{1}) {
		t.Fatalf("context replacement should clear messages/tools: prompts=%#v tools=%#v", seenPromptCounts, seenToolCounts)
	}
	if len(state.Messages) != 0 || len(state.Tools) != 0 {
		t.Fatalf("final replaced context mismatch: %#v", state)
	}
}

func TestAgentRunPrepareNextTurnContextUpdateDoesNotForceNextTurn(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "turn"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
				return &AgentLoopTurnUpdate{Context: &AgentContext{Messages: []Message{NewUserMessage("replacement")}}}, nil
			},
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 1 || len(state.Messages) != 1 || state.Messages[0].LLM.Content[0].Text != "replacement" {
		t.Fatalf("context update should not force next turn: streams=%d state=%#v", streamCalls, state)
	}
}

func TestAgentRunSteeringMessagesTriggerNextTurn(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("turn-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			GetSteeringMessages: func(ctx context.Context) ([]Message, error) {
				if streamCalls == 1 {
					return []Message{NewUserMessage("steer")}, nil
				}
				return nil, nil
			},
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 2, nil
			},
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 2 {
		t.Fatalf("expected steering message to trigger second turn, got %d", streamCalls)
	}
	if len(state.Messages) != 4 || state.Messages[2].LLM.Content[0].Text != "steer" {
		t.Fatalf("steering message mismatch: %#v", state.Messages)
	}
}

func TestAgentRunExplicitSteeringQueueTriggersNextTurn(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("turn-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return streamCalls >= 2, nil
		}},
	})
	agent.EnqueueSteering(NewUserMessage("steer"))

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 2 || len(state.Messages) != 4 || state.Messages[2].LLM.Content[0].Text != "steer" {
		t.Fatalf("explicit steering queue mismatch: streams=%d state=%#v", streamCalls, state.Messages)
	}
}

func TestAgentRunSteeringQueueOneAtATime(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SteeringMode: QueueOneAtATime,
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("turn-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return streamCalls >= 3, nil
		}},
	})
	agent.EnqueueSteering(NewUserMessage("steer-1"))
	agent.EnqueueSteering(NewUserMessage("steer-2"))

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 3 || len(state.Messages) != 6 || state.Messages[2].LLM.Content[0].Text != "steer-1" || state.Messages[4].LLM.Content[0].Text != "steer-2" {
		t.Fatalf("one-at-a-time steering mismatch: streams=%d state=%#v", streamCalls, state.Messages)
	}
}

func TestAgentRunUnknownSteeringQueueModeFallsBackToOneAtATime(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		SteeringMode: QueueMode("future"),
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("turn-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return streamCalls >= 3, nil
		}},
	})
	agent.EnqueueSteering(NewUserMessage("steer-1"))
	agent.EnqueueSteering(NewUserMessage("steer-2"))

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 3 || len(state.Messages) != 6 || state.Messages[2].LLM.Content[0].Text != "steer-1" || state.Messages[4].LLM.Content[0].Text != "steer-2" {
		t.Fatalf("unknown steering mode should drain one at a time: streams=%d state=%#v", streamCalls, state.Messages)
	}
}

func TestAgentRunExplicitSteeringQueuePreemptsFollowUpQueue(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("turn-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return streamCalls >= 3, nil
		}},
	})
	agent.EnqueueSteering(NewUserMessage("steer"))
	agent.EnqueueFollowUp(NewUserMessage("follow"))

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 3 || len(state.Messages) != 6 || state.Messages[2].LLM.Content[0].Text != "steer" || state.Messages[4].LLM.Content[0].Text != "follow" {
		t.Fatalf("queue priority mismatch: streams=%d state=%#v", streamCalls, state.Messages)
	}
}

func TestAgentRunFollowUpQueueOneAtATime(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Model:        ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		FollowUpMode: QueueOneAtATime,
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("turn-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
			return streamCalls >= 3, nil
		}},
	})
	agent.EnqueueFollowUp(NewUserMessage("follow-1"))
	agent.EnqueueFollowUp(NewUserMessage("follow-2"))

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 3 || len(state.Messages) != 6 || state.Messages[2].LLM.Content[0].Text != "follow-1" || state.Messages[4].LLM.Content[0].Text != "follow-2" {
		t.Fatalf("one-at-a-time follow-up mismatch: streams=%d state=%#v", streamCalls, state.Messages)
	}
}

func TestPendingMessageQueueDrainsLikeUpstream(t *testing.T) {
	queue := NewPendingMessageQueue(QueueOneAtATime)
	if queue.HasItems() {
		t.Fatal("new queue should be empty")
	}
	queue.Enqueue(NewUserMessage("first"))
	queue.Enqueue(NewUserMessage("second"))
	if !queue.HasItems() {
		t.Fatal("queue should report pending items")
	}
	first := queue.Drain()
	if len(first) != 1 || first[0].LLM.Content[0].Text != "first" {
		t.Fatalf("first drain mismatch: %#v", first)
	}
	second := queue.Drain()
	if len(second) != 1 || second[0].LLM.Content[0].Text != "second" {
		t.Fatalf("second drain mismatch: %#v", second)
	}
	if queue.HasItems() || len(queue.Drain()) != 0 {
		t.Fatalf("queue should be empty after draining")
	}

	all := NewPendingMessageQueue(QueueAll)
	all.Enqueue(NewUserMessage("a"))
	all.Enqueue(NewUserMessage("b"))
	if drained := all.Drain(); len(drained) != 2 || all.HasItems() {
		t.Fatalf("all queue drain mismatch drained=%#v has=%v", drained, all.HasItems())
	}
}

func TestAgentRunFollowUpMessagesOnlyWhenModelDoesNotContinue(t *testing.T) {
	streamCalls := 0
	followCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			if streamCalls == 1 {
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
				stream.Close(ai.DoneReasonToolCalls)
				return stream, nil
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: fmt.Sprintf("done-%d", streamCalls)})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
		Config: Config{
			GetFollowUpMessages: func(ctx context.Context) ([]Message, error) {
				followCalls++
				if streamCalls == 2 {
					return []Message{NewUserMessage("follow")}, nil
				}
				return nil, nil
			},
			ShouldStopAfterTurn: func(ctx context.Context, turn ShouldStopAfterTurnContext) (bool, error) {
				return streamCalls >= 3, nil
			},
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 3 || followCalls != 1 {
		t.Fatalf("follow-up queue mismatch: streams=%d follows=%d", streamCalls, followCalls)
	}
	if len(state.Messages) != 6 || state.Messages[4].LLM.Content[0].Text != "follow" {
		t.Fatalf("follow-up message mismatch: %#v", state.Messages)
	}
}

func TestAgentRunStopsWhenContextCancelledBetweenTurns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "missing", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			cancel()
			return stream, nil
		},
	})
	state, err := agent.Run(ctx, []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 1 || state.Running {
		t.Fatalf("expected cancelled loop to stop after one turn, streams=%d state=%#v", streamCalls, state)
	}
}

func TestAgentRunTerminatesWhenAllToolResultsTerminate(t *testing.T) {
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{terminatingTool{}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "finish", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if streamCalls != 1 {
		t.Fatalf("terminate should stop before next LLM call, got %d streams", streamCalls)
	}
	if state.Messages[2].ToolResult.Terminate == nil || !*state.Messages[2].ToolResult.Terminate {
		t.Fatalf("terminate flag not preserved: %#v", state.Messages[2].ToolResult)
	}
}

func TestAgentRunTerminateSkipsPrepareNextTurn(t *testing.T) {
	prepareCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{terminatingTool{}},
		Config: Config{PrepareNextTurn: func(ctx context.Context, turn PrepareNextTurnContext) (*AgentLoopTurnUpdate, error) {
			prepareCalls++
			return nil, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "finish", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	if prepareCalls != 0 {
		t.Fatalf("prepare_next_turn should be skipped after terminating tool batch, got %d calls", prepareCalls)
	}
}

func TestAfterToolCallCanPatchResult(t *testing.T) {
	tool := &recordingTool{}
	streamCalls := 0
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
			return AfterToolCallResult{Content: testStringPtr("patched"), Details: map[string]any{"patched": true}, IsError: boolPtr(true), Terminate: boolPtr(true)}, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			streamCalls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if streamCalls != 1 || result.Content != "patched" || result.Details["patched"] != true || !result.IsError || result.Error != "" || result.Terminate == nil || !*result.Terminate {
		t.Fatalf("patched result mismatch streams=%d result=%#v", streamCalls, result)
	}
}

func TestAfterToolCallMarkErrorDoesNotInventErrorText(t *testing.T) {
	tool := &recordingTool{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				return AfterToolCallResult{IsError: boolPtr(true)}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "ok:README.md" || result.Error != "" || !result.IsError {
		t.Fatalf("is_error patch should not invent error text: %#v", result)
	}
	llm := DefaultConvertToLLM([]Message{state.Messages[2]})
	if len(llm) != 1 || !llm[0].IsError || llm[0].Content[0].Text != "ok:README.md" {
		t.Fatalf("tool result LLM conversion mismatch: %#v", llm)
	}
}

func TestAfterToolCallCanPatchBlockedResult(t *testing.T) {
	tool := &recordingTool{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			BeforeToolCall: func(ctx context.Context, before BeforeToolCallContext) (BeforeToolCallResult, error) {
				return BeforeToolCallResult{Block: true, Reason: "blocked"}, nil
			},
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				if turn.Result.Error != "blocked" || turn.Call.ID != "call-1" || turn.Call.Name != "read" {
					t.Fatalf("after hook saw wrong blocked context: %#v", turn)
				}
				return AfterToolCallResult{Content: testStringPtr("patched blocked"), IsError: boolPtr(false)}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 0 {
		t.Fatalf("blocked tool executed: %#v", tool.calls)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "patched blocked" || result.Error != "" {
		t.Fatalf("patched blocked result mismatch: %#v", result)
	}
}

func TestAfterToolCallCanPatchPromptDeniedResult(t *testing.T) {
	tool := &askingTool{}
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{tool},
		Config: Config{
			OnControlPlanePrompt: func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
				return ControlPlaneDeny, nil
			},
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				if turn.Result.Error != "tool call denied by user via control-plane prompt" || turn.Call.ID != "call-1" || turn.Call.Name != "ask" {
					t.Fatalf("after hook saw wrong denied context: %#v", turn)
				}
				return AfterToolCallResult{Details: map[string]any{"patched": true}}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "ask", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("ask it")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 0 {
		t.Fatalf("denied tool executed: %#v", tool.calls)
	}
	result := state.Messages[2].ToolResult
	if result.Details["patched"] != true || result.Error == "" {
		t.Fatalf("patched denied result mismatch: %#v", result)
	}
}

func TestAfterToolCallCanPatchErroredToolResult(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{failingToolWithDetails{}},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				if !turn.IsError || turn.Result.Error != context.Canceled.Error() || turn.Call.ID != "call-1" || turn.Call.Name != "read" {
					t.Fatalf("after hook saw wrong errored context: %#v", turn)
				}
				return AfterToolCallResult{Content: testStringPtr("patched error"), Details: map[string]any{"patched": true}, IsError: boolPtr(false)}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "patched error" || result.Details["patched"] != true || result.Error != "" {
		t.Fatalf("patched errored result mismatch: %#v", result)
	}
}

func TestAfterToolCallCanPatchArbitraryDetailsValueLikeUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				return AfterToolCallResult{DetailsValue: []any{"trace"}}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if len(result.DetailsValue.([]any)) != 1 || result.Details != nil {
		t.Fatalf("patched details value mismatch: %#v", result)
	}
}

func TestAfterToolCallCanPatchNullDetailsValueLikeUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{failingToolWithDetails{}},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				return AfterToolCallResult{DetailsValueSet: true, DetailsValue: nil}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.DetailsValue != nil || result.Details != nil {
		t.Fatalf("patched null details value mismatch: %#v", result)
	}
}

func TestAfterToolCallCanPatchContentBlocksLikeUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				return AfterToolCallResult{ContentBlocks: []ai.ContentBlock{{Type: ai.ContentText, Text: "patched"}, {Type: ai.ContentImage, Data: "aW1n", MimeType: "image/png"}}}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "patched" || len(result.ContentBlocks) != 2 || result.ContentBlocks[1].Type != ai.ContentImage {
		t.Fatalf("patched content blocks mismatch: %#v", result)
	}
}

func TestAfterToolCallCanPatchEmptyContentBlocksLikeUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{&recordingTool{}},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				return AfterToolCallResult{ContentBlocksSet: true, ContentBlocks: []ai.ContentBlock{}}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "" || len(result.ContentBlocks) != 0 {
		t.Fatalf("empty content blocks patch mismatch: %#v", result)
	}
}

func TestAfterToolCallReceivesNormalizedToolResultLikeUpstream(t *testing.T) {
	var after AfterToolCallContext
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{blockResultTool{}},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				after = turn
				return AfterToolCallResult{}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "block_result", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("run it")})
	if err != nil {
		t.Fatal(err)
	}
	if after.Result.Content != "done" || len(after.Result.ContentBlocks) != 2 || after.Result.ContentBlocks[0].Type != ai.ContentText || len(after.Result.DetailsValue.([]any)) != 1 || after.Result.Details != nil {
		t.Fatalf("after hook should receive normalized result: %#v", after.Result)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "done" || len(result.ContentBlocks) != 2 || result.Details != nil {
		t.Fatalf("stored tool result should stay normalized: %#v", result)
	}
}

func TestErroredToolResultUsesErrorContentLikeUpstream(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{failingToolWithContent{}},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				if turn.Result.Content != context.Canceled.Error() || turn.Result.Error != context.Canceled.Error() {
					t.Fatalf("after hook saw wrong upstream error result: %#v", turn.Result)
				}
				return AfterToolCallResult{}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("read it")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != context.Canceled.Error() || result.Error != context.Canceled.Error() {
		t.Fatalf("errored tool result should use error text like upstream: %#v", result)
	}
}

func TestAfterToolCallCanPatchUnknownToolResult(t *testing.T) {
	var after AfterToolCallContext
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Config: Config{
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				after = turn
				return AfterToolCallResult{Content: testStringPtr("patched unknown"), IsError: boolPtr(false)}, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "missing", Arguments: map[string]any{"x": "y"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatal(err)
	}
	result := state.Messages[2].ToolResult
	if result.Content != "patched unknown" || result.Error != "" {
		t.Fatalf("unknown tool result not patched: %#v", result)
	}
	afterArgs, _ := after.Args.(map[string]any)
	if after.Call.Name != "missing" || afterArgs["x"] != "y" || !after.IsError || after.Result.Error != "No tool registered named 'missing'" || after.Result.Content != "No tool registered named 'missing'" {
		t.Fatalf("unknown tool after context mismatch: %#v", after)
	}
}

func TestToolUpdateCallbackPublishesEvent(t *testing.T) {
	var events []Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{updatingTool{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "update", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) { events = append(events, event) })
	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == EventTypeToolUpdate && event.ToolResult != nil && event.ToolResult.Content == "partial" && event.ToolResult.Details["pct"] == float64(50) {
			return
		}
	}
	t.Fatalf("missing tool update event: %#v", events)
}

func TestToolUpdateCallbackNormalizesContentBlocksAndDetailsValue(t *testing.T) {
	var updateEvent *Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{blockUpdatingTool{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "block_update", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) {
		if event.Type == EventTypeToolUpdate {
			copyEvent := event
			updateEvent = &copyEvent
		}
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	if updateEvent == nil || updateEvent.ToolResult == nil {
		t.Fatalf("missing tool update event: %#v", updateEvent)
	}
	result := updateEvent.ToolResult
	if result.Content != "partial" || len(result.ContentBlocks) != 2 || result.ContentBlocks[0].Type != ai.ContentText || result.ContentBlocks[1].Type != ai.ContentImage || len(result.DetailsValue.([]any)) != 1 || result.Details != nil {
		t.Fatalf("tool update result should be normalized like final results: %#v", result)
	}
}

func TestToolUpdateEventIncludesPreparedArguments(t *testing.T) {
	var updateEvent *Event
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{preparingUpdatingTool{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "prepare_update", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) {
		if event.Type == EventTypeToolUpdate {
			copyEvent := event
			updateEvent = &copyEvent
		}
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("start")}); err != nil {
		t.Fatal(err)
	}
	updateArgs, _ := updateEvent.ToolArgs.(map[string]any)
	if updateEvent == nil || updateArgs["path"] != "prepared.md" || updateEvent.ToolCall.Arguments["path"] != "prepared.md" {
		t.Fatalf("tool update args mismatch: %#v", updateEvent)
	}
}

func TestAgentRunExecutesToolsInParallelByDefault(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	var once sync.Once
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{sleepingTool{name: "a", started: started, release: release}, sleepingTool{name: "b", started: started, release: release}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-a", Name: "a", Arguments: map[string]any{}}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-b", Name: "b", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- err
	}()
	<-started
	select {
	case <-started:
		once.Do(func() { close(release) })
	case <-time.After(200 * time.Millisecond):
		once.Do(func() { close(release) })
		t.Fatal("second tool did not start before first was released")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAgentRunParallelAfterToolCallRunsAfterAllToolsComplete(t *testing.T) {
	started := make(chan string, 2)
	releaseSlow := make(chan struct{})
	releaseFast := make(chan struct{})
	close(releaseFast)
	after := make(chan string, 2)
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{
			sleepingTool{name: "slow", started: started, release: releaseSlow},
			sleepingTool{name: "fast", started: started, release: releaseFast},
		},
		Config: Config{
			AfterToolCall: func(ctx context.Context, turn AfterToolCallContext) (AfterToolCallResult, error) {
				after <- turn.Call.Name
				return AfterToolCallResult{}, nil
			},
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "slow", Name: "slow", Arguments: map[string]any{}}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "fast", Name: "fast", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})

	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("run tools")})
		done <- err
	}()

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case name := <-started:
			seen[name] = true
		case err := <-done:
			t.Fatalf("agent finished before both tools started: %v", err)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for tools to start: %#v", seen)
		}
	}

	select {
	case name := <-after:
		t.Fatalf("after_tool_call ran before all tools completed for %q", name)
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseSlow)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := []string{<-after, <-after}
	if !reflect.DeepEqual(got, []string{"slow", "fast"}) {
		t.Fatalf("after_tool_call order mismatch: %#v", got)
	}
}

func TestAgentRunCanExecuteToolsSequentially(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	agent := New(Options{
		Model:         ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools:         []Tool{sleepingTool{name: "a", started: started, release: release}, sleepingTool{name: "b", started: started, release: release}},
		ToolExecution: ToolExecutionSequential,
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-a", Name: "a", Arguments: map[string]any{}}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-b", Name: "b", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- err
	}()
	<-started
	select {
	case second := <-started:
		close(release)
		t.Fatalf("second tool started before first was released: %s", second)
	case <-time.After(50 * time.Millisecond):
		close(release)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAgentRunParallelToolPanicBecomesErrorResult(t *testing.T) {
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{panickingTool{}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-panic", Name: "panic", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	var results []ToolResult
	agent.Subscribe(func(event Event) {
		if event.Type == EventTypeToolExecutionEnd && event.ToolResult != nil {
			results = append(results, *event.ToolResult)
		}
	})

	state, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
	if err != nil {
		t.Fatalf("tool panic should become error result, got run error: %v", err)
	}
	if len(results) != 1 || !results[0].IsError || !strings.Contains(results[0].Error, "tool task join") || !strings.Contains(results[0].Error, "boom") {
		t.Fatalf("panic tool result mismatch: %#v", results)
	}
	if len(state.Messages) < 2 || state.Messages[len(state.Messages)-1].ToolResult == nil || !state.Messages[len(state.Messages)-1].ToolResult.IsError {
		t.Fatalf("panic tool result message missing: %#v", state.Messages)
	}
}

func TestAgentRunSequentialToolOverrideMakesBatchSequential(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{sleepingTool{name: "a", started: started, release: release, sequential: true}, sleepingTool{name: "b", started: started, release: release}},
		Config: Config{ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
			return true, nil
		}},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-a", Name: "a", Arguments: map[string]any{}}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-b", Name: "b", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- err
	}()
	<-started
	select {
	case second := <-started:
		close(release)
		t.Fatalf("sequential override ignored; second tool started early: %s", second)
	case <-time.After(50 * time.Millisecond):
		close(release)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAgentRunUnknownToolExecutionModeFailsClosedSequentialLikeUpstreamEnum(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	agent := New(Options{
		Model: ai.Model{ID: "fake", Provider: ai.Provider("test"), API: ai.Api("fake")},
		Tools: []Tool{sleepingTool{name: "a", started: started, release: release}, sleepingTool{name: "b", started: started, release: release}},
		Config: Config{
			ToolExecution: ToolExecutionMode("future"),
			ShouldStopAfterTurn: func(context.Context, ShouldStopAfterTurnContext) (bool, error) {
				return true, nil
			},
		},
		Stream: func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-a", Name: "a", Arguments: map[string]any{}}})
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-b", Name: "b", Arguments: map[string]any{}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		},
	})
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), []Message{NewUserMessage("start")})
		done <- err
	}()
	<-started
	select {
	case second := <-started:
		close(release)
		t.Fatalf("unknown tool execution mode should fail closed to sequential; second tool started early: %s", second)
	case <-time.After(50 * time.Millisecond):
		close(release)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func hasEvent(events []Event, eventType EventType) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func findEvent(events []Event, eventType EventType) *Event {
	for index := range events {
		if events[index].Type == eventType {
			return &events[index]
		}
	}
	return nil
}

func filterEvents(events []Event, eventType EventType) []Event {
	filtered := make([]Event, 0)
	for _, event := range events {
		if event.Type == eventType {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func eventTypes(events []Event) []EventType {
	types := make([]EventType, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func eventIndex(events []Event, eventType EventType) int {
	for index, event := range events {
		if event.Type == eventType {
			return index
		}
	}
	return -1
}

func hasInitialUserMessageEvent(events []Event) bool {
	for _, event := range events {
		if event.Type == EventTypeMessageStart && event.Message != nil && event.Message.LLM != nil && event.Message.LLM.Role == ai.RoleUser {
			return true
		}
	}
	return false
}

func boolPtr(value bool) *bool { return &value }

func testStringPtr(value string) *string { return &value }

func TestAgentPublishesSystemPromptMessageBeforeStream(t *testing.T) {
	var systemEvent *Event
	agent := New(Options{
		Model:        ai.Model{ID: "fake"},
		SystemPrompt: "system prompt",
		Stream: func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
			if len(messages) == 0 || messages[0].Role != ai.RoleSystem || messages[0].Content[0].Text != "system prompt" {
				t.Fatalf("first LLM message = %#v", messages)
			}
			stream := ai.NewAssistantMessageEventStream()
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		},
	})
	agent.Subscribe(func(event Event) {
		if event.Type == EventTypeSystemPrompt {
			copyEvent := event
			systemEvent = &copyEvent
		}
	})

	if _, err := agent.Run(context.Background(), []Message{NewUserMessage("hello")}); err != nil {
		t.Fatal(err)
	}
	if systemEvent == nil || systemEvent.LLMMessage == nil {
		t.Fatalf("missing system prompt event: %#v", systemEvent)
	}
	if systemEvent.LLMMessage.Role != ai.RoleSystem || systemEvent.LLMMessage.Content[0].Text != "system prompt" {
		t.Fatalf("system prompt message = %#v", systemEvent.LLMMessage)
	}
}
