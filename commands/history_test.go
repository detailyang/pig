package commands

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestHistoryCommandShowsRecentPrompts(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{History: []string{"first", "second", "third"}}
	out := Dispatch(context.Background(), "/history 2", registry, ctx)
	if out.Kind != OutcomeHandled || strings.Contains(out.Message, "1: first") || !strings.Contains(out.Message, "  2: second") || !strings.Contains(out.Message, "  3: third") {
		t.Fatalf("history mismatch: %#v", out)
	}
}

func TestHistoryCommandDefaultsAndTruncates(t *testing.T) {
	registry := DefaultRegistry()
	entries := make([]string, 25)
	for index := range entries {
		entries[index] = fmt.Sprintf("entry-%02d", index+1)
	}
	entries[24] = strings.Repeat("x", 205)
	out := Dispatch(context.Background(), "/history", registry, Context{History: entries})
	if out.Kind != OutcomeHandled || strings.Contains(out.Message, "1: entry-01") || !strings.Contains(out.Message, "  6: entry-06") || !strings.Contains(out.Message, strings.Repeat("x", 200)+"…") {
		t.Fatalf("default history mismatch: %#v", out)
	}
}

func TestHistoryCommandEmptyAndInvalidArgsDefaultLikeUpstream(t *testing.T) {
	registry := DefaultRegistry()
	empty := Dispatch(context.Background(), "/history", registry, Context{})
	if empty.Kind != OutcomeHandled || empty.Message != "(no history yet)" {
		t.Fatalf("empty mismatch: %#v", empty)
	}
	ctx := Context{History: []string{"first", "second"}}
	bad := Dispatch(context.Background(), "/history nope", registry, ctx)
	if bad.Kind != OutcomeHandled || bad.Message != "  1: first\n  2: second" {
		t.Fatalf("bad history arg should default like upstream: %#v", bad)
	}
	tooMany := Dispatch(context.Background(), "/history 1 2", registry, ctx)
	if tooMany.Kind != OutcomeHandled || tooMany.Message != "  2: second" {
		t.Fatalf("extra history args should be ignored like upstream: %#v", tooMany)
	}
}
