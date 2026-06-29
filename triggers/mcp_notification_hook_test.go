package triggers

import (
	"context"
	"testing"
	"time"

	"github.com/detailyang/pig/mcp"
)

func TestMCPNotificationHookRunsNotificationsIntoSinkLikeUpstream(t *testing.T) {
	notifications := make(chan mcp.ServerNotification, 2)
	notifications <- mcp.ServerNotification{Method: "notifications/tools/listChanged"}
	notifications <- mcp.ServerNotification{Method: "custom/event", Params: map[string]any{"_meta": map[string]any{"pie_dedup_key": "event-1", "pie_summary": "changed"}}}
	close(notifications)
	hook := NewMCPNotificationHook("fs", notifications)
	var upstreamHook *McpNotificationHook = hook
	if upstreamHook.Label() != "mcp:fs" {
		t.Fatalf("McpNotificationHook alias mismatch: %s", upstreamHook.Label())
	}
	sink := make(chan Trigger, 2)

	if err := hook.Run(context.Background(), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink) != 2 {
		t.Fatalf("sink count mismatch: %d", len(sink))
	}
	first := <-sink
	if first.Source.Kind != SourceMCP || first.Source.ServerName != "fs" || first.IDempotencyKey != "mcp:fs:tools" || first.ReplacementPolicy != ReplacementLatestReplaces {
		t.Fatalf("first trigger mismatch: %#v", first)
	}
	second := <-sink
	if second.IDempotencyKey != "mcp:fs:custom:event-1" || second.ReplacementPolicy != ReplacementDrop || second.PayloadSummary == nil || *second.PayloadSummary != "custom/event changed" {
		t.Fatalf("second trigger mismatch: %#v", second)
	}
	status := hook.Status()
	if status.State.Kind != HookStateDisconnected || status.State.Reason != "mcp transport closed" || status.LastEventAt == nil || status.LastError != nil || status.DroppedCount != 0 {
		t.Fatalf("status mismatch: %#v", status)
	}
}

func TestMCPNotificationHooksFromSources(t *testing.T) {
	notifications := make(chan mcp.ServerNotification)
	hooks := MCPNotificationHooksFromSources([]mcp.MCPNotificationSource{{ServerName: "fs", Notifications: notifications}})
	if len(hooks) != 1 || hooks[0].Label() != "mcp:fs" {
		t.Fatalf("hooks mismatch: %#v", hooks)
	}
}

func TestLoadedMCPInjectSetsDriveDirectInjectDynamicTriggerAction(t *testing.T) {
	loaded := mcp.LoadedMCP{InjectSummaryServers: map[string]bool{"summary": true}, InjectAndRunServers: map[string]bool{"run": true}}
	summary := "safe summary"
	summaryTrigger := Trigger{Source: Source{Kind: SourceMCP, ServerName: "summary"}, SourceLabel: "mcp:summary", EventLabel: "custom/event", PayloadSummary: &summary}
	action := DirectInjectDynamicTriggerAction(NewDynamicRegistry(), summaryTrigger, loaded.InjectSummaryServers, loaded.InjectAndRunServers)
	if action.Delivery != TriggerDeliveryInjectSummary || action.Promote != PromoteSummaryNow || action.PromoteTemplateBody != "{{trigger.payload_summary}}" {
		t.Fatalf("summary action mismatch: %#v", action)
	}
	runTrigger := Trigger{Source: Source{Kind: SourceMCP, ServerName: "run"}, SourceLabel: "mcp:run", EventLabel: "custom/event", PayloadSummary: &summary}
	action = DirectInjectDynamicTriggerAction(NewDynamicRegistry(), runTrigger, loaded.InjectSummaryServers, loaded.InjectAndRunServers)
	if action.Delivery != TriggerDeliveryInjectAndRun || action.Prompt != summary {
		t.Fatalf("run action mismatch: %#v", action)
	}
}

func TestMCPNotificationHookDropsCustomNotificationsWithoutDedupKeyLikeUpstream(t *testing.T) {
	notifications := make(chan mcp.ServerNotification, 1)
	notifications <- mcp.ServerNotification{Method: "custom/event", Params: map[string]any{"value": "x"}}
	close(notifications)
	hook := NewMCPNotificationHook("fs", notifications)
	sink := make(chan Trigger, 1)

	if err := hook.Run(context.Background(), sink); err != nil {
		t.Fatal(err)
	}
	if len(sink) != 0 {
		t.Fatalf("unexpected trigger: %#v", <-sink)
	}
	status := hook.Status()
	if status.DroppedCount != 1 || status.LastError == nil || status.State.Kind != HookStateDisconnected {
		t.Fatalf("drop status mismatch: %#v", status)
	}
}

func TestMCPNotificationHookReportsSinkClosedLikeUpstream(t *testing.T) {
	notifications := make(chan mcp.ServerNotification, 1)
	notifications <- mcp.ServerNotification{Method: "notifications/tools/listChanged"}
	hook := NewMCPNotificationHook("fs", notifications)
	sink := make(chan Trigger)
	close(sink)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := hook.Run(ctx, sink)
	if hookErr, ok := err.(HookError); !ok || hookErr.Kind != HookErrorSinkClosed {
		t.Fatalf("expected sink closed hook error, got %#v", err)
	}
	status := hook.Status()
	if status.State.Kind != HookStateDisconnected || status.State.Reason != "sink closed" {
		t.Fatalf("sink closed status mismatch: %#v", status)
	}
}

func TestMCPNotificationHookRunConsumesReceiverOnceLikeUpstream(t *testing.T) {
	notifications := make(chan mcp.ServerNotification)
	close(notifications)
	hook := NewMCPNotificationHook("fs", notifications)
	if err := hook.Run(context.Background(), make(chan Trigger, 1)); err != nil {
		t.Fatalf("first run should drain closed receiver cleanly, got %v", err)
	}
	err := hook.Run(context.Background(), make(chan Trigger, 1))
	if hookErr, ok := err.(HookError); !ok || hookErr.Kind != HookErrorOther {
		t.Fatalf("second run should fail after receiver is consumed like upstream, got %#v", err)
	}
}
