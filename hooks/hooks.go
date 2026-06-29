package hooks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/config"
	"github.com/detailyang/pig/harness"
)

const hookSummaryMaxChars = 2000

const defaultTimeout = 5 * time.Second

type Event string

const (
	AgentStart    Event = "agent_start"
	AgentEnd      Event = "agent_end"
	TurnStart     Event = "turn_start"
	TurnEnd       Event = "turn_end"
	MessageStart  Event = "message_start"
	MessageUpdate Event = "message_update"
	MessageEnd    Event = "message_end"
	ToolStart     Event = "tool_start"
	ToolUpdate    Event = "tool_update"
	ToolEnd       Event = "tool_end"
	Compaction    Event = "compaction"
)

func (event Event) String() string { return string(event) }

func (event Event) AsStr() string { return string(event) }

func ParseEvent(value string) (Event, bool) {
	for _, event := range AllEvents() {
		if event.String() == value {
			return event, true
		}
	}
	return "", false
}

func Parse(value string) (Event, bool) {
	return ParseEvent(value)
}

func AllEvents() []Event {
	return []Event{AgentStart, AgentEnd, TurnStart, TurnEnd, MessageStart, MessageUpdate, MessageEnd, ToolStart, ToolUpdate, ToolEnd, Compaction}
}

type OnFailure string

const (
	OnFailureWarn   OnFailure = "warn"
	OnFailureIgnore OnFailure = "ignore"
)

type CWDMode string

const (
	CWDProject CWDMode = "project"
	CWDPie     CWDMode = "pie"
	CWDHome    CWDMode = "home"
)

type Rule struct {
	Event          Event
	Command        string
	Webhook        string
	WebhookPresent bool
	Headers        map[string]string
	Timeout        time.Duration
	CWD            CWDMode
	OnFailure      OnFailure
	Tool           string
	ToolPresent    bool
	Source         string
}

func ParseHooksFile(text string, source string) ([]Rule, []string) {
	configs := parseHookRuleConfigs(text)
	rules := make([]Rule, 0, len(configs))
	diagnostics := []string{}
	for index, cfg := range configs {
		enabled, ok := parseHookEnabled(cfg["enabled"], cfg["enabled.quoted"] == "true")
		if !ok {
			diagnostics = append(diagnostics, fmt.Sprintf("hooks %s: hook #%d has invalid enabled %q", source, index+1, cfg["enabled"]))
			continue
		}
		if !enabled {
			continue
		}
		if cfg["headers.invalid"] == "true" {
			diagnostics = append(diagnostics, fmt.Sprintf("hooks %s: hook #%d has invalid headers", source, index+1))
			continue
		}
		invalidStringField := false
		for _, field := range []string{"event", "command", "webhook", "tool", "cwd", "on_failure"} {
			if !hookStringFieldOK(cfg, field) {
				diagnostics = append(diagnostics, fmt.Sprintf("hooks %s: hook #%d has invalid %s %q", source, index+1, field, cfg[field]))
				invalidStringField = true
				break
			}
		}
		if invalidStringField {
			continue
		}
		event, ok := ParseEvent(cfg["event"])
		if !ok {
			diagnostics = append(diagnostics, fmt.Sprintf("hooks %s: hook #%d has unknown event %q", source, index+1, cfg["event"]))
			continue
		}
		command := cfg["command"]
		webhook := cfg["webhook"]
		webhookPresent := cfg["webhook.quoted"] == "true"
		if strings.TrimSpace(command) == "" && !webhookPresent {
			diagnostics = append(diagnostics, fmt.Sprintf("hooks %s: hook #%d has neither command nor webhook", source, index+1))
			continue
		}
		cwd, ok := parseHookCWD(cfg["cwd"])
		if !ok {
			diagnostics = append(diagnostics, fmt.Sprintf("hooks %s: hook #%d has invalid cwd %q", source, index+1, cfg["cwd"]))
			continue
		}
		onFailure, ok := parseHookOnFailure(cfg["on_failure"])
		if !ok {
			diagnostics = append(diagnostics, fmt.Sprintf("hooks %s: hook #%d has invalid on_failure %q", source, index+1, cfg["on_failure"]))
			continue
		}
		headers, ok := parseHookHeaders(cfg["headers"])
		if !ok {
			diagnostics = append(diagnostics, fmt.Sprintf("hooks %s: hook #%d has invalid headers", source, index+1))
			continue
		}
		rule := Rule{Event: event, Webhook: webhook, WebhookPresent: webhookPresent, Headers: headers, Timeout: defaultTimeout, CWD: CWDProject, OnFailure: OnFailureWarn, Tool: cfg["tool"], ToolPresent: cfg["tool.quoted"] == "true", Source: source}
		if strings.TrimSpace(command) != "" {
			rule.Command = command
		}
		timeout, ok := parseHookTimeout(cfg["timeout_ms"], cfg["timeout_ms.quoted"] == "true")
		if !ok {
			diagnostics = append(diagnostics, fmt.Sprintf("hooks %s: hook #%d has invalid timeout_ms %q", source, index+1, cfg["timeout_ms"]))
			continue
		}
		if timeout > 0 {
			rule.Timeout = timeout
		}
		rule.CWD = cwd
		rule.OnFailure = onFailure
		rules = append(rules, rule)
	}
	return rules, diagnostics
}

func hookStringFieldOK(cfg map[string]string, field string) bool {
	_, exists := cfg[field]
	if !exists {
		return true
	}
	return cfg[field+".quoted"] == "true"
}

func parseHookEnabled(value string, quoted bool) (bool, bool) {
	if quoted {
		return false, false
	}
	switch value {
	case "", "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func parseHookCWD(value string) (CWDMode, bool) {
	switch value {
	case "", "project":
		return CWDProject, true
	case "pie":
		return CWDPie, true
	case "home":
		return CWDHome, true
	default:
		return "", false
	}
}

func parseHookOnFailure(value string) (OnFailure, bool) {
	switch value {
	case "", "warn":
		return OnFailureWarn, true
	case "ignore":
		return OnFailureIgnore, true
	default:
		return "", false
	}
}

func parseHookRuleConfigs(text string) []map[string]string {
	var configs []map[string]string
	var current map[string]string
	inHeaders := false
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[[hook]]" {
			current = map[string]string{}
			configs = append(configs, current)
			inHeaders = false
			continue
		}
		if line == "[hook.headers]" {
			inHeaders = true
			continue
		}
		if current == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if inHeaders {
			appendHookHeader(current, key, value)
			continue
		}
		trimmedKey := strings.TrimSpace(key)
		if headerKey, ok := strings.CutPrefix(trimmedKey, "headers."); ok {
			appendHookHeader(current, headerKey, value)
			continue
		}
		if isQuotedHookValue(value) {
			current[trimmedKey+".quoted"] = "true"
		}
		current[trimmedKey] = normalizeHookValue(value)
	}
	return configs
}

func isQuotedHookValue(value string) bool {
	value = strings.TrimSpace(stripTOMLComment(value))
	return strings.HasPrefix(value, `"`) || strings.HasPrefix(value, `'`)
}

func normalizeHookValue(value string) string {
	value = strings.TrimSpace(stripTOMLComment(value))
	if strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") {
		return value
	}
	return strings.Trim(value, `"'`)
}

func appendHookHeader(config map[string]string, key string, value string) {
	if !isQuotedHookValue(value) {
		config["headers.invalid"] = "true"
		return
	}
	headers, ok := parseHookHeaders(config["headers"])
	if !ok {
		config["headers.invalid"] = "true"
		return
	}
	if headers == nil {
		headers = map[string]string{}
	}
	key = strings.Trim(strings.TrimSpace(key), `"'`)
	value = strings.Trim(strings.TrimSpace(stripTOMLComment(value)), `"'`)
	if key != "" {
		headers[key] = value
	}
	config["headers"] = formatInlineHeaders(headers)
}

func parseHookTimeout(value string, quoted bool) (time.Duration, bool) {
	if quoted {
		return 0, false
	}
	if value == "" {
		return 0, true
	}
	millis, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(millis) * time.Millisecond, true
}

func parseHookHeaders(value string) (map[string]string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return nil, true
	}
	headers := map[string]string{}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}"))
	for _, item := range splitInlineTable(inner) {
		key, val, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `"'`)
		if !isQuotedHookValue(val) {
			return nil, false
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if key != "" {
			headers[key] = val
		}
	}
	return headers, true
}

func splitInlineTable(value string) []string {
	var parts []string
	inSingle := false
	inDouble := false
	start := 0
	for index, char := range value {
		switch char {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ',':
			if !inSingle && !inDouble {
				parts = append(parts, strings.TrimSpace(value[start:index]))
				start = index + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(value[start:]))
	return parts
}

func formatInlineHeaders(headers map[string]string) string {
	parts := make([]string, 0, len(headers))
	for key, value := range headers {
		parts = append(parts, fmt.Sprintf("%q=%q", key, value))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func stripTOMLComment(value string) string {
	inString := false
	for index, char := range value {
		if char == '"' {
			inString = !inString
		}
		if char == '#' && !inString {
			return value[:index]
		}
	}
	return value
}

func (rule Rule) Matches(data EventData) bool {
	if rule.Event != data.Event {
		return false
	}
	if (rule.ToolPresent || rule.Tool != "") && data.ToolName != rule.Tool {
		return false
	}
	return true
}

type EventData struct {
	Event                    Event
	MessageKind              string
	MessageKindPresent       bool
	MessageSummary           string
	MessageSummaryPresent    bool
	AssistantEvent           string
	AssistantEventPresent    bool
	ToolCallID               string
	ToolCallIDPresent        bool
	ToolName                 string
	ToolNamePresent          bool
	ToolIsError              *bool
	ToolArgs                 any
	ToolResultSummary        string
	ToolResultSummaryPresent bool
	CompactionTrigger        string
	CompactionTriggerPresent bool
	CompactionTokensBefore   *uint64
	CompactionSummary        string
	CompactionSummaryPresent bool
}

func BasicEventData(event Event) EventData {
	return EventData{Event: event}
}

func Basic(event Event) EventData {
	return BasicEventData(event)
}

type Payload struct {
	Event                    string  `json:"event"`
	SessionID                string  `json:"session_id"`
	CWD                      string  `json:"cwd"`
	ModelProvider            string  `json:"model_provider"`
	ModelID                  string  `json:"model_id"`
	ThinkingLevel            string  `json:"thinking_level"`
	Source                   string  `json:"source"`
	MessageKind              string  `json:"message_kind"`
	MessageKindPresent       bool    `json:"-"`
	MessageSummary           string  `json:"message_summary"`
	MessageSummaryPresent    bool    `json:"-"`
	AssistantEvent           string  `json:"assistant_event"`
	AssistantEventPresent    bool    `json:"-"`
	ToolCallID               string  `json:"tool_call_id"`
	ToolCallIDPresent        bool    `json:"-"`
	ToolName                 string  `json:"tool_name"`
	ToolNamePresent          bool    `json:"-"`
	ToolIsError              *bool   `json:"tool_is_error,omitempty"`
	ToolArgs                 any     `json:"tool_args,omitempty"`
	ToolResultSummary        string  `json:"tool_result_summary"`
	ToolResultSummaryPresent bool    `json:"-"`
	CompactionTrigger        string  `json:"compaction_trigger"`
	CompactionTriggerPresent bool    `json:"-"`
	CompactionTokensBefore   *uint64 `json:"compaction_tokens_before,omitempty"`
	CompactionSummary        string  `json:"compaction_summary"`
	CompactionSummaryPresent bool    `json:"-"`
}

type HookPayload = Payload

func MarshalPayload(payload Payload) ([]byte, error) {
	return marshalJSONNoHTMLEscape(payload)
}

func (payload Payload) MarshalJSON() ([]byte, error) {
	return marshalJSONNoHTMLEscape(struct {
		Event                  string  `json:"event"`
		SessionID              string  `json:"session_id"`
		CWD                    string  `json:"cwd"`
		ModelProvider          string  `json:"model_provider"`
		ModelID                string  `json:"model_id"`
		ThinkingLevel          string  `json:"thinking_level"`
		Source                 string  `json:"source"`
		MessageKind            *string `json:"message_kind"`
		MessageSummary         *string `json:"message_summary"`
		AssistantEvent         *string `json:"assistant_event"`
		ToolCallID             *string `json:"tool_call_id"`
		ToolName               *string `json:"tool_name"`
		ToolIsError            *bool   `json:"tool_is_error"`
		ToolArgs               any     `json:"tool_args"`
		ToolResultSummary      *string `json:"tool_result_summary"`
		CompactionTrigger      *string `json:"compaction_trigger"`
		CompactionTokensBefore *uint64 `json:"compaction_tokens_before"`
		CompactionSummary      *string `json:"compaction_summary"`
	}{
		Event:                  payload.Event,
		SessionID:              payload.SessionID,
		CWD:                    payload.CWD,
		ModelProvider:          payload.ModelProvider,
		ModelID:                payload.ModelID,
		ThinkingLevel:          payload.ThinkingLevel,
		Source:                 payload.Source,
		MessageKind:            optionalStringPresent(payload.MessageKind, payload.MessageKindPresent),
		MessageSummary:         optionalStringPresent(payload.MessageSummary, payload.MessageSummaryPresent),
		AssistantEvent:         optionalStringPresent(payload.AssistantEvent, payload.AssistantEventPresent),
		ToolCallID:             optionalStringPresent(payload.ToolCallID, payload.ToolCallIDPresent),
		ToolName:               optionalStringPresent(payload.ToolName, payload.ToolNamePresent),
		ToolIsError:            payload.ToolIsError,
		ToolArgs:               payload.ToolArgs,
		ToolResultSummary:      optionalStringPresent(payload.ToolResultSummary, payload.ToolResultSummaryPresent),
		CompactionTrigger:      optionalStringPresent(payload.CompactionTrigger, payload.CompactionTriggerPresent),
		CompactionTokensBefore: payload.CompactionTokensBefore,
		CompactionSummary:      optionalStringPresent(payload.CompactionSummary, payload.CompactionSummaryPresent),
	})
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalStringPresent(value string, present bool) *string {
	if !present {
		return nil
	}
	return &value
}

type Executor func(ctx context.Context, rule Rule, payload Payload) error

type RunnerOptions struct {
	SessionID     string
	CWD           string
	ModelProvider string
	ModelID       string
	ThinkingLevel string
	Rules         []Rule
	Executor      Executor
	HTTPClient    *http.Client
}

type Runner struct {
	sessionID     string
	cwd           string
	modelProvider string
	modelID       string
	thinkingLevel string
	rules         []Rule
	executor      Executor
	httpClient    *http.Client
}

type HookRunner = Runner

type LoadedHooks struct {
	Runner      *Runner
	Diagnostics []string
}

func Load(cwd string, options RunnerOptions) LoadedHooks {
	userPath := filepath.Join(config.BaseDir(), "hooks.toml")
	projectPath := filepath.Join(cwd, ".pie", "hooks.toml")
	diagnostics := []string{}
	rules := []Rule{}
	userText, hasUser := readHooksFile(userPath, "user", &diagnostics)
	allowProjectFromFile, userParseOK := parseAllowProjectHooks(userText)
	allowProject := envAllowsProjectHooks() || allowProjectFromFile
	if !userParseOK {
		diagnostics = append(diagnostics, fmt.Sprintf("hooks user: parse %s failed: allow_project_hooks must be bool", userPath))
	}
	if hasUser && userParseOK {
		userRules, userDiagnostics := ParseHooksFile(userText, "user")
		rules = append(rules, userRules...)
		diagnostics = append(diagnostics, userDiagnostics...)
	}
	if _, err := os.Stat(projectPath); err == nil {
		if allowProject {
			projectText, ok := readHooksFile(projectPath, "project", &diagnostics)
			if ok {
				projectRules, projectDiagnostics := ParseHooksFile(projectText, "project")
				rules = append(rules, projectRules...)
				diagnostics = append(diagnostics, projectDiagnostics...)
			}
		} else {
			diagnostics = append(diagnostics, fmt.Sprintf("project hooks ignored at %s; set allow_project_hooks = true in %s or PIE_ALLOW_PROJECT_HOOKS=1", projectPath, userPath))
		}
	}
	options.CWD = cwd
	options.Rules = append(append([]Rule(nil), options.Rules...), rules...)
	return LoadedHooks{Runner: NewRunner(options), Diagnostics: diagnostics}
}

func readHooksFile(path string, label string, diagnostics *[]string) (string, bool) {
	text, ok, readDiagnostics := ReadFile(path, label)
	*diagnostics = append(*diagnostics, readDiagnostics...)
	return text, ok
}

func ReadFile(path string, label string) (string, bool, []string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", false, []string{fmt.Sprintf("hooks %s: read %s failed: %v", label, path, err)}
		}
		return "", false, nil
	}
	if !utf8.Valid(data) {
		return "", false, []string{fmt.Sprintf("hooks %s: read %s failed: invalid UTF-8", label, path)}
	}
	return string(data), true, nil
}

func PushRules(rules *[]Rule, text string, source string) []string {
	parsed, diagnostics := ParseHooksFile(text, source)
	*rules = append(*rules, parsed...)
	return diagnostics
}

func envAllowsProjectHooks() bool {
	value := os.Getenv("PIE_ALLOW_PROJECT_HOOKS")
	return value == "1" || strings.EqualFold(value, "true")
}

func parseAllowProjectHooks(text string) (bool, bool) {
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || line == "[[hook]]" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "allow_project_hooks" {
			continue
		}
		if isQuotedHookValue(value) {
			return false, false
		}
		switch strings.TrimSpace(stripTOMLComment(value)) {
		case "true":
			return true, true
		case "false":
			return false, true
		default:
			return false, false
		}
	}
	return false, true
}

func NewRunner(options RunnerOptions) *Runner {
	runner := &Runner{sessionID: options.SessionID, cwd: options.CWD, modelProvider: options.ModelProvider, modelID: options.ModelID, thinkingLevel: options.ThinkingLevel, rules: append([]Rule(nil), options.Rules...), executor: options.Executor, httpClient: options.HTTPClient}
	if runner.thinkingLevel == "" {
		runner.thinkingLevel = "off"
	}
	if runner.executor == nil {
		runner.executor = runner.defaultExecute
	}
	if runner.httpClient == nil {
		runner.httpClient = http.DefaultClient
	}
	return runner
}

func (runner *Runner) IsEmpty() bool { return len(runner.rules) == 0 }
func (runner *Runner) Len() int      { return len(runner.rules) }

func (runner *Runner) HandleData(ctx context.Context, data EventData) error {
	for _, rule := range runner.rules {
		if !rule.Matches(data) {
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
		payload := runner.PayloadFor(rule, data)
		_ = runner.RunRule(ctx, rule, payload)
	}
	return nil
}

func (runner *Runner) RunRule(ctx context.Context, rule Rule, payload Payload) error {
	if runner == nil || runner.executor == nil {
		return nil
	}
	return runner.executor(ctx, rule, payload)
}

func (runner *Runner) HandleEvent(ctx context.Context, data EventData) error {
	return runner.HandleData(ctx, data)
}

func (runner *Runner) HandleHarnessEvent(ctx context.Context, data EventData) error {
	return runner.HandleData(ctx, data)
}

func (runner *Runner) Listener() agent.Listener {
	return func(event agent.Event) {
		if runner == nil {
			return
		}
		_ = runner.HandleEvent(context.Background(), EventDataFromAgentEvent(event))
	}
}

func (runner *Runner) HarnessListener() harness.HarnessListener {
	return func(event harness.HarnessEvent) {
		if runner == nil {
			return
		}
		data, ok := EventDataFromHarnessEvent(event)
		if !ok {
			return
		}
		_ = runner.HandleHarnessEvent(context.Background(), data)
	}
}

func EventDataFromAgentEvent(event agent.Event) EventData {
	data := EventData{}
	switch event.Type {
	case agent.EventTypeStart:
		data.Event = AgentStart
	case agent.EventTypeDone:
		data.Event = AgentEnd
	case agent.EventTypeTurnStart:
		data.Event = TurnStart
	case agent.EventTypeTurnEnd:
		data.Event = TurnEnd
	case agent.EventTypeMessageStart:
		data.Event = MessageStart
	case agent.EventTypeMessageUpdate:
		data.Event = MessageUpdate
	case agent.EventTypeMessageEnd:
		data.Event = MessageEnd
	case agent.EventTypeToolExecutionStart:
		data.Event = ToolStart
	case agent.EventTypeToolUpdate:
		data.Event = ToolUpdate
	case agent.EventTypeToolExecutionEnd:
		data.Event = ToolEnd
	default:
		return data
	}
	if event.Message != nil {
		data.MessageKind = messageKind(*event.Message)
		data.MessageKindPresent = true
		data.MessageSummary = messageSummary(*event.Message)
		data.MessageSummaryPresent = true
	}
	if event.AssistantMessageEvent != nil {
		data.AssistantEvent = assistantEventName(*event.AssistantMessageEvent)
		data.AssistantEventPresent = true
	}
	if event.ToolCall != nil {
		data.ToolCallID = event.ToolCall.ID
		data.ToolCallIDPresent = true
		data.ToolName = event.ToolCall.Name
		data.ToolNamePresent = true
	}
	if event.ToolArgs != nil {
		data.ToolArgs = event.ToolArgs
	}
	if event.ToolResult != nil {
		data.ToolResultSummary = toolResultSummary(*event.ToolResult)
		data.ToolResultSummaryPresent = true
		data.ToolIsError = &event.IsError
	}
	return data
}

func FromAgentEvent(event agent.Event) (EventData, bool) {
	data := EventDataFromAgentEvent(event)
	if data.Event == "" {
		return EventData{}, false
	}
	return data, true
}

func toolResultSummary(result agent.ToolResult) string {
	if len(result.ContentBlocks) > 0 {
		return truncateHookText(contentSummary(result.ContentBlocks))
	}
	return truncateHookText(result.Content)
}

func ResultSummary(result agent.ToolResult) string {
	return toolResultSummary(result)
}

func messageKind(message agent.Message) string {
	switch message.Kind {
	case agent.MessageKindLLM:
		if message.LLM != nil {
			switch message.LLM.Role {
			case ai.RoleUser:
				return "user"
			case ai.RoleAssistant:
				return "assistant"
			case ai.RoleTool:
				return "tool_result"
			}
		}
	case agent.MessageKindToolResult:
		return "tool_result"
	case agent.MessageKindCustom:
		if message.Custom != nil {
			return message.Custom.Role
		}
	}
	return string(message.Kind)
}

func assistantEventName(event ai.AssistantMessageEvent) string {
	return string(event.Type)
}

func messageSummary(message agent.Message) string {
	switch message.Kind {
	case agent.MessageKindLLM:
		if message.LLM != nil {
			return truncateHookText(contentSummary(message.LLM.Content))
		}
	case agent.MessageKindToolResult:
		if message.ToolResult != nil {
			if len(message.ToolResult.ContentBlocks) > 0 {
				return truncateHookText(contentSummary(message.ToolResult.ContentBlocks))
			}
			return truncateHookText(message.ToolResult.Content)
		}
	case agent.MessageKindCustom:
		if message.Custom != nil {
			data, _ := marshalJSONNoHTMLEscape(message.Custom.Payload)
			return truncateHookText(string(data))
		}
	}
	return ""
}

func contentSummary(blocks []ai.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ai.ContentText:
			parts = append(parts, block.Text)
		case ai.ContentThinking:
			parts = append(parts, "<thinking>")
		case ai.ContentToolCall:
			name := ""
			if block.ToolCall != nil {
				name = block.ToolCall.Name
			}
			parts = append(parts, "<tool_call "+name+">")
		case ai.ContentImage:
			parts = append(parts, "<image "+block.MimeType+">")
		}
	}
	return strings.Join(parts, "\n")
}

func truncateHookText(text string) string {
	runes := []rune(text)
	if len(runes) <= hookSummaryMaxChars {
		return text
	}
	return string(runes[:hookSummaryMaxChars]) + "…"
}

func Truncate(text string) string {
	return truncateHookText(text)
}

func EventDataFromHarnessEvent(event harness.HarnessEvent) (EventData, bool) {
	if event.Kind != harness.HarnessEventCompaction {
		return EventData{}, false
	}
	trigger := "auto"
	if event.FromHook {
		trigger = "manual"
	}
	tokensBefore := event.TokensBefore
	return EventData{Event: Compaction, CompactionTrigger: trigger, CompactionTriggerPresent: true, CompactionTokensBefore: &tokensBefore, CompactionSummary: truncateHookText(event.Summary), CompactionSummaryPresent: true}, true
}

func FromHarnessEvent(event harness.HarnessEvent) (EventData, bool) {
	return EventDataFromHarnessEvent(event)
}

func CompactionTrigger(fromHook bool) string {
	if fromHook {
		return "manual"
	}
	return "auto"
}

func (runner *Runner) PayloadFor(rule Rule, data EventData) Payload {
	toolResultSummaryPresent := data.ToolResultSummaryPresent || data.ToolResultSummary != "" || data.Event == ToolUpdate || data.Event == ToolEnd
	compactionSummaryPresent := data.CompactionSummaryPresent || data.CompactionSummary != "" || data.Event == Compaction
	toolInfoPresent := data.Event == ToolStart || data.Event == ToolUpdate || data.Event == ToolEnd
	return Payload{Event: data.Event.String(), SessionID: runner.sessionID, CWD: runner.cwd, ModelProvider: runner.modelProvider, ModelID: runner.modelID, ThinkingLevel: runner.thinkingLevel, Source: rule.Source, MessageKind: data.MessageKind, MessageKindPresent: data.MessageKindPresent || data.MessageKind != "" || data.Event == MessageStart || data.Event == MessageUpdate || data.Event == MessageEnd, MessageSummary: data.MessageSummary, MessageSummaryPresent: data.MessageSummaryPresent || data.MessageSummary != "", AssistantEvent: data.AssistantEvent, AssistantEventPresent: data.AssistantEventPresent || data.AssistantEvent != "" || data.Event == MessageStart || data.Event == MessageUpdate || data.Event == MessageEnd, ToolCallID: data.ToolCallID, ToolCallIDPresent: data.ToolCallIDPresent || data.ToolCallID != "" || toolInfoPresent, ToolName: data.ToolName, ToolNamePresent: data.ToolNamePresent || data.ToolName != "" || toolInfoPresent, ToolIsError: data.ToolIsError, ToolArgs: data.ToolArgs, ToolResultSummary: data.ToolResultSummary, ToolResultSummaryPresent: toolResultSummaryPresent, CompactionTrigger: data.CompactionTrigger, CompactionTriggerPresent: data.CompactionTriggerPresent || data.CompactionTrigger != "" || data.Event == Compaction, CompactionTokensBefore: data.CompactionTokensBefore, CompactionSummary: data.CompactionSummary, CompactionSummaryPresent: compactionSummaryPresent}
}

func (runner *Runner) defaultExecute(ctx context.Context, rule Rule, payload Payload) error {
	if rule.Command != "" {
		if err := runCommand(ctx, rule, payload, runner.cwd); err != nil {
			return err
		}
	}
	if rule.WebhookPresent || rule.Webhook != "" {
		return runner.runWebhook(ctx, rule, payload)
	}
	return nil
}

func runCommand(ctx context.Context, rule Rule, payload Payload, cwd string) error {
	timeout := rule.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payloadJSON, err := MarshalPayload(payload)
	if err != nil {
		return err
	}
	payloadPath, err := writePayloadFile(payloadJSON)
	if err != nil {
		return err
	}
	defer os.Remove(payloadPath)
	shell, flag := shellCommand()
	cmd := exec.Command(shell, flag, rule.Command)
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	cmd.Dir = cwdForRule(rule, cwd)
	cmd.Env = append(cmd.Environ(), envForPayload(payload, payloadPath)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("hook command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("command exited %d: %s", exitErr.ExitCode(), strings.TrimSpace(stderr.String()))
			}
			return fmt.Errorf("hook command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
	case <-runCtx.Done():
		terminateHookTree(cmd.Process)
		<-done
		if runCtx.Err() == context.Canceled {
			return fmt.Errorf("cancelled")
		}
		if runCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timed out after %dms", timeout.Milliseconds())
		}
	}
	return nil
}

func terminateHookTree(process *os.Process) {
	if runtime.GOOS == "windows" || process == nil {
		return
	}
	_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
}

func writePayloadFile(payloadJSON []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "pie-hooks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.Write(payloadJSON); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func envForPayload(payload Payload, payloadPath string) []string {
	env := []string{
		"PIE_HOOK_EVENT=" + payload.Event,
		"PIE_HOOK_PAYLOAD=" + payloadPath,
		"PIE_SESSION_ID=" + payload.SessionID,
		"PIE_CWD=" + payload.CWD,
		"PIE_MODEL_PROVIDER=" + payload.ModelProvider,
		"PIE_MODEL_ID=" + payload.ModelID,
		"PIE_THINKING_LEVEL=" + payload.ThinkingLevel,
	}
	if payload.MessageKindPresent {
		env = append(env, "PIE_MESSAGE_KIND="+payload.MessageKind)
	}
	if payload.AssistantEventPresent {
		env = append(env, "PIE_ASSISTANT_EVENT="+payload.AssistantEvent)
	}
	if payload.ToolCallIDPresent {
		env = append(env, "PIE_TOOL_CALL_ID="+payload.ToolCallID)
	}
	if payload.ToolNamePresent {
		env = append(env, "PIE_TOOL_NAME="+payload.ToolName)
	}
	if payload.ToolIsError != nil {
		env = append(env, fmt.Sprintf("PIE_TOOL_IS_ERROR=%t", *payload.ToolIsError))
	}
	if payload.CompactionTriggerPresent {
		env = append(env, "PIE_COMPACTION_TRIGGER="+payload.CompactionTrigger)
	}
	if payload.CompactionTokensBefore != nil {
		env = append(env, fmt.Sprintf("PIE_COMPACTION_TOKENS_BEFORE=%d", *payload.CompactionTokensBefore))
	}
	return env
}

func EnvFor(payload Payload, payloadPath string) map[string]string {
	env := map[string]string{}
	for _, entry := range envForPayload(payload, payloadPath) {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

func cwdForRule(rule Rule, projectCWD string) string {
	switch rule.CWD {
	case CWDPie:
		return config.BaseDir()
	case CWDHome:
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home
		}
	}
	return projectCWD
}

func CWDFor(rule Rule, projectCWD string) string {
	return cwdForRule(rule, projectCWD)
}

func (runner *Runner) runWebhook(ctx context.Context, rule Rule, payload Payload) error {
	payloadJSON, err := MarshalPayload(payload)
	if err != nil {
		return err
	}
	timeout := rule.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(runCtx, http.MethodPost, rule.Webhook, bytes.NewReader(payloadJSON))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range rule.Headers {
		request.Header.Set(key, value)
	}
	response, err := runner.httpClient.Do(request)
	if err != nil {
		if runCtx.Err() == context.Canceled {
			return fmt.Errorf("cancelled")
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("webhook status %s: %s", response.Status, firstRunes(string(body), 500))
	}
	return nil
}

func firstRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func shellCommand() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	return "/bin/sh", "-c"
}

func ShellProgram() string {
	program, _ := shellCommand()
	return program
}

func ShellArg() string {
	_, arg := shellCommand()
	return arg
}
