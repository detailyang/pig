package compaction

import (
	"context"
	"fmt"
	"strings"

	"github.com/detailyang/pig/ai"
)

const minSummaryPromptBudgetTokens uint64 = 1_024
const maxSummaryOverflowRetries = 3

type AISummarizerStreamFunc func(ctx context.Context, model ai.Model, request ai.Context, options ai.SimpleStreamOptions) *ai.AssistantMessageEventStream

type AISummarizerOptions struct {
	Model    ai.Model
	Settings Settings
	Options  ai.SimpleStreamOptions
	Stream   AISummarizerStreamFunc
}

type AISummarizer struct {
	model    ai.Model
	settings Settings
	options  ai.SimpleStreamOptions
	stream   AISummarizerStreamFunc
}

func NewAISummarizer(options AISummarizerOptions) *AISummarizer {
	stream := options.Stream
	if stream == nil {
		stream = ai.StreamSimple
	}
	settings := options.Settings
	if settings == (Settings{}) {
		settings = DefaultSettings()
	}
	return &AISummarizer{model: options.Model, settings: settings, options: options.Options, stream: stream}
}

func (summarizer *AISummarizer) Summarize(ctx context.Context, request SummarizationRequest) (string, error) {
	summary, _, err := summarizer.SummarizeWithUsage(ctx, request)
	return summary, err
}

func (summarizer *AISummarizer) SummarizeWithUsage(ctx context.Context, request SummarizationRequest) (string, ai.Usage, error) {
	options := summarizer.options
	if options.Base.MaxTokens == 0 {
		options.Base.MaxTokens = SummaryOutputTokens(summarizer.model, summarizer.settings)
	}
	budget := SummaryPromptBudgetTokens(summarizer.model, summarizer.settings)
	attempts := 0
	for {
		conversation := request.Conversation
		if budget > 0 && len(request.Messages) > 0 {
			conversation = serializeConversationForSummaryBudgetWithOverhead(request.Messages, budget, summaryPromptOverheadTokensFor(request.SystemPrompt))
		}
		message, ok := summarizer.stream(ctx, summarizer.model, ai.Context{
			SystemPrompt: request.SystemPrompt,
			Messages: []ai.Message{
				{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: conversation}}},
			},
		}, options).Result()
		if !ok {
			return "", ai.Usage{}, fmt.Errorf("summarizer stream did not complete")
		}
		if message.StopReason == ai.StopReasonError {
			if budget > minSummaryPromptBudgetTokens && attempts < maxSummaryOverflowRetries && ai.IsContextOverflow(message, summarizer.model.ContextWindow) {
				attempts++
				budget /= 2
				if budget < minSummaryPromptBudgetTokens {
					budget = minSummaryPromptBudgetTokens
				}
				continue
			}
			return "", ai.Usage{}, fmt.Errorf("summarizer failed: %s", message.ErrorMessage)
		}
		summary := strings.TrimSpace(message.Text())
		if summary == "" {
			return "", ai.Usage{}, fmt.Errorf("summarizer returned empty summary")
		}
		usage := ai.Usage{}
		if message.Usage != nil {
			usage = *message.Usage
		}
		return summary, usage, nil
	}
}
