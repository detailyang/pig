package compaction

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

func TestGenerateSummaryUsesCustomInstructionsBudgetAndMaxOutput(t *testing.T) {
	var gotPrompt string
	var gotConversation string
	var gotMaxTokens int
	budget := summaryPromptOverheadTokensFor(SummarizationSystemPrompt+"\n\nkeep decisions") + 80
	request := GenerateSummaryRequest{
		Model:              ai.Model{ID: "summary-model", Provider: ai.Provider("test"), API: ai.Api("fake"), ContextWindow: 4_000, MaxTokens: 1_000},
		Messages:           []agent.Message{agent.NewUserMessage(strings.Repeat("old ", 300)), agent.NewAssistantMessage("recent tail")},
		CustomInstructions: "keep decisions",
		PromptBudgetTokens: &budget,
		MaxOutputTokens:    intPtr(123),
		StreamFn: func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			gotPrompt = request.SystemPrompt
			gotConversation = request.Messages[0].Content[0].Text
			gotMaxTokens = options.Base.MaxTokens
			stream := ai.NewAssistantMessageEventStream()
			ai.ReplayFauxMessage(stream, ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("summary text")}))
			return stream
		},
	}

	output, err := GenerateSummary(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if output.Summary != "summary text" {
		t.Fatalf("summary mismatch: %#v", output)
	}
	if !strings.Contains(gotPrompt, SummarizationSystemPrompt) || !strings.Contains(gotPrompt, "keep decisions") {
		t.Fatalf("prompt should include base prompt and custom instructions: %q", gotPrompt)
	}
	if gotMaxTokens != 123 {
		t.Fatalf("max output tokens mismatch: %d", gotMaxTokens)
	}
	if !strings.Contains(gotConversation, "recent tail") || !strings.Contains(gotConversation, "compaction note") {
		t.Fatalf("conversation should be budget-trimmed with note and tail: %q", gotConversation)
	}
}

func TestGenerateSummaryErrorKinds(t *testing.T) {
	cases := []struct {
		name string
		fn   AISummarizerStreamFunc
		kind SummarizeErrorKind
	}{
		{
			name: "provider",
			fn: func(context.Context, ai.Model, ai.Context, ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				stream := ai.NewAssistantMessageEventStream()
				stream.Emit(ai.AssistantMessageEvent{Type: ai.EventError, Error: "provider failed"})
				return stream
			},
			kind: SummarizeErrorProvider,
		},
		{
			name: "overflow",
			fn: func(context.Context, ai.Model, ai.Context, ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				stream := ai.NewAssistantMessageEventStream()
				ai.ReplayFauxMessage(stream, ai.AssistantMessage{StopReason: ai.StopReasonError, ErrorMessage: "context length exceeded"})
				return stream
			},
			kind: SummarizeErrorContextOverflow,
		},
		{
			name: "empty",
			fn: func(context.Context, ai.Model, ai.Context, ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				stream := ai.NewAssistantMessageEventStream()
				stream.Close(ai.DoneReasonStop)
				return stream
			},
			kind: SummarizeErrorEmpty,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GenerateSummary(context.Background(), GenerateSummaryRequest{Model: ai.Model{ContextWindow: 100}, Messages: []agent.Message{agent.NewUserMessage("hello")}, Stream: tc.fn})
			var summarizeErr SummarizeError
			if !errors.As(err, &summarizeErr) || summarizeErr.Kind != tc.kind {
				t.Fatalf("error kind mismatch: err=%#v", err)
			}
		})
	}
}

func intPtr(value int) *int { return &value }
