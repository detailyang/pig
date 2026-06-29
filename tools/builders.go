package tools

import (
	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
	"github.com/detailyang/pig/triggers"
)

type StreamFn = agent.StreamFn

func SkillTools(root string, catalog *skills.Catalog) []agent.Tool {
	return []agent.Tool{
		NewCatalogSkillTool(catalog),
		NewCatalogListSkillsTool(catalog),
		NewCatalogInstallSkillTool(root, catalog),
		NewCatalogSkillBuilderTool(root, catalog),
		NewCatalogSetSkillStateTool(catalog),
		NewCatalogRemoveSkillTool(root, catalog),
	}
}

func TriggerTools(registry *triggers.DynamicRegistry) []agent.Tool {
	return []agent.Tool{
		NewTriggerTool{Registry: registry},
		ListTriggersTool{Registry: registry},
		RemoveTriggerTool{Registry: registry},
		SetTriggerStateTool{Registry: registry},
	}
}

func CronJobTools(registry *triggers.ScheduledCronRegistry) []agent.Tool {
	return []agent.Tool{
		NewCronJobTool{Registry: registry},
		ListCronJobsTool{Registry: registry},
		RemoveCronJobTool{Registry: registry},
		SetCronJobStateTool{Registry: registry},
	}
}

func task_tool(model ai.Model, streamFn StreamFn) agent.Tool {
	return NewSubagentTaskTool(agent.SubagentRunnerOptions{Model: model, Stream: streamFn})
}

func TaskToolFactory(model ai.Model, streamFn StreamFn) agent.Tool {
	return task_tool(model, streamFn)
}

func skill_tool(catalog *skills.Catalog) agent.Tool {
	return NewCatalogSkillTool(catalog)
}

func SkillToolFactory(catalog *skills.Catalog) agent.Tool {
	return skill_tool(catalog)
}

func install_skill_tool(root string, catalog *skills.Catalog) agent.Tool {
	return NewCatalogInstallSkillTool(root, catalog)
}

func InstallSkillToolFactory(root string, catalog *skills.Catalog) agent.Tool {
	return install_skill_tool(root, catalog)
}

func skill_builder_tool(root string, catalog *skills.Catalog) agent.Tool {
	return NewCatalogSkillBuilderTool(root, catalog)
}

func SkillBuilderToolFactory(root string, catalog *skills.Catalog) agent.Tool {
	return skill_builder_tool(root, catalog)
}

func set_skill_state_tool(catalog *skills.Catalog) agent.Tool {
	return NewCatalogSetSkillStateTool(catalog)
}

func SetSkillStateToolFactory(catalog *skills.Catalog) agent.Tool {
	return set_skill_state_tool(catalog)
}

func remove_skill_tool(root string, catalog *skills.Catalog) agent.Tool {
	return NewCatalogRemoveSkillTool(root, catalog)
}

func RemoveSkillToolFactory(root string, catalog *skills.Catalog) agent.Tool {
	return remove_skill_tool(root, catalog)
}

func new_cron_job_tool(registry *triggers.ScheduledCronRegistry) agent.Tool {
	return NewCronJobTool{Registry: registry}
}

func NewCronJobToolFactory(registry *triggers.ScheduledCronRegistry) agent.Tool {
	return new_cron_job_tool(registry)
}

func list_cron_jobs_tool(registry *triggers.ScheduledCronRegistry) agent.Tool {
	return ListCronJobsTool{Registry: registry}
}

func ListCronJobsToolFactory(registry *triggers.ScheduledCronRegistry) agent.Tool {
	return list_cron_jobs_tool(registry)
}

func remove_cron_job_tool(registry *triggers.ScheduledCronRegistry) agent.Tool {
	return RemoveCronJobTool{Registry: registry}
}

func RemoveCronJobToolFactory(registry *triggers.ScheduledCronRegistry) agent.Tool {
	return remove_cron_job_tool(registry)
}

func set_cron_job_state_tool(registry *triggers.ScheduledCronRegistry) agent.Tool {
	return SetCronJobStateTool{Registry: registry}
}

func SetCronJobStateToolFactory(registry *triggers.ScheduledCronRegistry) agent.Tool {
	return set_cron_job_state_tool(registry)
}

func new_trigger_tool(registry *triggers.DynamicRegistry) agent.Tool {
	return NewTriggerTool{Registry: registry}
}

func NewTriggerToolFactory(registry *triggers.DynamicRegistry) agent.Tool {
	return new_trigger_tool(registry)
}

func list_triggers_tool(registry *triggers.DynamicRegistry) agent.Tool {
	return ListTriggersTool{Registry: registry}
}

func ListTriggersToolFactory(registry *triggers.DynamicRegistry) agent.Tool {
	return list_triggers_tool(registry)
}

func remove_trigger_tool(registry *triggers.DynamicRegistry) agent.Tool {
	return RemoveTriggerTool{Registry: registry}
}

func RemoveTriggerToolFactory(registry *triggers.DynamicRegistry) agent.Tool {
	return remove_trigger_tool(registry)
}

func set_trigger_state_tool(registry *triggers.DynamicRegistry) agent.Tool {
	return SetTriggerStateTool{Registry: registry}
}

func SetTriggerStateToolFactory(registry *triggers.DynamicRegistry) agent.Tool {
	return set_trigger_state_tool(registry)
}
