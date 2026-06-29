package ai

import (
	"encoding/json"
	"fmt"
	"sort"
)

type CatalogRegistrationMode string

const (
	CatalogRegistrationBuiltin CatalogRegistrationMode = "builtin"
	CatalogRegistrationCustom  CatalogRegistrationMode = "custom"
)

type modelCatalogFile struct {
	Models []modelCatalogEntry `json:"models"`
}

type modelCatalogEntry struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	API              string             `json:"api"`
	Provider         string             `json:"provider"`
	BaseURL          string             `json:"baseUrl"`
	Reasoning        bool               `json:"reasoning"`
	Input            []string           `json:"input"`
	Cost             modelCatalogCost   `json:"cost"`
	ContextWindow    int                `json:"contextWindow"`
	MaxTokens        int                `json:"maxTokens"`
	Headers          map[string]string  `json:"headers"`
	ThinkingLevelMap map[string]*string `json:"thinkingLevelMap"`
	Compat           json.RawMessage    `json:"compat"`
}

type modelCatalogCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

func ParseModelCatalog(data []byte) ([]Model, error) {
	var wrapped modelCatalogFile
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	if len(wrapped.Models) > 0 {
		return modelCatalogEntriesToModels(wrapped.Models), nil
	}
	var providerMap map[string]map[string]modelCatalogEntry
	if err := json.Unmarshal(data, &providerMap); err != nil {
		return nil, err
	}
	providers := make([]string, 0, len(providerMap))
	for provider := range providerMap {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	var entries []modelCatalogEntry
	for _, provider := range providers {
		ids := make([]string, 0, len(providerMap[provider]))
		for id := range providerMap[provider] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			entry := providerMap[provider][id]
			if entry.ID == "" {
				entry.ID = id
			}
			if entry.Provider == "" {
				entry.Provider = provider
			}
			entries = append(entries, entry)
		}
	}
	return modelCatalogEntriesToModels(entries), nil
}

func RegisterModelCatalog(data []byte, mode CatalogRegistrationMode) ([]Model, error) {
	models, err := ParseModelCatalog(data)
	if err != nil {
		return nil, err
	}
	for _, model := range models {
		switch mode {
		case CatalogRegistrationBuiltin:
			RegisterBuiltinModel(model)
		case CatalogRegistrationCustom, "":
			RegisterCustomModel(model)
		default:
			return nil, fmt.Errorf("unknown catalog registration mode: %s", mode)
		}
	}
	return models, nil
}

func modelCatalogEntriesToModels(entries []modelCatalogEntry) []Model {
	models := make([]Model, 0, len(entries))
	for _, entry := range entries {
		models = append(models, entry.toModel())
	}
	return models
}

func (entry modelCatalogEntry) toModel() Model {
	input := make([]InputModality, 0, len(entry.Input))
	for _, item := range entry.Input {
		switch item {
		case "text":
			input = append(input, InputText)
		case "image":
			input = append(input, InputImage)
		}
	}
	return Model{
		ID:             entry.ID,
		Name:           entry.Name,
		API:            Api(entry.API),
		Provider:       Provider(entry.Provider),
		BaseURL:        entry.BaseURL,
		Reasoning:      entry.Reasoning,
		Input:          input,
		Cost:           &ModelCost{Input: entry.Cost.Input, Output: entry.Cost.Output, CacheRead: entry.Cost.CacheRead, CacheWrite: entry.Cost.CacheWrite},
		ContextWindow:  entry.ContextWindow,
		MaxTokens:      entry.MaxTokens,
		Headers:        entry.Headers,
		ThinkingLevels: modelCatalogThinkingLevelMap(entry.ThinkingLevelMap),
		Compat:         modelCatalogCompatMap(entry.Compat),
		CompatValue:    modelCatalogCompatValue(entry.Compat),
	}
}

func modelCatalogThinkingLevelMap(raw map[string]*string) map[string]*string {
	if raw == nil {
		return nil
	}
	levels := make(map[string]*string, len(raw))
	for level, value := range raw {
		if isModelThinkingLevel(level) {
			levels[level] = value
		}
	}
	return levels
}

func modelCatalogCompatMap(raw json.RawMessage) map[string]any {
	var compat map[string]any
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	if err := json.Unmarshal(raw, &compat); err != nil {
		return nil
	}
	return compat
}

func modelCatalogCompatValue(raw json.RawMessage) any {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}
