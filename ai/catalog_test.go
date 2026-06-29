package ai

import (
	"encoding/json"
	"testing"
)

func TestParseModelCatalogSupportsProviderMap(t *testing.T) {
	data := []byte(`{"anthropic":{"claude-test":{"id":"claude-test","name":"Claude Test","api":"anthropic-messages","provider":"anthropic","baseUrl":"https://api.anthropic.com","reasoning":true,"input":["text","image"],"cost":{"input":3,"output":15,"cacheRead":0.3,"cacheWrite":3.75},"contextWindow":200000,"maxTokens":8192,"thinkingLevelMap":{"high":"enabled","off":null},"headers":{"x-provider":"anthropic"}}},"openai":{"gpt-test":{"id":"gpt-test","name":"GPT Test","api":"openai-responses","provider":"openai","input":["text"],"cost":{"input":1,"output":2},"contextWindow":128000,"maxTokens":4096,"compat":{"requiresReasoningContentOnAssistantMessages":true}}}}`)
	models, err := ParseModelCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %#v", models)
	}
	byID := map[string]Model{}
	for _, model := range models {
		byID[model.ID] = model
	}
	claude := byID["claude-test"]
	if claude.Provider != Provider("anthropic") || claude.API != ApiAnthropic || !claude.Reasoning || len(claude.Input) != 2 || claude.Cost.CacheWrite != 3.75 || claude.ThinkingLevels["high"] == nil || claude.Headers["x-provider"] != "anthropic" {
		t.Fatalf("claude mismatch: %#v", claude)
	}
	if byID["gpt-test"].ContextWindow != 128000 || byID["gpt-test"].Compat["requiresReasoningContentOnAssistantMessages"] != true {
		t.Fatalf("gpt mismatch: %#v", byID["gpt-test"])
	}
}

func TestParseModelCatalogPreservesArbitraryCompatJSONLikeUpstream(t *testing.T) {
	data := []byte(`{"models":[{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":["text"],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":1,"maxTokens":1,"compat":["flag",{"nested":true}]}]}`)
	models, err := ParseModelCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("model count mismatch: %#v", models)
	}
	compat, ok := models[0].CompatValue.([]any)
	if !ok || len(compat) != 2 || compat[0] != "flag" {
		t.Fatalf("catalog compat should preserve arbitrary upstream JSON value: %#v", models[0].CompatValue)
	}
}

func TestParseModelCatalogDropsUnknownThinkingLevelMapKeysLikeUpstream(t *testing.T) {
	data := []byte(`{"models":[{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":["text"],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":1,"maxTokens":1,"thinkingLevelMap":{"high":"enabled","turbo":"ignored"}}]}`)
	models, err := ParseModelCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("model count mismatch: %#v", models)
	}
	if _, ok := models[0].ThinkingLevels["turbo"]; ok {
		t.Fatalf("unknown thinkingLevelMap key should be dropped like upstream: %#v", models[0].ThinkingLevels)
	}
	if models[0].ThinkingLevels[string(ModelThinkingHigh)] == nil || *models[0].ThinkingLevels[string(ModelThinkingHigh)] != "enabled" {
		t.Fatalf("known thinkingLevelMap key should remain: %#v", models[0].ThinkingLevels)
	}
	if _, err := json.Marshal(models[0]); err != nil {
		t.Fatalf("model with parsed thinkingLevelMap should marshal after dropping unknown keys: %v", err)
	}
}

func TestParseModelCatalogDropsUnknownInputModalitiesLikeUpstream(t *testing.T) {
	data := []byte(`{"models":[{"id":"m","name":"Model","api":"openai-responses","provider":"openai","baseUrl":"","reasoning":false,"input":["text","audio","image"],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":1,"maxTokens":1}]}`)
	models, err := ParseModelCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || len(models[0].Input) != 2 || models[0].Input[0] != InputText || models[0].Input[1] != InputImage {
		t.Fatalf("unknown input modalities should be dropped like upstream: %#v", models)
	}
}

func TestRegisterModelCatalogAsBuiltin(t *testing.T) {
	ClearBuiltinModels()
	ClearCustomModels()
	t.Cleanup(func() { ClearBuiltinModels(); ClearCustomModels() })
	data := []byte(`{"models":[{"id":"local","name":"Local","api":"openai-responses","provider":"local","input":["text"],"cost":{"input":0,"output":0},"contextWindow":100,"maxTokens":50}]}`)
	models, err := RegisterModelCatalog(data, CatalogRegistrationBuiltin)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("models mismatch: %#v", models)
	}
	if _, ok := GetModel(Provider("local"), "local"); !ok {
		t.Fatal("builtin model should be registered")
	}
}
