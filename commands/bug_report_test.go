package commands

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/skills"
)

func TestBugReportCommandReturnsDiagnosticOutcome(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{
		SessionID:     "sess-123",
		Model:         &ai.Model{Provider: ai.Provider("openai"), ID: "gpt-test"},
		ThinkingLevel: ai.ThinkingHigh,
		ToolCount:     3,
		LogPath:       "/tmp/pie.log",
		Skills:        []skills.Skill{{Name: "review"}, {Name: "plan"}},
	}
	out := Dispatch(context.Background(), "/bug-report", registry, ctx)
	if out.Kind != OutcomeWriteBugReport || out.BugReport.SessionID != "sess-123" || out.BugReport.Model != "openai:gpt-test" || out.BugReport.Thinking != "high" || out.BugReport.ToolCount != 3 || out.BugReport.SkillCount != 2 || out.BugReport.LogPath != "/tmp/pie.log" {
		t.Fatalf("bug report mismatch: %#v", out)
	}
	if out.Path == "" || filepath.Base(filepath.Dir(out.Path)) != "bug-reports" || filepath.Ext(out.Path) != ".txt" {
		t.Fatalf("path mismatch: %#v", out)
	}
	if !strings.Contains(out.BugReport.CostSummary, "tokens:") || !strings.Contains(out.Message, "write bug report:") {
		t.Fatalf("summary mismatch: %#v", out)
	}
}

func TestBugReportCommandDefaultsAndIgnoresArgsLikeUpstream(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/bug-report", registry, Context{})
	if out.Kind != OutcomeWriteBugReport || out.BugReport.Model != "" || out.BugReport.Thinking != "?" || out.BugReport.LogPath != "" {
		t.Fatalf("defaults mismatch: %#v", out)
	}
	extra := Dispatch(context.Background(), "/bug-report extra", registry, Context{})
	if extra.Kind != OutcomeWriteBugReport || extra.BugReport.Thinking != "?" {
		t.Fatalf("bug-report should ignore args like upstream: %#v", extra)
	}
}
