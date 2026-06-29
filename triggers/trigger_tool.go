package triggers

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type NewTriggerTool struct {
	Registry *DynamicRegistry
}

type ListTriggersTool struct {
	Registry *DynamicRegistry
}

type RemoveTriggerTool struct {
	Registry *DynamicRegistry
}

type SetTriggerStateTool struct {
	Registry *DynamicRegistry
}

func (NewTriggerTool) Name() string { return "NewTrigger" }
func (NewTriggerTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (NewTriggerTool) PermissionClassification(arguments map[string]any) agent.PermissionClassification {
	return agent.PermissionAsk
}
func (NewTriggerTool) PermissionReason(arguments map[string]any) string {
	hasCondition := strings.TrimSpace(optionalStringArg(ai.ToolCall{Arguments: arguments}, "condition", "")) != ""
	hasAction := strings.TrimSpace(optionalStringArg(ai.ToolCall{Arguments: arguments}, "action", "")) != ""
	hasSpec := strings.TrimSpace(optionalStringArg(ai.ToolCall{Arguments: arguments}, "spec", "")) != ""
	if hasCondition && hasAction {
		return "create dynamic trigger from `condition` + `action` fields"
	}
	if hasSpec {
		return "create dynamic trigger from `spec` field"
	}
	return "create dynamic trigger"
}
func (NewTriggerTool) Description() string {
	return "Create an event/condition-based dynamic trigger rule. Use this for future events such as a browser tab, file, MCP notification, webhook, or other condition becoming true. Do not use this for fixed time, recurring, scheduled, hourly, daily, weekly, cron, crontab, 定时任务, 每小时, or similar time-based jobs; use NewCronJob instead."
}
func (NewTriggerTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"condition":       map[string]any{"type": "string", "description": "The natural-language condition that should be evaluated against future trigger events."},
			"action":          map[string]any{"type": "string", "description": "The action to perform when the condition matches. This may be a shell command or a natural-language instruction."},
			"spec":            map[string]any{"type": "string", "description": "Fallback complete trigger rule text when condition and action cannot be supplied separately."},
			"fire_once":       map[string]any{"type": "boolean", "description": "Whether to disable the rule after the first successful match. Defaults to true unless the user explicitly asks for a repeating trigger."},
			"promote_to_chat": map[string]any{"type": "boolean", "description": "Whether successful trigger output should be inserted into the parent chat context so future turns can see it. Defaults to false unless the user explicitly asks for that behavior."},
		},
		"required":             []string{"condition", "action"},
		"additionalProperties": false,
	}
}
func (tool NewTriggerTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	condition := optionalStringArg(call, "condition", "")
	action := optionalStringArg(call, "action", "")
	spec := optionalStringArg(call, "spec", "")
	if !utf8.ValidString(condition) {
		return agent.ToolResult{}, fmt.Errorf("condition must be valid UTF-8")
	}
	if !utf8.ValidString(action) {
		return agent.ToolResult{}, fmt.Errorf("action must be valid UTF-8")
	}
	if !utf8.ValidString(spec) {
		return agent.ToolResult{}, fmt.Errorf("spec must be valid UTF-8")
	}
	if looksLikeFixedScheduleRequest(condition) || looksLikeFixedScheduleRequest(action) || looksLikeFixedScheduleRequest(spec) {
		return agent.ToolResult{}, fmt.Errorf("fixed scheduled jobs must use NewCronJob, not NewTrigger")
	}
	fireOnce := boolArgDefault(call, "fire_once", true)
	promoteToChat := boolArgDefault(call, "promote_to_chat", false)
	registry := tool.Registry
	if registry == nil {
		registry = NewDynamicRegistry()
	}
	var rule DynamicRule
	var err error
	_, hasCondition := call.Arguments["condition"].(string)
	_, hasAction := call.Arguments["action"].(string)
	if hasCondition && hasAction {
		rule, err = registry.AddRuleWithFlags(condition, action, fireOnce, promoteToChat)
	} else {
		if strings.TrimSpace(spec) == "" {
			return agent.ToolResult{}, fmt.Errorf("missing required args: provide condition and action")
		}
		rule, err = registry.AddFromSpec(spec)
	}
	if err != nil {
		return agent.ToolResult{}, err
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("created dynamic trigger %s\ncondition: %s\naction: %s\nfire_once: %t\npromote_to_chat: %t", rule.ID, rule.Condition, rule.Action, rule.FireOnce, rule.PromoteToChat), Details: map[string]any{"id": rule.ID, "condition": rule.Condition, "action": rule.Action, "enabled": rule.Enabled, "fire_once": rule.FireOnce, "fired_at": rule.FiredAt, "promote_to_chat": rule.PromoteToChat}}, nil
}

func looksLikeFixedScheduleRequest(text string) bool {
	lower := strings.ToLower(text)
	needles := []string{"every hour", "hourly", "every day", "daily", "every week", "weekly", "scheduled job", "cron", "crontab"}
	for _, needle := range needles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return strings.Contains(text, "定时任务") || strings.Contains(text, "定時任務") || strings.Contains(text, "每小时") || strings.Contains(text, "每小時") || strings.Contains(text, "每天") || strings.Contains(text, "每日") || strings.Contains(text, "每周") || strings.Contains(text, "每週")
}

func (ListTriggersTool) Name() string { return "ListTriggers" }
func (ListTriggersTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (ListTriggersTool) Description() string {
	return "List dynamic trigger rules currently registered in pie. Use this when the user asks to view, list, show, inspect, or find trigger ids."
}
func (ListTriggersTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
}
func (tool ListTriggersTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	registry := triggerRegistry(tool.Registry)
	rules := registry.List()
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: renderTriggerRulesForTool(rules), Details: map[string]any{"count": len(rules), "rules": rules, "storage_path": registry.StoragePath()}}, nil
}

func (RemoveTriggerTool) Name() string { return "RemoveTrigger" }
func (RemoveTriggerTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (RemoveTriggerTool) PermissionClassification(arguments map[string]any) agent.PermissionClassification {
	return agent.PermissionAsk
}
func (RemoveTriggerTool) PermissionReason(arguments map[string]any) string {
	if boolArgDefault(ai.ToolCall{Arguments: arguments}, "all", false) {
		return "remove ALL dynamic triggers"
	}
	if id := optionalStringArg(ai.ToolCall{Arguments: arguments}, "id", ""); id != "" {
		return fmt.Sprintf("remove dynamic trigger `%s`", id)
	}
	return "remove dynamic trigger"
}
func (RemoveTriggerTool) Description() string {
	return "Delete dynamic trigger rules. Use this when the user asks pie to delete, remove, or clear an existing dynamic trigger."
}
func (RemoveTriggerTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string", "description": "The exact dynamic trigger rule id to remove."}, "all": map[string]any{"type": "boolean", "description": "Set true only when the user explicitly asks to remove all dynamic trigger rules."}}, "additionalProperties": false}
}
func (tool RemoveTriggerTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	registry := triggerRegistry(tool.Registry)
	if boolArgDefault(call, "all", false) {
		count, err := registry.ClearRules()
		if err != nil {
			return agent.ToolResult{}, err
		}
		return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("removed %d dynamic trigger rule(s)", count), Details: map[string]any{"removed_count": count, "all": true}}, nil
	}
	id, err := stringArg(call, "id")
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("missing required arg: id")
	}
	if !utf8.ValidString(id) {
		return agent.ToolResult{}, fmt.Errorf("id must be valid UTF-8")
	}
	removed, err := registry.RemoveRule(id)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if removed == nil {
		return agent.ToolResult{}, fmt.Errorf("no dynamic trigger rule with id '%s'", id)
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("removed dynamic trigger %s\ncondition: %s\naction: %s", removed.ID, removed.Condition, removed.Action), Details: map[string]any{"id": removed.ID, "condition": removed.Condition, "action": removed.Action, "removed_count": 1}}, nil
}

func (SetTriggerStateTool) Name() string { return "SetTriggerState" }
func (SetTriggerStateTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (SetTriggerStateTool) PermissionClassification(arguments map[string]any) agent.PermissionClassification {
	enabled, ok := arguments["enabled"].(bool)
	if !ok || !enabled {
		return agent.PermissionAllow
	}
	return agent.PermissionAsk
}
func (SetTriggerStateTool) PermissionReason(arguments map[string]any) string {
	enabled, ok := arguments["enabled"].(bool)
	if !ok || !enabled {
		return ""
	}
	id, ok := arguments["id"].(string)
	if !ok {
		id = "<unknown>"
	}
	return fmt.Sprintf("re-enable dynamic trigger `%s`", id)
}
func (SetTriggerStateTool) Description() string {
	return "Enable or disable an existing dynamic trigger rule without deleting it. Use this when the user asks to pause, disable, enable, or resume a trigger."
}
func (SetTriggerStateTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string", "description": "The exact dynamic trigger rule id to update."}, "enabled": map[string]any{"type": "boolean", "description": "Set false to pause or disable the trigger; set true to enable or resume it."}}, "required": []string{"id", "enabled"}, "additionalProperties": false}
}
func (tool SetTriggerStateTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	registry := triggerRegistry(tool.Registry)
	id, err := stringArg(call, "id")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(id) {
		return agent.ToolResult{}, fmt.Errorf("id must be valid UTF-8")
	}
	enabled, err := boolArg(call, "enabled")
	if err != nil {
		return agent.ToolResult{}, err
	}
	rule, err := registry.SetRuleEnabled(id, enabled)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if rule == nil {
		return agent.ToolResult{}, fmt.Errorf("no dynamic trigger rule with id '%s'", id)
	}
	state := "disabled"
	if rule.Enabled {
		state = "enabled"
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("updated dynamic trigger %s\nstate: %s\ncondition: %s\naction: %s", rule.ID, state, rule.Condition, rule.Action), Details: map[string]any{"id": rule.ID, "condition": rule.Condition, "action": rule.Action, "enabled": rule.Enabled, "fire_once": rule.FireOnce, "fired_at": rule.FiredAt, "promote_to_chat": rule.PromoteToChat}}, nil
}

func triggerRegistry(registry *DynamicRegistry) *DynamicRegistry {
	if registry != nil {
		return registry
	}
	return NewDynamicRegistry()
}

func findTriggerRule(rules []DynamicRule, id string) (DynamicRule, bool) {
	for _, rule := range rules {
		if rule.ID == id {
			return rule, true
		}
	}
	return DynamicRule{}, false
}

func renderTriggerRulesForTool(rules []DynamicRule) string {
	if len(rules) == 0 {
		return "dynamic trigger rules: none"
	}
	lines := []string{fmt.Sprintf("dynamic trigger rules: %d", len(rules))}
	for _, rule := range rules {
		state := "disabled"
		if rule.Enabled {
			state = "enabled"
		}
		fireMode := "repeat"
		if rule.FireOnce {
			fireMode = "fire_once"
		}
		outputMode := "audit_only"
		if rule.PromoteToChat {
			outputMode = "promote_to_chat"
		}
		lines = append(lines, fmt.Sprintf("- %s [%s, %s, %s] created_at=%s condition: %s action: %s", rule.ID, state, fireMode, outputMode, rule.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), rule.Condition, rule.Action))
	}
	return strings.Join(lines, "\n")
}
