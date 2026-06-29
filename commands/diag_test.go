package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/cost"
	"github.com/detailyang/pig/skills"
)

func TestDiagCommandShowsDiagnosticSnapshot(t *testing.T) {
	registry := DefaultRegistry()
	model := ai.Model{Provider: ai.Provider("openai"), ID: "gpt-test"}
	snapshot := cost.Snapshot{TurnCount: 1, Tokens: ai.Usage{InputTokens: 10, OutputTokens: 5, TotalTokenCount: 15, HasTotalTokens: true, Cost: &ai.UsageCost{Total: 0.25}}}
	out := Dispatch(context.Background(), "/diag", registry, Context{
		SessionID:     "sess-1",
		Model:         &model,
		ThinkingLevel: ai.ThinkingHigh,
		ToolCount:     7,
		Skills:        []skills.Skill{{Name: "alpha"}, {Name: "beta"}},
		Cost:          snapshot,
		LogPath:       "/tmp/pig.log",
	})
	for _, want := range []string{
		"Diagnostic snapshot:",
		"session       sess-1",
		"model         openai:gpt-test",
		"thinking      high",
		"tools         7",
		"skills        2",
		"cost          tokens: in=10 out=5 cached=0 total=15 | cost $0.2500",
		"log file      /tmp/pig.log",
	} {
		if out.Kind != OutcomeHandled || !strings.Contains(out.Message, want) {
			t.Fatalf("diag missing %q: %#v", want, out)
		}
	}
}

func TestDiagCommandDefaultsMissingFields(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/diag", registry, Context{})
	if out.Kind != OutcomeHandled || !strings.Contains(out.Message, "model         (none)") || !strings.Contains(out.Message, "thinking      ?") || !strings.Contains(out.Message, "log file      (logging disabled)") {
		t.Fatalf("diag defaults mismatch: %#v", out)
	}
	extra := Dispatch(context.Background(), "/diag extra", registry, Context{})
	if extra.Kind != OutcomeHandled || !strings.Contains(extra.Message, "model         (none)") {
		t.Fatalf("diag should ignore args like upstream: %#v", extra)
	}
}
