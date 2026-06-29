package compaction

import (
	"context"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

type SupervisorOptions struct {
	ContextWindow int
	Settings      Settings
	Summarizer    Summarizer
	Now           func() string
	NewID         func() string
}

type Supervisor struct {
	options SupervisorOptions
}

type SupervisorResult struct {
	ShouldCompact bool
	Estimate      ContextUsageEstimate
	Compaction    Result
	Entry         *session.Entry
}

func NewSupervisor(options SupervisorOptions) *Supervisor {
	if options.Settings == (Settings{}) {
		options.Settings = DefaultSettings()
	}
	if options.Now == nil {
		options.Now = func() string { return time.Now().UTC().Format(time.RFC3339Nano) }
	}
	if options.NewID == nil {
		options.NewID = func() string { return "compaction-" + time.Now().UTC().Format("20060102150405.000000000") }
	}
	return &Supervisor{options: options}
}

func (supervisor *Supervisor) MaybeCompact(ctx context.Context, entries []session.Entry) (SupervisorResult, error) {
	messages := messagesFromEntries(entries)
	estimate := EstimateContextTokens(messages)
	if !ShouldCompact(estimate.Tokens, supervisor.options.ContextWindow, supervisor.options.Settings) && !HasContextOverflow(messages, supervisor.options.ContextWindow, supervisor.options.Settings) {
		return SupervisorResult{Estimate: estimate}, nil
	}
	compacted, err := Compact(ctx, entries, supervisor.options.Settings, supervisor.options.Summarizer)
	if err != nil {
		return SupervisorResult{ShouldCompact: true, Estimate: estimate}, err
	}
	entry := supervisor.compactionEntry(entries, compacted, len(entries))
	return SupervisorResult{ShouldCompact: true, Estimate: estimate, Compaction: compacted, Entry: &entry}, nil
}

func HasContextOverflow(messages []agent.Message, contextWindow int, settings Settings) bool {
	if !settings.Enabled {
		return false
	}
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Kind != agent.MessageKindLLM || message.LLM == nil || message.LLM.Role != ai.RoleAssistant {
			continue
		}
		assistant := ai.AssistantMessage{Usage: message.LLM.Usage, StopReason: message.LLM.StopReason, ErrorMessage: message.LLM.ErrorMessage}
		return ai.IsContextOverflow(assistant, contextWindow)
	}
	return false
}

func (supervisor *Supervisor) CompactAndAppend(ctx context.Context, storage session.Storage) (SupervisorResult, error) {
	entries, err := storage.GetEntries()
	if err != nil {
		return SupervisorResult{}, err
	}
	result, err := supervisor.MaybeCompact(ctx, entries)
	if err != nil || result.Entry == nil {
		return result, err
	}
	if err := storage.AppendEntry(*result.Entry); err != nil {
		return result, err
	}
	return result, nil
}

func (supervisor *Supervisor) CompactAndRewrite(ctx context.Context, storage session.Storage) (SupervisorResult, error) {
	rewriter, ok := storage.(session.Rewriter)
	if !ok {
		return SupervisorResult{}, session.Error{Code: session.ErrorStorageFailure, Message: "storage does not support entry replacement"}
	}
	entries, err := storage.GetEntries()
	if err != nil {
		return SupervisorResult{}, err
	}
	result, err := supervisor.MaybeCompact(ctx, entries)
	if err != nil || !result.ShouldCompact {
		return result, err
	}
	entry := supervisor.compactionEntry(entries, result.Compaction, result.Compaction.Cut.CutIndex)
	result.Entry = &entry
	rewritten := rewriteCompactedEntries(entries, entry, result.Compaction.Cut.CutIndex)
	if err := rewriter.ReplaceEntries(rewritten); err != nil {
		return result, err
	}
	return result, nil
}

func (supervisor *Supervisor) compactionEntry(entries []session.Entry, result Result, parentIndex int) session.Entry {
	firstKept := ""
	if result.Cut.FirstKeptEntryID != nil {
		firstKept = *result.Cut.FirstKeptEntryID
	}
	var parent *string
	if len(entries) > 0 && parentIndex > 0 {
		if parentIndex > len(entries) {
			parentIndex = len(entries)
		}
		id := entries[parentIndex-1].ID()
		parent = &id
	}
	return session.NewCompactionEntry(supervisor.options.NewID(), parent, supervisor.options.Now(), result.Summary, firstKept, result.TokensBefore, map[string]any{"messageCount": len(result.Messages), "cutIndex": result.Cut.CutIndex}, false)
}

func rewriteCompactedEntries(entries []session.Entry, compactionEntry session.Entry, cutIndex int) []session.Entry {
	if cutIndex < 0 {
		cutIndex = 0
	}
	if cutIndex > len(entries) {
		cutIndex = len(entries)
	}
	compactionEntry.ParentID = nil
	out := []session.Entry{compactionEntry}
	previousID := compactionEntry.ID()
	for _, entry := range entries[cutIndex:] {
		parentID := previousID
		entry.ParentID = &parentID
		out = append(out, entry)
		previousID = entry.ID()
	}
	return out
}
