package commands

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/triggers"
)

func TestTriggersCommandShowsRulesAndStatus(t *testing.T) {
	registry := DefaultRegistry()
	firedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rules := []triggers.DynamicRule{
		{ID: "dyn-1", Condition: strings.Repeat("c", 90), Action: "summarize", Enabled: true, FireOnce: true, PromoteToChat: true, FiredAt: &firedAt},
		{ID: "dyn-2", Condition: "file changed", Action: strings.Repeat("a", 90), Enabled: false, FireOnce: false},
	}
	ctx := Context{
		TriggerRules: rules,
		TriggerSources: []TriggerSourceEntry{
			{State: "connected", SubscriptionLabels: []string{"dynamic trigger periodic check"}},
			{State: "disconnected", Reason: "protocol_mismatch", RequiresAttention: "upgrade hub"},
		},
		TriggerRuntime:     triggers.TriggerRuntimeSnapshot{AcceptedTotal: 7, DedupedTotal: 8, CycleSuppressedTotal: 9, ActiveTraces: 6, DedupEntries: 5},
		RunningTriggers:    []RunningTriggerEntry{{TraceID: "trace-1"}},
		TriggerStoragePath: "/tmp/pie/triggers.json",
	}
	listed := Dispatch(context.Background(), "/triggers rules extra", registry, ctx)
	if listed.Kind != OutcomeHandled || !strings.Contains(listed.Message, "Dynamic trigger rules (2):") || !strings.Contains(listed.Message, "dyn-1 [enabled, fire_once, promote_to_chat, fired_at=2026-01-02T03:04:05Z]") || !strings.Contains(listed.Message, strings.Repeat("c", 80)+"…") || !strings.Contains(listed.Message, "dyn-2 [disabled, repeat, audit_only]") || !strings.Contains(listed.Message, strings.Repeat("a", 80)+"…") {
		t.Fatalf("rules mismatch: %#v", listed)
	}
	status := Dispatch(context.Background(), "/triggers status extra", registry, ctx)
	if status.Kind != OutcomeHandled || !strings.Contains(status.Message, "Trigger status:") || !strings.Contains(status.Message, "dynamic rules: 2 total, 1 enabled, 1 disabled (1 fire_once, 1 repeat, 1 promote_to_chat)") || !strings.Contains(status.Message, "local dynamic checker: 1 registered, polls every 600s while enabled rules exist") || !strings.Contains(status.Message, "push trigger sources: 1 configured source(s) feed server-pushed events into the same trigger runtime") || !strings.Contains(status.Message, "storage: /tmp/pie/triggers.json") || !strings.Contains(status.Message, "output: default is TUI + audit only; rules marked promote_to_chat also enter the main chat context") || !strings.Contains(status.Message, "engine: accepted=7 deduped=8 cycle_suppressed=9 recent_traces=6 dedup_entries=5 running=1") || !strings.Contains(status.Message, "sources: 2 total, 1 connected, 1 require attention") || !strings.Contains(status.Message, "commands: /triggers rules | /triggers sources | /triggers disable <id> | /triggers enable <id> | /triggers remove <id> | /triggers audit") {
		t.Fatalf("status mismatch: %#v", status)
	}

	if RenderDynamicTriggerRules(rules, len(rules)) != listed.Message {
		t.Fatalf("exported rules renderer should match /triggers rules")
	}
	if RenderTriggerStatus(ctx) != status.Message {
		t.Fatalf("exported status renderer should match /triggers status")
	}
	if RenderTriggersStatus(ctx) != status.Message {
		t.Fatalf("upstream-named exported status renderer should match /triggers status")
	}
}

func TestTriggersCommandReturnsMutationOutcomes(t *testing.T) {
	registry := DefaultRegistry()
	enable := Dispatch(context.Background(), "/triggers enable dyn-1 extra", registry, Context{})
	if enable.Kind != OutcomeSetTriggerRuleEnabled || enable.TargetID == nil || *enable.TargetID != "dyn-1" || !enable.Enabled || enable.Message != "enabled trigger dyn-1" {
		t.Fatalf("enable mismatch: %#v", enable)
	}
	disable := Dispatch(context.Background(), "/triggers pause dyn-1 extra", registry, Context{})
	if disable.Kind != OutcomeSetTriggerRuleEnabled || disable.TargetID == nil || *disable.TargetID != "dyn-1" || disable.Enabled || disable.Message != "disabled trigger dyn-1" {
		t.Fatalf("disable mismatch: %#v", disable)
	}
	remove := Dispatch(context.Background(), "/triggers remove dyn-1 extra", registry, Context{})
	if remove.Kind != OutcomeRemoveTriggerRule || remove.TargetID == nil || *remove.TargetID != "dyn-1" || remove.RemoveAll {
		t.Fatalf("remove mismatch: %#v", remove)
	}
	clear := Dispatch(context.Background(), "/triggers rm --all", registry, Context{})
	if clear.Kind != OutcomeRemoveTriggerRule || !clear.RemoveAll {
		t.Fatalf("clear mismatch: %#v", clear)
	}
}

func TestTriggersCommandUsageErrors(t *testing.T) {
	registry := DefaultRegistry()
	cases := map[string]string{
		"/triggers nope":   "unknown /triggers command: nope. usage: /triggers [status|rules|sources|enable <id>|disable <id>|remove <id>|remove --all|running|audit [N]|abort <trace_id>|abort --all]",
		"/triggers enable": "usage: /triggers enable <id>",
		"/triggers remove": "usage: /triggers remove <id>|--all",
	}
	for input, want := range cases {
		out := Dispatch(context.Background(), input, registry, Context{})
		if out.Kind != OutcomeError || out.Message != want {
			t.Fatalf("%s mismatch: %#v", input, out)
		}
	}
}

func TestTriggersCommandHelpUsageMatchesUpstream(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/help triggers", registry, Context{})
	if out.Kind != OutcomeHandled || !strings.Contains(out.Message, "/triggers [status|rules|sources|enable <id>|disable <id>|remove <id>|remove --all|running|audit [N]|abort <trace_id>|abort --all]") {
		t.Fatalf("triggers help mismatch: %#v", out)
	}
}
