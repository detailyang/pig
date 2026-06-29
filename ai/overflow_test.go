package ai

import "testing"

func overflowErrorMessage(text string) AssistantMessage {
	return AssistantMessage{StopReason: StopReasonError, ErrorMessage: text}
}

func TestIsContextOverflowDetectsProviderErrors(t *testing.T) {
	cases := []string{
		"prompt is too long: 213462 tokens > 200000 maximum",
		"Your input exceeds the context window of this model",
		"The input token count (1196265) exceeds the maximum number of tokens allowed",
		"400 status code (no body)",
	}
	for _, text := range cases {
		if !IsContextOverflow(overflowErrorMessage(text), 0) {
			t.Fatalf("expected overflow for %q", text)
		}
	}
}

func TestIsContextOverflowExcludesRateLimits(t *testing.T) {
	cases := []string{
		"Throttling error: Too many tokens, please wait before trying again.",
		"rate limit exceeded",
		"too many requests",
	}
	for _, text := range cases {
		if IsContextOverflow(overflowErrorMessage(text), 0) {
			t.Fatalf("unexpected overflow for %q", text)
		}
	}
}

func TestIsContextOverflowDetectsSilentOverflow(t *testing.T) {
	message := AssistantMessage{StopReason: StopReasonEndTurn, Usage: &Usage{InputTokens: 250_000}}
	if !IsContextOverflow(message, 200_000) {
		t.Fatal("expected usage over context window to overflow")
	}
	report := DetectContextOverflow(message, 200_000)
	if !report.Overflowed || report.RequestedTokens != 250_000 || report.LimitTokens != 200_000 {
		t.Fatalf("overflow report mismatch: %#v", report)
	}
	if IsContextOverflow(message, 300_000) {
		t.Fatal("did not expect usage under context window to overflow")
	}
}

func TestIsContextOverflowDetectsLengthStopZeroOutput(t *testing.T) {
	message := AssistantMessage{StopReason: StopReasonMaxTokens, Usage: &Usage{InputTokens: 199_000, OutputTokens: 0}}
	if !IsContextOverflow(message, 200_000) {
		t.Fatal("expected length stop with no output near context window to overflow")
	}
	message.Usage.OutputTokens = 1
	if IsContextOverflow(message, 200_000) {
		t.Fatal("did not expect nonzero output to count as silent overflow")
	}
}
