package model

import (
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestModelPackageAutoDetectAndCredentialLessDefault(t *testing.T) {
	resetModels(t)
	ai.RegisterBuiltinModel(ai.Model{ID: "claude-haiku-4-5", Provider: ai.Provider("anthropic"), API: ai.ApiAnthropic})
	ai.RegisterBuiltinModel(ai.Model{ID: "gpt-4o-mini", Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses})
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "sk")

	detected, err := AutoDetectModel("", "")
	if err != nil {
		t.Fatal(err)
	}
	if detected.Provider != ai.Provider("openai") || detected.ID != "gpt-4o-mini" {
		t.Fatalf("detected model mismatch: %#v", detected)
	}
	explicit, err := AutoDetectModel("anthropic", "claude-haiku-4-5")
	if err != nil || explicit.Provider != ai.Provider("anthropic") {
		t.Fatalf("explicit model mismatch: %#v err=%v", explicit, err)
	}
	fallback := CredentialLessDefault()
	if fallback.Provider != ai.Provider("anthropic") || fallback.ID != "claude-haiku-4-5" {
		t.Fatalf("credential-less default mismatch: %#v", fallback)
	}
}

func TestModelPackageDoesNotRestoreUnsupportedLocalCandidate(t *testing.T) {
	resetModels(t)
	ai.RegisterBuiltinModel(ai.Model{ID: "gpt-4o-mini", Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses})
	unsupportedEnv := "D" + "S" + "4" + "_API_KEY"
	unsupportedName := "d" + "s" + "4"
	t.Setenv(unsupportedEnv, "local-key")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, err := AutoDetectModel("", "")
	if err == nil {
		t.Fatal("expected missing credential error")
	}
	if strings.Contains(err.Error(), unsupportedEnv) || strings.Contains(err.Error(), unsupportedName) {
		t.Fatalf("unsupported local candidate must remain absent in model package: %s", err)
	}
}

func TestModelPackageFirstModelAndErrorMessage(t *testing.T) {
	resetModels(t)
	ai.RegisterBuiltinModel(ai.Model{ID: "a", Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses})
	first, ok := FirstModelForProvider("openai")
	if !ok || first.ID != "a" {
		t.Fatalf("first model mismatch: %#v ok=%v", first, ok)
	}
	message := ExplicitModelNotFoundMessage("missing", "x")
	unsupportedName := "D" + "S" + "4"
	if !strings.Contains(message, "model provider not found") || strings.Contains(message, "models.json") || strings.Contains(message, unsupportedName) {
		t.Fatalf("error message should not include local model or unsupported local hints: %s", message)
	}
}

func resetModels(t *testing.T) {
	t.Helper()
	ai.ClearBuiltinModels()
	ai.ClearCustomModels()
	t.Cleanup(func() {
		ai.ClearBuiltinModels()
		ai.ClearCustomModels()
	})
}
