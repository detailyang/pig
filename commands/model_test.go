package commands

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/config"
)

func TestModelCommandOpensPickerListsAndSetsModel(t *testing.T) {
	resetModelRegistry(t)
	t.Setenv("OPENAI_API_KEY", "openai-key")
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
	ai.RegisterBuiltinModel(ai.Model{Provider: ai.Provider("openai"), ID: "gpt-test", API: ai.ApiOpenAIResponses, Name: "GPT Test"})
	ai.RegisterBuiltinModel(ai.Model{Provider: ai.Provider("openai"), ID: "gpt test spaced", API: ai.ApiOpenAIResponses, Name: "GPT Spaced"})
	ai.RegisterBuiltinModel(ai.Model{Provider: ai.Provider("anthropic"), ID: "claude-test", API: ai.ApiAnthropicMessages, Name: "Claude Test"})
	registry := DefaultRegistry()

	picker := Dispatch(context.Background(), "/model", registry, Context{})
	if picker.Kind != OutcomeOpenModelPicker {
		t.Fatalf("picker mismatch: %#v", picker)
	}
	listed := Dispatch(context.Background(), "/model list openai extra", registry, Context{})
	if listed.Kind != OutcomeHandled || !strings.Contains(listed.Message, "Supported models for provider 'openai' (2):") || !strings.Contains(listed.Message, "    - gpt-test — GPT Test") || strings.Contains(listed.Message, "claude-test") {
		t.Fatalf("list mismatch: %#v", listed)
	}
	all := Dispatch(context.Background(), "/model list", registry, Context{})
	if all.Kind != OutcomeHandled || !strings.Contains(all.Message, "Supported providers/models: 2 providers, 3 models") || !strings.Contains(all.Message, "Custom models can be registered explicitly with config.LoadModelsFile/LoadModelsFiles; local models.json auto-loading is disabled.") || !strings.Contains(all.Message, "  anthropic (1)") || !strings.Contains(all.Message, "    - claude-test — Claude Test") || !strings.Contains(all.Message, "  openai (2)") {
		t.Fatalf("all list mismatch: %#v", all)
	}
	unknownProvider := Dispatch(context.Background(), "/model list missing", registry, Context{})
	if unknownProvider.Kind != OutcomeError || unknownProvider.Message != "unknown provider 'missing'. Known providers: anthropic(1), openai(2)" {
		t.Fatalf("unknown provider mismatch: %#v", unknownProvider)
	}
	set := Dispatch(context.Background(), "/model openai:gpt-test", registry, Context{})
	if set.Kind != OutcomeSetModel || set.Model.Provider != ai.Provider("openai") || set.Model.ID != "gpt-test" || set.Message != "switched to openai:gpt-test" {
		t.Fatalf("set mismatch: %#v", set)
	}
	slash := Dispatch(context.Background(), "/model anthropic/claude-test", registry, Context{})
	if slash.Kind != OutcomeSetModel || slash.Model.Provider != ai.Provider("anthropic") || slash.Model.ID != "claude-test" {
		t.Fatalf("slash spec mismatch: %#v", slash)
	}
	separateArgs := Dispatch(context.Background(), "/model openai gpt-test", registry, Context{})
	if separateArgs.Kind != OutcomeSetModel || separateArgs.Model.ID != "gpt-test" {
		t.Fatalf("separate args mismatch: %#v", separateArgs)
	}
	spacedID := Dispatch(context.Background(), "/model openai gpt test spaced", registry, Context{})
	if spacedID.Kind != OutcomeSetModel || spacedID.Model.ID != "gpt test spaced" {
		t.Fatalf("spaced model id mismatch: %#v", spacedID)
	}
}

func TestModelCommandCredentialHintMatchesUpstream(t *testing.T) {
	resetModelRegistry(t)
	baseDir := t.TempDir()
	t.Setenv("PIE_DIR", baseDir)
	t.Setenv("DEEPSEEK_API_KEY", "")
	ai.RegisterBuiltinModel(ai.Model{Provider: ai.Provider("deepseek"), ID: "deepseek-test", API: ai.ApiOpenAIResponses})
	registry := DefaultRegistry()

	missing := Dispatch(context.Background(), "/model deepseek:deepseek-test", registry, Context{})
	if missing.Kind != OutcomeSetModel || missing.Message != "selected deepseek:deepseek-test, but login is required: set DEEPSEEK_API_KEY or run /login deepseek" {
		t.Fatalf("missing credential mismatch: %#v", missing)
	}

	t.Setenv("DEEPSEEK_API_KEY", "env-key")
	withEnv := Dispatch(context.Background(), "/model deepseek:deepseek-test", registry, Context{})
	if withEnv.Kind != OutcomeSetModel || withEnv.Message != "switched to deepseek:deepseek-test" {
		t.Fatalf("env credential mismatch: %#v", withEnv)
	}

	t.Setenv("DEEPSEEK_API_KEY", "")
	store := config.AuthStore{}
	store.Set("deepseek", config.ProviderCredential{Type: config.CredentialAPIKey, Value: "stored-key"})
	if err := store.SaveTo(filepath.Join(baseDir, "auth.json")); err != nil {
		t.Fatal(err)
	}
	withStore := Dispatch(context.Background(), "/model deepseek:deepseek-test", registry, Context{})
	if withStore.Kind != OutcomeSetModel || withStore.Message != "switched to deepseek:deepseek-test" {
		t.Fatalf("stored credential mismatch: %#v", withStore)
	}
}

func TestModelCommandRejectsInvalidSpecsAndUnknownModels(t *testing.T) {
	resetModelRegistry(t)
	ai.RegisterBuiltinModel(ai.Model{Provider: ai.Provider("openai"), ID: "gpt-test", API: ai.ApiOpenAIResponses})
	registry := DefaultRegistry()

	badSpec := Dispatch(context.Background(), "/model openai", registry, Context{})
	if badSpec.Kind != OutcomeError || !strings.Contains(badSpec.Message, "expected provider:model-id") {
		t.Fatalf("bad spec mismatch: %#v", badSpec)
	}
	unknown := Dispatch(context.Background(), "/model openai:missing", registry, Context{})
	if unknown.Kind != OutcomeError || unknown.Message != "unknown model in catalog: openai:missing. Candidates: gpt-test" {
		t.Fatalf("unknown mismatch: %#v", unknown)
	}
	unknownProvider := Dispatch(context.Background(), "/model missing:gpt-test", registry, Context{})
	if unknownProvider.Kind != OutcomeError || unknownProvider.Message != "unknown provider 'missing'. Known providers: openai(1)" {
		t.Fatalf("unknown provider mismatch: %#v", unknownProvider)
	}
}

func TestParseModelSpecAcceptsUpstreamForms(t *testing.T) {
	provider, id, ok := ParseModelSpec("deepseek:deepseek-v4-pro")
	if !ok || provider != "deepseek" || id != "deepseek-v4-pro" {
		t.Fatalf("colon spec mismatch provider=%q id=%q ok=%v", provider, id, ok)
	}
	provider, id, ok = ParseModelSpec("deepseek/deepseek-v4-pro")
	if !ok || provider != "deepseek" || id != "deepseek-v4-pro" {
		t.Fatalf("slash spec mismatch provider=%q id=%q ok=%v", provider, id, ok)
	}
	provider, id, ok = ParseModelSpec("deepseek deepseek-v4-pro")
	if !ok || provider != "deepseek" || id != "deepseek-v4-pro" {
		t.Fatalf("space spec mismatch provider=%q id=%q ok=%v", provider, id, ok)
	}
	if provider, id, ok = ParseModelSpec("deepseek"); ok || provider != "" || id != "" {
		t.Fatalf("single token should fail provider=%q id=%q ok=%v", provider, id, ok)
	}
}

func resetModelRegistry(t *testing.T) {
	t.Helper()
	ai.ClearBuiltinModels()
	ai.ClearCustomModels()
	t.Cleanup(func() {
		ai.ClearBuiltinModels()
		ai.ClearCustomModels()
	})
}
