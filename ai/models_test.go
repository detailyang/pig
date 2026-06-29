package ai

import "testing"

func TestListAPIsOnlyIncludesBuiltins(t *testing.T) {
	ClearBuiltinModels()
	ClearCustomModels()
	t.Cleanup(func() { ClearBuiltinModels(); ClearCustomModels() })
	RegisterBuiltinModel(Model{ID: "builtin", Provider: Provider("builtin-provider"), API: Api("builtin-api")})
	RegisterCustomModel(Model{ID: "custom", Provider: Provider("custom-provider"), API: Api("custom-api")})

	apis := ListAPIs()
	if len(apis) != 1 || apis[0] != Api("builtin-api") {
		t.Fatalf("expected only builtin APIs, got %#v", apis)
	}
	aliasAPIs := ListApis()
	if len(aliasAPIs) != 1 || aliasAPIs[0] != Api("builtin-api") {
		t.Fatalf("upstream ListApis alias mismatch: %#v", aliasAPIs)
	}
}

func TestCustomModelOverridesBuiltinLookupButBothRemainListed(t *testing.T) {
	ClearBuiltinModels()
	ClearCustomModels()
	t.Cleanup(func() { ClearBuiltinModels(); ClearCustomModels() })
	RegisterBuiltinModel(Model{ID: "same", Name: "builtin", Provider: Provider("p"), API: Api("builtin-api")})
	RegisterCustomModel(Model{ID: "same", Name: "custom", Provider: Provider("p"), API: Api("custom-api")})

	model, ok := GetModel(Provider("p"), "same")
	if !ok || model.Name != "custom" {
		t.Fatalf("custom model should override lookup: %#v ok=%v", model, ok)
	}
	models := ListModels()
	if len(models) != 2 {
		t.Fatalf("list should include builtin plus custom registry entries, got %#v", models)
	}
}

func TestListModelsReturnsBuiltinsBeforeCustomLikeUpstream(t *testing.T) {
	ClearBuiltinModels()
	ClearCustomModels()
	t.Cleanup(func() { ClearBuiltinModels(); ClearCustomModels() })
	for index := 0; index < 26; index++ {
		RegisterBuiltinModel(Model{ID: string(rune('a' + index)), Provider: Provider("p"), API: Api("builtin-api")})
	}
	RegisterBuiltinModel(Model{ID: "b", Name: "updated", Provider: Provider("p"), API: Api("builtin-api")})
	RegisterCustomModel(Model{ID: "custom", Provider: Provider("p"), API: Api("custom-api")})
	RegisterCustomModel(Model{ID: "custom", Name: "updated", Provider: Provider("p"), API: Api("custom-api")})

	models := ListModels()
	if len(models) != 27 || models[0].ID != "a" || models[1].ID != "b" || models[1].Name != "updated" || models[25].ID != "z" || models[26].ID != "custom" || models[26].Name != "updated" {
		t.Fatalf("models should list builtins before custom models like upstream, got %#v", models)
	}
}

func TestCredentialLessDefaultAndFirstModelForProviderMatchUpstreamModelHelpers(t *testing.T) {
	ClearBuiltinModels()
	ClearCustomModels()
	t.Cleanup(func() { ClearBuiltinModels(); ClearCustomModels() })
	RegisterBuiltinModel(Model{ID: "fallback", Provider: Provider("fallback-provider"), API: ApiFaux})
	RegisterBuiltinModel(Model{ID: "claude-haiku-4-5", Provider: Provider("anthropic"), API: ApiAnthropicMessages})
	RegisterBuiltinModel(Model{ID: "other", Provider: Provider("anthropic"), API: ApiAnthropicMessages})

	model := CredentialLessDefault()
	if model.Provider != Provider("anthropic") || model.ID != "claude-haiku-4-5" {
		t.Fatalf("credential-less default mismatch: %#v", model)
	}
	first, ok := FirstModelForProvider("anthropic")
	if !ok || first.ID != "claude-haiku-4-5" {
		t.Fatalf("first model mismatch: %#v ok=%v", first, ok)
	}
}

func TestAutoDetectModelExplicitOverrideAndEnvWithoutDS4(t *testing.T) {
	ClearBuiltinModels()
	ClearCustomModels()
	t.Cleanup(func() { ClearBuiltinModels(); ClearCustomModels() })
	RegisterBuiltinModel(Model{ID: "gpt-4o-mini", Provider: Provider("openai"), API: ApiOpenAIResponses})
	RegisterBuiltinModel(Model{ID: "custom", Provider: Provider("local"), API: ApiFaux})
	t.Setenv("OPENAI_API_KEY", "key")

	explicit, err := AutoDetectModel("local", "custom")
	if err != nil || explicit.Provider != Provider("local") || explicit.ID != "custom" {
		t.Fatalf("explicit model mismatch: %#v err=%v", explicit, err)
	}
	detected, err := AutoDetectModel("", "")
	if err != nil || detected.Provider != Provider("openai") || detected.ID != "gpt-4o-mini" {
		t.Fatalf("env detected model mismatch: %#v err=%v", detected, err)
	}
}
