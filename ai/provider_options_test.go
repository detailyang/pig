package ai

import (
	"encoding/json"
	"testing"
)

func TestOpenAIProviderOptionsMarshalLikeUpstreamSerde(t *testing.T) {
	responses, err := json.Marshal(OpenAIResponsesOptions{ReasoningEffort: "high", ReasoningSummary: "detailed", ServiceTier: "priority"})
	if err != nil {
		t.Fatal(err)
	}
	if string(responses) != `{"reasoning_effort":"high","reasoning_summary":"detailed","service_tier":"priority"}` {
		t.Fatalf("responses options mismatch: %s", responses)
	}

	completions, err := json.Marshal(OpenAICompletionsOptions{ReasoningEffort: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if string(completions) != `{"reasoning_effort":"medium"}` {
		t.Fatalf("completions options mismatch: %s", completions)
	}

	empty, err := json.Marshal(OpenAIResponsesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) != `{}` {
		t.Fatalf("empty options should omit nil fields like upstream serde, got %s", empty)
	}
}

func TestGoogleAndAnthropicProviderOptionsMarshalLikeUpstreamSerde(t *testing.T) {
	google, err := json.Marshal(GoogleOptions{ThinkingEnabled: boolPtr(true), ThinkingBudgetTokens: intPtr(1024)})
	if err != nil {
		t.Fatal(err)
	}
	if string(google) != `{"thinking_enabled":true,"thinking_budget_tokens":1024}` {
		t.Fatalf("google options mismatch: %s", google)
	}

	anthropic, err := json.Marshal(AnthropicOptions{Thinking: &AnthropicThinking{Kind: "enabled", BudgetTokens: intPtr(4096)}})
	if err != nil {
		t.Fatal(err)
	}
	if string(anthropic) != `{"thinking":{"type":"enabled","budget_tokens":4096}}` {
		t.Fatalf("anthropic options mismatch: %s", anthropic)
	}

	empty, err := json.Marshal(AnthropicOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) != `{}` {
		t.Fatalf("empty anthropic options should omit nil fields like upstream serde, got %s", empty)
	}
}

func boolPtr(value bool) *bool { return &value }

func intPtr(value int) *int { return &value }
