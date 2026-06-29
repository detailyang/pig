package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/triggers"
)

func TestNewTriggerToolCreatesRuleFromConditionAction(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	tool := NewTriggerTool{Registry: registry}
	result, err := tool.Execute(context.Background(), ai.ToolCall{ID: "call-1", Name: "NewTrigger", Arguments: map[string]any{"condition": "tests fail", "action": "run go test", "fire_once": false, "promote_to_chat": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rules := registry.List()
	if len(rules) != 1 || rules[0].Condition != "tests fail" || rules[0].Action != "run go test" || rules[0].FireOnce || !rules[0].PromoteToChat {
		t.Fatalf("rule mismatch: %#v", rules)
	}
	if !strings.Contains(result.Content, "created dynamic trigger") || result.Details["condition"] != "tests fail" || result.Details["promote_to_chat"] != true {
		t.Fatalf("result mismatch: %#v", result)
	}
}

func TestNewTriggerToolCreatesRuleFromSpec(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	tool := NewTriggerTool{Registry: registry}
	result, err := tool.Execute(context.Background(), ai.ToolCall{ID: "call-1", Name: "NewTrigger", Arguments: map[string]any{"spec": "if build fails, then run go test ./..."}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rules := registry.List()
	if len(rules) != 1 || rules[0].Condition != "build fails," || rules[0].Action != "go test ./..." || !rules[0].FireOnce {
		t.Fatalf("spec rule mismatch: %#v result=%#v", rules, result)
	}
}

func TestNewTriggerToolUsesConditionActionWhenKeysExistLikeUpstream(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	_, err := (NewTriggerTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "NewTrigger", Arguments: map[string]any{"condition": "", "action": "", "spec": "if build fails, then run go test"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "condition and action must both be non-empty") {
		t.Fatalf("expected empty condition/action error instead of spec fallback, got %v", err)
	}
	if len(registry.List()) != 0 {
		t.Fatalf("rule should not be created from spec fallback: %#v", registry.List())
	}
}

func TestNewTriggerToolFallsBackToSpecWhenConditionActionAreNotStringsLikeUpstream(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	_, err := (NewTriggerTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "NewTrigger", Arguments: map[string]any{"condition": 123, "action": true, "spec": "if build fails, then run go test"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rules := registry.List()
	if len(rules) != 1 || rules[0].Condition != "build fails," || rules[0].Action != "go test" {
		t.Fatalf("should create rule from spec fallback: %#v", rules)
	}
}

func TestTriggerToolsRejectInvalidUTF8Arguments(t *testing.T) {
	bad := string([]byte{0xff})
	registry := triggers.NewDynamicRegistry()
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "condition", run: func() error {
			_, err := (NewTriggerTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "NewTrigger", Arguments: map[string]any{"condition": bad, "action": "run"}}, nil)
			return err
		}, want: "condition must be valid UTF-8"},
		{name: "action", run: func() error {
			_, err := (NewTriggerTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "NewTrigger", Arguments: map[string]any{"condition": "cond", "action": bad}}, nil)
			return err
		}, want: "action must be valid UTF-8"},
		{name: "spec", run: func() error {
			_, err := (NewTriggerTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "NewTrigger", Arguments: map[string]any{"spec": bad}}, nil)
			return err
		}, want: "spec must be valid UTF-8"},
		{name: "remove id", run: func() error {
			_, err := (RemoveTriggerTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "RemoveTrigger", Arguments: map[string]any{"id": bad}}, nil)
			return err
		}, want: "id must be valid UTF-8"},
		{name: "set id", run: func() error {
			_, err := (SetTriggerStateTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "SetTriggerState", Arguments: map[string]any{"id": bad, "enabled": false}}, nil)
			return err
		}, want: "id must be valid UTF-8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestNewTriggerToolRejectsCronLikeRequests(t *testing.T) {
	tool := NewTriggerTool{Registry: triggers.NewDynamicRegistry()}
	for _, condition := range []string{"every hour", "every day", "every week"} {
		_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "NewTrigger", Arguments: map[string]any{"condition": condition, "action": "run health check"}}, nil)
		if err == nil || !strings.Contains(err.Error(), "fixed scheduled jobs must use NewCronJob") {
			t.Fatalf("expected cron rejection for %q, got %v", condition, err)
		}
	}
}

func TestNewTriggerToolDefinitionMatchesUpstream(t *testing.T) {
	tool := NewTriggerTool{}
	if !strings.Contains(tool.Description(), "future events such as a browser tab") || !strings.Contains(tool.Description(), "use NewCronJob instead") {
		t.Fatalf("description should match upstream guidance, got %q", tool.Description())
	}
	params := tool.Parameters()
	required, ok := params["required"].([]string)
	if !ok || len(required) != 2 || required[0] != "condition" || required[1] != "action" {
		t.Fatalf("required schema mismatch: %#v", params["required"])
	}
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties mismatch: %#v", params["properties"])
	}
	for _, key := range []string{"condition", "action", "spec", "fire_once", "promote_to_chat"} {
		property, ok := properties[key].(map[string]any)
		if !ok || property["description"] == "" {
			t.Fatalf("property %s should include upstream description: %#v", key, properties[key])
		}
	}
}

func TestRemoveTriggerToolPermissionMatchesUpstream(t *testing.T) {
	tool := RemoveTriggerTool{}
	if got := tool.PermissionClassification(map[string]any{"id": "dyn-123"}); got != agent.PermissionAsk {
		t.Fatalf("single remove should prompt, got %s", got)
	}
	if reason := tool.PermissionReason(map[string]any{"id": "dyn-123"}); reason != "remove dynamic trigger `dyn-123`" {
		t.Fatalf("single remove reason mismatch: %q", reason)
	}
	if got := tool.PermissionClassification(map[string]any{"all": true}); got != agent.PermissionAsk {
		t.Fatalf("remove all should prompt, got %s", got)
	}
	if reason := tool.PermissionReason(map[string]any{"all": true}); reason != "remove ALL dynamic triggers" {
		t.Fatalf("remove all reason mismatch: %q", reason)
	}
	if reason := tool.PermissionReason(map[string]any{}); reason != "remove dynamic trigger" {
		t.Fatalf("fallback reason mismatch: %q", reason)
	}
}

func TestTriggerManagementToolDefinitionsMatchUpstream(t *testing.T) {
	list := ListTriggersTool{}
	if list.Description() != "List dynamic trigger rules currently registered in pie. Use this when the user asks to view, list, show, inspect, or find trigger ids." {
		t.Fatalf("list description mismatch: %q", list.Description())
	}

	remove := RemoveTriggerTool{}
	if remove.Description() != "Delete dynamic trigger rules. Use this when the user asks pie to delete, remove, or clear an existing dynamic trigger." {
		t.Fatalf("remove description mismatch: %q", remove.Description())
	}
	removeProperties := remove.Parameters()["properties"].(map[string]any)
	for _, key := range []string{"id", "all"} {
		property, ok := removeProperties[key].(map[string]any)
		if !ok || property["description"] == "" {
			t.Fatalf("remove property %s should include upstream description: %#v", key, removeProperties[key])
		}
	}

	setState := SetTriggerStateTool{}
	if setState.Description() != "Enable or disable an existing dynamic trigger rule without deleting it. Use this when the user asks to pause, disable, enable, or resume a trigger." {
		t.Fatalf("set state description mismatch: %q", setState.Description())
	}
	setProperties := setState.Parameters()["properties"].(map[string]any)
	for _, key := range []string{"id", "enabled"} {
		property, ok := setProperties[key].(map[string]any)
		if !ok || property["description"] == "" {
			t.Fatalf("set property %s should include upstream description: %#v", key, setProperties[key])
		}
	}
}

func TestListTriggersToolRendersRules(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	rule, err := registry.AddRuleWithFlags("tests fail", "run go test", false, true)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (ListTriggersTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{ID: "call-1", Name: "ListTriggers"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "dynamic trigger rules: 1") || !strings.Contains(result.Content, rule.ID) || result.Details["count"] != 1 {
		t.Fatalf("list mismatch: %#v", result)
	}
	wantLine := "- " + rule.ID + " [enabled, repeat, promote_to_chat] created_at=" + rule.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00") + " condition: tests fail action: run go test"
	if !strings.Contains(result.Content, wantLine) {
		t.Fatalf("list content should match upstream line shape:\nwant %q\ngot  %q", wantLine, result.Content)
	}
}

func TestRemoveTriggerToolRemovesOneOrAll(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	rule, err := registry.AddRule("tests fail", "run go test")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := (RemoveTriggerTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{ID: "call-1", Name: "RemoveTrigger", Arguments: map[string]any{"id": rule.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.List()) != 0 || removed.Details["removed_count"] != 1 || !strings.Contains(removed.Content, "removed dynamic trigger") {
		t.Fatalf("remove mismatch: %#v", removed)
	}
	if _, err := registry.AddRule("again", "run"); err != nil {
		t.Fatal(err)
	}
	all, err := (RemoveTriggerTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "RemoveTrigger", Arguments: map[string]any{"all": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.List()) != 0 || all.Details["removed_count"] != 1 || all.Details["all"] != true {
		t.Fatalf("remove all mismatch: %#v", all)
	}
}

func TestSetTriggerStateToolEnablesAndDisables(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	rule, err := registry.AddRule("tests fail", "run go test")
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := (SetTriggerStateTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "SetTriggerState", Arguments: map[string]any{"id": rule.ID, "enabled": false}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantDisabledContent := "updated dynamic trigger " + rule.ID + "\nstate: disabled\ncondition: tests fail\naction: run go test"
	if registry.List()[0].Enabled || disabled.Details["enabled"] != false || disabled.Content != wantDisabledContent {
		t.Fatalf("disable mismatch: %#v", disabled)
	}
	enabled, err := (SetTriggerStateTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "SetTriggerState", Arguments: map[string]any{"id": rule.ID, "enabled": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantEnabledContent := "updated dynamic trigger " + rule.ID + "\nstate: enabled\ncondition: tests fail\naction: run go test"
	if !registry.List()[0].Enabled || enabled.Details["enabled"] != true || enabled.Content != wantEnabledContent {
		t.Fatalf("enable mismatch: %#v", enabled)
	}
}
