package config

import (
	"path/filepath"
	"testing"
)

func TestProviderCredentialUpstreamVariantAliases(t *testing.T) {
	if ProviderCredentialApiKey != CredentialAPIKey {
		t.Fatalf("provider credential api key alias mismatch")
	}
}

func TestAuthStoreUpstreamMethodAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store := AuthStore{}
	store.Set("openai", ProviderCredential{Type: CredentialAPIKey, Value: "stored"})
	if err := store.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	reloaded, err := AuthStore{}.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.ResolveForProvider("openai"); !ok || got != "stored" {
		t.Fatalf("LoadFrom alias mismatch got=%q ok=%v", got, ok)
	}
	t.Setenv("PIE_DIR", t.TempDir())
	defaultStore, err := AuthStore{}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultStore.Providers) != 0 {
		t.Fatalf("Load alias should read default empty auth store, got %#v", defaultStore)
	}
}
