package ai

import "testing"

func TestOpenAIPlaceholderHelpersMatchUpstreamStubs(t *testing.T) {
	OpenAIPromptCachePlaceholder()
	OpenAIResponsesSharedPlaceholder()
	Placeholder()
}
