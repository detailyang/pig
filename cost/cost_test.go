package cost

import (
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

func TestCostTrackerAccumulatesAndResets(t *testing.T) {
	tracker := NewTracker()
	tracker.Record(&ai.Usage{InputTokens: 100, OutputTokens: 50, CacheReadTokens: 10, CacheWriteTokens: 5, TotalTokenCount: 165, HasTotalTokens: true, Cost: &ai.UsageCost{Input: 0.001, Output: 0.002, CacheRead: 0.0001, CacheWrite: 0.0002, Total: 0.0033}})
	tracker.Record(&ai.Usage{InputTokens: 100, OutputTokens: 50, CacheReadTokens: 10, CacheWriteTokens: 5, TotalTokenCount: 165, HasTotalTokens: true, Cost: &ai.UsageCost{Input: 0.001, Output: 0.002, CacheRead: 0.0001, CacheWrite: 0.0002, Total: 0.0033}})

	snap := tracker.Snapshot()
	if snap.Tokens.InputTokens != 200 || snap.Tokens.OutputTokens != 100 || snap.Tokens.TotalTokens() != 330 || snap.TurnCount != 2 {
		t.Fatalf("snapshot mismatch: %#v", snap)
	}
	if snap.TotalCost() != 0.0066 {
		t.Fatalf("cost mismatch: %.6f", snap.TotalCost())
	}
	if !strings.Contains(OneLineSummary(snap), "tokens: in=200 out=100 cached=30 total=330") {
		t.Fatalf("summary mismatch: %q", OneLineSummary(snap))
	}
	if !strings.Contains(FullBreakdown(snap), "turns:") || !strings.Contains(FullBreakdown(snap), "Cost (USD):") {
		t.Fatalf("breakdown mismatch: %q", FullBreakdown(snap))
	}
	tracker.Reset()
	if tracker.Snapshot().TurnCount != 0 || tracker.Snapshot().Tokens.InputTokens != 0 {
		t.Fatalf("reset mismatch: %#v", tracker.Snapshot())
	}
}

func TestCostTrackerAccumulatesExplicitTotalTokensLikeUpstream(t *testing.T) {
	tracker := NewTracker()
	tracker.Record(&ai.Usage{InputTokens: 1, OutputTokens: 2, TotalTokenCount: 10, HasTotalTokens: true})
	tracker.Record(&ai.Usage{InputTokens: 3, OutputTokens: 4, TotalTokenCount: 20, HasTotalTokens: true})

	snap := tracker.Snapshot()
	if snap.Tokens.TotalTokens() != 30 {
		t.Fatalf("total tokens should sum explicit provider totals like upstream, got %#v", snap.Tokens)
	}
	if !strings.Contains(OneLineSummary(snap), "total=30") {
		t.Fatalf("summary should use explicit provider total tokens, got %q", OneLineSummary(snap))
	}
}

func TestCostTrackerKeepsMissingTotalTokensAtZeroLikeUpstream(t *testing.T) {
	tracker := NewTracker()
	tracker.Record(&ai.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 2, CacheWriteTokens: 1})

	snap := tracker.Snapshot()
	if snap.Tokens.HasTotalTokens || snap.Tokens.TotalTokenCount != 0 {
		t.Fatalf("missing total_tokens should remain zero like upstream, got %#v", snap.Tokens)
	}
	if !strings.Contains(OneLineSummary(snap), "total=0") {
		t.Fatalf("summary should use recorded total_tokens field like upstream, got %q", OneLineSummary(snap))
	}
}

func TestCostTrackerAsListenerRecordsAssistantMessageEndOnly(t *testing.T) {
	tracker := NewTracker()
	listener := tracker.AsListener()

	listener(agent.Event{Type: agent.EventTypeMessageEnd, Message: &agent.Message{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleUser, Usage: &ai.Usage{InputTokens: 100}}}})
	listener(agent.Event{Type: agent.EventTypeMessageEnd, Message: &agent.Message{Kind: agent.MessageKindToolResult, ToolResult: &agent.ToolResult{Content: "tool"}}})
	listener(agent.Event{Type: agent.EventTypeAssistant, Message: &agent.Message{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{InputTokens: 50}}}})
	listener(agent.Event{Type: agent.EventTypeMessageEnd, Message: &agent.Message{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleAssistant, Usage: &ai.Usage{InputTokens: 3, OutputTokens: 4, TotalTokenCount: 7, HasTotalTokens: true, Cost: &ai.UsageCost{Total: 0.25}}}}})

	snap := tracker.Snapshot()
	if snap.TurnCount != 1 || snap.Tokens.InputTokens != 3 || snap.Tokens.OutputTokens != 4 || snap.Tokens.TotalTokens() != 7 || snap.TotalCost() != 0.25 {
		t.Fatalf("listener should record only assistant message_end usage, got %#v", snap)
	}
}

func TestCostTrackerSaturatesTokenCountersLikeUpstream(t *testing.T) {
	tracker := NewTracker()
	tracker.Record(&ai.Usage{InputTokens: maxInt, OutputTokens: maxInt, CacheReadTokens: maxInt, CacheWriteTokens: maxInt, TotalTokenCount: maxInt, HasTotalTokens: true})
	tracker.Record(&ai.Usage{InputTokens: 1, OutputTokens: 1, CacheReadTokens: 1, CacheWriteTokens: 1, TotalTokenCount: 1, HasTotalTokens: true})

	snap := tracker.Snapshot()
	if snap.Tokens.InputTokens != maxInt || snap.Tokens.OutputTokens != maxInt || snap.Tokens.CacheReadTokens != maxInt || snap.Tokens.CacheWriteTokens != maxInt || snap.Tokens.TotalTokenCount != maxInt {
		t.Fatalf("token counters should saturate at max int like upstream saturating_add, got %#v", snap.Tokens)
	}
}

func TestCostUpstreamExportedNames(t *testing.T) {
	tracker := NewCostTracker()
	var _ *CostTracker = tracker
	tracker.Record(&ai.Usage{InputTokens: 1, OutputTokens: 2, Cost: &ai.UsageCost{Total: 0.5}})
	snapshot := tracker.Snapshot()
	var _ CostSnapshot = snapshot
	if snapshot.TotalCost() != 0.5 {
		t.Fatalf("total cost mismatch: %#v", snapshot)
	}
	if OneLineSummaryUpstream(snapshot) != OneLineSummary(snapshot) {
		t.Fatalf("one-line summary wrapper mismatch")
	}
	if FullBreakdownUpstream(snapshot) != FullBreakdown(snapshot) {
		t.Fatalf("full breakdown wrapper mismatch")
	}
	if cost_one_line_summary(snapshot) != OneLineSummary(snapshot) {
		t.Fatalf("cost_one_line_summary wrapper mismatch")
	}
	if cost_full_breakdown(snapshot) != FullBreakdown(snapshot) {
		t.Fatalf("cost_full_breakdown wrapper mismatch")
	}
}
