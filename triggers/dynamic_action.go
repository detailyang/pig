package triggers

import "fmt"

func DynamicTriggerAction(registry *DynamicRegistry, trigger Trigger) CronAction {
	if registry == nil {
		return CronAction{}
	}
	var enabled []DynamicRule
	for _, rule := range registry.List() {
		if rule.Enabled {
			enabled = append(enabled, rule)
		}
	}
	if len(enabled) == 0 {
		return DefaultTriggerAction(trigger)
	}
	promoteIDs := dynamicPromoteRuleIDs(enabled)
	promote := PromoteNone
	if len(promoteIDs) > 0 {
		promote = PromoteSummaryWhenSummaryContains
	}
	return CronAction{Prompt: RenderDynamicTriggerPrompt(trigger, enabled), Promote: promote, PromoteRequiredSubstrings: promoteIDs, PromoteRequiresApproval: false, Delivery: TriggerDeliverySubAgent}
}

func DefaultTriggerAction(trigger Trigger) CronAction {
	return CronAction{Prompt: trigger.SourceLabel + " fired: " + trigger.EventLabel, Promote: PromoteNone, Delivery: TriggerDeliverySubAgent}
}

func dynamicPromoteRuleIDs(rules []DynamicRule) []string {
	var ids []string
	for _, rule := range rules {
		if rule.PromoteToChat {
			ids = append(ids, rule.ID)
		}
	}
	return ids
}

func DynamicSummaryOnlyAction(trigger Trigger) CronAction {
	action := CronAction{Delivery: TriggerDeliveryInjectSummary, Promote: PromoteNone}
	if trigger.PayloadSummary != nil {
		action.Promote = PromoteSummaryNow
		action.PromoteTemplateBody = "{{trigger.payload_summary}}"
	}
	return action
}

func DirectInjectDynamicTriggerAction(registry *DynamicRegistry, trigger Trigger, injectSummaryServers, injectAndRunServers map[string]bool) CronAction {
	server := ""
	if trigger.Source.Kind == SourceMCP {
		server = trigger.Source.ServerName
	}
	if server != "" && injectAndRunServers[server] {
		prompt := trigger.SourceLabel + " fired: " + trigger.EventLabel
		if trigger.PayloadSummary != nil {
			prompt = *trigger.PayloadSummary
		}
		return CronAction{Prompt: prompt, Promote: PromoteNone, Delivery: TriggerDeliveryInjectAndRun}
	}
	if server != "" && injectSummaryServers[server] {
		return DynamicSummaryOnlyAction(trigger)
	}
	return DynamicTriggerAction(registry, trigger)
}

func RenderDynamicTriggerPrompt(trigger Trigger, rules []DynamicRule) string {
	rulesJSON := mustPrettyJSON(rules, "[]")
	payload := any(nil)
	if trigger.PayloadVisibility == PayloadShared {
		payload = trigger.Payload
	}
	triggerJSON := mustPrettyJSON(map[string]any{
		"source_kind":        trigger.SourceKind,
		"source":             trigger.Source,
		"source_label":       trigger.SourceLabel,
		"event_label":        trigger.EventLabel,
		"payload_visibility": trigger.PayloadVisibility,
		"payload_summary":    trigger.PayloadSummary,
		"payload":            payload,
		"received_at":        trigger.ReceivedAt,
		"idempotency_key":    trigger.IDempotencyKey,
		"trace_id":           trigger.TraceID,
		"authority": map[string]any{
			"principal_id":     trigger.Authority.PrincipalID,
			"principal_label":  trigger.Authority.PrincipalLabel,
			"credential_scope": trigger.Authority.CredentialScope,
		},
	}, "{}")
	return fmt.Sprintf("A trigger check event arrived.\n\nEvent:\n%s\n\nDynamic trigger rules:\n%s\n\nEvaluate each rule's natural-language condition. For source-specific events, compare the rule against the event. For `local:dynamic` periodic checks, inspect current local or remote state with the available tools whenever the condition depends on filesystem state, paths, environment variables, shell expansion, command output, clock time, network/API state, or any fact not already present in the Event JSON. Do not report no match for those conditions until after the needed inspection. If no enabled rule matches after any required inspection, reply with exactly: no dynamic trigger rule matched.\n\nIf one or more rules match, execute each matching rule's action. Treat the action as an instruction from the user. If it asks to read or print a file, use the read tool or a safe shell command, then include the requested file contents in your final response. If it asks to run a local program or shell command, use the bash tool. Keep the final response concise and include the exact matched rule id(s), for example `matched dyn-...`.", triggerJSON, rulesJSON)
}

func mustPrettyJSON(value any, fallback string) string {
	data, err := marshalJSONIndentNoHTMLEscape(value, "", "  ")
	if err != nil {
		return fallback
	}
	return string(data)
}
