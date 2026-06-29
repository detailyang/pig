package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/cost"
)

func TestCostCommandShowsBreakdownAndResetOutcome(t *testing.T) {
	registry := DefaultRegistry()
	snapshot := cost.Snapshot{TurnCount: 2, Tokens: ai.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 3, CacheWriteTokens: 2, TotalTokenCount: 20, HasTotalTokens: true, Cost: &ai.UsageCost{Input: 0.001, Output: 0.002, CacheRead: 0.0003, CacheWrite: 0.0002, Total: 0.0035}}}
	shown := Dispatch(context.Background(), "/cost", registry, Context{Cost: snapshot})
	if shown.Kind != OutcomeHandled || !strings.Contains(shown.Message, "turns:") || !strings.Contains(shown.Message, "input         10") || !strings.Contains(shown.Message, "total         $0.0035") {
		t.Fatalf("cost show mismatch: %#v", shown)
	}
	reset := Dispatch(context.Background(), "/cost reset extra", registry, Context{Cost: snapshot})
	if reset.Kind != OutcomeResetCost || reset.Message != "cost counters reset" {
		t.Fatalf("cost reset mismatch: %#v", reset)
	}
	shownWithArg := Dispatch(context.Background(), "/cost now", registry, Context{Cost: snapshot})
	if shownWithArg.Kind != OutcomeHandled || shownWithArg.Message != shown.Message {
		t.Fatalf("non-reset cost arg should show breakdown like upstream: %#v", shownWithArg)
	}
}
