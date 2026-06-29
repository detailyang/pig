package tools

import (
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
	"github.com/detailyang/pig/triggers"
)

func TestSkillToolsExposeCatalogBackedModelTools(t *testing.T) {
	catalog := skills.NewCatalog(skills.CatalogOptions{})
	root := t.TempDir()
	tools := SkillTools(root, catalog)
	expected := []string{"Skill", "ListSkills", "InstallSkill", "SkillBuilder", "SetSkillState", "RemoveSkill"}
	if strings.Join(toolNames(tools), ",") != strings.Join(expected, ",") {
		t.Fatalf("skill tool names mismatch: got %v want %v", toolNames(tools), expected)
	}
	if tools[0].(SkillTool).Catalog != catalog {
		t.Fatalf("Skill tool did not retain catalog")
	}
	if tools[2].(InstallSkillTool).Root != root || tools[2].(InstallSkillTool).Catalog != catalog {
		t.Fatalf("InstallSkill tool was not catalog/root backed: %#v", tools[2])
	}
}

func TestTriggerToolsExposeRegistryBackedModelTools(t *testing.T) {
	registry := triggers.NewDynamicRegistry()
	tools := TriggerTools(registry)
	expected := []string{"NewTrigger", "ListTriggers", "RemoveTrigger", "SetTriggerState"}
	if strings.Join(toolNames(tools), ",") != strings.Join(expected, ",") {
		t.Fatalf("trigger tool names mismatch: got %v want %v", toolNames(tools), expected)
	}
	if tools[0].(NewTriggerTool).Registry != registry || tools[3].(SetTriggerStateTool).Registry != registry {
		t.Fatalf("trigger tools did not retain registry")
	}
}

func TestCronJobToolsExposeRegistryBackedModelTools(t *testing.T) {
	registry := triggers.NewScheduledCronRegistry()
	tools := CronJobTools(registry)
	expected := []string{"NewCronJob", "ListCronJobs", "RemoveCronJob", "SetCronJobState"}
	if strings.Join(toolNames(tools), ",") != strings.Join(expected, ",") {
		t.Fatalf("cron tool names mismatch: got %v want %v", toolNames(tools), expected)
	}
	if tools[0].(NewCronJobTool).Registry != registry || tools[3].(SetCronJobStateTool).Registry != registry {
		t.Fatalf("cron tools did not retain registry")
	}
}

func TestToolSetHelpersReturnFreshSlices(t *testing.T) {
	first := TriggerTools(nil)
	second := TriggerTools(nil)
	first[0] = BashTool{}
	if second[0].Name() == "bash" {
		t.Fatalf("tool helper reused mutable slice: %#v", second)
	}
}

func TestUpstreamToolFactoryWrappers(t *testing.T) {
	catalog := skills.NewCatalog(skills.CatalogOptions{})
	root := t.TempDir()
	dynamic := triggers.NewDynamicRegistry()
	cron := triggers.NewScheduledCronRegistry()

	factories := []struct {
		name string
		tool any
	}{
		{"task", TaskToolFactory(ai.Model{}, nil)},
		{"Skill", SkillToolFactory(catalog)},
		{"InstallSkill", InstallSkillToolFactory(root, catalog)},
		{"SkillBuilder", SkillBuilderToolFactory(root, catalog)},
		{"SetSkillState", SetSkillStateToolFactory(catalog)},
		{"RemoveSkill", RemoveSkillToolFactory(root, catalog)},
		{"NewCronJob", NewCronJobToolFactory(cron)},
		{"ListCronJobs", ListCronJobsToolFactory(cron)},
		{"RemoveCronJob", RemoveCronJobToolFactory(cron)},
		{"SetCronJobState", SetCronJobStateToolFactory(cron)},
		{"NewTrigger", NewTriggerToolFactory(dynamic)},
		{"ListTriggers", ListTriggersToolFactory(dynamic)},
		{"RemoveTrigger", RemoveTriggerToolFactory(dynamic)},
		{"SetTriggerState", SetTriggerStateToolFactory(dynamic)},
	}
	for _, factory := range factories {
		tool, ok := factory.tool.(interface{ Name() string })
		if !ok || tool.Name() != factory.name {
			t.Fatalf("factory %s returned %#v", factory.name, factory.tool)
		}
	}
}
