package commands

import (
	"context"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/session"
)

func TestSessionsCommandListsSessions(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Sessions: []SessionListEntry{
		{ID: "0123456789abcdef-extra", CreatedAt: "2026-01-02T03:04:05Z", Preview: "first prompt"},
		{ID: "short", CreatedAt: "2026-01-03T03:04:05Z"},
	}}
	out := Dispatch(context.Background(), "/sessions", registry, ctx)
	want := "Sessions:\n  0123456789abcdef  2026-01-02T03:04:05Z  first prompt\n  short  2026-01-03T03:04:05Z  "
	if out.Kind != OutcomeHandled || out.Message != want {
		t.Fatalf("sessions mismatch: %#v", out)
	}
}

func TestSessionsCommandEmptyAndUsage(t *testing.T) {
	registry := DefaultRegistry()
	empty := Dispatch(context.Background(), "/sessions", registry, Context{})
	if empty.Kind != OutcomeHandled || empty.Message != "(no sessions for this cwd)" {
		t.Fatalf("empty mismatch: %#v", empty)
	}
	extra := Dispatch(context.Background(), "/sessions extra", registry, Context{})
	if extra.Kind != OutcomeHandled || extra.Message != "(no sessions for this cwd)" {
		t.Fatalf("sessions should ignore args like upstream: %#v", extra)
	}
}

func TestSessionListEntriesFromSessionsBuildsPreview(t *testing.T) {
	entries := SessionListEntriesFromSessions([]SessionSummaryInput{
		{ID: "sess-1", CreatedAt: "now", Entries: []session.Entry{session.NewMessageEntry("u1", nil, "now", agent.NewUserMessage("hello world"))}},
		{ID: "sess-2", CreatedAt: "later"},
	})
	if len(entries) != 2 || entries[0].Preview != "hello world" || entries[1].Preview != "" {
		t.Fatalf("entries mismatch: %#v", entries)
	}
}
