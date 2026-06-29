package commands

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/session"
)

func TestTriggersCommandShowsRunningTriggers(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{RunningTriggers: []RunningTriggerEntry{{TraceID: "trace-1", SourceLabel: "mcp:github", EventLabel: "pr_merged", StartedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), PromptPreview: strings.Repeat("p", 130)}}}
	out := Dispatch(context.Background(), "/triggers running extra", registry, ctx)
	if out.Kind != OutcomeHandled || !strings.Contains(out.Message, "Running triggers (1):") || !strings.Contains(out.Message, "trace-1  mcp:github / pr_merged  since 2026-01-02T03:04:05Z") || !strings.Contains(out.Message, strings.Repeat("p", 120)+"…") {
		t.Fatalf("running mismatch: %#v", out)
	}
	empty := Dispatch(context.Background(), "/triggers running", registry, Context{})
	if empty.Kind != OutcomeHandled || empty.Message != "(no running triggers)" {
		t.Fatalf("empty mismatch: %#v", empty)
	}
}

func TestTriggersCommandShowsAuditRows(t *testing.T) {
	registry := DefaultRegistry()
	entries := []session.Entry{
		customEntry("old", "trigger", map[string]any{"trace_id": "trace-old", "state": "accepted", "source_label": "local", "event_label": "old", "payload_summary": "old summary"}),
		customEntry("decision", "trigger", map[string]any{"trace_id": "trace-decision", "state": "permission_denied", "source_label": "mcp:github", "event_label": "pr_merged", "payload_summary": "safe summary", "evaluator_decision": map[string]any{"outcome": "accept", "permission": "deny", "reason": "policy says no", "raw_payload": "must-not-render"}, "payload": map[string]any{"secret": "must-not-render"}}),
		customEntry("new", "trigger_result", map[string]any{"trace_id": "trace-new", "success": false, "summary": strings.Repeat("s", 170), "payload": "must-not-leak"}),
		customEntry("promo", "trigger_promotion", map[string]any{"trace_id": "trace-promo", "state": "promoted", "redaction_status": "redacted"}),
	}
	out := Dispatch(context.Background(), "/triggers audit 3 extra", registry, Context{Branch: entries})
	if out.Kind != OutcomeHandled || !strings.Contains(out.Message, "Recent trigger audit (3):") || strings.Contains(out.Message, "trace-old") || !strings.Contains(out.Message, "trigger_promotion/promoted") || !strings.Contains(out.Message, "redaction_status=redacted") || !strings.Contains(out.Message, "trigger_result/failed") || !strings.Contains(out.Message, strings.Repeat("s", 160)+"…") || !strings.Contains(out.Message, "decision: accept") || !strings.Contains(out.Message, "permission: deny") || !strings.Contains(out.Message, "reason: policy says no") || strings.Contains(out.Message, "must-not-leak") || strings.Contains(out.Message, "must-not-render") || strings.Contains(out.Message, "payload") {
		t.Fatalf("audit mismatch: %#v", out)
	}
	invalidAudit := Dispatch(context.Background(), "/triggers audit x", registry, Context{Branch: entries})
	if invalidAudit.Kind != OutcomeHandled || !strings.Contains(invalidAudit.Message, "Recent trigger audit (4):") {
		t.Fatalf("invalid audit limit should default like upstream: %#v", invalidAudit)
	}
}

func TestTriggersCommandAbortOutcomesAndRuntimeUsage(t *testing.T) {
	registry := DefaultRegistry()
	abortOne := Dispatch(context.Background(), "/triggers abort trace-1 extra", registry, Context{})
	if abortOne.Kind != OutcomeAbortTrigger || abortOne.TargetID == nil || *abortOne.TargetID != "trace-1" || abortOne.RemoveAll {
		t.Fatalf("abort one mismatch: %#v", abortOne)
	}
	abortAll := Dispatch(context.Background(), "/triggers abort --all", registry, Context{})
	if abortAll.Kind != OutcomeAbortTrigger || !abortAll.RemoveAll {
		t.Fatalf("abort all mismatch: %#v", abortAll)
	}
	cases := map[string]string{
		"/triggers abort": "usage: /triggers abort <trace_id>|--all",
	}
	for input, want := range cases {
		out := Dispatch(context.Background(), input, registry, Context{})
		if out.Kind != OutcomeError || out.Message != want {
			t.Fatalf("%s mismatch: %#v", input, out)
		}
	}
}

func TestTriggersSourcesMatchesUpstreamRendering(t *testing.T) {
	registry := DefaultRegistry()
	empty := Dispatch(context.Background(), "/triggers sources", registry, Context{})
	if empty.Kind != OutcomeHandled || empty.Message != "(no trigger sources registered)" {
		t.Fatalf("empty sources mismatch: %#v", empty)
	}

	ctx := Context{TriggerSources: []TriggerSourceEntry{{State: "disconnected", Reason: "protocol_mismatch", LastEventAt: time.Date(2026, 5, 22, 19, 0, 0, 0, time.UTC), LastError: strings.Repeat("e", 170), QueuedCount: 2, DroppedCount: 3, DedupedCount: 4, SubscriptionLabels: []string{"repo c4pt0r/pie"}, RequiresAttention: "upgrade hub"}}}
	sources := Dispatch(context.Background(), "/triggers hooks", registry, ctx)
	if sources.Kind != OutcomeHandled || !strings.Contains(sources.Message, "Trigger sources (1):") || !strings.Contains(sources.Message, "  - source #1: disconnected (protocol_mismatch) queued=2 dropped=3 deduped=4 last_event=2026-05-22T19:00:00Z  attention: upgrade hub") || !strings.Contains(sources.Message, "      subscriptions: repo c4pt0r/pie") || !strings.Contains(sources.Message, "      last error: "+strings.Repeat("e", 160)+"…") {
		t.Fatalf("sources mismatch: %#v", sources)
	}
}

func customEntry(id, customType string, data any) session.Entry {
	return session.Entry{EntryType: session.EntryTypeCustom, EntryID: id, Timestamp: id + "-time", CustomType: customType, Data: data}
}
