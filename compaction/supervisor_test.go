package compaction

import (
	"context"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

func TestSupervisorNoopsBelowThreshold(t *testing.T) {
	supervisor := NewSupervisor(SupervisorOptions{ContextWindow: 1000, Settings: DefaultSettings(), Summarizer: SummarizerFunc(func(ctx context.Context, request SummarizationRequest) (string, error) {
		t.Fatal("summarizer should not run")
		return "", nil
	})})
	entries := []session.Entry{messageEntry("u1", nil, agent.NewUserMessage("short"))}
	result, err := supervisor.MaybeCompact(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}
	if result.ShouldCompact || result.Compaction.Compacted || result.Entry != nil {
		t.Fatalf("noop mismatch: %#v", result)
	}
}

func TestSupervisorCompactsAndBuildsSessionEntry(t *testing.T) {
	settings := DefaultSettings()
	settings.KeepRecentTokens = 1
	supervisor := NewSupervisor(SupervisorOptions{ContextWindow: 10, Settings: settings, Now: func() string { return "ts" }, NewID: func() string { return "compact-1" }, Summarizer: SummarizerFunc(func(ctx context.Context, request SummarizationRequest) (string, error) {
		return "summary", nil
	})})
	entries := []session.Entry{
		messageEntry("u1", nil, agent.NewUserMessage("old user text long enough")),
		messageEntry("a1", strPtr("u1"), assistantWithUsage("old assistant", &ai.Usage{InputTokens: 100, OutputTokens: 100})),
		messageEntry("u2", strPtr("a1"), agent.NewUserMessage("new user")),
	}
	result, err := supervisor.MaybeCompact(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ShouldCompact || !result.Compaction.Compacted || result.Entry == nil {
		t.Fatalf("expected compaction: %#v", result)
	}
	if result.Entry.EntryType != session.EntryTypeCompaction || result.Entry.EntryID != "compact-1" || result.Entry.Summary != "summary" || result.Entry.Timestamp != "ts" || result.Entry.TokensBefore == 0 {
		t.Fatalf("entry mismatch: %#v", result.Entry)
	}
	if result.Estimate.Tokens <= 8 {
		t.Fatalf("estimate should exceed threshold: %#v", result.Estimate)
	}
	if len(result.Compaction.Messages) == 0 || result.Compaction.Messages[0].LLM == nil {
		t.Fatalf("compacted messages mismatch: %#v", result.Compaction.Messages)
	}
}

func TestSupervisorCompactsOnContextOverflowError(t *testing.T) {
	settings := DefaultSettings()
	settings.KeepRecentTokens = 1
	supervisor := NewSupervisor(SupervisorOptions{ContextWindow: 100_000, Settings: settings, Now: func() string { return "ts" }, NewID: func() string { return "compact-1" }, Summarizer: SummarizerFunc(func(ctx context.Context, request SummarizationRequest) (string, error) {
		return "summary", nil
	})})
	overflow := agent.NewAssistantMessage("")
	overflow.LLM.StopReason = ai.StopReasonError
	overflow.LLM.ErrorMessage = "Your input exceeds the context window of this model"
	entries := []session.Entry{
		messageEntry("u1", nil, agent.NewUserMessage("old user text long enough")),
		messageEntry("a1", strPtr("u1"), agent.NewAssistantMessage("old assistant")),
		messageEntry("u2", strPtr("a1"), agent.NewUserMessage("new user")),
		messageEntry("a2", strPtr("u2"), overflow),
	}
	result, err := supervisor.MaybeCompact(context.Background(), entries)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ShouldCompact || !result.Compaction.Compacted || result.Entry == nil {
		t.Fatalf("expected overflow compaction: %#v", result)
	}
	if result.Estimate.Tokens > 80_000 {
		t.Fatalf("test should prove overflow bypasses threshold, got estimate %#v", result.Estimate)
	}
}

func TestSupervisorCompactAndAppendWritesCompactionEntry(t *testing.T) {
	settings := DefaultSettings()
	settings.KeepRecentTokens = 1
	storage := session.NewMemoryStorage(session.Metadata{ID: "s", CreatedAt: "ts0"})
	entries := []session.Entry{
		messageEntry("u1", nil, agent.NewUserMessage("old user text long enough")),
		messageEntry("a1", strPtr("u1"), assistantWithUsage("old assistant", &ai.Usage{InputTokens: 100, OutputTokens: 100})),
		messageEntry("u2", strPtr("a1"), agent.NewUserMessage("new user")),
	}
	for _, entry := range entries {
		if err := storage.AppendEntry(entry); err != nil {
			t.Fatal(err)
		}
	}
	supervisor := NewSupervisor(SupervisorOptions{ContextWindow: 10, Settings: settings, Now: func() string { return "ts" }, NewID: func() string { return "compact-1" }, Summarizer: SummarizerFunc(func(ctx context.Context, request SummarizationRequest) (string, error) {
		return "summary", nil
	})})
	result, err := supervisor.CompactAndAppend(context.Background(), storage)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ShouldCompact || result.Entry == nil || result.Entry.EntryID != "compact-1" {
		t.Fatalf("result mismatch: %#v", result)
	}
	stored, err := storage.GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 4 || stored[3].EntryType != session.EntryTypeCompaction || stored[3].Summary != "summary" {
		t.Fatalf("stored entries mismatch: %#v", stored)
	}
	if stored[3].ParentID == nil || *stored[3].ParentID != "u2" {
		t.Fatalf("append compaction parent should be previous leaf like upstream: %#v", stored[3])
	}
	leaf, err := storage.GetLeafID()
	if err != nil {
		t.Fatal(err)
	}
	if leaf == nil || *leaf != "compact-1" {
		t.Fatalf("leaf mismatch: %v", leaf)
	}
}

func TestSupervisorCompactAndRewritePrunesSummarizedHistory(t *testing.T) {
	settings := DefaultSettings()
	settings.KeepRecentTokens = 1
	storage := session.NewMemoryStorage(session.Metadata{ID: "s", CreatedAt: "ts0"})
	entries := []session.Entry{
		messageEntry("u1", nil, agent.NewUserMessage("old user text long enough")),
		messageEntry("a1", strPtr("u1"), assistantWithUsage("old assistant", &ai.Usage{InputTokens: 100, OutputTokens: 100})),
		messageEntry("u2", strPtr("a1"), agent.NewUserMessage("new user")),
	}
	for _, entry := range entries {
		if err := storage.AppendEntry(entry); err != nil {
			t.Fatal(err)
		}
	}
	supervisor := NewSupervisor(SupervisorOptions{ContextWindow: 10, Settings: settings, Now: func() string { return "ts" }, NewID: func() string { return "compact-1" }, Summarizer: SummarizerFunc(func(ctx context.Context, request SummarizationRequest) (string, error) {
		return "summary", nil
	})})
	result, err := supervisor.CompactAndRewrite(context.Background(), storage)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ShouldCompact || result.Entry == nil || result.Entry.EntryID != "compact-1" {
		t.Fatalf("result mismatch: %#v", result)
	}
	stored, err := storage.GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || stored[0].EntryType != session.EntryTypeCompaction || stored[0].Summary != "summary" || stored[1].ID() != "u2" {
		t.Fatalf("stored entries mismatch: %#v", stored)
	}
	if stored[0].ParentID != nil || stored[1].ParentID == nil || *stored[1].ParentID != "compact-1" {
		t.Fatalf("parent chain mismatch: %#v", stored)
	}
	leaf, err := storage.GetLeafID()
	if err != nil {
		t.Fatal(err)
	}
	if leaf == nil || *leaf != "u2" {
		t.Fatalf("leaf mismatch: %v", leaf)
	}
}
