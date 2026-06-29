package harness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/compaction"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/templates"
)

func TestHarnessUpstreamErrorAliasesAreAvailable(t *testing.T) {
	if !errors.Is(EvaluatorErrorCancelled, ErrEvaluatorCancelled) {
		t.Fatalf("evaluator cancelled alias mismatch")
	}
	if EvaluatorErrorRun(errors.New("boom")).Error() != "evaluator agent failed: boom" {
		t.Fatalf("evaluator run alias mismatch")
	}
	if !errors.Is(ReloadSkillsErrorNotConfigured, ErrReloadSkillsNotConfigured) {
		t.Fatalf("reload skills alias mismatch")
	}
}

func TestHarnessExecutionEnvAliasesUseNativeEnv(t *testing.T) {
	env := NewNativeEnv(t.TempDir())
	var iface ExecutionEnv = env
	if iface.CWD() == "" {
		t.Fatal("native env should expose cwd through harness alias")
	}
	if err := iface.WriteFile(context.Background(), "nested/file.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	text, err := iface.ReadTextFile(context.Background(), "nested/file.txt")
	if err != nil || text != "hello" {
		t.Fatalf("native env alias read mismatch text=%q err=%v", text, err)
	}
	var _ ExecOptions = ExecOptions{CWD: iface.CWD()}
	var _ ExecOutput = ExecOutput{Stdout: "ok"}
	var _ FileInfo
	var _ FileError = FileError{Code: FileErrorNotFound, Message: "missing"}
	var _ ExecutionError = ExecutionError{Code: ExecutionErrorTimeout, Message: "timeout"}
}

func TestHarnessResourceAndErrorTypeAliasesMatchUpstream(t *testing.T) {
	var _ Skill = Skill{Name: "review", Source: SkillSourceProject}
	var _ SkillFrontmatter = SkillFrontmatter{Name: "review", HasName: true}
	var _ SkillDiagnostic = SkillDiagnostic{Code: SkillDiagnosticParseFailed, Message: "bad", Path: "SKILL.md"}
	var _ PromptTemplate = templates.PromptTemplate{Name: "daily"}
	var _ SessionError = session.SessionError{Code: SessionErrorNotFound, Message: "missing"}
	var _ CompactionError = compaction.CompactionError{Code: CompactionErrorInvalidSession, Message: "invalid"}

	if SkillSourceBuiltin.Label() != "builtin" || SkillSourceUser.Label() != "user" || SkillSourceProject.Label() != "project" {
		t.Fatalf("skill source labels mismatch")
	}
}

func TestHarnessCostAliasesMatchUpstream(t *testing.T) {
	tracker := NewCostTracker()
	var _ *CostTracker = tracker
	tracker.Record(&ai.Usage{InputTokens: 2, OutputTokens: 3, TotalTokenCount: 5, HasTotalTokens: true, Cost: &ai.UsageCost{Total: 0.25}})
	snapshot := tracker.Snapshot()
	var _ CostSnapshot = snapshot
	if costOneLineSummary(snapshot) != CostOneLineSummary(snapshot) || costFullBreakdown(snapshot) != CostFullBreakdown(snapshot) {
		t.Fatalf("cost summary aliases mismatch")
	}
	if !strings.Contains(CostOneLineSummary(snapshot), "total=5") || !strings.Contains(CostFullBreakdown(snapshot), "Cost (USD)") {
		t.Fatalf("cost summaries missing expected content")
	}
}

func TestHarnessPromptTemplateAliasesMatchUpstream(t *testing.T) {
	registry := NewPromptTemplateRegistry([]PromptTemplate{{Name: "daily", Content: "hello {{name}}"}})
	if listed := registry.List(); len(listed) != 1 || listed[0].Name != "daily" {
		t.Fatalf("template registry list mismatch: %#v", listed)
	}
	template, ok := registry.Get("daily")
	if !ok || template.Name != "daily" {
		t.Fatalf("template registry get mismatch: %#v ok=%v", template, ok)
	}
	if got := InterpolatePromptTemplate(template, map[string]any{"name": "world"}); got != "hello world" {
		t.Fatalf("template interpolation mismatch: %q", got)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "daily.md"), []byte("---\nname: daily\ndescription: Daily prompt\n---\nhello"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := LoadTemplates([]string{dir})
	var _ LoadTemplatesOutput = loaded
	if len(loaded.Templates) != 1 || loaded.Templates[0].Name != "daily" || len(loaded.Diagnostics) != 0 {
		t.Fatalf("LoadTemplates mismatch: %#v", loaded)
	}
}

func TestHarnessNotificationHookAliasesMatchUpstream(t *testing.T) {
	sink := make(chan Trigger, 1)
	var _ TriggerSink = sink
	err := HookError{Kind: HookErrorSinkClosed}
	if err.Error() != "sink closed" {
		t.Fatalf("hook error mismatch: %v", err)
	}
	status := PendingNotificationHookStatus()
	var _ NotificationHookStatus = status
	if status.State.Kind != HookStateDisconnected || status.State.Reason != "not yet started" {
		t.Fatalf("pending status mismatch: %#v", status)
	}
	var _ HookFuture = func(context.Context) error { return nil }
}
