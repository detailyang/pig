package ai

import "testing"

func TestVendoredGeneratedCatalogParsesAndRegisters(t *testing.T) {
	if len(BUILTIN_MODELS) != len(vendoredGeneratedModelCatalog) {
		t.Fatalf("builtin models alias length mismatch: %d vs %d", len(BUILTIN_MODELS), len(vendoredGeneratedModelCatalog))
	}
	if len(VendoredGeneratedModelCatalog()) < 100_000 {
		t.Fatalf("expected full generated catalog payload, got %d bytes", len(VendoredGeneratedModelCatalog()))
	}
	models, err := ParseVendoredGeneratedModelCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) < 100 {
		t.Fatalf("expected generated catalog models, got %d", len(models))
	}
	if !hasGeneratedCatalogModel(models, Provider("openai"), "gpt-4o") {
		t.Fatalf("expected openai/gpt-4o in generated catalog")
	}
	if !hasGeneratedCatalogModelAPI(models, Provider("anthropic"), ApiAnthropic) {
		t.Fatalf("expected generated anthropic models to use ApiAnthropic")
	}
	ClearBuiltinModels()
	t.Cleanup(ClearBuiltinModels)
	registered, err := RegisterVendoredGeneratedModelCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(registered) != len(models) {
		t.Fatalf("registered count mismatch: %d vs %d", len(registered), len(models))
	}
	if _, ok := GetModel(Provider("openai"), "gpt-4o"); !ok {
		t.Fatal("registered generated model not found")
	}
}

func TestVendoredGeneratedCatalogIsRegisteredByDefaultLikeUpstream(t *testing.T) {
	resetBuiltinModelAutoLoadForTest()
	t.Cleanup(ClearBuiltinModels)

	model, ok := GetModel(Provider("openai"), "gpt-4o")
	if !ok || model.API != ApiOpenAIResponses {
		t.Fatalf("expected generated builtin model by default, got %#v ok=%v", model, ok)
	}
}

func resetBuiltinModelAutoLoadForTest() {
	modelRegistry.Lock()
	defer modelRegistry.Unlock()
	modelRegistry.builtins = map[string]Model{}
	modelRegistry.builtinsLoaded = false
	modelRegistry.disableBuiltinLoad = false
}

func TestVendoredGeneratedCatalogAPIsHaveBuiltinProviders(t *testing.T) {
	models, err := ParseVendoredGeneratedModelCatalog()
	if err != nil {
		t.Fatal(err)
	}
	ClearAPIProviders()
	t.Cleanup(ClearAPIProviders)
	RegisterBuiltinProviders()
	seen := map[Api]bool{}
	for _, model := range models {
		seen[model.API] = true
	}
	for api := range seen {
		if _, ok := GetAPIProvider(api); !ok {
			t.Fatalf("generated catalog api %q has no builtin provider", api)
		}
	}
}

func hasGeneratedCatalogModel(models []Model, provider Provider, id string) bool {
	for _, model := range models {
		if model.Provider == provider && model.ID == id {
			return true
		}
	}
	return false
}

func hasGeneratedCatalogModelAPI(models []Model, provider Provider, api Api) bool {
	for _, model := range models {
		if model.Provider == provider && model.API == api {
			return true
		}
	}
	return false
}
