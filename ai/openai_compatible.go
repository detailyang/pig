package ai

import "strings"

func missingOpenAICompatibleAPIKeyMessage(model Model) string {
	provider := string(model.Provider)
	names := EnvVarNames(provider)
	if len(names) == 0 {
		return "no API key for provider: " + provider + "; pass options.api_key or configure a provider-specific credential"
	}
	return "no API key for provider: " + provider + "; set " + strings.Join(names, " or ") + " or pass options.api_key"
}
