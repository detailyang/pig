package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/triggers"
)

func TestNewCronJobToolCreatesJob(t *testing.T) {
	registry := triggers.NewScheduledCronRegistry()
	result, err := (NewCronJobTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "NewCronJob", Arguments: map[string]any{"schedule": "every hour", "action": "check health", "stateful": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].Schedule != "0 * * * *" || jobs[0].Action != "check health" || !jobs[0].Stateful || !jobs[0].Enabled {
		t.Fatalf("job mismatch: %#v", jobs)
	}
	if !strings.Contains(result.Content, "created cron job") || result.Details["schedule"] != "0 * * * *" || result.Details["stateful"] != true || result.Details["scope"] != "session" {
		t.Fatalf("result mismatch: %#v", result)
	}
	wantContent := "created cron job " + jobs[0].ID + "\nschedule: 0 * * * *\naction: check health"
	if result.Content != wantContent {
		t.Fatalf("content should match upstream shape:\nwant %q\n got %q", wantContent, result.Content)
	}
	if _, ok := result.Details["audit_entry_id"]; !ok {
		t.Fatalf("result should include audit_entry_id like upstream: %#v", result.Details)
	}
	if _, ok := result.Details["storage_path"]; ok {
		t.Fatalf("result should not include storage_path for NewCronJob: %#v", result.Details)
	}
}

func TestCronToolDefinitionsMatchUpstream(t *testing.T) {
	newCron := NewCronJobTool{}
	if !strings.Contains(newCron.Description(), "session-scoped cron scheduled job") || !strings.Contains(newCron.Description(), "Do not use NewTrigger") {
		t.Fatalf("new cron description mismatch: %q", newCron.Description())
	}
	newParams := newCron.Parameters()
	newRequired, ok := newParams["required"].([]string)
	if !ok || len(newRequired) != 2 || newRequired[0] != "schedule" || newRequired[1] != "action" {
		t.Fatalf("new cron required mismatch: %#v", newParams["required"])
	}
	newProperties := newParams["properties"].(map[string]any)
	for _, key := range []string{"schedule", "action", "stateful"} {
		property, ok := newProperties[key].(map[string]any)
		if !ok || property["description"] == "" {
			t.Fatalf("new cron property %s should include upstream description: %#v", key, newProperties[key])
		}
	}
	if newProperties["stateful"].(map[string]any)["default"] != false {
		t.Fatalf("stateful default mismatch: %#v", newProperties["stateful"])
	}

	list := ListCronJobsTool{}
	if !strings.Contains(list.Description(), "List the session-scoped cron scheduled jobs") || !strings.Contains(list.Description(), "定时任务") {
		t.Fatalf("list cron description mismatch: %q", list.Description())
	}

	remove := RemoveCronJobTool{}
	if !strings.Contains(remove.Description(), "Preview or confirm removal") || !strings.Contains(remove.Description(), "confirm=true only after") {
		t.Fatalf("remove cron description mismatch: %q", remove.Description())
	}
	removeProperties := remove.Parameters()["properties"].(map[string]any)
	for _, key := range []string{"id", "confirm"} {
		property, ok := removeProperties[key].(map[string]any)
		if !ok || property["description"] == "" {
			t.Fatalf("remove cron property %s should include upstream description: %#v", key, removeProperties[key])
		}
	}

	setState := SetCronJobStateTool{}
	if !strings.Contains(setState.Description(), "Disable a session-scoped cron scheduled job") || !strings.Contains(setState.Description(), "/cron enable <id>") {
		t.Fatalf("set cron state description mismatch: %q", setState.Description())
	}
	setProperties := setState.Parameters()["properties"].(map[string]any)
	for _, key := range []string{"id", "enabled"} {
		property, ok := setProperties[key].(map[string]any)
		if !ok || property["description"] == "" {
			t.Fatalf("set cron property %s should include upstream description: %#v", key, setProperties[key])
		}
	}
}

func TestNewCronJobToolEmptyActionErrorMatchesUpstream(t *testing.T) {
	_, err := (NewCronJobTool{Registry: triggers.NewScheduledCronRegistry()}).Execute(context.Background(), ai.ToolCall{Name: "NewCronJob", Arguments: map[string]any{"schedule": "every hour", "action": ""}}, nil)
	if err == nil || err.Error() != "cron action cannot be empty" {
		t.Fatalf("empty action error mismatch: %v", err)
	}
}

func TestNewCronJobToolNonStringArgsAreMissingLikeUpstream(t *testing.T) {
	_, err := (NewCronJobTool{Registry: triggers.NewScheduledCronRegistry()}).Execute(context.Background(), ai.ToolCall{Name: "NewCronJob", Arguments: map[string]any{"schedule": 123, "action": "check"}}, nil)
	if err == nil || err.Error() != "missing required arg: schedule" {
		t.Fatalf("schedule error mismatch: %v", err)
	}
	_, err = (NewCronJobTool{Registry: triggers.NewScheduledCronRegistry()}).Execute(context.Background(), ai.ToolCall{Name: "NewCronJob", Arguments: map[string]any{"schedule": "every hour", "action": false}}, nil)
	if err == nil || err.Error() != "missing required arg: action" {
		t.Fatalf("action error mismatch: %v", err)
	}
}

func TestCronToolsRejectInvalidUTF8Arguments(t *testing.T) {
	bad := string([]byte{0xff})
	registry := triggers.NewScheduledCronRegistry()
	job, err := registry.AddJob("*/10 * * * *", "summarize")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "schedule", run: func() error {
			_, err := (NewCronJobTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "NewCronJob", Arguments: map[string]any{"schedule": bad, "action": "check"}}, nil)
			return err
		}, want: "schedule must be valid UTF-8"},
		{name: "action", run: func() error {
			_, err := (NewCronJobTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "NewCronJob", Arguments: map[string]any{"schedule": "*/10 * * * *", "action": bad}}, nil)
			return err
		}, want: "action must be valid UTF-8"},
		{name: "remove id", run: func() error {
			_, err := (RemoveCronJobTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "RemoveCronJob", Arguments: map[string]any{"id": bad}}, nil)
			return err
		}, want: "id must be valid UTF-8"},
		{name: "set id", run: func() error {
			_, err := (SetCronJobStateTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "SetCronJobState", Arguments: map[string]any{"id": bad, "enabled": false}}, nil)
			return err
		}, want: "id must be valid UTF-8"},
	}
	_ = job
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil || err.Error() != tc.want {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestListCronJobsToolRendersJobs(t *testing.T) {
	registry := triggers.NewScheduledCronRegistry()
	job, err := registry.AddJob("*/10 * * * *", "summarize")
	if err != nil {
		t.Fatal(err)
	}
	result, err := (ListCronJobsTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "ListCronJobs"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "cron jobs: 1") || !strings.Contains(result.Content, job.ID) || result.Details["count"] != 1 {
		t.Fatalf("list mismatch: %#v", result)
	}
	if !strings.Contains(result.Content, "session cron jobs: 1") || strings.Contains(result.Content, "stateless") {
		t.Fatalf("list content should match upstream wording: %q", result.Content)
	}
	jobs, ok := result.Details["jobs"].([]map[string]any)
	if !ok || len(jobs) != 1 {
		t.Fatalf("jobs details mismatch: %#v", result.Details["jobs"])
	}
	if jobs[0]["action_preview"] != "summarize" || jobs[0]["scope"] != "session" {
		t.Fatalf("job details should include upstream action_preview and scope: %#v", jobs[0])
	}
	if _, ok := jobs[0]["action"]; ok {
		t.Fatalf("job details should not include raw action like upstream: %#v", jobs[0])
	}
	if _, ok := jobs[0]["stateful"]; ok {
		t.Fatalf("job details should not include stateful like upstream list details: %#v", jobs[0])
	}
}

func TestListCronJobsDetailsFormatsNullableTimesLikeUpstream(t *testing.T) {
	registry := triggers.NewScheduledCronRegistry()
	job, err := registry.AddJob("* * * * *", "summarize")
	if err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 22, 8, 1, 0, 0, time.UTC)
	if due := registry.DueJobs(since, now); len(due) != 1 {
		t.Fatalf("expected one due job, got %#v", due)
	}

	result, err := (ListCronJobsTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "ListCronJobs"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobs := result.Details["jobs"].([]map[string]any)
	details := jobs[0]
	if details["running_trace_id"] != registry.List()[0].RunningTraceID {
		t.Fatalf("running_trace_id mismatch: %#v", details)
	}
	if _, ok := details["last_due_at"].(string); !ok {
		t.Fatalf("last_due_at should be RFC3339 string like upstream: %#v", details["last_due_at"])
	}
	if _, ok := details["last_fired_at"].(string); !ok {
		t.Fatalf("last_fired_at should be RFC3339 string like upstream: %#v", details["last_fired_at"])
	}
	if details["last_completed_at"] != nil {
		t.Fatalf("nil last_completed_at should stay null: %#v", details["last_completed_at"])
	}
	if details["id"] != job.ID {
		t.Fatalf("id mismatch: %#v", details)
	}
}

func TestListCronJobsPreservesExplicitEmptyLastErrorLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-empty-error"
schedule = "0 0 1 1 *"
action = "check"
enabled = true
last_error = ""
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := triggers.NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	result, err := (ListCronJobsTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "ListCronJobs"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobs := result.Details["jobs"].([]map[string]any)
	if jobs[0]["last_error"] != "" {
		t.Fatalf("explicit empty last_error should be preserved like upstream: %#v", jobs[0])
	}
	if !strings.Contains(result.Content, "last_error: ") {
		t.Fatalf("rendered list should include explicit empty last_error like upstream: %q", result.Content)
	}
}

func TestRemoveCronJobToolRequiresConfirmation(t *testing.T) {
	registry := triggers.NewScheduledCronRegistry()
	job, err := registry.AddJob("*/10 * * * *", "summarize")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := (RemoveCronJobTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "RemoveCronJob", Arguments: map[string]any{"id": job.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.List()) != 1 || !strings.Contains(preview.Content, "requires confirmation") {
		t.Fatalf("preview mismatch: %#v", preview)
	}
	if preview.Details["confirmation_required"] != true || preview.Details["removed_count"] != 0 || preview.Details["scope"] != "session" || preview.Details["action_preview"] != "summarize" {
		t.Fatalf("preview details mismatch: %#v", preview.Details)
	}
	if _, ok := preview.Details["requires_confirmation"]; ok {
		t.Fatalf("preview should not include legacy requires_confirmation detail: %#v", preview.Details)
	}
	if _, ok := preview.Details["removed"]; ok {
		t.Fatalf("preview should not include legacy removed detail: %#v", preview.Details)
	}
	removed, err := (RemoveCronJobTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "RemoveCronJob", Arguments: map[string]any{"id": job.ID, "confirm": true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.List()) != 0 || removed.Details["removed_count"] != 1 || removed.Details["scope"] != "session" || !strings.Contains(removed.Content, "removed cron job") {
		t.Fatalf("remove mismatch: %#v", removed)
	}
	if _, ok := removed.Details["removed"]; ok {
		t.Fatalf("remove should not include legacy removed detail: %#v", removed.Details)
	}
}

func TestRemoveCronJobToolNonStringIDIsMissingLikeUpstream(t *testing.T) {
	_, err := (RemoveCronJobTool{Registry: triggers.NewScheduledCronRegistry()}).Execute(context.Background(), ai.ToolCall{Name: "RemoveCronJob", Arguments: map[string]any{"id": 123}}, nil)
	if err == nil || err.Error() != "missing required arg: id" {
		t.Fatalf("id error mismatch: %v", err)
	}
}

func TestSetCronJobStateToolDisablesJob(t *testing.T) {
	registry := triggers.NewScheduledCronRegistry()
	job, err := registry.AddJob("*/10 * * * *", "summarize")
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := (SetCronJobStateTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "SetCronJobState", Arguments: map[string]any{"id": job.ID, "enabled": false}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if registry.List()[0].Enabled || disabled.Details["enabled"] != false || !strings.Contains(disabled.Content, "updated cron job") {
		t.Fatalf("disable mismatch: %#v", disabled)
	}
	if !strings.Contains(disabled.Content, "state: disabled") || strings.Contains(disabled.Content, "enabled: false") {
		t.Fatalf("disable content should match upstream state wording: %q", disabled.Content)
	}
	if disabled.Details["scope"] != "session" {
		t.Fatalf("disable details should include session scope: %#v", disabled.Details)
	}
	if _, ok := disabled.Details["audit_entry_id"]; !ok {
		t.Fatalf("disable details should include audit_entry_id: %#v", disabled.Details)
	}
	if _, ok := disabled.Details["action"]; ok {
		t.Fatalf("disable details should not include action like upstream: %#v", disabled.Details)
	}
	_, err = (SetCronJobStateTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "SetCronJobState", Arguments: map[string]any{"id": job.ID, "enabled": true}}, nil)
	if err == nil || !strings.Contains(err.Error(), "enabling cron jobs from model-facing tools requires user confirmation; use /cron enable <id>") {
		t.Fatalf("expected enable confirmation error, got %v", err)
	}
	if registry.List()[0].Enabled {
		t.Fatal("job should remain disabled")
	}
}

func TestSetCronJobStateToolArgErrorsMatchUpstream(t *testing.T) {
	_, err := (SetCronJobStateTool{Registry: triggers.NewScheduledCronRegistry()}).Execute(context.Background(), ai.ToolCall{Name: "SetCronJobState", Arguments: map[string]any{"id": 123, "enabled": false}}, nil)
	if err == nil || err.Error() != "missing required arg: id" {
		t.Fatalf("id error mismatch: %v", err)
	}
	_, err = (SetCronJobStateTool{Registry: triggers.NewScheduledCronRegistry()}).Execute(context.Background(), ai.ToolCall{Name: "SetCronJobState", Arguments: map[string]any{"id": "cron-1", "enabled": "false"}}, nil)
	if err == nil || err.Error() != "missing required arg: enabled" {
		t.Fatalf("enabled error mismatch: %v", err)
	}
}

func TestSetCronJobStateToolRejectsEnableLikeUpstream(t *testing.T) {
	registry := triggers.NewScheduledCronRegistry()
	job, err := registry.AddJob("*/10 * * * *", "summarize")
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.SetJobEnabled(job.ID, false)
	if err != nil {
		t.Fatal(err)
	}

	_, err = (SetCronJobStateTool{Registry: registry}).Execute(context.Background(), ai.ToolCall{Name: "SetCronJobState", Arguments: map[string]any{"id": job.ID, "enabled": true}}, nil)
	if err == nil || !strings.Contains(err.Error(), "enabling cron jobs from model-facing tools requires user confirmation; use /cron enable <id>") {
		t.Fatalf("expected enable confirmation error, got %v", err)
	}
	if registry.List()[0].Enabled {
		t.Fatal("job should remain disabled")
	}
}
