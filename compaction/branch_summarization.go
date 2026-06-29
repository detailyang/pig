package compaction

import (
	"context"
	"fmt"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

const BranchSummaryInstructions = "Produce a concise branch summary of the conversation below. Capture the goal of this branch, what was accomplished, and the most recent state so a sibling branch can pick up without replaying every message."

type BranchSummaryResult struct {
	Summary string
	Usage   ai.Usage
}

func SummarizeBranch(ctx context.Context, entries []session.Entry, summarizer Summarizer) (BranchSummaryResult, error) {
	messages := messagesFromEntries(entries)
	if len(messages) == 0 {
		return BranchSummaryResult{}, nil
	}
	if summarizer == nil {
		return BranchSummaryResult{}, fmt.Errorf("branch summarizer is required")
	}
	request := SummarizationRequest{
		SystemPrompt: SummarizationSystemPrompt + "\n\n" + BranchSummaryInstructions,
		Conversation: SerializeConversation(messages),
		Messages:     messages,
	}
	summary, usage, err := summarizeWithUsage(ctx, summarizer, request)
	if err != nil {
		return BranchSummaryResult{}, err
	}
	return BranchSummaryResult{Summary: summary, Usage: usage}, nil
}
