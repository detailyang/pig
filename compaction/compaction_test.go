package compaction

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

type compactUsageSummarizer struct{}

func (compactUsageSummarizer) Summarize(ctx context.Context, request SummarizationRequest) (string, error) {
	return "fallback summary", nil
}

func (compactUsageSummarizer) SummarizeWithUsage(ctx context.Context, request SummarizationRequest) (string, ai.Usage, error) {
	return "summary of old context", ai.Usage{InputTokens: 11, OutputTokens: 5}, nil
}

func TestCompactionErrorUpstreamExportedNames(t *testing.T) {
	var code CompactionErrorCode = CompactionErrorSummarizationFailed
	data, err := json.Marshal(code)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"summarization_failed"` {
		t.Fatalf("compaction error code should marshal snake_case, got %s", data)
	}

	var compactionErr CompactionError = CompactionError{Code: CompactionErrorInvalidSession, Message: "bad session"}
	if compactionErr.Error() != "bad session" {
		t.Fatalf("compaction error message mismatch: %q", compactionErr.Error())
	}
}

func TestEstimateTextTokensASCIIAndCJK(t *testing.T) {
	if EstimateTextTokens("abcd") != 1 || EstimateTextTokens("abcde") != 2 || EstimateTextTokens("你好") != 2 {
		t.Fatalf("bad token estimates")
	}
}

func TestEstimateTokensIgnoresLegacyAssistantToolCallsLikeUpstream(t *testing.T) {
	message := agent.Message{Kind: agent.MessageKindLLM, LLM: &ai.Message{
		Role:      ai.RoleAssistant,
		Content:   []ai.ContentBlock{{Type: ai.ContentText, Text: "ok"}},
		ToolCalls: []ai.ToolCall{{Name: strings.Repeat("x", 40), Arguments: map[string]any{"text": strings.Repeat("y", 40)}}},
	}}

	if got, want := EstimateTokens(message), EstimateTextTokens("ok"); got != want {
		t.Fatalf("legacy assistant ToolCalls should not affect token estimate like upstream: got %d want %d", got, want)
	}
}

func TestCalculateContextTokensFallsBackWhenTotalIsZeroLikeUpstream(t *testing.T) {
	usage := &ai.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 2, CacheWriteTokens: 1, TotalTokenCount: 0, HasTotalTokens: true}

	if got := CalculateContextTokens(usage); got != 18 {
		t.Fatalf("zero totalTokens should fall back to component sum like upstream, got %d", got)
	}
}

func TestCompactionUpstreamExportedNames(t *testing.T) {
	var settings CompactionSettings = DefaultCompactionSettings
	if settings != DefaultSettings() {
		t.Fatalf("compaction settings alias mismatch: %#v", settings)
	}
	if DEFAULT_COMPACTION_SETTINGS != DefaultCompactionSettings {
		t.Fatalf("uppercase default settings mismatch")
	}
	if SUMMARIZATION_SYSTEM_PROMPT != SummarizationSystemPrompt {
		t.Fatalf("uppercase summarization prompt mismatch")
	}

	entries := []session.Entry{
		messageEntry("u1", nil, agent.NewUserMessage("old user")),
		messageEntry("u2", strPtr("u1"), agent.NewUserMessage("tail")),
	}
	settings.KeepRecentTokens = 1
	var cut CutPointResult = FindCutPoint(entries, settings)
	if cut.CutIndex != 1 || cut.FirstKeptEntryID == nil || *cut.FirstKeptEntryID != "u2" {
		t.Fatalf("cut point alias mismatch: %#v", cut)
	}

	var prep CompactionPreparation = PrepareCompaction(entries, settings)
	if prep.Cut.CutIndex != cut.CutIndex || len(prep.EntriesToSummarize) != 1 {
		t.Fatalf("compaction preparation alias mismatch: %#v", prep)
	}

	var result CompactionResult = Result{Compacted: true, Summary: "summary", Cut: cut, TokensBefore: prep.TokensBefore}
	if result.Summary != "summary" || !result.Compacted {
		t.Fatalf("compaction result alias mismatch: %#v", result)
	}
	if CalculateContextTokens(nil) != 0 || EstimateTextTokens("abcd") != 1 || ShouldCompact(81, 100, settings) != settings.Enabled {
		t.Fatalf("upstream helper names should remain callable")
	}
}

func TestCompactCallsSummarizerAndKeepsTail(t *testing.T) {
	entries := []session.Entry{
		messageEntry("u1", nil, agent.NewUserMessage("old user")),
		messageEntry("a1", strPtr("u1"), agent.NewAssistantMessage("old assistant")),
		messageEntry("u2", strPtr("a1"), agent.NewUserMessage("new user")),
		messageEntry("a2", strPtr("u2"), agent.NewAssistantMessage("new assistant")),
	}
	settings := DefaultSettings()
	settings.KeepRecentTokens = 2
	var got SummarizationRequest
	result, err := Compact(context.Background(), entries, settings, SummarizerFunc(func(ctx context.Context, request SummarizationRequest) (string, error) {
		got = request
		return "summary of old context", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Conversation, "old user") || strings.Contains(got.Conversation, "new user") {
		t.Fatalf("summarizer conversation mismatch: %q", got.Conversation)
	}
	if result.Summary != "summary of old context" || result.Cut.CutIndex != 2 {
		t.Fatalf("result metadata mismatch: %#v", result)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("expected summary + 2 kept messages, got %#v", result.Messages)
	}
	if result.Messages[0].LLM == nil || result.Messages[0].LLM.Role != ai.RoleUser || !strings.Contains(result.Messages[0].LLM.Content[0].Text, "summary of old context") {
		t.Fatalf("summary message mismatch: %#v", result.Messages[0])
	}
	if result.Messages[1].LLM.Content[0].Text != "new user" || result.Messages[2].LLM.Content[0].Text != "new assistant" {
		t.Fatalf("kept messages mismatch: %#v", result.Messages)
	}
}

func TestCompactReturnsSummarizerUsageLikeUpstream(t *testing.T) {
	entries := []session.Entry{
		messageEntry("u1", nil, agent.NewUserMessage("old user")),
		messageEntry("a1", strPtr("u1"), agent.NewAssistantMessage("old assistant")),
		messageEntry("u2", strPtr("a1"), agent.NewUserMessage("new user")),
	}
	settings := DefaultSettings()
	settings.KeepRecentTokens = 2

	result, err := Compact(context.Background(), entries, settings, compactUsageSummarizer{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Summary != "summary of old context" || result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 5 {
		t.Fatalf("compact should include summarizer usage like upstream: %#v", result)
	}
}

func TestCompactNoopWhenNothingToSummarize(t *testing.T) {
	result, err := Compact(context.Background(), nil, DefaultSettings(), SummarizerFunc(func(ctx context.Context, request SummarizationRequest) (string, error) {
		t.Fatal("summarizer should not be called")
		return "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Compacted || len(result.Messages) != 0 {
		t.Fatalf("noop mismatch: %#v", result)
	}
}

func TestEstimateContextTokensUsesLastAssistantUsage(t *testing.T) {
	messages := []agent.Message{
		agent.NewUserMessage("one two three four"),
		assistantWithUsage("ok", &ai.Usage{InputTokens: 100, OutputTokens: 20}),
		agent.NewUserMessage("tail text"),
	}
	est := EstimateContextTokens(messages)
	if est.UsageTokens != 120 || est.LastUsageIndex == nil || *est.LastUsageIndex != 1 || est.Tokens <= 120 || est.TrailingTokens == 0 {
		t.Fatalf("estimate mismatch: %#v", est)
	}
}

func TestEstimateTokensForToolResultUsesToolNameAndContentLikeUpstream(t *testing.T) {
	message := ai.Message{Role: ai.RoleTool, ToolName: "read", ToolCallID: "ignored-call-id", Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "failed"}}}

	got := EstimateTokens(agent.Message{Kind: agent.MessageKindLLM, LLM: &message})
	want := EstimateTextTokens("read") + EstimateTextTokens("failed")

	if got != want {
		t.Fatalf("tool result token estimate should match upstream tool_name + content: got %d want %d", got, want)
	}
}

func TestEstimateContextTokensIgnoresErroredAssistantUsage(t *testing.T) {
	errored := assistantWithUsage("overflow", &ai.Usage{InputTokens: 50_000, OutputTokens: 1})
	errored.LLM.StopReason = ai.StopReasonError
	errored.LLM.ErrorMessage = "context length exceeded"
	messages := []agent.Message{
		agent.NewUserMessage("short"),
		errored,
	}
	est := EstimateContextTokens(messages)
	if est.UsageTokens != 0 || est.LastUsageIndex != nil || est.Tokens >= 50_000 {
		t.Fatalf("errored usage should be ignored: %#v", est)
	}
}

func TestGetLastAssistantUsageSkipsErroredMessages(t *testing.T) {
	good := assistantWithUsage("ok", &ai.Usage{InputTokens: 10, OutputTokens: 2})
	errored := assistantWithUsage("bad", &ai.Usage{InputTokens: 50_000, OutputTokens: 1})
	errored.LLM.StopReason = ai.StopReasonError
	entries := []session.Entry{
		messageEntry("a1", nil, good),
		messageEntry("a2", strPtr("a1"), errored),
	}
	usage, ok := GetLastAssistantUsage(entries)
	if !ok || usage.InputTokens != 10 || usage.OutputTokens != 2 {
		t.Fatalf("usage mismatch: %#v ok=%v", usage, ok)
	}
}

func TestShouldCompactThresholdAndDisabled(t *testing.T) {
	settings := DefaultSettings()
	if ShouldCompact(80_000, 100_000, settings) {
		t.Fatal("threshold is strict greater than 80%")
	}
	if !ShouldCompact(80_001, 100_000, settings) {
		t.Fatal("should compact above threshold")
	}
	settings.Enabled = false
	if ShouldCompact(90_000, 100_000, settings) {
		t.Fatal("disabled compaction should not run")
	}
}

func TestSummaryOutputTokensMatchesUpstreamBounds(t *testing.T) {
	settings := DefaultSettings()
	if got := SummaryOutputTokens(ai.Model{MaxTokens: 8_000}, settings); got != 8_000 {
		t.Fatalf("model max should cap reserve, got %d", got)
	}
	if got := SummaryOutputTokens(ai.Model{ContextWindow: 8_000, MaxTokens: 5_000}, settings); got != 2_000 {
		t.Fatalf("context quarter should cap output, got %d", got)
	}
	settings.ReserveTokens = 0
	if got := SummaryOutputTokens(ai.Model{}, settings); got != DefaultSettings().ReserveTokens {
		t.Fatalf("zero reserve should fall back to default, got %d", got)
	}
	if got := SummaryOutputTokens(ai.Model{ContextWindow: 1}, settings); got != 1 {
		t.Fatalf("context cap should be at least one token, got %d", got)
	}
}

func TestFindCutPointKeepsRecentTurnBoundary(t *testing.T) {
	entries := []session.Entry{
		messageEntry("u1", nil, agent.NewUserMessage(strings.Repeat("a", 100))),
		messageEntry("a1", strPtr("u1"), agent.NewAssistantMessage(strings.Repeat("b", 100))),
		messageEntry("u2", strPtr("a1"), agent.NewUserMessage("short")),
		messageEntry("a2", strPtr("u2"), agent.NewAssistantMessage("tail")),
	}
	settings := DefaultSettings()
	settings.KeepRecentTokens = 2
	cut := FindCutPoint(entries, settings)
	if cut.CutIndex != 2 || cut.FirstKeptEntryID == nil || *cut.FirstKeptEntryID != "u2" {
		t.Fatalf("cut mismatch: %#v", cut)
	}
}

func TestPrepareCompactionAndSerializeConversation(t *testing.T) {
	entries := []session.Entry{
		messageEntry("u1", nil, agent.NewUserMessage("hello")),
		messageEntry("a1", strPtr("u1"), agent.NewAssistantMessage("world")),
		messageEntry("u2", strPtr("a1"), agent.NewUserMessage("tail")),
	}
	settings := DefaultSettings()
	settings.KeepRecentTokens = 1
	prep := PrepareCompaction(entries, settings)
	if len(prep.EntriesToSummarize) == 0 || prep.TokensBefore == 0 {
		t.Fatalf("prep mismatch: %#v", prep)
	}
	tool := ai.Message{Role: ai.RoleTool, Name: "legacy", ToolName: "read", IsError: true, Details: map[string]any{"exit_code": float64(2)}, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "failed"}}}
	text := SerializeConversation([]agent.Message{agent.NewUserMessage("hello"), agent.NewAssistantMessage("world"), {Kind: agent.MessageKindLLM, LLM: &tool}})
	if !strings.Contains(text, "USER:\nhello") || !strings.Contains(text, "ASSISTANT:\nworld") || !strings.Contains(text, "TOOL_RESULT[read]:\nfailed") || strings.Contains(text, "<details>") {
		t.Fatalf("serialized mismatch: %q", text)
	}
}

func TestSerializeConversationToolResultOmitsErrorAndDetailsLikeUpstream(t *testing.T) {
	tool := ai.Message{Role: ai.RoleTool, ToolName: "read", IsError: true, Details: map[string]any{"exit_code": float64(2)}, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "failed"}}}

	text := SerializeConversation([]agent.Message{{Kind: agent.MessageKindLLM, LLM: &tool}})

	if text != "TOOL_RESULT[read]:\nfailed\n\n" {
		t.Fatalf("tool result serialization should match upstream, got %q", text)
	}
}

func TestSerializeConversationAssistantOmitsDetailsLikeUpstream(t *testing.T) {
	message := ai.Message{Role: ai.RoleAssistant, Details: map[string]any{"trace": "a < b"}, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "done"}}}

	text := SerializeConversation([]agent.Message{{Kind: agent.MessageKindLLM, LLM: &message}})

	if text != "ASSISTANT:\ndone\n\n" {
		t.Fatalf("assistant details should be omitted like upstream, got %q", text)
	}
}

func TestSerializeConversationToolCallArgumentsDoNotHTMLEscapeLikeUpstream(t *testing.T) {
	message := ai.Message{Role: ai.RoleAssistant, ToolCalls: []ai.ToolCall{{Name: "write", Arguments: map[string]any{"text": "a < b && c > d"}}}}

	text := SerializeConversation([]agent.Message{{Kind: agent.MessageKindLLM, LLM: &message}})

	if text != "ASSISTANT:\n<tool_call name=\"write\">{\"text\":\"a < b && c > d\"}</tool_call>\n\n" {
		t.Fatalf("tool call serialization should match upstream serde_json formatting, got %q", text)
	}
}

func TestSerializeConversationContentBlockToolCallLikeUpstream(t *testing.T) {
	message := ai.Message{Role: ai.RoleAssistant, Content: []ai.ContentBlock{{Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{Name: "write", Arguments: map[string]any{"text": "a < b && c > d"}}}}}

	text := SerializeConversation([]agent.Message{{Kind: agent.MessageKindLLM, LLM: &message}})

	if text != "ASSISTANT:\n<tool_call name=\"write\">{\"text\":\"a < b && c > d\"}</tool_call>\n\n" {
		t.Fatalf("content block tool call serialization should match upstream, got %q", text)
	}
}

func TestSerializeConversationCustomPayloadDoesNotHTMLEscapeLikeUpstream(t *testing.T) {
	message := agent.Message{Kind: agent.MessageKindCustom, Custom: &agent.CustomMessage{Role: "trigger", Payload: map[string]any{"summary": "a < b && c > d"}}}

	text := SerializeConversation([]agent.Message{message})

	if text != "TRIGGER:\n{\"summary\":\"a < b && c > d\"}\n\n" {
		t.Fatalf("custom payload serialization should match upstream serde_json formatting, got %q", text)
	}
}

func TestEstimateTokensCustomPayloadDoesNotHTMLEscapeLikeUpstream(t *testing.T) {
	message := agent.Message{Kind: agent.MessageKindCustom, Custom: &agent.CustomMessage{Role: "trigger", Payload: map[string]any{"summary": "a < b && c > d"}}}
	want := EstimateTextTokens("trigger") + EstimateTextTokens(`{"summary":"a < b && c > d"}`)
	if got := EstimateTokens(message); got != want {
		t.Fatalf("custom payload token estimate should use serde_json-style no-escape payload: got %d want %d", got, want)
	}
}

func TestSerializeConversationForSummaryBudgetOmitsOlderMessages(t *testing.T) {
	messages := []agent.Message{
		agent.NewUserMessage(strings.Repeat("old ", 400)),
		agent.NewAssistantMessage(strings.Repeat("middle ", 200)),
		agent.NewUserMessage("recent tail"),
	}
	conversation := SerializeConversationForSummaryBudget(messages, 700)
	if !strings.Contains(conversation, "[compaction note: omitted") {
		t.Fatalf("expected omission note, got %q", conversation)
	}
	if strings.Contains(conversation, "old old old") || !strings.Contains(conversation, "recent tail") {
		t.Fatalf("conversation should keep recent content only: %q", conversation)
	}
}

func TestSerializeConversationForSummaryBudgetCapsSingleOversizedMessage(t *testing.T) {
	conversation := SerializeConversationForSummaryBudget([]agent.Message{agent.NewUserMessage(strings.Repeat("x", 50_000))}, 2_000)
	if !strings.HasPrefix(conversation, "[compaction note: omitted older serialized content") {
		t.Fatalf("expected serialized-content omission note, got %q", conversation[:min(len(conversation), 120)])
	}
	if EstimateTextTokens(conversation) > 2_000 {
		t.Fatalf("serialized compaction prompt must fit budget, got %d", EstimateTextTokens(conversation))
	}
}

func assistantWithUsage(text string, usage *ai.Usage) agent.Message {
	message := agent.NewAssistantMessage(text)
	message.LLM.Usage = usage
	return message
}

func messageEntry(id string, parent *string, message agent.Message) session.Entry {
	return session.NewMessageEntry(id, parent, "t", message)
}

func strPtr(value string) *string { return &value }
