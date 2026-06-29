package modelpicker

import (
	"fmt"
	"sort"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/config"
)

var SupportedAPIs = map[ai.Api]bool{
	ai.ApiOpenAICompletions:    true,
	ai.ApiOpenAIResponses:      true,
	ai.ApiOpenAICodexResponses: true,
	ai.ApiAnthropicMessages:    true,
}

type ModelEntry struct {
	ID   string
	Name string
}

type ProviderGroup struct {
	Provider      string
	HasCredential bool
	Models        []ModelEntry
}

type LevelKind string

const (
	LevelProviders LevelKind = "providers"
	LevelModels    LevelKind = "models"
)

type Level struct {
	Kind          LevelKind
	ProviderIndex int
}

type PickerLevel = Level

type ActiveModel struct {
	Provider string
	ID       string
	Valid    bool
}

type State struct {
	Groups []ProviderGroup
	Level  Level
	Cursor int
	Active *ActiveModel
}

type ModelPickerState = State

type Row struct {
	Text     string
	Selected bool
}

func Catalog(hasCredential func(provider string) bool) []ProviderGroup {
	return CatalogFromModels(ai.ListModels(), hasCredential)
}

func CatalogWith(hasCredential func(provider string) bool) []ProviderGroup {
	return CatalogFromModels(ai.ListModels(), hasCredential)
}

func CatalogFromModels(models []ai.Model, hasCredential func(provider string) bool) []ProviderGroup {
	if hasCredential == nil {
		hasCredential = config.HasModelCredential
	}
	groups := map[string][]ModelEntry{}
	for _, model := range models {
		if !SupportedAPIs[model.API] {
			continue
		}
		provider := string(model.Provider)
		groups[provider] = append(groups[provider], ModelEntry{ID: model.ID, Name: model.Name})
	}
	providers := make([]string, 0, len(groups))
	for provider := range groups {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	out := make([]ProviderGroup, 0, len(providers))
	for _, provider := range providers {
		entries := groups[provider]
		sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
		out = append(out, ProviderGroup{Provider: provider, HasCredential: hasCredential(provider), Models: entries})
	}
	return out
}

func NewState(groups []ProviderGroup, active *ActiveModel) *State {
	copyGroups := append([]ProviderGroup(nil), groups...)
	return &State{Groups: copyGroups, Level: Level{Kind: LevelProviders}, Active: active}
}

func (state *State) Up() {
	if state.Cursor > 0 {
		state.Cursor--
	}
}

func (state *State) Down() {
	if state.Cursor+1 < state.len() {
		state.Cursor++
	}
}

func (state *State) Enter() string {
	if state.len() == 0 {
		return ""
	}
	switch state.Level.Kind {
	case LevelProviders:
		providerIndex := state.Cursor
		group := state.Groups[providerIndex]
		state.Cursor = 0
		if state.Active != nil && state.Active.Valid && state.Active.Provider == group.Provider {
			for index, model := range group.Models {
				if model.ID == state.Active.ID {
					state.Cursor = index
					break
				}
			}
		}
		state.Level = Level{Kind: LevelModels, ProviderIndex: providerIndex}
		return ""
	case LevelModels:
		group := state.Groups[state.Level.ProviderIndex]
		if len(group.Models) == 0 {
			return ""
		}
		return fmt.Sprintf("%s:%s", group.Provider, group.Models[state.Cursor].ID)
	default:
		return ""
	}
}

func (state *State) Back() bool {
	if state.Level.Kind == LevelModels {
		state.Cursor = state.Level.ProviderIndex
		state.Level = Level{Kind: LevelProviders}
		return false
	}
	return true
}

func (state *State) View(visible int) (string, []Row) {
	if visible < 1 {
		visible = 1
	}
	title, texts := state.rows()
	start := state.Cursor + 1 - visible
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > len(texts) {
		end = len(texts)
	}
	rows := make([]Row, 0, end-start)
	for index := start; index < end; index++ {
		rows = append(rows, Row{Text: texts[index], Selected: index == state.Cursor})
	}
	return title, rows
}

func (state *State) len() int {
	if state == nil {
		return 0
	}
	switch state.Level.Kind {
	case LevelModels:
		if state.Level.ProviderIndex < 0 || state.Level.ProviderIndex >= len(state.Groups) {
			return 0
		}
		return len(state.Groups[state.Level.ProviderIndex].Models)
	default:
		return len(state.Groups)
	}
}

func (state *State) rows() (string, []string) {
	if state.Level.Kind == LevelModels && state.Level.ProviderIndex >= 0 && state.Level.ProviderIndex < len(state.Groups) {
		group := state.Groups[state.Level.ProviderIndex]
		rows := make([]string, 0, len(group.Models))
		for _, model := range group.Models {
			text := model.ID
			if state.Active != nil && state.Active.Valid && state.Active.Provider == group.Provider && state.Active.ID == model.ID {
				text += " ●"
			}
			rows = append(rows, text)
		}
		return group.Provider + " models", rows
	}
	rows := make([]string, 0, len(state.Groups))
	for _, group := range state.Groups {
		key := ""
		if !group.HasCredential {
			key = " · no key"
		}
		rows = append(rows, fmt.Sprintf("%s (%d)%s", group.Provider, len(group.Models), key))
	}
	return "Select provider", rows
}
