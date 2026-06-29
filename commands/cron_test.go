package commands

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCronCommandListsJobs(t *testing.T) {
	registry := DefaultRegistry()
	last := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ctx := Context{CronJobs: []CronJobEntry{
		{ID: "cron-1", Schedule: "*/5 * * * *", Action: strings.Repeat("a", 130), Enabled: true, Stateful: true, RunningTraceID: "trace-1", SkippedOverlapCount: 2},
		{ID: "cron-2", Schedule: "0 9 * * 1", Action: "weekly", Enabled: false, LastFiredAt: &last},
	}}
	out := Dispatch(context.Background(), "/cron", registry, ctx)
	if out.Kind != OutcomeHandled || !strings.Contains(out.Message, "Cron jobs (session, 2):") || !strings.Contains(out.Message, "cron-1  enabled  */5 * * * *  [stateful], running trace-1") || !strings.Contains(out.Message, strings.Repeat("a", 120)+"…") || !strings.Contains(out.Message, "overlap skips: 2") || !strings.Contains(out.Message, "cron-2  disabled  0 9 * * 1") || !strings.Contains(out.Message, "last fired: 2026-01-02T03:04:05Z") {
		t.Fatalf("cron list mismatch: %#v", out)
	}
	if RenderCronJobs(ctx.CronJobs) != out.Message {
		t.Fatalf("exported cron renderer should match /cron list")
	}
	alias := Dispatch(context.Background(), "/crontab ls", registry, ctx)
	if alias.Kind != OutcomeHandled || alias.Message != out.Message {
		t.Fatalf("alias mismatch: %#v", alias)
	}
	extra := Dispatch(context.Background(), "/cron list extra", registry, ctx)
	if extra.Kind != OutcomeHandled || extra.Message != out.Message {
		t.Fatalf("list should ignore extra args like upstream: %#v", extra)
	}
}

func TestCronCommandListRedactsSecretLikeActionPreview(t *testing.T) {
	secret := "sk-abcdefghijklmnopqrstuvwxyz123456"
	bearer := "Bearer abcdefghijklmnopqrstuvwxyz"
	out := RenderCronJobs([]CronJobEntry{{ID: "cron-secret", Schedule: "* * * * *", Action: "call API with " + bearer + " and " + secret, Enabled: true}})
	if strings.Contains(out, secret) || strings.Contains(out, bearer) || !strings.Contains(out, "[REDACTED:") {
		t.Fatalf("cron action preview should redact secret-like values:\n%s", out)
	}
}

func TestCronCommandReturnsMutationOutcomes(t *testing.T) {
	registry := DefaultRegistry()
	add := Dispatch(context.Background(), `/cron add --stateful "*/5 * * * *" run health check`, registry, Context{})
	if add.Kind != OutcomeAddCronJob || add.Schedule != "*/5 * * * *" || add.Prompt != "run health check" || !add.Stateful || add.Message != "add cron job: */5 * * * *" {
		t.Fatalf("add mismatch: %#v", add)
	}
	enable := Dispatch(context.Background(), "/cron enable cron-1 extra", registry, Context{})
	if enable.Kind != OutcomeSetCronJobEnabled || enable.TargetID == nil || *enable.TargetID != "cron-1" || !enable.Enabled || enable.Message != "enabled cron job cron-1" {
		t.Fatalf("enable mismatch: %#v", enable)
	}
	disable := Dispatch(context.Background(), "/cron pause cron-1 extra", registry, Context{})
	if disable.Kind != OutcomeSetCronJobEnabled || disable.TargetID == nil || *disable.TargetID != "cron-1" || disable.Enabled || disable.Message != "disabled cron job cron-1" {
		t.Fatalf("disable mismatch: %#v", disable)
	}
	remove := Dispatch(context.Background(), "/cron rm cron-1 extra", registry, Context{})
	if remove.Kind != OutcomeRemoveCronJob || remove.TargetID == nil || *remove.TargetID != "cron-1" || remove.Message != "remove cron job cron-1" {
		t.Fatalf("remove mismatch: %#v", remove)
	}
}

func TestCronCommandUsageErrors(t *testing.T) {
	registry := DefaultRegistry()
	cases := map[string]string{
		"/cron nope":              "unknown /cron command: nope. usage: /cron [list|add \"<5-field-cron>\" <prompt>|enable <id>|disable <id>|remove <id>]",
		"/cron add":               "usage: /cron add [--stateful] \"<minute hour dom month dow>\" <prompt>",
		"/cron add * * * * * run": "usage: /cron add [--stateful] \"<minute hour dom month dow>\" <prompt>",
		"/cron enable":            "usage: /cron enable <id>",
		"/cron remove":            "usage: /cron remove <id>",
	}
	for input, want := range cases {
		out := Dispatch(context.Background(), input, registry, Context{})
		if out.Kind != OutcomeError || out.Message != want {
			t.Fatalf("%s mismatch: %#v", input, out)
		}
	}
}
