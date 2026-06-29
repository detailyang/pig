package cost

import (
	"fmt"
	"sync"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type Snapshot struct {
	Tokens    ai.Usage
	TurnCount uint64
}

type CostSnapshot = Snapshot

func (snapshot Snapshot) TotalCost() float64 {
	if snapshot.Tokens.Cost == nil {
		return 0
	}
	return snapshot.Tokens.Cost.Total
}

type Tracker struct {
	mu       sync.Mutex
	snapshot Snapshot
}

type CostTracker = Tracker

func NewTracker() *Tracker { return &Tracker{} }

func NewCostTracker() *CostTracker { return NewTracker() }

func (tracker *Tracker) Snapshot() Snapshot {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return cloneSnapshot(tracker.snapshot)
}

func (tracker *Tracker) Reset() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.snapshot = Snapshot{}
}

func (tracker *Tracker) Record(usage *ai.Usage) {
	if usage == nil {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.snapshot.Tokens.InputTokens = saturatingAddInt(tracker.snapshot.Tokens.InputTokens, usage.InputTokens)
	tracker.snapshot.Tokens.OutputTokens = saturatingAddInt(tracker.snapshot.Tokens.OutputTokens, usage.OutputTokens)
	tracker.snapshot.Tokens.CacheReadTokens = saturatingAddInt(tracker.snapshot.Tokens.CacheReadTokens, usage.CacheReadTokens)
	tracker.snapshot.Tokens.CacheWriteTokens = saturatingAddInt(tracker.snapshot.Tokens.CacheWriteTokens, usage.CacheWriteTokens)
	if usage.HasTotalTokens {
		tracker.snapshot.Tokens.TotalTokenCount = saturatingAddInt(tracker.snapshot.Tokens.TotalTokenCount, usage.TotalTokenCount)
		tracker.snapshot.Tokens.HasTotalTokens = true
	}
	if tracker.snapshot.Tokens.Cost == nil {
		tracker.snapshot.Tokens.Cost = &ai.UsageCost{}
	}
	if usage.Cost != nil {
		tracker.snapshot.Tokens.Cost.Input += usage.Cost.Input
		tracker.snapshot.Tokens.Cost.Output += usage.Cost.Output
		tracker.snapshot.Tokens.Cost.CacheRead += usage.Cost.CacheRead
		tracker.snapshot.Tokens.Cost.CacheWrite += usage.Cost.CacheWrite
		tracker.snapshot.Tokens.Cost.Total += usage.Cost.Total
	}
	tracker.snapshot.TurnCount++
}

func (tracker *Tracker) AsListener() agent.AgentListener {
	return func(event agent.Event) {
		if event.Type != agent.EventTypeMessageEnd || event.Message == nil || event.Message.LLM == nil || event.Message.LLM.Role != ai.RoleAssistant {
			return
		}
		tracker.Record(event.Message.LLM.Usage)
	}
}

func (tracker *Tracker) AsListenerUpstream() agent.AgentListener { return tracker.AsListener() }

func saturatingAddInt(left, right int) int {
	if right > 0 && left > maxInt-right {
		return maxInt
	}
	if right < 0 && left < minInt-right {
		return minInt
	}
	return left + right
}

const (
	maxInt = int(^uint(0) >> 1)
	minInt = -maxInt - 1
)

func OneLineSummary(snapshot Snapshot) string {
	return fmt.Sprintf("tokens: in=%d out=%d cached=%d total=%d | cost $%.4f", snapshot.Tokens.InputTokens, snapshot.Tokens.OutputTokens, snapshot.Tokens.CacheReadTokens+snapshot.Tokens.CacheWriteTokens, snapshot.Tokens.TotalTokenCount, snapshot.TotalCost())
}

func OneLineSummaryUpstream(snapshot CostSnapshot) string { return OneLineSummary(snapshot) }

func cost_one_line_summary(snapshot CostSnapshot) string { return OneLineSummary(snapshot) }

func FullBreakdown(snapshot Snapshot) string {
	cost := snapshot.Tokens.Cost
	if cost == nil {
		cost = &ai.UsageCost{}
	}
	return fmt.Sprintf("  turns:        %d\n\nTokens:\n\n  input         %d\n  output        %d\n  cache read    %d\n  cache write   %d\n  total         %d\n\nCost (USD):\n\n  input         $%.4f\n  output        $%.4f\n  cache read    $%.4f\n  cache write   $%.4f\n  total         $%.4f\n", snapshot.TurnCount, snapshot.Tokens.InputTokens, snapshot.Tokens.OutputTokens, snapshot.Tokens.CacheReadTokens, snapshot.Tokens.CacheWriteTokens, snapshot.Tokens.TotalTokenCount, cost.Input, cost.Output, cost.CacheRead, cost.CacheWrite, cost.Total)
}

func FullBreakdownUpstream(snapshot CostSnapshot) string { return FullBreakdown(snapshot) }

func cost_full_breakdown(snapshot CostSnapshot) string { return FullBreakdown(snapshot) }

func cloneSnapshot(snapshot Snapshot) Snapshot {
	if snapshot.Tokens.Cost != nil {
		costCopy := *snapshot.Tokens.Cost
		snapshot.Tokens.Cost = &costCopy
	}
	return snapshot
}
