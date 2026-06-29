package triggers

type BeforeTriggerActionContext struct {
	Trigger Trigger
}

type BeforeTriggerActionHook func(BeforeTriggerActionContext) CronAction

type HarnessEvent struct {
	TraceID string
	Summary string
	Error   string
}

type HarnessListenerResult struct {
	DynamicRules []DynamicRule
	Cron         StatefulCronCompletionResult
}

type HarnessListener func(HarnessEvent) HarnessListenerResult

func DirectInjectActionHook(injectSummaryServers, injectAndRunServers map[string]bool, inner BeforeTriggerActionHook) BeforeTriggerActionHook {
	return func(ctx BeforeTriggerActionContext) CronAction {
		if inner == nil {
			return DirectInjectDynamicTriggerAction(nil, ctx.Trigger, injectSummaryServers, injectAndRunServers)
		}
		server := ""
		if ctx.Trigger.Source.Kind == SourceMCP {
			server = ctx.Trigger.Source.ServerName
		}
		if server != "" && (injectSummaryServers[server] || injectAndRunServers[server]) {
			return DirectInjectDynamicTriggerAction(nil, ctx.Trigger, injectSummaryServers, injectAndRunServers)
		}
		return inner(ctx)
	}
}

func BeforeTriggerActionHookForDynamicRegistry(registry *DynamicRegistry) BeforeTriggerActionHook {
	return func(ctx BeforeTriggerActionContext) CronAction {
		return DynamicTriggerAction(registry, ctx.Trigger)
	}
}

func before_trigger_action_hook(registry *DynamicRegistry) BeforeTriggerActionHook {
	return BeforeTriggerActionHookForDynamicRegistry(registry)
}

func direct_inject_action_hook(injectSummaryServers, injectAndRunServers map[string]bool, inner BeforeTriggerActionHook) BeforeTriggerActionHook {
	return DirectInjectActionHook(injectSummaryServers, injectAndRunServers, inner)
}

func FireOnceHarnessListener(registry *DynamicRegistry) HarnessListener {
	return func(event HarnessEvent) HarnessListenerResult {
		changed, _ := HandleDynamicTriggerCompletion(registry, event.Summary)
		return HarnessListenerResult{DynamicRules: changed}
	}
}

func fire_once_harness_listener(registry *DynamicRegistry) HarnessListener {
	return FireOnceHarnessListener(registry)
}

func CronActionHook(registry *CronRegistry, inner BeforeTriggerActionHook) BeforeTriggerActionHook {
	return func(ctx BeforeTriggerActionContext) CronAction {
		if payload, ok := ctx.Trigger.Payload.(ScheduledCronPayload); ok {
			if action := CronTriggerAction(registry, payload); action.Prompt != "" || action.Delivery != "" {
				return action
			}
		}
		if inner == nil {
			return CronAction{}
		}
		return inner(ctx)
	}
}

func cron_action_hook(registry *CronRegistry, inner BeforeTriggerActionHook) BeforeTriggerActionHook {
	return CronActionHook(registry, inner)
}

func CronHarnessListener(registry *CronRegistry, cronSidecarPath string, inboxPath string) HarnessListener {
	return func(event HarnessEvent) HarnessListenerResult {
		result, _ := HandleStatefulCronCompletion(registry, StatefulCronCompletionOptions{CronSidecarPath: cronSidecarPath, InboxPath: inboxPath, TraceID: event.TraceID, Summary: event.Summary, Error: event.Error})
		return HarnessListenerResult{Cron: result}
	}
}

func cron_harness_listener(registry *CronRegistry, cronSidecarPath string, inboxPath string) HarnessListener {
	return CronHarnessListener(registry, cronSidecarPath, inboxPath)
}
