package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/compaction"
	"github.com/detailyang/pig/cost"
	"github.com/detailyang/pig/mcp"
	"github.com/detailyang/pig/messages"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/skills"
	"github.com/detailyang/pig/templates"
	"github.com/detailyang/pig/triggers"
)

type NotificationHookStatus = triggers.NotificationHookStatus

func PendingNotificationHookStatus() NotificationHookStatus {
	return triggers.PendingNotificationHookStatus()
}

const DefaultTurnContinuationCap uint32 = 25

const DEFAULT_TURN_CONTINUATION_CAP = DefaultTurnContinuationCap

type EvaluatorOutput struct {
	LastAssistantText *string
}

type EvaluatorError = error

var ErrEvaluatorCancelled = errors.New("evaluator cancelled")

var EvaluatorErrorCancelled error = ErrEvaluatorCancelled

type evaluatorRunError struct {
	err error
}

func (err evaluatorRunError) Error() string { return "evaluator agent failed: " + err.err.Error() }

func (err evaluatorRunError) Unwrap() error { return err.err }

func EvaluatorRunError(err error) error {
	if err == nil {
		return nil
	}
	return evaluatorRunError{err: err}
}

func EvaluatorErrorRun(err error) error { return EvaluatorRunError(err) }

func EvaluatorCancelledError() error { return ErrEvaluatorCancelled }

type ReloadSkillsFn func(context.Context) (skills.LoadSkillsOutput, error)

type ReloadSkillsError = error

var ErrReloadSkillsNotConfigured = errors.New("reload_skills_fn was not configured at harness construction")

var ReloadSkillsErrorNotConfigured error = ErrReloadSkillsNotConfigured

func ReloadSkillsNotConfiguredError() error { return ErrReloadSkillsNotConfigured }

type BeforeTriggerContext struct {
	Trigger triggers.Trigger
	Runtime triggers.TriggerRuntimeSnapshot
}

type BeforeTriggerHook func(context.Context, BeforeTriggerContext) BeforeTriggerDecision

type OnTriggerPromptHook func(context.Context, TriggerPromptRequest) TriggerPromptDecision

type AgentHarnessOptions struct {
	SystemPrompt         string
	Model                ai.Model
	ThinkingLevel        ai.ThinkingLevel
	Skills               []skills.Skill
	PromptTemplates      []templates.PromptTemplate
	Tools                []agent.AgentTool
	Session              *session.Session
	StreamFn             agent.StreamFn
	Compaction           compaction.CompactionSettings
	CompactionSummarizer compaction.Summarizer
	BeforeToolCall       agent.BeforeToolCallHook
	AfterToolCall        agent.AfterToolCallHook
	OnControlPlanePrompt agent.OnControlPlanePromptHook
	BudgetCapUSD         *float64
	TriggerRuntime       triggers.TriggerRuntimeConfig
	BeforeTrigger        BeforeTriggerHook
	OnTriggerPrompt      OnTriggerPromptHook
	BeforeTriggerAction  BeforeTriggerActionHook
	ReloadSkillsFn       ReloadSkillsFn
	OnTurnEnd            OnTurnEndHook
	TurnContinuationCap  *uint32
}

func NewAgentHarnessOptions(model ai.Model, session *session.Session) AgentHarnessOptions {
	return AgentHarnessOptions{
		Model:          model,
		ThinkingLevel:  ai.ThinkingOff,
		Session:        session,
		Compaction:     compaction.DEFAULT_COMPACTION_SETTINGS,
		TriggerRuntime: triggers.DefaultTriggerRuntimeConfig(),
	}
}

type AgentHarness struct {
	Options           AgentHarnessOptions
	agent             *agent.Agent
	cost              *cost.CostTracker
	baseSystemPrompt  string
	session           *session.Session
	skills            []skills.Skill
	templates         templates.PromptTemplateRegistry
	compaction        compaction.CompactionSettings
	summarizer        compaction.Summarizer
	sessionStarted    bool
	triggerRuntime    *triggers.TriggerRuntime
	notificationHooks []triggers.DynNotificationHook
	runningTriggers   map[string]runningTriggerHandle
	mu                sync.Mutex
	listener          []HarnessListener
}

type runningTriggerHandle struct {
	state RunningTriggerState
	abort func()
}

func NewAgentHarness(options AgentHarnessOptions) *AgentHarness {
	state := agent.State{SystemPrompt: buildSystemPrompt(options.SystemPrompt, options.Skills), Model: &options.Model, ThinkingLevel: &options.ThinkingLevel, Tools: append([]agent.Tool(nil), options.Tools...)}
	return &AgentHarness{Options: options, agent: agent.New(agent.Options{InitialState: &state, Stream: options.StreamFn, Tools: options.Tools, Config: agent.Config{ConvertToLLM: harnessConvertToLLM, BeforeToolCall: options.BeforeToolCall, AfterToolCall: options.AfterToolCall, OnControlPlanePrompt: options.OnControlPlanePrompt}}), cost: cost.NewCostTracker(), baseSystemPrompt: options.SystemPrompt, session: options.Session, skills: append([]skills.Skill(nil), options.Skills...), templates: templates.NewPromptTemplateRegistry(options.PromptTemplates), compaction: options.Compaction, summarizer: options.CompactionSummarizer, triggerRuntime: triggers.NewTriggerRuntimeWithConfig(options.TriggerRuntime), runningTriggers: map[string]runningTriggerHandle{}}
}

func harnessConvertToLLM(agentMessages []agent.Message) []ai.Message {
	converted := make([]ai.Message, 0, len(agentMessages))
	for _, message := range agentMessages {
		if customMessage, ok := harnessCustomSummaryMessage(message); ok {
			converted = append(converted, customMessage)
			continue
		}
		converted = append(converted, agent.DefaultConvertToLLM([]agent.Message{message})...)
	}
	return converted
}

func harnessCustomSummaryMessage(message agent.Message) (ai.Message, bool) {
	if message.Kind != agent.MessageKindCustom || message.Custom == nil {
		return ai.Message{}, false
	}
	var label string
	switch message.Custom.Role {
	case "compaction_summary":
		label = "Compaction summary"
	case "branch_summary":
		label = "Branch summary"
	default:
		return ai.Message{}, false
	}
	summary, ok := customSummaryPayload(message.Custom.Payload)
	if !ok || summary == "" {
		return ai.Message{}, false
	}
	return ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: label + ":\n" + summary}}}, true
}

func customSummaryPayload(payload any) (string, bool) {
	object, ok := payload.(map[string]any)
	if !ok {
		return "", false
	}
	summary, ok := object["summary"].(string)
	return summary, ok
}

func (harness *AgentHarness) Agent() *agent.Agent {
	if harness == nil {
		return nil
	}
	return harness.agent
}

func (harness *AgentHarness) Cost() cost.CostSnapshot {
	if harness == nil || harness.cost == nil {
		return cost.CostSnapshot{}
	}
	return harness.cost.Snapshot()
}

func (harness *AgentHarness) ResetCost() {
	if harness == nil || harness.cost == nil {
		return
	}
	harness.cost.Reset()
}

func (harness *AgentHarness) SubscribeHarness(listener HarnessListener) func() {
	if harness == nil || listener == nil {
		return func() {}
	}
	harness.mu.Lock()
	harness.listener = append(harness.listener, listener)
	index := len(harness.listener) - 1
	harness.mu.Unlock()

	return func() {
		harness.mu.Lock()
		defer harness.mu.Unlock()
		if index >= 0 && index < len(harness.listener) && harness.listener[index] != nil {
			harness.listener[index] = nil
		}
	}
}

func (harness *AgentHarness) emitHarnessEvent(event HarnessEvent) {
	if harness == nil {
		return
	}
	harness.mu.Lock()
	listeners := append([]HarnessListener(nil), harness.listener...)
	harness.mu.Unlock()
	for _, listener := range listeners {
		if listener != nil {
			func() {
				defer func() { _ = recover() }()
				listener(event)
			}()
		}
	}
}

func (harness *AgentHarness) Session() *session.Session {
	if harness == nil {
		return nil
	}
	return harness.session
}

func (harness *AgentHarness) Skills() []skills.Skill {
	if harness == nil {
		return nil
	}
	harness.mu.Lock()
	defer harness.mu.Unlock()
	return append([]skills.Skill(nil), harness.skills...)
}

func (harness *AgentHarness) Templates() []templates.PromptTemplate {
	if harness == nil {
		return nil
	}
	harness.mu.Lock()
	defer harness.mu.Unlock()
	return append([]templates.PromptTemplate(nil), harness.templates.List()...)
}

func (harness *AgentHarness) SystemPrompt() string {
	if harness == nil || harness.agent == nil {
		return ""
	}
	return harness.agent.State().SystemPrompt
}

func (harness *AgentHarness) ReplaceSkills(newSkills []skills.Skill) {
	if harness == nil {
		return
	}
	harness.mu.Lock()
	harness.skills = append([]skills.Skill(nil), newSkills...)
	prompt := buildSystemPrompt(harness.baseSystemPrompt, harness.skills)
	harness.mu.Unlock()
	harness.setAgentSystemPrompt(prompt)
}

func (harness *AgentHarness) ReplacePromptTemplates(newTemplates []templates.PromptTemplate) {
	if harness == nil {
		return
	}
	harness.mu.Lock()
	harness.templates = templates.NewPromptTemplateRegistry(newTemplates)
	harness.mu.Unlock()
}

func (harness *AgentHarness) ReplaceTools(tools []agent.AgentTool) {
	if harness == nil || harness.agent == nil {
		return
	}
	harness.agent.ReplaceTools(tools)
}

func (harness *AgentHarness) ApplyLoadedMCP(loaded mcp.LoadedMCP, registry *triggers.DynamicRegistry) {
	if harness == nil {
		return
	}
	if len(loaded.Tools) > 0 && harness.agent != nil {
		state := harness.agent.State()
		tools := append([]agent.Tool(nil), state.Tools...)
		tools = append(tools, loaded.Tools...)
		harness.agent.ReplaceTools(tools)
	}
	for _, hook := range triggers.MCPNotificationHooksFromSources(loaded.NotificationSources) {
		harness.RegisterNotificationHook(hook)
	}
	if len(loaded.InjectSummaryServers) == 0 && len(loaded.InjectAndRunServers) == 0 {
		return
	}
	previous := harness.Options.BeforeTriggerAction
	harness.Options.BeforeTriggerAction = func(ctx BeforeTriggerActionContext) TriggerAction {
		if ctx.Trigger.Source.Kind == triggers.SourceMCP {
			server := ctx.Trigger.Source.ServerName
			if loaded.InjectAndRunServers[server] || loaded.InjectSummaryServers[server] {
				return TriggerActionFromCronAction(triggers.DirectInjectDynamicTriggerAction(registry, ctx.Trigger, loaded.InjectSummaryServers, loaded.InjectAndRunServers))
			}
		}
		if previous != nil {
			return previous(ctx)
		}
		if registry != nil {
			return TriggerActionFromCronAction(triggers.DynamicTriggerAction(registry, ctx.Trigger))
		}
		return DefaultTriggerActionFor(ctx.Trigger)
	}
}

func (harness *AgentHarness) SetCompactionSettings(settings compaction.CompactionSettings) {
	if harness == nil {
		return
	}
	harness.mu.Lock()
	harness.compaction = settings
	harness.mu.Unlock()
}

func (harness *AgentHarness) SetCompactionSummarizer(summarizer compaction.Summarizer) {
	if harness == nil {
		return
	}
	harness.mu.Lock()
	harness.summarizer = summarizer
	harness.mu.Unlock()
}

func (harness *AgentHarness) Abort() {
	if harness == nil || harness.agent == nil {
		return
	}
	harness.agent.Abort()
}

func (harness *AgentHarness) NotificationStatusSnapshot() NotificationStatusSnapshot {
	if harness == nil {
		return NotificationStatusSnapshot{}
	}
	harness.mu.Lock()
	hooks := append([]triggers.DynNotificationHook(nil), harness.notificationHooks...)
	running := make([]RunningTriggerState, 0, len(harness.runningTriggers))
	for _, handle := range harness.runningTriggers {
		running = append(running, handle.state)
	}
	runtime := harness.triggerRuntime
	harness.mu.Unlock()
	statuses := make([]NotificationHookStatus, 0, len(hooks))
	for _, hook := range hooks {
		statuses = append(statuses, hook.Status())
	}
	var runtimeSnapshot triggers.TriggerRuntimeSnapshot
	if runtime != nil {
		runtimeSnapshot = runtime.Snapshot()
	}
	return NotificationStatusSnapshot{Hooks: statuses, Runtime: runtimeSnapshot, Running: running}
}

func (harness *AgentHarness) AbortTrigger(traceID string) {
	if harness == nil {
		return
	}
	harness.mu.Lock()
	handle, ok := harness.runningTriggers[traceID]
	delete(harness.runningTriggers, traceID)
	harness.mu.Unlock()
	if ok && handle.abort != nil {
		handle.abort()
	}
}

func (harness *AgentHarness) AbortAllTriggers() {
	if harness == nil {
		return
	}
	harness.mu.Lock()
	handles := make([]runningTriggerHandle, 0, len(harness.runningTriggers))
	for _, handle := range harness.runningTriggers {
		handles = append(handles, handle)
	}
	harness.runningTriggers = map[string]runningTriggerHandle{}
	harness.mu.Unlock()
	for _, handle := range handles {
		if handle.abort != nil {
			handle.abort()
		}
	}
}

func (harness *AgentHarness) RegisterNotificationHook(hook triggers.DynNotificationHook) {
	if harness == nil || hook == nil {
		return
	}
	sink := make(chan triggers.Trigger, 64)
	harness.mu.Lock()
	harness.notificationHooks = append(harness.notificationHooks, hook)
	harness.mu.Unlock()
	go func() {
		defer close(sink)
		_ = hook.Run(context.Background(), sink)
	}()
	go func() {
		for trigger := range sink {
			harness.HandleTrigger(trigger)
		}
	}()
}

func (harness *AgentHarness) HandleTrigger(trigger triggers.Trigger) error {
	if harness == nil {
		return fmt.Errorf("agent harness is nil")
	}
	harness.emitHarnessEvent(TriggerHandlingStartEvent(trigger.IDempotencyKey, trigger.SourceKind, trigger.SourceLabel, trigger.EventLabel, trigger.TraceID))
	if harness.triggerRuntime == nil {
		harness.triggerRuntime = triggers.NewTriggerRuntimeWithConfig(harness.Options.TriggerRuntime)
	}
	outcome := harness.triggerRuntime.Evaluate(trigger)
	state, evaluatorDecision := harness.triggerOutcomeDecision(trigger, outcome)
	record := triggers.RecordReceivedFrom(trigger)
	record.State = state
	record.EvaluatorDecision = evaluatorDecision
	auditEntryID := (*string)(nil)
	if harness.session != nil {
		entryID, err := harness.session.AppendCustom("trigger", record)
		if err != nil {
			harness.emitHarnessEvent(PersistenceErrorEvent("trigger_audit", "trigger audit append failed: "+sessionErrorCodeString(err)))
		} else {
			auditEntryID = &entryID
		}
	}
	harness.emitHarnessEvent(TriggerHandledEvent(trigger.IDempotencyKey, trigger.TraceID, state, auditEntryID, evaluatorDecision))
	if state == triggers.StateAccepted {
		harness.runAcceptedTriggerAction(trigger)
	}
	return nil
}

func (harness *AgentHarness) runAcceptedTriggerAction(trigger triggers.Trigger) {
	runtimeSnapshot := triggers.TriggerRuntimeSnapshot{}
	if harness.triggerRuntime != nil {
		runtimeSnapshot = harness.triggerRuntime.Snapshot()
	}
	go harness.runTriggerAction(trigger, runtimeSnapshot)
}

func (harness *AgentHarness) runTriggerAction(trigger triggers.Trigger, runtimeSnapshot triggers.TriggerRuntimeSnapshot) {
	action := DefaultTriggerActionFor(trigger)
	if harness.Options.BeforeTriggerAction != nil {
		action = harness.Options.BeforeTriggerAction(BeforeTriggerActionContext{Trigger: trigger, Runtime: runtimeSnapshot})
	}
	switch action.Delivery {
	case TriggerDeliveryInjectSummary:
		harness.runInjectSummaryTriggerAction(trigger, action)
	case TriggerDeliveryInjectAndRun:
		harness.runInjectAndRunTriggerAction(trigger, action)
	default:
		harness.runSubAgentTriggerAction(trigger, action)
	}
}

func (harness *AgentHarness) runSubAgentTriggerAction(trigger triggers.Trigger, action TriggerAction) {
	promptPreview := previewForBanner(action.Prompt, 80)
	runningState := RunningTriggerState{TraceID: trigger.TraceID, SourceLabel: trigger.SourceLabel, EventLabel: trigger.EventLabel, StartedAt: time.Now().UTC(), PromptPreview: promptPreview}
	runCtx, cancel := context.WithCancel(context.Background())
	aborted := atomic.Bool{}
	abort := func() {
		aborted.Store(true)
		cancel()
	}
	harness.trackRunningTrigger(runningState, abort)
	defer cancel()
	defer harness.clearRunningTrigger(trigger.TraceID)
	harness.emitHarnessEvent(TriggerExecutionStartedEvent(trigger.TraceID, trigger.SourceLabel, trigger.EventLabel, promptPreview))

	parentState := harness.agent.State()
	detailsBuilder := newTriggerResultDetailsBuilder()
	subTools := append([]agent.Tool(nil), parentState.Tools...)
	subTools = append(subTools, triggerResultMarkerTool{builder: detailsBuilder})
	subState := agent.State{SystemPrompt: parentState.SystemPrompt, Model: parentState.Model, ThinkingLevel: parentState.ThinkingLevel, Tools: subTools}
	subAgent := agent.New(agent.Options{InitialState: &subState, Stream: harness.Options.StreamFn, Tools: subTools, Config: agent.Config{BeforeToolCall: harness.Options.BeforeToolCall, AfterToolCall: harness.Options.AfterToolCall, OnControlPlanePrompt: harness.Options.OnControlPlanePrompt}})
	_, runErr := subAgent.Run(runCtx, []agent.AgentMessage{agent.NewUserMessage(action.Prompt)})
	if aborted.Load() && (runErr == nil || errors.Is(runErr, context.Canceled)) {
		runErr = fmt.Errorf("aborted")
	}
	state := subAgent.State()
	summary := lastAssistantText(state)
	details := detailsBuilder.Snapshot()
	success := runErr == nil
	var reason any
	if runErr != nil {
		reason = runErr.Error()
		if summary == nil && runErr.Error() == "aborted" {
			summary = stringPtr("aborted")
		}
	}
	result := map[string]any{"trace_id": trigger.TraceID, "branch_id": nil, "success": success, "summary": optionalString(summary), "message_count": len(state.Messages), "cost_usd": nil, "reason": reason, "details": details}
	harness.appendTriggerResult("trigger_result", "trigger_result", result)
	if success {
		harness.emitHarnessEvent(TriggerCompletedEvent(trigger.TraceID, summary, nil, details))
	} else {
		reasonText := runErr.Error()
		harness.emitHarnessEvent(TriggerFailedEvent(trigger.TraceID, reasonText, summary, details))
	}
	harness.applyPromotion(trigger, success, summary, len(state.Messages), action.Promote, action.PromoteRequiresApproval, details)
}

func (harness *AgentHarness) runInjectSummaryTriggerAction(trigger triggers.Trigger, action TriggerAction) {
	summary := ""
	if trigger.PayloadSummary != nil {
		summary = *trigger.PayloadSummary
	}
	preview := summary
	if preview == "" {
		preview = "(no summary)"
	}
	preview = previewForBanner(preview, 80)
	harness.emitHarnessEvent(TriggerExecutionStartedEvent(trigger.TraceID, trigger.SourceLabel, trigger.EventLabel, preview))
	result := map[string]any{"trace_id": trigger.TraceID, "branch_id": nil, "success": true, "summary": optionalString(trigger.PayloadSummary), "message_count": 0, "cost_usd": float64(0), "reason": nil, "details": nil, "delivery": "inject_summary"}
	harness.appendTriggerResult("trigger_result", "trigger_result (inject)", result)
	zero := float64(0)
	harness.emitHarnessEvent(TriggerCompletedEvent(trigger.TraceID, trigger.PayloadSummary, &zero, nil))
	if trigger.PayloadSummary == nil {
		return
	}
	harness.applyPromotion(trigger, true, trigger.PayloadSummary, 0, action.Promote, action.PromoteRequiresApproval, nil)
}

func (harness *AgentHarness) runInjectAndRunTriggerAction(trigger triggers.Trigger, action TriggerAction) {
	body, _ := truncatePromotionBody(action.Prompt, 4096)
	body, prefixInjected := ensureTriggerPrefix(body, trigger.TraceID)
	harness.emitHarnessEvent(TriggerExecutionStartedEvent(trigger.TraceID, trigger.SourceLabel, trigger.EventLabel, previewForBanner(body, 80)))
	message := agent.NewUserMessage(body)
	queuedForFollowup := harness.agent != nil && harness.agent.IsStreaming()
	if queuedForFollowup {
		harness.agent.EnqueueFollowUp(message)
	} else {
		if harness.session != nil {
			if _, err := harness.session.AppendMessage(message); err != nil {
				harness.emitHarnessEvent(PersistenceErrorEvent("trigger_inject_and_run", "inject_and_run append failed: "+sessionErrorCodeString(err)))
			}
		}
		if harness.agent != nil {
			state := harness.agent.State()
			state.Messages = append(state.Messages, message)
			harness.agent.ReplaceState(state)
		}
	}
	runDispatch := "main_run_request"
	if queuedForFollowup {
		runDispatch = "follow_up"
	}
	result := map[string]any{"trace_id": trigger.TraceID, "branch_id": nil, "success": true, "summary": body, "message_count": 0, "cost_usd": float64(0), "reason": nil, "details": nil, "delivery": "inject_and_run", "prefix_injected": prefixInjected, "run_dispatch": runDispatch}
	harness.appendTriggerResult("trigger_result", "trigger_result (inject_and_run)", result)
	zero := float64(0)
	harness.emitHarnessEvent(TriggerCompletedEvent(trigger.TraceID, &body, &zero, nil))
	if !queuedForFollowup {
		harness.emitHarnessEvent(TriggerRequestsMainRunEvent(trigger.TraceID))
	}
}

func (harness *AgentHarness) applyPromotion(trigger triggers.Trigger, success bool, summary *string, messageCount int, promote PromoteAction, requiresApproval bool, details any) {
	if promote.Kind == PromoteNone {
		return
	}
	promoteKind := string(promote.Kind)
	if promote.Kind == PromoteSummaryWhenResultDetailsMatch {
		reason := PromotionSkipPointerMissing.AuditString()
		if promote.Condition != nil {
			_, reason = promote.Condition.Evaluate(details)
		}
		if reason != "" {
			harness.appendTriggerPromotion(map[string]any{"state": "skipped", "trace_id": trigger.TraceID, "promote_kind": string(PromoteSummaryWhenResultDetailsMatch), "reason": reason, "template_name": nil, "template_hash": nil, "inserted_entry_id": nil, "rule_id": nil, "redaction_status": "skipped", "dedup_collapsed": false, "prefix_injected": false})
			return
		}
	}
	if promote.Kind == PromoteSummaryWhenSummaryContains {
		summaryText := ""
		if summary != nil {
			summaryText = *summary
		}
		matched := false
		for _, needle := range promote.RequiredSubstrings {
			if strings.Contains(summaryText, needle) {
				matched = true
				break
			}
		}
		if !matched {
			return
		}
		promoteKind = string(PromoteSummaryNow)
	}
	if promote.Kind != PromoteSummaryNow && promote.Kind != PromoteSummaryWhenSummaryContains && promote.Kind != PromoteSummaryWhenResultDetailsMatch {
		return
	}
	templateBody, templateName, templateHash := promotionTemplate(promote.TemplateBody)
	body, renderErr := renderPromotionTemplate(templateBody, trigger, success, summary, messageCount)
	if renderErr != nil {
		redactionStatus := "render_error"
		if strings.HasPrefix(renderErr.Error(), "forbidden template field") {
			redactionStatus = "forbidden_field"
		}
		harness.appendTriggerPromotion(map[string]any{"state": "failed", "trace_id": trigger.TraceID, "promote_kind": promoteKind, "template_name": templateName, "template_hash": templateHash, "inserted_entry_id": nil, "rule_id": nil, "redaction_status": redactionStatus, "dedup_collapsed": false, "prefix_injected": false})
		harness.emitHarnessEvent(PersistenceErrorEvent("trigger_promotion", renderErr.Error()))
		return
	}
	body, prefixInjected := ensureTriggerPrefix(body, trigger.TraceID)
	truncated := false
	body, truncated = truncatePromotionBody(body, 4096)
	redactionStatus := "clean"
	if truncated {
		redactionStatus = "truncated"
	}
	if requiresApproval {
		harness.appendTriggerPromotion(map[string]any{"state": "pending", "trace_id": trigger.TraceID, "promote_kind": promoteKind, "template_name": templateName, "template_hash": templateHash, "inserted_entry_id": nil, "rule_id": nil, "redaction_status": redactionStatus, "dedup_collapsed": false, "prefix_injected": prefixInjected})
		harness.emitHarnessEvent(PromotionPendingEvent(trigger.TraceID, promoteKind, &templateName, &body))
		return
	}
	message := agent.NewUserMessage(body)
	queuedForFollowup := harness.agent != nil && harness.agent.IsStreaming()
	insertedEntryID := ""
	if queuedForFollowup {
		harness.agent.EnqueueFollowUp(message)
	} else if harness.session != nil {
		id, err := harness.session.AppendMessage(message)
		if err != nil {
			harness.emitHarnessEvent(PersistenceErrorEvent("trigger_promotion", "promotion message append failed: "+sessionErrorCodeString(err)))
			harness.appendTriggerPromotion(map[string]any{"state": "failed", "trace_id": trigger.TraceID, "promote_kind": promoteKind, "template_name": templateName, "template_hash": templateHash, "inserted_entry_id": nil, "rule_id": nil, "redaction_status": "render_error", "dedup_collapsed": false, "prefix_injected": prefixInjected})
			return
		}
		insertedEntryID = id
	}
	if harness.agent != nil && !queuedForFollowup {
		state := harness.agent.State()
		state.Messages = append(state.Messages, message)
		harness.agent.ReplaceState(state)
	}
	auditState := "success"
	var auditInsertedEntryID any = insertedEntryID
	if queuedForFollowup {
		auditState = "queued"
		auditInsertedEntryID = nil
	}
	harness.appendTriggerPromotion(map[string]any{"state": auditState, "trace_id": trigger.TraceID, "promote_kind": promoteKind, "template_name": templateName, "template_hash": templateHash, "inserted_entry_id": auditInsertedEntryID, "rule_id": nil, "redaction_status": redactionStatus, "dedup_collapsed": false, "prefix_injected": prefixInjected})
	harness.emitHarnessEvent(TriggerPromotedEvent(trigger.TraceID, promoteKind, insertedEntryID, &templateName, redactionStatus))
}

func promotionTemplate(templateBody *string) (string, string, string) {
	template := "[Trigger {{trace_id}}] {{trigger.source_label}} fired {{trigger.event_label}}.\nResult: {{result.summary}}"
	if templateBody != nil {
		template = *templateBody
	}
	hash := sha256HexString(template)
	name := "default"
	if templateBody != nil {
		name = "inline:" + hash[:8]
	}
	return template, name, hash
}

func renderPromotionTemplate(template string, trigger triggers.Trigger, success bool, summary *string, messageCount int) (string, error) {
	summaryText := ""
	if summary != nil {
		summaryText = *summary
	}
	values := map[string]string{
		"trace_id":                           trigger.TraceID,
		"trigger.source.kind":                triggerSourceKindForTemplate(trigger),
		"trigger.source.server_name":         trigger.Source.ServerName,
		"trigger.source.method":              trigger.Source.Method,
		"trigger.source.subkind":             trigger.Source.Subkind,
		"trigger.source_label":               trigger.SourceLabel,
		"trigger.event_label":                trigger.EventLabel,
		"trigger.payload_summary":            triggerPayloadSummaryForTemplate(trigger),
		"trigger.received_at":                trigger.ReceivedAt.UTC().Format(time.RFC3339),
		"trigger.idempotency_key":            trigger.IDempotencyKey,
		"trigger.authority.principal_id":     trigger.Authority.PrincipalID,
		"trigger.authority.principal_label":  trigger.Authority.PrincipalLabel,
		"trigger.authority.credential_scope": string(trigger.Authority.CredentialScope),
		"result.summary":                     summaryText,
		"result.status":                      promotionResultStatus(success),
		"result.message_count":               fmt.Sprintf("%d", messageCount),
		"result.cost_usd":                    "null",
		"result.branch_id":                   "null",
	}
	pattern := regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)
	errorText := ""
	out := pattern.ReplaceAllStringFunc(template, func(match string) string {
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
		if isForbiddenPromotionTemplateField(name) {
			errorText = "forbidden template field: " + name
			return ""
		}
		value, ok := values[name]
		if !ok {
			errorText = "unknown template field: " + name
			return ""
		}
		return value
	})
	if errorText != "" {
		return "", errors.New(errorText)
	}
	if strings.Contains(out, "{{") {
		return "", errors.New("unknown template field: unclosed `{{` placeholder")
	}
	return out, nil
}

func triggerSourceKindForTemplate(trigger triggers.Trigger) string {
	if trigger.Source.Kind != "" {
		return string(trigger.Source.Kind)
	}
	return string(trigger.SourceKind)
}

func triggerPayloadSummaryForTemplate(trigger triggers.Trigger) string {
	if trigger.PayloadSummary == nil {
		return ""
	}
	return *trigger.PayloadSummary
}

func promotionResultStatus(success bool) string {
	if success {
		return "success"
	}
	return "failed"
}

func isForbiddenPromotionTemplateField(name string) bool {
	return name == "trigger.payload" || name == "trigger.authority.allowed_source_actions" || strings.HasPrefix(name, "_meta")
}

func sha256HexString(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (harness *AgentHarness) appendTriggerPromotion(data map[string]any) {
	if harness.session == nil {
		return
	}
	if _, err := harness.session.AppendCustom("trigger_promotion", data); err != nil {
		state, _ := data["state"].(string)
		message := "trigger_promotion append failed: " + sessionErrorCodeString(err)
		if state != "" {
			message = "trigger_promotion (" + state + ") append failed: " + sessionErrorCodeString(err)
		}
		harness.emitHarnessEvent(PersistenceErrorEvent("trigger_promotion", message))
	}
}

func sessionErrorCodeString(err error) string {
	var sessionErr session.Error
	if errors.As(err, &sessionErr) {
		return string(sessionErr.Code)
	}
	return err.Error()
}

func (harness *AgentHarness) appendTriggerResult(context string, messagePrefix string, result map[string]any) {
	if harness.session == nil {
		return
	}
	if _, err := harness.session.AppendCustom("trigger_result", result); err != nil {
		harness.emitHarnessEvent(PersistenceErrorEvent(context, messagePrefix+" append failed: "+sessionErrorCodeString(err)))
	}
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringPtr(value string) *string { return &value }

func ensureTriggerPrefix(body string, traceID string) (string, bool) {
	prefix := "[Trigger " + traceID + "] "
	if strings.HasPrefix(body, prefix) {
		return body, false
	}
	return prefix + body, true
}

func (harness *AgentHarness) triggerOutcomeDecision(trigger triggers.Trigger, outcome triggers.EvaluationOutcome) (triggers.State, map[string]any) {
	switch outcome.Kind {
	case triggers.OutcomeDeduped:
		return triggers.StateDeduped, map[string]any{"outcome": "deduped", "replacement_policy": outcome.ReplacementPolicy, "previous_trace_id": outcome.PreviousTraceID}
	case triggers.OutcomeCycleSuppressed:
		return triggers.StateCycleSuppressed, map[string]any{"outcome": "cycle_suppressed", "hop_count": outcome.HopCount}
	default:
		decision := AllowBeforeTrigger()
		if harness.Options.BeforeTrigger != nil {
			decision = harness.Options.BeforeTrigger(context.Background(), BeforeTriggerContext{Trigger: trigger, Runtime: harness.triggerRuntime.Snapshot()})
		}
		switch decision.Kind {
		case BeforeTriggerDeny:
			return triggers.StatePermissionDenied, map[string]any{"outcome": "accept", "permission": "deny", "reason": decision.Reason}
		case BeforeTriggerPrompt:
			resolved := harness.resolveTriggerPrompt(trigger, decision.Reason)
			state := triggers.StateNeedsApproval
			if resolved.decision.Kind == TriggerPromptAllow {
				state = triggers.StateAccepted
			}
			return state, map[string]any{"outcome": "accept", "permission": "prompt", "trigger_prompt_id": resolved.request.TriggerPromptID, "prompt_decision": resolved.decision.AuditString(), "reason": resolved.request.Reason, "decision_reason": triggerPromptDecisionReason(resolved.decision)}
		default:
			return triggers.StateAccepted, map[string]any{"outcome": "accept", "permission": "allow"}
		}
	}
}

type resolvedTriggerPrompt struct {
	request  TriggerPromptRequest
	decision TriggerPromptDecision
}

func (harness *AgentHarness) resolveTriggerPrompt(trigger triggers.Trigger, reason string) resolvedTriggerPrompt {
	request := buildTriggerPromptRequest(trigger, reason)
	harness.emitHarnessEvent(TriggerPromptRequestEvent(request))
	decision := TriggerPromptDecision{}
	if harness.Options.OnTriggerPrompt != nil {
		decision = harness.Options.OnTriggerPrompt(context.Background(), request)
	} else {
		decision = DenyTriggerPrompt("trigger prompt required but no on_trigger_prompt hook configured (fail-closed deny — see issue #110 design v0.2)")
	}
	harness.writeTriggerPromptAudit(request, decision)
	return resolvedTriggerPrompt{request: request, decision: decision}
}

func (harness *AgentHarness) writeTriggerPromptAudit(request TriggerPromptRequest, decision TriggerPromptDecision) {
	if harness.session == nil {
		return
	}
	data := map[string]any{"schema_version": 1, "trigger_prompt_id": request.TriggerPromptID, "trace_id": request.TraceID, "source_label": capControlPlaneAuditLabel(request.SourceLabel), "receiver_agent_id": optionalString(request.ReceiverAgentID), "sender_agent_id": request.SenderAgentID, "action_class": request.ActionClass, "decision": decision.AuditString(), "reason": triggerPromptDecisionReason(decision), "at": time.Now().UTC().Format(time.RFC3339)}
	if _, err := harness.session.AppendCustom("trigger_prompt", data); err != nil {
		harness.emitHarnessEvent(PersistenceErrorEvent("trigger_prompt", "trigger prompt audit append failed: "+sessionErrorCodeString(err)))
	}
}

func triggerPromptDecisionReason(decision TriggerPromptDecision) any {
	if decision.Reason == nil {
		return nil
	}
	reason := capTriggerPromptReason(*decision.Reason)
	return reason
}

func buildTriggerPromptRequest(trigger triggers.Trigger, reason string) TriggerPromptRequest {
	receiverAgentID := validatedPayloadAgentID(trigger, "_meta", "receiver_agent_id")
	if receiverAgentID == nil {
		receiverAgentID = validatedPayloadAgentID(trigger, "receiver_agent_id")
	}
	senderAgentID := firstNonNilString(validatedPayloadAgentID(trigger, "_meta", "sender_agent_id"), validatedPayloadAgentID(trigger, "sender_agent_id"), validatedPayloadAgentID(trigger, "agent_id"))
	if senderAgentID == "" {
		senderAgentID = capControlPlaneAuditLabel(trigger.Authority.PrincipalID)
	}
	actionClass := firstNonNilString(validatedPayloadActionClass(trigger, "_meta", "action_class"), validatedPayloadActionClass(trigger, "action_class"))
	if actionClass == "" {
		actionClass = capControlPlaneAuditLabel(trigger.EventLabel)
	}
	var triggerSummary *string
	if trigger.PayloadSummary != nil {
		summary := truncateStringBytesOnRuneBoundary(*trigger.PayloadSummary, 4096)
		triggerSummary = &summary
	}
	payload := map[string]any{"source_kind": trigger.SourceKind, "source_label": capControlPlaneAuditLabel(trigger.SourceLabel), "event_label": capControlPlaneAuditLabel(trigger.EventLabel), "payload_visibility": trigger.PayloadVisibility, "payload_summary": triggerSummary, "authority": map[string]any{"principal_id": trigger.Authority.PrincipalID, "principal_label": capControlPlaneAuditLabel(trigger.Authority.PrincipalLabel), "credential_scope": trigger.Authority.CredentialScope, "allowed_source_actions": append([]string(nil), trigger.Authority.AllowedSourceActions...)}}
	binding := []any{"trigger_prompt:v1", trigger.IDempotencyKey, trigger.TraceID, trigger.SourceKind, trigger.SourceLabel, trigger.EventLabel, receiverAgentID, senderAgentID, actionClass}
	bindingJSON, _ := marshalJSONNoHTMLEscape(binding)
	sum := sha256.Sum256(bindingJSON)
	return TriggerPromptRequest{TriggerPromptID: hex.EncodeToString(sum[:]), TraceID: trigger.TraceID, SourceLabel: capControlPlaneAuditLabel(trigger.SourceLabel), ReceiverAgentID: receiverAgentID, SenderAgentID: senderAgentID, ActionClass: actionClass, TriggerSummary: triggerSummary, Payload: payload, Reason: capTriggerPromptReason(reason)}
}

func firstNonNilString(values ...*string) string {
	for _, value := range values {
		if value != nil {
			return *value
		}
	}
	return ""
}

func validatedPayloadAgentID(trigger triggers.Trigger, path ...string) *string {
	value := triggerPayloadString(trigger.Payload, path...)
	if !isValidUUIDString(value) {
		return nil
	}
	return &value
}

var uuidStringPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isValidUUIDString(value string) bool {
	return uuidStringPattern.MatchString(value)
}

func validatedPayloadActionClass(trigger triggers.Trigger, path ...string) *string {
	value := triggerPayloadString(trigger.Payload, path...)
	if !isValidActionClass(value) {
		return nil
	}
	return &value
}

func triggerPayloadString(payload any, path ...string) string {
	current := payload
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	value, _ := current.(string)
	return value
}

func isValidActionClass(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "sk-") || strings.Contains(lower, "bearer") || strings.Contains(lower, "token") {
		return false
	}
	for index, char := range value {
		if index == 0 && (char < 'a' || char > 'z') {
			return false
		}
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' || char == '.' || char == ':') {
			return false
		}
	}
	return true
}

func capTriggerPromptReason(reason string) string {
	runes := []rune(reason)
	if len(runes) <= 512 {
		return reason
	}
	return string(runes[:511]) + "…"
}

func (harness *AgentHarness) trackRunningTrigger(state RunningTriggerState, abort func()) {
	if harness == nil || state.TraceID == "" {
		return
	}
	harness.mu.Lock()
	if harness.runningTriggers == nil {
		harness.runningTriggers = map[string]runningTriggerHandle{}
	}
	harness.runningTriggers[state.TraceID] = runningTriggerHandle{state: state, abort: abort}
	harness.mu.Unlock()
}

func (harness *AgentHarness) clearRunningTrigger(traceID string) {
	if harness == nil {
		return
	}
	harness.mu.Lock()
	delete(harness.runningTriggers, traceID)
	harness.mu.Unlock()
}

func (harness *AgentHarness) EnqueueSteering(message agent.AgentMessage) {
	if harness == nil || harness.agent == nil {
		return
	}
	harness.agent.EnqueueSteering(message)
}

func (harness *AgentHarness) EnqueueFollowUp(message agent.AgentMessage) {
	if harness == nil || harness.agent == nil {
		return
	}
	harness.agent.EnqueueFollowUp(message)
}

func (harness *AgentHarness) Subscribe(listener agent.AgentListener) func() {
	if harness == nil || harness.agent == nil {
		return func() {}
	}
	return harness.agent.Subscribe(listener)
}

func (harness *AgentHarness) SetModel(model ai.Model) (string, error) {
	if harness == nil || harness.session == nil {
		return "", session.Error{Code: session.ErrorStorageFailure, Message: "session is not configured"}
	}
	entryID, err := harness.session.AppendModelChange(string(model.Provider), model.ID)
	if err != nil {
		return "", err
	}
	if harness.agent != nil {
		harness.agent.SetModel(model)
	}
	return entryID, nil
}

func (harness *AgentHarness) SetThinkingLevel(level ai.ThinkingLevel) (string, error) {
	if harness == nil || harness.session == nil {
		return "", session.Error{Code: session.ErrorStorageFailure, Message: "session is not configured"}
	}
	entryID, err := harness.session.AppendThinkingLevelChange(string(level))
	if err != nil {
		return "", err
	}
	if harness.agent != nil {
		harness.agent.SetThinkingLevel(level)
	}
	return entryID, nil
}

func (harness *AgentHarness) MoveTo(entryID string, summary *session.BranchSummaryInput) (*string, error) {
	if harness == nil || harness.session == nil {
		return nil, session.Error{Code: session.ErrorStorageFailure, Message: "session is not configured"}
	}
	fromEntryID, _ := harness.session.LeafID()
	summaryEntryID, err := harness.session.MoveTo(entryID, summary)
	if err != nil {
		return nil, err
	}
	if _, err := harness.RehydrateFromSession(); err != nil {
		return nil, err
	}
	var toEntryID *string
	if entryID != "" {
		toEntryID = &entryID
	}
	harness.emitHarnessEvent(BranchEvent(fromEntryID, toEntryID, summaryEntryID))
	return summaryEntryID, nil
}

func (harness *AgentHarness) RehydrateFromSession() (session.Context, error) {
	if harness == nil || harness.session == nil {
		return session.Context{}, session.Error{Code: session.ErrorStorageFailure, Message: "session is not configured"}
	}
	ctx, err := harness.session.BuildContext()
	if err != nil {
		return session.Context{}, err
	}
	if harness.agent != nil {
		state := harness.agent.State()
		state.Messages = append([]agent.AgentMessage(nil), ctx.Messages...)
		if ctx.Model != nil {
			if model, ok := ai.GetModel(ai.Provider(ctx.Model.Provider), ctx.Model.ModelID); ok {
				state.Model = &model
			}
		}
		if level, ok := parseThinkingLevel(ctx.ThinkingLevel); ok {
			state.ThinkingLevel = &level
		}
		harness.agent.ReplaceState(state)
	}
	return ctx, nil
}

func (harness *AgentHarness) ForceCompact(ctx context.Context, summarizer compaction.Summarizer) (bool, error) {
	return harness.doCompact(ctx, true, summarizer)
}

func (harness *AgentHarness) doCompact(ctx context.Context, fromHook bool, summarizer compaction.Summarizer) (bool, error) {
	if harness == nil || harness.session == nil || harness.agent == nil {
		return false, fmt.Errorf("agent harness is not configured")
	}
	entries, err := harness.session.Branch(nil)
	if err != nil {
		harness.emitHarnessEvent(CompactionEvent(fromHook, "compaction skipped: session branch read failed: "+err.Error(), 0))
		return false, nil
	}
	harness.mu.Lock()
	settings := harness.compaction
	harness.mu.Unlock()
	if summarizer == nil {
		summarizer = harness.defaultCompactionSummarizer(settings)
	}
	result, err := compaction.Compact(ctx, entries, settings, summarizer)
	if err != nil {
		return false, fmt.Errorf("compaction failed: %w", err)
	}
	if !result.Compacted || result.Summary == "" {
		return false, nil
	}
	firstKeptEntryID := firstKeptEntryID(entries, result.Cut.CutIndex)
	if _, err := harness.session.AppendCompaction(result.Summary, firstKeptEntryID, result.TokensBefore, nil, fromHook); err != nil {
		return false, fmt.Errorf("session append compaction: %w", err)
	}
	harness.emitHarnessEvent(CompactionEvent(fromHook, result.Summary, result.TokensBefore))
	state := harness.agent.State()
	state.Messages = append([]agent.AgentMessage{messages.CompactionSummary(result.Summary)}, result.Messages[1:]...)
	harness.agent.ReplaceState(state)
	return true, nil
}

func (harness *AgentHarness) defaultCompactionSummarizer(settings compaction.CompactionSettings) compaction.Summarizer {
	if harness == nil || harness.agent == nil {
		return nil
	}
	state := harness.agent.State()
	if state.Model == nil {
		return nil
	}
	model := *state.Model
	stream := compaction.AISummarizerStreamFunc(nil)
	if harness.Options.StreamFn != nil {
		stream = func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return streamAgentContext(ctx, harness.Options.StreamFn, model, request, nil, options)
		}
	}
	return compaction.NewAISummarizer(compaction.AISummarizerOptions{Model: model, Settings: settings, Stream: stream})
}

func streamAgentContext(ctx context.Context, stream agent.StreamFn, model ai.Model, request ai.Context, tools []ai.Tool, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
	messages := append([]ai.Message(nil), request.Messages...)
	if request.SystemPrompt != "" {
		messages = append([]ai.Message{{Role: ai.RoleSystem, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: request.SystemPrompt}}}}, messages...)
	}
	out, err := stream(ctx, model, messages, tools, options)
	if err != nil {
		return ai.ErrorStream(err.Error())
	}
	return out
}

func (harness *AgentHarness) runAutoCompaction(ctx context.Context) error {
	if harness == nil || harness.agent == nil {
		return nil
	}
	harness.mu.Lock()
	settings := harness.compaction
	summarizer := harness.summarizer
	harness.mu.Unlock()
	if !settings.Enabled {
		return nil
	}
	state := harness.agent.State()
	if state.Model == nil {
		return nil
	}
	estimate := compaction.EstimateContextTokens(state.Messages)
	if !compaction.ShouldCompact(estimate.Tokens, state.Model.ContextWindow, settings) {
		return nil
	}
	_, err := harness.doCompact(ctx, false, summarizer)
	return err
}

func firstKeptEntryID(entries []session.Entry, cutIndex int) string {
	if cutIndex < 0 {
		cutIndex = 0
	}
	for _, entry := range entries[cutIndex:] {
		if entry.Type() == session.EntryTypeMessage {
			return entry.ID()
		}
	}
	return ""
}

func (harness *AgentHarness) PromptFromTemplate(ctx context.Context, name string, vars map[string]any) error {
	if harness == nil {
		return fmt.Errorf("agent harness is nil")
	}
	harness.mu.Lock()
	template, ok := harness.templates.Get(name)
	harness.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown prompt template: %s", name)
	}
	return harness.Prompt(ctx, templates.Interpolate(template, vars))
}

func (harness *AgentHarness) Prompt(ctx context.Context, text string) error {
	return harness.promptWithMessage(ctx, agent.NewUserMessage(text))
}

func (harness *AgentHarness) PromptWithImages(ctx context.Context, text string, images []ai.ContentBlock) error {
	blocks := make([]ai.ContentBlock, 0, len(images)+1)
	if text != "" {
		blocks = append(blocks, ai.ContentBlock{Type: ai.ContentText, Text: text})
	}
	for _, image := range images {
		image.Type = ai.ContentImage
		blocks = append(blocks, image)
	}
	message := ai.Message{Role: ai.RoleUser, Content: blocks}
	return harness.promptWithMessage(ctx, agent.AgentMessage{Kind: agent.MessageKindLLM, LLM: &message})
}

func (harness *AgentHarness) Continue(ctx context.Context) error {
	if harness == nil || harness.agent == nil {
		return fmt.Errorf("agent harness is not configured")
	}
	harness.ensureSessionStartEmitted()
	if err := harness.checkBudgetCap(); err != nil {
		return err
	}
	if err := harness.runAutoCompaction(ctx); err != nil {
		return err
	}
	return harness.runTurnWithContinuation(ctx, nil, harness.lastUserTextFromState())
}

func (harness *AgentHarness) promptWithMessage(ctx context.Context, message agent.AgentMessage) error {
	if harness == nil || harness.agent == nil {
		return fmt.Errorf("agent harness is not configured")
	}
	harness.ensureSessionStartEmitted()
	if err := harness.checkBudgetCap(); err != nil {
		return err
	}
	if err := harness.runAutoCompaction(ctx); err != nil {
		return err
	}
	lastUserPrompt := extractUserMessageText(message)
	return harness.runTurnWithContinuation(ctx, &message, lastUserPrompt)
}

func (harness *AgentHarness) runTurnWithContinuation(ctx context.Context, firstMessage *agent.AgentMessage, lastUserPrompt *string) error {
	continuationCount := uint32(0)
	pendingMessage := firstMessage
	isFirstIteration := true
	for {
		unsubscribe, listenerError := harness.subscribeSessionPersistence()
		var runErr error
		if isFirstIteration {
			if pendingMessage == nil {
				_, runErr = harness.agent.Continue(ctx)
			} else {
				_, runErr = harness.agent.Run(ctx, []agent.AgentMessage{*pendingMessage})
			}
		} else {
			_, runErr = harness.agent.Run(ctx, []agent.AgentMessage{*pendingMessage})
		}
		unsubscribe()
		if runErr != nil {
			return runErr
		}
		if err := listenerError(); err != nil {
			return err
		}
		isFirstIteration = false

		if harness.Options.OnTurnEnd == nil {
			return nil
		}
		cap := DefaultTurnContinuationCap
		if harness.Options.TurnContinuationCap != nil {
			cap = *harness.Options.TurnContinuationCap
		}
		if continuationCount >= cap {
			reason := fmt.Sprintf("continuation cap reached: %d >= %d", continuationCount, cap)
			harness.recordTurnEndDecision("budget_limited", continuationCount, &reason, nil, nil)
			return nil
		}
		state := harness.agent.State()
		decision := harness.Options.OnTurnEnd(OnTurnEndContext{Transcript: state.Messages, ContinuationCount: continuationCount, LastUserPrompt: lastUserPrompt})
		switch decision.Action.Kind {
		case TurnEndNoop:
			return nil
		case TurnEndStop:
			harness.recordTurnEndDecision("stop", continuationCount, nil, nil, decision.Payload)
			return nil
		case TurnEndPause:
			reason := decision.Action.Reason
			harness.recordTurnEndDecision("pause", continuationCount, &reason, nil, decision.Payload)
			return nil
		case TurnEndContinue:
			continuationCount++
			preview := previewForBanner(decision.Action.Prompt, 80)
			harness.recordTurnEndDecision("continue", continuationCount, nil, &preview, decision.Payload)
			if err := harness.checkBudgetCap(); err != nil {
				return err
			}
			if err := harness.runAutoCompaction(ctx); err != nil {
				return err
			}
			message := agent.NewUserMessage(decision.Action.Prompt)
			pendingMessage = &message
			lastUserPrompt = &decision.Action.Prompt
		default:
			return nil
		}
	}
}

func (harness *AgentHarness) recordTurnEndDecision(decision string, continuationCount uint32, reason *string, nextPromptPreview *string, payload map[string]any) {
	var reasonValue any
	if reason != nil {
		reasonValue = *reason
	}
	var nextPromptPreviewValue any
	if nextPromptPreview != nil {
		nextPromptPreviewValue = *nextPromptPreview
	}
	data := map[string]any{
		"decision":            decision,
		"continuation_count":  continuationCount,
		"reason":              reasonValue,
		"next_prompt_preview": nextPromptPreviewValue,
		"payload":             payload,
	}
	if payload == nil {
		data["payload"] = nil
	}
	if harness.session != nil {
		if _, err := harness.session.AppendCustom("turn_end_decision", data); err != nil {
			harness.emitHarnessEvent(PersistenceErrorEvent("turn_end_decision", "turn_end_decision append failed: "+sessionErrorCodeString(err)))
		}
	}
	harness.emitHarnessEvent(TurnEndedEvent(decision, continuationCount, reason, nextPromptPreview))
}

func (harness *AgentHarness) lastUserTextFromState() *string {
	if harness == nil || harness.agent == nil {
		return nil
	}
	state := harness.agent.State()
	for index := len(state.Messages) - 1; index >= 0; index-- {
		if text := extractUserMessageText(state.Messages[index]); text != nil {
			return text
		}
	}
	return nil
}

func extractUserMessageText(message agent.AgentMessage) *string {
	if message.Kind != agent.MessageKindLLM || message.LLM == nil || message.LLM.Role != ai.RoleUser {
		return nil
	}
	var builder strings.Builder
	for _, block := range message.LLM.Content {
		if block.Type != ai.ContentText || block.Text == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(block.Text)
	}
	text := builder.String()
	if text == "" {
		return nil
	}
	return &text
}

func previewForBanner(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + "…"
}

func (harness *AgentHarness) subscribeSessionPersistence() (func(), func() error) {
	if harness == nil || harness.agent == nil || harness.session == nil {
		return func() {}, func() error { return nil }
	}
	var mu sync.Mutex
	listenerErrors := []error{}
	unsubscribe := harness.agent.Subscribe(func(event agent.AgentEvent) {
		if event.Type == agent.EventTypeControlPlanePromptResolved {
			if err := harness.appendControlPlanePromptAudit(event); err != nil {
				mu.Lock()
				listenerErrors = append(listenerErrors, err)
				mu.Unlock()
			}
			return
		}
		if event.Type != agent.EventTypeMessageEnd || event.Message == nil {
			return
		}
		harness.recordCostFromMessage(*event.Message)
		if _, err := harness.session.AppendMessage(*event.Message); err != nil {
			mu.Lock()
			listenerErrors = append(listenerErrors, fmt.Errorf("session append message: %w", err))
			mu.Unlock()
		}
	})
	return unsubscribe, func() error {
		mu.Lock()
		defer mu.Unlock()
		if len(listenerErrors) == 0 {
			return nil
		}
		return listenerErrors[0]
	}
}

func (harness *AgentHarness) appendControlPlanePromptAudit(event agent.AgentEvent) error {
	if harness == nil || harness.session == nil || event.ControlPlanePrompt == nil {
		return nil
	}
	request := event.ControlPlanePrompt
	data := map[string]any{
		"schema_version": 1,
		"tool_call_id":   request.ToolCallID,
		"tool_name":      request.ToolName,
		"args_hash":      request.ArgsHash,
		"label":          capControlPlaneAuditLabel(request.Label),
		"decision":       event.ControlPlanePromptDecision,
		"reason":         event.ControlPlanePromptReason,
		"at":             time.Now().UTC().Format(time.RFC3339Nano),
	}
	_, err := harness.session.AppendCustom("control_plane_prompt", data)
	return err
}

const controlPlanePromptLabelCapChars = 200

func capControlPlaneAuditLabel(label string) string {
	if len([]rune(label)) <= controlPlanePromptLabelCapChars {
		return label
	}
	runes := []rune(label)
	return string(runes[:controlPlanePromptLabelCapChars-1]) + "…"
}

func (harness *AgentHarness) checkBudgetCap() error {
	if harness == nil || harness.Options.BudgetCapUSD == nil {
		return nil
	}
	total := harness.Cost().TotalCost()
	if total > *harness.Options.BudgetCapUSD {
		return fmt.Errorf("budget cap reached: current cost $%.4f exceeds cap $%.4f", total, *harness.Options.BudgetCapUSD)
	}
	return nil
}

func (harness *AgentHarness) recordCostFromMessage(message agent.AgentMessage) {
	if harness == nil || harness.cost == nil || message.Kind != agent.MessageKindLLM || message.LLM == nil || message.LLM.Role != ai.RoleAssistant || message.LLM.Usage == nil {
		return
	}
	harness.cost.Record(message.LLM.Usage)
}

func (harness *AgentHarness) ensureSessionStartEmitted() {
	if harness == nil {
		return
	}
	harness.mu.Lock()
	if harness.sessionStarted {
		harness.mu.Unlock()
		return
	}
	harness.sessionStarted = true
	count := 0
	if harness.agent != nil {
		count = len(harness.agent.State().Messages)
	}
	harness.mu.Unlock()
	harness.emitHarnessEvent(SessionStartEvent(count))
}

func (harness *AgentHarness) ReloadSkillsFromDisk(ctx context.Context) (skills.LoadSkillsOutput, error) {
	if harness == nil || harness.Options.ReloadSkillsFn == nil {
		return skills.LoadSkillsOutput{}, ErrReloadSkillsNotConfigured
	}
	out, err := harness.Options.ReloadSkillsFn(ctx)
	if err != nil {
		return out, err
	}
	harness.ReplaceSkills(out.Skills)
	harness.emitHarnessEvent(SkillsReloadedEvent(len(out.Skills)))
	return out, nil
}

func (harness *AgentHarness) RunEvaluator(ctx context.Context, userPrompt string) (EvaluatorOutput, error) {
	if harness == nil || harness.agent == nil {
		return EvaluatorOutput{}, EvaluatorRunError(fmt.Errorf("agent harness is not configured"))
	}
	parentState := harness.agent.State()
	model := ai.Model{}
	if parentState.Model != nil {
		model = *parentState.Model
	}
	thinkingLevel := ai.ThinkingLevel("")
	if parentState.ThinkingLevel != nil {
		thinkingLevel = *parentState.ThinkingLevel
	}
	return harness.RunEvaluatorWithConfig(ctx, parentState.SystemPrompt, userPrompt, model, thinkingLevel)
}

func (harness *AgentHarness) RunEvaluatorWithConfig(ctx context.Context, systemPrompt string, userPrompt string, model ai.Model, thinkingLevel ai.ThinkingLevel) (EvaluatorOutput, error) {
	if harness == nil {
		return EvaluatorOutput{}, EvaluatorRunError(fmt.Errorf("agent harness is not configured"))
	}
	if ctx.Err() != nil {
		return EvaluatorOutput{}, EvaluatorCancelledError()
	}
	evaluatorState := agent.State{SystemPrompt: systemPrompt, Model: &model, ThinkingLevel: &thinkingLevel, Tools: []agent.Tool{}}
	evaluator := agent.New(agent.Options{InitialState: &evaluatorState, Stream: harness.Options.StreamFn})
	_, err := evaluator.Run(ctx, []agent.AgentMessage{agent.NewUserMessage(userPrompt)})
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return EvaluatorOutput{}, EvaluatorCancelledError()
		}
		return EvaluatorOutput{}, EvaluatorRunError(err)
	}
	return EvaluatorOutput{LastAssistantText: lastAssistantText(evaluator.State())}, nil
}

func lastAssistantText(state agent.State) *string {
	for index := len(state.Messages) - 1; index >= 0; index-- {
		message := state.Messages[index]
		if message.Kind != agent.MessageKindLLM || message.LLM == nil || message.LLM.Role != ai.RoleAssistant {
			continue
		}
		var builder strings.Builder
		for _, block := range message.LLM.Content {
			if block.Type != ai.ContentText || block.Text == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(block.Text)
		}
		text := builder.String()
		if text == "" {
			return nil
		}
		text, _ = truncatePromotionBody(text, 4096)
		return &text
	}
	return nil
}

func truncateStringBytesOnRuneBoundary(text string, maxBytes int) string {
	if maxBytes < 0 || len(text) <= maxBytes {
		return text
	}
	end := 0
	for index := range text {
		if index > maxBytes {
			break
		}
		end = index
	}
	if end == 0 {
		return ""
	}
	return text[:end]
}

const promotionTruncationMarker = "…[truncated]"

func truncatePromotionBody(text string, maxBytes int) (string, bool) {
	if maxBytes < 0 || len(text) <= maxBytes {
		return text, false
	}
	budget := maxBytes - len(promotionTruncationMarker)
	if budget < 0 {
		return promotionTruncationMarker, true
	}
	end := 0
	for index := range text {
		if index > budget {
			break
		}
		end = index
	}
	truncated := text[:end]
	return truncated + promotionTruncationMarker, true
}

func (harness *AgentHarness) setAgentSystemPrompt(prompt string) {
	if harness.agent == nil {
		return
	}
	harness.agent.SetSystemPrompt(prompt)
}

func buildSystemPrompt(base string, skillList []skills.Skill) string {
	skillsBlock := skills.FormatSkillsForSystemPrompt(skillList)
	if base == "" {
		return skillsBlock
	}
	if skillsBlock == "" {
		return base
	}
	return base + "\n\n" + skillsBlock
}

func parseThinkingLevel(value string) (ai.ThinkingLevel, bool) {
	switch ai.ThinkingLevel(value) {
	case ai.ThinkingOff, ai.ThinkingMinimal, ai.ThinkingLow, ai.ThinkingMedium, ai.ThinkingHigh, ai.ThinkingXHigh:
		return ai.ThinkingLevel(value), true
	default:
		return "", false
	}
}

type BeforeTriggerDecisionKind string

const (
	BeforeTriggerAllow  BeforeTriggerDecisionKind = "allow"
	BeforeTriggerDeny   BeforeTriggerDecisionKind = "deny"
	BeforeTriggerPrompt BeforeTriggerDecisionKind = "prompt"
)

type BeforeTriggerDecision struct {
	Kind   BeforeTriggerDecisionKind `json:"kind"`
	Reason string                    `json:"reason,omitempty"`
}

func AllowBeforeTrigger() BeforeTriggerDecision {
	return BeforeTriggerDecision{Kind: BeforeTriggerAllow}
}

func DenyBeforeTrigger(reason string) BeforeTriggerDecision {
	return BeforeTriggerDecision{Kind: BeforeTriggerDeny, Reason: reason}
}

func PromptBeforeTrigger(reason string) BeforeTriggerDecision {
	return BeforeTriggerDecision{Kind: BeforeTriggerPrompt, Reason: reason}
}

type TriggerPromptRequest struct {
	TriggerPromptID string         `json:"trigger_prompt_id"`
	TraceID         string         `json:"trace_id"`
	SourceLabel     string         `json:"source_label"`
	ReceiverAgentID *string        `json:"receiver_agent_id,omitempty"`
	SenderAgentID   string         `json:"sender_agent_id"`
	ActionClass     string         `json:"action_class"`
	TriggerSummary  *string        `json:"trigger_summary,omitempty"`
	Payload         map[string]any `json:"payload"`
	Reason          string         `json:"reason"`
}

type TriggerPromptDecisionKind string

const (
	TriggerPromptAllow   TriggerPromptDecisionKind = "allow"
	TriggerPromptDeny    TriggerPromptDecisionKind = "deny"
	TriggerPromptTimeout TriggerPromptDecisionKind = "timeout"
)

type TriggerPromptDecision struct {
	Kind   TriggerPromptDecisionKind `json:"kind"`
	Reason *string                   `json:"reason,omitempty"`
}

func AllowTriggerPrompt() TriggerPromptDecision {
	return TriggerPromptDecision{Kind: TriggerPromptAllow}
}

func DenyTriggerPrompt(reason string) TriggerPromptDecision {
	return TriggerPromptDecision{Kind: TriggerPromptDeny, Reason: &reason}
}

func TimeoutTriggerPrompt(reason string) TriggerPromptDecision {
	return TriggerPromptDecision{Kind: TriggerPromptTimeout, Reason: &reason}
}

func (decision TriggerPromptDecision) AuditString() string {
	switch decision.Kind {
	case TriggerPromptDeny:
		return "deny"
	case TriggerPromptTimeout:
		return "timeout"
	default:
		return "allow"
	}
}

func (decision TriggerPromptDecision) AsAuditStr() string {
	return decision.AuditString()
}

type TriggerAction struct {
	Prompt                  string
	Promote                 PromoteAction
	PromoteRequiresApproval bool
	Delivery                TriggerDelivery
}

type TriggerDelivery string

const (
	TriggerDeliverySubAgent      TriggerDelivery = "sub_agent"
	TriggerDeliveryInjectSummary TriggerDelivery = "inject_summary"
	TriggerDeliveryInjectAndRun  TriggerDelivery = "inject_and_run"
)

func DefaultTriggerActionFor(trigger triggers.Trigger) TriggerAction {
	return TriggerAction{Prompt: trigger.SourceLabel + " fired: " + trigger.EventLabel, Promote: PromoteAction{Kind: PromoteNone}, Delivery: TriggerDeliverySubAgent}
}

func (action TriggerAction) DefaultFor(trigger triggers.Trigger) TriggerAction {
	return DefaultTriggerActionFor(trigger)
}

func DynamicDirectInjectBeforeTriggerActionHook(registry *triggers.DynamicRegistry, injectSummaryServers, injectAndRunServers map[string]bool) BeforeTriggerActionHook {
	return func(ctx BeforeTriggerActionContext) TriggerAction {
		return TriggerActionFromCronAction(triggers.DirectInjectDynamicTriggerAction(registry, ctx.Trigger, injectSummaryServers, injectAndRunServers))
	}
}

func TriggerActionFromCronAction(action triggers.CronAction) TriggerAction {
	return TriggerAction{Prompt: action.Prompt, Promote: promoteActionFromCronAction(action), PromoteRequiresApproval: action.PromoteRequiresApproval, Delivery: TriggerDelivery(action.Delivery)}
}

func promoteActionFromCronAction(action triggers.CronAction) PromoteAction {
	promote := PromoteAction{Kind: PromoteActionKind(action.Promote)}
	if action.PromoteTemplateBody != "" {
		promote.TemplateBody = &action.PromoteTemplateBody
	}
	if len(action.PromoteRequiredSubstrings) > 0 {
		promote.RequiredSubstrings = append([]string(nil), action.PromoteRequiredSubstrings...)
	}
	return promote
}

type PromoteActionKind string

const (
	PromoteNone                          PromoteActionKind = "none"
	PromoteSummaryNow                    PromoteActionKind = "promote_summary_now"
	PromoteSummaryWhenSummaryContains    PromoteActionKind = "promote_summary_when_summary_contains"
	PromoteSummaryWhenResultDetailsMatch PromoteActionKind = "promote_summary_when_result_details_match"

	PromoteActionNone = PromoteNone
)

type PromoteAction struct {
	Kind               PromoteActionKind
	TemplateBody       *string
	RequiredSubstrings []string
	Condition          *PromotionConditionAnyOf
}

type PromotionConditionAnyOf struct {
	JSONPointer string
	AnyOf       []string
}

type PromotionCondition = PromotionConditionAnyOf

func (condition PromotionConditionAnyOf) Evaluate(details any) ([]string, string) {
	value, ok := jsonPointer(details, condition.JSONPointer)
	if !ok {
		return nil, PromotionSkipPointerMissing.AuditString()
	}
	items, ok := value.([]any)
	if !ok {
		return nil, PromotionSkipValueNotArray.AuditString()
	}
	matched := []string{}
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		for _, candidate := range condition.AnyOf {
			if text == candidate {
				matched = append(matched, text)
				break
			}
		}
	}
	if len(matched) == 0 {
		return nil, PromotionSkipEmptyIntersection.AuditString()
	}
	return matched, ""
}

type PromotionConditionSkipReason string

const (
	PromotionSkipPointerMissing    PromotionConditionSkipReason = "result_details_missing"
	PromotionSkipValueNotArray     PromotionConditionSkipReason = "result_details_not_array"
	PromotionSkipEmptyIntersection PromotionConditionSkipReason = "no_matching_rule_id"

	PromotionConditionSkipReasonPointerMissing    = PromotionSkipPointerMissing
	PromotionConditionSkipReasonValueNotArray     = PromotionSkipValueNotArray
	PromotionConditionSkipReasonEmptyIntersection = PromotionSkipEmptyIntersection
)

func (reason PromotionConditionSkipReason) AuditString() string { return string(reason) }

func (reason PromotionConditionSkipReason) AsAuditStr() string { return reason.AuditString() }

func jsonPointer(value any, pointer string) (any, bool) {
	if pointer == "" {
		return value, true
	}
	if len(pointer) == 0 || pointer[0] != '/' {
		return nil, false
	}
	current := value
	for _, part := range strings.Split(pointer[1:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

type triggerResultDetailsBuilder struct {
	mu             sync.Mutex
	matchedRuleIDs []string
}

func newTriggerResultDetailsBuilder() *triggerResultDetailsBuilder {
	return &triggerResultDetailsBuilder{}
}

func (builder *triggerResultDetailsBuilder) MarkDynamicRuleMatched(ruleID string) {
	if builder == nil || ruleID == "" {
		return
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	for _, existing := range builder.matchedRuleIDs {
		if existing == ruleID {
			return
		}
	}
	builder.matchedRuleIDs = append(builder.matchedRuleIDs, ruleID)
}

func (builder *triggerResultDetailsBuilder) Snapshot() any {
	if builder == nil {
		return nil
	}
	builder.mu.Lock()
	defer builder.mu.Unlock()
	if len(builder.matchedRuleIDs) == 0 {
		return nil
	}
	ids := make([]any, 0, len(builder.matchedRuleIDs))
	for _, id := range builder.matchedRuleIDs {
		ids = append(ids, id)
	}
	return map[string]any{"dynamic_trigger": map[string]any{"matched_rule_ids": ids}}
}

type triggerResultMarkerTool struct {
	builder *triggerResultDetailsBuilder
}

func (tool triggerResultMarkerTool) Name() string { return "mark_dynamic_rule_matched" }

func (tool triggerResultMarkerTool) Description() string {
	return "Mark a dynamic trigger rule as matched for trigger result promotion."
}

func (tool triggerResultMarkerTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	ruleID, _ := call.Arguments["rule_id"].(string)
	tool.builder.MarkDynamicRuleMatched(ruleID)
	return agent.ToolResult{Content: "marked dynamic rule"}, nil
}

type BeforeTriggerActionContext struct {
	Trigger triggers.Trigger
	Runtime triggers.TriggerRuntimeSnapshot
}

type BeforeTriggerActionHook func(ctx BeforeTriggerActionContext) TriggerAction

type OnTurnEndContext struct {
	Transcript        []agent.AgentMessage
	ContinuationCount uint32
	LastUserPrompt    *string
}

type TurnEndActionKind string

const (
	TurnEndNoop     TurnEndActionKind = "noop"
	TurnEndStop     TurnEndActionKind = "stop"
	TurnEndPause    TurnEndActionKind = "pause"
	TurnEndContinue TurnEndActionKind = "continue"

	TurnEndActionNoop = TurnEndNoop
)

type TurnEndAction struct {
	Kind   TurnEndActionKind
	Reason string
	Prompt string
}

func NoopTurnEnd() TurnEndAction { return TurnEndAction{Kind: TurnEndNoop} }

func StopTurnEnd() TurnEndAction { return TurnEndAction{Kind: TurnEndStop} }

func PauseTurnEnd(reason string) TurnEndAction {
	return TurnEndAction{Kind: TurnEndPause, Reason: reason}
}

func ContinueTurnEnd(prompt string) TurnEndAction {
	return TurnEndAction{Kind: TurnEndContinue, Prompt: prompt}
}

func (action TurnEndAction) AuditString() (string, bool) {
	switch action.Kind {
	case TurnEndStop:
		return "stop", true
	case TurnEndPause:
		return "pause", true
	case TurnEndContinue:
		return "continue", true
	default:
		return "", false
	}
}

func (action TurnEndAction) AsAuditStr() (string, bool) { return action.AuditString() }

type TurnEndDecision struct {
	Action  TurnEndAction
	Payload map[string]any
}

func NewTurnEndDecision(action TurnEndAction, payload map[string]any) TurnEndDecision {
	return TurnEndDecision{Action: action, Payload: payload}
}

type OnTurnEndHook func(ctx OnTurnEndContext) TurnEndDecision

type NotificationStatusSnapshot struct {
	Hooks   []NotificationHookStatus
	Runtime triggers.TriggerRuntimeSnapshot
	Running []RunningTriggerState
}

type RunningTriggerState struct {
	TraceID       string
	SourceLabel   string
	EventLabel    string
	StartedAt     time.Time
	PromptPreview string
}

type HarnessEventKind string

const (
	HarnessEventSessionStart            HarnessEventKind = "session_start"
	HarnessEventCompaction              HarnessEventKind = "compaction"
	HarnessEventBranch                  HarnessEventKind = "branch"
	HarnessEventTriggerHandlingStart    HarnessEventKind = "trigger_handling_start"
	HarnessEventTriggerHandled          HarnessEventKind = "trigger_handled"
	HarnessEventTriggerPromptRequest    HarnessEventKind = "trigger_prompt_request"
	HarnessEventTriggerExecutionStarted HarnessEventKind = "trigger_execution_started"
	HarnessEventTriggerCompleted        HarnessEventKind = "trigger_completed"
	HarnessEventTriggerFailed           HarnessEventKind = "trigger_failed"
	HarnessEventTriggerRequestsMainRun  HarnessEventKind = "trigger_requests_main_run"
	HarnessEventPersistenceError        HarnessEventKind = "persistence_error"
	HarnessEventTriggerPromoted         HarnessEventKind = "trigger_promoted"
	HarnessEventPromotionPending        HarnessEventKind = "promotion_pending"
	HarnessEventTurnEnded               HarnessEventKind = "turn_ended"
	HarnessEventSkillsReloaded          HarnessEventKind = "skills_reloaded"
)

type HarnessEvent struct {
	Kind                 HarnessEventKind
	MessagesReplayed     int
	FromHook             bool
	Summary              string
	TokensBefore         uint64
	FromEntryID          *string
	ToEntryID            *string
	SummaryEntryID       *string
	Decision             string
	ContinuationCount    uint32
	Reason               *string
	NextPromptPreview    *string
	Total                int
	IDempotencyKey       string
	SourceKind           triggers.SourceKind
	SourceLabel          string
	EventLabel           string
	TraceID              string
	PromptPreview        string
	CostUSD              *float64
	Details              any
	TriggerState         triggers.TriggerState
	AuditEntryID         *string
	EvaluatorDecision    map[string]any
	TriggerPromptRequest TriggerPromptRequest
	Context              string
	Message              string
	PromoteKind          string
	InsertedEntryID      string
	TemplateName         *string
	RedactionStatus      string
	Preview              *string
}

type HarnessListener func(HarnessEvent)

func AdaptTriggerHarnessListener(listener triggers.HarnessListener) HarnessListener {
	return func(event HarnessEvent) {
		if listener == nil {
			return
		}
		_ = listener(triggers.HarnessEvent{TraceID: event.TraceID, Summary: event.Summary, Error: event.Message})
	}
}

func SessionStartEvent(messagesReplayed int) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventSessionStart, MessagesReplayed: messagesReplayed}
}

func CompactionEvent(fromHook bool, summary string, tokensBefore uint64) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventCompaction, FromHook: fromHook, Summary: summary, TokensBefore: tokensBefore}
}

func BranchEvent(fromEntryID *string, toEntryID *string, summaryEntryID *string) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventBranch, FromEntryID: fromEntryID, ToEntryID: toEntryID, SummaryEntryID: summaryEntryID}
}

func TurnEndedEvent(decision string, continuationCount uint32, reason *string, nextPromptPreview *string) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventTurnEnded, Decision: decision, ContinuationCount: continuationCount, Reason: reason, NextPromptPreview: nextPromptPreview}
}

func SkillsReloadedEvent(total int) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventSkillsReloaded, Total: total}
}

func TriggerHandlingStartEvent(idempotencyKey string, sourceKind triggers.SourceKind, sourceLabel string, eventLabel string, traceID string) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventTriggerHandlingStart, IDempotencyKey: idempotencyKey, SourceKind: sourceKind, SourceLabel: sourceLabel, EventLabel: eventLabel, TraceID: traceID}
}

func TriggerHandledEvent(idempotencyKey string, traceID string, state triggers.TriggerState, auditEntryID *string, evaluatorDecision map[string]any) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventTriggerHandled, IDempotencyKey: idempotencyKey, TraceID: traceID, TriggerState: state, AuditEntryID: auditEntryID, EvaluatorDecision: evaluatorDecision}
}

func TriggerPromptRequestEvent(request TriggerPromptRequest) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventTriggerPromptRequest, TriggerPromptRequest: request}
}

func TriggerExecutionStartedEvent(traceID string, sourceLabel string, eventLabel string, promptPreview string) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventTriggerExecutionStarted, TraceID: traceID, SourceLabel: sourceLabel, EventLabel: eventLabel, PromptPreview: promptPreview}
}

func TriggerCompletedEvent(traceID string, summary *string, costUSD *float64, details any) HarnessEvent {
	event := HarnessEvent{Kind: HarnessEventTriggerCompleted, TraceID: traceID, CostUSD: costUSD, Details: details}
	if summary != nil {
		event.Summary = *summary
	}
	return event
}

func TriggerFailedEvent(traceID string, reason string, summary *string, details any) HarnessEvent {
	event := HarnessEvent{Kind: HarnessEventTriggerFailed, TraceID: traceID, Reason: &reason, Details: details}
	if summary != nil {
		event.Summary = *summary
	}
	return event
}

func TriggerRequestsMainRunEvent(traceID string) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventTriggerRequestsMainRun, TraceID: traceID}
}

func PersistenceErrorEvent(context string, message string) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventPersistenceError, Context: context, Message: message}
}

func TriggerPromotedEvent(traceID string, promoteKind string, insertedEntryID string, templateName *string, redactionStatus string) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventTriggerPromoted, TraceID: traceID, PromoteKind: promoteKind, InsertedEntryID: insertedEntryID, TemplateName: templateName, RedactionStatus: redactionStatus}
}

func PromotionPendingEvent(traceID string, promoteKind string, templateName *string, preview *string) HarnessEvent {
	return HarnessEvent{Kind: HarnessEventPromotionPending, TraceID: traceID, PromoteKind: promoteKind, TemplateName: templateName, Preview: preview}
}
