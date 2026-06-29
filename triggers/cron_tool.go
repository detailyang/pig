package triggers

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/bugreport"
)

type NewCronJobTool struct {
	Registry *ScheduledCronRegistry
}

type ListCronJobsTool struct {
	Registry *ScheduledCronRegistry
}

type RemoveCronJobTool struct {
	Registry *ScheduledCronRegistry
}

type SetCronJobStateTool struct {
	Registry *ScheduledCronRegistry
}

func (NewCronJobTool) Name() string { return "NewCronJob" }
func (NewCronJobTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionSequential
}
func (NewCronJobTool) Description() string {
	return "Create a session-scoped cron scheduled job. Use this when the user asks for a fixed time, recurring, scheduled, hourly, daily, weekly, crontab, 定时任务, 每小时, 每天, or similar time-based job. Do not use NewTrigger for these scheduled jobs. Cron jobs are scoped to the current chat session by default."
}
func (NewCronJobTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"schedule": map[string]any{"type": "string", "description": "A 5-field cron expression in local time (minute hour day-of-month month day-of-week), or a supported alias such as hourly / every hour / 每小时."},
			"action":   map[string]any{"type": "string", "description": "Natural-language instruction to run when the schedule is due."},
			"stateful": map[string]any{"type": "boolean", "default": false, "description": "Loop mode: run in a fresh sub-agent that keeps persistent notes across runs (injected each time) and routes findings to the triage inbox instead of the chat. Use for recurring watch/triage jobs like \"check for new issues and report only what changed\"."},
		},
		"required":             []string{"schedule", "action"},
		"additionalProperties": false,
	}
}
func (tool NewCronJobTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	registry := cronRegistry(tool.Registry)
	schedule, err := requiredCronStringArg(call, "schedule")
	if err != nil {
		return agent.ToolResult{}, err
	}
	schedule, err = NormalizeScheduledCron(schedule)
	if err != nil {
		return agent.ToolResult{}, err
	}
	action, err := requiredCronStringArg(call, "action")
	if err != nil {
		return agent.ToolResult{}, err
	}
	stateful := boolArgDefault(call, "stateful", false)
	job, err := registry.AddJobFull(schedule, action, stateful)
	if err != nil {
		return agent.ToolResult{}, err
	}
	details := map[string]any{"id": job.ID, "schedule": job.Schedule, "action": job.Action, "enabled": job.Enabled, "stateful": job.Stateful, "scope": "session", "audit_entry_id": nil}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("created cron job %s\nschedule: %s\naction: %s", job.ID, job.Schedule, cronActionPreview(job.Action)), Details: details}, nil
}

func requiredCronStringArg(call ai.ToolCall, key string) (string, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return "", fmt.Errorf("missing required arg: %s", key)
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("missing required arg: %s", key)
	}
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("%s must be valid UTF-8", key)
	}
	return text, nil
}

func requiredCronBoolArg(call ai.ToolCall, key string) (bool, error) {
	value, ok := call.Arguments[key]
	if !ok {
		return false, fmt.Errorf("missing required arg: %s", key)
	}
	typed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("missing required arg: %s", key)
	}
	return typed, nil
}

func (ListCronJobsTool) Name() string { return "ListCronJobs" }
func (ListCronJobsTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (ListCronJobsTool) Description() string {
	return "List the session-scoped cron scheduled jobs. Use this when the user asks to view, list, inspect, or find scheduled jobs, cron jobs, crontab entries, 定时任务, or recurring jobs."
}
func (ListCronJobsTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
}
func (tool ListCronJobsTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	registry := cronRegistry(tool.Registry)
	jobs := registry.List()
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: renderCronJobsForTool(jobs), Details: map[string]any{"count": len(jobs), "scope": "session", "storage_path": registry.StoragePath(), "jobs": cronJobsDetails(jobs)}}, nil
}

func (RemoveCronJobTool) Name() string { return "RemoveCronJob" }
func (RemoveCronJobTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionSequential
}
func (RemoveCronJobTool) Description() string {
	return "Preview or confirm removal of a session-scoped cron scheduled job by exact id. Use confirm=false first when the user asks to delete, remove, or clear a scheduled job, cron job, crontab entry, or 定时任务. Call confirm=true only after the user explicitly confirms removal."
}
func (RemoveCronJobTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      map[string]any{"type": "string", "description": "Exact cron job id, for example cron-abc123."},
			"confirm": map[string]any{"type": "boolean", "description": "false to preview the removal; true only after explicit user confirmation."},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}
func (tool RemoveCronJobTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	registry := cronRegistry(tool.Registry)
	id, err := requiredCronStringArg(call, "id")
	if err != nil {
		return agent.ToolResult{}, err
	}
	job, ok := findCronJob(registry.List(), id)
	if !ok {
		return agent.ToolResult{}, fmt.Errorf("no cron job with id '%s'", id)
	}
	if !boolArgDefault(call, "confirm", false) {
		preview := cronActionPreview(job.Action)
		return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("remove cron job %s requires confirmation\nschedule: %s\naction: %s\ncall RemoveCronJob again with confirm=true only after the user confirms", job.ID, job.Schedule, preview), Details: map[string]any{"id": job.ID, "removed_count": 0, "confirmation_required": true, "scope": "session", "action_preview": preview}}, nil
	}
	removed, err := registry.RemoveJob(id)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if removed == nil {
		return agent.ToolResult{}, fmt.Errorf("no cron job with id '%s'", id)
	}
	details := map[string]any{"id": removed.ID, "removed_count": 1, "scope": "session"}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("removed cron job %s\nschedule: %s\naction: %s", removed.ID, removed.Schedule, cronActionPreview(removed.Action)), Details: details}, nil
}

func (SetCronJobStateTool) Name() string { return "SetCronJobState" }
func (SetCronJobStateTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionSequential
}
func (SetCronJobStateTool) Description() string {
	return "Disable a session-scoped cron scheduled job by exact id. Model-facing enable/resume is refused until control-plane confirmation is wired; use /cron enable <id> for enabling."
}
func (SetCronJobStateTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":      map[string]any{"type": "string", "description": "Exact cron job id, for example cron-abc123."},
			"enabled": map[string]any{"type": "boolean", "description": "true to enable/resume the cron job; false to disable/pause it."},
		},
		"required":             []string{"id", "enabled"},
		"additionalProperties": false,
	}
}
func (tool SetCronJobStateTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	registry := cronRegistry(tool.Registry)
	id, err := requiredCronStringArg(call, "id")
	if err != nil {
		return agent.ToolResult{}, err
	}
	enabled, err := requiredCronBoolArg(call, "enabled")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if enabled {
		return agent.ToolResult{}, fmt.Errorf("enabling cron jobs from model-facing tools requires user confirmation; use /cron enable <id>")
	}
	job, err := registry.SetJobEnabled(id, enabled)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if job == nil {
		return agent.ToolResult{}, fmt.Errorf("no cron job with id '%s'", id)
	}
	state := "disabled"
	if job.Enabled {
		state = "enabled"
	}
	details := map[string]any{"id": job.ID, "schedule": job.Schedule, "enabled": job.Enabled, "stateful": job.Stateful, "scope": "session", "audit_entry_id": nil}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("updated cron job %s\nstate: %s\nschedule: %s\naction: %s", job.ID, state, job.Schedule, cronActionPreview(job.Action)), Details: details}, nil
}

func cronRegistry(registry *ScheduledCronRegistry) *ScheduledCronRegistry {
	if registry != nil {
		return registry
	}
	return NewScheduledCronRegistry()
}

func findCronJob(jobs []ScheduledCronJob, id string) (ScheduledCronJob, bool) {
	for _, job := range jobs {
		if job.ID == id {
			return job, true
		}
	}
	return ScheduledCronJob{}, false
}

func renderCronJobsForTool(jobs []ScheduledCronJob) string {
	if len(jobs) == 0 {
		return "session cron jobs: none"
	}
	now := time.Now().UTC()
	lines := []string{fmt.Sprintf("session cron jobs: %d", len(jobs))}
	for _, job := range jobs {
		state := "disabled"
		if job.Enabled {
			state = "enabled"
		}
		lines = append(lines, fmt.Sprintf("- %s [%s] schedule: %s action: %s", job.ID, state, job.Schedule, cronActionPreview(job.Action)))
		if job.Enabled {
			if nextRun, err := job.NextRunAfter(now); err == nil {
				lines = append(lines, fmt.Sprintf("  next_run: %s", nextRun.Format(time.RFC3339)))
			}
		}
		if job.RunningTraceID != "" {
			lines = append(lines, fmt.Sprintf("  running_trace_id: %s", job.RunningTraceID))
		}
		if job.LastError != "" || job.HasLastError() {
			lines = append(lines, fmt.Sprintf("  last_error: %s", cronActionPreview(job.LastError)))
		}
		if job.SkippedOverlapCount > 0 {
			lines = append(lines, fmt.Sprintf("  skipped_overlap_count: %d", job.SkippedOverlapCount))
		}
	}
	return strings.Join(lines, "\n")
}

func cronJobsDetails(jobs []ScheduledCronJob) []map[string]any {
	details := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		details = append(details, cronJobDetails(job))
	}
	return details
}

func cronJobDetails(job ScheduledCronJob) map[string]any {
	now := time.Now().UTC()
	var nextRun any
	if job.Enabled {
		if run, err := job.NextRunAfter(now); err == nil {
			nextRun = run.Format(time.RFC3339)
		}
	}
	var lastError any
	if job.LastError != "" || job.HasLastError() {
		lastError = cronActionPreview(job.LastError)
	}
	var runningTraceID any
	if job.RunningTraceID != "" {
		runningTraceID = job.RunningTraceID
	}
	return map[string]any{
		"id":                    job.ID,
		"schedule":              job.Schedule,
		"action_preview":        cronActionPreview(job.Action),
		"enabled":               job.Enabled,
		"scope":                 "session",
		"created_at":            job.CreatedAt.Format(time.RFC3339),
		"last_due_at":           cronTimeForTool(job.LastDueAt),
		"last_fired_at":         cronTimeForTool(job.LastFiredAt),
		"last_completed_at":     cronTimeForTool(job.LastCompletedAt),
		"last_error":            lastError,
		"running_trace_id":      runningTraceID,
		"skipped_overlap_count": job.SkippedOverlapCount,
		"next_run":              nextRun,
	}
}

func cronTimeForTool(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func cronActionPreview(action string) string {
	preview := bugreport.Redact(action)
	var builder strings.Builder
	for index, char := range []rune(preview) {
		if index == 120 {
			builder.WriteString("……")
			return builder.String()
		}
		builder.WriteRune(char)
	}
	return builder.String()
}
