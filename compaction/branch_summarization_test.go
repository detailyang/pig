package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

type branchUsageSummarizer struct{}

func (branchUsageSummarizer) Summarize(ctx context.Context, request SummarizationRequest) (string, error) {
	return "fallback summary", nil
}

func (branchUsageSummarizer) SummarizeWithUsage(ctx context.Context, request SummarizationRequest) (string, ai.Usage, error) {
	return "branch summary", ai.Usage{InputTokens: 7, OutputTokens: 3}, nil
}

func TestSummarizeBranchEmptyEntriesMatchesUpstream(t *testing.T) {
	result, err := SummarizeBranch(context.Background(), nil, SummarizerFunc(func(ctx context.Context, request SummarizationRequest) (string, error) {
		t.Fatal("summarizer should not be called for empty branch")
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "" {
		t.Fatalf("empty branch summary mismatch: %#v", result)
	}
}

func TestSummarizeBranchUsesOnlyMessageEntriesAndBranchInstructions(t *testing.T) {
	entries := []session.Entry{
		session.NewMessageEntry("u1", nil, "now", agent.NewUserMessage("goal")),
		{EntryType: session.EntryTypeLabel, EntryID: "label", Timestamp: "now", LabelValue: strPtr("skip")},
		session.NewMessageEntry("a1", strPtr("u1"), "now", agent.NewAssistantMessage("done")),
	}
	var got SummarizationRequest
	result, err := SummarizeBranch(context.Background(), entries, SummarizerFunc(func(ctx context.Context, request SummarizationRequest) (string, error) {
		got = request
		return "branch summary", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary != "branch summary" {
		t.Fatalf("branch summary result mismatch: %#v", result)
	}
	if len(got.Messages) != 2 || !strings.Contains(got.Conversation, "goal") || strings.Contains(got.Conversation, "skip") {
		t.Fatalf("branch summarization request should include only messages: %#v conversation=%q", got.Messages, got.Conversation)
	}
	if !strings.Contains(got.SystemPrompt, "branch summary") || !strings.Contains(got.SystemPrompt, SummarizationSystemPrompt) {
		t.Fatalf("branch instructions missing from system prompt: %q", got.SystemPrompt)
	}
}

func TestSummarizeBranchReturnsUsageLikeUpstream(t *testing.T) {
	entries := []session.Entry{session.NewMessageEntry("u1", nil, "now", agent.NewUserMessage("goal"))}

	result, err := SummarizeBranch(context.Background(), entries, branchUsageSummarizer{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Summary != "branch summary" || result.Usage.InputTokens != 7 || result.Usage.OutputTokens != 3 {
		t.Fatalf("branch summary should include summarizer usage like upstream: %#v", result)
	}
}
