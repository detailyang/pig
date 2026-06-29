package triggers

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJobRegistryPersistsCronAndFileJobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trigger-jobs.json")
	registry := NewJobRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	cron, err := registry.AddCron(JobSpec{ID: "standup", Label: "Standup", Prompt: "summarize", Every: time.Minute, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	file, err := registry.AddFile(JobSpec{ID: "watch-readme", Label: "Readme", Path: "README.md", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if cron.Kind != JobKindCron || file.Kind != JobKindFile {
		t.Fatalf("job kinds mismatch: %#v %#v", cron, file)
	}
	if _, err := registry.SetEnabled("standup", false); err != nil {
		t.Fatal(err)
	}

	reloaded := NewJobRegistry()
	if err := reloaded.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	jobs := reloaded.List()
	if len(jobs) != 2 {
		t.Fatalf("job count mismatch: %#v", jobs)
	}
	if job, ok := findJob(jobs, "standup"); !ok || job.Enabled || job.Every != time.Minute {
		t.Fatalf("cron reload mismatch: %#v ok=%v", job, ok)
	}
	if job, ok := findJob(jobs, "watch-readme"); !ok || !job.Enabled || job.Path != "README.md" {
		t.Fatalf("file reload mismatch: %#v ok=%v", job, ok)
	}
}

func TestJobRegistrySaveDoesNotHTMLEscapeLikeSerdeJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trigger-jobs.json")
	registry := NewJobRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddCron(JobSpec{ID: "html", Prompt: "a < b && c > d", Every: time.Minute, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	content := string(mustReadFile(t, path))
	if strings.Contains(content, `\u003c`) || strings.Contains(content, `\u003e`) || strings.Contains(content, `\u0026`) {
		t.Fatalf("job registry JSON should not HTML-escape like serde_json, got %s", content)
	}
	if !strings.Contains(content, `"prompt": "a < b && c > d"`) {
		t.Fatalf("job registry JSON should preserve literal prompt, got %s", content)
	}
}

func TestInboxJSONLDoesNotHTMLEscapeLikeSerdeJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	entry, err := AppendInbox(path, "cron <daily>", "a < b && c > d", "trace-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetInboxStatus(path, entry.ID, InboxStatusClaimed); err != nil {
		t.Fatal(err)
	}
	content := string(mustReadFile(t, path))
	if strings.Contains(content, `\u003c`) || strings.Contains(content, `\u003e`) || strings.Contains(content, `\u0026`) {
		t.Fatalf("inbox JSONL should not HTML-escape like serde_json, got %s", content)
	}
	if !strings.Contains(content, `"source":"cron <daily>"`) || !strings.Contains(content, `"text":"a < b && c > d"`) {
		t.Fatalf("inbox JSONL should preserve literal strings, got %s", content)
	}
}

func TestScheduledCronRegistryClearForTestsMatchesUpstream(t *testing.T) {
	registry := NewScheduledCronRegistry()
	if _, err := registry.AddJob("*/5 * * * *", "summarize"); err != nil {
		t.Fatal(err)
	}
	registry.ClearForTests()
	if len(registry.List()) != 0 || registry.StoragePath() != "" {
		t.Fatalf("clear for tests should reset registry state")
	}
}

func TestJobRegistryBuildsPollersForSupervisor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trigger-jobs.json")
	registry := NewJobRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddCron(JobSpec{ID: "tick", Every: time.Minute, Prompt: "ping", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(t.TempDir(), "watched.txt")
	if _, err := registry.AddFile(JobSpec{ID: "watch", Path: filePath, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	pollers := registry.Pollers()
	if len(pollers) != 2 {
		t.Fatalf("poller count mismatch: %#v", pollers)
	}
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	supervisor := NewSupervisor(SupervisorOptions{Pollers: pollers})
	if got := supervisor.Poll(now); len(got.Results) != 0 {
		t.Fatalf("first poll should prime adapters: %#v", got)
	}
	if err := writeTinyFile(filePath, "one"); err != nil {
		t.Fatal(err)
	}
	result := supervisor.Poll(now.Add(time.Minute))
	if len(result.Accepted) != 2 {
		t.Fatalf("expected cron and file trigger, got %#v", result)
	}
	if !hasSubkind(result.Accepted, "cron") || !hasSubkind(result.Accepted, "file") {
		t.Fatalf("missing accepted subkind: %#v", result.Accepted)
	}
}

func TestJobRegistryRemovePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trigger-jobs.json")
	registry := NewJobRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddCron(JobSpec{ID: "tick", Every: time.Minute, Prompt: "ping", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	removed, err := registry.Remove("tick")
	if err != nil {
		t.Fatal(err)
	}
	if removed.ID != "tick" {
		t.Fatalf("remove mismatch: %#v", removed)
	}
	reloaded := NewJobRegistry()
	if err := reloaded.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 0 {
		t.Fatalf("removed job reloaded: %#v", reloaded.List())
	}
	if _, err := registry.Remove("missing"); err == nil {
		t.Fatal("expected missing job error")
	}
}

func TestScheduledCronRegistryRoundTripsTOMLAndEnableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJobFull("*/10 * * * *", "summarize", true)
	if err != nil {
		t.Fatal(err)
	}
	if !job.Enabled || !job.Stateful || job.Schedule != "*/10 * * * *" || job.Action != "summarize" {
		t.Fatalf("job mismatch: %#v", job)
	}
	updated, err := registry.SetJobEnabled(job.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || updated.Enabled {
		t.Fatalf("disable mismatch: %#v", updated)
	}

	reloaded := NewScheduledCronRegistry()
	if err := reloaded.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	jobs := reloaded.List()
	if len(jobs) != 1 || jobs[0].ID != job.ID || jobs[0].Enabled || !jobs[0].Stateful {
		t.Fatalf("reloaded jobs mismatch: %#v", jobs)
	}
	content := string(mustReadFile(t, path))
	if !strings.Contains(content, "[[jobs]]") || !strings.Contains(content, `schedule = "*/10 * * * *"`) || !strings.Contains(content, `stateful = true`) {
		t.Fatalf("cron TOML mismatch:\n%s", content)
	}
}

func TestScheduledCronRegistryLoadsMultipleTOMLJobsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	content := `[[jobs]]
id = "cron-one"
schedule = "0 * * * *"
action = "first"
enabled = true
stateful = false
created_at = "2026-06-22T08:00:00Z"

[[jobs]]
id = "cron-two"
schedule = "30 * * * *"
action = "second"
enabled = false
stateful = true
created_at = "2026-06-22T09:00:00Z"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	jobs := registry.List()
	if len(jobs) != 2 || jobs[0].ID != "cron-one" || jobs[1].ID != "cron-two" || !jobs[1].Stateful {
		t.Fatalf("jobs mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadRejectsInvalidUTF8LikeUpstreamReadToString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte("[[jobs]]\nid = \"cron-good\"\nschedule = \"* * * * *\"\naction = \"ok\"\nenabled = true\nstateful = false\ncreated_at = \"2026-01-02T03:04:05Z\"\nnote = \"\xff\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 read error like upstream, got %v", err)
	}
}

func TestScheduledCronRegistryLoadAcceptsWhitespaceInJobsHeaderLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[ jobs ]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("jobs header whitespace should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].ID != "cron-good" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadIgnoresUnknownTablesLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[metadata]
version = 1

[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("unknown tables should be ignored like serde: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].ID != "cron-good" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadIgnoresUnknownArrayTablesLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[metadata]]
version = 1

[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("unknown array tables should be ignored like serde: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].ID != "cron-good" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadIgnoresNestedJobsArrayTablesLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[foo.jobs]]
id = "cron-nested"
schedule = "* * * * *"
action = "nested"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"

[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("nested jobs table should be ignored like serde: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].ID != "cron-good" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryRemovePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJob("*/10 * * * *", "summarize")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := registry.RemoveJob(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if removed == nil || removed.ID != job.ID {
		t.Fatalf("remove mismatch: %#v", removed)
	}
	if missing, err := registry.RemoveJob("missing"); err != nil || missing != nil {
		t.Fatalf("missing remove mismatch removed=%#v err=%v", missing, err)
	}
	reloaded := NewScheduledCronRegistry()
	if err := reloaded.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 0 {
		t.Fatalf("removed job reloaded: %#v", reloaded.List())
	}
}

func TestScheduledCronRegistryDueJobsMarksRunningAndSkipsOverlap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJob("* * * * *", "say hello")
	if err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC)

	due := registry.DueJobs(since, now)
	if len(due) != 1 || due[0].Job.ID != job.ID || !due[0].DueAt.Equal(time.Date(2026, 5, 26, 22, 1, 0, 0, time.UTC)) || due[0].Job.RunningTraceID == "" {
		t.Fatalf("due mismatch: %#v", due)
	}
	later := now.Add(time.Minute)
	if skipped := registry.DueJobs(now, later); len(skipped) != 0 {
		t.Fatalf("running job should skip overlap: %#v", skipped)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].SkippedOverlapCount != 1 || jobs[0].LastError != "skipped: previous run still active" {
		t.Fatalf("overlap state mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistrySkippedOverlapCountSaturatesLikeUpstream(t *testing.T) {
	registry := NewScheduledCronRegistry()
	registry.jobs = []ScheduledCronJob{{ID: "cron-running", Schedule: "* * * * *", Action: "say hello", Enabled: true, RunningTraceID: "trace-running", SkippedOverlapCount: uint64(math.MaxUint64), CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}
	if due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC)); len(due) != 0 {
		t.Fatalf("running job should skip overlap: %#v", due)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].SkippedOverlapCount != uint64(math.MaxUint64) {
		t.Fatalf("skipped_overlap_count should saturate: %#v", jobs)
	}
}

func TestScheduledCronAdapterEmitsUpstreamShapedTrigger(t *testing.T) {
	registry := NewScheduledCronRegistry()
	job, err := registry.AddJob("* * * * *", "summarize things")
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewScheduledCronAdapter(registry)
	triggers := adapter.PollRange(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(triggers) != 1 {
		t.Fatalf("trigger count mismatch: %#v", triggers)
	}
	trigger := triggers[0]
	if trigger.Source.Kind != SourceLocal || trigger.Source.Subkind != "cron" || trigger.SourceKind != SourceKindLocal || trigger.SourceLabel != "Cron" || trigger.EventLabel != job.ID {
		t.Fatalf("trigger source mismatch: %#v", trigger)
	}
	if trigger.PayloadVisibility != PayloadLocal || trigger.ReplacementPolicy != ReplacementDrop || trigger.Authority.PrincipalID != "local-cron" || trigger.Authority.CredentialScope != ScopeNone {
		t.Fatalf("trigger policy mismatch: %#v", trigger)
	}
	if trigger.PayloadSummary == nil || !strings.Contains(*trigger.PayloadSummary, job.ID) || !strings.Contains(*trigger.PayloadSummary, "summarize things") {
		t.Fatalf("trigger summary mismatch: %#v", trigger.PayloadSummary)
	}
	payload, ok := trigger.Payload.(ScheduledCronPayload)
	if !ok || payload.JobID != job.ID || payload.DueAt.IsZero() {
		t.Fatalf("payload mismatch: %#v", trigger.Payload)
	}
	if trigger.IDempotencyKey != "cron:"+job.ID+":"+payload.DueAt.Format(time.RFC3339) || trigger.TraceID == "" {
		t.Fatalf("idempotency/trace mismatch: %#v", trigger)
	}
}

func TestScheduledCronAdapterRedactsSecretLikeActionText(t *testing.T) {
	registry := NewScheduledCronRegistry()
	secret := "sk-abcdefghijklmnopqrstuvwxyz123456"
	bearer := "Bearer abcdefghijklmnopqrstuvwxyz"
	if _, err := registry.AddJob("* * * * *", "use token "+secret+" and "+bearer); err != nil {
		t.Fatal(err)
	}
	adapter := NewScheduledCronAdapter(registry)
	triggers := adapter.PollRange(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(triggers) != 1 || triggers[0].PayloadSummary == nil {
		t.Fatalf("trigger mismatch: %#v", triggers)
	}
	record := RecordReceivedFrom(triggers[0])
	if record.PayloadSummary == nil {
		t.Fatal("missing payload summary")
	}
	summary := *record.PayloadSummary
	if strings.Contains(summary, secret) || strings.Contains(summary, bearer) || !strings.Contains(summary, "[REDACTED:") {
		t.Fatalf("secret leaked in summary: %s", summary)
	}
}

func TestScheduledCronAdapterPollPrimesThenEmits(t *testing.T) {
	registry := NewScheduledCronRegistry()
	job, err := registry.AddJob("* * * * *", "summarize things")
	if err != nil {
		t.Fatal(err)
	}
	adapter := NewScheduledCronAdapter(registry)
	now := time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC)
	if got := adapter.Poll(now); len(got) != 0 {
		t.Fatalf("first poll should prime adapter: %#v", got)
	}
	got := adapter.Poll(now.Add(time.Minute + 5*time.Second))
	if len(got) != 1 || got[0].EventLabel != job.ID {
		t.Fatalf("second poll mismatch: %#v", got)
	}
}

func TestScheduledCronNotificationHookStatusMatchesUpstream(t *testing.T) {
	registry := NewScheduledCronRegistry()
	hook := NewScheduledCronNotificationHook(registry)
	if hook.Label() != "cron" {
		t.Fatalf("label mismatch: %q", hook.Label())
	}
	status := hook.Status()
	if status.QueuedCount != 0 || len(status.SubscriptionLabels) != 1 || status.SubscriptionLabels[0] != "local crontab: 0 jobs" {
		t.Fatalf("empty status mismatch: %#v", status)
	}
	first, err := registry.AddJob("* * * * *", "running")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddJob("0 * * * *", "enabled"); err != nil {
		t.Fatal(err)
	}
	if due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC)); len(due) != 1 || due[0].Job.ID != first.ID {
		t.Fatalf("due mismatch: %#v", due)
	}
	status = hook.Status()
	if status.QueuedCount != 1 || len(status.SubscriptionLabels) != 1 || status.SubscriptionLabels[0] != "local crontab: 2 job(s), 2 enabled" {
		t.Fatalf("running status mismatch: %#v", status)
	}
}

func TestScheduledCronNotificationHookRunReportsClosedSinkLikeUpstream(t *testing.T) {
	registry := NewScheduledCronRegistry()
	if _, err := registry.AddJob("* * * * *", "run tests"); err != nil {
		t.Fatal(err)
	}
	hook := NewScheduledCronNotificationHook(registry)
	hook.tickEvery = time.Millisecond
	hook.adapter.lastScan = time.Now().UTC().Add(-2 * time.Minute)
	sink := make(chan Trigger)
	close(sink)
	err := hook.Run(context.Background(), sink)
	if hookErr, ok := err.(HookError); !ok || hookErr.Kind != HookErrorSinkClosed {
		t.Fatalf("expected sink closed hook error, got %#v", err)
	}
	status := hook.Status()
	if status.State.Kind != HookStateDisconnected || status.State.Reason != "sink closed" || status.LastError == nil || *status.LastError != "sink closed" {
		t.Fatalf("sink closed status mismatch: %#v", status)
	}
}

func TestScheduledCronTriggerActionUsesLoopStateForStatefulJobs(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "sess1.cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(sidecar); err != nil {
		t.Fatal(err)
	}
	stateful, err := registry.AddJobFull("* * * * *", "watch things", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteLoopState(LoopStatePath(sidecar, stateful.ID), "seen: #9"); err != nil {
		t.Fatal(err)
	}
	action := CronTriggerAction(registry, ScheduledCronPayload{JobID: stateful.ID})
	if action.Delivery != TriggerDeliverySubAgent || action.Promote != PromoteNone || !strings.Contains(action.Prompt, "seen: #9") || !strings.Contains(action.Prompt, "watch things") {
		t.Fatalf("stateful action mismatch: %#v", action)
	}
	nonStateful, err := registry.AddJob("* * * * *", "plain run")
	if err != nil {
		t.Fatal(err)
	}
	action = CronTriggerAction(registry, ScheduledCronPayload{JobID: nonStateful.ID})
	if action.Delivery != TriggerDeliveryInjectAndRun || action.Prompt != "plain run" || action.Promote != PromoteNone {
		t.Fatalf("plain action mismatch: %#v", action)
	}
}

func TestScheduledCronRegistryParserSupportsRangesListsAndSundayAlias(t *testing.T) {
	registry := NewScheduledCronRegistry()
	if _, err := registry.AddJob("5-10/5 9,17 * * 0,7", "check"); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 6, 21, 8, 59, 0, 0, time.UTC)
	now := time.Date(2026, 6, 21, 9, 5, 0, 0, time.UTC)
	due := registry.DueJobs(since, now)
	if len(due) != 1 || !due[0].DueAt.Equal(time.Date(2026, 6, 21, 9, 5, 0, 0, time.UTC)) {
		t.Fatalf("range/list due mismatch: %#v", due)
	}
}

func TestScheduledCronRegistryUsesLocalTimeAndSkipsCurrentMinute(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("test-local", 2*60*60)
	t.Cleanup(func() { time.Local = oldLocal })
	registry := NewScheduledCronRegistry()
	if _, err := registry.AddJob("5 1 * * *", "local time check"); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 5, 26, 23, 5, 0, 0, time.UTC)
	now := since.Add(24 * time.Hour)
	due := registry.DueJobs(since, now)
	if len(due) != 1 {
		t.Fatalf("expected one local-time due job, got %#v", due)
	}
	if !due[0].DueAt.After(since) || due[0].DueAt.Equal(since) {
		t.Fatalf("due time should be after current minute: %#v", due[0].DueAt)
	}
	localDue := due[0].DueAt.In(time.Local)
	if localDue.Hour() != 1 || localDue.Minute() != 5 {
		t.Fatalf("due time should match local schedule, got utc=%s local=%s", due[0].DueAt, localDue)
	}
}

func TestScheduledCronRegistryRejectsInvalidSchedule(t *testing.T) {
	registry := NewScheduledCronRegistry()
	for _, schedule := range []string{"60 * * * *", "*/0 * * * *", "10-5 * * * *", "* * *", "every hour"} {
		if _, err := registry.AddJob(schedule, "check"); err == nil {
			t.Fatalf("expected invalid schedule error for %q", schedule)
		}
	}
}

func TestScheduledCronRegistryAcceptsParseableScheduleWithNoFiveYearRunLikeUpstream(t *testing.T) {
	registry := NewScheduledCronRegistry()
	job, err := registry.AddJob("0 0 31 2 *", "check leap impossibility")
	if err != nil {
		t.Fatalf("parseable cron should be accepted even if it has no next run within five years: %v", err)
	}
	if job.Schedule != "0 0 31 2 *" {
		t.Fatalf("schedule mismatch: %#v", job)
	}
}

func TestScheduledCronDueJobsReportsNoNextRunLikeUpstream(t *testing.T) {
	registry := NewScheduledCronRegistry()
	if _, err := registry.AddJob("0 0 31 2 *", "check leap impossibility"); err != nil {
		t.Fatal(err)
	}
	if due := registry.DueJobs(time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 22, 0, 1, 0, 0, time.UTC)); len(due) != 0 {
		t.Fatalf("expected no due jobs, got %#v", due)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].LastError != "no next run within 5 years" {
		t.Fatalf("last_error should match upstream no-next-run message: %#v", jobs)
	}
}

func TestNormalizeScheduledCronAliases(t *testing.T) {
	cases := map[string]string{
		"hourly":      "0 * * * *",
		"every hour":  "0 * * * *",
		"daily":       "0 9 * * *",
		"every week":  "0 9 * * 1",
		"每小时提醒我":      "0 * * * *",
		"每天总结":        "0 9 * * *",
		"每週回顾":        "0 9 * * 1",
		"*/5 * * * *": "*/5 * * * *",
	}
	for input, want := range cases {
		got, err := NormalizeScheduledCron(input)
		if err != nil {
			t.Fatalf("NormalizeScheduledCron(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeScheduledCron(%q)=%q want %q", input, got, want)
		}
	}
	if _, err := NormalizeScheduledCron("1 2 3 4"); err == nil || err.Error() != "invalid schedule: provide a 5-field cron expression, or a supported alias such as hourly / every hour / 每小时" {
		t.Fatalf("expected upstream invalid schedule error, got %v", err)
	}
	if _, err := NormalizeScheduledCron("someday maybe"); err == nil || err.Error() != "invalid schedule: provide a 5-field cron expression, or a supported alias such as hourly / every hour / 每小时" {
		t.Fatalf("expected invalid alias error, got %v", err)
	}
}

func TestScheduledCronRegistryAddJobScheduleErrorsMatchUpstream(t *testing.T) {
	registry := NewScheduledCronRegistry()
	cases := map[string]string{
		"1 2 3 4":      "cron schedule must have 5 fields: minute hour day-of-month month day-of-week",
		"60 * * * *":   "invalid cron field `60`: value 60 outside 0-59",
		"*/0 * * * *":  "invalid cron field `*/0`: step must be at least 1",
		"*/x * * * *":  "invalid cron field `*/x`: step must be a positive integer",
		"10-5 * * * *": "invalid cron field `10-5`: range start must be <= range end",
		", * * * *":    "invalid cron field `,`: empty item",
		"x * * * *":    "invalid cron field `x`: `x` is not a number",
		"-1 * * * *":   "invalid cron field `-1`: `` is not a number",
		"+1 * * * *":   "invalid cron field `+1`: `+1` is not a number",
	}
	for schedule, want := range cases {
		_, err := registry.AddJob(schedule, "summarize")
		if err == nil || err.Error() != want {
			t.Fatalf("schedule %q error mismatch:\nwant %q\n got %v", schedule, want, err)
		}
	}
}

func TestScheduledCronRegistryRejectsOversizedActionLikeUpstream(t *testing.T) {
	registry := NewScheduledCronRegistry()
	if _, err := registry.AddJob("* * * * *", strings.Repeat("x", maxScheduledCronActionBytes)); err != nil {
		t.Fatalf("max-sized action should be accepted: %v", err)
	}
	_, err := registry.AddJob("* * * * *", strings.Repeat("x", maxScheduledCronActionBytes+1))
	if err == nil || err.Error() != "cron action exceeds 4096 bytes" {
		t.Fatalf("oversized action error mismatch: %v", err)
	}
}

func TestScheduledCronRegistryValidatesActionBeforeScheduleLikeUpstream(t *testing.T) {
	registry := NewScheduledCronRegistry()
	_, err := registry.AddJob("not a cron", "")
	if err == nil || err.Error() != "cron action cannot be empty" {
		t.Fatalf("empty action should win over invalid schedule, got %v", err)
	}
	_, err = registry.AddJob("not a cron", strings.Repeat("x", maxScheduledCronActionBytes+1))
	if err == nil || err.Error() != "cron action exceeds 4096 bytes" {
		t.Fatalf("oversized action should win over invalid schedule, got %v", err)
	}
}

func TestStatefulCronPromptInjectsPreviousStateAndProtocol(t *testing.T) {
	prompt := ComposeStatefulCronPrompt("check the issues", "baseline: #1 #2")
	if !strings.Contains(prompt, "[loop-state]") || !strings.Contains(prompt, "baseline: #1 #2") || !strings.Contains(prompt, "check the issues") || !strings.Contains(prompt, "<loop-state>") || !strings.Contains(prompt, "<inbox>") {
		t.Fatalf("prompt missing state/action/protocol: %s", prompt)
	}
	if ComposeStatefulPrompt("check the issues", "baseline: #1 #2") != prompt {
		t.Fatalf("upstream-named prompt helper should match cron prompt helper")
	}
	first := ComposeStatefulCronPrompt("check", "")
	if !strings.Contains(first, "(first run)") {
		t.Fatalf("first prompt missing first-run marker: %s", first)
	}
}

func TestStatefulCronTagExtractionHandlesAbsentTruncatedAndCaps(t *testing.T) {
	text := "did work\n<inbox>finding one</inbox>\nmore\n<inbox>finding two</inbox>\n<loop-state>seen: a,b</loop-state>"
	if got := ExtractCronTagBlock(text, "loop-state"); got != "seen: a,b" {
		t.Fatalf("loop-state mismatch: %q", got)
	}
	if got, ok := ExtractTagBlock(text, "loop-state"); !ok || got != "seen: a,b" {
		t.Fatalf("upstream-named loop-state mismatch: got=%q ok=%v", got, ok)
	}
	gotInbox := ExtractCronTagAll(text, "inbox", 16)
	if len(gotInbox) != 2 || gotInbox[0] != "finding one" || gotInbox[1] != "finding two" {
		t.Fatalf("inbox mismatch: %#v", gotInbox)
	}
	gotInbox = ExtractTagAll(text, "inbox", 16)
	if len(gotInbox) != 2 || gotInbox[0] != "finding one" || gotInbox[1] != "finding two" {
		t.Fatalf("upstream-named inbox mismatch: %#v", gotInbox)
	}
	if got := ExtractCronTagBlock("no tags here", "loop-state"); got != "" {
		t.Fatalf("absent tag mismatch: %q", got)
	}
	if got, ok := ExtractTagBlock("no tags here", "loop-state"); ok || got != "" {
		t.Fatalf("upstream-named absent tag mismatch: got=%q ok=%v", got, ok)
	}
	if got := ExtractCronTagBlock("x <loop-state>cut off", "loop-state"); got != "" {
		t.Fatalf("truncated tag mismatch: %q", got)
	}
	many := ""
	for index := 0; index < 30; index++ {
		many += fmt.Sprintf("<inbox>f%d</inbox>", index)
	}
	if got := ExtractCronTagAll(many, "inbox", 16); len(got) != 16 {
		t.Fatalf("cap mismatch: %d", len(got))
	}
}

func TestStripLoopProtocolTagsRemovesPersistedBlocks(t *testing.T) {
	text := "checked\n<inbox>issue #9</inbox>\n\nmore\n<loop-state>seen #9</loop-state>\ndone"
	got := StripLoopProtocolTags(text)
	if strings.Contains(got, "<inbox>") || strings.Contains(got, "<loop-state>") || !strings.Contains(got, "checked") || !strings.Contains(got, "more") || !strings.Contains(got, "done") {
		t.Fatalf("stripped text mismatch: %q", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("stripped text should collapse blank residue: %q", got)
	}
	plain := "line one\n\nline two"
	if got := StripLoopProtocolTags(plain); got != plain {
		t.Fatalf("plain text should remain untouched: %q", got)
	}
}

func TestStatefulCronLoopStatePathAndCap(t *testing.T) {
	sidecar := filepath.Join(t.TempDir(), "019abc.cron.toml")
	path := LoopStatePath(sidecar, "cron-1234567890abcdef")
	if filepath.Base(path) != "019abc.loop-cron-12345678.md" {
		t.Fatalf("loop state path mismatch: %s", path)
	}
	if err := WriteLoopState(path, strings.Repeat("x", LoopStateMaxChars+10)); err != nil {
		t.Fatal(err)
	}
	state, ok := ReadLoopState(path)
	if !ok {
		t.Fatal("expected loop state")
	}
	if len([]rune(state)) != LoopStateMaxChars+1 || !strings.HasSuffix(state, "…") {
		t.Fatalf("state cap mismatch len=%d suffix=%q", len([]rune(state)), state[len(state)-3:])
	}
	if err := WriteLoopState(path, "  next baseline  \n"); err != nil {
		t.Fatal(err)
	}
	state, ok = ReadLoopState(path)
	if !ok || state != "next baseline" {
		t.Fatalf("trimmed state mismatch state=%q ok=%v", state, ok)
	}
}

func TestReadLoopStateInvalidUTF8ReturnsNoneLikeUpstreamReadToString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.loop-cron-12345678.md")
	if err := os.WriteFile(path, []byte("previous\xffstate"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state, ok := ReadLoopState(path); ok {
		t.Fatalf("invalid UTF-8 loop state should be ignored like upstream, got %q", state)
	}
}

func TestInboxAppendListAndStatusRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	if NewInboxCount(path) != 0 {
		t.Fatal("missing inbox should count as zero")
	}
	first, err := AppendInbox(path, "cron:job-1", "found a flaky test", "trace-a", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := AppendInbox(path, "cron:job-2", "  PR #9 needs rebase  ", "trace-b", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || second.Text != "PR #9 needs rebase" {
		t.Fatalf("append mismatch first=%#v second=%#v", first, second)
	}
	entries, err := ListNewInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != first.ID || NewInboxCount(path) != 2 {
		t.Fatalf("list mismatch entries=%#v count=%d", entries, NewInboxCount(path))
	}
	claimed, err := SetInboxStatus(path, first.ID, InboxStatusClaimed)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.Status != InboxStatusClaimed || NewInboxCount(path) != 1 {
		t.Fatalf("claim mismatch claimed=%#v count=%d", claimed, NewInboxCount(path))
	}
	dismissed, err := DismissAllNewInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if dismissed != 1 || NewInboxCount(path) != 0 {
		t.Fatalf("dismiss mismatch dismissed=%d count=%d", dismissed, NewInboxCount(path))
	}
	all, err := ListInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("history should be preserved: %#v", all)
	}
}

func TestInboxUpstreamShortAPINames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	if DefaultInboxPath() == "" {
		t.Fatal("default inbox path should be set")
	}
	entry, err := Append(path, "cron:job", "hello", "trace", "sess")
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(entry.Text)) > MaxEntryTextChars+1 {
		t.Fatalf("entry text cap mismatch: %#v", entry)
	}
	newEntries, err := ListNew(path)
	if err != nil || len(newEntries) != 1 || NewCount(path) != 1 {
		t.Fatalf("list new mismatch entries=%#v count=%d err=%v", newEntries, NewCount(path), err)
	}
	updated, err := SetStatus(path, entry.ID, InboxStatusClaimed)
	if err != nil || updated == nil || updated.Status != InboxStatusClaimed {
		t.Fatalf("set status mismatch updated=%#v err=%v", updated, err)
	}
	second, err := Append(path, "cron:job", "second", "trace-2", "sess")
	if err != nil || second.ID == "" {
		t.Fatalf("second append mismatch entry=%#v err=%v", second, err)
	}
	dismissed, err := DismissAllNew(path)
	if err != nil || dismissed != 1 || NewCount(path) != 0 {
		t.Fatalf("dismiss all mismatch dismissed=%d count=%d err=%v", dismissed, NewCount(path), err)
	}
	all, err := List(path)
	if err != nil || len(all) != 2 {
		t.Fatalf("list mismatch entries=%#v err=%v", all, err)
	}
}

func TestListInboxInvalidUTF8ReturnsReadErrorLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	line := `{"id":"inb-1","created_at":"2026-01-02T03:04:05Z","source":"cron","text":"bad` + "\xff" + `","trace_id":"trace","session_id":"sess","status":"new"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListInbox(path); err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 read error like upstream, got %v", err)
	}
	if count := NewInboxCount(path); count != 0 {
		t.Fatalf("new inbox count should hide unreadable inbox errors, got %d", count)
	}
}

func TestInboxSetStatusUpdatesDuplicateIDsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	entries := []InboxEntry{
		{ID: "inb-dup", CreatedAt: "2026-01-01T00:00:00Z", Source: "cron:a", Text: "first", TraceID: "trace-a", SessionID: "sess", Status: InboxStatusNew},
		{ID: "inb-other", CreatedAt: "2026-01-01T00:00:01Z", Source: "cron:b", Text: "other", TraceID: "trace-b", SessionID: "sess", Status: InboxStatusNew},
		{ID: "inb-dup", CreatedAt: "2026-01-01T00:00:02Z", Source: "cron:c", Text: "last", TraceID: "trace-c", SessionID: "sess", Status: InboxStatusNew},
	}
	if err := writeInboxEntries(path, entries); err != nil {
		t.Fatal(err)
	}

	updated, err := SetInboxStatus(path, "inb-dup", InboxStatusClaimed)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || updated.Text != "last" || updated.Status != InboxStatusClaimed {
		t.Fatalf("expected last duplicate updated entry, got %#v", updated)
	}
	all, err := ListInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if all[0].Status != InboxStatusClaimed || all[1].Status != InboxStatusNew || all[2].Status != InboxStatusClaimed {
		t.Fatalf("all duplicate ids should be updated like upstream, got %#v", all)
	}
}

func TestInboxCapsOversizedTextAndSkipsCorruptLines(t *testing.T) {
	if MAX_ENTRY_TEXT_CHARS != MaxInboxEntryTextChars {
		t.Fatalf("inbox text cap alias mismatch: %d", MAX_ENTRY_TEXT_CHARS)
	}

	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	longSource := strings.Repeat("源", 81)
	longText := strings.Repeat("x", 2000)
	entry, err := AppendInbox(path, longSource, longText, "t", "s")
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(entry.Source)) != 80 || strings.HasSuffix(entry.Source, "…") {
		t.Fatalf("source should truncate to 80 chars without ellipsis like upstream: %q", entry.Source)
	}
	if len([]rune(entry.Text)) != MaxInboxEntryTextChars+1 || !strings.HasSuffix(entry.Text, "…") {
		t.Fatalf("capped text mismatch len=%d suffix=%q", len([]rune(entry.Text)), entry.Text[len(entry.Text)-3:])
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{not json\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendInbox(path, "cron:j", "after corruption", "t2", "s"); err != nil {
		t.Fatal(err)
	}
	entries, err := ListInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Text != "after corruption" {
		t.Fatalf("corrupt line should be skipped: %#v", entries)
	}
}

func TestInboxSkipsOversizedCorruptLineLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	before, err := AppendInbox(path, "cron:before", "before", "trace-before", "sess")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.Repeat("x", 128*1024) + "\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := AppendInbox(path, "cron:after", "after", "trace-after", "sess")
	if err != nil {
		t.Fatal(err)
	}

	entries, err := ListInbox(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != before.ID || entries[1].ID != after.ID {
		t.Fatalf("oversized corrupt line should be skipped, got %#v", entries)
	}
}

func TestHandleStatefulCronCompletionPersistsStateInboxAndClearsTrace(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "sess1.cron.toml")
	inboxPath := filepath.Join(dir, "inbox.jsonl")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(sidecar); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJobFull("* * * * *", "watch things", true)
	if err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("due mismatch: %#v", due)
	}
	traceID := due[0].Job.RunningTraceID
	result, err := HandleStatefulCronCompletion(registry, StatefulCronCompletionOptions{CronSidecarPath: sidecar, InboxPath: inboxPath, TraceID: traceID, Summary: "checked. <inbox>issue #9 looks stuck</inbox> done <loop-state>seen: #9</loop-state>", SessionID: "override-session"})
	if err != nil {
		t.Fatal(err)
	}
	if result.LoopState != "seen: #9" || len(result.InboxEntries) != 1 {
		t.Fatalf("completion result mismatch: %#v", result)
	}
	state, ok := ReadLoopState(LoopStatePath(sidecar, job.ID))
	if !ok || state != "seen: #9" {
		t.Fatalf("loop state mismatch state=%q ok=%v", state, ok)
	}
	entries, err := ListNewInbox(inboxPath)
	if err != nil {
		t.Fatal(err)
	}
	wantSource := "cron:" + string([]rune(job.ID)[:13])
	if len(entries) != 1 || !strings.Contains(entries[0].Text, "issue #9") || entries[0].TraceID != traceID || entries[0].Source != wantSource {
		t.Fatalf("inbox mismatch: %#v", entries)
	}
	if entries[0].SessionID != "sess1" {
		t.Fatalf("inbox session should come from cron sidecar stem like upstream: %#v", entries[0])
	}
	if running := registry.List()[0].RunningTraceID; running != "" {
		t.Fatalf("job should be completed, running trace=%q", running)
	}
}

func TestHandleStatefulCronCompletionPersistsEmptyLoopStateLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "sess1.cron.toml")
	inboxPath := filepath.Join(dir, "inbox.jsonl")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(sidecar); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJobFull("* * * * *", "watch things", true)
	if err != nil {
		t.Fatal(err)
	}
	statePath := LoopStatePath(sidecar, job.ID)
	if err := WriteLoopState(statePath, "previous state"); err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("due mismatch: %#v", due)
	}
	result, err := HandleStatefulCronCompletion(registry, StatefulCronCompletionOptions{CronSidecarPath: sidecar, InboxPath: inboxPath, TraceID: due[0].Job.RunningTraceID, Summary: "done <loop-state>   </loop-state>", SessionID: "sess1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.LoopState != "" {
		t.Fatalf("empty loop-state result mismatch: %#v", result)
	}
	state, ok := ReadLoopState(statePath)
	if !ok || state != "" {
		t.Fatalf("empty loop-state should replace previous state, got state=%q ok=%v", state, ok)
	}
}

func TestHandleStatefulCronCompletionContinuesWhenLoopStateWriteFailsLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "sess1.cron.toml")
	inboxPath := filepath.Join(dir, "inbox.jsonl")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(sidecar); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJobFull("* * * * *", "watch things", true)
	if err != nil {
		t.Fatal(err)
	}
	statePath := LoopStatePath(sidecar, job.ID)
	if err := os.Mkdir(statePath, 0o755); err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("due mismatch: %#v", due)
	}
	result, err := HandleStatefulCronCompletion(registry, StatefulCronCompletionOptions{CronSidecarPath: sidecar, InboxPath: inboxPath, TraceID: due[0].Job.RunningTraceID, Summary: "done <inbox>issue #10</inbox><loop-state>new state</loop-state>", SessionID: "sess1"})
	if err != nil {
		t.Fatalf("loop-state write failure should not stop completion: %v", err)
	}
	if result.LoopState != "" || len(result.InboxEntries) != 1 {
		t.Fatalf("completion should skip failed loop-state but keep inbox: %#v", result)
	}
	entries, err := ListNewInbox(inboxPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Text != "issue #10" {
		t.Fatalf("inbox should still be appended: %#v", entries)
	}
}

func TestHandleStatefulCronCompletionContinuesWhenInboxAppendFailsLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "sess1.cron.toml")
	inboxPath := filepath.Join(dir, "inbox.jsonl")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(sidecar); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddJobFull("* * * * *", "watch things", true); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(inboxPath, 0o755); err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("due mismatch: %#v", due)
	}
	result, err := HandleStatefulCronCompletion(registry, StatefulCronCompletionOptions{CronSidecarPath: sidecar, InboxPath: inboxPath, TraceID: due[0].Job.RunningTraceID, Summary: "done <inbox>issue #11</inbox><loop-state>new state</loop-state>", SessionID: "sess1"})
	if err != nil {
		t.Fatalf("inbox append failure should not stop completion: %v", err)
	}
	if result.LoopState != "new state" || len(result.InboxEntries) != 0 {
		t.Fatalf("completion should keep loop-state and skip failed inbox: %#v", result)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].RunningTraceID != "" || jobs[0].LastCompletedAt == nil {
		t.Fatalf("job should still be completed: %#v", jobs)
	}
}

func TestHandleStatefulCronCompletionRecordsErrorWithoutInbox(t *testing.T) {
	dir := t.TempDir()
	sidecar := filepath.Join(dir, "sess1.cron.toml")
	inboxPath := filepath.Join(dir, "inbox.jsonl")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(sidecar); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddJobFull("* * * * *", "watch things", true); err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("due mismatch: %#v", due)
	}
	traceID := due[0].Job.RunningTraceID
	result, err := HandleStatefulCronCompletion(registry, StatefulCronCompletionOptions{CronSidecarPath: sidecar, InboxPath: inboxPath, TraceID: traceID, Summary: "<inbox>should not persist</inbox><loop-state>ignore</loop-state>", SessionID: "sess1", Error: "agent failed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.InboxEntries) != 0 || result.LoopState != "" {
		t.Fatalf("error completion should not persist tags: %#v", result)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].RunningTraceID != "" || jobs[0].LastError != "agent failed" || jobs[0].LastCompletedAt == nil {
		t.Fatalf("error completion state mismatch: %#v", jobs)
	}
	if NewInboxCount(inboxPath) != 0 {
		t.Fatalf("error completion should not append inbox")
	}
}

func TestScheduledCronRegistryMarksCompletedByTrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJob("* * * * *", "say hello")
	if err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("due mismatch: %#v", due)
	}
	traceID := due[0].Job.RunningTraceID
	if running := registry.JobForTrace(traceID); running == nil || running.ID != job.ID {
		t.Fatalf("running job mismatch: %#v", running)
	}

	registry.MarkCompleted(traceID, "failed")
	if running := registry.JobForTrace(traceID); running != nil {
		t.Fatalf("completed job should no longer be running: %#v", running)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].RunningTraceID != "" || jobs[0].LastError != "failed" || jobs[0].LastCompletedAt == nil {
		t.Fatalf("completion state mismatch: %#v", jobs)
	}
	reloaded := NewScheduledCronRegistry()
	if err := reloaded.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.List(); len(got) != 1 || got[0].RunningTraceID != "" || got[0].LastError != "failed" || got[0].LastCompletedAt == nil {
		t.Fatalf("persisted completion mismatch: %#v", got)
	}
}

func TestCronUpstreamExportedNames(t *testing.T) {
	registry := GlobalCronRegistry()
	if registry != global_cron_registry() {
		t.Fatal("global cron registry aliases should return same pointer")
	}
	var cronRegistry *CronRegistry = registry
	job, err := cronRegistry.AddJob("0 * * * *", "summarize")
	if err != nil {
		t.Fatal(err)
	}
	var cronJob CronJob = job
	if cronJob.Schedule != "0 * * * *" || cronJob.Action != "summarize" {
		t.Fatalf("cron job alias mismatch: %#v", cronJob)
	}
	hook := NewCronNotificationHook(cronRegistry)
	if hook.Label() != "cron" {
		t.Fatalf("cron hook label mismatch: %q", hook.Label())
	}
	audit := CronControlPlaneAudit("add", "user", nil, &cronJob)
	if audit["op"] != "add" || audit["actor"] != "user" || audit["after"] == nil {
		t.Fatalf("audit mismatch: %#v", audit)
	}
	var _ AddCronJobError = err
	var _ CronStorageError = err
	var _ CronScheduleError = err
}

func TestCronHookUpstreamFunctionNames(t *testing.T) {
	registry := NewCronRegistry()
	job, err := registry.AddJob("0 * * * *", "summarize")
	if err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC), time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC))
	if len(due) != 1 || due[0].Job.RunningTraceID == "" {
		t.Fatalf("due job mismatch: %#v", due)
	}
	trigger := CronTriggerForJob(job, due[0].DueAt, due[0].Job.RunningTraceID, time.Now().UTC())
	hook := CronActionHook(registry, nil)
	action := hook(BeforeTriggerActionContext{Trigger: trigger})
	if action.Prompt != "summarize" || action.Delivery != TriggerDeliveryInjectAndRun {
		t.Fatalf("cron action hook mismatch: %#v", action)
	}
	if cron_action_hook(registry, nil)(BeforeTriggerActionContext{Trigger: trigger}).Prompt != "summarize" {
		t.Fatal("snake_case cron action alias mismatch")
	}
	listener := CronHarnessListener(registry, "", "")
	result := listener(HarnessEvent{TraceID: due[0].Job.RunningTraceID, Summary: "done"})
	if result.Cron.Job == nil || result.Cron.Job.ID != job.ID {
		t.Fatalf("cron listener mismatch: %#v", result)
	}
	if cron_harness_listener(registry, "", "")(HarnessEvent{TraceID: "trace-cron", Summary: "done"}).Cron.Job != nil {
		t.Fatal("second listener call should not find completed trace")
	}
}

func TestScheduledCronRegistryMarkCompletedUpdatesMemoryWhenPersistenceFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddJob("* * * * *", "say hello"); err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("due mismatch: %#v", due)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	registry.MarkCompleted(due[0].Job.RunningTraceID, "done")
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].RunningTraceID != "" || jobs[0].LastError != "done" || jobs[0].LastCompletedAt == nil {
		t.Fatalf("memory state should update even when persistence fails: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadClearsStaleRunningState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJob("* * * * *", "say hello")
	if err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(due) != 1 || registry.List()[0].RunningTraceID == "" {
		t.Fatalf("expected running job: due=%#v jobs=%#v", due, registry.List())
	}
	reloaded := NewScheduledCronRegistry()
	if err := reloaded.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	jobs := reloaded.List()
	if len(jobs) != 1 || jobs[0].ID != job.ID || jobs[0].RunningTraceID != "" || jobs[0].LastError != "cleared stale running state on startup" {
		t.Fatalf("reloaded stale state mismatch: %#v", jobs)
	}
	persisted := NewScheduledCronRegistry()
	if err := persisted.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if got := persisted.List(); len(got) != 1 || got[0].RunningTraceID != "" {
		t.Fatalf("stale state should be persisted cleared: %#v", got)
	}
}

func TestScheduledCronRegistryLoadClearsEmptyRunningTraceIDLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-empty-running"
schedule = "* * * * *"
action = "check"
enabled = true
running_trace_id = ""
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].LastError != "cleared stale running state on startup" {
		t.Fatalf("empty Some running_trace_id should be cleared like upstream: %#v", jobs)
	}
}

func TestScheduledCronRegistryPersistsExplicitEmptyLastErrorLikeUpstream(t *testing.T) {
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
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	job := registry.List()[0]
	if _, err := registry.SetJobEnabled(job.ID, false); err != nil {
		t.Fatal(err)
	}
	content := string(mustReadFile(t, path))
	if !strings.Contains(content, `last_error = ""`) {
		t.Fatalf("explicit empty last_error should survive persistence like upstream:\n%s", content)
	}
}

func TestScheduledCronRegistryDueJobClearsPersistedLastErrorLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-clear-error"
schedule = "* * * * *"
action = "check"
enabled = true
last_error = ""
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 0, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("expected due job, got %#v", due)
	}
	content := string(mustReadFile(t, path))
	if strings.Contains(content, `last_error`) {
		t.Fatalf("successful due job should clear last_error option like upstream:\n%s", content)
	}
}

func TestScheduledCronRegistryMarkCompletedClearsLastErrorOnSuccessLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddJob("* * * * *", "check"); err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 0, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("expected due job, got %#v", due)
	}
	registry.MarkCompleted(due[0].Job.RunningTraceID, "failed once")
	due = registry.DueJobs(time.Date(2026, 5, 26, 22, 1, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 2, 0, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("expected second due job, got %#v", due)
	}
	registry.MarkCompleted(due[0].Job.RunningTraceID, "")
	content := string(mustReadFile(t, path))
	if strings.Contains(content, `last_error`) {
		t.Fatalf("successful completion should clear last_error option like upstream:\n%s", content)
	}
}

func TestScheduledCronRegistryMarkCompletedPersistsErrorLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddJob("* * * * *", "check"); err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 0, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("expected due job, got %#v", due)
	}
	registry.MarkCompleted(due[0].Job.RunningTraceID, "agent failed")
	content := string(mustReadFile(t, path))
	if !strings.Contains(content, `last_error = "agent failed"`) {
		t.Fatalf("failed completion should persist last_error like upstream:\n%s", content)
	}
}

func TestScheduledCronRegistryEmptyTraceDoesNotMatchIdleJobLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddJob("* * * * *", "check"); err != nil {
		t.Fatal(err)
	}
	if job := registry.JobForTrace(""); job != nil {
		t.Fatalf("empty trace should not match idle job like upstream: %#v", job)
	}
	registry.MarkCompleted("", "should not attach")
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].LastCompletedAt != nil || jobs[0].LastError != "" {
		t.Fatalf("empty trace completion should not mutate idle job: %#v", jobs)
	}
}

func TestScheduledCronRegistryDisableClearsRunningTraceOptionLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-running"
schedule = "* * * * *"
action = "check"
enabled = true
running_trace_id = "trace-existing"
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetJobEnabled("cron-running", false); err != nil {
		t.Fatal(err)
	}
	content := string(mustReadFile(t, path))
	if strings.Contains(content, `running_trace_id`) {
		t.Fatalf("disable should clear running_trace_id option like upstream:\n%s", content)
	}
}

func TestScheduledCronRegistryLoadRejectsInvalidSchedule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "60 * * * *"
action = "bad"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected invalid schedule error")
	}
}

func TestScheduledCronRegistryLoadRejectsInvalidBoolLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad"
enabled = "yes"
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected invalid bool parse error")
	}
}

func TestScheduledCronRegistryLoadRejectsInvalidSkippedOverlapCountLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad"
enabled = true
skipped_overlap_count = "many"
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected invalid skipped_overlap_count parse error")
	}
}

func TestScheduledCronRegistryLoadAcceptsSignedSkippedOverlapCountLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
skipped_overlap_count = +1
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("leading plus integer should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].SkippedOverlapCount != 1 {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsUnderscoreSkippedOverlapCountLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
skipped_overlap_count = 1_000
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("underscore integer should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].SkippedOverlapCount != 1000 {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsHexSkippedOverlapCountLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
skipped_overlap_count = 0x10
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("hex integer should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].SkippedOverlapCount != 16 {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsOctalAndBinarySkippedOverlapCountLikeUpstream(t *testing.T) {
	for value, want := range map[string]uint64{"0o10": 8, "0b10": 2} {
		path := filepath.Join(t.TempDir(), "cron.toml")
		content := strings.ReplaceAll(`[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
skipped_overlap_count = VALUE
stateful = false
created_at = "2026-01-02T03:04:05Z"
`, "VALUE", value)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		registry := NewScheduledCronRegistry()
		if err := registry.LoadFromPath(path); err != nil {
			t.Fatalf("%s integer should parse like TOML: %v", value, err)
		}
		jobs := registry.List()
		if len(jobs) != 1 || jobs[0].SkippedOverlapCount != want {
			t.Fatalf("%s job mismatch: %#v", value, jobs)
		}
	}
}

func TestScheduledCronRegistryLoadRejectsInvalidUnderscoreSkippedOverlapCountLikeUpstream(t *testing.T) {
	for _, value := range []string{"_1", "1_", "1__000"} {
		path := filepath.Join(t.TempDir(), "cron.toml")
		content := strings.ReplaceAll(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad"
enabled = true
skipped_overlap_count = VALUE
stateful = false
created_at = "2026-01-02T03:04:05Z"
`, "VALUE", value)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		registry := NewScheduledCronRegistry()
		if err := registry.LoadFromPath(path); err == nil {
			t.Fatalf("expected invalid underscore integer parse error for %q", value)
		}
	}
}

func TestScheduledCronRegistryLoadRejectsLeadingZeroSkippedOverlapCountLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad"
enabled = true
skipped_overlap_count = 01
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected leading zero integer parse error")
	}
}

func TestScheduledCronRegistryLoadRejectsUppercaseBasePrefixLikeUpstream(t *testing.T) {
	for _, value := range []string{"0X10", "0O10", "0B10"} {
		path := filepath.Join(t.TempDir(), "cron.toml")
		content := strings.ReplaceAll(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad"
enabled = true
skipped_overlap_count = VALUE
stateful = false
created_at = "2026-01-02T03:04:05Z"
`, "VALUE", value)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		registry := NewScheduledCronRegistry()
		if err := registry.LoadFromPath(path); err == nil {
			t.Fatalf("expected uppercase base prefix parse error for %q", value)
		}
	}
}

func TestScheduledCronRegistryLoadRejectsSignedNonDecimalSkippedOverlapCountLikeUpstream(t *testing.T) {
	for _, value := range []string{"+0x10", "+0o10", "+0b10"} {
		path := filepath.Join(t.TempDir(), "cron.toml")
		content := strings.ReplaceAll(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad"
enabled = true
skipped_overlap_count = VALUE
stateful = false
created_at = "2026-01-02T03:04:05Z"
`, "VALUE", value)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		registry := NewScheduledCronRegistry()
		if err := registry.LoadFromPath(path); err == nil {
			t.Fatalf("expected signed non-decimal integer parse error for %q", value)
		}
	}
}

func TestScheduledCronRegistryLoadRejectsInvalidCreatedAtLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad"
enabled = true
stateful = false
created_at = "not-a-time"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected invalid created_at parse error")
	}
}

func TestScheduledCronRegistryLoadRejectsMissingCreatedAtLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad"
enabled = true
stateful = false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected missing created_at parse error")
	}
}

func TestScheduledCronRegistryLoadRejectsMissingEnabledLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad"
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected missing enabled parse error")
	}
}

func TestScheduledCronRegistryLoadRejectsMissingIDLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
schedule = "* * * * *"
action = "bad"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected missing id parse error")
	}
}

func TestScheduledCronRegistryLoadRejectsMissingScheduleLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
action = "bad"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "missing cron schedule") {
		t.Fatalf("expected missing schedule parse error, got %v", err)
	}
}

func TestScheduledCronRegistryLoadRejectsMissingActionLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "missing cron action") {
		t.Fatalf("expected missing action parse error, got %v", err)
	}
}

func TestScheduledCronRegistryLoadRejectsInlineMissingRequiredFieldsLikeUpstream(t *testing.T) {
	cases := []struct {
		name string
		job  string
		want string
	}{
		{name: "id", job: `{ schedule = "* * * * *", action = "bad", enabled = true, created_at = "2026-01-02T03:04:05Z" }`, want: "missing cron id"},
		{name: "schedule", job: `{ id = "cron-bad", action = "bad", enabled = true, created_at = "2026-01-02T03:04:05Z" }`, want: "missing cron schedule"},
		{name: "action", job: `{ id = "cron-bad", schedule = "* * * * *", enabled = true, created_at = "2026-01-02T03:04:05Z" }`, want: "missing cron action"},
		{name: "enabled", job: `{ id = "cron-bad", schedule = "* * * * *", action = "bad", created_at = "2026-01-02T03:04:05Z" }`, want: "missing cron enabled"},
		{name: "created_at", job: `{ id = "cron-bad", schedule = "* * * * *", action = "bad", enabled = true }`, want: "missing cron created_at"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cron.toml")
			if err := os.WriteFile(path, []byte("jobs = ["+tc.job+"]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			registry := NewScheduledCronRegistry()
			if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q parse error, got %v", tc.want, err)
			}
		})
	}
}

func TestScheduledCronRegistryLoadRejectsDuplicateKeyLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
schedule = "*/5 * * * *"
action = "bad"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "duplicate cron field") {
		t.Fatalf("expected duplicate key parse error, got %v", err)
	}
}

func TestScheduledCronRegistryLoadRejectsUnquotedStringLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = bad
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected unquoted string parse error")
	}
}

func TestScheduledCronRegistryLoadRejectsNonTOMLEscapeLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-bad"
schedule = "* * * * *"
action = "bad\x41"
enabled = true
stateful = false
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil {
		t.Fatal("expected invalid TOML escape parse error")
	}
}

func TestScheduledCronRegistryLoadAcceptsInlineCommentsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]] # a cron job
id = "cron-good" # stable id
schedule = "* * * * *" # every minute
action = "bad # keep hash inside string" # comment
enabled = true # enabled
stateful = false # stateless
created_at = "2026-01-02T03:04:05Z" # timestamp
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("inline comments should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].Action != "bad # keep hash inside string" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsLiteralStringsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = 'cron-good'
schedule = '* * * * *'
action = 'bad # keep hash in literal string' # comment
enabled = true
stateful = false
created_at = '2026-01-02T03:04:05Z'
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("literal strings should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].ID != "cron-good" || jobs[0].Action != "bad # keep hash in literal string" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryWritesTOMLCompatibleEscapesLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	action := "bell\a nul\x00 vertical\v delete\x7f"
	if _, err := registry.AddJob("* * * * *", action); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, invalidEscape := range []string{`\a`, `\v`, `\x00`, `\x7f`} {
		if strings.Contains(text, invalidEscape) {
			t.Fatalf("cron TOML should not contain non-TOML Go escapes %s:\n%s", invalidEscape, text)
		}
	}
	reloaded := NewScheduledCronRegistry()
	if err := reloaded.LoadFromPath(path); err != nil {
		t.Fatalf("self-written cron TOML should reload like upstream toml output: %v\n%s", err, text)
	}
	jobs := reloaded.List()
	if len(jobs) != 1 || jobs[0].Action != action {
		t.Fatalf("reloaded action mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsNativeDatetimeLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
stateful = false
created_at = 2026-01-02T03:04:05Z
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("native datetimes should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].CreatedAt.Format(time.RFC3339) != "2026-01-02T03:04:05Z" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadDefaultsOptionalFieldsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("missing stateful should default false like upstream: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].RunningTraceID != "" || jobs[0].LastDueAt != nil || jobs[0].LastFiredAt != nil || jobs[0].LastCompletedAt != nil || jobs[0].LastError != "" || jobs[0].SkippedOverlapCount != 0 || jobs[0].Stateful {
		t.Fatalf("optional fields should default to zero values: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsEmptyJobsArrayLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte("jobs = [ ]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("empty jobs array should parse like upstream: %v", err)
	}
	if jobs := registry.List(); len(jobs) != 0 {
		t.Fatalf("expected no jobs, got %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsInlineJobsArrayLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`jobs = [{ id = "cron-inline", schedule = "* * * * *", action = "ok", enabled = true, stateful = false, created_at = "2026-01-02T03:04:05Z" }]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("inline jobs array should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].ID != "cron-inline" || jobs[0].Action != "ok" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsInlineJobsArrayCommaInStringLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`jobs = [{ id = "cron-inline", schedule = "* * * * *", action = "say, hi", enabled = true, stateful = false, created_at = "2026-01-02T03:04:05Z" }]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("inline jobs comma string should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].Action != "say, hi" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsMultipleInlineJobsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`jobs = [{ id = "cron-one", schedule = "* * * * *", action = "one", enabled = true, stateful = false, created_at = "2026-01-02T03:04:05Z" }, { id = "cron-two", schedule = "0 * * * *", action = "two", enabled = false, stateful = true, created_at = "2026-01-02T04:05:06Z" }]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("multiple inline jobs should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 2 || jobs[0].ID != "cron-one" || jobs[1].ID != "cron-two" || !jobs[1].Stateful {
		t.Fatalf("jobs mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsInlineOptionalFieldsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`jobs = [{ id = "cron-inline", schedule = "* * * * *", action = "ok", enabled = true, running_trace_id = "trace-inline", last_error = "failed", skipped_overlap_count = 0x10, stateful = false, created_at = "2026-01-02T03:04:05Z" }]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("inline optional fields should parse like TOML: %v", err)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].SkippedOverlapCount != 16 || jobs[0].LastError != "cleared stale running state on startup" {
		t.Fatalf("job mismatch: %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadAcceptsEmptyFileLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("empty existing cron file should parse like upstream: %v", err)
	}
	if jobs := registry.List(); len(jobs) != 0 {
		t.Fatalf("expected no jobs, got %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadIgnoresTopLevelUnknownFieldsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte("version = 1\njobs = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("top-level unknown fields should be ignored like serde: %v", err)
	}
	if jobs := registry.List(); len(jobs) != 0 {
		t.Fatalf("expected no jobs, got %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadDefaultsMissingJobsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte("version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("missing jobs should default to empty like upstream: %v", err)
	}
	if jobs := registry.List(); len(jobs) != 0 {
		t.Fatalf("expected no jobs, got %#v", jobs)
	}
}

func TestScheduledCronRegistryLoadRejectsInvalidTopLevelJobsTypeLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte("jobs = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "invalid cron jobs") {
		t.Fatalf("expected invalid jobs type error, got %v", err)
	}
}

func TestScheduledCronRegistryLoadRejectsDuplicateTopLevelJobsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte("jobs = []\njobs = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "duplicate cron field") {
		t.Fatalf("expected duplicate top-level jobs error, got %v", err)
	}
}

func TestScheduledCronRegistryLoadRejectsJobsArrayThenTableLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	if err := os.WriteFile(path, []byte(`jobs = []
[[jobs]]
id = "cron-good"
schedule = "* * * * *"
action = "ok"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "duplicate cron field") {
		t.Fatalf("expected duplicate jobs error, got %v", err)
	}
}

func TestScheduledCronRegistryDueJobsWritesOnlyOnStateChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddJob("0 0 1 1 *", "yearly job"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC)); len(got) != 0 {
		t.Fatalf("yearly job should not be due: %#v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("idle tick should not rewrite sidecar, err=%v", err)
	}
	if _, err := registry.AddJob("* * * * *", "every minute"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC)); len(got) != 1 {
		t.Fatalf("minute job should be due: %#v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state-changing tick should persist sidecar: %v", err)
	}
}

func TestScheduledCronRegistryDueJobsKeepsMemoryWhenPersistFailsLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cron.toml")
	registry := NewScheduledCronRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	job, err := registry.AddJob("* * * * *", "say hello")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	due := registry.DueJobs(time.Date(2026, 5, 26, 22, 0, 0, 0, time.UTC), time.Date(2026, 5, 26, 22, 1, 5, 0, time.UTC))
	if len(due) != 1 || due[0].Job.ID != job.ID {
		t.Fatalf("due mismatch: %#v", due)
	}
	jobs := registry.List()
	if len(jobs) != 1 || jobs[0].RunningTraceID == "" || jobs[0].LastFiredAt == nil {
		t.Fatalf("memory state should update even if sidecar write fails: %#v", jobs)
	}
}

func findJob(jobs []Job, id string) (Job, bool) {
	for _, job := range jobs {
		if job.ID == id {
			return job, true
		}
	}
	return Job{}, false
}

func hasSubkind(results []PollResult, subkind string) bool {
	for _, result := range results {
		if result.Trigger.Source.Subkind == subkind {
			return true
		}
	}
	return false
}

func writeTinyFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
