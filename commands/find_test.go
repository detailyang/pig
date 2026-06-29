package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/session"
)

func TestFindCommandReturnsSearchOutcome(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/find release plan", registry, Context{CWD: "/repo"})
	if out.Kind != OutcomeFindSessions || out.Query != "release plan" || out.CWD != "/repo" || out.Message != "find sessions: release plan" {
		t.Fatalf("find mismatch: %#v", out)
	}
}

func TestFindCommandUsage(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/find", registry, Context{})
	if out.Kind != OutcomeError || out.Message != "usage: /find <query>" {
		t.Fatalf("usage mismatch: %#v", out)
	}
}

func TestFindMatchesSearchesUserAndAssistantText(t *testing.T) {
	entries := []session.Entry{
		messageEntry("u1", agent.NewUserMessage("Plan the release window")),
		messageEntry("a1", agent.NewAssistantMessage(strings.Repeat("x", 130))),
		messageEntry("u2", agent.NewUserMessage("unrelated")),
	}
	hits := FindMatches("release", []SessionSearchInput{{Path: "/sessions/abc.jsonl", Entries: entries}})
	if len(hits) != 1 || hits[0].Session != "abc" || hits[0].Snippet != "Plan the release window" {
		t.Fatalf("hits mismatch: %#v", hits)
	}
	longHits := FindMatches(strings.Repeat("x", 10), []SessionSearchInput{{Path: "/sessions/def.jsonl", Entries: entries}})
	if len(longHits) != 1 || len([]rune(longHits[0].Snippet)) != 120 {
		t.Fatalf("long hit mismatch: %#v", longHits)
	}
	if none := FindMatches("missing", []SessionSearchInput{{Path: "/sessions/abc.jsonl", Entries: entries}}); len(none) != 0 {
		t.Fatalf("expected no hits, got %#v", none)
	}
}

func TestFindResultsTextFormatsMatches(t *testing.T) {
	empty := FindResultsText(nil)
	if empty != "(no matches)" {
		t.Fatalf("empty mismatch: %q", empty)
	}
	text := FindResultsText([]FindMatch{{Session: "abc", Snippet: "hello\nworld"}, {Session: "def", Snippet: "bye"}})
	if !strings.Contains(text, "  abc  hello world") || !strings.Contains(text, "  def  bye") || !strings.Contains(text, "(2 match(es))") {
		t.Fatalf("text mismatch: %q", text)
	}
}

func messageEntry(id string, message agent.Message) session.Entry {
	return session.NewMessageEntry(id, nil, "now", message)
}
