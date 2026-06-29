package compaction

import (
	"context"
	"strings"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type SummarizeErrorKind string

const (
	SummarizeErrorAborted         SummarizeErrorKind = "aborted"
	SummarizeErrorProvider        SummarizeErrorKind = "provider"
	SummarizeErrorContextOverflow SummarizeErrorKind = "context_overflow"
	SummarizeErrorEmpty           SummarizeErrorKind = "empty"
)

type SummarizeError struct {
	Kind    SummarizeErrorKind
	Message string
}

func (err SummarizeError) Error() string {
	switch err.Kind {
	case SummarizeErrorAborted:
		return "aborted"
	case SummarizeErrorContextOverflow:
		return "summarizer prompt overflowed the model context window: " + err.Message
	case SummarizeErrorEmpty:
		return "summarizer produced no message"
	default:
		return "provider error: " + err.Message
	}
}

type GenerateSummaryRequest struct {
	Model              ai.Model
	Messages           []agent.Message
	CustomInstructions string
	PromptBudgetTokens *uint64
	MaxOutputTokens    *int
	Stream             AISummarizerStreamFunc
	StreamFn           AISummarizerStreamFunc
}

type GenerateSummaryOutput struct {
	Summary string
	Usage   ai.Usage
}

func GenerateSummary(ctx context.Context, request GenerateSummaryRequest) (GenerateSummaryOutput, error) {
	if err := ctx.Err(); err != nil {
		return GenerateSummaryOutput{}, SummarizeError{Kind: SummarizeErrorAborted, Message: err.Error()}
	}
	prompt := SummarizationSystemPrompt
	if strings.TrimSpace(request.CustomInstructions) != "" {
		prompt += "\n\n" + request.CustomInstructions
	}
	conversation := SerializeConversation(request.Messages)
	if request.PromptBudgetTokens != nil {
		conversation = serializeConversationForSummaryBudgetWithOverhead(request.Messages, *request.PromptBudgetTokens, summaryPromptOverheadTokensFor(prompt))
	}
	options := ai.SimpleStreamOptions{}
	if request.MaxOutputTokens != nil {
		options.Base.MaxTokens = *request.MaxOutputTokens
	}
	stream := request.Stream
	if stream == nil {
		stream = request.StreamFn
	}
	if stream == nil {
		stream = ai.StreamSimple
	}
	message, ok := stream(ctx, request.Model, ai.Context{
		SystemPrompt: prompt,
		Messages:     []ai.Message{{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: conversation}}}},
	}, options).Result()
	if err := ctx.Err(); err != nil {
		return GenerateSummaryOutput{}, SummarizeError{Kind: SummarizeErrorAborted, Message: err.Error()}
	}
	if !ok {
		return GenerateSummaryOutput{}, SummarizeError{Kind: SummarizeErrorEmpty}
	}
	if message.StopReason == ai.StopReasonError {
		messageText := message.ErrorMessage
		if messageText == "" {
			messageText = "summarization failed"
		}
		if ai.IsContextOverflow(message, request.Model.ContextWindow) {
			return GenerateSummaryOutput{}, SummarizeError{Kind: SummarizeErrorContextOverflow, Message: messageText}
		}
		return GenerateSummaryOutput{}, SummarizeError{Kind: SummarizeErrorProvider, Message: messageText}
	}
	summary := strings.TrimSpace(message.Text())
	if summary == "" {
		return GenerateSummaryOutput{}, SummarizeError{Kind: SummarizeErrorEmpty}
	}
	usage := ai.Usage{}
	if message.Usage != nil {
		usage = *message.Usage
	}
	return GenerateSummaryOutput{Summary: summary, Usage: usage}, nil
}
