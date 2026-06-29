package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/triggers"
)

func TestInboxCommandListsNewAndAllEntries(t *testing.T) {
	registry := DefaultRegistry()
	entries := []triggers.InboxEntry{
		{ID: "inb-123456789abc0000", CreatedAt: "2026-01-02T03:04:05Z", Source: "cron:daily", Text: "issue #9", Status: triggers.InboxStatusNew},
		{ID: "inb-claimed", CreatedAt: "2026-01-03T03:04:05Z", Source: "cron:weekly", Text: "old", Status: triggers.InboxStatusClaimed},
	}
	list := Dispatch(context.Background(), "/inbox list extra", registry, Context{Inbox: entries})
	if list.Kind != OutcomeHandled || !strings.Contains(list.Message, "Inbox (1 new):") || !strings.Contains(list.Message, "1. [inb-12345678] issue #9  (cron:daily, 2026-01-02T03:04)") || !strings.Contains(list.Message, "claim with /inbox claim <n>") || strings.Contains(list.Message, "old") {
		t.Fatalf("list mismatch: %#v", list)
	}
	all := Dispatch(context.Background(), "/inbox all extra", registry, Context{Inbox: entries})
	if all.Kind != OutcomeHandled || !strings.Contains(all.Message, "Inbox history (2 total):") || !strings.Contains(all.Message, "[new] issue #9  (cron:daily)") || !strings.Contains(all.Message, "[claimed] old  (cron:weekly)") {
		t.Fatalf("all mismatch: %#v", all)
	}
}

func TestInboxCommandClaimDismissAndClearOutcomes(t *testing.T) {
	registry := DefaultRegistry()
	entries := []triggers.InboxEntry{{ID: "inb-123456789abc0000", Source: "cron:daily", Text: "issue #9", Status: triggers.InboxStatusNew}}
	claim := Dispatch(context.Background(), "/inbox claim 1 extra", registry, Context{Inbox: entries})
	if claim.Kind != OutcomeSetInboxStatus || claim.TargetID == nil || *claim.TargetID != "inb-123456789abc0000" || claim.InboxStatus != triggers.InboxStatusClaimed || claim.Prompt != "A recurring loop (cron:daily) reported this finding — investigate and address it:\nissue #9" || claim.ErrorContext != "inbox claim" {
		t.Fatalf("claim mismatch: %#v", claim)
	}
	claimZero := Dispatch(context.Background(), "/inbox claim 0", registry, Context{Inbox: entries})
	if claimZero.Kind != OutcomeSetInboxStatus || claimZero.TargetID == nil || *claimZero.TargetID != "inb-123456789abc0000" {
		t.Fatalf("claim zero should resolve first entry like upstream saturating_sub, got %#v", claimZero)
	}
	dismiss := Dispatch(context.Background(), "/inbox dismiss inb-123 extra", registry, Context{Inbox: entries})
	if dismiss.Kind != OutcomeSetInboxStatus || dismiss.TargetID == nil || *dismiss.TargetID != "inb-123456789abc0000" || dismiss.InboxStatus != triggers.InboxStatusDismissed || dismiss.Message != "dismissed: issue #9" {
		t.Fatalf("dismiss mismatch: %#v", dismiss)
	}
	clear := Dispatch(context.Background(), "/inbox clear extra", registry, Context{Inbox: entries})
	if clear.Kind != OutcomeClearInbox || clear.Message != "dismissed 1 inbox entry" {
		t.Fatalf("clear mismatch: %#v", clear)
	}
}

func TestInboxCommandEmptyAndUsage(t *testing.T) {
	registry := DefaultRegistry()
	empty := Dispatch(context.Background(), "/inbox", registry, Context{})
	if empty.Kind != OutcomeHandled || empty.Message != "inbox: empty — stateful loops (/cron add --stateful) report findings here" {
		t.Fatalf("empty mismatch: %#v", empty)
	}
	cases := map[string]string{
		"/inbox claim":     "usage: /inbox claim|dismiss <n or inb-id>",
		"/inbox dismiss 2": "no inbox entry #2 (have 0)",
		"/inbox unknown":   "unknown /inbox subcommand: unknown; usage: /inbox [all|claim <n>|dismiss <n>|clear]",
	}
	for input, want := range cases {
		out := Dispatch(context.Background(), input, registry, Context{})
		if out.Kind != OutcomeError || out.Message != want {
			t.Fatalf("%s mismatch: %#v", input, out)
		}
	}
}
