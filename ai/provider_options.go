package ai

type OpenAICompletionsOptions struct {
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type OpenAIResponsesOptions struct {
	ReasoningEffort  string `json:"reasoning_effort,omitempty"`
	ReasoningSummary string `json:"reasoning_summary,omitempty"`
	ServiceTier      string `json:"service_tier,omitempty"`
}

type GoogleOptions struct {
	ThinkingEnabled      *bool `json:"thinking_enabled,omitempty"`
	ThinkingBudgetTokens *int  `json:"thinking_budget_tokens,omitempty"`
}

type AnthropicOptions struct {
	Thinking *AnthropicThinking `json:"thinking,omitempty"`
}

type AnthropicThinking struct {
	Kind         string `json:"type"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}
