package modelpicker

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/config"
)

func TestCatalogKeepsSupportedAPIFamiliesOnlyAndSorts(t *testing.T) {
	models := []ai.Model{
		{Provider: ai.Provider("openai"), ID: "z", Name: "Z", API: ai.ApiOpenAIResponses},
		{Provider: ai.Provider("bedrock"), ID: "claude", Name: "Claude", API: ai.ApiBedrockConverseStream},
		{Provider: ai.Provider("openai"), ID: "a", Name: "A", API: ai.ApiOpenAICompletions},
		{Provider: ai.Provider("anthropic"), ID: "b", Name: "B", API: ai.ApiAnthropicMessages},
	}
	groups := CatalogFromModels(models, func(provider string) bool { return provider == "anthropic" })
	if len(groups) != 2 || groups[0].Provider != "anthropic" || groups[1].Provider != "openai" {
		t.Fatalf("bad groups: %#v", groups)
	}
	if !groups[0].HasCredential || groups[1].HasCredential {
		t.Fatalf("credential flags mismatch: %#v", groups)
	}
	if got := []string{groups[1].Models[0].ID, groups[1].Models[1].ID}; !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("models not sorted: %#v", groups[1].Models)
	}
}

func TestCatalogDefaultCredentialDetectionMatchesUpstream(t *testing.T) {
	ai.ClearBuiltinModels()
	ai.ClearCustomModels()
	t.Cleanup(func() {
		ai.ClearBuiltinModels()
		ai.ClearCustomModels()
	})
	baseDir := t.TempDir()
	t.Setenv("PIE_DIR", baseDir)
	t.Setenv("OPENAI_API_KEY", "openai-key")
	t.Setenv("ANTHROPIC_API_KEY", "")
	ai.RegisterBuiltinModel(ai.Model{Provider: ai.Provider("openai"), ID: "gpt-test", API: ai.ApiOpenAIResponses})
	ai.RegisterBuiltinModel(ai.Model{Provider: ai.Provider("anthropic"), ID: "claude-test", API: ai.ApiAnthropicMessages})
	store := config.AuthStore{}
	store.Set("anthropic", config.ProviderCredential{Type: config.CredentialAPIKey, Value: "stored-key"})
	if err := store.SaveTo(filepath.Join(baseDir, "auth.json")); err != nil {
		t.Fatal(err)
	}
	groups := Catalog(nil)
	if len(groups) != 2 || groups[0].Provider != "anthropic" || groups[1].Provider != "openai" || !groups[0].HasCredential || !groups[1].HasCredential {
		t.Fatalf("default credential detection mismatch: %#v", groups)
	}
}

func TestCatalogWithMatchesUpstreamInjectedCredentialCore(t *testing.T) {
	ai.ClearBuiltinModels()
	ai.ClearCustomModels()
	t.Cleanup(func() {
		ai.ClearBuiltinModels()
		ai.ClearCustomModels()
	})
	ai.RegisterBuiltinModel(ai.Model{Provider: ai.Provider("openai"), ID: "gpt-test", API: ai.ApiOpenAIResponses})
	ai.RegisterBuiltinModel(ai.Model{Provider: ai.Provider("anthropic"), ID: "claude-test", API: ai.ApiAnthropicMessages})

	groups := CatalogWith(func(provider string) bool { return provider == "openai" })
	if len(groups) != 2 || groups[0].Provider != "anthropic" || groups[0].HasCredential || groups[1].Provider != "openai" || !groups[1].HasCredential {
		t.Fatalf("catalog_with alias mismatch: %#v", groups)
	}
}

func TestModelPickerUpstreamStateAliases(t *testing.T) {
	var state *ModelPickerState = NewState(twoGroups(), nil)
	var level PickerLevel = state.Level
	if level.Kind != LevelProviders {
		t.Fatalf("picker level alias mismatch: %#v", level)
	}
	state.Down()
	if state.Cursor != 1 {
		t.Fatalf("model picker state alias should expose navigation state: %#v", state)
	}
}

func TestPickerNavigatesDescendsSelectsAndBacksOut(t *testing.T) {
	picker := NewState(twoGroups(), nil)
	picker.Down()
	if picker.Cursor != 1 {
		t.Fatalf("cursor=%d", picker.Cursor)
	}
	if selected := picker.Enter(); selected != "" || picker.Level.Kind != LevelModels || picker.Level.ProviderIndex != 1 || picker.Cursor != 0 {
		t.Fatalf("enter provider mismatch selected=%q picker=%#v", selected, picker)
	}
	if selected := picker.Enter(); selected != "openai:gpt-5.2" {
		t.Fatalf("selected=%q", selected)
	}
	if close := picker.Back(); close || picker.Level.Kind != LevelProviders || picker.Cursor != 1 {
		t.Fatalf("back mismatch close=%v picker=%#v", close, picker)
	}
	if close := picker.Back(); !close {
		t.Fatal("provider-level back should close")
	}
}

func TestPickerStartsOnActiveModelAndMarksView(t *testing.T) {
	active := ActiveModel{Provider: "anthropic", ID: "claude-opus-4-8", Valid: true}
	picker := NewState(twoGroups(), &active)
	picker.Enter()
	if picker.Cursor != 1 {
		t.Fatalf("active cursor=%d", picker.Cursor)
	}
	title, rows := picker.View(10)
	if title != "anthropic models" || len(rows) != 2 || rows[1].Text != "claude-opus-4-8 ●" || !rows[1].Selected {
		t.Fatalf("bad view title=%q rows=%#v", title, rows)
	}
}

func TestPickerViewWindowsAndEmptyCatalog(t *testing.T) {
	models := make([]ModelEntry, 20)
	for i := range models {
		models[i] = ModelEntry{ID: "m-" + twoDigits(i), Name: "M"}
	}
	picker := NewState([]ProviderGroup{{Provider: "anthropic", HasCredential: true, Models: models}}, nil)
	picker.Enter()
	for i := 0; i < 15; i++ {
		picker.Down()
	}
	_, rows := picker.View(5)
	if len(rows) != 5 || !rowSelected(rows, "m-15") {
		t.Fatalf("bad window rows=%#v", rows)
	}
	empty := NewState(nil, nil)
	if selected := empty.Enter(); selected != "" || empty.Cursor != 0 {
		t.Fatalf("empty should be inert: selected=%q empty=%#v", selected, empty)
	}
	empty.Down()
	if empty.Cursor != 0 || !empty.Back() {
		t.Fatalf("empty navigation mismatch: %#v", empty)
	}
}

func twoGroups() []ProviderGroup {
	return []ProviderGroup{
		{Provider: "anthropic", HasCredential: true, Models: []ModelEntry{{ID: "claude-haiku-4-5", Name: "Haiku"}, {ID: "claude-opus-4-8", Name: "Opus"}}},
		{Provider: "openai", HasCredential: false, Models: []ModelEntry{{ID: "gpt-5.2", Name: "GPT"}}},
	}
}

func rowSelected(rows []Row, text string) bool {
	for _, row := range rows {
		if row.Selected && row.Text == text {
			return true
		}
	}
	return false
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return "1" + string(rune('0'+value-10))
}
