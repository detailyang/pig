package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/detailyang/pig/ai"
)

func TestControlPlanePromptRequestMarshalsStableFields(t *testing.T) {
	data, err := json.Marshal(ControlPlanePromptRequest{ToolCallID: "call-1", ToolName: "bash", ArgsHash: "hash", Label: "Run bash", Payload: map[string]any{"args_hash": "hash"}, Reason: "needs review"})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["toolCallId"] != "call-1" || object["toolName"] != "bash" || object["argsHash"] != "hash" || object["label"] != "Run bash" || object["reason"] != "needs review" {
		t.Fatalf("prompt request wire fields mismatch: %s", data)
	}
	if _, ok := object["ToolCallID"]; ok {
		t.Fatalf("prompt request should not leak Go field names: %s", data)
	}
}

func TestControlPlanePromptRequestPayloadPreservesArbitraryJSONValueLikeUpstream(t *testing.T) {
	data, err := json.Marshal(ControlPlanePromptRequest{ToolCallID: "call-1", ToolName: "bash", ArgsHash: "hash", Label: "Run bash", Payload: []any{"flag", map[string]any{"ticket": json.Number("9007199254740993")}}, Reason: "needs review"})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	payload, ok := object["payload"].([]any)
	if !ok || len(payload) != 2 || payload[0] != "flag" {
		t.Fatalf("prompt request payload should preserve arbitrary JSON value like upstream, got %s", data)
	}
}

func TestControlPlanePromptResolvedEventMarshalsStableFields(t *testing.T) {
	event := Event{Type: EventTypeControlPlanePromptResolved, ControlPlanePrompt: &ControlPlanePromptRequest{ToolCallID: "call-1", ToolName: "bash", ArgsHash: "hash", Label: "Run bash"}, ControlPlanePromptDecision: ControlPlaneDeny, ControlPlanePromptReason: "denied"}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["type"] != "control_plane_prompt_resolved" || object["decision"] != "deny" || object["reason"] != "denied" {
		t.Fatalf("control-plane event wire fields mismatch: %s", data)
	}
	if object["toolCallId"] != "call-1" || object["toolName"] != "bash" || object["argsHash"] != "hash" || object["label"] != "Run bash" {
		t.Fatalf("control-plane prompt event payload mismatch: %s", data)
	}
	if _, ok := object["controlPlanePrompt"]; ok {
		t.Fatalf("control-plane event should flatten prompt fields like upstream: %s", data)
	}
	if _, ok := object["ControlPlanePrompt"]; ok {
		t.Fatalf("control-plane event should not leak Go field names: %s", data)
	}
}

func TestControlPlanePromptDecisionAsAuditStringMatchesUpstream(t *testing.T) {
	if ControlPlaneAllow.AsAuditString() != "allow" || ControlPlaneDeny.AsAuditString() != "deny" || ControlPlaneTimeout.AsAuditString() != "timeout" {
		t.Fatalf("audit strings mismatch allow=%q deny=%q timeout=%q", ControlPlaneAllow.AsAuditString(), ControlPlaneDeny.AsAuditString(), ControlPlaneTimeout.AsAuditString())
	}
	if got := ControlPlanePromptDecision("future").AsAuditString(); got != "deny" {
		t.Fatalf("unknown decision should audit as fail-closed deny like upstream enum, got %q", got)
	}
}

func TestToolExecutionStartEventMarshalsUpstreamFields(t *testing.T) {
	event := Event{Type: EventTypeToolExecutionStart, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash"}, ToolArgs: map[string]any{"path": "README.md"}}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["type"] != "tool_execution_start" || object["toolCallId"] != "call-1" || object["toolName"] != "bash" || object["args"].(map[string]any)["path"] != "README.md" {
		t.Fatalf("tool execution start wire mismatch: %s", data)
	}
	if _, ok := object["toolCall"]; ok {
		t.Fatalf("tool execution start should flatten tool call fields: %s", data)
	}
}

func TestToolExecutionStartEventArgsPreservesArbitraryJSONValueLikeUpstream(t *testing.T) {
	event := Event{Type: EventTypeToolExecutionStart, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash"}, ToolArgs: []any{"flag", map[string]any{"ticket": json.Number("9007199254740993"), "path": "<tag>&value"}}}
	data, err := marshalJSONNoHTMLEscape(event)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"args":["flag",{"path":"<tag>&value","ticket":9007199254740993}],"toolCallId":"call-1","toolName":"bash","type":"tool_execution_start"}` {
		t.Fatalf("tool execution args should preserve upstream serde_json number and HTML-sensitive characters, got %s", data)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	args, ok := object["args"].([]any)
	if !ok || len(args) != 2 || args[0] != "flag" {
		t.Fatalf("tool execution args should preserve arbitrary JSON value like upstream, got %s", data)
	}
}

func TestToolExecutionUpdateAndEndEventsMarshalUpstreamFields(t *testing.T) {
	update := Event{Type: EventTypeToolUpdate, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash"}, ToolArgs: map[string]any{"path": "README.md"}, ToolResult: &ToolResult{Content: "partial"}}
	updateData, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	var updateObject map[string]any
	if err := json.Unmarshal(updateData, &updateObject); err != nil {
		t.Fatal(err)
	}
	if updateObject["type"] != "tool_update" || updateObject["toolCallId"] != "call-1" || updateObject["toolName"] != "bash" || updateObject["partialResult"] == nil {
		t.Fatalf("tool update wire mismatch: %s", updateData)
	}
	if _, ok := updateObject["toolResult"]; ok {
		t.Fatalf("tool update should use upstream partialResult field: %s", updateData)
	}

	end := Event{Type: EventTypeToolExecutionEnd, ToolCall: &ai.ToolCall{ID: "call-1", Name: "bash"}, ToolResult: &ToolResult{Content: "done"}, IsError: true}
	endData, err := json.Marshal(end)
	if err != nil {
		t.Fatal(err)
	}
	var endObject map[string]any
	if err := json.Unmarshal(endData, &endObject); err != nil {
		t.Fatal(err)
	}
	if endObject["type"] != "tool_execution_end" || endObject["toolCallId"] != "call-1" || endObject["toolName"] != "bash" || endObject["result"] == nil || endObject["isError"] != true {
		t.Fatalf("tool execution end wire mismatch: %s", endData)
	}
}

func TestControlPlaneAllowAndDenyHooks(t *testing.T) {
	allow := AllowControlPlanePromptHook()
	decision, err := allow(context.Background(), ControlPlanePromptRequest{ToolName: "bash"})
	if err != nil || decision != ControlPlaneAllow {
		t.Fatalf("allow mismatch decision=%s err=%v", decision, err)
	}
	upstreamAllow := AllowHook()
	decision, err = upstreamAllow(context.Background(), ControlPlanePromptRequest{ToolName: "bash"})
	if err != nil || decision != ControlPlaneAllow {
		t.Fatalf("upstream-named allow mismatch decision=%s err=%v", decision, err)
	}
	deny := DenyControlPlanePromptHook("no ui")
	decision, err = deny(context.Background(), ControlPlanePromptRequest{ToolName: "bash"})
	if err != nil || decision != ControlPlaneDeny {
		t.Fatalf("deny mismatch decision=%s err=%v", decision, err)
	}
	upstreamDeny := DenyHook("no ui")
	decision, err = upstreamDeny(context.Background(), ControlPlanePromptRequest{ToolName: "bash"})
	if err != nil || decision != ControlPlaneDeny {
		t.Fatalf("upstream-named deny mismatch decision=%s err=%v", decision, err)
	}
}

func TestInteractiveControlPlanePromptHookQueuesAndResolves(t *testing.T) {
	hook, queue := InteractiveHook()
	result := make(chan ControlPlanePromptDecision, 1)
	reason := ""
	go func() {
		decision, err := hook(withControlPlanePromptReason(context.Background(), &reason), ControlPlanePromptRequest{ToolCallID: "call-1", ToolName: "bash"})
		if err != nil {
			t.Errorf("hook error: %v", err)
		}
		result <- decision
	}()
	prompt, ok := queue.Next(context.Background())
	if !ok || prompt.Request.ToolCallID != "call-1" || prompt.Request.ToolName != "bash" {
		t.Fatalf("prompt mismatch ok=%v prompt=%#v", ok, prompt)
	}
	var upstreamAlias *UiControlPlanePrompt = prompt
	if upstreamAlias.Request.ToolName != "bash" {
		t.Fatalf("UI prompt alias mismatch: %#v", upstreamAlias)
	}
	prompt.ResolveWithReason(ControlPlaneDeny, "review denied")
	select {
	case decision := <-result:
		if decision != ControlPlaneDeny || reason != "review denied" {
			t.Fatalf("decision mismatch: %s", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for decision")
	}
}

func TestInteractiveControlPlanePromptHookFailsClosedOnCancelAndClose(t *testing.T) {
	hook, queue := NewInteractiveControlPlanePromptHook(1)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan ControlPlanePromptDecision, 1)
	reason := ""
	go func() {
		decision, err := hook(withControlPlanePromptReason(ctx, &reason), ControlPlanePromptRequest{ToolCallID: "cancel", ToolName: "bash"})
		if err != nil {
			t.Errorf("hook error: %v", err)
		}
		result <- decision
	}()
	prompt, ok := queue.Next(context.Background())
	if !ok || prompt.Request.ToolCallID != "cancel" {
		t.Fatalf("prompt mismatch ok=%v prompt=%#v", ok, prompt)
	}
	cancel()
	select {
	case decision := <-result:
		if decision != ControlPlaneDeny || reason != "control-plane prompt cancelled" {
			t.Fatalf("cancel should deny with upstream reason, got decision=%s reason=%q", decision, reason)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancel decision")
	}

	queue.Close()
	closedReason := ""
	decision, err := hook(withControlPlanePromptReason(context.Background(), &closedReason), ControlPlanePromptRequest{ToolCallID: "closed", ToolName: "bash"})
	if err != nil || decision != ControlPlaneDeny || closedReason != "control-plane prompt UI is unavailable" {
		t.Fatalf("closed queue should deny with upstream reason decision=%s reason=%q err=%v", decision, closedReason, err)
	}
	if _, ok := queue.Next(context.Background()); ok {
		t.Fatal("closed queue should not yield prompts")
	}
}

func TestInteractiveControlPlanePromptHookFailsClosedWhenUIClosesPendingPrompt(t *testing.T) {
	hook, queue := NewInteractiveControlPlanePromptHook(1)
	result := make(chan ControlPlanePromptDecision, 1)
	reason := ""
	go func() {
		decision, err := hook(withControlPlanePromptReason(context.Background(), &reason), ControlPlanePromptRequest{ToolCallID: "pending", ToolName: "bash"})
		if err != nil {
			t.Errorf("hook error: %v", err)
		}
		result <- decision
	}()
	prompt, ok := queue.Next(context.Background())
	if !ok || prompt.Request.ToolCallID != "pending" {
		t.Fatalf("prompt mismatch ok=%v prompt=%#v", ok, prompt)
	}
	queue.Close()
	select {
	case decision := <-result:
		if decision != ControlPlaneDeny || reason != "control-plane prompt UI closed before a decision" {
			t.Fatalf("closed pending prompt should deny with upstream reason, got decision=%s reason=%q", decision, reason)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed pending decision")
	}
}
