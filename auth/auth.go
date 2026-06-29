package auth

import "github.com/detailyang/pig/config"

type CredentialType = config.CredentialType

const (
	CredentialAPIKey         = config.CredentialAPIKey
	ProviderCredentialApiKey = config.ProviderCredentialApiKey
)

type ProviderCredential = config.ProviderCredential

type AuthStore = config.AuthStore

func AuthPath() string {
	return config.AuthPath()
}

func LoadDefaultAuthStore() (AuthStore, error) {
	return config.LoadDefaultAuthStore()
}

func LoadAuthStore(path string) (AuthStore, error) {
	return config.LoadAuthStore(path)
}

func ModelCredentialHint(provider string) string {
	return config.ModelCredentialHint(provider)
}

func HasModelCredential(provider string) bool {
	return config.HasModelCredential(provider)
}
