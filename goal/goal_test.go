package goal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/harness"
	"github.com/detailyang/pig/session"
)

func TestLatestFromEntriesSkipsClearedAndReadsNewestGoalState(t *testing.T) {
	old := State{Condition: "old", Status: StatusPursuing, UpdatedAt: "2026-01-01T00:00:00Z"}
	latest := State{Condition: "new", Status: StatusPaused, Iterations: 2, LastReason: stringPtr("waiting"), UpdatedAt: "2026-01-02T00:00:00Z"}
	entries := []session.Entry{
		CustomEntry("1", old),
		{EntryType: session.EntryTypeCustom, EntryID: "ignore", CustomType: "other", Data: map[string]any{"status": "pursuing"}},
		CustomEntry("2", latest),
	}
	state, ok := LatestFromEntries(entries)
	if !ok || state.Condition != "new" || state.Status != StatusPaused || state.Iterations != 2 || state.LastReason == nil || *state.LastReason != "waiting" {
		t.Fatalf("latest mismatch: ok=%v state=%#v", ok, state)
	}

	cleared := append(entries, CustomEntry("3", State{Condition: "new", Status: StatusCleared, UpdatedAt: "2026-01-03T00:00:00Z"}))
	if state, ok := CurrentFromEntries(cleared); ok || state.Status != "" {
		t.Fatalf("cleared goal should not be current: ok=%v state=%#v", ok, state)
	}
}

func stringPtr(value string) *string { return &value }

func TestGoalStateTransitions(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	state := NewState("ship the Go port", now)
	if state.Condition != "ship the Go port" || state.Status != StatusPursuing || state.Iterations != 0 || state.LastReason != nil || state.UpdatedAt != now.Format(time.RFC3339) || !state.Active() {
		t.Fatalf("new state mismatch: %#v", state)
	}
	paused, err := Pause(state, "need user input", now.Add(time.Minute))
	if err != nil || paused.Status != StatusPaused || paused.LastReason == nil || *paused.LastReason != "need user input" || !paused.Active() {
		t.Fatalf("pause mismatch: state=%#v err=%v", paused, err)
	}
	resumed, err := Resume(paused, now.Add(2*time.Minute))
	if err != nil || resumed.Status != StatusPursuing || resumed.LastReason == nil {
		t.Fatalf("resume mismatch: state=%#v err=%v", resumed, err)
	}
	cleared := Clear(resumed, now.Add(3*time.Minute))
	if cleared.Status != StatusCleared || cleared.Active() {
		t.Fatalf("clear mismatch: %#v", cleared)
	}
	if _, err := Resume(state, now); err == nil {
		t.Fatal("resume should reject non-paused goal")
	}
}

func TestSetPersistsGoalStateLikeUpstreamSet(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Session: sess, Model: ai.Model{ID: "faux"}})

	state, err := Set(h, "finish the port", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if state.Condition != "finish the port" || state.Status != StatusPursuing || state.Iterations != 0 || state.LastReason != nil {
		t.Fatalf("set state mismatch: %#v", state)
	}
	current, ok := Current(h)
	if !ok || current.Condition != "finish the port" || current.Status != StatusPursuing {
		t.Fatalf("set should persist current goal: ok=%v state=%#v", ok, current)
	}
}

func TestGoalUpstreamPublicNames(t *testing.T) {
	if CUSTOM_TYPE != CustomType || MAX_CONTINUATIONS != MaxContinuations {
		t.Fatalf("upstream constants mismatch: custom=%q max=%d", CUSTOM_TYPE, MAX_CONTINUATIONS)
	}
	var status GoalStatus = GoalStatusPursuing
	if status.AsStr() != "pursuing" || status != StatusPursuing {
		t.Fatalf("status alias mismatch: %#v", status)
	}
	state := GoalState{Condition: "ship", Status: GoalStatusBudgetLimited, UpdatedAt: "2026-01-02T03:04:05Z"}
	if !state.Active() {
		t.Fatalf("state alias should preserve Active behavior: %#v", state)
	}
}

func TestGoalStateJSONMatchesUpstreamSerde(t *testing.T) {
	data, err := json.Marshal(State{Condition: "ship", Status: StatusPursuing, UpdatedAt: "2026-01-02T03:04:05Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"iterations":0`) {
		t.Fatalf("iterations=0 should serialize like upstream u32 field, got %s", data)
	}

	var state State
	if err := json.Unmarshal([]byte(`{"condition":"ship","status":"future","updated_at":"2026-01-02T03:04:05Z"}`), &state); err == nil {
		t.Fatalf("unknown status should fail like upstream enum, got %#v", state)
	}
	if data, err := json.Marshal(State{Condition: "ship", Status: Status("future"), UpdatedAt: "2026-01-02T03:04:05Z"}); err == nil {
		t.Fatalf("unknown status should not marshal like upstream enum, got %s", data)
	}
}

func TestParseDecisionAcceptsEmbeddedJSONAndRequiresReason(t *testing.T) {
	decision, err := ParseDecision("prefix {\"ok\": false, \"reason\": \"missing tests\"} suffix")
	if err != nil || decision.OK || decision.Reason != "missing tests" {
		t.Fatalf("decision mismatch: %#v err=%v", decision, err)
	}
	if _, err := ParseDecision("{\"ok\": true}"); err == nil || err.Error() != "goal evaluator returned an empty reason" {
		t.Fatalf("expected missing reason error, got %v", err)
	}
	long := strings.Repeat("x", 400)
	if _, err := ParseDecision(long); err == nil || !strings.HasPrefix(err.Error(), "goal evaluator returned invalid JSON: [transcript truncated to last 300 chars]\n") {
		t.Fatalf("expected bounded parse error, got %v", err)
	}
}

func TestApplyDecisionStopsContinuesAndBudgetLimits(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	achieved, action := ApplyDecision(NewState("verify", now), Decision{OK: true, Reason: "tests passed"}, now.Add(time.Minute))
	if achieved.Status != StatusAchieved || achieved.Iterations != 1 || action.Kind != ActionStop || action.Payload.OK == nil || !*action.Payload.OK {
		t.Fatalf("achieved mismatch: state=%#v action=%#v", achieved, action)
	}

	continued, action := ApplyDecision(NewState("verify", now), Decision{OK: false, Reason: "missing full test"}, now.Add(time.Minute))
	if continued.Status != StatusPursuing || continued.Iterations != 1 || action.Kind != ActionContinue || !strings.Contains(action.Prompt, "Goal evaluator says") || action.Payload.OK == nil || *action.Payload.OK {
		t.Fatalf("continue mismatch: state=%#v action=%#v", continued, action)
	}

	nearLimit := NewState("verify", now)
	nearLimit.Iterations = MaxContinuations - 1
	limited, action := ApplyDecision(nearLimit, Decision{OK: false, Reason: "still missing"}, now.Add(time.Minute))
	if limited.Status != StatusBudgetLimited || limited.Iterations != MaxContinuations || action.Kind != ActionPause || !strings.Contains(action.Reason, "goal continuation limit reached") {
		t.Fatalf("budget mismatch: state=%#v action=%#v", limited, action)
	}
}

func TestTranscriptFromMessagesIsBoundedTail(t *testing.T) {
	messages := []agent.Message{
		agent.NewUserMessage("hello"),
		agent.NewAssistantMessage("thinking\nanswer"),
		agent.NewToolResultMessage(agent.ToolResult{Name: "bash", Content: "ok", Error: ""}),
	}
	transcript := TranscriptFromMessages(messages, 1000)
	if !strings.Contains(transcript, "User: hello") || !strings.Contains(transcript, "Assistant: thinking\nanswer") || !strings.Contains(transcript, "ToolResult(bash error=false): ok") {
		t.Fatalf("transcript mismatch:\n%s", transcript)
	}
	bounded := TailChars("abcdef", 3)
	if bounded != "[transcript truncated to last 3 chars]\ndef" {
		t.Fatalf("tail mismatch: %q", bounded)
	}
}

func TestGoalTranscriptHelpersMatchUpstreamContentRules(t *testing.T) {
	user := AgentMessageText(agent.Message{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "see"}, {Type: ai.ContentImage}}}})
	if user != "User: see\n[image]" {
		t.Fatalf("user transcript mismatch: %q", user)
	}
	assistant := AgentMessageText(agent.Message{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{{Type: ai.ContentImage}, {Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{Name: "bash"}}, {Type: ai.ContentText, Text: "done"}}}})
	if assistant != "Assistant: bash\ndone" {
		t.Fatalf("assistant transcript mismatch: %q", assistant)
	}
	tool := AgentMessageText(agent.NewToolResultMessage(agent.ToolResult{Name: "bash", ContentBlocks: []ai.ContentBlock{{Type: ai.ContentImage}, {Type: ai.ContentText, Text: "ok"}}, IsError: true}))
	if tool != "ToolResult(bash error=true): ok" {
		t.Fatalf("tool result transcript mismatch: %q", tool)
	}
	if UserContentText(ai.UserContentBlocksValue([]ai.UserContentBlock{{Type: ai.UserContentText, Text: "a"}, {Type: ai.UserContentImage}})) != "a\n[image]" {
		t.Fatalf("user content text mismatch")
	}
}

func TestCurrentReadsHarnessSessionGoalState(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Session: sess, Model: ai.Model{ID: "faux"}})
	state := NewState("ship", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if _, err := sess.AppendCustom(CustomType, state); err != nil {
		t.Fatal(err)
	}
	current, ok := Current(h)
	if !ok || current.Condition != "ship" || current.Status != StatusPursuing {
		t.Fatalf("current mismatch: ok=%v state=%#v", ok, current)
	}
}

func TestGoalPersistenceAndDecisionHelpersMatchUpstream(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Session: sess, Model: ai.Model{ID: "faux"}})
	state := NewState("ship", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	state.LastReason = stringPtr("missing proof")

	if err := AppendState(h, state); err != nil {
		t.Fatal(err)
	}
	current, ok := Current(h)
	if !ok || current.Condition != "ship" {
		t.Fatalf("append state mismatch: ok=%v state=%#v", ok, current)
	}
	payload := GoalPayload(state, boolPtr(true))
	if payload.GoalStatus != StatusPursuing || payload.Condition != "ship" || payload.OK == nil || !*payload.OK || payload.Reason == nil || *payload.Reason != "missing proof" {
		t.Fatalf("goal payload mismatch: %#v", payload)
	}
	decision := PauseDecision("wait", state)
	if decision.Kind != ActionPause || decision.Reason != "wait" || decision.Payload.OK != nil || decision.Payload.GoalStatus != StatusPursuing {
		t.Fatalf("pause decision mismatch: %#v", decision)
	}
	pausePayload, err := json.Marshal(decision.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pausePayload), `"ok":null`) {
		t.Fatalf("pause payload should include explicit ok:null like upstream goal_payload, got %s", pausePayload)
	}
	paused := PersistPause(h, state, "blocked", time.Date(2026, 1, 2, 3, 5, 5, 0, time.UTC))
	if paused.Status != StatusPaused || paused.LastReason == nil || *paused.LastReason != "blocked" {
		t.Fatalf("persist pause state mismatch: %#v", paused)
	}
	current, ok = Current(h)
	if !ok || current.Status != StatusPaused || current.LastReason == nil || *current.LastReason != "blocked" {
		t.Fatalf("persist pause current mismatch: ok=%v state=%#v", ok, current)
	}
}

func TestStopHookEvaluatesGoalAndReturnsContinue(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	state := NewState("finish tests", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if _, err := sess.AppendCustom(CustomType, state); err != nil {
		t.Fatal(err)
	}
	stream := func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		out := ai.NewAssistantMessageEventStream()
		out.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: `{"ok": false, "reason": "missing proof"}`})
		out.Close(ai.DoneReasonStop)
		return out, nil
	}
	h := harness.NewAgentHarness(harness.AgentHarnessOptions{Session: sess, Model: ai.Model{ID: "faux", Provider: ai.Provider("faux"), API: ai.ApiFaux}, StreamFn: stream})
	hook := StopHook(h)
	decision := hook(harness.OnTurnEndContext{Transcript: []agent.AgentMessage{agent.NewUserMessage("run tests")}})
	if decision.Action.Kind != harness.TurnEndContinue || !strings.Contains(decision.Action.Prompt, "missing proof") || decision.Payload["goal_status"] != string(StatusPursuing) {
		t.Fatalf("decision mismatch: %#v", decision)
	}
	current, ok := Current(h)
	if !ok || current.Iterations != 1 || current.LastReason == nil || *current.LastReason != "missing proof" {
		t.Fatalf("persisted state mismatch: ok=%v state=%#v", ok, current)
	}
}
