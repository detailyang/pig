package config

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/ai"
)

type DetectOptions struct {
	Provider string
	ModelID  string
	BaseURL  string
	Headers  map[string]string
	Auth     AuthStore
}

type LoadedLocalModels struct {
	Models []ai.Model
}

type detectCandidate struct {
	EnvVar   string
	Provider string
	ModelID  string
}

var detectCandidates = []detectCandidate{
	{"ANTHROPIC_API_KEY", "anthropic", "claude-haiku-4-5"},
	{"OPENAI_API_KEY", "openai", "gpt-4o-mini"},
	{"OPENROUTER_API_KEY", "openrouter", "openai/gpt-4o-mini"},
	{"GROQ_API_KEY", "groq", "llama-3.3-70b-versatile"},
	{"MISTRAL_API_KEY", "mistral", "mistral-large-latest"},
	{"GEMINI_API_KEY", "google", "gemini-2.0-flash"},
	{"GOOGLE_API_KEY", "google", "gemini-2.0-flash"},
}

func AutoDetectModel(options DetectOptions) (ai.Model, error) {
	if options.Provider != "" && options.ModelID != "" {
		model, ok := ai.GetModel(ai.Provider(options.Provider), options.ModelID)
		if !ok {
			return ai.Model{}, fmt.Errorf("%s", explicitModelNotFoundMessage(options.Provider, options.ModelID))
		}
		return applyModelOptions(model, options), nil
	}
	store := options.Auth
	if store.Providers == nil {
		loaded, err := LoadDefaultAuthStore()
		if err == nil {
			store = loaded
		}
	}
	for _, candidate := range detectCandidates {
		envSet := strings.TrimSpace(os.Getenv(candidate.EnvVar)) != ""
		_, stored := store.Get(candidate.Provider)
		if !envSet && !stored {
			continue
		}
		if model, ok := ai.GetModel(ai.Provider(candidate.Provider), candidate.ModelID); ok {
			return applyModelOptions(model, options), nil
		}
		if model, ok := firstModelForProvider(candidate.Provider); ok {
			return applyModelOptions(model, options), nil
		}
	}
	return ai.Model{}, fmt.Errorf("no API key found. Set one of: %s env vars, or run `/login <provider> <key>` from inside pie.", candidateEnvList())
}

func CredentialLessDefault() (ai.Model, bool) {
	candidate := detectCandidates[0]
	if model, ok := ai.GetModel(ai.Provider(candidate.Provider), candidate.ModelID); ok {
		return model, true
	}
	models := ai.ListModels()
	if len(models) == 0 {
		return ai.Model{}, false
	}
	return models[0], true
}

func LoadModelsFile(path string) ([]ai.Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("read %s: invalid UTF-8", path)
	}
	return ai.RegisterModelCatalog(data, ai.CatalogRegistrationCustom)
}

func LoadModelsFiles(paths ...string) ([]ai.Model, error) {
	var out []ai.Model
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		models, err := LoadModelsFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out = append(out, models...)
	}
	return out, nil
}

func LoadLocalModelsAll(cwd string, cliBaseURL string) (LoadedLocalModels, error) {
	return LoadedLocalModels{}, nil
}

func LoadAll(cwd string, cliBaseURL string) (LoadedLocalModels, error) {
	return LoadLocalModelsAll(cwd, cliBaseURL)
}

func LoadLocalModelsFromPaths(paths []string, cliBaseURL string) (LoadedLocalModels, error) {
	return LoadedLocalModels{}, nil
}

func LoadAllFromPathsWithBaseURL(paths []string, cliBaseURL string) (LoadedLocalModels, error) {
	return LoadLocalModelsFromPaths(paths, cliBaseURL)
}

func applyModelOptions(model ai.Model, options DetectOptions) ai.Model {
	if options.BaseURL != "" {
		model.BaseURL = strings.TrimRight(options.BaseURL, "/")
	}
	if options.Headers != nil {
		headers := make(map[string]string, len(model.Headers)+len(options.Headers))
		for key, value := range model.Headers {
			headers[key] = value
		}
		for key, value := range options.Headers {
			headers[key] = value
		}
		model.Headers = headers
	}
	return model
}

func firstModelForProvider(provider string) (ai.Model, bool) {
	for _, model := range ai.ListModels() {
		if model.Provider == ai.Provider(provider) {
			return model, true
		}
	}
	return ai.Model{}, false
}

func explicitModelNotFoundMessage(provider, id string) string {
	byProvider := map[string][]string{}
	for _, model := range ai.ListModels() {
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
	candidateLimit := min(len(models), 12)
	more := ""
	if len(models) > 12 {
		more = fmt.Sprintf("; run `/model list %s` inside pie for all %d models", provider, len(models))
	}
	return fmt.Sprintf("model not found in catalog: provider=%s id=%s. Candidates: %s%s", provider, id, strings.Join(models[:candidateLimit], ", "), more)
}

func candidateEnvList() string {
	parts := make([]string, 0, len(detectCandidates))
	for _, candidate := range detectCandidates {
		parts = append(parts, candidate.EnvVar)
	}
	return strings.Join(parts, ", ")
}
