package triggers

import "testing"

func TestUpstreamTriggerToolTypesAreInTriggersPackage(t *testing.T) {
	dynamic := NewDynamicRegistry()
	tools := []interface{ Name() string }{
		NewTriggerTool{Registry: dynamic},
		ListTriggersTool{Registry: dynamic},
		RemoveTriggerTool{Registry: dynamic},
		SetTriggerStateTool{Registry: dynamic},
	}
	want := []string{"NewTrigger", "ListTriggers", "RemoveTrigger", "SetTriggerState"}
	for index, tool := range tools {
		if tool.Name() != want[index] {
			t.Fatalf("tool %d name mismatch: got %q want %q", index, tool.Name(), want[index])
		}
	}
}

func TestUpstreamCronToolTypesAreInTriggersPackage(t *testing.T) {
	cron := NewScheduledCronRegistry()
	tools := []interface{ Name() string }{
		NewCronJobTool{Registry: cron},
		ListCronJobsTool{Registry: cron},
		RemoveCronJobTool{Registry: cron},
		SetCronJobStateTool{Registry: cron},
	}
	want := []string{"NewCronJob", "ListCronJobs", "RemoveCronJob", "SetCronJobState"}
	for index, tool := range tools {
		if tool.Name() != want[index] {
			t.Fatalf("tool %d name mismatch: got %q want %q", index, tool.Name(), want[index])
		}
	}
}
