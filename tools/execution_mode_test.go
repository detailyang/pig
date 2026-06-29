package tools

import (
	"testing"

	"github.com/detailyang/pig/agent"
)

func TestControlPlaneWriteToolsRunSequentially(t *testing.T) {
	tools := []agent.Tool{InstallSkillTool{}, SkillBuilderTool{}, SetSkillStateTool{}, RemoveSkillTool{}, NewCronJobTool{}, RemoveCronJobTool{}, SetCronJobStateTool{}}
	for _, tool := range tools {
		override, ok := tool.(agent.ToolExecutionModeOverride)
		if !ok {
			t.Fatalf("%s should override execution mode", tool.Name())
		}
		if override.ExecutionMode() != agent.ToolExecutionSequential {
			t.Fatalf("%s execution mode = %s, want %s", tool.Name(), override.ExecutionMode(), agent.ToolExecutionSequential)
		}
	}
}

func TestUpstreamParallelToolsDeclareParallelMode(t *testing.T) {
	tools := []agent.Tool{ReadTool{}, LSTool{}, FindTool{}, GrepTool{}, GitTool{}, SkillTool{}, NewTaskTool(TaskToolOptions{}), WebFetchTool{}, NewWebSearchTool(WebSearchOptions{APIKey: "test"}), NewTriggerTool{}, ListTriggersTool{}, RemoveTriggerTool{}, SetTriggerStateTool{}, ListCronJobsTool{}}
	for _, tool := range tools {
		override, ok := tool.(agent.ToolExecutionModeOverride)
		if !ok {
			t.Fatalf("%s should override execution mode", tool.Name())
		}
		if override.ExecutionMode() != agent.ToolExecutionParallel {
			t.Fatalf("%s execution mode = %s, want %s", tool.Name(), override.ExecutionMode(), agent.ToolExecutionParallel)
		}
	}
}

func TestSkillControlPlanePermissionClassifications(t *testing.T) {
	install, ok := agent.Tool(InstallSkillTool{}).(agent.ToolPermissionClassifier)
	if !ok {
		t.Fatal("InstallSkill should classify permissions")
	}
	if got := install.PermissionClassification(map[string]any{}); got != agent.PermissionAsk {
		t.Fatalf("install classification = %s, want %s", got, agent.PermissionAsk)
	}

	builder, ok := agent.Tool(SkillBuilderTool{}).(agent.ToolPermissionClassifier)
	if !ok {
		t.Fatal("SkillBuilder should classify permissions")
	}
	if got := builder.PermissionClassification(map[string]any{"name": "alpha", "confirm": false}); got != agent.PermissionAllow {
		t.Fatalf("builder preview classification = %s, want %s", got, agent.PermissionAllow)
	}
	if got := builder.PermissionClassification(map[string]any{"name": "alpha", "confirm": true}); got != agent.PermissionAsk {
		t.Fatalf("builder confirm classification = %s, want %s", got, agent.PermissionAsk)
	}

	setState, ok := agent.Tool(SetSkillStateTool{}).(agent.ToolPermissionClassifier)
	if !ok {
		t.Fatal("SetSkillState should classify permissions")
	}
	if got := setState.PermissionClassification(map[string]any{"name": "alpha", "enabled": false}); got != agent.PermissionAllow {
		t.Fatalf("disable classification = %s, want %s", got, agent.PermissionAllow)
	}
	if got := setState.PermissionClassification(map[string]any{"name": "alpha", "enabled": true}); got != agent.PermissionAsk {
		t.Fatalf("enable classification = %s, want %s", got, agent.PermissionAsk)
	}
	if got := setState.PermissionClassification(map[string]any{"name": "alpha"}); got != agent.PermissionAllow {
		t.Fatalf("missing enabled classification = %s, want %s", got, agent.PermissionAllow)
	}

	remove, ok := agent.Tool(RemoveSkillTool{}).(agent.ToolPermissionClassifier)
	if !ok {
		t.Fatal("RemoveSkill should classify permissions")
	}
	if got := remove.PermissionClassification(map[string]any{"name": "alpha"}); got != agent.PermissionAsk {
		t.Fatalf("remove classification = %s, want %s", got, agent.PermissionAsk)
	}
}

func TestDynamicTriggerPermissionClassifications(t *testing.T) {
	newTrigger, ok := agent.Tool(NewTriggerTool{}).(agent.ToolPermissionClassifier)
	if !ok {
		t.Fatal("NewTrigger should classify permissions")
	}
	if got := newTrigger.PermissionClassification(map[string]any{"condition": "secret token", "action": "run"}); got != agent.PermissionAsk {
		t.Fatalf("new trigger classification = %s, want %s", got, agent.PermissionAsk)
	}
	newReasoner, ok := agent.Tool(NewTriggerTool{}).(agent.ToolPermissionReasoner)
	if !ok {
		t.Fatal("NewTrigger should provide permission reason")
	}
	if got := newReasoner.PermissionReason(map[string]any{"condition": "secret token", "action": "run"}); got != "create dynamic trigger from `condition` + `action` fields" {
		t.Fatalf("condition/action reason = %q", got)
	}
	if got := newReasoner.PermissionReason(map[string]any{"spec": "if secret token then run"}); got != "create dynamic trigger from `spec` field" {
		t.Fatalf("spec reason = %q", got)
	}
	if got := newReasoner.PermissionReason(map[string]any{}); got != "create dynamic trigger" {
		t.Fatalf("empty reason = %q", got)
	}

	setState, ok := agent.Tool(SetTriggerStateTool{}).(agent.ToolPermissionClassifier)
	if !ok {
		t.Fatal("SetTriggerState should classify permissions")
	}
	if got := setState.PermissionClassification(map[string]any{"id": "dyn-secret", "enabled": false}); got != agent.PermissionAllow {
		t.Fatalf("disable classification = %s, want %s", got, agent.PermissionAllow)
	}
	if got := setState.PermissionClassification(map[string]any{"id": "dyn-secret", "enabled": true}); got != agent.PermissionAsk {
		t.Fatalf("enable classification = %s, want %s", got, agent.PermissionAsk)
	}
	setReasoner, ok := agent.Tool(SetTriggerStateTool{}).(agent.ToolPermissionReasoner)
	if !ok {
		t.Fatal("SetTriggerState should provide permission reason")
	}
	if got := setReasoner.PermissionReason(map[string]any{"id": "dyn-secret", "enabled": true}); got != "re-enable dynamic trigger `dyn-secret`" {
		t.Fatalf("enable reason = %q", got)
	}
	if got := setReasoner.PermissionReason(map[string]any{"enabled": true}); got != "re-enable dynamic trigger `<unknown>`" {
		t.Fatalf("unknown reason = %q", got)
	}
	if got := setReasoner.PermissionReason(map[string]any{"id": "dyn-secret", "enabled": false}); got != "" {
		t.Fatalf("disable reason = %q, want empty", got)
	}
}
