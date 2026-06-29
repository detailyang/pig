package commands

import (
	"context"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestThinkingCommandShowsAndSetsLevel(t *testing.T) {
	registry := DefaultRegistry()
	show := Dispatch(context.Background(), "/thinking", registry, Context{ThinkingLevel: ai.ThinkingHigh})
	if show.Kind != OutcomeHandled || show.Message != "thinking level: high" {
		t.Fatalf("show mismatch: %#v", show)
	}
	set := Dispatch(context.Background(), "/thinking minimal extra", registry, Context{})
	if set.Kind != OutcomeSetThinkingLevel || set.ThinkingLevel != ai.ThinkingMinimal || set.Message != "thinking level: minimal" {
		t.Fatalf("set mismatch: %#v", set)
	}
	upper := Dispatch(context.Background(), "/thinking XHIGH", registry, Context{})
	if upper.Kind != OutcomeSetThinkingLevel || upper.ThinkingLevel != ai.ThinkingXHigh {
		t.Fatalf("uppercase set mismatch: %#v", upper)
	}
}

func TestThinkingCommandRejectsBadUsage(t *testing.T) {
	registry := DefaultRegistry()
	badLevel := Dispatch(context.Background(), "/thinking huge", registry, Context{})
	if badLevel.Kind != OutcomeError || badLevel.Message != "invalid level: huge" {
		t.Fatalf("bad level mismatch: %#v", badLevel)
	}
}
