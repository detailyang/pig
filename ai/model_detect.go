package ai

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type modelDetectionCandidate struct {
	EnvVar   string
	Provider string
	ModelID  string
}

var modelDetectionCandidates = []modelDetectionCandidate{
	{EnvVar: "ANTHROPIC_API_KEY", Provider: "anthropic", ModelID: "claude-haiku-4-5"},
	{EnvVar: "OPENAI_API_KEY", Provider: "openai", ModelID: "gpt-4o-mini"},
	{EnvVar: "OPENROUTER_API_KEY", Provider: "openrouter", ModelID: "openai/gpt-4o-mini"},
	{EnvVar: "GROQ_API_KEY", Provider: "groq", ModelID: "llama-3.3-70b-versatile"},
	{EnvVar: "MISTRAL_API_KEY", Provider: "mistral", ModelID: "mistral-large-latest"},
	{EnvVar: "GEMINI_API_KEY", Provider: "google", ModelID: "gemini-2.0-flash"},
	{EnvVar: "GOOGLE_API_KEY", Provider: "google", ModelID: "gemini-2.0-flash"},
}

func AutoDetectModel(overrideProvider string, overrideModel string) (Model, error) {
	if overrideProvider != "" && overrideModel != "" {
		provider := Provider(overrideProvider)
		if model, ok := GetModel(provider, overrideModel); ok {
			return model, nil
		}
		return Model{}, fmt.Errorf("%s", ExplicitModelNotFoundMessage(overrideProvider, overrideModel))
	}
	for _, candidate := range modelDetectionCandidates {
		if strings.TrimSpace(os.Getenv(candidate.EnvVar)) == "" {
			continue
		}
		provider := Provider(candidate.Provider)
		if model, ok := GetModel(provider, candidate.ModelID); ok {
			return model, nil
		}
		if model, ok := FirstModelForProvider(candidate.Provider); ok {
			return model, nil
		}
	}
	envVars := make([]string, 0, len(modelDetectionCandidates))
	for _, candidate := range modelDetectionCandidates {
		envVars = append(envVars, candidate.EnvVar)
	}
	return Model{}, fmt.Errorf("no API key found. Set one of: %s env vars, or run `/login <provider> <key>` from inside pie.", strings.Join(envVars, ", "))
}

func CredentialLessDefault() Model {
	candidate := modelDetectionCandidates[0]
	if model, ok := GetModel(Provider(candidate.Provider), candidate.ModelID); ok {
		return model
	}
	models := ListModels()
	if len(models) == 0 {
		return Model{}
	}
	return models[0]
}

func ExplicitModelNotFoundMessage(provider string, id string) string {
	byProvider := map[string][]string{}
	for _, model := range ListModels() {
		byProvider[string(model.Provider)] = append(byProvider[string(model.Provider)], model.ID)
	}
	models, ok := byProvider[provider]
	if !ok {
		providers := make([]string, 0, len(byProvider))
		for provider, models := range byProvider {
			providers = append(providers, fmt.Sprintf("%s(%d)", provider, len(models)))
		}
		sort.Strings(providers)
		return fmt.Sprintf("model provider not found in catalog: provider=%s. Known providers: %s", provider, strings.Join(providers, ", "))
	}
	sort.Strings(models)
	candidates := models
	if len(candidates) > 12 {
		candidates = candidates[:12]
	}
	more := ""
	if len(models) > 12 {
		more = fmt.Sprintf("; run `/model list %s` inside pie for all %d models", provider, len(models))
	}
	return fmt.Sprintf("model not found in catalog: provider=%s id=%s. Candidates: %s%s", provider, id, strings.Join(candidates, ", "), more)
}

func FirstModelForProvider(provider string) (Model, bool) {
	wanted := Provider(provider)
	for _, model := range ListModels() {
		if model.Provider == wanted {
			return model, true
		}
	}
	return Model{}, false
}
