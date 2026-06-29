package ai

import "regexp"

type ContextOverflow struct {
	Overflowed      bool `json:"overflowed"`
	RequestedTokens int  `json:"requestedTokens,omitempty"`
	LimitTokens     int  `json:"limitTokens,omitempty"`
}

var overflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)prompt is too long`),
	regexp.MustCompile(`(?i)request_too_large`),
	regexp.MustCompile(`(?i)input is too long for requested model`),
	regexp.MustCompile(`(?i)exceeds the context window`),
	regexp.MustCompile(`(?i)exceeds (?:the )?(?:model'?s )?maximum context length of [\d,]+ tokens?`),
	regexp.MustCompile(`(?i)input token count.*exceeds the maximum`),
	regexp.MustCompile(`(?i)maximum prompt length is \d+`),
	regexp.MustCompile(`(?i)reduce the length of the messages`),
	regexp.MustCompile(`(?i)maximum context length is \d+ tokens`),
	regexp.MustCompile(`(?i)input \(\d+ tokens\) is longer than the model'?s context length \(\d+ tokens\)`),
	regexp.MustCompile(`(?i)exceeds the limit of \d+`),
	regexp.MustCompile(`(?i)exceeds the available context size`),
	regexp.MustCompile(`(?i)greater than the context length`),
	regexp.MustCompile(`(?i)context window exceeds limit`),
	regexp.MustCompile(`(?i)exceeded model token limit`),
	regexp.MustCompile(`(?i)too large for model with \d+ maximum context length`),
	regexp.MustCompile(`(?i)model_context_window_exceeded`),
	regexp.MustCompile(`(?i)prompt too long; exceeded (?:max )?context length`),
	regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),
	regexp.MustCompile(`(?i)too many tokens`),
	regexp.MustCompile(`(?i)token limit exceeded`),
	regexp.MustCompile(`(?i)^4(?:00|13)\s*(?:status code)?\s*\(no body\)`),
}

var nonOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(Throttling error|Service unavailable):`),
	regexp.MustCompile(`(?i)rate limit`),
	regexp.MustCompile(`(?i)too many requests`),
}

func IsContextOverflow(message AssistantMessage, contextWindow int) bool {
	return DetectContextOverflow(message, contextWindow).Overflowed
}

func DetectContextOverflow(message AssistantMessage, contextWindow int) ContextOverflow {
	if message.StopReason == StopReasonError && message.ErrorMessage != "" {
		if !matchesAny(nonOverflowPatterns, message.ErrorMessage) && matchesAny(overflowPatterns, message.ErrorMessage) {
			return ContextOverflow{Overflowed: true}
		}
	}

	if contextWindow <= 0 || message.Usage == nil {
		return ContextOverflow{}
	}

	inputTokens := message.Usage.InputTokens + message.Usage.CacheReadTokens
	if message.StopReason == StopReasonEndTurn && inputTokens > contextWindow {
		return ContextOverflow{Overflowed: true, RequestedTokens: inputTokens, LimitTokens: contextWindow}
	}
	if message.StopReason == StopReasonMaxTokens && message.Usage.OutputTokens == 0 && float64(inputTokens) >= float64(contextWindow)*0.99 {
		return ContextOverflow{Overflowed: true, RequestedTokens: inputTokens, LimitTokens: contextWindow}
	}
	return ContextOverflow{}
}

func matchesAny(patterns []*regexp.Regexp, text string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}
