package ai

import "testing"

func resetBuiltinProvidersEnsureForTest() {
	builtinProvidersMu.Lock()
	defer builtinProvidersMu.Unlock()
	builtinProvidersEnsured = false
}

func TestRegisterBuiltinProviders(t *testing.T) {
	ClearAPIProviders()
	RegisterBuiltinProviders()
	for _, api := range []Api{
		ApiAnthropic,
		ApiFaux,
		ApiOpenAIResponses,
		ApiOpenAICompletions,
		ApiGoogleGenerativeAI,
		ApiMistral,
		ApiAzureOpenAIResponses,
		ApiGoogleVertex,
		ApiBedrockConverseStream,
		ApiOpenAICodexResponses,
	} {
		if _, ok := GetAPIProvider(api); !ok {
			t.Fatalf("expected provider for %s", api)
		}
	}
	if _, ok := GetAPIProvider(ApiOpenAI); ok {
		t.Fatal("openai chat provider should not be registered as builtin like upstream")
	}
}

func TestEnsureBuiltinProvidersRunsOnceLikeUpstream(t *testing.T) {
	resetBuiltinProvidersEnsureForTest()
	ClearAPIProviders()
	Ensure()
	if _, ok := GetAPIProvider(ApiFaux); !ok {
		t.Fatal("expected faux provider after first ensure")
	}

	ClearAPIProviders()
	Ensure()
	if _, ok := GetAPIProvider(ApiFaux); ok {
		t.Fatal("ensure should be process-once like upstream OnceLock")
	}
}
