package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

type CredentialType string

const (
	CredentialAPIKey CredentialType = "api_key"

	ProviderCredentialApiKey = CredentialAPIKey
)

type ProviderCredential struct {
	Type  CredentialType `json:"kind"`
	Value string         `json:"value,omitempty"`
}

func (credential *ProviderCredential) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	var kind CredentialType
	if err := json.Unmarshal(object["kind"], &kind); err != nil {
		return err
	}
	type alias ProviderCredential
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	switch kind {
	case CredentialAPIKey:
		if err := requireAuthJSONField(object, "api_key", "value"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("config provider credential unknown kind %q", kind)
	}
	*credential = ProviderCredential(decoded)
	return nil
}

func (credential ProviderCredential) MarshalJSON() ([]byte, error) {
	switch credential.Type {
	case "", CredentialAPIKey:
		object := struct {
			Kind  CredentialType `json:"kind"`
			Value string         `json:"value"`
		}{Kind: credential.Type, Value: credential.Value}
		if object.Kind == "" {
			object.Kind = CredentialAPIKey
		}
		return marshalJSONNoHTMLEscape(object)
	default:
		return nil, fmt.Errorf("config provider credential unknown kind %q", credential.Type)
	}
}

func requireAuthJSONField(object map[string]json.RawMessage, kind string, field string) error {
	raw, ok := object[field]
	if !ok {
		return fmt.Errorf("config provider credential %s missing required field %q", kind, field)
	}
	if string(raw) == "null" {
		return fmt.Errorf("config provider credential %s %s cannot be null", kind, field)
	}
	return nil
}

type AuthStore struct {
	Version   uint32                        `json:"version"`
	Providers map[string]ProviderCredential `json:"providers"`
}

func (store AuthStore) MarshalJSON() ([]byte, error) {
	version := store.Version
	if version == 0 {
		version = 1
	}
	providers := store.Providers
	if providers == nil {
		providers = map[string]ProviderCredential{}
	}
	object := struct {
		Version   uint32                        `json:"version"`
		Providers map[string]ProviderCredential `json:"providers"`
	}{Version: version, Providers: providers}
	return marshalJSONNoHTMLEscape(object)
}

func (store *AuthStore) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if rawProviders, ok := object["providers"]; ok && string(rawProviders) == "null" {
		return fmt.Errorf("config auth store providers cannot be null")
	}
	if rawVersion, ok := object["version"]; ok && string(rawVersion) == "null" {
		return fmt.Errorf("config auth store version cannot be null")
	}
	type alias AuthStore
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Providers == nil {
		decoded.Providers = map[string]ProviderCredential{}
	}
	if decoded.Version == 0 {
		decoded.Version = 1
	}
	*store = AuthStore(decoded)
	return nil
}

func LoadDefaultAuthStore() (AuthStore, error) { return LoadAuthStore(AuthPath()) }

func (store AuthStore) Load() (AuthStore, error) { return LoadDefaultAuthStore() }

func (store AuthStore) LoadFrom(path string) (AuthStore, error) { return LoadAuthStore(path) }

func LoadAuthStore(path string) (AuthStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AuthStore{}, nil
		}
		return AuthStore{}, err
	}
	if stringsTrim(string(data)) == "" {
		return AuthStore{}, nil
	}
	if !utf8.Valid(data) {
		return AuthStore{}, fmt.Errorf("auth store is not valid UTF-8")
	}
	var store AuthStore
	if err := json.Unmarshal(data, &store); err != nil {
		return AuthStore{}, err
	}
	if store.Version == 0 {
		store.Version = 1
	}
	return store, nil
}

func (store AuthStore) Save() error { return store.SaveTo(AuthPath()) }

func (store AuthStore) SaveTo(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if store.Providers == nil {
		store.Providers = map[string]ProviderCredential{}
	}
	if store.Version == 0 {
		store.Version = 1
	}
	data, err := marshalJSONIndentNoHTMLEscape(store)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (store *AuthStore) Set(provider string, credential ProviderCredential) {
	if store.Providers == nil {
		store.Providers = map[string]ProviderCredential{}
	}
	if credential.Type == "" {
		credential.Type = CredentialAPIKey
	}
	store.Providers[provider] = credential
}

func (store *AuthStore) Remove(provider string) (ProviderCredential, bool) {
	if store.Providers == nil {
		return ProviderCredential{}, false
	}
	credential, ok := store.Providers[provider]
	delete(store.Providers, provider)
	return credential, ok
}

func (store AuthStore) Get(provider string) (ProviderCredential, bool) {
	if store.Providers == nil {
		return ProviderCredential{}, false
	}
	credential, ok := store.Providers[provider]
	return credential, ok
}

func (store AuthStore) ResolveForProvider(provider string) (string, bool) {
	for _, envVar := range EnvVarNames(provider) {
		if value := os.Getenv(envVar); stringsTrim(value) != "" {
			return value, true
		}
	}
	credential, ok := store.Get(provider)
	if !ok {
		return "", false
	}
	return credential.Value, credential.Value != ""
}

func ModelCredentialHint(provider string) string {
	vars := EnvVarNames(provider)
	for _, name := range vars {
		if stringsTrim(os.Getenv(name)) != "" {
			return ""
		}
	}
	if store, err := LoadDefaultAuthStore(); err == nil {
		if _, ok := store.Get(provider); ok {
			return ""
		}
	}
	envHint := "set the provider API key env var"
	if len(vars) > 0 {
		envHint = "set " + joinStrings(vars, " or ")
	}
	return fmt.Sprintf("%s or run /login %s", envHint, provider)
}

func HasModelCredential(provider string) bool {
	return ModelCredentialHint(provider) == ""
}

func joinStrings(values []string, separator string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, value := range values[1:] {
		out += separator + value
	}
	return out
}

func stringsTrim(value string) string {
	for len(value) > 0 && (value[0] == ' ' || value[0] == '\t' || value[0] == '\n' || value[0] == '\r') {
		value = value[1:]
	}
	for len(value) > 0 {
		last := value[len(value)-1]
		if last != ' ' && last != '\t' && last != '\n' && last != '\r' {
			break
		}
		value = value[:len(value)-1]
	}
	return value
}
