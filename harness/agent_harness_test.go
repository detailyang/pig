package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/compaction"
	"github.com/detailyang/pig/mcp"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/skills"
	"github.com/detailyang/pig/templates"
	"github.com/detailyang/pig/triggers"
)

func TestDefaultTurnContinuationCapMatchesUpstream(t *testing.T) {
	if DefaultTurnContinuationCap != 25 {
		t.Fatalf("default turn continuation cap mismatch: %d", DefaultTurnContinuationCap)
	}
}

func TestEvaluatorOutputAndErrorsMatchUpstreamSurface(t *testing.T) {
	text := "done"
	output := EvaluatorOutput{LastAssistantText: &text}
	if output.LastAssistantText == nil || *output.LastAssistantText != "done" {
		t.Fatalf("evaluator output mismatch: %#v", output)
	}

	var runErr EvaluatorError = EvaluatorRunError(errors.New("boom"))
	if err := runErr; err == nil || err.Error() != "evaluator agent failed: boom" {
		t.Fatalf("run error mismatch: %v", err)
	}
	var cancelledErr EvaluatorError = EvaluatorCancelledError()
	if err := cancelledErr; !errors.Is(err, ErrEvaluatorCancelled) || err.Error() != "evaluator cancelled" {
		t.Fatalf("cancelled error mismatch: %v", err)
	}
}

func TestReloadSkillsFnAndErrorMatchUpstreamSurface(t *testing.T) {
	called := false
	var reload ReloadSkillsFn = func(ctx context.Context) (skills.LoadSkillsOutput, error) {
		called = true
		return skills.LoadSkillsOutput{Skills: []skills.Skill{{Name: "demo"}}}, nil
	}
	output, err := reload(context.Background())
	if err != nil || !called || len(output.Skills) != 1 || output.Skills[0].Name != "demo" {
		t.Fatalf("reload function mismatch: output=%#v err=%v called=%v", output, err, called)
	}

	var reloadErr ReloadSkillsError = ReloadSkillsNotConfiguredError()
	if err := reloadErr; !errors.Is(err, ErrReloadSkillsNotConfigured) || err.Error() != "reload_skills_fn was not configured at harness construction" {
		t.Fatalf("reload error mismatch: %v", err)
	}
}

func TestPromotionConditionCompatNameMatchesUpstreamSurface(t *testing.T) {
	condition := PromotionCondition{JSONPointer: "/dynamic_trigger/matched_rule_ids", AnyOf: []string{"rule-1"}}
	matched, reason := condition.Evaluate(map[string]any{"dynamic_trigger": map[string]any{"matched_rule_ids": []any{"rule-1"}}})
	if reason != "" || len(matched) != 1 || matched[0] != "rule-1" {
		t.Fatalf("promotion condition mismatch: matched=%#v reason=%q", matched, reason)
	}
}

func TestAgentHarnessOptionsDefaultsMatchUpstreamSurface(t *testing.T) {
	model := ai.Model{ID: "test-model"}
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(model, sess)

	if options.SystemPrompt != "" || options.Model.ID != "test-model" || options.ThinkingLevel != ai.ThinkingOff {
		t.Fatalf("basic options mismatch: %#v", options)
	}
	if len(options.Skills) != 0 || len(options.PromptTemplates) != 0 || len(options.Tools) != 0 {
		t.Fatalf("collection defaults mismatch: %#v", options)
	}
	if options.Session != sess || options.StreamFn != nil || options.BudgetCapUSD != nil {
		t.Fatalf("session/stream/budget defaults mismatch: %#v", options)
	}
	if options.Compaction != compaction.DEFAULT_COMPACTION_SETTINGS {
		t.Fatalf("compaction default mismatch: %#v", options.Compaction)
	}
	if options.TriggerRuntime != triggers.DefaultTriggerRuntimeConfig() {
		t.Fatalf("trigger runtime default mismatch: %#v", options.TriggerRuntime)
	}
	if options.BeforeToolCall != nil || options.AfterToolCall != nil || options.OnControlPlanePrompt != nil || options.BeforeTrigger != nil || options.OnTriggerPrompt != nil || options.BeforeTriggerAction != nil || options.ReloadSkillsFn != nil || options.OnTurnEnd != nil || options.TurnContinuationCap != nil {
		t.Fatalf("hook defaults mismatch: %#v", options)
	}

	options.Skills = []skills.Skill{{Name: "skill"}}
	options.PromptTemplates = []templates.PromptTemplate{{Name: "template"}}
	if options.Skills[0].Name != "skill" || options.PromptTemplates[0].Name != "template" {
		t.Fatalf("resource fields mismatch: %#v", options)
	}
}

func TestAgentHarnessNewCostAndAgentAccessors(t *testing.T) {
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage()))
	harness := NewAgentHarness(options)
	if harness.Agent() == nil {
		t.Fatalf("agent accessor returned nil")
	}
	if snapshot := harness.Cost(); snapshot.TurnCount != 0 || snapshot.Tokens.TotalTokens() != 0 {
		t.Fatalf("initial cost mismatch: %#v", snapshot)
	}

	harness.cost.Record(&ai.Usage{InputTokens: 3, OutputTokens: 4})
	if snapshot := harness.Cost(); snapshot.TurnCount != 1 || snapshot.Tokens.TotalTokens() != 7 {
		t.Fatalf("recorded cost mismatch: %#v", snapshot)
	}
	harness.ResetCost()
	if snapshot := harness.Cost(); snapshot.TurnCount != 0 || snapshot.Tokens.TotalTokens() != 0 {
		t.Fatalf("reset cost mismatch: %#v", snapshot)
	}
}

func TestAgentHarnessSubscribeHarnessUnsubscribesListener(t *testing.T) {
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage())))
	seen := 0
	unsubscribe := harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventSkillsReloaded {
			seen++
		}
	})
	harness.emitHarnessEvent(SkillsReloadedEvent(1))
	unsubscribe()
	harness.emitHarnessEvent(SkillsReloadedEvent(2))
	if seen != 1 {
		t.Fatalf("listener unsubscribe mismatch: %d", seen)
	}
}

func TestAgentHarnessEventBusIsolatesPanickingListener(t *testing.T) {
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage())))
	seen := 0
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventSkillsReloaded {
			seen++
		}
	})
	harness.SubscribeHarness(func(event HarnessEvent) {
		panic("isolated")
	})
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventSkillsReloaded {
			seen++
		}
	})

	harness.emitHarnessEvent(SkillsReloadedEvent(1))
	if seen != 2 {
		t.Fatalf("panicking listener should not stop siblings, seen=%d", seen)
	}
}

func TestAgentHarnessResourceSnapshotsAndReplacement(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	initialSkill := skills.Skill{Name: "alpha", Description: "first"}
	initialTemplate := templates.PromptTemplate{Name: "daily", Content: "hello"}
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.SystemPrompt = "base"
	options.Skills = []skills.Skill{initialSkill}
	options.PromptTemplates = []templates.PromptTemplate{initialTemplate}
	harness := NewAgentHarness(options)

	if harness.Session() != sess {
		t.Fatalf("session accessor mismatch")
	}
	if got := harness.Skills(); len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("skills snapshot mismatch: %#v", got)
	}
	if got := harness.Templates(); len(got) != 1 || got[0].Name != "daily" {
		t.Fatalf("templates snapshot mismatch: %#v", got)
	}
	if prompt := harness.SystemPrompt(); !strings.Contains(prompt, "base") || !strings.Contains(prompt, "- name: alpha") {
		t.Fatalf("system prompt missing base or skills: %q", prompt)
	}

	agentBeforeReplace := harness.Agent()
	replacementSkill := skills.Skill{Name: "beta", Description: "second"}
	harness.ReplaceSkills([]skills.Skill{replacementSkill})
	if harness.Agent() != agentBeforeReplace {
		t.Fatalf("replace skills should keep the same agent instance")
	}
	if got := harness.Skills(); len(got) != 1 || got[0].Name != "beta" {
		t.Fatalf("replaced skills mismatch: %#v", got)
	}
	if prompt := harness.SystemPrompt(); !strings.Contains(prompt, "base") || strings.Contains(prompt, "alpha") || !strings.Contains(prompt, "- name: beta") {
		t.Fatalf("replaced system prompt mismatch: %q", prompt)
	}

	harness.ReplacePromptTemplates([]templates.PromptTemplate{{Name: "weekly", Content: "bye"}})
	if got := harness.Templates(); len(got) != 1 || got[0].Name != "weekly" {
		t.Fatalf("replaced templates mismatch: %#v", got)
	}
}

func TestAgentHarnessReloadSkillsAppliesCatalogAndEmitsEvent(t *testing.T) {
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage()))
	options.SystemPrompt = "base"
	reloadCalls := 0
	options.ReloadSkillsFn = func(context.Context) (skills.LoadSkillsOutput, error) {
		reloadCalls++
		return skills.LoadSkillsOutput{Skills: []skills.Skill{{Name: "fresh", Description: "new"}}, Diagnostics: []skills.Diagnostic{{Code: skills.DiagnosticParseFailed, Message: "frontmatter malformed", Path: "/tmp/bad/SKILL.md"}}}, nil
	}
	harness := NewAgentHarness(options)
	var totals []int
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventSkillsReloaded {
			totals = append(totals, event.Total)
		}
	})

	output, err := harness.ReloadSkillsFromDisk(context.Background())
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if len(output.Skills) != 1 || output.Skills[0].Name != "fresh" {
		t.Fatalf("reload output mismatch: %#v", output)
	}
	if reloadCalls != 1 {
		t.Fatalf("reload should call loader exactly once, got %d", reloadCalls)
	}
	if len(output.Diagnostics) != 1 || output.Diagnostics[0].Message != "frontmatter malformed" {
		t.Fatalf("reload diagnostics should propagate like upstream: %#v", output.Diagnostics)
	}
	if got := harness.Skills(); len(got) != 1 || got[0].Name != "fresh" {
		t.Fatalf("reload did not apply skills: %#v", got)
	}
	if prompt := harness.SystemPrompt(); !strings.Contains(prompt, "- name: fresh") {
		t.Fatalf("reload did not rebuild prompt: %q", prompt)
	}
	if len(totals) != 1 || totals[0] != 1 {
		t.Fatalf("reload event mismatch: %#v", totals)
	}
}

func TestAgentHarnessReloadSkillsPreservesMessageStateAndStreamingFlag(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.SystemPrompt = "base"
	options.ReloadSkillsFn = func(context.Context) (skills.LoadSkillsOutput, error) {
		return skills.LoadSkillsOutput{Skills: []skills.Skill{{Name: "reloaded", Description: "post-reload"}}}, nil
	}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "ok"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)

	if err := harness.Prompt(context.Background(), "hello"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	preState := harness.Agent().State()
	if len(preState.Messages) == 0 || preState.Running {
		t.Fatalf("pre-reload state mismatch: %#v", preState)
	}
	output, err := harness.ReloadSkillsFromDisk(context.Background())
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if len(output.Skills) != 1 || output.Skills[0].Name != "reloaded" {
		t.Fatalf("reload output mismatch: %#v", output)
	}
	postState := harness.Agent().State()
	if len(postState.Messages) != len(preState.Messages) || postState.Running != preState.Running {
		t.Fatalf("reload should preserve messages and running flag: before=%#v after=%#v", preState, postState)
	}
	if !strings.Contains(postState.SystemPrompt, "reloaded") {
		t.Fatalf("reload should rebuild system prompt with new skills: %q", postState.SystemPrompt)
	}
}

func TestAgentHarnessApplyLoadedMCPRegistersToolsHooksAndDirectInject(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess))
	notifications := make(chan mcp.ServerNotification, 1)
	loaded := mcp.LoadedMCP{
		Tools:                []agent.Tool{stubHarnessTool{name: "mcp-tool"}},
		NotificationSources:  []mcp.MCPNotificationSource{{ServerName: "fs", Notifications: notifications}},
		InjectSummaryServers: map[string]bool{"fs": true},
	}

	harness.ApplyLoadedMCP(loaded, triggers.NewDynamicRegistry())
	if tools := harness.Agent().State().Tools; len(tools) == 0 || tools[len(tools)-1].Name() != "mcp-tool" {
		t.Fatalf("mcp tools not appended: %#v", tools)
	}
	waitFor(t, func() bool {
		snapshot := harness.NotificationStatusSnapshot()
		return len(snapshot.Hooks) == 1 && snapshot.Hooks[0].State.Kind == triggers.HookStateConnected
	})
	if harness.Options.BeforeTriggerAction == nil {
		t.Fatal("before trigger action hook not installed")
	}
	summary := "safe summary"
	action := harness.Options.BeforeTriggerAction(BeforeTriggerActionContext{Trigger: triggers.Trigger{Source: triggers.Source{Kind: triggers.SourceMCP, ServerName: "fs"}, SourceLabel: "mcp:fs", EventLabel: "changed", PayloadSummary: &summary}})
	if action.Delivery != TriggerDeliveryInjectSummary || action.Promote.Kind != PromoteSummaryNow {
		t.Fatalf("direct inject action mismatch: %#v", action)
	}
	close(notifications)
}

func TestAgentHarnessAgentProxyMethods(t *testing.T) {
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage())))
	tool := stubHarnessTool{name: "demo"}
	harness.ReplaceTools([]agent.AgentTool{tool})
	if tools := harness.Agent().State().Tools; len(tools) != 1 || tools[0].Name() != "demo" {
		t.Fatalf("replace tools mismatch: %#v", tools)
	}

	seen := 0
	unsubscribe := harness.Subscribe(func(event agent.AgentEvent) {
		if event.Type == agent.EventTypeStart {
			seen++
		}
	})
	if _, err := harness.Agent().Run(context.Background(), nil); err != nil {
		t.Fatalf("agent run failed: %v", err)
	}
	unsubscribe()
	if _, err := harness.Agent().Run(context.Background(), nil); err != nil {
		t.Fatalf("agent run after unsubscribe failed: %v", err)
	}
	if seen != 1 {
		t.Fatalf("subscribe proxy mismatch: %d", seen)
	}

	harness.EnqueueSteering(agent.NewUserMessage("steer"))
	harness.EnqueueFollowUp(agent.NewUserMessage("follow"))
	harness.Abort()
}

func TestAgentHarnessSetModelPersistsAndUpdatesState(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "old", Provider: ai.Provider("old-provider")}, sess))
	model := ai.Model{ID: "gpt-test", Provider: ai.Provider("openai")}

	entryID, err := harness.SetModel(model)
	if err != nil {
		t.Fatalf("set model failed: %v", err)
	}
	if entryID == "" {
		t.Fatalf("set model returned empty entry id")
	}
	state := harness.Agent().State()
	if state.Model == nil || state.Model.ID != "gpt-test" || state.Model.Provider != ai.Provider("openai") {
		t.Fatalf("agent model not updated: %#v", state.Model)
	}
	entry, err := sess.GetEntry(entryID)
	if err != nil || entry == nil {
		t.Fatalf("model entry missing entry=%#v err=%v", entry, err)
	}
	if entry.Type() != session.EntryTypeModelChange || entry.Provider != "openai" || entry.ModelID != "gpt-test" {
		t.Fatalf("model change entry mismatch: %#v", entry)
	}
}

func TestAgentHarnessSetThinkingLevelPersistsAndUpdatesState(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess))

	entryID, err := harness.SetThinkingLevel(ai.ThinkingHigh)
	if err != nil {
		t.Fatalf("set thinking level failed: %v", err)
	}
	if entryID == "" {
		t.Fatalf("set thinking level returned empty entry id")
	}
	state := harness.Agent().State()
	if state.ThinkingLevel == nil || *state.ThinkingLevel != ai.ThinkingHigh {
		t.Fatalf("agent thinking level not updated: %#v", state.ThinkingLevel)
	}
	entry, err := sess.GetEntry(entryID)
	if err != nil || entry == nil {
		t.Fatalf("thinking level entry missing entry=%#v err=%v", entry, err)
	}
	if entry.Type() != session.EntryTypeThinkingLevelChange || entry.ThinkingLevel != "high" {
		t.Fatalf("thinking level entry mismatch: %#v", entry)
	}
}

func TestAgentHarnessRehydrateFromSessionRestoresMessagesModelAndThinking(t *testing.T) {
	ai.ClearBuiltinModels()
	ai.ClearCustomModels()
	t.Cleanup(func() { ai.ClearBuiltinModels(); ai.ClearCustomModels() })
	ai.RegisterBuiltinModel(ai.Model{ID: "warm", Provider: ai.Provider("test-provider"), API: ai.Api("test-api")})
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendThinkingLevelChange("high"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendModelChange("test-provider", "warm"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "cold", Provider: ai.Provider("cold-provider")}, sess))

	ctx, err := harness.RehydrateFromSession()
	if err != nil {
		t.Fatalf("rehydrate failed: %v", err)
	}
	if ctx.ThinkingLevel != "high" || len(ctx.Messages) != 1 || ctx.Model == nil || ctx.Model.Provider != "test-provider" || ctx.Model.ModelID != "warm" {
		t.Fatalf("session context mismatch: %#v", ctx)
	}
	state := harness.Agent().State()
	if len(state.Messages) != 1 || state.ThinkingLevel == nil || *state.ThinkingLevel != ai.ThinkingHigh || state.Model == nil || state.Model.ID != "warm" || state.Model.Provider != ai.Provider("test-provider") {
		t.Fatalf("agent state mismatch after rehydrate: %#v", state)
	}
}

func TestAgentHarnessRehydrateFromSessionKeepsCurrentModelWhenCatalogMisses(t *testing.T) {
	ai.ClearBuiltinModels()
	ai.ClearCustomModels()
	t.Cleanup(func() { ai.ClearBuiltinModels(); ai.ClearCustomModels() })
	sess := session.NewSession(session.NewMemorySessionStorage())
	if _, err := sess.AppendModelChange("missing-provider", "missing-model"); err != nil {
		t.Fatal(err)
	}
	cold := ai.Model{ID: "cold", Provider: ai.Provider("cold-provider")}
	harness := NewAgentHarness(NewAgentHarnessOptions(cold, sess))

	ctx, err := harness.RehydrateFromSession()
	if err != nil {
		t.Fatalf("rehydrate failed: %v", err)
	}
	if ctx.Model == nil || ctx.Model.Provider != "missing-provider" || ctx.Model.ModelID != "missing-model" {
		t.Fatalf("session context model mismatch: %#v", ctx.Model)
	}
	state := harness.Agent().State()
	if state.Model == nil || state.Model.ID != cold.ID || state.Model.Provider != cold.Provider {
		t.Fatalf("catalog miss should keep current model: %#v", state.Model)
	}
}

func TestAgentHarnessMoveToMovesLeafRehydratesAndEmitsBranchEvent(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	first, err := sess.AppendMessage(agent.NewUserMessage("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := sess.AppendMessage(agent.NewUserMessage("second"))
	if err != nil {
		t.Fatal(err)
	}
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess))
	var branchEvent *HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventBranch {
			copyEvent := event
			branchEvent = &copyEvent
		}
	})

	summaryEntryID, err := harness.MoveTo(first, &session.BranchSummaryInput{Summary: "fork"})
	if err != nil {
		t.Fatalf("move to failed: %v", err)
	}
	if summaryEntryID == nil || *summaryEntryID == "" {
		t.Fatalf("summary entry id missing: %#v", summaryEntryID)
	}
	leaf, err := sess.LeafID()
	if err != nil || leaf == nil || *leaf != *summaryEntryID {
		t.Fatalf("leaf mismatch leaf=%v err=%v", leaf, err)
	}
	if branchEvent == nil || branchEvent.FromEntryID == nil || *branchEvent.FromEntryID != second || branchEvent.ToEntryID == nil || *branchEvent.ToEntryID != first || branchEvent.SummaryEntryID == nil || *branchEvent.SummaryEntryID != *summaryEntryID {
		t.Fatalf("branch event mismatch: %#v", branchEvent)
	}
	state := harness.Agent().State()
	if len(state.Messages) != 2 || state.Messages[0].LLM == nil || state.Messages[0].LLM.Content[0].Text != "first" || state.Messages[1].Custom == nil {
		t.Fatalf("agent state should rehydrate moved branch and summary: %#v", state.Messages)
	}
}

func TestAgentHarnessPromptRunsAgentAndEmitsSessionStart(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess))
	var starts []int
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventSessionStart {
			starts = append(starts, event.MessagesReplayed)
		}
	})

	if err := harness.Prompt(context.Background(), "hello"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	state := harness.Agent().State()
	if len(state.Messages) != 2 || state.Messages[0].LLM == nil || state.Messages[0].LLM.Content[0].Text != "hello" || state.Messages[1].LLM == nil {
		t.Fatalf("prompt state mismatch: %#v", state.Messages)
	}
	if len(starts) != 1 || starts[0] != 0 {
		t.Fatalf("session start event mismatch: %#v", starts)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Type() != session.EntryTypeMessage || entries[1].Type() != session.EntryTypeMessage {
		t.Fatalf("prompt should persist user and assistant messages, got %#v", entries)
	}
	if entries[0].Message == nil || entries[0].Message.LLM == nil || entries[0].Message.LLM.Content[0].Text != "hello" {
		t.Fatalf("persisted user message mismatch: %#v", entries[0])
	}
}

func TestAgentHarnessPromptReportsSessionPersistenceFailureWithUpstreamContext(t *testing.T) {
	storage := &failMessageAppendStorage{MemoryStorage: session.NewMemorySessionStorage()}
	sess := session.NewSession(storage)
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess))

	err := harness.Prompt(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "session append message") || !strings.Contains(err.Error(), "message append failed") {
		t.Fatalf("prompt persistence error mismatch: %v", err)
	}
}

func TestAgentHarnessPromptFromTemplateInterpolatesAndPrompts(t *testing.T) {
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage()))
	options.PromptTemplates = []templates.PromptTemplate{{Name: "greet", Content: "hello {{name}}"}}
	harness := NewAgentHarness(options)

	if err := harness.PromptFromTemplate(context.Background(), "greet", map[string]any{"name": "Ada"}); err != nil {
		t.Fatalf("prompt from template failed: %v", err)
	}
	state := harness.Agent().State()
	if len(state.Messages) == 0 || state.Messages[0].LLM == nil || state.Messages[0].LLM.Content[0].Text != "hello Ada" {
		t.Fatalf("template prompt mismatch: %#v", state.Messages)
	}
	if err := harness.PromptFromTemplate(context.Background(), "missing", nil); err == nil || !strings.Contains(err.Error(), "unknown prompt template: missing") {
		t.Fatalf("missing template error mismatch: %v", err)
	}
}

func TestAgentHarnessContinueRunsExistingConversation(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess))
	if err := harness.Prompt(context.Background(), "hello"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	entriesBefore, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	before := len(harness.Agent().State().Messages)
	if err := harness.Continue(context.Background()); err != nil {
		t.Fatalf("continue failed: %v", err)
	}
	after := len(harness.Agent().State().Messages)
	if after <= before {
		t.Fatalf("continue should append assistant message, before=%d after=%d", before, after)
	}
	entriesAfter, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entriesBefore) != 2 || len(entriesAfter) != 3 {
		t.Fatalf("continue should persist only the new assistant message, before=%d after=%d entries=%#v", len(entriesBefore), len(entriesAfter), entriesAfter)
	}
}

func TestAgentHarnessPromptWithImagesBuildsUserBlocks(t *testing.T) {
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage())))
	images := []ai.ContentBlock{{Type: ai.ContentImage, MimeType: "image/png", Data: "abc"}}
	if err := harness.PromptWithImages(context.Background(), "look", images); err != nil {
		t.Fatalf("prompt with images failed: %v", err)
	}
	state := harness.Agent().State()
	if len(state.Messages) == 0 || state.Messages[0].LLM == nil || len(state.Messages[0].LLM.Content) != 2 {
		t.Fatalf("image prompt content mismatch: %#v", state.Messages)
	}
	if state.Messages[0].LLM.Content[0].Type != ai.ContentText || state.Messages[0].LLM.Content[0].Text != "look" || state.Messages[0].LLM.Content[1].Type != ai.ContentImage {
		t.Fatalf("image prompt block order mismatch: %#v", state.Messages[0].LLM.Content)
	}

	harness = NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage())))
	if err := harness.PromptWithImages(context.Background(), "", images); err != nil {
		t.Fatalf("image-only prompt failed: %v", err)
	}
	if content := harness.Agent().State().Messages[0].LLM.Content; len(content) != 1 || content[0].Type != ai.ContentImage {
		t.Fatalf("image-only prompt mismatch: %#v", content)
	}
}

func TestAgentHarnessCostTracksAssistantUsageAndBudgetCap(t *testing.T) {
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage()))
	options.StreamFn = func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "ok"}})
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventUsage, Usage: &ai.Usage{InputTokens: 3, OutputTokens: 4, Cost: &ai.UsageCost{Input: 0.01, Output: 0.02, Total: 0.03}}})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	capUSD := 0.02
	options.BudgetCapUSD = &capUSD
	harness := NewAgentHarness(options)

	if err := harness.Prompt(context.Background(), "hello"); err != nil {
		t.Fatalf("first prompt should be allowed before cap is exceeded: %v", err)
	}
	snapshot := harness.Cost()
	if snapshot.TurnCount != 1 || snapshot.Tokens.InputTokens != 3 || snapshot.Tokens.OutputTokens != 4 || snapshot.TotalCost() != 0.03 {
		t.Fatalf("cost snapshot mismatch: %#v", snapshot)
	}
	if err := harness.Prompt(context.Background(), "again"); err == nil || !strings.Contains(err.Error(), "budget cap reached") {
		t.Fatalf("second prompt should fail budget cap, got %v", err)
	}
	harness.ResetCost()
	if err := harness.Prompt(context.Background(), "after reset"); err != nil {
		t.Fatalf("reset cost should unblock budget gate: %v", err)
	}
}

func TestAgentHarnessAbortPromptlyUnblocksStalledStream(t *testing.T) {
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage()))
	started := make(chan struct{})
	options.StreamFn = func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream().MarkLive()
		close(started)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	done := make(chan error, 1)
	go func() {
		done <- harness.Prompt(context.Background(), "hi")
	}()
	<-started
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	harness.Abort()

	select {
	case err := <-done:
		if err == nil || err.Error() != "aborted" {
			t.Fatalf("expected aborted prompt, got %v", err)
		}
	case <-deadline.C:
		t.Fatal("abort should unblock stalled stream promptly")
	}
}

func TestAgentHarnessAbortCancelsInFlightPromptWithoutPersistingAssistant(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	started := make(chan struct{})
	release := make(chan struct{})
	options.StreamFn = func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream().MarkLive()
		close(started)
		go func() {
			<-release
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "should-not-arrive"}})
			stream.Close(ai.DoneReasonStop)
		}()
		return stream, nil
	}
	harness := NewAgentHarness(options)
	done := make(chan error, 1)
	go func() {
		done <- harness.Prompt(context.Background(), "hi")
	}()
	<-started
	harness.Abort()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "abort") {
			t.Fatalf("expected abort error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("abort should complete in-flight prompt")
	}
	close(release)

	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	userCount := 0
	assistantCount := 0
	for _, entry := range entries {
		if entry.Type() != session.EntryTypeMessage || entry.Message == nil || entry.Message.LLM == nil {
			continue
		}
		switch entry.Message.LLM.Role {
		case ai.RoleUser:
			userCount++
		case ai.RoleAssistant:
			assistantCount++
		}
	}
	if userCount != 1 || assistantCount != 0 {
		t.Fatalf("aborted prompt persistence mismatch: users=%d assistants=%d entries=%#v", userCount, assistantCount, entries)
	}
}

func TestAgentHarnessPersistsControlPlanePromptAudit(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.Tools = []agent.AgentTool{harnessAskingTool{}}
	options.OnControlPlanePrompt = agent.DenyControlPlanePromptHook("no ui")
	streamCalls := 0
	options.StreamFn = func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		if streamCalls == 1 {
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "ask", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		}
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "done"}})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)

	if err := harness.Prompt(context.Background(), "ask"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var audit *session.Entry
	for index := range entries {
		if entries[index].Type() == session.EntryTypeCustom && entries[index].CustomType == "control_plane_prompt" {
			audit = &entries[index]
		}
	}
	if audit == nil {
		t.Fatalf("missing control plane prompt audit in entries: %#v", entries)
	}
	data, ok := audit.Data.(map[string]any)
	if !ok || data["tool_call_id"] != "call-1" || data["tool_name"] != "ask" || data["decision"] != agent.ControlPlaneDeny || data["reason"] != "no ui" || data["label"] == "" || data["at"] == "" {
		t.Fatalf("control plane audit payload mismatch: %#v", audit.Data)
	}
	if hash, ok := data["args_hash"].(string); !ok || len(hash) != 64 {
		t.Fatalf("control plane audit args_hash mismatch: %#v", data["args_hash"])
	}
}

func TestAgentHarnessControlPlanePromptNoHookAuditsFailClosedDeny(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.Tools = []agent.AgentTool{harnessAskingTool{}}
	streamCalls := 0
	options.StreamFn = func(context.Context, ai.Model, []ai.Message, []ai.Tool, ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		if streamCalls == 1 {
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "ask", Arguments: map[string]any{"path": "raw.md"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		}
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "done"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)

	if err := harness.Prompt(context.Background(), "ask"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	audits := filterEntriesByCustomType(entries, "control_plane_prompt")
	if len(audits) != 1 {
		t.Fatalf("expected one control plane prompt audit: %#v", entries)
	}
	data := audits[0].Data.(map[string]any)
	reason, _ := data["reason"].(string)
	if data["decision"] != agent.ControlPlaneDeny || !strings.Contains(reason, "no on_control_plane_prompt hook") {
		t.Fatalf("control plane no-hook audit mismatch: %#v", data)
	}
}

func TestControlPlanePromptAuditLabelCapMatchesUpstream(t *testing.T) {
	short := strings.Repeat("界", 200)
	if got := capControlPlaneAuditLabel(short); got != short {
		t.Fatalf("short label should pass through, got %q", got)
	}
	long := strings.Repeat("界", 201)
	got := capControlPlaneAuditLabel(long)
	if len([]rune(got)) != 200 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long label should cap to 200 chars with ellipsis, got chars=%d value=%q", len([]rune(got)), got)
	}
}

func TestAgentHarnessForceCompactPersistsEventAndRewritesState(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	oldUser, _ := sess.AppendMessage(agent.NewUserMessage("old user"))
	oldAssistant, _ := sess.AppendMessage(agent.NewAssistantMessage("old assistant"))
	newUser, _ := sess.AppendMessage(agent.NewUserMessage("new user"))
	newAssistant, _ := sess.AppendMessage(agent.NewAssistantMessage("new assistant"))
	_ = oldUser
	_ = oldAssistant
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.Compaction = compaction.DefaultSettings()
	harness := NewAgentHarness(options)
	settings := options.Compaction
	settings.KeepRecentTokens = 2
	harness.SetCompactionSettings(settings)
	if _, err := harness.RehydrateFromSession(); err != nil {
		t.Fatal(err)
	}
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventCompaction {
			events = append(events, event)
		}
	})

	compacted, err := harness.ForceCompact(context.Background(), compaction.SummarizerFunc(func(ctx context.Context, request compaction.SummarizationRequest) (string, error) {
		if !strings.Contains(request.Conversation, "old user") || strings.Contains(request.Conversation, "new user") {
			t.Fatalf("summarizer conversation mismatch: %q", request.Conversation)
		}
		return "summary of old context", nil
	}))
	if err != nil {
		t.Fatalf("force compact failed: %v", err)
	}
	if !compacted {
		t.Fatalf("force compact should report true")
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var compactEntry *session.Entry
	for index := range entries {
		if entries[index].Type() == session.EntryTypeCompaction {
			compactEntry = &entries[index]
		}
	}
	if compactEntry == nil || compactEntry.Summary != "summary of old context" || compactEntry.FirstKeptEntryID != newUser || compactEntry.TokensBefore == 0 || compactEntry.FromHook == nil || !*compactEntry.FromHook {
		t.Fatalf("compaction entry mismatch: %#v", compactEntry)
	}
	if len(events) != 1 || !events[0].FromHook || events[0].Summary != "summary of old context" || events[0].TokensBefore == 0 {
		t.Fatalf("compaction event mismatch: %#v", events)
	}
	state := harness.Agent().State()
	if len(state.Messages) != 3 || state.Messages[0].Custom == nil || state.Messages[0].Custom.Role != "compaction_summary" || state.Messages[1].LLM.Content[0].Text != "new user" || state.Messages[2].LLM.Content[0].Text != "new assistant" {
		t.Fatalf("compacted state mismatch: %#v; kept ids %s %s", state.Messages, newUser, newAssistant)
	}
}

func TestAgentHarnessForceCompactSkipsWhenSessionBranchReadFails(t *testing.T) {
	storage := session.NewMemorySessionStorage()
	sess := session.NewSession(storage)
	missing := "missing-parent"
	storage.AppendEntry(session.NewMessageEntry("leaf", &missing, "ts", agent.NewUserMessage("orphan")))
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	harness := NewAgentHarness(options)
	initialState := harness.Agent().State()
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventCompaction {
			events = append(events, event)
		}
	})

	compacted, err := harness.ForceCompact(context.Background(), compaction.SummarizerFunc(func(ctx context.Context, request compaction.SummarizationRequest) (string, error) {
		t.Fatal("summarizer should not be called when session branch read fails")
		return "", nil
	}))
	if err != nil {
		t.Fatalf("force compact should not fail on branch read error: %v", err)
	}
	if compacted {
		t.Fatalf("force compact should report false on branch read error")
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Type() == session.EntryTypeCompaction {
			t.Fatalf("compaction entry should not be appended on branch read failure: %#v", entries)
		}
	}
	if len(events) != 1 || !strings.HasPrefix(events[0].Summary, "compaction skipped:") || events[0].TokensBefore != 0 {
		t.Fatalf("expected diagnostic compaction event, got %#v", events)
	}
	if state := harness.Agent().State(); len(state.Messages) != len(initialState.Messages) || state.ErrorMessage != initialState.ErrorMessage {
		t.Fatalf("agent state should not mutate on skipped compaction: before=%#v after=%#v", initialState, state)
	}
}

func TestAgentHarnessPromptRunsAutoCompactionBeforeUserMessage(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	_, _ = sess.AppendMessage(agent.NewUserMessage("old user"))
	_, _ = sess.AppendMessage(harnessAssistantWithUsage("old assistant", &ai.Usage{InputTokens: 90, OutputTokens: 1}))
	newUser, _ := sess.AppendMessage(agent.NewUserMessage("new user"))
	_, _ = sess.AppendMessage(agent.NewAssistantMessage("new assistant"))
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model", ContextWindow: 100}, sess)
	settings := compaction.DefaultSettings()
	settings.KeepRecentTokens = 2
	options.Compaction = settings
	options.CompactionSummarizer = compaction.SummarizerFunc(func(ctx context.Context, request compaction.SummarizationRequest) (string, error) {
		return "auto summary", nil
	})
	harness := NewAgentHarness(options)
	if _, err := harness.RehydrateFromSession(); err != nil {
		t.Fatal(err)
	}
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventCompaction {
			events = append(events, event)
		}
	})

	if err := harness.Prompt(context.Background(), "after compact"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var compactEntry *session.Entry
	for index := range entries {
		if entries[index].Type() == session.EntryTypeCompaction {
			compactEntry = &entries[index]
		}
	}
	if compactEntry == nil || compactEntry.Summary != "auto summary" || compactEntry.FirstKeptEntryID != newUser || compactEntry.FromHook != nil {
		t.Fatalf("auto compaction entry mismatch: %#v", compactEntry)
	}
	if len(events) != 1 || events[0].FromHook || events[0].Summary != "auto summary" {
		t.Fatalf("auto compaction event mismatch: %#v", events)
	}
}

func TestAgentHarnessPromptCombinesSkillsAndAutoCompactionContext(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	_, _ = sess.AppendMessage(agent.NewUserMessage("old user"))
	_, _ = sess.AppendMessage(harnessAssistantWithUsage("old assistant", &ai.Usage{InputTokens: 90, OutputTokens: 1}))
	newUser, _ := sess.AppendMessage(agent.NewUserMessage("new user"))
	_, _ = sess.AppendMessage(agent.NewAssistantMessage("new assistant"))
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model", ContextWindow: 100}, sess)
	options.SystemPrompt = "base system"
	options.Skills = []skills.Skill{{Name: "review", Description: "Review code", Content: "Use concise review notes."}}
	settings := compaction.DefaultSettings()
	settings.KeepRecentTokens = 2
	options.Compaction = settings
	options.CompactionSummarizer = compaction.SummarizerFunc(func(ctx context.Context, request compaction.SummarizationRequest) (string, error) {
		return "auto summary with skill", nil
	})
	var llmMessages []ai.Message
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		llmMessages = append([]ai.Message(nil), messages...)
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	if _, err := harness.RehydrateFromSession(); err != nil {
		t.Fatal(err)
	}

	if err := harness.Prompt(context.Background(), "after compact"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if len(llmMessages) < 4 || llmMessages[0].Role != ai.RoleSystem || !strings.Contains(llmMessages[0].Content[0].Text, "base system") || !strings.Contains(llmMessages[0].Content[0].Text, "review") {
		t.Fatalf("system prompt should include base prompt and skills: %#v", llmMessages)
	}
	if llmMessages[1].Role != ai.RoleUser || !strings.Contains(llmMessages[1].Content[0].Text, "auto summary with skill") {
		t.Fatalf("compaction summary should lead compacted context: %#v", llmMessages)
	}
	if llmMessages[len(llmMessages)-1].Role != ai.RoleUser || llmMessages[len(llmMessages)-1].Content[0].Text != "after compact" {
		t.Fatalf("new prompt should be preserved after compaction: %#v", llmMessages)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var compactEntry *session.Entry
	for index := range entries {
		if entries[index].Type() == session.EntryTypeCompaction {
			compactEntry = &entries[index]
		}
	}
	if compactEntry == nil || compactEntry.Summary != "auto summary with skill" || compactEntry.FirstKeptEntryID != newUser {
		t.Fatalf("combined compaction entry mismatch: %#v", compactEntry)
	}
}

func TestAgentHarnessMoveToBranchSummaryIsVisibleToNextPrompt(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	rootMessage, _ := sess.AppendMessage(agent.NewUserMessage("root context"))
	_, _ = sess.AppendMessage(agent.NewAssistantMessage("old branch answer"))
	var llmMessages []ai.Message
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		llmMessages = append([]ai.Message(nil), messages...)
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	if _, err := harness.MoveTo(rootMessage, &session.BranchSummaryInput{Summary: "branch summary for next run"}); err != nil {
		t.Fatalf("move to branch failed: %v", err)
	}

	if err := harness.Prompt(context.Background(), "continue branch"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if len(llmMessages) < 3 || llmMessages[0].Role != ai.RoleUser || llmMessages[0].Content[0].Text != "root context" {
		t.Fatalf("branch prompt should preserve target context: %#v", llmMessages)
	}
	foundSummary := false
	for _, message := range llmMessages {
		if message.Role == ai.RoleUser && strings.Contains(message.Content[0].Text, "branch summary for next run") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatalf("branch summary should be visible to LLM: %#v", llmMessages)
	}
	if llmMessages[len(llmMessages)-1].Role != ai.RoleUser || llmMessages[len(llmMessages)-1].Content[0].Text != "continue branch" {
		t.Fatalf("new branch prompt should be last: %#v", llmMessages)
	}
}

func TestAgentHarnessAutoCompactionUsesDefaultAISummarizer(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	_, _ = sess.AppendMessage(agent.NewUserMessage("old user"))
	_, _ = sess.AppendMessage(harnessAssistantWithUsage("old assistant", &ai.Usage{InputTokens: 9000, OutputTokens: 1}))
	newUser, _ := sess.AppendMessage(agent.NewUserMessage("new user"))
	_, _ = sess.AppendMessage(agent.NewAssistantMessage("new assistant"))
	streamCalls := 0
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model", ContextWindow: 10000}, sess)
	settings := compaction.DefaultSettings()
	settings.KeepRecentTokens = 2
	options.Compaction = settings
	options.StreamFn = func(ctx context.Context, model ai.Model, llm []ai.Message, tools []ai.Tool, options ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		if streamCalls == 1 {
			if len(llm) != 2 || llm[0].Role != ai.RoleSystem || llm[1].Role != ai.RoleUser || !strings.Contains(llm[1].Content[0].Text, "old user") {
				t.Fatalf("summarizer request mismatch: %#v", llm)
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "stream summary"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		}
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "answer"}})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	if _, err := harness.RehydrateFromSession(); err != nil {
		t.Fatal(err)
	}

	if err := harness.Prompt(context.Background(), "after compact"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if streamCalls != 2 {
		t.Fatalf("expected summarizer stream plus prompt stream, got %d", streamCalls)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var compactEntry *session.Entry
	for index := range entries {
		if entries[index].Type() == session.EntryTypeCompaction {
			compactEntry = &entries[index]
		}
	}
	if compactEntry == nil || compactEntry.Summary != "stream summary" || compactEntry.FirstKeptEntryID != newUser {
		t.Fatalf("default summarizer compaction mismatch: %#v", compactEntry)
	}
}

func TestAgentHarnessRunEvaluatorUsesIsolatedToollessAgent(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "eval-model"}, sess)
	options.SystemPrompt = "judge system"
	options.ThinkingLevel = ai.ThinkingHigh
	options.Tools = []agent.AgentTool{stubHarnessTool{name: "parent-tool"}}
	var calls int
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		calls++
		if model.ID != "eval-model" || len(tools) != 0 || streamOptions.ThinkingLevel != ai.ThinkingHigh {
			t.Fatalf("evaluator stream args mismatch: model=%#v tools=%#v options=%#v", model, tools, streamOptions)
		}
		if len(messages) != 2 || messages[0].Role != ai.RoleSystem || messages[0].Content[0].Text != "judge system" || messages[1].Role != ai.RoleUser || messages[1].Content[0].Text != "is goal done?" {
			t.Fatalf("evaluator messages mismatch: %#v", messages)
		}
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, ContentIndex: 0, Delta: "yes"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	harness.cost.Record(&ai.Usage{InputTokens: 1, OutputTokens: 2})

	output, err := harness.RunEvaluator(context.Background(), "is goal done?")
	if err != nil {
		t.Fatalf("run evaluator failed: %v", err)
	}
	if output.LastAssistantText == nil || *output.LastAssistantText != "yes" || calls != 1 {
		t.Fatalf("evaluator output mismatch: %#v calls=%d", output, calls)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("evaluator should not persist to parent session: %#v", entries)
	}
	if snapshot := harness.Cost(); snapshot.TurnCount != 1 || snapshot.Tokens.TotalTokens() != 3 {
		t.Fatalf("evaluator should not update parent cost: %#v", snapshot)
	}
	if len(harness.Agent().State().Messages) != 0 {
		t.Fatalf("evaluator should not mutate parent agent state: %#v", harness.Agent().State().Messages)
	}
}

func TestAgentHarnessRunEvaluatorWithConfigUsesExplicitInputs(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "parent-model"}, sess)
	options.SystemPrompt = "parent system"
	options.ThinkingLevel = ai.ThinkingHigh
	options.Tools = []agent.AgentTool{stubHarnessTool{name: "parent-tool"}}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		if model.ID != "eval-model" || len(tools) != 0 || streamOptions.ThinkingLevel != ai.ThinkingOff {
			t.Fatalf("evaluator stream args mismatch: model=%#v tools=%#v options=%#v", model, tools, streamOptions)
		}
		if len(messages) != 2 || messages[0].Role != ai.RoleSystem || messages[0].Content[0].Text != "eval system" || messages[1].Role != ai.RoleUser || messages[1].Content[0].Text != "eval prompt" {
			t.Fatalf("evaluator messages mismatch: %#v", messages)
		}
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "verdict"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)

	output, err := harness.RunEvaluatorWithConfig(context.Background(), "eval system", "eval prompt", ai.Model{ID: "eval-model"}, ai.ThinkingOff)
	if err != nil {
		t.Fatalf("run evaluator failed: %v", err)
	}
	if output.LastAssistantText == nil || *output.LastAssistantText != "verdict" {
		t.Fatalf("evaluator output mismatch: %#v", output)
	}
	if state := harness.Agent().State(); len(state.Messages) != 0 || state.SystemPrompt != buildSystemPrompt("parent system", nil) {
		t.Fatalf("explicit evaluator should not mutate parent state: %#v", state)
	}
}

func TestAgentHarnessRunEvaluatorJoinsAndCapsLastAssistantText(t *testing.T) {
	options := NewAgentHarnessOptions(ai.Model{ID: "eval-model"}, session.NewSession(session.NewMemorySessionStorage()))
	long := strings.Repeat("你", 4097)
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: "first"}})
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentThinking, Thinking: "skip"}})
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventContentBlock, ContentBlock: &ai.ContentBlock{Type: ai.ContentText, Text: long}})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}

	output, err := NewAgentHarness(options).RunEvaluator(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("run evaluator failed: %v", err)
	}
	if output.LastAssistantText == nil {
		t.Fatalf("expected assistant text")
	}
	if len(*output.LastAssistantText) > 4096 || !utf8.ValidString(*output.LastAssistantText) || !strings.HasPrefix(*output.LastAssistantText, "first\n") {
		t.Fatalf("assistant text should be joined and capped on rune boundary: bytes=%d", len(*output.LastAssistantText))
	}
}

func TestAgentHarnessRunEvaluatorMapsCancellation(t *testing.T) {
	options := NewAgentHarnessOptions(ai.Model{ID: "eval-model"}, session.NewSession(session.NewMemorySessionStorage()))
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewAgentHarness(options).RunEvaluator(ctx, "prompt")
	if !errors.Is(err, ErrEvaluatorCancelled) {
		t.Fatalf("expected evaluator cancelled, got %v", err)
	}
}

func TestAgentHarnessRunEvaluatorWrapsRunError(t *testing.T) {
	boom := errors.New("boom")
	options := NewAgentHarnessOptions(ai.Model{ID: "eval-model"}, session.NewSession(session.NewMemorySessionStorage()))
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		return nil, boom
	}

	_, err := NewAgentHarness(options).RunEvaluator(context.Background(), "prompt")
	if !errors.Is(err, boom) || err.Error() != "evaluator agent failed: boom" {
		t.Fatalf("expected wrapped evaluator run error, got %v", err)
	}
}

func TestAgentHarnessPromptRunsTurnEndContinuationAndAudits(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	var prompts []string
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		last := messages[len(messages)-1]
		prompts = append(prompts, last.Content[0].Text)
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "answer to " + last.Content[0].Text})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	var hookContexts []OnTurnEndContext
	options.OnTurnEnd = func(ctx OnTurnEndContext) TurnEndDecision {
		hookContexts = append(hookContexts, ctx)
		if ctx.ContinuationCount == 0 {
			return NewTurnEndDecision(ContinueTurnEnd("follow up"), map[string]any{"why": "more"})
		}
		return NewTurnEndDecision(StopTurnEnd(), map[string]any{"done": true})
	}
	harness := NewAgentHarness(options)
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTurnEnded {
			events = append(events, event)
		}
	})

	if err := harness.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if strings.Join(prompts, ",") != "start,follow up" {
		t.Fatalf("continuation prompts mismatch: %#v", prompts)
	}
	if len(hookContexts) != 2 || hookContexts[0].ContinuationCount != 0 || hookContexts[0].LastUserPrompt == nil || *hookContexts[0].LastUserPrompt != "start" || hookContexts[1].ContinuationCount != 1 || hookContexts[1].LastUserPrompt == nil || *hookContexts[1].LastUserPrompt != "follow up" {
		t.Fatalf("hook contexts mismatch: %#v", hookContexts)
	}
	if len(events) != 2 || events[0].Decision != "continue" || events[0].ContinuationCount != 1 || events[0].NextPromptPreview == nil || *events[0].NextPromptPreview != "follow up" || events[1].Decision != "stop" || events[1].ContinuationCount != 1 {
		t.Fatalf("turn ended events mismatch: %#v", events)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	var audits []session.Entry
	for _, entry := range entries {
		if entry.Type() == session.EntryTypeCustom && entry.CustomType == "turn_end_decision" {
			audits = append(audits, entry)
		}
	}
	firstAudit := audits[0].Data.(map[string]any)
	secondAudit := audits[1].Data.(map[string]any)
	if len(audits) != 2 || firstAudit["decision"] != "continue" || firstAudit["continuation_count"] != uint32(1) || firstAudit["next_prompt_preview"] != "follow up" || secondAudit["decision"] != "stop" || secondAudit["continuation_count"] != uint32(1) {
		t.Fatalf("turn end audit mismatch: %#v", audits)
	}
}

func TestAgentHarnessTurnEndNoopWritesNoAuditOrEvent(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.OnTurnEnd = func(ctx OnTurnEndContext) TurnEndDecision {
		return NewTurnEndDecision(NoopTurnEnd(), map[string]any{"ignored": true})
	}
	harness := NewAgentHarness(options)
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTurnEnded {
			events = append(events, event)
		}
	})

	if err := harness.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Type() == session.EntryTypeCustom && entry.CustomType == "turn_end_decision" {
			t.Fatalf("noop should not write turn end audit: %#v", entry)
		}
	}
	if len(events) != 0 {
		t.Fatalf("noop should not emit turn ended event: %#v", events)
	}
}

func TestAgentHarnessTurnEndContinuationCapStopsBeforeHook(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	cap := uint32(1)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.TurnContinuationCap = &cap
	var hookCalls int
	options.OnTurnEnd = func(ctx OnTurnEndContext) TurnEndDecision {
		hookCalls++
		return NewTurnEndDecision(ContinueTurnEnd("again"), nil)
	}
	harness := NewAgentHarness(options)
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTurnEnded {
			events = append(events, event)
		}
	})

	if err := harness.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if hookCalls != 1 {
		t.Fatalf("hook should not run after cap is reached, got %d calls", hookCalls)
	}
	if len(events) != 2 || events[0].Decision != "continue" || events[1].Decision != "budget_limited" || events[1].ContinuationCount != 1 || events[1].Reason == nil {
		t.Fatalf("cap events mismatch: %#v", events)
	}
}

func TestAgentHarnessTurnEndDecisionAppendFailureEmitsUpstreamPersistenceError(t *testing.T) {
	storage := &failCustomTypeAppendStorage{MemoryStorage: session.NewMemorySessionStorage(), customType: "turn_end_decision"}
	sess := session.NewSession(storage)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.OnTurnEnd = func(ctx OnTurnEndContext) TurnEndDecision {
		return NewTurnEndDecision(StopTurnEnd(), nil)
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError && event.Context == "turn_end_decision" {
			persistenceErrors = append(persistenceErrors, event)
		}
	})

	if err := harness.Prompt(context.Background(), "start"); err != nil {
		t.Fatalf("prompt failed: %v", err)
	}
	if len(persistenceErrors) != 1 || persistenceErrors[0].Message != "turn_end_decision append failed: storage_failure" {
		t.Fatalf("turn end persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessRegisterNotificationHookRunsTriggersAndSnapshotsStatus(t *testing.T) {
	hook := newStubNotificationHook()
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage()))
	harness := NewAgentHarness(options)
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerHandlingStart || event.Kind == HarnessEventTriggerHandled {
			events = append(events, event)
		}
	})

	harness.RegisterNotificationHook(hook)
	snapshot := harness.NotificationStatusSnapshot()
	if len(snapshot.Hooks) != 1 || snapshot.Hooks[0].State.Kind != triggers.HookStateConnected || snapshot.Runtime.AcceptedTotal != 0 {
		t.Fatalf("initial notification snapshot mismatch: %#v", snapshot)
	}
	hook.emit(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()})
	hook.emit(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace-2", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()})
	hook.close()
	hook.wait(t)
	waitFor(t, func() bool {
		snapshot := harness.NotificationStatusSnapshot()
		return snapshot.Runtime.AcceptedTotal == 1 && snapshot.Runtime.DedupedTotal == 1 && len(events) == 4 && events[3].TriggerState == triggers.StateDeduped
	})

	snapshot = harness.NotificationStatusSnapshot()
	if snapshot.Runtime.AcceptedTotal != 1 || snapshot.Runtime.DedupedTotal != 1 || len(snapshot.Running) != 0 {
		t.Fatalf("runtime snapshot mismatch: %#v", snapshot)
	}
	if len(events) != 4 || events[0].Kind != HarnessEventTriggerHandlingStart || events[1].TriggerState != triggers.StateAccepted || events[3].TriggerState != triggers.StateDeduped {
		t.Fatalf("trigger events mismatch: %#v", events)
	}
}

func TestAgentHarnessHandleTriggerPersistsAuditAndBeforeTriggerDecision(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTrigger = func(ctx context.Context, before BeforeTriggerContext) BeforeTriggerDecision {
		if before.Trigger.TraceID != "trace" || before.Runtime.AcceptedTotal != 1 {
			t.Fatalf("before trigger context mismatch: %#v", before)
		}
		return DenyBeforeTrigger("policy")
	}
	harness := NewAgentHarness(options)
	var handled []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerHandled {
			handled = append(handled, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadVisibility: triggers.PayloadLocal, PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	if len(handled) != 1 || handled[0].TriggerState != triggers.StatePermissionDenied || handled[0].AuditEntryID == nil || handled[0].EvaluatorDecision["permission"] != "deny" || handled[0].EvaluatorDecision["reason"] != "policy" {
		t.Fatalf("handled event mismatch: %#v", handled)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Type() != session.EntryTypeCustom || entries[0].CustomType != "trigger" {
		t.Fatalf("trigger audit entry mismatch: %#v", entries)
	}
	record := entries[0].Data.(triggers.TriggerRecord)
	if record.State != triggers.StatePermissionDenied || record.PayloadSummary == nil || *record.PayloadSummary != "summary" {
		t.Fatalf("trigger audit record mismatch: %#v", record)
	}
	decision := record.EvaluatorDecision.(map[string]any)
	if decision["outcome"] != "accept" || decision["permission"] != "deny" || decision["reason"] != "policy" {
		t.Fatalf("trigger audit decision mismatch: %#v", decision)
	}
}

func TestAgentHarnessHandleTriggerAuditAppendFailureUsesUpstreamErrorCode(t *testing.T) {
	storage := &failCustomTypeAppendStorage{MemoryStorage: session.NewMemorySessionStorage(), customType: "trigger"}
	sess := session.NewSession(storage)
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess))
	var persistenceErrors []HarnessEvent
	var handled []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError {
			persistenceErrors = append(persistenceErrors, event)
		}
		if event.Kind == HarnessEventTriggerHandled {
			handled = append(handled, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	if len(handled) != 1 || handled[0].AuditEntryID != nil {
		t.Fatalf("handled event mismatch: %#v", handled)
	}
	if len(persistenceErrors) != 1 || persistenceErrors[0].Context != "trigger_audit" || persistenceErrors[0].Message != "trigger audit append failed: storage_failure" {
		t.Fatalf("trigger audit persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerDedupAuditIncludesPreviousTrace(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess))
	now := time.Now()
	trigger := triggers.Trigger{IDempotencyKey: "key", TraceID: "trace-1", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: now}
	if err := harness.HandleTrigger(trigger); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	trigger.TraceID = "trace-2"
	if err := harness.HandleTrigger(trigger); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	triggerAudits := filterEntriesByCustomType(entries, "trigger")
	if len(triggerAudits) != 2 {
		t.Fatalf("expected two trigger audits, got %#v", entries)
	}
	record := triggerAudits[1].Data.(triggers.TriggerRecord)
	decision := record.EvaluatorDecision.(map[string]any)
	if record.State != triggers.StateDeduped || decision["outcome"] != "deduped" || decision["previous_trace_id"] != "trace-1" || decision["replacement_policy"] != triggers.ReplacementDrop {
		t.Fatalf("dedup audit mismatch: record=%#v decision=%#v", record, decision)
	}
}

func TestAgentHarnessBeforeTriggerDoesNotRunOnDedupedPath(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	hookCalls := 0
	options.BeforeTrigger = func(ctx context.Context, before BeforeTriggerContext) BeforeTriggerDecision {
		hookCalls++
		return AllowBeforeTrigger()
	}
	harness := NewAgentHarness(options)
	now := time.Now()
	trigger := triggers.Trigger{IDempotencyKey: "key", TraceID: "trace-1", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: now}
	if err := harness.HandleTrigger(trigger); err != nil {
		t.Fatalf("first trigger failed: %v", err)
	}
	trigger.TraceID = "trace-2"
	trigger.ReceivedAt = now.Add(time.Second)
	if err := harness.HandleTrigger(trigger); err != nil {
		t.Fatalf("dedup trigger failed: %v", err)
	}
	if hookCalls != 1 {
		t.Fatalf("before trigger hook should run only for accepted path, got %d calls", hookCalls)
	}
}

func TestAgentHarnessHandleTriggerCycleSuppressionPersistsCycleSuppressedAudit(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.TriggerRuntime = triggers.RuntimeConfig{DedupWindow: time.Minute, CycleHopLimit: 1}
	actionCalls := 0
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		actionCalls++
		return TriggerAction{Delivery: TriggerDeliveryInjectSummary}
	}
	harness := NewAgentHarness(options)
	now := time.Now()
	first := triggers.Trigger{IDempotencyKey: "key-1", TraceID: "trace-loop", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: now}
	second := first
	second.IDempotencyKey = "key-2"
	second.ReceivedAt = now.Add(time.Second)

	if err := harness.HandleTrigger(first); err != nil {
		t.Fatalf("first trigger failed: %v", err)
	}
	if err := harness.HandleTrigger(second); err != nil {
		t.Fatalf("second trigger failed: %v", err)
	}
	waitFor(t, func() bool { return actionCalls == 1 })
	if actionCalls != 1 {
		t.Fatalf("cycle suppressed trigger should not run action, got %d calls", actionCalls)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	triggerAudits := filterEntriesByCustomType(entries, "trigger")
	if len(triggerAudits) != 2 {
		t.Fatalf("expected two trigger audits, got %#v", entries)
	}
	record := triggerAudits[1].Data.(triggers.TriggerRecord)
	decision := record.EvaluatorDecision.(map[string]any)
	if record.State != triggers.StateCycleSuppressed || decision["outcome"] != "cycle_suppressed" || decision["hop_count"] != uint32(1) {
		t.Fatalf("cycle suppression audit mismatch: record=%#v decision=%#v", record, decision)
	}
}

func TestAgentHarnessBeforeTriggerPromptResolvesAndAuditsAllow(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTrigger = func(ctx context.Context, before BeforeTriggerContext) BeforeTriggerDecision {
		return PromptBeforeTrigger(strings.Repeat("r", 600))
	}
	var promptRequests []TriggerPromptRequest
	options.OnTriggerPrompt = func(ctx context.Context, request TriggerPromptRequest) TriggerPromptDecision {
		promptRequests = append(promptRequests, request)
		return AllowTriggerPrompt()
	}
	harness := NewAgentHarness(options)
	var promptEvents []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerPromptRequest {
			promptEvents = append(promptEvents, event)
		}
	})
	receiverID := "550e8400-e29b-41d4-a716-446655440000"
	senderID := "550e8400-e29b-41d4-a716-446655440001"
	trigger := triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindMCP, SourceLabel: "MCP long", EventLabel: "run_task", PayloadVisibility: triggers.PayloadShared, PayloadSummary: strPtr(strings.Repeat("s", 5000)), Payload: map[string]any{"_meta": map[string]any{"receiver_agent_id": receiverID, "sender_agent_id": senderID, "action_class": "review.pr"}}, Authority: triggers.Authority{PrincipalID: "principal", PrincipalLabel: "Alice", CredentialScope: triggers.ScopeUser, AllowedSourceActions: []string{"read"}}, ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}

	if err := harness.HandleTrigger(trigger); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	if len(promptRequests) != 1 || len(promptEvents) != 1 {
		t.Fatalf("prompt request counts mismatch: requests=%#v events=%#v", promptRequests, promptEvents)
	}
	request := promptRequests[0]
	if request.TriggerPromptID != expectedTriggerPromptID(trigger, &receiverID, senderID, "review.pr") || request.ReceiverAgentID == nil || *request.ReceiverAgentID != receiverID || request.SenderAgentID != senderID || request.ActionClass != "review.pr" || len([]rune(request.Reason)) != 512 || request.TriggerSummary == nil || len(*request.TriggerSummary) > 4096 {
		t.Fatalf("prompt request mismatch: %#v", request)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	promptAudits := filterEntriesByCustomType(entries, "trigger_prompt")
	triggerAudits := filterEntriesByCustomType(entries, "trigger")
	if len(promptAudits) != 1 || len(triggerAudits) != 1 {
		t.Fatalf("prompt audit entries mismatch: %#v", entries)
	}
	promptAudit := promptAudits[0].Data.(map[string]any)
	if promptAudit["decision"] != "allow" || promptAudit["trigger_prompt_id"] != request.TriggerPromptID || promptAudit["reason"] != nil || promptAudit["sender_agent_id"] != senderID || promptAudit["action_class"] != "review.pr" {
		t.Fatalf("prompt audit mismatch: %#v", promptAudit)
	}
	record := triggerAudits[0].Data.(triggers.TriggerRecord)
	decision := record.EvaluatorDecision.(map[string]any)
	if record.State != triggers.StateAccepted || decision["permission"] != "prompt" || decision["prompt_decision"] != "allow" || decision["trigger_prompt_id"] != request.TriggerPromptID {
		t.Fatalf("trigger prompt decision mismatch: record=%#v decision=%#v", record, decision)
	}
}

func TestAgentHarnessBeforeTriggerPromptDefaultsDenyWithoutHook(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTrigger = func(ctx context.Context, before BeforeTriggerContext) BeforeTriggerDecision {
		return PromptBeforeTrigger("need approval")
	}
	harness := NewAgentHarness(options)

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", Authority: triggers.Authority{PrincipalID: "principal"}, ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	promptAudit := entries[0].Data.(map[string]any)
	record := entries[1].Data.(triggers.TriggerRecord)
	decision := record.EvaluatorDecision.(map[string]any)
	if promptAudit["decision"] != "deny" || promptAudit["reason"] == nil || record.State != triggers.StateNeedsApproval || decision["prompt_decision"] != "deny" || decision["decision_reason"] == nil {
		t.Fatalf("default prompt deny mismatch: audit=%#v record=%#v decision=%#v", promptAudit, record, decision)
	}
}

func TestAgentHarnessBeforeTriggerPromptRejectsUntrustedPayloadIdentityFields(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	oversizedPromptReason := "prompt-reason-" + strings.Repeat("x", 700)
	oversizedDenyReason := "deny-reason-" + strings.Repeat("y", 700)
	options.BeforeTrigger = func(ctx context.Context, before BeforeTriggerContext) BeforeTriggerDecision {
		return PromptBeforeTrigger(oversizedPromptReason)
	}
	var promptRequests []TriggerPromptRequest
	options.OnTriggerPrompt = func(ctx context.Context, request TriggerPromptRequest) TriggerPromptDecision {
		promptRequests = append(promptRequests, request)
		return DenyTriggerPrompt(oversizedDenyReason)
	}
	harness := NewAgentHarness(options)
	trigger := triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindMCP, SourceLabel: "mcp", EventLabel: "notification", Payload: map[string]any{"_meta": map[string]any{"receiver_agent_id": "sk-receiver-secret-token", "sender_agent_id": "Bearer sender-secret-token", "action_class": "sk-action-secret-token"}}, Authority: triggers.Authority{PrincipalID: "33333333-3333-4333-8333-333333333333"}, ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}

	if err := harness.HandleTrigger(trigger); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	if len(promptRequests) != 1 {
		t.Fatalf("prompt request missing: %#v", promptRequests)
	}
	request := promptRequests[0]
	if request.ReceiverAgentID != nil || request.SenderAgentID != "33333333-3333-4333-8333-333333333333" || request.ActionClass != "notification" || len([]rune(request.Reason)) > 512 || !strings.HasSuffix(request.Reason, "…") {
		t.Fatalf("prompt request should reject untrusted identity fields: %#v", request)
	}
	requestPayload, err := json.Marshal(request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(requestPayload), "sk-receiver-secret-token") || strings.Contains(string(requestPayload), "Bearer sender-secret-token") || strings.Contains(string(requestPayload), "sk-action-secret-token") {
		t.Fatalf("prompt request leaked raw identity payload: %s", string(requestPayload))
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	promptAudit := filterEntriesByCustomType(entries, "trigger_prompt")[0].Data.(map[string]any)
	if promptAudit["receiver_agent_id"] != nil || promptAudit["sender_agent_id"] != "33333333-3333-4333-8333-333333333333" || promptAudit["action_class"] != "notification" {
		t.Fatalf("prompt audit should reject untrusted identity fields: %#v", promptAudit)
	}
	if reason, _ := promptAudit["reason"].(string); len([]rune(reason)) > 512 || !strings.HasSuffix(reason, "…") {
		t.Fatalf("prompt audit reason should be capped: %#v", promptAudit)
	}
	triggerAudit := filterEntriesByCustomType(entries, "trigger")[0].Data.(triggers.TriggerRecord)
	decision := triggerAudit.EvaluatorDecision.(map[string]any)
	if reason, _ := decision["reason"].(string); len([]rune(reason)) > 512 {
		t.Fatalf("trigger decision reason should be capped: %#v", decision)
	}
	if decisionReason, _ := decision["decision_reason"].(string); len([]rune(decisionReason)) > 512 {
		t.Fatalf("trigger decision deny reason should be capped: %#v", decision)
	}
}

func TestAgentHarnessTriggerPromptAuditAppendFailureUsesUpstreamErrorCode(t *testing.T) {
	storage := &failCustomTypeAppendStorage{MemoryStorage: session.NewMemorySessionStorage(), customType: "trigger_prompt"}
	sess := session.NewSession(storage)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTrigger = func(ctx context.Context, before BeforeTriggerContext) BeforeTriggerDecision {
		return PromptBeforeTrigger("need approval")
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError {
			persistenceErrors = append(persistenceErrors, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", Authority: triggers.Authority{PrincipalID: "principal"}, ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	if len(persistenceErrors) != 1 || persistenceErrors[0].Context != "trigger_prompt" || persistenceErrors[0].Message != "trigger prompt audit append failed: storage_failure" {
		t.Fatalf("trigger prompt audit persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerInjectSummaryWritesResult(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		if ctx.Trigger.TraceID != "trace" || ctx.Runtime.AcceptedTotal != 1 {
			t.Fatalf("action context mismatch: %#v", ctx)
		}
		return TriggerAction{Delivery: TriggerDeliveryInjectSummary}
	}
	harness := NewAgentHarness(options)
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerExecutionStarted || event.Kind == HarnessEventTriggerCompleted {
			events = append(events, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(events) == 2 })
	if len(events) != 2 || events[0].Kind != HarnessEventTriggerExecutionStarted || events[0].PromptPreview != "summary" || events[1].Kind != HarnessEventTriggerCompleted || events[1].Summary != "summary" || events[1].CostUSD == nil || *events[1].CostUSD != 0 {
		t.Fatalf("inject summary events mismatch: %#v", events)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].CustomType != "trigger_result" {
		t.Fatalf("trigger result entries mismatch: %#v", entries)
	}
	result := entries[1].Data.(map[string]any)
	if result["delivery"] != "inject_summary" || result["success"] != true || result["summary"] != "summary" || result["message_count"] != 0 || result["cost_usd"] != float64(0) {
		t.Fatalf("inject summary result mismatch: %#v", result)
	}
}

func TestAgentHarnessHandleTriggerInjectSummaryWithoutPayloadPromotesNothing(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow}}
	}
	harness := NewAgentHarness(options)
	var promoted []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerPromoted || event.Kind == HarnessEventPromotionPending {
			promoted = append(promoted, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool {
		entries, err := sess.Entries()
		if err != nil {
			return false
		}
		return len(filterEntriesByCustomType(entries, "trigger_result")) == 1
	})
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if messages := filterEntriesByType(entries, session.EntryTypeMessage); len(messages) != 0 {
		t.Fatalf("nil payload summary should not insert promotion message: %#v", messages)
	}
	if promotions := filterEntriesByCustomType(entries, "trigger_promotion"); len(promotions) != 0 {
		t.Fatalf("nil payload summary should not write promotion audit: %#v", promotions)
	}
	results := filterEntriesByCustomType(entries, "trigger_result")
	if len(results) != 1 {
		t.Fatalf("inject summary result missing: %#v", entries)
	}
	result := results[0].Data.(map[string]any)
	if result["delivery"] != "inject_summary" || result["summary"] != nil || result["success"] != true {
		t.Fatalf("inject summary result mismatch: %#v", result)
	}
	if len(promoted) != 0 {
		t.Fatalf("nil payload summary should not emit promotion events: %#v", promoted)
	}
}

func TestAgentHarnessHandleTriggerInjectSummaryResultAppendFailureEmitsUpstreamPersistenceError(t *testing.T) {
	storage := &failCustomTypeAppendStorage{MemoryStorage: session.NewMemorySessionStorage(), customType: "trigger_result"}
	sess := session.NewSession(storage)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Delivery: TriggerDeliveryInjectSummary}
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError {
			persistenceErrors = append(persistenceErrors, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(persistenceErrors) == 1 })
	if len(persistenceErrors) != 1 || persistenceErrors[0].Context != "trigger_result" || persistenceErrors[0].Message != "trigger_result (inject) append failed: storage_failure" {
		t.Fatalf("inject summary persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerInjectAndRunAppendsUserAndRequestsRun(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "please inspect", Delivery: TriggerDeliveryInjectAndRun}
	}
	harness := NewAgentHarness(options)
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerExecutionStarted || event.Kind == HarnessEventTriggerCompleted || event.Kind == HarnessEventTriggerRequestsMainRun {
			events = append(events, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool {
		entries, err := sess.Entries()
		if err != nil {
			return false
		}
		return len(entries) == 3
	})
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[1].Type() != session.EntryTypeMessage || entries[2].CustomType != "trigger_result" {
		t.Fatalf("inject and run entries mismatch: %#v", entries)
	}
	injected := entries[1].Message
	if injected == nil || injected.LLM == nil || injected.LLM.Content[0].Text != "[Trigger trace] please inspect" {
		t.Fatalf("injected message mismatch: %#v", injected)
	}
	if state := harness.Agent().State(); len(state.Messages) != 1 || state.Messages[0].LLM.Content[0].Text != "[Trigger trace] please inspect" {
		t.Fatalf("agent state injection mismatch: %#v", state.Messages)
	}
	result := entries[2].Data.(map[string]any)
	if result["delivery"] != "inject_and_run" || result["prefix_injected"] != true || result["run_dispatch"] != "main_run_request" || result["summary"] != "[Trigger trace] please inspect" {
		t.Fatalf("inject and run result mismatch: %#v", result)
	}
	if len(events) != 3 || events[2].Kind != HarnessEventTriggerRequestsMainRun || events[2].TraceID != "trace" {
		t.Fatalf("inject and run events mismatch: %#v", events)
	}
}

func TestAgentHarnessScheduledCronPollInjectsAndRequestsMainRun(t *testing.T) {
	registry := triggers.NewScheduledCronRegistry()
	if err := registry.LoadFromPath(filepath.Join(t.TempDir(), "cron.toml")); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJob("* * * * *", "inspect scheduled loop")
	if err != nil {
		t.Fatal(err)
	}
	adapter := triggers.NewScheduledCronAdapter(registry)
	now := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	if got := adapter.Poll(now); len(got) != 0 {
		t.Fatalf("first poll should prime scheduled cron adapter, got %#v", got)
	}
	fired := adapter.Poll(now.Add(time.Minute))
	if len(fired) != 1 || fired[0].EventLabel != job.ID || fired[0].TraceID == "" {
		t.Fatalf("scheduled cron did not produce one trigger: %#v", fired)
	}

	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerActionFromCronAction(triggers.CronTriggerAction(registry, ctx.Trigger.Payload.(triggers.ScheduledCronPayload)))
	}
	harness := NewAgentHarness(options)
	var requested []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerRequestsMainRun {
			requested = append(requested, event)
		}
	})

	if err := harness.HandleTrigger(fired[0]); err != nil {
		t.Fatalf("handle scheduled cron trigger failed: %v", err)
	}
	waitForMessageEntries(t, sess, 1)
	waitForCustomEntries(t, sess, "trigger_result", 1)
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	messages := filterEntriesByType(entries, session.EntryTypeMessage)
	if len(messages) != 1 || messages[0].Message == nil || messages[0].Message.LLM == nil || messages[0].Message.LLM.Content[0].Text != "[Trigger "+fired[0].TraceID+"] inspect scheduled loop" {
		t.Fatalf("scheduled cron injected message mismatch: %#v", messages)
	}
	results := filterEntriesByCustomType(entries, "trigger_result")
	if len(results) != 1 {
		t.Fatalf("trigger result missing: %#v", entries)
	}
	result := results[0].Data.(map[string]any)
	if result["delivery"] != "inject_and_run" || result["run_dispatch"] != "main_run_request" || result["summary"] != "[Trigger "+fired[0].TraceID+"] inspect scheduled loop" {
		t.Fatalf("scheduled cron trigger result mismatch: %#v", result)
	}
	if len(requested) != 1 || requested[0].TraceID != fired[0].TraceID {
		t.Fatalf("scheduled cron should request main run once: %#v", requested)
	}
}

func TestAgentHarnessHandleTriggerInjectAndRunWhileStreamingEnqueuesFollowUpNoMainRunEvent(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	parentStarted := make(chan struct{})
	releaseParent := make(chan struct{})
	actionEntered := make(chan struct{})
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		close(actionEntered)
		return TriggerAction{Prompt: "react to the event", Delivery: TriggerDeliveryInjectAndRun}
	}
	streamCalls := 0
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		if streamCalls == 1 {
			close(parentStarted)
			<-releaseParent
		}
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "resp"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerCompleted || event.Kind == HarnessEventTriggerRequestsMainRun {
			events = append(events, event)
		}
	})
	parentDone := make(chan error, 1)
	go func() {
		parentDone <- harness.Prompt(context.Background(), "kick off parent")
	}()
	select {
	case <-parentStarted:
	case <-time.After(time.Second):
		t.Fatal("parent stream did not start")
	}
	if !harness.Agent().IsStreaming() {
		t.Fatalf("parent must be streaming before trigger")
	}

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	select {
	case <-actionEntered:
	case <-time.After(time.Second):
		t.Fatal("trigger action did not run while parent was streaming")
	}
	waitForCustomEntries(t, sess, "trigger_result", 1)
	close(releaseParent)
	select {
	case err := <-parentDone:
		if err != nil {
			t.Fatalf("parent prompt failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("parent prompt did not finish")
	}
	for _, event := range events {
		if event.Kind == HarnessEventTriggerRequestsMainRun {
			t.Fatalf("streaming parent should not request main run: %#v", events)
		}
	}
	waitForCustomEntries(t, sess, "trigger_result", 1)
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	results := filterEntriesByCustomType(entries, "trigger_result")
	if len(results) != 1 {
		t.Fatalf("trigger result missing: %#v", entries)
	}
	result := results[0].Data.(map[string]any)
	if result["delivery"] != "inject_and_run" || result["run_dispatch"] != "follow_up" {
		t.Fatalf("inject and run dispatch mismatch: %#v", result)
	}
	messageEntries := filterEntriesByType(entries, session.EntryTypeMessage)
	foundFollowUp := false
	for _, entry := range messageEntries {
		if entry.Message != nil && entry.Message.LLM != nil && len(entry.Message.LLM.Content) > 0 && entry.Message.LLM.Content[0].Text == "[Trigger trace] react to the event" {
			foundFollowUp = true
		}
	}
	if !foundFollowUp {
		t.Fatalf("follow-up message should be persisted by parent drain: %#v", messageEntries)
	}
}

func TestAgentHarnessHandleTriggerInjectAndRunResultAppendFailureEmitsUpstreamPersistenceError(t *testing.T) {
	storage := &failCustomTypeAppendStorage{MemoryStorage: session.NewMemorySessionStorage(), customType: "trigger_result"}
	sess := session.NewSession(storage)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "please inspect", Delivery: TriggerDeliveryInjectAndRun}
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError {
			persistenceErrors = append(persistenceErrors, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(persistenceErrors) == 1 })
	if len(persistenceErrors) != 1 || persistenceErrors[0].Context != "trigger_result" || persistenceErrors[0].Message != "trigger_result (inject_and_run) append failed: storage_failure" {
		t.Fatalf("inject and run persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerInjectAndRunMessageAppendFailureEmitsUpstreamPersistenceError(t *testing.T) {
	storage := &failMessageAppendStorage{MemoryStorage: session.NewMemorySessionStorage()}
	sess := session.NewSession(storage)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "please inspect", Delivery: TriggerDeliveryInjectAndRun}
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError && event.Context == "trigger_inject_and_run" {
			persistenceErrors = append(persistenceErrors, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(persistenceErrors) == 1 })
	if len(persistenceErrors) != 1 || persistenceErrors[0].Message != "inject_and_run append failed: storage_failure" {
		t.Fatalf("inject and run message persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerInjectAndRunTruncatesPromptWithUpstreamMarker(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	longPrompt := strings.Repeat("a", 5000)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: longPrompt, Delivery: TriggerDeliveryInjectAndRun}
	}
	harness := NewAgentHarness(options)

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool {
		entries, err := sess.Entries()
		if err != nil {
			return false
		}
		return len(entries) == 3
	})
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[1].Type() != session.EntryTypeMessage || entries[2].CustomType != "trigger_result" {
		t.Fatalf("inject and run entries mismatch: %#v", entries)
	}
	body := entries[1].Message.LLM.Content[0].Text
	if len(body) != 4112 || !strings.HasPrefix(body, "[Trigger trace] ") || !strings.HasSuffix(body, "…[truncated]") {
		t.Fatalf("truncated inject body mismatch: len=%d prefix=%v suffix=%v", len(body), strings.HasPrefix(body, "[Trigger trace] "), strings.HasSuffix(body, "…[truncated]"))
	}
	result := entries[2].Data.(map[string]any)
	if result["summary"] != body || result["prefix_injected"] != true || result["delivery"] != "inject_and_run" {
		t.Fatalf("inject and run result mismatch: %#v", result)
	}
}

func TestAgentHarnessHandleTriggerSubAgentRunsAndWritesResult(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	dynamicRegistry := triggers.NewDynamicRegistry()
	dynamicRule, err := dynamicRegistry.AddRule("$HOME contains helloworld", "print $HOME/helloworld")
	if err != nil {
		t.Fatal(err)
	}
	longSummary := dynamicRule.ID + " " + strings.Repeat("你", 1366)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.SystemPrompt = "parent system"
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent}
	}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		if model.ID != "test-model" || len(messages) != 2 || messages[0].Role != ai.RoleSystem || messages[0].Content[0].Text != "parent system" || messages[1].Content[0].Text != "investigate" {
			t.Fatalf("sub-agent stream args mismatch: model=%#v messages=%#v", model, messages)
		}
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: longSummary})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	harness.SubscribeHarness(AdaptTriggerHarnessListener(triggers.FireOnceHarnessListener(dynamicRegistry)))
	var events []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerExecutionStarted || event.Kind == HarnessEventTriggerCompleted || event.Kind == HarnessEventTriggerFailed {
			events = append(events, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(events) >= 2 })
	if running := harness.NotificationStatusSnapshot().Running; len(running) != 0 {
		t.Fatalf("sub-agent should clear running state: %#v", running)
	}
	if len(events) != 2 || events[0].Kind != HarnessEventTriggerExecutionStarted || events[0].PromptPreview != "investigate" || events[1].Kind != HarnessEventTriggerCompleted || len(events[1].Summary) > 4096 || !strings.HasSuffix(events[1].Summary, "…[truncated]") || events[1].CostUSD != nil {
		t.Fatalf("sub-agent events mismatch: %#v", events)
	}
	if body := strings.TrimSuffix(events[1].Summary, "…[truncated]"); !utf8.ValidString(events[1].Summary) || !strings.HasPrefix(body, dynamicRule.ID+" ") || strings.Trim(strings.TrimPrefix(body, dynamicRule.ID+" "), "你") != "" {
		t.Fatalf("sub-agent summary should truncate CJK on rune boundary: %q", events[1].Summary)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].CustomType != "trigger_result" {
		t.Fatalf("sub-agent result entries mismatch: %#v", entries)
	}
	result := entries[1].Data.(map[string]any)
	if result["success"] != true || len(result["summary"].(string)) > 4096 || !strings.HasSuffix(result["summary"].(string), "…[truncated]") || !utf8.ValidString(result["summary"].(string)) || result["message_count"] != 2 || result["cost_usd"] != nil || result["details"] != nil {
		t.Fatalf("sub-agent result mismatch: %#v", result)
	}
	rules := dynamicRegistry.List()
	if len(rules) != 1 || rules[0].ID != dynamicRule.ID || rules[0].Enabled || rules[0].FiredAt == nil {
		t.Fatalf("fire-once dynamic rule should be disabled after trigger summary: %#v", rules)
	}
}

func TestAgentHarnessHandleTriggerReturnsBeforeSlowSubAgentCompletes(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	releaseSlow := make(chan struct{})
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: ctx.Trigger.TraceID, Delivery: TriggerDeliverySubAgent}
	}
	streamCalls := 0
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalls++
		if streamCalls == 1 {
			<-releaseSlow
		}
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "done"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	var handled []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerHandled {
			handled = append(handled, event)
		}
	})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key-slow", TraceID: "trace-slow", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()})
	}()
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first trigger failed: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("handle trigger must return before slow sub-agent completes")
	}
	startSecond := time.Now()
	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key-fast", TraceID: "trace-fast", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("second trigger failed: %v", err)
	}
	if elapsed := time.Since(startSecond); elapsed >= 200*time.Millisecond {
		t.Fatalf("second trigger should not block on first sub-agent, took %v", elapsed)
	}
	seenFastHandled := false
	for _, event := range handled {
		if event.TraceID == "trace-fast" && event.TriggerState == triggers.StateAccepted {
			seenFastHandled = true
		}
	}
	if !seenFastHandled {
		t.Fatalf("second trigger should reach handled promptly: %#v", handled)
	}
	close(releaseSlow)
}

func TestAgentHarnessHandleTriggerReturnsBeforeSlowBeforeTriggerAction(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	actionEntered := make(chan struct{})
	releaseAction := make(chan struct{})
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		close(actionEntered)
		<-releaseAction
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent}
	}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "done"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	done := make(chan error, 1)
	go func() {
		done <- harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()})
	}()

	select {
	case <-actionEntered:
	case <-time.After(time.Second):
		t.Fatal("before trigger action did not start")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("handle trigger failed: %v", err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("handle trigger blocked on before trigger action")
	}
	close(releaseAction)
	waitFor(t, func() bool {
		entries, err := sess.Entries()
		if err != nil {
			return false
		}
		return len(filterEntriesByCustomType(entries, "trigger_result")) == 1
	})
}

func TestAgentHarnessHandleTriggerPromotesSummaryNow(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	templateBody := "Result: {{result.summary}} from {{trigger.source_label}}"
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "sub summary"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	var promoted []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerPromoted {
			promoted = append(promoted, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool {
		entries, err := sess.Entries()
		if err != nil {
			return false
		}
		return len(filterEntriesByCustomType(entries, "trigger_promotion")) == 1
	})
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	messageEntries := filterEntriesByType(entries, session.EntryTypeMessage)
	promotionEntries := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(messageEntries) != 1 || len(promotionEntries) != 1 {
		t.Fatalf("promotion entries mismatch: %#v", entries)
	}
	if got := messageEntries[0].Message.LLM.Content[0].Text; got != "[Trigger trace] Result: sub summary from cron" {
		t.Fatalf("promoted message mismatch: %q", got)
	}
	if state := harness.Agent().State(); len(state.Messages) != 1 || state.Messages[0].LLM.Content[0].Text != "[Trigger trace] Result: sub summary from cron" {
		t.Fatalf("promoted agent state mismatch: %#v", state.Messages)
	}
	audit := promotionEntries[0].Data.(map[string]any)
	expectedHash := sha256Hex(templateBody)
	if audit["state"] != "success" || audit["trace_id"] != "trace" || audit["promote_kind"] != "promote_summary_now" || audit["inserted_entry_id"] == nil || audit["redaction_status"] != "clean" || audit["prefix_injected"] != true || audit["template_hash"] != expectedHash || audit["template_name"] != "inline:"+expectedHash[:8] {
		t.Fatalf("promotion audit mismatch: %#v", audit)
	}
	if len(promoted) != 1 || promoted[0].PromoteKind != "promote_summary_now" || promoted[0].InsertedEntryID == "" || promoted[0].RedactionStatus != "clean" || promoted[0].TemplateName == nil || *promoted[0].TemplateName != "inline:"+expectedHash[:8] {
		t.Fatalf("promotion event mismatch: %#v", promoted)
	}
}

func TestAgentHarnessHandleTriggerPromotionMessageAppendFailureKeepsTemplateHash(t *testing.T) {
	storage := &failMessageAppendStorage{MemoryStorage: session.NewMemorySessionStorage()}
	sess := session.NewSession(storage)
	templateBody := "Result: {{result.summary}}"
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError && event.Context == "trigger_promotion" {
			persistenceErrors = append(persistenceErrors, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitForCustomEntries(t, sess, "trigger_promotion", 1)
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if messages := filterEntriesByType(entries, session.EntryTypeMessage); len(messages) != 0 {
		t.Fatalf("failed append should not persist promotion message: %#v", messages)
	}
	promotions := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotions) != 1 {
		t.Fatalf("failed promotion audit missing: %#v", entries)
	}
	expectedHash := sha256Hex(templateBody)
	audit := promotions[0].Data.(map[string]any)
	if audit["state"] != "failed" || audit["template_hash"] != expectedHash || audit["template_name"] != "inline:"+expectedHash[:8] || audit["redaction_status"] != "render_error" {
		t.Fatalf("failed promotion audit mismatch: %#v", audit)
	}
	if len(persistenceErrors) != 1 || persistenceErrors[0].Message != "promotion message append failed: storage_failure" {
		t.Fatalf("persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerPromotionAuditAppendFailureIncludesState(t *testing.T) {
	storage := &failCustomTypeAppendStorage{MemoryStorage: session.NewMemorySessionStorage(), customType: "trigger_promotion"}
	sess := session.NewSession(storage)
	templateBody := "Result: {{result.summary}}"
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	var promoted []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError && event.Context == "trigger_promotion" {
			persistenceErrors = append(persistenceErrors, event)
		}
		if event.Kind == HarnessEventTriggerPromoted {
			promoted = append(promoted, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(promoted) == 1 })
	if len(promoted) != 1 {
		t.Fatalf("promotion event should still emit: %#v", promoted)
	}
	if len(persistenceErrors) != 1 || persistenceErrors[0].Message != "trigger_promotion (success) append failed: storage_failure" {
		t.Fatalf("promotion audit persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerPromotionPendingAuditAppendFailureIncludesState(t *testing.T) {
	storage := &failCustomTypeAppendStorage{MemoryStorage: session.NewMemorySessionStorage(), customType: "trigger_promotion"}
	sess := session.NewSession(storage)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow}, PromoteRequiresApproval: true}
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	var pending []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError && event.Context == "trigger_promotion" {
			persistenceErrors = append(persistenceErrors, event)
		}
		if event.Kind == HarnessEventPromotionPending {
			pending = append(pending, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(pending) == 1 })
	if len(pending) != 1 {
		t.Fatalf("pending event should still emit: %#v", pending)
	}
	if len(persistenceErrors) != 1 || persistenceErrors[0].Message != "trigger_promotion (pending) append failed: storage_failure" {
		t.Fatalf("pending promotion audit persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerPromotionFailedAuditAppendFailureIncludesState(t *testing.T) {
	storage := &failCustomTypeAppendStorage{MemoryStorage: session.NewMemorySessionStorage(), customType: "trigger_promotion"}
	sess := session.NewSession(storage)
	templateBody := "leak {{trigger.payload}}"
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError && event.Context == "trigger_promotion" {
			persistenceErrors = append(persistenceErrors, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), Payload: map[string]any{"secret": "raw"}, ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(persistenceErrors) == 2 })
	if len(persistenceErrors) != 2 || persistenceErrors[0].Message != "trigger_promotion (failed) append failed: storage_failure" || !strings.Contains(persistenceErrors[1].Message, "forbidden template field: trigger.payload") {
		t.Fatalf("failed promotion audit persistence errors mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerPromotionQueuesWhenParentStreaming(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	parentStarted := make(chan struct{})
	releaseParent := make(chan struct{})
	templateBody := "Result: {{result.summary}}"
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	streamCalls := 0
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		if streamCalls == 1 {
			close(parentStarted)
			<-releaseParent
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "parent done"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		}
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "sub summary"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	var promoted []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerPromoted {
			promoted = append(promoted, event)
		}
	})
	parentDone := make(chan error, 1)
	go func() {
		_, err := harness.Agent().Run(context.Background(), []agent.AgentMessage{agent.NewUserMessage("parent")})
		parentDone <- err
	}()
	select {
	case <-parentStarted:
	case <-time.After(time.Second):
		t.Fatal("parent agent did not start streaming")
	}

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool {
		entries, err := sess.Entries()
		if err != nil {
			return false
		}
		return len(filterEntriesByCustomType(entries, "trigger_promotion")) == 1
	})
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if messages := filterEntriesByType(entries, session.EntryTypeMessage); len(messages) != 0 {
		t.Fatalf("queued promotion should not append message directly: %#v", messages)
	}
	promotions := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotions) != 1 {
		t.Fatalf("queued promotion audit missing: %#v", entries)
	}
	promotion := promotions[0].Data.(map[string]any)
	if promotion["state"] != "queued" || promotion["inserted_entry_id"] != nil || promotion["prefix_injected"] != true {
		t.Fatalf("queued promotion audit mismatch: %#v", promotion)
	}
	if len(promoted) != 1 || promoted[0].InsertedEntryID != "" {
		t.Fatalf("queued promotion event mismatch: %#v", promoted)
	}
	close(releaseParent)
	select {
	case err := <-parentDone:
		if err != nil {
			t.Fatalf("parent run failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("parent agent did not finish")
	}
}

func TestAgentHarnessHandleTriggerQueuedPromotionPersistsOnceAfterParentDrainsFollowUp(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	parentStarted := make(chan struct{})
	releaseParent := make(chan struct{})
	templateBody := "Result: {{result.summary}}"
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	parentFirstStarted := false
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		lastText := ""
		if len(messages) > 0 && len(messages[len(messages)-1].Content) > 0 {
			lastText = messages[len(messages)-1].Content[0].Text
		}
		if lastText == "investigate" {
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "sub summary"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		}
		if !parentFirstStarted {
			parentFirstStarted = true
			close(parentStarted)
			<-releaseParent
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "parent done"})
			stream.Close(ai.DoneReasonStop)
			return stream, nil
		}
		if !strings.HasPrefix(lastText, "[Trigger trace] Result: sub summary") {
			return nil, errors.New("queued promotion was not drained into parent context")
		}
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "followup done"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	promptDone := make(chan error, 1)
	go func() {
		promptDone <- harness.Prompt(context.Background(), "parent")
	}()
	select {
	case <-parentStarted:
	case <-time.After(time.Second):
		t.Fatal("parent agent did not start streaming")
	}

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool {
		entries, err := sess.Entries()
		if err != nil {
			return false
		}
		return len(filterEntriesByCustomType(entries, "trigger_promotion")) == 1
	})
	beforeDrain, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if messages := filterEntriesByType(beforeDrain, session.EntryTypeMessage); len(messages) != 1 || messages[0].Message.LLM.Content[0].Text != "parent" {
		t.Fatalf("queued promotion should not append before parent drain: %#v", messages)
	}
	close(releaseParent)
	select {
	case err := <-promptDone:
		if err != nil {
			t.Fatalf("parent prompt failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("parent prompt did not finish")
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	messages := filterEntriesByType(entries, session.EntryTypeMessage)
	if len(messages) != 4 {
		t.Fatalf("expected parent/user, assistant, queued promotion, followup assistant messages once each: %#v", messages)
	}
	if messages[2].Message.LLM.Content[0].Text != "[Trigger trace] Result: sub summary" {
		t.Fatalf("queued promotion message mismatch: %#v", messages[2].Message)
	}
	promotions := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotions) != 1 || promotions[0].Data.(map[string]any)["state"] != "queued" {
		t.Fatalf("queued promotion audit mismatch: %#v", promotions)
	}
}

func TestAgentHarnessHandleTriggerPromotionDefaultTemplateUsesUpstreamTraceIDField(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow}}
	}
	harness := NewAgentHarness(options)
	var promoted []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerPromoted {
			promoted = append(promoted, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitForMessageEntries(t, sess, 1)
	waitFor(t, func() bool { return len(promoted) == 1 })
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	messages := filterEntriesByType(entries, session.EntryTypeMessage)
	if len(messages) != 1 {
		t.Fatalf("default promotion message missing: %#v", entries)
	}
	if len(promoted) != 1 || promoted[0].InsertedEntryID != messages[0].ID() || promoted[0].RedactionStatus != "clean" || promoted[0].TemplateName == nil || *promoted[0].TemplateName != "default" {
		t.Fatalf("default promotion event mismatch: promoted=%#v message=%#v", promoted, messages[0])
	}
	if got := messages[0].Message.LLM.Content[0].Text; got != "[Trigger trace] cron fired due.\nResult: summary" {
		t.Fatalf("default promotion body mismatch: %q", got)
	}
	promotions := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotions) != 1 {
		t.Fatalf("default promotion audit missing: %#v", entries)
	}
	if audit := promotions[0].Data.(map[string]any); audit["state"] != "success" || audit["template_name"] != "default" || audit["inserted_entry_id"] != messages[0].ID() || audit["redaction_status"] != "clean" || audit["prefix_injected"] != false {
		t.Fatalf("default promotion audit mismatch: %#v", audit)
	}
}

func TestAgentHarnessHandleTriggerPromotionInlineTemplateNameUsesHashNotBody(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	templateBody := "Custom RFC4-style prompt: {{trigger.source_label}} -> {{result.summary}}"
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	harness := NewAgentHarness(options)
	var promoted []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerPromoted {
			promoted = append(promoted, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(promoted) == 1 })
	expectedHash := sha256Hex(templateBody)
	if promoted[0].TemplateName == nil || *promoted[0].TemplateName != "inline:"+expectedHash[:8] || strings.Contains(*promoted[0].TemplateName, templateBody) {
		t.Fatalf("inline template event name mismatch: %#v", promoted[0].TemplateName)
	}
	waitForCustomEntries(t, sess, "trigger_promotion", 1)
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	promotions := filterEntriesByCustomType(entries, "trigger_promotion")
	audit := promotions[0].Data.(map[string]any)
	if audit["template_name"] != "inline:"+expectedHash[:8] || audit["template_hash"] != expectedHash || strings.Contains(fmt.Sprintf("%v", audit), templateBody) {
		t.Fatalf("inline template audit identity mismatch: %#v", audit)
	}
}

func TestAgentHarnessHandleTriggerPromotionTruncatesBodyWithUpstreamMarker(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	templateBody := strings.Repeat("a", 5000)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	harness := NewAgentHarness(options)

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitForMessageEntries(t, sess, 1)
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	messages := filterEntriesByType(entries, session.EntryTypeMessage)
	if len(messages) != 1 {
		t.Fatalf("promotion message missing: %#v", entries)
	}
	body := messages[0].Message.LLM.Content[0].Text
	if len(body) != 4096 || !strings.HasSuffix(body, "…[truncated]") || !strings.HasPrefix(body, "[Trigger trace] ") {
		t.Fatalf("truncated body mismatch: len=%d prefix=%v suffix=%v", len(body), strings.HasPrefix(body, "[Trigger trace] "), strings.HasSuffix(body, "…[truncated]"))
	}
	promotions := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotions) != 1 {
		t.Fatalf("promotion audit missing: %#v", entries)
	}
	if audit := promotions[0].Data.(map[string]any); audit["redaction_status"] != "truncated" || audit["prefix_injected"] != true {
		t.Fatalf("truncated promotion audit mismatch: %#v", audit)
	}
}

func TestAgentHarnessHandleTriggerPromotionTemplateRenderErrorFailsClosed(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	templateBody := "leak {{_meta.secret}}"
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError && event.Context == "trigger_promotion" {
			persistenceErrors = append(persistenceErrors, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitForCustomEntries(t, sess, "trigger_promotion", 1)
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if messages := filterEntriesByType(entries, session.EntryTypeMessage); len(messages) != 0 {
		t.Fatalf("render error should not append message: %#v", messages)
	}
	promotionEntries := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotionEntries) != 1 {
		t.Fatalf("failed promotion audit missing: %#v", entries)
	}
	audit := promotionEntries[0].Data.(map[string]any)
	if audit["state"] != "failed" || audit["redaction_status"] != "forbidden_field" || audit["prefix_injected"] != false || audit["template_hash"] != sha256Hex(templateBody) {
		t.Fatalf("failed promotion audit mismatch: %#v", audit)
	}
	if len(persistenceErrors) != 1 || !strings.Contains(persistenceErrors[0].Message, "forbidden template field") {
		t.Fatalf("render error event mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerPromotionTemplatePayloadFieldFailsForbidden(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	templateBody := "leak {{trigger.payload}}"
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow, TemplateBody: &templateBody}}
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError && event.Context == "trigger_promotion" {
			persistenceErrors = append(persistenceErrors, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), Payload: map[string]any{"secret": "raw"}, ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitForCustomEntries(t, sess, "trigger_promotion", 1)
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if messages := filterEntriesByType(entries, session.EntryTypeMessage); len(messages) != 0 {
		t.Fatalf("forbidden payload should not append message: %#v", messages)
	}
	promotions := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotions) != 1 {
		t.Fatalf("failed promotion audit missing: %#v", entries)
	}
	if audit := promotions[0].Data.(map[string]any); audit["state"] != "failed" || audit["redaction_status"] != "forbidden_field" {
		t.Fatalf("failed promotion audit mismatch: %#v", audit)
	}
	if len(persistenceErrors) != 1 || !strings.Contains(persistenceErrors[0].Message, "forbidden template field: trigger.payload") {
		t.Fatalf("render error event mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessHandleTriggerPromotionRequiresApproval(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryNow}, PromoteRequiresApproval: true}
	}
	harness := NewAgentHarness(options)
	var pending []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPromotionPending {
			pending = append(pending, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitForCustomEntries(t, sess, "trigger_promotion", 1)
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if messages := filterEntriesByType(entries, session.EntryTypeMessage); len(messages) != 0 {
		t.Fatalf("approval-required promotion should not append message: %#v", messages)
	}
	promotionEntries := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotionEntries) != 1 {
		t.Fatalf("promotion audit missing: %#v", entries)
	}
	audit := promotionEntries[0].Data.(map[string]any)
	if audit["state"] != "pending" || audit["redaction_status"] != "clean" {
		t.Fatalf("pending promotion audit mismatch: %#v", audit)
	}
	if len(pending) != 1 || pending[0].Preview == nil || !strings.Contains(*pending[0].Preview, "summary") {
		t.Fatalf("pending promotion event mismatch: %#v", pending)
	}
}

func TestAgentHarnessHandleTriggerPromotionDetailsMatchMissingSkipsWithAudit(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliveryInjectSummary, Promote: PromoteAction{Kind: PromoteSummaryWhenResultDetailsMatch, Condition: &PromotionConditionAnyOf{JSONPointer: "/dynamic_trigger/matched_rule_ids", AnyOf: []string{"rule-1"}}}}
	}
	harness := NewAgentHarness(options)
	var promoted []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerPromoted || event.Kind == HarnessEventPromotionPending {
			promoted = append(promoted, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitForCustomEntries(t, sess, "trigger_promotion", 1)
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if messages := filterEntriesByType(entries, session.EntryTypeMessage); len(messages) != 0 {
		t.Fatalf("skipped promotion should not append message: %#v", messages)
	}
	promotionEntries := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotionEntries) != 1 {
		t.Fatalf("promotion skipped audit missing: %#v", entries)
	}
	audit := promotionEntries[0].Data.(map[string]any)
	if audit["state"] != "skipped" || audit["reason"] != PromotionSkipPointerMissing.AuditString() || audit["promote_kind"] != "promote_summary_when_result_details_match" || audit["redaction_status"] != "skipped" {
		t.Fatalf("skipped promotion audit mismatch: %#v", audit)
	}
	if len(promoted) != 0 {
		t.Fatalf("skipped promotion should not emit promotion event: %#v", promoted)
	}
}

func TestAgentHarnessHandleTriggerPromotionDetailsMatchPromotesFromMarkerTool(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent, Promote: PromoteAction{Kind: PromoteSummaryWhenResultDetailsMatch, Condition: &PromotionConditionAnyOf{JSONPointer: "/dynamic_trigger/matched_rule_ids", AnyOf: []string{"rule-1"}}}}
	}
	streamCalls := 0
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalls++
		stream := ai.NewAssistantMessageEventStream()
		if streamCalls == 1 {
			foundMarker := false
			for _, tool := range tools {
				if tool.Name == "mark_dynamic_rule_matched" {
					foundMarker = true
				}
			}
			if !foundMarker {
				return nil, errors.New("marker tool was not exposed to sub-agent")
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventToolCall, ToolCall: &ai.ToolCall{ID: "mark-1", Name: "mark_dynamic_rule_matched", Arguments: map[string]any{"rule_id": "rule-1"}}})
			stream.Close(ai.DoneReasonToolCalls)
			return stream, nil
		}
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "matched summary"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	var completed []HarnessEvent
	var promoted []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerCompleted {
			completed = append(completed, event)
		}
		if event.Kind == HarnessEventTriggerPromoted {
			promoted = append(promoted, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", PayloadSummary: strPtr("summary"), ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(completed) == 1 })
	if len(completed) != 1 {
		t.Fatalf("completed event missing: %#v", completed)
	}
	details := completed[0].Details.(map[string]any)
	matched := details["dynamic_trigger"].(map[string]any)["matched_rule_ids"].([]any)
	if len(matched) != 1 || matched[0] != "rule-1" {
		t.Fatalf("completed details mismatch: %#v", completed[0].Details)
	}
	if len(promoted) != 1 || promoted[0].PromoteKind != string(PromoteSummaryWhenResultDetailsMatch) {
		t.Fatalf("promotion event mismatch: %#v", promoted)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	results := filterEntriesByCustomType(entries, "trigger_result")
	if len(results) != 1 {
		t.Fatalf("trigger result missing: %#v", entries)
	}
	resultDetails := results[0].Data.(map[string]any)["details"].(map[string]any)
	resultMatched := resultDetails["dynamic_trigger"].(map[string]any)["matched_rule_ids"].([]any)
	if len(resultMatched) != 1 || resultMatched[0] != "rule-1" {
		t.Fatalf("trigger result details mismatch: %#v", resultDetails)
	}
	promotions := filterEntriesByCustomType(entries, "trigger_promotion")
	if len(promotions) != 1 {
		t.Fatalf("promotion audit missing: %#v", entries)
	}
	promotion := promotions[0].Data.(map[string]any)
	if promotion["state"] != "success" || promotion["promote_kind"] != string(PromoteSummaryWhenResultDetailsMatch) {
		t.Fatalf("promotion audit mismatch: %#v", promotion)
	}
}

func TestAgentHarnessHandleTriggerSubAgentFailureWritesResult(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	boom := errors.New("boom")
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent}
	}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		return nil, boom
	}
	harness := NewAgentHarness(options)
	var failed []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerFailed {
			failed = append(failed, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(failed) == 1 })
	if len(failed) != 1 || failed[0].Reason == nil || *failed[0].Reason != "boom" {
		t.Fatalf("sub-agent failed event mismatch: %#v", failed)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	result := entries[1].Data.(map[string]any)
	if result["success"] != false || result["reason"] != "boom" || result["message_count"] != 1 {
		t.Fatalf("sub-agent failed result mismatch: %#v", result)
	}
}

func TestAgentHarnessHandleTriggerSubAgentResultAppendFailureEmitsUpstreamPersistenceError(t *testing.T) {
	storage := &failCustomTypeAppendStorage{MemoryStorage: session.NewMemorySessionStorage(), customType: "trigger_result"}
	sess := session.NewSession(storage)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent}
	}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "sub summary"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	var persistenceErrors []HarnessEvent
	var completed []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventPersistenceError {
			persistenceErrors = append(persistenceErrors, event)
		}
		if event.Kind == HarnessEventTriggerCompleted {
			completed = append(completed, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(completed) == 1 })
	if len(completed) != 1 {
		t.Fatalf("terminal event should still emit: %#v", completed)
	}
	if len(persistenceErrors) != 1 || persistenceErrors[0].Context != "trigger_result" || persistenceErrors[0].Message != "trigger_result append failed: storage_failure" {
		t.Fatalf("trigger result persistence error mismatch: %#v", persistenceErrors)
	}
}

func TestAgentHarnessAbortTriggerRemovesRunningSnapshot(t *testing.T) {
	harness := NewAgentHarness(NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage())))
	started := time.Now().UTC()
	harness.trackRunningTrigger(RunningTriggerState{TraceID: "trace", SourceLabel: "cron", EventLabel: "due", StartedAt: started, PromptPreview: "run"}, nil)
	if running := harness.NotificationStatusSnapshot().Running; len(running) != 1 || running[0].TraceID != "trace" {
		t.Fatalf("running trigger snapshot mismatch: %#v", running)
	}

	harness.AbortTrigger("trace")
	if running := harness.NotificationStatusSnapshot().Running; len(running) != 0 {
		t.Fatalf("abort trigger should remove running state: %#v", running)
	}
	harness.trackRunningTrigger(RunningTriggerState{TraceID: "trace-1"}, nil)
	harness.trackRunningTrigger(RunningTriggerState{TraceID: "trace-2"}, nil)
	harness.AbortAllTriggers()
	if running := harness.NotificationStatusSnapshot().Running; len(running) != 0 {
		t.Fatalf("abort all should remove running states: %#v", running)
	}
}

func TestAgentHarnessAbortTriggerCancelsRunningSubAgentAndAuditsFailure(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	streamEntered := make(chan struct{})
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent}
	}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		close(streamEntered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	harness := NewAgentHarness(options)
	var failed []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerFailed {
			failed = append(failed, event)
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()})
	}()

	select {
	case <-streamEntered:
	case <-time.After(time.Second):
		t.Fatal("sub-agent stream did not start")
	}
	harness.AbortTrigger("trace")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("handle trigger failed: %v", err)
		}
	default:
	}
	waitFor(t, func() bool { return len(failed) == 1 })
	if running := harness.NotificationStatusSnapshot().Running; len(running) != 0 {
		t.Fatalf("aborted trigger should leave no running snapshot: %#v", running)
	}
	if len(failed) != 1 || failed[0].Reason == nil || *failed[0].Reason != "aborted" {
		t.Fatalf("aborted trigger failed event mismatch: %#v", failed)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	results := filterEntriesByCustomType(entries, "trigger_result")
	if len(results) != 1 {
		t.Fatalf("trigger result audit missing: %#v", entries)
	}
	result := results[0].Data.(map[string]any)
	if result["success"] != false || result["summary"] != "aborted" || result["reason"] != "aborted" {
		t.Fatalf("aborted trigger result mismatch: %#v", result)
	}
}

func TestAgentHarnessAbortTriggerFromStartedEventCancelsBeforeRunBegins(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	streamCalled := false
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: "investigate", Delivery: TriggerDeliverySubAgent}
	}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		streamCalled = true
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "should not complete"})
		stream.Close(ai.DoneReasonStop)
		return stream, nil
	}
	harness := NewAgentHarness(options)
	var failed []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerExecutionStarted {
			harness.AbortTrigger(event.TraceID)
		}
		if event.Kind == HarnessEventTriggerFailed {
			failed = append(failed, event)
		}
	})

	if err := harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key", TraceID: "trace", SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()}); err != nil {
		t.Fatalf("handle trigger failed: %v", err)
	}
	waitFor(t, func() bool { return len(failed) == 1 })
	if streamCalled {
		t.Fatal("abort from started event should cancel before the sub-agent stream runs")
	}
	if failed[0].Reason == nil || *failed[0].Reason != "aborted" {
		t.Fatalf("started-event abort failed event mismatch: %#v", failed)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	results := filterEntriesByCustomType(entries, "trigger_result")
	if len(results) != 1 {
		t.Fatalf("trigger result audit missing: %#v", entries)
	}
	result := results[0].Data.(map[string]any)
	if result["success"] != false || result["reason"] != "aborted" {
		t.Fatalf("started-event abort result mismatch: %#v", result)
	}
}

func TestAgentHarnessAbortAllTriggersCancelsRunningSubAgentsAndAuditsFailures(t *testing.T) {
	sess := session.NewSession(session.NewMemorySessionStorage())
	started := make(chan string, 2)
	options := NewAgentHarnessOptions(ai.Model{ID: "test-model"}, sess)
	options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerAction{Prompt: ctx.Trigger.TraceID, Delivery: TriggerDeliverySubAgent}
	}
	options.StreamFn = func(ctx context.Context, model ai.Model, messages []ai.Message, tools []ai.Tool, streamOptions ai.SimpleStreamOptions) (*ai.AssistantMessageEventStream, error) {
		if len(messages) > 0 && len(messages[len(messages)-1].Content) > 0 {
			started <- messages[len(messages)-1].Content[0].Text
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	harness := NewAgentHarness(options)
	var failedMu sync.Mutex
	var failed []HarnessEvent
	harness.SubscribeHarness(func(event HarnessEvent) {
		if event.Kind == HarnessEventTriggerFailed {
			failedMu.Lock()
			defer failedMu.Unlock()
			failed = append(failed, event)
		}
	})
	done := make(chan error, 2)
	for _, traceID := range []string{"trace-1", "trace-2"} {
		traceID := traceID
		go func() {
			done <- harness.HandleTrigger(triggers.Trigger{IDempotencyKey: "key-" + traceID, TraceID: traceID, SourceKind: triggers.SourceKindLocal, SourceLabel: "cron", EventLabel: "due", ReplacementPolicy: triggers.ReplacementDrop, ReceivedAt: time.Now()})
		}()
	}
	seenStarted := map[string]bool{}
	for len(seenStarted) < 2 {
		select {
		case traceID := <-started:
			seenStarted[traceID] = true
		case <-time.After(time.Second):
			t.Fatalf("sub-agents did not start: %#v", seenStarted)
		}
	}
	if running := harness.NotificationStatusSnapshot().Running; len(running) != 2 {
		t.Fatalf("running triggers mismatch before abort all: %#v", running)
	}

	harness.AbortAllTriggers()
	for index := 0; index < 2; index++ {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("handle trigger failed: %v", err)
			}
		default:
		}
	}
	waitFor(t, func() bool {
		failedMu.Lock()
		defer failedMu.Unlock()
		return len(failed) == 2
	})
	if running := harness.NotificationStatusSnapshot().Running; len(running) != 0 {
		t.Fatalf("abort all should leave no running snapshot: %#v", running)
	}
	failedMu.Lock()
	failedSnapshot := append([]HarnessEvent(nil), failed...)
	failedMu.Unlock()
	if len(failedSnapshot) != 2 {
		t.Fatalf("failed events mismatch: %#v", failedSnapshot)
	}
	failedByTrace := map[string]string{}
	for _, event := range failedSnapshot {
		if event.Reason != nil {
			failedByTrace[event.TraceID] = *event.Reason
		}
	}
	if failedByTrace["trace-1"] != "aborted" || failedByTrace["trace-2"] != "aborted" {
		t.Fatalf("failed reasons mismatch: %#v", failedByTrace)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	results := filterEntriesByCustomType(entries, "trigger_result")
	if len(results) != 2 {
		t.Fatalf("trigger result audits missing: %#v", entries)
	}
	resultsByTrace := map[string]map[string]any{}
	for _, entry := range results {
		data := entry.Data.(map[string]any)
		resultsByTrace[data["trace_id"].(string)] = data
	}
	for _, traceID := range []string{"trace-1", "trace-2"} {
		result := resultsByTrace[traceID]
		if result["success"] != false || result["summary"] != "aborted" || result["reason"] != "aborted" {
			t.Fatalf("aborted trigger result mismatch for %s: %#v", traceID, result)
		}
	}
}

func TestBeforeTriggerDecisionUpstreamNames(t *testing.T) {
	if AllowBeforeTrigger().Kind != BeforeTriggerAllow {
		t.Fatalf("allow decision mismatch")
	}
	deny := DenyBeforeTrigger("policy")
	if deny.Kind != BeforeTriggerDeny || deny.Reason != "policy" {
		t.Fatalf("deny decision mismatch: %#v", deny)
	}
	prompt := PromptBeforeTrigger("approval")
	if prompt.Kind != BeforeTriggerPrompt || prompt.Reason != "approval" {
		t.Fatalf("prompt decision mismatch: %#v", prompt)
	}
}

func TestTriggerPromptDecisionAuditStrings(t *testing.T) {
	cases := []struct {
		decision TriggerPromptDecision
		audit    string
	}{
		{AllowTriggerPrompt(), "allow"},
		{DenyTriggerPrompt("no"), "deny"},
		{TimeoutTriggerPrompt("slow"), "timeout"},
	}
	for _, tc := range cases {
		if tc.decision.AuditString() != tc.audit {
			t.Fatalf("audit string mismatch: %#v", tc.decision)
		}
		data, err := json.Marshal(tc.decision)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 {
			t.Fatalf("empty json for %#v", tc.decision)
		}
	}
}

func TestTriggerPromptRequestShape(t *testing.T) {
	request := TriggerPromptRequest{TriggerPromptID: "id", TraceID: "trace", SourceLabel: "cron", SenderAgentID: "agent", ActionClass: "write", Reason: "needs approval", Payload: map[string]any{"summary": "hi"}}
	if request.TriggerPromptID != "id" || request.Payload["summary"] != "hi" {
		t.Fatalf("trigger prompt request mismatch: %#v", request)
	}
}

func TestBuildTriggerPromptRequestIDUsesSerdeJSONBindingWithoutHTMLEscape(t *testing.T) {
	trigger := triggers.Trigger{IDempotencyKey: "key<1>", TraceID: "trace&1", SourceKind: triggers.SourceKindMCP, SourceLabel: "mcp <fs>", EventLabel: "changed > file", Authority: triggers.Authority{PrincipalID: "agent", PrincipalLabel: "agent", CredentialScope: triggers.ScopeUser}}
	request := buildTriggerPromptRequest(trigger, "approval")
	if request.TriggerPromptID != expectedTriggerPromptID(trigger, nil, "agent", "changed > file") {
		t.Fatalf("trigger prompt id should use serde_json-style binding without HTML escaping, got %s", request.TriggerPromptID)
	}
}

func TestTriggerActionDefaultForMatchesUpstream(t *testing.T) {
	trigger := triggers.Trigger{SourceLabel: "cron", EventLabel: "job due", ReceivedAt: time.Now()}
	action := DefaultTriggerActionFor(trigger)
	if action.Prompt != "cron fired: job due" || action.Promote.Kind != PromoteNone || action.PromoteRequiresApproval || action.Delivery != TriggerDeliverySubAgent {
		t.Fatalf("default trigger action mismatch: %#v", action)
	}
}

func TestDynamicDirectInjectBeforeTriggerActionHookMatchesUpstream(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	rule, err := registry.AddRule("fallback", "run fallback")
	if err != nil {
		t.Fatal(err)
	}
	summary := "file changed"
	trigger := triggers.Trigger{Source: triggers.Source{Kind: triggers.SourceMCP, ServerName: "filesystem", Method: "changed"}, SourceKind: triggers.SourceKindMCP, SourceLabel: "MCP filesystem", EventLabel: "file changed", PayloadVisibility: triggers.PayloadLocal, PayloadSummary: &summary, IDempotencyKey: "key", ReplacementPolicy: triggers.ReplacementDrop, TraceID: "trace", Authority: triggers.Authority{PrincipalID: "p1", PrincipalLabel: "user", CredentialScope: triggers.ScopeUser}, ReceivedAt: time.Now()}
	hook := DynamicDirectInjectBeforeTriggerActionHook(registry, map[string]bool{"filesystem": true}, map[string]bool{"filesystem": true})

	action := hook(BeforeTriggerActionContext{Trigger: trigger})
	if action.Delivery != TriggerDeliveryInjectAndRun || action.Prompt != "file changed" || action.Promote.Kind != PromoteNone {
		t.Fatalf("inject_and_run action mismatch: %#v", action)
	}

	hook = DynamicDirectInjectBeforeTriggerActionHook(registry, map[string]bool{"filesystem": true}, nil)
	action = hook(BeforeTriggerActionContext{Trigger: trigger})
	if action.Delivery != TriggerDeliveryInjectSummary || action.Promote.Kind != PromoteSummaryNow || action.Promote.TemplateBody == nil || *action.Promote.TemplateBody != "{{trigger.payload_summary}}" {
		t.Fatalf("summary action mismatch: %#v", action)
	}

	trigger.Source.ServerName = "other"
	action = hook(BeforeTriggerActionContext{Trigger: trigger})
	if action.Delivery != TriggerDeliverySubAgent || !strings.Contains(action.Prompt, rule.ID) || action.Promote.Kind != PromoteNone {
		t.Fatalf("fallback action mismatch: %#v", action)
	}
}

func TestDynamicDirectInjectBeforeTriggerActionHookDefaultsWhenNoRulesLikeUpstream(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	trigger := triggers.Trigger{SourceLabel: "MCP filesystem", EventLabel: "file changed", ReceivedAt: time.Now()}
	hook := DynamicDirectInjectBeforeTriggerActionHook(registry, nil, nil)
	action := hook(BeforeTriggerActionContext{Trigger: trigger})
	if action.Delivery != TriggerDeliverySubAgent || action.Promote.Kind != PromoteNone || action.Prompt != "MCP filesystem fired: file changed" {
		t.Fatalf("default action mismatch: %#v", action)
	}
}

func TestDynamicDirectInjectBeforeTriggerActionHookMapsPromoteSubstringsLikeUpstream(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	promoted, err := registry.AddRuleWithFlags("important", "tell chat", true, true)
	if err != nil {
		t.Fatal(err)
	}
	auditOnly, err := registry.AddRuleWithFlags("audit", "audit only", true, false)
	if err != nil {
		t.Fatal(err)
	}
	hook := DynamicDirectInjectBeforeTriggerActionHook(registry, nil, nil)
	action := hook(BeforeTriggerActionContext{Trigger: triggers.Trigger{SourceLabel: "MCP", EventLabel: "event", ReceivedAt: time.Now()}})
	if action.Promote.Kind != PromoteSummaryWhenSummaryContains || action.Promote.TemplateBody != nil || len(action.Promote.RequiredSubstrings) != 1 || action.Promote.RequiredSubstrings[0] != promoted.ID {
		t.Fatalf("promote condition mismatch: %#v", action.Promote)
	}
	if strings.Contains(strings.Join(action.Promote.RequiredSubstrings, "\n"), auditOnly.ID) {
		t.Fatalf("audit-only rule should not be promoted: %#v", action.Promote)
	}
}

func TestPromotionConditionAnyOfMatchesStructuredDetails(t *testing.T) {
	if PromoteActionNone != PromoteNone {
		t.Fatalf("promote action none alias mismatch")
	}
	if PromotionConditionSkipReasonPointerMissing != PromotionSkipPointerMissing || PromotionConditionSkipReasonValueNotArray != PromotionSkipValueNotArray || PromotionConditionSkipReasonEmptyIntersection != PromotionSkipEmptyIntersection {
		t.Fatalf("promotion skip reason aliases mismatch")
	}
	condition := PromotionConditionAnyOf{JSONPointer: "/dynamic_trigger/matched_rule_ids", AnyOf: []string{"b", "c"}}
	matched, reason := condition.Evaluate(map[string]any{"dynamic_trigger": map[string]any{"matched_rule_ids": []any{"a", "b"}}})
	if reason != "" || len(matched) != 1 || matched[0] != "b" {
		t.Fatalf("promotion condition match mismatch: matched=%#v reason=%q", matched, reason)
	}
	if _, reason := condition.Evaluate(map[string]any{}); reason != PromotionSkipPointerMissing.AuditString() {
		t.Fatalf("missing pointer reason mismatch: %q", reason)
	}
	if _, reason := condition.Evaluate(map[string]any{"dynamic_trigger": map[string]any{"matched_rule_ids": "bad"}}); reason != PromotionSkipValueNotArray.AuditString() {
		t.Fatalf("non-array reason mismatch: %q", reason)
	}
	if _, reason := condition.Evaluate(map[string]any{"dynamic_trigger": map[string]any{"matched_rule_ids": []any{"x"}}}); reason != PromotionSkipEmptyIntersection.AuditString() {
		t.Fatalf("empty intersection reason mismatch: %q", reason)
	}
}

func TestBeforeTriggerActionContextShape(t *testing.T) {
	ctx := BeforeTriggerActionContext{Trigger: triggers.Trigger{TraceID: "trace"}, Runtime: triggers.TriggerRuntimeSnapshot{AcceptedTotal: 2}}
	if ctx.Trigger.TraceID != "trace" || ctx.Runtime.AcceptedTotal != 2 {
		t.Fatalf("before trigger action context mismatch: %#v", ctx)
	}
}

func TestTurnEndActionAuditStringsAndDecisionEnvelope(t *testing.T) {
	if TurnEndActionNoop != TurnEndNoop {
		t.Fatalf("turn end action noop alias mismatch")
	}
	cases := []struct {
		action TurnEndAction
		audit  string
		ok     bool
	}{
		{NoopTurnEnd(), "", false},
		{StopTurnEnd(), "stop", true},
		{PauseTurnEnd("wait"), "pause", true},
		{ContinueTurnEnd("next"), "continue", true},
	}
	for _, tc := range cases {
		got, ok := tc.action.AuditString()
		if got != tc.audit || ok != tc.ok {
			t.Fatalf("audit mismatch for %#v: got %q %v", tc.action, got, ok)
		}
	}
	payload := map[string]any{"score": float64(1)}
	decision := NewTurnEndDecision(ContinueTurnEnd("keep going"), payload)
	if decision.Action.Kind != TurnEndContinue || decision.Payload["score"] != float64(1) {
		t.Fatalf("turn end decision mismatch: %#v", decision)
	}
}

func TestOnTurnEndContextShape(t *testing.T) {
	ctx := OnTurnEndContext{Transcript: []agent.AgentMessage{agent.NewUserMessage("hello")}, ContinuationCount: 3, LastUserPrompt: strPtr("hello")}
	if len(ctx.Transcript) != 1 || ctx.ContinuationCount != 3 || ctx.LastUserPrompt == nil || *ctx.LastUserPrompt != "hello" {
		t.Fatalf("turn end context mismatch: %#v", ctx)
	}
}

func TestNotificationStatusSnapshotShape(t *testing.T) {
	started := time.Now().UTC()
	snapshot := NotificationStatusSnapshot{
		Hooks:   []NotificationHookStatus{PendingNotificationHookStatus()},
		Runtime: triggers.TriggerRuntimeSnapshot{AcceptedTotal: 3},
		Running: []RunningTriggerState{{TraceID: "trace", SourceLabel: "cron", EventLabel: "job", StartedAt: started, PromptPreview: "run"}},
	}
	if len(snapshot.Hooks) != 1 || snapshot.Runtime.AcceptedTotal != 3 || snapshot.Running[0].StartedAt != started {
		t.Fatalf("notification status snapshot mismatch: %#v", snapshot)
	}
}

func TestHarnessEventConstructors(t *testing.T) {
	events := []HarnessEvent{
		SessionStartEvent(2),
		CompactionEvent(true, "summary", 42),
		BranchEvent(strPtr("from"), strPtr("to"), nil),
		TurnEndedEvent("continue", 1, nil, strPtr("next")),
		SkillsReloadedEvent(5),
	}
	if events[0].Kind != HarnessEventSessionStart || events[0].MessagesReplayed != 2 {
		t.Fatalf("session start event mismatch: %#v", events[0])
	}
	if events[1].Kind != HarnessEventCompaction || !events[1].FromHook || events[1].TokensBefore != 42 {
		t.Fatalf("compaction event mismatch: %#v", events[1])
	}
	if events[2].Kind != HarnessEventBranch || events[2].FromEntryID == nil || *events[2].FromEntryID != "from" {
		t.Fatalf("branch event mismatch: %#v", events[2])
	}
	if events[3].Kind != HarnessEventTurnEnded || events[3].Decision != "continue" || events[3].NextPromptPreview == nil {
		t.Fatalf("turn ended event mismatch: %#v", events[3])
	}
	if events[4].Kind != HarnessEventSkillsReloaded || events[4].Total != 5 {
		t.Fatalf("skills reloaded event mismatch: %#v", events[4])
	}
}

func TestHarnessTriggerEventConstructors(t *testing.T) {
	start := TriggerHandlingStartEvent("key", triggers.SourceKindLocal, "cron", "due", "trace")
	if start.Kind != HarnessEventTriggerHandlingStart || start.IDempotencyKey != "key" || start.SourceKind != triggers.SourceKindLocal || start.TraceID != "trace" {
		t.Fatalf("trigger handling start event mismatch: %#v", start)
	}

	handled := TriggerHandledEvent("key", "trace", triggers.StateAccepted, strPtr("audit"), map[string]any{"outcome": "accept"})
	if handled.Kind != HarnessEventTriggerHandled || handled.TriggerState != triggers.StateAccepted || handled.AuditEntryID == nil || handled.EvaluatorDecision == nil {
		t.Fatalf("trigger handled event mismatch: %#v", handled)
	}

	request := TriggerPromptRequest{TriggerPromptID: "prompt", TraceID: "trace"}
	prompt := TriggerPromptRequestEvent(request)
	if prompt.Kind != HarnessEventTriggerPromptRequest || prompt.TriggerPromptRequest.TriggerPromptID != "prompt" {
		t.Fatalf("trigger prompt event mismatch: %#v", prompt)
	}

	persist := PersistenceErrorEvent("trigger_audit", "disk full")
	if persist.Kind != HarnessEventPersistenceError || persist.Context != "trigger_audit" || persist.Message != "disk full" {
		t.Fatalf("persistence error event mismatch: %#v", persist)
	}

	promoted := TriggerPromotedEvent("trace", "promote_summary_now", "entry", nil, "ok")
	if promoted.Kind != HarnessEventTriggerPromoted || promoted.InsertedEntryID != "entry" || promoted.RedactionStatus != "ok" {
		t.Fatalf("trigger promoted event mismatch: %#v", promoted)
	}

	pending := PromotionPendingEvent("trace", "promote_summary_now", nil, strPtr("preview"))
	if pending.Kind != HarnessEventPromotionPending || pending.Preview == nil || *pending.Preview != "preview" {
		t.Fatalf("promotion pending event mismatch: %#v", pending)
	}
}

func strPtr(value string) *string { return &value }

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func waitForCustomEntries(t *testing.T, sess *session.Session, customType string, count int) {
	t.Helper()
	waitFor(t, func() bool {
		entries, err := sess.Entries()
		if err != nil {
			return false
		}
		return len(filterEntriesByCustomType(entries, customType)) == count
	})
}

func waitForMessageEntries(t *testing.T, sess *session.Session, count int) {
	t.Helper()
	waitFor(t, func() bool {
		entries, err := sess.Entries()
		if err != nil {
			return false
		}
		return len(filterEntriesByType(entries, session.EntryTypeMessage)) == count
	})
}

func expectedTriggerPromptID(trigger triggers.Trigger, receiverAgentID *string, senderAgentID string, actionClass string) string {
	binding := []any{"trigger_prompt:v1", trigger.IDempotencyKey, trigger.TraceID, trigger.SourceKind, trigger.SourceLabel, trigger.EventLabel, receiverAgentID, senderAgentID, actionClass}
	data, _ := marshalJSONNoHTMLEscape(binding)
	return sha256Hex(string(data))
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func filterEntriesByCustomType(entries []session.Entry, customType string) []session.Entry {
	filtered := []session.Entry{}
	for _, entry := range entries {
		if entry.Type() == session.EntryTypeCustom && entry.CustomType == customType {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func filterEntriesByType(entries []session.Entry, entryType session.EntryType) []session.Entry {
	filtered := []session.Entry{}
	for _, entry := range entries {
		if entry.Type() == entryType {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

type stubNotificationHook struct {
	triggers chan triggers.Trigger
	done     chan struct{}
	mu       sync.Mutex
	status   triggers.NotificationHookStatus
}

func newStubNotificationHook() *stubNotificationHook {
	return &stubNotificationHook{triggers: make(chan triggers.Trigger, 8), done: make(chan struct{}), status: triggers.NotificationHookStatus{State: triggers.HookState{Kind: triggers.HookStateConnected}, SubscriptionLabels: []string{"stub"}}}
}

func (hook *stubNotificationHook) Label() string { return "stub" }

func (hook *stubNotificationHook) Run(ctx context.Context, sink triggers.TriggerSink) error {
	defer close(hook.done)
	for trigger := range hook.triggers {
		sink <- trigger
	}
	return nil
}

func (hook *stubNotificationHook) Status() triggers.NotificationHookStatus {
	hook.mu.Lock()
	defer hook.mu.Unlock()
	return hook.status
}

func (hook *stubNotificationHook) emit(trigger triggers.Trigger) {
	hook.triggers <- trigger
}

func (hook *stubNotificationHook) close() {
	close(hook.triggers)
}

func (hook *stubNotificationHook) wait(t *testing.T) {
	t.Helper()
	select {
	case <-hook.done:
	case <-time.After(time.Second):
		t.Fatal("notification hook did not stop")
	}
}

type stubHarnessTool struct{ name string }

func (tool stubHarnessTool) Name() string { return tool.name }

func (tool stubHarnessTool) Description() string { return "" }

func (tool stubHarnessTool) Execute(context.Context, ai.ToolCall, agent.ToolUpdateFunc) (agent.ToolResult, error) {
	return agent.ToolResult{}, nil
}

type harnessAskingTool struct{}

func (tool harnessAskingTool) Name() string { return "ask" }

func (tool harnessAskingTool) Description() string { return "ask tool" }

func (tool harnessAskingTool) PermissionClassification(map[string]any) agent.PermissionClassification {
	return agent.PermissionAsk
}

func (tool harnessAskingTool) Execute(context.Context, ai.ToolCall, agent.ToolUpdateFunc) (agent.ToolResult, error) {
	return agent.ToolResult{Content: "allowed"}, nil
}

func harnessAssistantWithUsage(text string, usage *ai.Usage) agent.Message {
	message := agent.NewAssistantMessage(text)
	message.LLM.Usage = usage
	return message
}

type failMessageAppendStorage struct {
	*session.MemoryStorage
}

func (storage *failMessageAppendStorage) AppendEntry(entry session.Entry) error {
	if entry.Type() == session.EntryTypeMessage {
		return session.Error{Code: session.ErrorStorageFailure, Message: "message append failed"}
	}
	return storage.MemoryStorage.AppendEntry(entry)
}

type failCustomTypeAppendStorage struct {
	*session.MemoryStorage
	customType string
}

func (storage *failCustomTypeAppendStorage) AppendEntry(entry session.Entry) error {
	if entry.Type() == session.EntryTypeCustom && entry.CustomType == storage.customType {
		return session.Error{Code: session.ErrorStorageFailure, Message: storage.customType + " append failed"}
	}
	return storage.MemoryStorage.AppendEntry(entry)
}
