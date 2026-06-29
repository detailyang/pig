package compaction

import (
	"context"
	"fmt"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

type SummarizationRequest struct {
	SystemPrompt string
	Conversation string
	Messages     []agent.Message
	TokensBefore uint64
	Cut          CutPointResult
}

type Summarizer interface {
	Summarize(ctx context.Context, request SummarizationRequest) (string, error)
}

type SummarizerWithUsage interface {
	SummarizeWithUsage(ctx context.Context, request SummarizationRequest) (string, ai.Usage, error)
}

type SummarizerFunc func(ctx context.Context, request SummarizationRequest) (string, error)

func (fn SummarizerFunc) Summarize(ctx context.Context, request SummarizationRequest) (string, error) {
	return fn(ctx, request)
}

type Result struct {
	Compacted    bool
	Summary      string
	Cut          CutPointResult
	TokensBefore uint64
	Usage        ai.Usage
	Messages     []agent.Message
}

type CompactionResult = Result

func Compact(ctx context.Context, entries []session.Entry, settings Settings, summarizer Summarizer) (Result, error) {
	prep := PrepareCompaction(entries, settings)
	kept := messagesFromEntries(entries[prep.Cut.CutIndex:])
	if len(prep.EntriesToSummarize) == 0 {
		return Result{Cut: prep.Cut, Messages: kept}, nil
	}
	if summarizer == nil {
		return Result{}, fmt.Errorf("compaction summarizer is required")
	}
	messagesToSummarize := messagesFromEntries(prep.EntriesToSummarize)
	conversation := SerializeConversation(messagesToSummarize)
	summary, usage, err := summarizeWithUsage(ctx, summarizer, SummarizationRequest{SystemPrompt: SummarizationSystemPrompt, Conversation: conversation, Messages: messagesToSummarize, TokensBefore: prep.TokensBefore, Cut: prep.Cut})
	if err != nil {
		return Result{}, err
	}
	messages := append([]agent.Message{NewSummaryMessage(summary)}, kept...)
	return Result{Compacted: true, Summary: summary, Cut: prep.Cut, TokensBefore: prep.TokensBefore, Usage: usage, Messages: messages}, nil
}

func summarizeWithUsage(ctx context.Context, summarizer Summarizer, request SummarizationRequest) (string, ai.Usage, error) {
	if usageSummarizer, ok := summarizer.(SummarizerWithUsage); ok {
		return usageSummarizer.SummarizeWithUsage(ctx, request)
	}
	summary, err := summarizer.Summarize(ctx, request)
	return summary, ai.Usage{}, err
}

func NewSummaryMessage(summary string) agent.Message {
	return agent.Message{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "Previous conversation summary:\n" + summary}}}}
}

func messagesFromEntries(entries []session.Entry) []agent.Message {
	messages := make([]agent.Message, 0, len(entries))
	for _, entry := range entries {
		if entry.Type() == session.EntryTypeMessage && entry.Message != nil {
			messages = append(messages, *entry.Message)
		}
	}
	return messages
}
