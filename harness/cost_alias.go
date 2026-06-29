package harness

import "github.com/detailyang/pig/cost"

type CostSnapshot = cost.CostSnapshot
type CostTracker = cost.CostTracker

func NewCostTracker() *CostTracker { return cost.NewCostTracker() }

func CostOneLineSummary(snapshot CostSnapshot) string { return cost.OneLineSummary(snapshot) }

func CostFullBreakdown(snapshot CostSnapshot) string { return cost.FullBreakdown(snapshot) }

func costOneLineSummary(snapshot CostSnapshot) string { return CostOneLineSummary(snapshot) }

func costFullBreakdown(snapshot CostSnapshot) string { return CostFullBreakdown(snapshot) }
