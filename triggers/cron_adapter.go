package triggers

import (
	"fmt"
	"strings"
	"time"

	"github.com/detailyang/pig/bugreport"
)

const cronSubkind = "cron"

const maxScheduledCronActionPreviewChars = 120

type ScheduledCronPayload struct {
	JobID string    `json:"job_id"`
	DueAt time.Time `json:"due_at"`
}

type ScheduledCronAdapter struct {
	registry *ScheduledCronRegistry
	lastScan time.Time
}

func NewScheduledCronAdapter(registry *ScheduledCronRegistry) *ScheduledCronAdapter {
	return &ScheduledCronAdapter{registry: registry}
}

func (adapter *ScheduledCronAdapter) Poll(now time.Time) []Trigger {
	if adapter.lastScan.IsZero() {
		adapter.lastScan = now
		return nil
	}
	triggers := adapter.PollRange(adapter.lastScan, now)
	adapter.lastScan = now
	return triggers
}

func (adapter *ScheduledCronAdapter) PollRange(since, now time.Time) []Trigger {
	if adapter == nil || adapter.registry == nil {
		return nil
	}
	due := adapter.registry.DueJobs(since, now)
	out := make([]Trigger, 0, len(due))
	for _, item := range due {
		out = append(out, CronTriggerForJob(item.Job, item.DueAt, item.Job.RunningTraceID, now))
	}
	return out
}

func CronTriggerForJob(job ScheduledCronJob, dueAt time.Time, traceID string, receivedAt time.Time) Trigger {
	summary := fmt.Sprintf("cron `%s` due at %s: %s", job.ID, dueAt.UTC().Format(time.RFC3339), previewScheduledCronAction(job.Action, maxScheduledCronActionPreviewChars))
	return Trigger{Source: Source{Kind: SourceLocal, Subkind: cronSubkind}, SourceKind: SourceKindLocal, SourceLabel: "Cron", EventLabel: job.ID, PayloadVisibility: PayloadLocal, PayloadSummary: &summary, Payload: ScheduledCronPayload{JobID: job.ID, DueAt: dueAt.UTC()}, IDempotencyKey: fmt.Sprintf("cron:%s:%s", job.ID, dueAt.UTC().Format(time.RFC3339)), ReplacementPolicy: ReplacementDrop, TraceID: traceID, Authority: Authority{PrincipalID: "local-cron", PrincipalLabel: "local cron", CredentialScope: ScopeNone}, ReceivedAt: receivedAt.UTC()}
}

type TriggerDelivery string

const (
	TriggerDeliveryInjectAndRun  TriggerDelivery = "inject_and_run"
	TriggerDeliverySubAgent      TriggerDelivery = "sub_agent"
	TriggerDeliveryInjectSummary TriggerDelivery = "inject_summary"
)

type PromoteAction string

const (
	PromoteNone                       PromoteAction = "none"
	PromoteSummaryNow                 PromoteAction = "promote_summary_now"
	PromoteSummaryWhenSummaryContains PromoteAction = "promote_summary_when_summary_contains"
)

type CronAction struct {
	Prompt                    string
	Promote                   PromoteAction
	PromoteTemplateBody       string
	PromoteRequiredSubstrings []string
	PromoteRequiresApproval   bool
	Delivery                  TriggerDelivery
}

func CronTriggerAction(registry *ScheduledCronRegistry, payload ScheduledCronPayload) CronAction {
	if registry == nil || payload.JobID == "" {
		return CronAction{}
	}
	var job *ScheduledCronJob
	for _, candidate := range registry.List() {
		if candidate.ID == payload.JobID {
			copyJob := candidate
			job = &copyJob
			break
		}
	}
	if job == nil {
		return CronAction{}
	}
	if job.Stateful {
		state := ""
		if storagePath := registry.StoragePath(); storagePath != "" {
			if loaded, ok := ReadLoopState(LoopStatePath(storagePath, job.ID)); ok {
				state = loaded
			}
		}
		return CronAction{Prompt: ComposeStatefulCronPrompt(job.Action, state), Promote: PromoteNone, Delivery: TriggerDeliverySubAgent}
	}
	return CronAction{Prompt: job.Action, Promote: PromoteNone, Delivery: TriggerDeliveryInjectAndRun}
}

func previewScheduledCronAction(action string, max int) string {
	action = bugreport.Redact(strings.TrimSpace(action))
	runes := []rune(action)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return action
}
