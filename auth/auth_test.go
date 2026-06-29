package auth

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthPackageMirrorsUpstreamAPIKeyStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store := AuthStore{}
	store.Set("openai", ProviderCredential{Type: CredentialAPIKey, Value: "stored"})
	if err := store.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.ResolveForProvider("openai"); !ok || got != "stored" {
		t.Fatalf("resolve mismatch got=%q ok=%v", got, ok)
	}
	removed, ok := reloaded.Remove("openai")
	if !ok || removed.Value != "stored" {
		t.Fatalf("remove mismatch removed=%#v ok=%v", removed, ok)
	}
}

func TestAuthPackageRejectsUnsupportedCredentialByDesign(t *testing.T) {
	var credential ProviderCredential
	unsupportedKind := "o" + "auth"
	err := json.Unmarshal([]byte(`{"kind":"`+unsupportedKind+`","access_token":"token"}`), &credential)
	if err == nil || !strings.Contains(err.Error(), `unknown kind "`+unsupportedKind+`"`) {
		t.Fatalf("unsupported credential should remain rejected, credential=%#v err=%v", credential, err)
	}
}

func TestAuthPackageDefaultPathUsesPieDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", dir)
	if got := AuthPath(); got != filepath.Join(dir, "auth.json") {
		t.Fatalf("auth path mismatch: %q", got)
	}
}
