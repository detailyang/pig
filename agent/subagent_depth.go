package agent

import "context"

type subagentDepthKey struct{}

func SubagentDepth(ctx context.Context) int {
	depth, _ := ctx.Value(subagentDepthKey{}).(int)
	return depth
}

func ContextWithSubagentDepth(ctx context.Context, depth int) context.Context {
	if depth < 0 {
		depth = 0
	}
	return context.WithValue(ctx, subagentDepthKey{}, depth)
}

func ContextWithIncrementedSubagentDepth(ctx context.Context) context.Context {
	return ContextWithSubagentDepth(ctx, SubagentDepth(ctx)+1)
}
