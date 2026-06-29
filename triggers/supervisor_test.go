package triggers

import (
	"testing"
	"time"
)

func TestSupervisorPollEvaluatesAdaptersAndDedups(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	runtime := NewRuntime(RuntimeConfig{DedupWindow: time.Minute, CycleHopLimit: 5})
	poller := &staticPoller{triggers: []Trigger{sampleTrigger("k1", "trace-1", now), sampleTrigger("k1", "trace-2", now)}}
	supervisor := NewSupervisor(SupervisorOptions{Runtime: runtime, Pollers: []Poller{poller}})
	result := supervisor.Poll(now)
	if len(result.Results) != 2 || len(result.Accepted) != 1 || len(result.Deduped) != 1 {
		t.Fatalf("poll result mismatch: %#v", result)
	}
	if result.Results[0].Outcome.Kind != OutcomeAccept || result.Results[1].Outcome.Kind != OutcomeDeduped {
		t.Fatalf("outcomes mismatch: %#v", result.Results)
	}
	if snap := runtime.Snapshot(); snap.AcceptedTotal != 1 || snap.DedupedTotal != 1 {
		t.Fatalf("runtime snapshot mismatch: %#v", snap)
	}
}

func TestSupervisorSupportsCronAndFileAdapters(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	cron := NewCronIntervalAdapter([]CronIntervalJob{{ID: "job", Every: time.Minute, Enabled: true}})
	cron.Poll(now)
	supervisor := NewSupervisor(SupervisorOptions{Pollers: []Poller{cron}})
	result := supervisor.Poll(now.Add(time.Minute))
	if len(result.Accepted) != 1 || result.Accepted[0].Trigger.Source.Subkind != "cron" {
		t.Fatalf("cron poll mismatch: %#v", result)
	}
}

type staticPoller struct{ triggers []Trigger }

func (poller *staticPoller) Poll(now time.Time) []Trigger { return poller.triggers }
