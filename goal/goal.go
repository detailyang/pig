package goal

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/harness"
	"github.com/detailyang/pig/session"
)

const CustomType = "goal_state"
const TranscriptCharLimit = 40000
const MaxContinuations uint32 = 8

const CUSTOM_TYPE = CustomType
const TRANSCRIPT_CHAR_LIMIT = TranscriptCharLimit
const MAX_CONTINUATIONS = MaxContinuations
const CUSTOMTYPE = CustomType
const MAXCONTINUATIONS = MaxContinuations

type Status string

type GoalStatus = Status

const (
	StatusPursuing      Status = "pursuing"
	StatusPaused        Status = "paused"
	StatusAchieved      Status = "achieved"
	StatusBudgetLimited Status = "budget_limited"
	StatusCleared       Status = "cleared"

	GoalStatusPursuing      GoalStatus = StatusPursuing
	GoalStatusPaused        GoalStatus = StatusPaused
	GoalStatusAchieved      GoalStatus = StatusAchieved
	GoalStatusBudgetLimited GoalStatus = StatusBudgetLimited
	GoalStatusCleared       GoalStatus = StatusCleared
)

func (status Status) AsStr() string { return string(status) }

func (status *Status) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if validStatus(Status(value)) {
		*status = Status(value)
		return nil
	}
	return fmt.Errorf("goal status unknown value %q", value)
}

func (status Status) MarshalJSON() ([]byte, error) {
	if !validStatus(status) {
		return nil, fmt.Errorf("goal status unknown value %q", status)
	}
	return json.Marshal(string(status))
}

func validStatus(status Status) bool {
	switch status {
	case StatusPursuing, StatusPaused, StatusAchieved, StatusBudgetLimited, StatusCleared:
		return true
	default:
		return false
	}
}

type State struct {
	Condition  string  `json:"condition"`
	Status     Status  `json:"status"`
	Iterations uint32  `json:"iterations"`
	LastReason *string `json:"last_reason,omitempty"`
	UpdatedAt  string  `json:"updated_at"`
}

type GoalState = State

func (state State) Active() bool {
	switch state.Status {
	case StatusPursuing, StatusPaused, StatusBudgetLimited:
		return true
	default:
		return false
	}
}

func NewState(condition string, now time.Time) State {
	return State{Condition: condition, Status: StatusPursuing, UpdatedAt: now.UTC().Format(time.RFC3339)}
}

func Set(h *harness.AgentHarness, condition string, now time.Time) (State, error) {
	if h == nil || h.Session() == nil {
		return State{}, fmt.Errorf("goal requires session")
	}
	state := NewState(condition, now)
	if err := AppendState(h, state); err != nil {
		return State{}, err
	}
	return state, nil
}

func AppendState(h *harness.AgentHarness, state State) error {
	if h == nil || h.Session() == nil {
		return fmt.Errorf("goal requires session")
	}
	_, err := h.Session().AppendCustom(CustomType, state)
	return err
}

func Pause(state State, reason string, now time.Time) (State, error) {
	if !state.Active() || state.Status == StatusCleared || state.Status == StatusAchieved {
		return State{}, fmt.Errorf("no active goal; set one with /goal <condition>")
	}
	state.Status = StatusPaused
	state.LastReason = &reason
	state.UpdatedAt = now.UTC().Format(time.RFC3339)
	return state, nil
}

func Resume(state State, now time.Time) (State, error) {
	if state.Status != StatusPaused && state.Status != StatusBudgetLimited {
		return State{}, fmt.Errorf("goal is not paused")
	}
	state.Status = StatusPursuing
	state.UpdatedAt = now.UTC().Format(time.RFC3339)
	return state, nil
}

func Clear(state State, now time.Time) State {
	state.Status = StatusCleared
	state.UpdatedAt = now.UTC().Format(time.RFC3339)
	return state
}

func CustomEntry(id string, state State) session.Entry {
	return session.Entry{EntryType: session.EntryTypeCustom, EntryID: id, CustomType: CustomType, Data: state, Timestamp: state.UpdatedAt}
}

func LatestFromEntries(entries []session.Entry) (State, bool) {
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.EntryType != session.EntryTypeCustom || entry.CustomType != CustomType || entry.Data == nil {
			continue
		}
		state, err := decodeState(entry.Data)
		if err == nil {
			return state, true
		}
	}
	return State{}, false
}

func CurrentFromEntries(entries []session.Entry) (State, bool) {
	state, ok := LatestFromEntries(entries)
	if !ok || state.Status == StatusCleared {
		return State{}, false
	}
	return state, true
}

func Current(h *harness.AgentHarness) (State, bool) {
	if h == nil || h.Session() == nil {
		return State{}, false
	}
	entries, err := h.Session().Entries()
	if err != nil {
		return State{}, false
	}
	return CurrentFromEntries(entries)
}

func StopHook(h *harness.AgentHarness) harness.OnTurnEndHook {
	return func(ctx harness.OnTurnEndContext) harness.TurnEndDecision {
		return EvaluateStopHook(context.Background(), h, ctx)
	}
}

func EvaluateStopHook(ctx context.Context, h *harness.AgentHarness, turn harness.OnTurnEndContext) harness.TurnEndDecision {
	state, ok := Current(h)
	if !ok || state.Status != StatusPursuing {
		return harness.NewTurnEndDecision(harness.NoopTurnEnd(), nil)
	}
	model := ai.Model{}
	if h != nil && h.Agent() != nil {
		agentState := h.Agent().State()
		if agentState.Model != nil {
			model = *agentState.Model
		}
	}
	if model.ID == "" && model.Provider == "" && model.API == "" {
		return goalPauseDecision(h, state, "goal evaluator has no current model")
	}
	transcript := TranscriptFromMessages(turn.Transcript, TranscriptCharLimit)
	output, err := h.RunEvaluatorWithConfig(ctx, EvaluatorSystemPrompt(), EvaluatorUserPrompt(state.Condition, transcript), model, ai.ThinkingOff)
	if err != nil {
		return goalPauseDecision(h, state, "goal evaluator failed: "+err.Error())
	}
	if output.LastAssistantText == nil {
		return goalPauseDecision(h, state, "goal evaluator returned no text")
	}
	decision, err := ParseDecision(*output.LastAssistantText)
	if err != nil {
		return goalPauseDecision(h, state, "goal evaluator failed: "+err.Error())
	}
	updated, action := ApplyDecision(state, decision, time.Now())
	persistStateBestEffort(h, updated)
	return turnEndDecisionFromAction(action)
}

func goalPauseDecision(h *harness.AgentHarness, state State, reason string) harness.TurnEndDecision {
	state = PersistPause(h, state, reason, time.Now())
	return turnEndDecisionFromAction(PauseDecision(reason, state))
}

func persistStateBestEffort(h *harness.AgentHarness, state State) {
	_ = AppendState(h, state)
}

func turnEndDecisionFromAction(action Action) harness.TurnEndDecision {
	payloadMap := map[string]any{
		"goal_status":       string(action.Payload.GoalStatus),
		"condition":         action.Payload.Condition,
		"ok":                action.Payload.OK,
		"reason":            action.Payload.Reason,
		"iterations":        action.Payload.Iterations,
		"max_continuations": action.Payload.MaxContinuations,
		"updated_at":        action.Payload.UpdatedAt,
	}
	switch action.Kind {
	case ActionStop:
		return harness.NewTurnEndDecision(harness.StopTurnEnd(), payloadMap)
	case ActionPause:
		return harness.NewTurnEndDecision(harness.PauseTurnEnd(action.Reason), payloadMap)
	case ActionContinue:
		return harness.NewTurnEndDecision(harness.ContinueTurnEnd(action.Prompt), payloadMap)
	default:
		return harness.NewTurnEndDecision(harness.NoopTurnEnd(), payloadMap)
	}
}

func decodeState(value any) (State, error) {
	if state, ok := value.(State); ok {
		return state, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

type Decision struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

func ParseDecision(text string) (Decision, error) {
	trimmed := strings.TrimSpace(text)
	var decision Decision
	if err := json.Unmarshal([]byte(trimmed), &decision); err != nil {
		start := strings.Index(trimmed, "{")
		end := strings.LastIndex(trimmed, "}")
		if start < 0 || end < start || json.Unmarshal([]byte(trimmed[start:end+1]), &decision) != nil {
			return Decision{}, fmt.Errorf("goal evaluator returned invalid JSON: %s", TailChars(trimmed, 300))
		}
	}
	if strings.TrimSpace(decision.Reason) == "" {
		return Decision{}, fmt.Errorf("goal evaluator returned an empty reason")
	}
	return decision, nil
}

type ActionKind string

const (
	ActionNoop     ActionKind = "noop"
	ActionStop     ActionKind = "stop"
	ActionContinue ActionKind = "continue"
	ActionPause    ActionKind = "pause"
)

type Action struct {
	Kind    ActionKind
	Prompt  string
	Reason  string
	Payload Payload
}

type Payload struct {
	GoalStatus       Status  `json:"goal_status"`
	Condition        string  `json:"condition"`
	OK               *bool   `json:"ok"`
	Reason           *string `json:"reason,omitempty"`
	Iterations       uint32  `json:"iterations"`
	MaxContinuations uint32  `json:"max_continuations"`
	UpdatedAt        string  `json:"updated_at"`
}

func ApplyDecision(state State, decision Decision, now time.Time) (State, Action) {
	state.Iterations++
	state.LastReason = &decision.Reason
	state.UpdatedAt = now.UTC().Format(time.RFC3339)
	if decision.OK {
		state.Status = StatusAchieved
		return state, Action{Kind: ActionStop, Payload: payload(state, boolPtr(true))}
	}
	if state.Iterations >= MaxContinuations {
		state.Status = StatusBudgetLimited
		reason := fmt.Sprintf("goal continuation limit reached (%d); resume with /goal resume", MaxContinuations)
		return state, Action{Kind: ActionPause, Reason: reason, Payload: payload(state, boolPtr(false))}
	}
	return state, Action{Kind: ActionContinue, Prompt: ContinuationPrompt(state.Condition, decision.Reason), Payload: payload(state, boolPtr(false))}
}

func PauseAction(state State, reason string) Action {
	return PauseDecision(reason, state)
}

func PauseDecision(reason string, state State) Action {
	return Action{Kind: ActionPause, Reason: reason, Payload: GoalPayload(state, nil)}
}

func PersistPause(h *harness.AgentHarness, state State, reason string, now time.Time) State {
	state.Status = StatusPaused
	state.LastReason = &reason
	state.UpdatedAt = now.UTC().Format(time.RFC3339)
	persistStateBestEffort(h, state)
	return state
}

func payload(state State, ok *bool) Payload {
	return GoalPayload(state, ok)
}

func GoalPayload(state State, ok *bool) Payload {
	return Payload{GoalStatus: state.Status, Condition: state.Condition, OK: ok, Reason: state.LastReason, Iterations: state.Iterations, MaxContinuations: MaxContinuations, UpdatedAt: state.UpdatedAt}
}

func EvaluatorUserPrompt(condition, transcript string) string {
	return fmt.Sprintf("Goal condition:\n%s\n\nConversation transcript:\n%s", condition, transcript)
}

func EvaluatorSystemPrompt() string {
	return `You are evaluating a stop-condition hook in pie.
Read the conversation transcript carefully, then judge whether the user-provided condition is satisfied.
You cannot call tools. Only use explicit evidence in the transcript.
Your response must be a JSON object with one of these shapes:
{"ok": true, "reason": "<quote evidence from the transcript that satisfies the condition>"}
{"ok": false, "reason": "<quote what is missing or what blocks the condition>"}
Always include a reason field, quoting specific text from the transcript whenever possible.
If the transcript does not contain clear evidence that the condition is satisfied, return {"ok": false, "reason": "insufficient evidence in transcript"}.`
}

func ContinuationPrompt(condition, reason string) string {
	return fmt.Sprintf("The current /goal is not satisfied yet.\n\nGoal condition:\n%s\n\nGoal evaluator says what is missing or blocking completion:\n%s\n\nContinue working toward the goal. Do not claim completion until the transcript contains explicit evidence that satisfies the condition.", condition, reason)
}

func TranscriptFromMessages(messages []agent.Message, maxChars int) string {
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		if line, ok := agentMessageText(message); ok {
			lines = append(lines, line)
		}
	}
	return TailChars(strings.Join(lines, "\n\n"), maxChars)
}

func AgentMessageText(message agent.Message) string {
	text, _ := agentMessageText(message)
	return text
}

func agentMessageText(message agent.Message) (string, bool) {
	switch message.Kind {
	case agent.MessageKindLLM:
		if message.LLM == nil {
			return "", false
		}
		if message.LLM.Role == ai.RoleUser {
			return "User: " + UserContentText(contentBlockUserContent(message.LLM.Content, true)), true
		}
		if message.LLM.Role == ai.RoleAssistant {
			text := assistantContentText(message.LLM.Content)
			if strings.TrimSpace(text) == "" {
				return "", false
			}
			return "Assistant: " + text, true
		}
		text := UserContentText(contentBlockUserContent(message.LLM.Content, true))
		if strings.TrimSpace(text) == "" {
			return "", false
		}
		return string(message.LLM.Role) + ": " + text, true
	case agent.MessageKindToolResult:
		if message.ToolResult == nil {
			return "", false
		}
		text := toolResultText(*message.ToolResult)
		if strings.TrimSpace(text) == "" {
			return "", false
		}
		return fmt.Sprintf("ToolResult(%s error=%v): %s", message.ToolResult.Name, message.ToolResult.Error != "" || message.ToolResult.IsError, text), true
	default:
		return "", false
	}
}

func UserContentText(content ai.UserContent) string {
	if content.Blocks == nil {
		return content.Text
	}
	parts := make([]string, 0, len(content.Blocks))
	for _, block := range content.Blocks {
		switch block.Type {
		case ai.UserContentText:
			parts = append(parts, block.Text)
		case ai.UserContentImage:
			parts = append(parts, "[image]")
		}
	}
	return strings.Join(parts, "\n")
}

func assistantContentText(blocks []ai.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ai.ContentText:
			parts = append(parts, block.Text)
		case ai.ContentThinking:
			parts = append(parts, block.Thinking)
		case ai.ContentToolCall:
			if block.ToolCall != nil {
				parts = append(parts, block.ToolCall.Name)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func toolResultText(result agent.ToolResult) string {
	if len(result.ContentBlocks) > 0 {
		return UserContentText(contentBlockUserContent(result.ContentBlocks, false))
	}
	return result.Content
}

func contentBlockUserContent(blocks []ai.ContentBlock, includeImages bool) ai.UserContent {
	userBlocks := make([]ai.UserContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ai.ContentText:
			userBlocks = append(userBlocks, ai.UserContentBlock{Type: ai.UserContentText, Text: block.Text})
		case ai.ContentImage:
			if includeImages {
				userBlocks = append(userBlocks, ai.UserContentBlock{Type: ai.UserContentImage, Data: block.Data, MimeType: block.MimeType})
			}
		}
	}
	return ai.UserContentBlocksValue(userBlocks)
}

func TailChars(text string, maxChars int) string {
	if maxChars <= 0 {
		return "[transcript truncated to last 0 chars]\n"
	}
	if utf8.RuneCountInString(text) <= maxChars {
		return text
	}
	runes := []rune(text)
	tail := string(runes[len(runes)-maxChars:])
	return fmt.Sprintf("[transcript truncated to last %d chars]\n%s", maxChars, tail)
}

func boolPtr(value bool) *bool { return &value }
