package ai

import "sync"

var builtinProvidersMu sync.Mutex
var builtinProvidersEnsured bool

func EnsureBuiltinProviders() {
	builtinProvidersMu.Lock()
	defer builtinProvidersMu.Unlock()
	if builtinProvidersEnsured {
		return
	}
	builtinProvidersEnsured = true
	RegisterBuiltinProviders()
}

func Ensure() {
	EnsureBuiltinProviders()
}

func RegisterBuiltinProviders() {
	registerBuiltinProvider(NewFauxProvider())
	registerBuiltinProvider(NewOpenAIResponsesProvider())
	registerBuiltinProvider(NewAzureOpenAIResponsesProvider())
	registerBuiltinProvider(NewCodexResponsesProvider())
	registerBuiltinProvider(NewOpenAICompletionsProvider())
	registerBuiltinProvider(NewAnthropicProvider())
	registerBuiltinProvider(NewMistralProvider())
	registerBuiltinProvider(NewBedrockProvider())
	registerBuiltinProvider(NewGoogleProvider())
	registerBuiltinProvider(NewGoogleVertexProvider())
}

func registerBuiltinProvider(provider APIProvider) {
	RegisterAPIProvider(provider, "builtin")
}
