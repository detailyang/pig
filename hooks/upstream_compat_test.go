package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/harness"
)

func TestHookRunnerAndPayloadCompatSurface(t *testing.T) {
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Rules: []Rule{{Event: AgentStart}}})
	var upstreamRunner *HookRunner = runner
	if upstreamRunner.IsEmpty() || upstreamRunner.Len() != 1 {
		t.Fatalf("hook runner mismatch: empty=%v len=%d", upstreamRunner.IsEmpty(), upstreamRunner.Len())
	}
	var payload HookPayload = upstreamRunner.PayloadFor(Rule{Event: AgentStart}, EventData{Event: AgentStart})
	if payload.Event != "agent_start" || payload.SessionID != "s1" {
		t.Fatalf("hook payload mismatch: %#v", payload)
	}
}

func TestHookEventAsStrMatchesUpstream(t *testing.T) {
	if AgentStart.AsStr() != "agent_start" || ToolEnd.AsStr() != "tool_end" || Compaction.AsStr() != "compaction" {
		t.Fatalf("hook event AsStr mismatch: %q %q %q", AgentStart.AsStr(), ToolEnd.AsStr(), Compaction.AsStr())
	}
}

func TestBasicEventDataMatchesUpstreamBasic(t *testing.T) {
	data := BasicEventData(TurnStart)
	if data.Event != TurnStart || data.MessageKindPresent || data.ToolNamePresent || data.CompactionSummaryPresent || data.ToolIsError != nil {
		t.Fatalf("basic event data should only carry event: %#v", data)
	}
}

func TestEventDataFromAgentEventIncludesMessageKindAndSummary(t *testing.T) {
	data := EventDataFromAgentEvent(agent.Event{Type: agent.EventTypeMessageEnd, Message: ptrAgentMessage(agent.NewUserMessage("hello hook"))})
	if data.Event != MessageEnd || data.MessageKind != "user" || !data.MessageKindPresent || data.MessageSummary != "hello hook" || !data.MessageSummaryPresent {
		t.Fatalf("message event data mismatch: %#v", data)
	}
}

func TestEventDataFromAgentEventCustomSummaryDoesNotHTMLEscapeLikeSerdeJSON(t *testing.T) {
	message := agent.Message{Kind: agent.MessageKindCustom, Custom: &agent.CustomMessage{Role: "trigger", Payload: map[string]any{"text": "a < b && c > d"}}}
	data := EventDataFromAgentEvent(agent.Event{Type: agent.EventTypeMessageEnd, Message: &message})
	if strings.Contains(data.MessageSummary, `\u003c`) || strings.Contains(data.MessageSummary, `\u003e`) || strings.Contains(data.MessageSummary, `\u0026`) {
		t.Fatalf("custom message summary should not HTML-escape like serde_json, got %q", data.MessageSummary)
	}
	if data.MessageSummary != `{"text":"a < b && c > d"}` {
		t.Fatalf("custom message summary mismatch: %q", data.MessageSummary)
	}
}

func TestEventDataFromAgentEventIncludesAssistantEventName(t *testing.T) {
	data := EventDataFromAgentEvent(agent.Event{Type: agent.EventTypeMessageUpdate, Message: ptrAgentMessage(agent.NewAssistantMessage("hello")), AssistantMessageEvent: &ai.AssistantMessageEvent{Type: ai.EventTextDelta}})
	if data.Event != MessageUpdate || data.AssistantEvent != "text_delta" || !data.AssistantEventPresent {
		t.Fatalf("assistant event data mismatch: %#v", data)
	}
}

func TestEventDataFromAgentEventIncludesToolArgs(t *testing.T) {
	args := map[string]any{"path": "README.md"}
	data := EventDataFromAgentEvent(agent.Event{Type: agent.EventTypeToolExecutionStart, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read"}, ToolArgs: args})
	if data.Event != ToolStart || data.ToolCallID != "call-1" || data.ToolName != "read" || data.ToolArgs == nil {
		t.Fatalf("tool start event data mismatch: %#v", data)
	}
}

func TestEventDataFromAgentEventSummarizesToolResultBlocks(t *testing.T) {
	result := agent.ToolResult{Name: "vision", ContentBlocks: []ai.ContentBlock{{Type: ai.ContentImage, MimeType: "image/png"}}}
	data := EventDataFromAgentEvent(agent.Event{Type: agent.EventTypeToolExecutionEnd, ToolCall: &ai.ToolCall{ID: "call-1", Name: "vision"}, ToolResult: &result})
	if data.ToolResultSummary != "<image image/png>" || !data.ToolResultSummaryPresent {
		t.Fatalf("tool result summary mismatch: %#v", data)
	}
}

func TestRunnerHandleEventAliasesMatchUpstreamSurface(t *testing.T) {
	runner := NewRunner(RunnerOptions{Rules: []Rule{{Event: MessageEnd, Command: "true"}}})
	if err := runner.HandleEvent(context.Background(), EventData{Event: MessageEnd}); err != nil {
		t.Fatal(err)
	}
	if err := runner.HandleHarnessEvent(context.Background(), EventData{Event: MessageEnd}); err != nil {
		t.Fatal(err)
	}
}

func ptrAgentMessage(message agent.Message) *agent.Message { return &message }

func TestRunnerListenerMatchesUpstreamSurface(t *testing.T) {
	called := 0
	runner := NewRunner(RunnerOptions{Rules: []Rule{{Event: AgentStart}}, Executor: func(ctx context.Context, rule Rule, payload Payload) error {
		called++
		return nil
	}})
	listener := runner.Listener()
	listener(agent.Event{Type: agent.EventTypeStart})
	if called != 1 {
		t.Fatalf("listener should dispatch agent_start once, got %d", called)
	}
}

func TestRunnerHarnessListenerMatchesUpstreamSurface(t *testing.T) {
	var got Payload
	runner := NewRunner(RunnerOptions{Rules: []Rule{{Event: Compaction}}, Executor: func(ctx context.Context, rule Rule, payload Payload) error {
		got = payload
		return nil
	}})
	listener := runner.HarnessListener()
	listener(harness.CompactionEvent(true, "compacted", 42))
	if got.Event != "compaction" || got.CompactionTrigger != "manual" || got.CompactionTokensBefore == nil || *got.CompactionTokensBefore != 42 || got.CompactionSummary != "compacted" {
		t.Fatalf("harness listener payload mismatch: %#v", got)
	}
}

func TestEventDataFromHarnessEventTruncatesCompactionSummaryLikeUpstream(t *testing.T) {
	summary := strings.Repeat("好", 2001)
	data, ok := EventDataFromHarnessEvent(harness.CompactionEvent(false, summary, 42))
	if !ok {
		t.Fatal("expected compaction harness event data")
	}
	want := strings.Repeat("好", 2000) + "…"
	if data.CompactionSummary != want || !data.CompactionSummaryPresent {
		t.Fatalf("compaction summary mismatch: chars=%d present=%v", len([]rune(data.CompactionSummary)), data.CompactionSummaryPresent)
	}
}

func TestHooksUpstreamHelperAliases(t *testing.T) {
	if event, ok := Parse("tool_end"); !ok || event != ToolEnd {
		t.Fatalf("Parse mismatch: %q ok=%v", event, ok)
	}
	if CompactionTrigger(true) != "manual" || CompactionTrigger(false) != "auto" {
		t.Fatalf("compaction trigger mismatch")
	}
	if ShellProgram() == "" || ShellArg() == "" {
		t.Fatalf("shell helper mismatch: %q %q", ShellProgram(), ShellArg())
	}
	if Truncate(strings.Repeat("x", 2001)) != strings.Repeat("x", 2000)+"…" {
		t.Fatalf("truncate mismatch")
	}
	data := Basic(TurnStart)
	if data.Event != TurnStart || data.MessageKindPresent {
		t.Fatalf("basic mismatch: %#v", data)
	}
	agentData, ok := FromAgentEvent(agent.Event{Type: agent.EventTypeToolExecutionEnd, ToolResult: &agent.ToolResult{Content: "ok"}})
	if !ok || agentData.Event != ToolEnd || agentData.ToolResultSummary != "ok" {
		t.Fatalf("from agent event mismatch: ok=%v data=%#v", ok, agentData)
	}
	harnessData, ok := FromHarnessEvent(harness.CompactionEvent(true, "summary", 7))
	if !ok || harnessData.CompactionTrigger != "manual" || harnessData.CompactionTokensBefore == nil || *harnessData.CompactionTokensBefore != 7 {
		t.Fatalf("from harness event mismatch: ok=%v data=%#v", ok, harnessData)
	}
	if ResultSummary(agent.ToolResult{ContentBlocks: []ai.ContentBlock{{Type: ai.ContentImage, MimeType: "image/png"}}}) != "<image image/png>" {
		t.Fatalf("result summary mismatch")
	}
	payload := Payload{Event: "tool_end", SessionID: "s1", CWD: "/repo", ModelProvider: "p", ModelID: "m", ThinkingLevel: "off", ToolName: "read", ToolNamePresent: true}
	env := EnvFor(payload, "/tmp/payload.json")
	if env["PIE_HOOK_EVENT"] != "tool_end" || env["PIE_TOOL_NAME"] != "read" || env["PIE_HOOK_PAYLOAD"] != "/tmp/payload.json" {
		t.Fatalf("env mismatch: %#v", env)
	}
	if CWDFor(Rule{CWD: CWDProject}, "/repo") != "/repo" {
		t.Fatalf("cwd mismatch")
	}
}

func TestHooksReadFileAndPushRulesHelpers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.toml")
	text := "[[hook]]\nevent = \"agent_start\"\ncommand = \"echo ok\"\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok, diagnostics := ReadFile(path, "user")
	if !ok || got != text || len(diagnostics) != 0 {
		t.Fatalf("ReadFile mismatch: ok=%v text=%q diagnostics=%#v", ok, got, diagnostics)
	}
	badPath := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(badPath, []byte("\xff"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, diagnostics := ReadFile(badPath, "user"); ok || len(diagnostics) != 1 || !strings.Contains(diagnostics[0], "invalid UTF-8") {
		t.Fatalf("ReadFile invalid UTF-8 mismatch: ok=%v diagnostics=%#v", ok, diagnostics)
	}
	rules := []Rule{{Event: TurnStart}}
	diagnostics = PushRules(&rules, text, "user")
	if len(diagnostics) != 0 || len(rules) != 2 || rules[1].Event != AgentStart || rules[1].Source != "user" {
		t.Fatalf("PushRules mismatch: rules=%#v diagnostics=%#v", rules, diagnostics)
	}
}

func TestRunnerRunRuleMatchesUpstreamSingleRuleExecution(t *testing.T) {
	var gotRule Rule
	var gotPayload Payload
	runner := NewRunner(RunnerOptions{SessionID: "s1", CWD: t.TempDir(), Executor: func(ctx context.Context, rule Rule, payload Payload) error {
		gotRule = rule
		gotPayload = payload
		return nil
	}})
	rule := Rule{Event: ToolEnd, Command: "hook", Source: "test"}
	payload := Payload{Event: "tool_end", SessionID: "s1", ToolName: "bash", ToolNamePresent: true}
	if err := runner.RunRule(context.Background(), rule, payload); err != nil {
		t.Fatal(err)
	}
	if gotRule.Command != "hook" || gotPayload.Event != "tool_end" || gotPayload.ToolName != "bash" {
		t.Fatalf("run rule mismatch: rule=%#v payload=%#v", gotRule, gotPayload)
	}
}
