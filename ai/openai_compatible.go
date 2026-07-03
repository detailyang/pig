package ai

import (
	"net/url"
	"strings"
)

func missingOpenAICompatibleAPIKeyMessage(model Model) string {
	provider := string(model.Provider)
	names := EnvVarNames(provider)
	if len(names) == 0 {
		return "no API key for provider: " + provider + "; pass options.api_key or configure a provider-specific credential"
	}
	return "no API key for provider: " + provider + "; set " + strings.Join(names, " or ") + " or pass options.api_key"
}

func hasOpenAICompatibleAPIVersionPath(base string) bool {
	parsed, err := url.Parse(base)
	if err != nil {
		return false
	}
	path := strings.Trim(strings.TrimRight(parsed.Path, "/"), "/")
	if path == "" {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if isOpenAICompatibleAPIVersionSegment(segment) {
			return true
		}
	}
	return false
}

func isOpenAICompatibleAPIVersionSegment(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, ch := range segment[1:] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
