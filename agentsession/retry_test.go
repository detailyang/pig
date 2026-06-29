package agentsession

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/harness"
	"github.com/detailyang/pig/session"
)

func TestRetryablePatternsMatchUpstreamRegex(t *testing.T) {
	for _, message := range []string{
		"overloaded_error",
		"Provider returned error: 429 Too Many Requests",
		"rate limit exceeded",
		"HTTP 503 Service Unavailable",
		"websocket closed",
		"stream ended before message_stop",
		"socket hang up",
		"reset before headers",
		"request timed out",
		"retry delay from provider",
	} {
		if !IsRetryableError(message) {
			t.Fatalf("expected retryable: %q", message)
		}
	}
	for _, message := range []string{"bad request: missing field", "Unauthorized", "model not found"} {
		if IsRetryableError(message) {
			t.Fatalf("expected non-retryable: %q", message)
		}
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	if got := BackoffMS(1, 1000, 60000); got != 1000 {
		t.Fatalf("attempt 1=%d", got)
	}
	if got := BackoffMS(2, 1000, 60000); got != 2000 {
		t.Fatalf("attempt 2=%d", got)
	}
	if got := BackoffMS(9, 1000, 60000); got != 60000 {
		t.Fatalf("attempt 9=%d", got)
	}
	if got := BackoffMS(100, 2, 1<<62); got != 2048 {
		t.Fatalf("exponent should cap at 10, got %d", got)
	}
}

func TestRetryPolicyPlansRetriesAndFallback(t *testing.T) {
	settings := RetrySettings{Enabled: true, MaxRetries: 2, BaseDelayMS: 100, MaxDelayMS: 1000, FallbackModel: &ModelRef{Provider: "openai", ModelID: "gpt-fallback"}}
	policy := NewRetryPolicy(settings)
	first := policy.Next("HTTP 503")
	if first.Action != RetryActionRetry || first.Attempt != 1 || first.DelayMS != 100 {
		t.Fatalf("first=%#v", first)
	}
	second := policy.Next("rate limit")
	if second.Action != RetryActionRetry || second.Attempt != 2 || second.DelayMS != 200 {
		t.Fatalf("second=%#v", second)
	}
	fallback := policy.Next("overloaded")
	if fallback.Action != RetryActionFallback || fallback.Fallback == nil || fallback.Fallback.ModelID != "gpt-fallback" || fallback.Attempt != 0 {
		t.Fatalf("fallback=%#v", fallback)
	}
	afterFallback := policy.Next("500 server error")
	if afterFallback.Action != RetryActionRetry || afterFallback.Attempt != 1 {
		t.Fatalf("after fallback=%#v", afterFallback)
	}
	policy.Next("503")
	exhausted := policy.Next("503")
	if exhausted.Action != RetryActionGiveUp || exhausted.ErrorMessage != "503" {
		t.Fatalf("exhausted=%#v", exhausted)
	}
}

func TestRetryPolicyHonorsDisabledAndNonRetryable(t *testing.T) {
	disabled := NewRetryPolicy(RetrySettings{Enabled: false, MaxRetries: 5})
	if got := disabled.Next("HTTP 503"); got.Action != RetryActionGiveUp {
		t.Fatalf("disabled=%#v", got)
	}
	enabled := NewRetryPolicy(RetrySettings{Enabled: true, MaxRetries: 5})
	if got := enabled.Next("bad request"); got.Action != RetryActionGiveUp {
		t.Fatalf("non retryable=%#v", got)
	}
}

func TestAgentSessionZeroRetrySettingsUseUpstreamDefaults(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	calls := 0
	stream := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		calls++
		out := ai.NewAssistantMessageEventStream()
		if calls == 1 {
			out.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, Message: &ai.AssistantMessage{Role: ai.AssistantRoleAssistant, StopReason: ai.StopReasonError, ErrorMessage: "HTTP 503 overloaded", Usage: &ai.Usage{}}})
			return out, nil
		}
		out.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok-after-default-retry"})
		out.Close(ai.DoneReasonStop)
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("test")}, Session: sess, StreamFn: stream})
	runner := New(h, RetrySettings{})

	if err := runner.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected default retry to rerun once, got %d calls", calls)
	}
}

func TestAgentSessionRetriesAndRewindsFailedAssistant(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	calls := 0
	stream := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		calls++
		out := ai.NewAssistantMessageEventStream()
		if calls == 1 {
			out.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, Message: &ai.AssistantMessage{Role: ai.AssistantRoleAssistant, API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopReasonError, ErrorMessage: "HTTP 503 overloaded", Usage: &ai.Usage{}}})
			return out, nil
		}
		out.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
		out.Close(ai.DoneReasonStop)
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("test")}, Session: sess, StreamFn: stream})
	runner := New(h, RetrySettings{Enabled: true, MaxRetries: 1, BaseDelayMS: 1, MaxDelayMS: 1})

	if err := runner.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected retry, got %d stream calls", calls)
	}
	state := h.Agent().State()
	if len(state.Messages) != 2 || state.Messages[1].LLM == nil || state.Messages[1].LLM.Role != ai.RoleAssistant || state.Messages[1].LLM.ErrorMessage != "" || state.Messages[1].LLM.Content[0].Text != "ok" {
		t.Fatalf("failed assistant should be removed before retry, state=%#v", state.Messages)
	}
	branch, err := sess.Branch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 2 || branch[1].Message == nil || branch[1].Message.LLM == nil || branch[1].Message.LLM.ErrorMessage != "" || branch[1].Message.LLM.Content[0].Text != "ok" {
		t.Fatalf("active branch should contain user plus successful assistant only, branch=%#v", branch)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("append-only log should keep failed assistant entry, entries=%#v", entries)
	}
}

func TestAgentSessionDoesNotRewindSuccessfulAssistantAfterRunError(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	calls := 0
	stream := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		calls++
		if calls == 2 {
			return nil, fmt.Errorf("HTTP 503")
		}
		out := ai.NewAssistantMessageEventStream()
		if calls == 1 {
			out.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
		} else {
			out.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok-after-retry"})
		}
		out.Close(ai.DoneReasonStop)
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("test")}, Session: sess, StreamFn: stream})
	runner := New(h, RetrySettings{Enabled: true, MaxRetries: 1, BaseDelayMS: 1, MaxDelayMS: 1})

	if err := runner.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if err := runner.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := h.Agent().State()
	if len(state.Messages) != 3 || state.Messages[1].LLM == nil || state.Messages[1].LLM.Content[0].Text != "ok" || state.Messages[2].LLM == nil || state.Messages[2].LLM.Content[0].Text != "ok-after-retry" {
		t.Fatalf("successful assistant should not be rewound after run error, state=%#v", state.Messages)
	}
}

func TestAgentSessionRetryRewindsConsecutiveFailedAssistantsFromState(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	calls := 0
	stream := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		calls++
		out := ai.NewAssistantMessageEventStream()
		if calls == 1 {
			out.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, Message: &ai.AssistantMessage{Role: ai.AssistantRoleAssistant, StopReason: ai.StopReasonError, ErrorMessage: "HTTP 503 second", Usage: &ai.Usage{}}})
			return out, nil
		}
		out.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
		out.Close(ai.DoneReasonStop)
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("test")}, Session: sess, StreamFn: stream})
	runner := New(h, RetrySettings{Enabled: true, MaxRetries: 1, BaseDelayMS: 1, MaxDelayMS: 1})

	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.Message{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleAssistant, StopReason: ai.StopReasonError, ErrorMessage: "HTTP 503 first"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.RehydrateFromSession(); err != nil {
		t.Fatal(err)
	}

	if err := runner.Continue(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := h.Agent().State()
	if len(state.Messages) != 2 || state.Messages[0].LLM == nil || state.Messages[0].LLM.Role != ai.RoleUser || state.Messages[1].LLM == nil || state.Messages[1].LLM.Content[0].Text != "ok" {
		t.Fatalf("all consecutive failed assistants should be removed before retry, state=%#v", state.Messages)
	}
}

func TestAgentSessionIgnoresErrorMessageWithoutErrorStopReasonLikeUpstream(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	stream := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		out := ai.NewAssistantMessageEventStream()
		out.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, Message: &ai.AssistantMessage{Role: ai.AssistantRoleAssistant, StopReason: ai.StopReasonEndTurn, ErrorMessage: "HTTP 503 overloaded", Usage: &ai.Usage{}}})
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("test")}, Session: sess, StreamFn: stream})
	runner := New(h, RetrySettings{Enabled: true, MaxRetries: 1, BaseDelayMS: 1, MaxDelayMS: 1})

	if err := runner.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
}

func TestAgentSessionUsesUpstreamDefaultAssistantErrorMessage(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	stream := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		out := ai.NewAssistantMessageEventStream()
		out.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, Message: &ai.AssistantMessage{Role: ai.AssistantRoleAssistant, StopReason: ai.StopReasonError, Usage: &ai.Usage{}}})
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("test")}, Session: sess, StreamFn: stream})
	runner := New(h, RetrySettings{Enabled: true, MaxRetries: 1, BaseDelayMS: 1, MaxDelayMS: 1})

	err := runner.Prompt(context.Background(), "hello")
	if err == nil || err.Error() != "assistant stopped with an error" {
		t.Fatalf("expected upstream default assistant error, got %v", err)
	}
}

func TestAgentSessionMissingFallbackModelReturnsOriginalErrorLikeUpstream(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	stream := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		out := ai.NewAssistantMessageEventStream()
		out.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, Message: &ai.AssistantMessage{Role: ai.AssistantRoleAssistant, StopReason: ai.StopReasonError, ErrorMessage: "HTTP 503 overloaded", Usage: &ai.Usage{}}})
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("test")}, Session: sess, StreamFn: stream})
	runner := New(h, RetrySettings{Enabled: true, MaxRetries: 0, FallbackModel: &ModelRef{Provider: "missing-provider", ModelID: "missing-model"}})

	err := runner.Prompt(context.Background(), "hello")
	if err == nil || err.Error() != "HTTP 503 overloaded" {
		t.Fatalf("missing fallback should return original error like upstream, got %v", err)
	}
	state := h.Agent().State()
	if len(state.Messages) != 2 || state.Messages[1].LLM == nil || state.Messages[1].LLM.ErrorMessage != "HTTP 503 overloaded" {
		t.Fatalf("missing fallback should not rewind failed assistant, state=%#v", state.Messages)
	}
	branch, err := sess.Branch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 2 || branch[1].Message == nil || branch[1].Message.LLM == nil || branch[1].Message.LLM.ErrorMessage != "HTTP 503 overloaded" {
		t.Fatalf("missing fallback should keep failed assistant as active leaf, branch=%#v", branch)
	}
}

func TestAgentSessionFallbackModelRetriesOnceAfterPrimaryExhausts(t *testing.T) {
	provider := ai.Provider("fallback-success")
	modelID := "fallback-model"
	ai.RegisterCustomModel(ai.Model{ID: modelID, Provider: provider, API: ai.Api("test")})
	defer ai.UnregisterCustomModel(provider, modelID)

	sess := session.NewSession(session.NewMemorySessionStorage())
	var seenModels []string
	stream := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		seenModels = append(seenModels, string(model.Provider)+":"+model.ID)
		out := ai.NewAssistantMessageEventStream()
		if model.Provider == provider && model.ID == modelID {
			out.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok-fallback"})
			out.Close(ai.DoneReasonStop)
			return out, nil
		}
		out.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, Message: &ai.AssistantMessage{Role: ai.AssistantRoleAssistant, StopReason: ai.StopReasonError, ErrorMessage: "HTTP 503 overloaded", Usage: &ai.Usage{}}})
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Model: ai.Model{ID: "primary-model", Provider: ai.Provider("primary"), API: ai.Api("test")}, Session: sess, StreamFn: stream})
	runner := New(h, RetrySettings{Enabled: true, MaxRetries: 0, FallbackModel: &ModelRef{Provider: string(provider), ModelID: modelID}})

	if err := runner.Prompt(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seenModels, []string{"primary:primary-model", "fallback-success:fallback-model"}) {
		t.Fatalf("fallback model sequence mismatch: %#v", seenModels)
	}
	state := h.Agent().State()
	if state.Model == nil || state.Model.Provider != provider || state.Model.ID != modelID {
		t.Fatalf("agent should stay on fallback model, got %#v", state.Model)
	}
	if len(state.Messages) != 2 || state.Messages[1].LLM == nil || state.Messages[1].LLM.Content[0].Text != "ok-fallback" {
		t.Fatalf("fallback success state mismatch: %#v", state.Messages)
	}
}

func TestAgentSessionHelperSurfaceMatchesUpstream(t *testing.T) {
	if Default().MaxRetries != DefaultRetrySettings().MaxRetries || !Default().Enabled {
		t.Fatalf("default retry settings mismatch: %#v", Default())
	}

	errorMessage := ai.AssistantMessage{Role: ai.AssistantRoleAssistant, StopReason: ai.StopReasonError, ErrorMessage: "provider overloaded"}
	if got := AssistantErrorMessage(&errorMessage); got != "provider overloaded" {
		t.Fatalf("assistant error mismatch: %q", got)
	}
	errorMessage.ErrorMessage = ""
	if got := AssistantErrorMessage(&errorMessage); got != "assistant stopped with an error" {
		t.Fatalf("default assistant error mismatch: %q", got)
	}
	errorMessage.StopReason = ai.StopReasonStop
	if got := AssistantErrorMessage(&errorMessage); got != "" {
		t.Fatalf("non-error assistant should be empty, got %q", got)
	}

	sess := session.NewSession(session.NewMemorySessionStorage())
	userID, err := sess.AppendMessage(agent.NewUserMessage("hello"))
	if err != nil {
		t.Fatal(err)
	}
	assistantMessage := agent.NewAssistantMessage("failed")
	assistantMessage.LLM.StopReason = ai.StopReasonError
	if _, err := sess.AppendMessage(assistantMessage); err != nil {
		t.Fatal(err)
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Session: sess, Model: ai.Model{ID: "faux", Provider: ai.Provider("faux"), API: ai.ApiFaux}})
	h.Agent().ReplaceState(agent.State{Messages: []agent.Message{agent.NewUserMessage("hello"), assistantMessage}})
	runner := New(h, RetrySettings{})
	last := runner.LastAssistant()
	if last == nil || last.StopReason != ai.StopReasonError {
		t.Fatalf("last assistant mismatch: %#v", last)
	}
	if err := runner.RewindFailedAssistant(); err != nil {
		t.Fatal(err)
	}
	state := h.Agent().State()
	if len(state.Messages) != 1 || state.Messages[0].LLM.Role != ai.RoleUser {
		t.Fatalf("rewound state mismatch: %#v", state.Messages)
	}
	leaf, err := sess.LeafID()
	if err != nil || leaf == nil || *leaf != userID {
		t.Fatalf("rewound leaf mismatch: leaf=%v err=%v", leaf, err)
	}
}

func TestAgentSessionWrapsFallbackSetModelErrorLikeUpstream(t *testing.T) {
	provider := ai.Provider("fallback-set-model-fail")
	modelID := "fallback-model"
	ai.RegisterCustomModel(ai.Model{ID: modelID, Provider: provider, API: ai.Api("test")})
	defer ai.UnregisterCustomModel(provider, modelID)
	sess := session.NewSession(&failModelChangeAppendStorage{MemoryStorage: session.NewMemorySessionStorage()})
	stream := func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		out := ai.NewAssistantMessageEventStream()
		out.Emit(ai.AssistantMessageEvent{Type: ai.EventDone, Message: &ai.AssistantMessage{Role: ai.AssistantRoleAssistant, StopReason: ai.StopReasonError, ErrorMessage: "HTTP 503 overloaded", Usage: &ai.Usage{}}})
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Model: ai.Model{ID: "test-model", Provider: ai.Provider("test"), API: ai.Api("test")}, Session: sess, StreamFn: stream})
	runner := New(h, RetrySettings{Enabled: true, MaxRetries: 0, FallbackModel: &ModelRef{Provider: string(provider), ModelID: modelID}})

	err := runner.Prompt(context.Background(), "hello")
	if err == nil || err.Error() != "fallback set_model failed: model change append failed" {
		t.Fatalf("expected wrapped fallback set_model error, got %v", err)
	}
}

type failModelChangeAppendStorage struct {
	*session.MemoryStorage
}

func (storage *failModelChangeAppendStorage) AppendEntry(entry session.Entry) error {
	if entry.Type() == session.EntryTypeModelChange {
		return fmt.Errorf("model change append failed")
	}
	return storage.MemoryStorage.AppendEntry(entry)
}
