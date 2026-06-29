package compaction

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

type CompactionErrorCode string

const (
	CompactionErrorAborted             CompactionErrorCode = "aborted"
	CompactionErrorSummarizationFailed CompactionErrorCode = "summarization_failed"
	CompactionErrorInvalidSession      CompactionErrorCode = "invalid_session"
	CompactionErrorUnknown             CompactionErrorCode = "unknown"
)

type CompactionError struct {
	Code    CompactionErrorCode `json:"code"`
	Message string              `json:"message"`
}

func (err CompactionError) Error() string { return err.Message }

type Settings struct {
	Enabled          bool
	ReserveTokens    int
	KeepRecentTokens int
}

type CompactionSettings = Settings

var DefaultCompactionSettings = Settings{Enabled: true, ReserveTokens: 16_384, KeepRecentTokens: 20_000}

var DEFAULT_COMPACTION_SETTINGS = DefaultCompactionSettings

func DefaultSettings() Settings {
	return DefaultCompactionSettings
}

func SummaryOutputTokens(model ai.Model, settings Settings) int {
	reserve := settings.ReserveTokens
	if reserve <= 0 {
		reserve = DefaultSettings().ReserveTokens
	}
	output := reserve
	if model.MaxTokens > 0 && model.MaxTokens < output {
		output = model.MaxTokens
	}
	if model.ContextWindow > 0 {
		quarter := model.ContextWindow / 4
		if quarter < 1 {
			quarter = 1
		}
		if output > quarter {
			output = quarter
		}
	}
	return output
}

func SummaryPromptBudgetTokens(model ai.Model, settings Settings) uint64 {
	if model.ContextWindow == 0 {
		return 64_000
	}
	window := uint64(model.ContextWindow)
	output := uint64(SummaryOutputTokens(model, settings))
	if window < output {
		return 0
	}
	return (window - output) * 4 / 5
}

func CalculateContextTokens(usage *ai.Usage) uint64 {
	if usage == nil {
		return 0
	}
	if usage.TotalTokenCount > 0 {
		return uint64(usage.TotalTokenCount)
	}
	return uint64(usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens)
}

func EstimateTextTokens(text string) uint64 {
	var ascii uint64
	var nonASCII uint64
	for _, ch := range text {
		if ch <= 127 {
			ascii++
		} else {
			nonASCII++
		}
	}
	return (ascii+3)/4 + nonASCII
}

func EstimateTokens(message agent.Message) uint64 {
	switch message.Kind {
	case agent.MessageKindLLM:
		if message.LLM == nil {
			return 0
		}
		return estimateAIMessageTokens(*message.LLM)
	case agent.MessageKindToolResult:
		if message.ToolResult == nil {
			return 0
		}
		return EstimateTextTokens(message.ToolResult.Name) + EstimateTextTokens(message.ToolResult.Content)
	case agent.MessageKindCustom:
		if message.Custom == nil {
			return 0
		}
		payload, _ := marshalJSONNoHTMLEscape(message.Custom.Payload)
		return EstimateTextTokens(message.Custom.Role) + EstimateTextTokens(string(payload))
	default:
		return 0
	}
}

func estimateAIMessageTokens(message ai.Message) uint64 {
	var total uint64
	for _, block := range message.Content {
		total += estimateContentBlockTokens(block)
	}
	if message.Role == ai.RoleTool {
		return total + EstimateTextTokens(toolResultName(message))
	}
	return total
}

func estimateContentBlockTokens(block ai.ContentBlock) uint64 {
	switch block.Type {
	case ai.ContentImage:
		return 768
	case ai.ContentThinking:
		return EstimateTextTokens(block.Thinking)
	default:
		return EstimateTextTokens(block.Text)
	}
}

type ContextUsageEstimate struct {
	Tokens         uint64
	UsageTokens    uint64
	TrailingTokens uint64
	LastUsageIndex *int
}

func EstimateContextTokens(messages []agent.Message) ContextUsageEstimate {
	lastIndex := -1
	var lastUsage *ai.Usage
	for index, message := range messages {
		if usage := assistantUsage(message); usage != nil {
			lastIndex = index
			lastUsage = usage
		}
	}
	if lastUsage == nil {
		var total uint64
		for _, message := range messages {
			total += EstimateTokens(message)
		}
		return ContextUsageEstimate{Tokens: total, TrailingTokens: total}
	}
	usageTokens := CalculateContextTokens(lastUsage)
	var trailing uint64
	for _, message := range messages[lastIndex+1:] {
		trailing += EstimateTokens(message)
	}
	idx := lastIndex
	return ContextUsageEstimate{Tokens: usageTokens + trailing, UsageTokens: usageTokens, TrailingTokens: trailing, LastUsageIndex: &idx}
}

func GetLastAssistantUsage(entries []session.Entry) (*ai.Usage, bool) {
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.Type() != session.EntryTypeMessage || entry.Message == nil {
			continue
		}
		if usage := assistantUsage(*entry.Message); usage != nil {
			copy := *usage
			return &copy, true
		}
	}
	return nil, false
}

func assistantUsage(message agent.Message) *ai.Usage {
	if message.Kind != agent.MessageKindLLM || message.LLM == nil || message.LLM.Role != ai.RoleAssistant || message.LLM.Usage == nil {
		return nil
	}
	if message.LLM.StopReason == ai.StopReasonError || message.LLM.StopReason == ai.StopReasonAborted {
		return nil
	}
	usage := message.LLM.Usage
	if usage.TotalTokens() == 0 {
		return nil
	}
	return usage
}

func ShouldCompact(contextTokens uint64, contextWindow int, settings Settings) bool {
	if !settings.Enabled || contextWindow <= 0 {
		return false
	}
	threshold := (uint64(contextWindow) * 4) / 5
	return contextTokens > threshold
}

func FindTurnStartIndex(entries []session.Entry, entryIndex int, startIndex int) int {
	if len(entries) == 0 {
		return 0
	}
	upper := entryIndex
	if upper >= len(entries) {
		upper = len(entries) - 1
	}
	for index := upper; index >= startIndex; index-- {
		entry := entries[index]
		if entry.Type() == session.EntryTypeMessage && entry.Message != nil && entry.Message.Kind == agent.MessageKindLLM && entry.Message.LLM != nil && entry.Message.LLM.Role == ai.RoleUser {
			return index
		}
	}
	return startIndex
}

type CutPointResult struct {
	CutIndex         int
	FirstKeptEntryID *string
}

func FindCutPoint(entries []session.Entry, settings Settings) CutPointResult {
	if len(entries) == 0 {
		return CutPointResult{}
	}
	var acc uint64
	target := len(entries)
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.Type() == session.EntryTypeMessage && entry.Message != nil {
			acc += EstimateTokens(*entry.Message)
		}
		if acc >= uint64(settings.KeepRecentTokens) {
			target = index
			break
		}
	}
	cut := FindTurnStartIndex(entries, target, 0)
	id := entries[cut].ID()
	return CutPointResult{CutIndex: cut, FirstKeptEntryID: &id}
}

type Preparation struct {
	Cut                CutPointResult
	EntriesToSummarize []session.Entry
	TokensBefore       uint64
}

type CompactionPreparation = Preparation

func PrepareCompaction(entries []session.Entry, settings Settings) Preparation {
	cut := FindCutPoint(entries, settings)
	prefix := append([]session.Entry(nil), entries[:cut.CutIndex]...)
	var tokens uint64
	for _, entry := range prefix {
		if entry.Type() == session.EntryTypeMessage && entry.Message != nil {
			tokens += EstimateTokens(*entry.Message)
		}
	}
	return Preparation{Cut: cut, EntriesToSummarize: prefix, TokensBefore: tokens}
}

const SummarizationSystemPrompt = "You are a context summarization assistant. Your task is to read a conversation between a user and an AI coding assistant, then produce a structured summary preserving the user's intent, the files and topics discussed, decisions made, and any work still in progress. Be concise but thorough; the assistant will rely on your summary instead of replaying the dropped messages."

const SUMMARIZATION_SYSTEM_PROMPT = SummarizationSystemPrompt

const summaryPromptFramingTokens uint64 = 512

func summaryPromptOverheadTokens() uint64 {
	return summaryPromptOverheadTokensFor(SummarizationSystemPrompt)
}

func summaryPromptOverheadTokensFor(systemPrompt string) uint64 {
	return summaryPromptFramingTokens + EstimateTextTokens(systemPrompt)
}

func summarizePromptEstimateTokens(messages []agent.Message) uint64 {
	var conversation uint64
	for _, message := range messages {
		conversation += EstimateTokens(message)
	}
	return summaryPromptOverheadTokens() + conversation
}

func trimMessagesForSummaryBudget(messages []agent.Message, budgetTokens uint64) []agent.Message {
	return trimMessagesForSummaryBudgetWithOverhead(messages, budgetTokens, summaryPromptOverheadTokens())
}

func trimMessagesForSummaryBudgetWithOverhead(messages []agent.Message, budgetTokens uint64, overheadTokens uint64) []agent.Message {
	if overheadTokens+estimateMessagesTokens(messages) <= budgetTokens {
		return append([]agent.Message(nil), messages...)
	}
	kept := make([]agent.Message, 0, len(messages))
	total := overheadTokens
	for index := len(messages) - 1; index >= 0; index-- {
		messageTokens := EstimateTokens(messages[index])
		if len(kept) > 0 && total+messageTokens > budgetTokens {
			break
		}
		kept = append(kept, messages[index])
		total += messageTokens
		if total >= budgetTokens {
			break
		}
	}
	for left, right := 0, len(kept)-1; left < right; left, right = left+1, right-1 {
		kept[left], kept[right] = kept[right], kept[left]
	}
	if omitted := len(messages) - len(kept); omitted > 0 {
		note := agent.NewUserMessage("[compaction note: omitted " + strconv.Itoa(omitted) + " older message(s) before summarization because the session exceeded the summarizer prompt budget]")
		kept = append([]agent.Message{note}, kept...)
	}
	return kept
}

func estimateMessagesTokens(messages []agent.Message) uint64 {
	var total uint64
	for _, message := range messages {
		total += EstimateTokens(message)
	}
	return total
}

func suffixStartForTokenBudget(text string, budgetTokens uint64) int {
	var ascii uint64
	var nonASCII uint64
	start := len(text)
	for index := len(text); index > 0; {
		r, size := utf8.DecodeLastRuneInString(text[:index])
		if r == utf8.RuneError && size == 0 {
			break
		}
		nextASCII := ascii
		nextNonASCII := nonASCII
		if r <= 127 {
			nextASCII++
		} else {
			nextNonASCII++
		}
		if (nextASCII+3)/4+nextNonASCII > budgetTokens {
			break
		}
		ascii = nextASCII
		nonASCII = nextNonASCII
		index -= size
		start = index
	}
	return start
}

func SerializeConversationForSummaryBudget(messages []agent.Message, budgetTokens uint64) string {
	return serializeConversationForSummaryBudgetWithOverhead(messages, budgetTokens, summaryPromptOverheadTokens())
}

func serializeConversationForSummaryBudgetWithOverhead(messages []agent.Message, budgetTokens uint64, overheadTokens uint64) string {
	messages = trimMessagesForSummaryBudgetWithOverhead(messages, budgetTokens, overheadTokens)
	conversation := SerializeConversation(messages)
	availableTokens := uint64(0)
	if budgetTokens > overheadTokens {
		availableTokens = budgetTokens - overheadTokens
	}
	if EstimateTextTokens(conversation) <= availableTokens {
		return conversation
	}
	note := "[compaction note: omitted older serialized content before summarization because the session exceeded the summarizer prompt budget]\n\n"
	noteTokens := EstimateTextTokens(note)
	if availableTokens <= noteTokens {
		limit := availableTokens * 4
		var builder strings.Builder
		for _, r := range note {
			if uint64(builder.Len()) >= limit {
				break
			}
			builder.WriteRune(r)
		}
		return builder.String()
	}
	start := suffixStartForTokenBudget(conversation, availableTokens-noteTokens)
	return note + conversation[start:]
}

func SerializeConversation(messages []agent.Message) string {
	var builder strings.Builder
	for _, message := range messages {
		switch message.Kind {
		case agent.MessageKindLLM:
			if message.LLM == nil {
				continue
			}
			serializeAIMessage(&builder, *message.LLM)
		case agent.MessageKindToolResult:
			if message.ToolResult != nil {
				builder.WriteString("TOOL_RESULT[")
				builder.WriteString(message.ToolResult.Name)
				builder.WriteString("]:\n")
				builder.WriteString(message.ToolResult.Content)
				builder.WriteString("\n\n")
			}
		case agent.MessageKindCustom:
			if message.Custom != nil {
				builder.WriteString(strings.ToUpper(message.Custom.Role))
				builder.WriteString(":\n")
				payload, _ := marshalJSONNoHTMLEscape(message.Custom.Payload)
				builder.Write(payload)
				builder.WriteString("\n\n")
			}
		}
	}
	return builder.String()
}

func serializeAIMessage(builder *strings.Builder, message ai.Message) {
	switch message.Role {
	case ai.RoleUser:
		builder.WriteString("USER:\n")
	case ai.RoleAssistant:
		builder.WriteString("ASSISTANT:\n")
	case ai.RoleTool:
		builder.WriteString("TOOL_RESULT[")
		builder.WriteString(toolResultName(message))
		builder.WriteString("]")
		builder.WriteString(":\n")
	default:
		builder.WriteString(strings.ToUpper(string(message.Role)))
		builder.WriteString(":\n")
	}
	for _, block := range message.Content {
		switch block.Type {
		case ai.ContentImage:
			builder.WriteString("<image>")
		case ai.ContentThinking:
			builder.WriteString("<thinking>")
			builder.WriteString(block.Thinking)
			builder.WriteString("</thinking>")
		case ai.ContentToolCall:
			if block.ToolCall != nil {
				serializeToolCall(builder, *block.ToolCall)
			}
		default:
			builder.WriteString(block.Text)
		}
	}
	for _, call := range message.ToolCalls {
		serializeToolCall(builder, call)
	}
	builder.WriteString("\n\n")
}

func serializeToolCall(builder *strings.Builder, call ai.ToolCall) {
	arguments, _ := marshalJSONNoHTMLEscape(call.Arguments)
	builder.WriteString("<tool_call name=\"")
	builder.WriteString(call.Name)
	builder.WriteString("\">")
	builder.Write(arguments)
	builder.WriteString("</tool_call>")
}

func marshalJSONNoHTMLEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func toolResultName(message ai.Message) string {
	if message.ToolName != "" {
		return message.ToolName
	}
	return message.Name
}
