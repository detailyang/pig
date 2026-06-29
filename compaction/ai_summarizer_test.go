package compaction

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

func TestAISummarizerStreamFuncUsesSimpleStreamOptionsLikeUpstream(t *testing.T) {
	streamType := reflect.TypeOf(AISummarizerStreamFunc(nil))
	if streamType.In(3) != reflect.TypeOf(ai.SimpleStreamOptions{}) {
		t.Fatalf("AISummarizerStreamFunc should use SimpleStreamOptions, got %s", streamType.In(3))
	}
}

func TestAISummarizerCallsStreamAndReturnsText(t *testing.T) {
	var gotMessages []ai.Message
	var gotOptions ai.SimpleStreamOptions
	summarizer := NewAISummarizer(AISummarizerOptions{
		Model:    ai.Model{ID: "summary-model", Provider: ai.Provider("test"), API: ai.Api("fake"), ContextWindow: 8_000, MaxTokens: 5_000},
		Settings: DefaultSettings(),
		Stream: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			if request.SystemPrompt != "sys" {
				t.Fatalf("system prompt should use context field like upstream, got %#v", request)
			}
			gotMessages = append([]ai.Message(nil), request.Messages...)
			gotOptions = options
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "summary text"})
			stream.Close(ai.DoneReasonStop)
			return stream
		},
	})
	summary, err := summarizer.Summarize(context.Background(), SummarizationRequest{SystemPrompt: "sys", Conversation: "USER:\nhello"})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "summary text" {
		t.Fatalf("summary mismatch: %q", summary)
	}
	if len(gotMessages) != 1 || gotMessages[0].Role != ai.RoleUser || !strings.Contains(gotMessages[0].Content[0].Text, "USER:\nhello") {
		t.Fatalf("messages mismatch: %#v", gotMessages)
	}
	if gotOptions.Base.MaxTokens != 2_000 {
		t.Fatalf("summarizer max tokens should be bounded like upstream, got %#v", gotOptions)
	}
}

func TestAISummarizerSummarizeWithUsageReturnsMessageUsageLikeUpstream(t *testing.T) {
	summarizer := NewAISummarizer(AISummarizerOptions{Stream: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventUsage, Usage: &ai.Usage{InputTokens: 7, OutputTokens: 3}})
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "summary text"})
		stream.Close(ai.DoneReasonStop)
		return stream
	}})

	summary, usage, err := summarizer.SummarizeWithUsage(context.Background(), SummarizationRequest{SystemPrompt: "sys", Conversation: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "summary text" || usage.InputTokens != 7 || usage.OutputTokens != 3 {
		t.Fatalf("summary usage mismatch: summary=%q usage=%#v", summary, usage)
	}
}

func TestAISummarizerSurfacesStreamErrorsAndEmptyOutput(t *testing.T) {
	failed := NewAISummarizer(AISummarizerOptions{Stream: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventError, Error: "provider failed"})
		stream.Close(ai.DoneReasonStop)
		return stream
	}})
	if _, err := failed.Summarize(context.Background(), SummarizationRequest{SystemPrompt: "sys", Conversation: "body"}); err == nil || !strings.Contains(err.Error(), "provider failed") {
		t.Fatalf("expected provider error, got %v", err)
	}
	empty := NewAISummarizer(AISummarizerOptions{Stream: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		stream.Close(ai.DoneReasonStop)
		return stream
	}})
	if _, err := empty.Summarize(context.Background(), SummarizationRequest{SystemPrompt: "sys", Conversation: "body"}); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty summary error, got %v", err)
	}
	errSummary := NewAISummarizer(AISummarizerOptions{Stream: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		stream.Emit(ai.AssistantMessageEvent{Type: ai.EventError, Error: errors.New("boom").Error()})
		stream.Close(ai.DoneReasonStop)
		return stream
	}})
	if _, err := errSummary.Summarize(context.Background(), SummarizationRequest{SystemPrompt: "sys", Conversation: "body"}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func TestAISummarizerTrimsConversationToPromptBudget(t *testing.T) {
	var gotConversation string
	summarizer := NewAISummarizer(AISummarizerOptions{
		Model:    ai.Model{ID: "summary-model", Provider: ai.Provider("test"), API: ai.Api("fake"), ContextWindow: 4_000, MaxTokens: 1_000},
		Settings: DefaultSettings(),
		Stream: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			gotConversation = request.Messages[0].Content[0].Text
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "summary"})
			stream.Close(ai.DoneReasonStop)
			return stream
		},
	})
	_, err := summarizer.Summarize(context.Background(), SummarizationRequest{
		SystemPrompt: "sys",
		Conversation: strings.Repeat("old ", 20_000),
		Messages: []agent.Message{
			agent.NewUserMessage(strings.Repeat("old ", 20_000)),
			agent.NewUserMessage("recent tail"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	budget := SummaryPromptBudgetTokens(ai.Model{ContextWindow: 4_000, MaxTokens: 1_000}, DefaultSettings())
	if !strings.Contains(gotConversation, "[compaction note: omitted") || !strings.Contains(gotConversation, "recent tail") || EstimateTextTokens(gotConversation) > budget {
		t.Fatalf("conversation was not trimmed to budget=%d: tokens=%d text=%q", budget, EstimateTextTokens(gotConversation), gotConversation)
	}
}

func TestAISummarizerPromptBudgetAccountsForCustomSystemPromptLikeUpstream(t *testing.T) {
	var gotConversation string
	model := ai.Model{ID: "summary-model", Provider: ai.Provider("test"), API: ai.Api("fake"), ContextWindow: 4_000, MaxTokens: 1_000}
	summarizer := NewAISummarizer(AISummarizerOptions{
		Model:    model,
		Settings: DefaultSettings(),
		Stream: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			gotConversation = request.Messages[0].Content[0].Text
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "summary"})
			stream.Close(ai.DoneReasonStop)
			return stream
		},
	})
	longSystemPrompt := SummarizationSystemPrompt + "\n\n" + strings.Repeat("custom ", 100)

	_, err := summarizer.Summarize(context.Background(), SummarizationRequest{
		SystemPrompt: longSystemPrompt,
		Conversation: strings.Repeat("old ", 50_000),
		Messages:     []agent.Message{agent.NewUserMessage(strings.Repeat("old ", 50_000))},
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := SummaryPromptBudgetTokens(model, DefaultSettings())
	actualPromptEstimate := summaryPromptFramingTokens + EstimateTextTokens(longSystemPrompt) + EstimateTextTokens(gotConversation)
	if actualPromptEstimate > budget {
		t.Fatalf("custom system prompt overhead should fit budget: estimate=%d budget=%d", actualPromptEstimate, budget)
	}
}

func TestAISummarizerRetriesWithSmallerBudgetOnOverflow(t *testing.T) {
	var conversations []string
	summarizer := NewAISummarizer(AISummarizerOptions{
		Model:    ai.Model{ID: "summary-model", Provider: ai.Provider("test"), API: ai.Api("fake"), ContextWindow: 4_000, MaxTokens: 1_000},
		Settings: DefaultSettings(),
		Stream: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			conversations = append(conversations, request.Messages[0].Content[0].Text)
			stream := ai.NewAssistantMessageEventStream()
			if len(conversations) == 1 {
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventError, Error: "prompt is too long"})
				stream.Close(ai.DoneReasonStop)
				return stream
			}
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventTextDelta, Delta: "summary after retry"})
			stream.Close(ai.DoneReasonStop)
			return stream
		},
	})
	summary, err := summarizer.Summarize(context.Background(), SummarizationRequest{
		SystemPrompt: "sys",
		Conversation: strings.Repeat("old ", 20_000),
		Messages: []agent.Message{
			agent.NewUserMessage(strings.Repeat("old ", 20_000)),
			agent.NewUserMessage("recent tail"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary != "summary after retry" || len(conversations) != 2 {
		t.Fatalf("retry result mismatch summary=%q calls=%d", summary, len(conversations))
	}
	if EstimateTextTokens(conversations[1]) > EstimateTextTokens(conversations[0]) || !strings.Contains(conversations[1], "recent tail") {
		t.Fatalf("retry should use a smaller bounded prompt: %#v", conversations)
	}
}

func TestAISummarizerDoesNotRetryNonOverflowError(t *testing.T) {
	calls := 0
	summarizer := NewAISummarizer(AISummarizerOptions{
		Model:    ai.Model{ID: "summary-model", Provider: ai.Provider("test"), API: ai.Api("fake"), ContextWindow: 4_000, MaxTokens: 1_000},
		Settings: DefaultSettings(),
		Stream: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			calls++
			stream := ai.NewAssistantMessageEventStream()
			stream.Emit(ai.AssistantMessageEvent{Type: ai.EventError, Error: "rate limit"})
			stream.Close(ai.DoneReasonStop)
			return stream
		},
	})
	_, err := summarizer.Summarize(context.Background(), SummarizationRequest{SystemPrompt: "sys", Conversation: "body", Messages: []agent.Message{agent.NewUserMessage("body")}})
	if err == nil || !strings.Contains(err.Error(), "rate limit") || calls != 1 {
		t.Fatalf("non-overflow error should not retry: err=%v calls=%d", err, calls)
	}
}
