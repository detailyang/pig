package commands

import (
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/config"
	"github.com/detailyang/pig/harness"
	"github.com/detailyang/pig/session"
	"github.com/detailyang/pig/skills"
)

func TestPrintHelpReturnsGeneralHelpText(t *testing.T) {
	text := PrintHelp(DefaultRegistry(), "")
	for _, want := range []string{"/help", "/model", "Anything else is sent as a prompt"} {
		if !strings.Contains(text, want) {
			t.Fatalf("PrintHelp missing %q in:\n%s", want, text)
		}
	}
}

func TestPrintHelpWithSkillsIncludesSkillShortcuts(t *testing.T) {
	text := PrintHelpWithSkills(DefaultRegistry(), "", []skills.Skill{{Name: "review", Description: "review code", Source: skills.SourceProject}})
	if !strings.Contains(text, "Skill commands:") || !strings.Contains(text, "/review [prompt]") || !strings.Contains(text, "review code") {
		t.Fatalf("PrintHelpWithSkills missing skill shortcut:\n%s", text)
	}
}

func TestSaveAPIKeyStoresCredential(t *testing.T) {
	t.Setenv("PIE_DIR", t.TempDir())
	path, err := SaveApiKey("openai", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if path != config.AuthPath() {
		t.Fatalf("path mismatch: got %q want %q", path, config.AuthPath())
	}
	store, err := config.LoadDefaultAuthStore()
	if err != nil {
		t.Fatal(err)
	}
	credential, ok := store.Get("openai")
	if !ok || credential.Type != config.CredentialAPIKey || credential.Value != "test-key" {
		t.Fatalf("credential mismatch: ok=%v credential=%#v", ok, credential)
	}
}

func TestModelCredentialHintUsesConfigAuthStore(t *testing.T) {
	t.Setenv("PIE_DIR", t.TempDir())
	if hint := ModelCredentialHint("deepseek"); !strings.Contains(hint, "DEEPSEEK_API_KEY") || !strings.Contains(hint, "/login deepseek") {
		t.Fatalf("credential hint mismatch: %q", hint)
	}
	if _, err := SaveApiKey("deepseek", "stored-key"); err != nil {
		t.Fatal(err)
	}
	if hint := ModelCredentialHint("deepseek"); hint != "" {
		t.Fatalf("credential hint should be empty after stored key, got %q", hint)
	}
}

func TestCommandOutcomeUpstreamVariantAliases(t *testing.T) {
	if CommandOutcomeHandled != OutcomeHandled || CommandOutcomeQuit != OutcomeQuit || CommandOutcomeClearScreen != OutcomeClearScreen || CommandOutcomeAttachSkill != OutcomeAttachSkill || CommandOutcomeRunAgentPrompt != OutcomeRunPrompt || CommandOutcomeRunPromptTemplate != OutcomeRunPromptTemplate || CommandOutcomeRunCompaction != OutcomeRunCompaction || CommandOutcomeLoginSecret != OutcomeLoginSecret || CommandOutcomeOpenModelPicker != OutcomeOpenModelPicker || CommandOutcomeSessionImportActivation != OutcomeSessionImportActivation {
		t.Fatalf("command outcome aliases mismatch")
	}
	if RunAgentPrompt("hello", "run").Kind != CommandOutcomeRunAgentPrompt {
		t.Fatalf("run agent prompt alias constructor mismatch")
	}
	activation := SessionImportActivation("session.piesession", []string{"trigger-1"}, []string{"cron-1"})
	if activation.Kind != CommandOutcomeSessionImportActivation || activation.Path != "session.piesession" || len(activation.TriggerIDs) != 1 || len(activation.CronIDs) != 1 {
		t.Fatalf("session import activation mismatch: %#v", activation)
	}
}

func TestCommandCtxExposesUpstreamHarnessField(t *testing.T) {
	h := harness.NewAgentHarness(harness.NewAgentHarnessOptions(ai.Model{ID: "gpt-test"}, session.NewSession(session.NewMemoryStorage(session.Metadata{ID: "sess-1", CreatedAt: "now"}))))
	ctx := CommandCtx{Harness: h, SessionID: "sess-1"}
	if ctx.Harness != h {
		t.Fatal("command context should expose upstream harness field")
	}
}
