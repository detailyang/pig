package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestPathsAndConfigParsing(t *testing.T) {
	t.Setenv("PIE_DIR", filepath.Join(t.TempDir(), "pie-home"))
	if got := BaseDir(); !strings.HasSuffix(got, "pie-home") {
		t.Fatalf("base dir mismatch: %s", got)
	}
	if DefaultBaseDir() != BaseDir() || DefaultSkillsRoot() != filepath.Join(BaseDir(), "skills") {
		t.Fatalf("default path helpers mismatch base=%s skills=%s", DefaultBaseDir(), DefaultSkillsRoot())
	}
	hash := CWDHash("/tmp/project")
	if len(hash) != 12 || hash != CwdHash("/tmp/project") || hash == CwdHash("/tmp/other") {
		t.Fatalf("bad cwd hash: %s", hash)
	}
	if got := SessionsDirForCwd("/tmp/project"); !strings.Contains(got, filepath.Join("sessions", hash)) {
		t.Fatalf("sessions dir mismatch: %s", got)
	}
	secs, ok, err := ParseTriggerPollIntervalSecs("[triggers]\npoll_interval_secs = 15\n")
	if err != nil || !ok || secs != 15 {
		t.Fatalf("trigger parse mismatch secs=%d ok=%v err=%v", secs, ok, err)
	}
	if _, _, err := ParseTriggerPollIntervalSecs("[triggers]\npoll_interval_secs = 0\n"); err == nil {
		t.Fatal("expected zero interval error")
	}
}

func TestBaseDirUsesPieDirValueVerbatimLikeUpstream(t *testing.T) {
	t.Setenv("PIE_DIR", "  /tmp/pie-home  ")
	if got := BaseDir(); got != "  /tmp/pie-home  " {
		t.Fatalf("PIE_DIR should be used verbatim like upstream, got %q", got)
	}
}

func TestConfigParsingRejectsMalformedTOMLLikeUpstream(t *testing.T) {
	if _, _, err := ParseTriggerPollIntervalSecs("[triggers]\npoll_interval_secs 15\n"); err == nil || !strings.Contains(err.Error(), "parse config.toml") {
		t.Fatalf("expected trigger parse error, got %v", err)
	}
}

func TestConfigParsingRejectsInvalidUTF8LikeUpstreamTOML(t *testing.T) {
	if _, _, err := ParseTriggerPollIntervalSecs(string([]byte("[triggers]\npoll_interval_secs = 1\n# \xff\n"))); err == nil || !strings.Contains(err.Error(), "parse config.toml") {
		t.Fatalf("expected trigger invalid UTF-8 parse error, got %v", err)
	}
}

func TestConfigParsingRejectsWrongValueTypesLikeUpstreamSerde(t *testing.T) {
	if _, _, err := ParseTriggerPollIntervalSecs("[triggers]\npoll_interval_secs = \"15\"\n"); err == nil || !strings.Contains(err.Error(), "parse config.toml") {
		t.Fatalf("expected quoted trigger interval parse error, got %v", err)
	}
	if _, _, err := ParseTriggerPollIntervalSecs("triggers = 1\n"); err == nil || !strings.Contains(err.Error(), "parse config.toml") {
		t.Fatalf("expected scalar triggers section parse error, got %v", err)
	}
}

func TestConfigParsingRejectsDuplicateKeysLikeUpstreamTOML(t *testing.T) {
	if _, _, err := ParseTriggerPollIntervalSecs("[triggers]\npoll_interval_secs = 15\npoll_interval_secs = 20\n"); err == nil || !strings.Contains(err.Error(), "parse config.toml") {
		t.Fatalf("expected duplicate trigger interval parse error, got %v", err)
	}
}

func TestConfigParsingRejectsDuplicateTablesLikeUpstreamTOML(t *testing.T) {
	if _, _, err := ParseTriggerPollIntervalSecs("[triggers]\npoll_interval_secs = 15\n[triggers]\npoll_interval_secs = 20\n"); err == nil || !strings.Contains(err.Error(), "parse config.toml") {
		t.Fatalf("expected duplicate triggers table parse error, got %v", err)
	}
}

func TestConfigParsingAcceptsInlineTablesLikeUpstreamTOML(t *testing.T) {
	secs, ok, err := ParseTriggerPollIntervalSecs("triggers = { poll_interval_secs = 15 }\n")
	if err != nil || !ok || secs != 15 {
		t.Fatalf("inline trigger table mismatch secs=%d ok=%v err=%v", secs, ok, err)
	}
}

func TestConfigParsingRejectsInlineTableEmptyMiddleFieldLikeUpstreamTOML(t *testing.T) {
	for _, text := range []string{
		"triggers = { poll_interval_secs = 15,, other = 1 }\n",
	} {
		if _, _, err := ParseTriggerPollIntervalSecs(text); err == nil || !strings.Contains(err.Error(), "parse config.toml") {
			t.Fatalf("expected inline empty field to fail like upstream, text=%q err=%v", text, err)
		}
	}
}

func TestConfigParsingAcceptsQuotedTablesAndKeysLikeUpstreamTOML(t *testing.T) {
	secs, ok, err := ParseTriggerPollIntervalSecs("[\"triggers\"]\n'poll_interval_secs' = 15\n")
	if err != nil || !ok || secs != 15 {
		t.Fatalf("quoted trigger table/key mismatch secs=%d ok=%v err=%v", secs, ok, err)
	}
}

func TestConfigParsingAcceptsDottedKeysLikeUpstreamTOML(t *testing.T) {
	secs, ok, err := ParseTriggerPollIntervalSecs("triggers.poll_interval_secs = 15\n")
	if err != nil || !ok || secs != 15 {
		t.Fatalf("dotted trigger key mismatch secs=%d ok=%v err=%v", secs, ok, err)
	}
}

func TestConfigParsingRejectsInvalidQuotedKeysLikeUpstreamTOML(t *testing.T) {
	for _, text := range []string{
		"[\"triggers]\npoll_interval_secs = 15\n",
		"[triggers]\n'poll_interval_secs = 15\n",
		"triggers = { 'poll_interval_secs = 15 }\n",
	} {
		if _, _, err := ParseTriggerPollIntervalSecs(text); err == nil || !strings.Contains(err.Error(), "parse config.toml") {
			t.Fatalf("expected invalid quoted key/table to fail like upstream, text=%q err=%v", text, err)
		}
	}
}

func TestConfigParsingRejectsInvalidBareKeysLikeUpstreamTOML(t *testing.T) {
	for _, text := range []string{
		"[bad table]\npoll_interval_secs = 15\n",
		"[triggers]\npoll interval secs = 15\n",
		"[triggers]\npoll/interval = 15\n",
		"triggers = { poll$interval = 15 }\n",
	} {
		if _, _, err := ParseTriggerPollIntervalSecs(text); err == nil || !strings.Contains(err.Error(), "parse config.toml") {
			t.Fatalf("expected invalid bare key/table to fail like upstream, text=%q err=%v", text, err)
		}
	}
}

func TestConfigParsingIgnoresUnknownSectionsAndKeysLikeUpstream(t *testing.T) {
	secs, ok, err := ParseTriggerPollIntervalSecs("top_level_unknown = true\n[metadata]\nowner = \"test\"\n[[audit]]\nname = \"ignored\"\n[triggers]\npoll_interval_secs = 15\nunknown = false\n")
	if err != nil || !ok || secs != 15 {
		t.Fatalf("unknown config should be ignored secs=%d ok=%v err=%v", secs, ok, err)
	}
}

func TestConfigParsingAcceptsUnderscoreIntegersLikeUpstreamTOML(t *testing.T) {
	secs, ok, err := ParseTriggerPollIntervalSecs("[triggers]\npoll_interval_secs = 1_000\n")
	if err != nil || !ok || secs != 1000 {
		t.Fatalf("underscore integer mismatch secs=%d ok=%v err=%v", secs, ok, err)
	}
}

func TestConfigParsingAcceptsPositiveSignedIntegersLikeUpstreamTOML(t *testing.T) {
	secs, ok, err := ParseTriggerPollIntervalSecs("[triggers]\npoll_interval_secs = +1_000\n")
	if err != nil || !ok || secs != 1000 {
		t.Fatalf("positive signed integer mismatch secs=%d ok=%v err=%v", secs, ok, err)
	}
}

func TestConfigParsingAcceptsPrefixedIntegersLikeUpstreamTOML(t *testing.T) {
	for text, want := range map[string]uint64{
		"[triggers]\npoll_interval_secs = 0x10\n":    16,
		"[triggers]\npoll_interval_secs = 0o10\n":    8,
		"[triggers]\npoll_interval_secs = 0b10000\n": 16,
	} {
		secs, ok, err := ParseTriggerPollIntervalSecs(text)
		if err != nil || !ok || secs != want {
			t.Fatalf("prefixed integer mismatch text=%q secs=%d ok=%v err=%v", text, secs, ok, err)
		}
	}
}

func TestConfigParsingRejectsInvalidUnderscoreIntegersLikeUpstreamTOML(t *testing.T) {
	for _, text := range []string{
		"[triggers]\npoll_interval_secs = _1000\n",
		"[triggers]\npoll_interval_secs = 1000_\n",
		"[triggers]\npoll_interval_secs = 1__000\n",
	} {
		if _, _, err := ParseTriggerPollIntervalSecs(text); err == nil {
			t.Fatalf("expected invalid underscore integer to fail: %q", text)
		}
	}
}

func TestConfigParsingRejectsLeadingZeroIntegersLikeUpstreamTOML(t *testing.T) {
	for _, text := range []string{
		"[triggers]\npoll_interval_secs = 015\n",
		"[triggers]\npoll_interval_secs = +015\n",
	} {
		if _, _, err := ParseTriggerPollIntervalSecs(text); err == nil {
			t.Fatalf("expected leading zero integer to fail: %q", text)
		}
	}
}

func TestAuthStoreRoundTripAndEnvPriority(t *testing.T) {
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
	if got, ok := reloaded.Get("openai"); !ok || got.Value != "stored" {
		t.Fatalf("auth round trip mismatch: %#v ok=%v", got, ok)
	}
	t.Setenv("OPENAI_API_KEY", "env-key")
	if got, ok := reloaded.ResolveForProvider("openai"); !ok || got != "env-key" {
		t.Fatalf("env should win got=%q ok=%v", got, ok)
	}
	t.Setenv("OPENAI_API_KEY", "")
	if got, ok := reloaded.ResolveForProvider("openai"); !ok || got != "stored" {
		t.Fatalf("store fallback mismatch got=%q ok=%v", got, ok)
	}
}

func TestAuthStoreSaveDoesNotHTMLEscapeLikeUpstreamSerdeJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store := AuthStore{}
	store.Set("openai", ProviderCredential{Type: CredentialAPIKey, Value: "<tag>&value"})
	if err := store.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("auth.json should not HTML-escape strings like upstream serde_json: %s", text)
	}
	if !strings.Contains(text, `"value": "<tag>&value"`) {
		t.Fatalf("auth.json missing unescaped credential value: %s", text)
	}
}

func TestEnvVarNamesAndCredentialHintMatchUpstream(t *testing.T) {
	t.Setenv("PIE_DIR", t.TempDir())
	if names := EnvVarNames("unknown-provider"); len(names) != 0 {
		t.Fatalf("unknown provider should not synthesize fallback env vars: %#v", names)
	}
	if hint := ModelCredentialHint("unknown-provider"); hint != "set the provider API key env var or run /login unknown-provider" {
		t.Fatalf("unknown provider hint mismatch: %q", hint)
	}
	if names := EnvVarNames("deepseek"); len(names) != 1 || names[0] != "DEEPSEEK_API_KEY" {
		t.Fatalf("deepseek env names mismatch: %#v", names)
	}
}

func TestProviderCredentialMarshalIncludesUpstreamRequiredFields(t *testing.T) {
	data, err := json.Marshal(ProviderCredential{Type: CredentialAPIKey})
	if err != nil {
		t.Fatal(err)
	}
	var apiKey map[string]any
	if err := json.Unmarshal(data, &apiKey); err != nil {
		t.Fatal(err)
	}
	if apiKey["kind"] != "api_key" || apiKey["value"] != "" || len(apiKey) != 2 {
		t.Fatalf("api key credential should include required value like upstream, got %s", data)
	}

}

func TestProviderCredentialMarshalRejectsUnknownKindLikeUpstreamEnum(t *testing.T) {
	if data, err := json.Marshal(ProviderCredential{Type: CredentialType("future"), Value: "x"}); err == nil {
		t.Fatalf("unknown credential kind should fail like upstream enum, got %s", data)
	}
}

func TestProviderCredentialUnmarshalRejectsInvalidUpstreamTaggedEnum(t *testing.T) {
	inputs := []string{
		`{"kind":"api_key"}`,
		`{"kind":"api_key","value":null}`,
		`{"kind":"future","value":"x"}`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			var credential ProviderCredential
			if err := json.Unmarshal([]byte(input), &credential); err == nil {
				t.Fatalf("invalid credential should fail like upstream serde enum: %#v", credential)
			}
		})
	}
}

func TestProviderCredentialUnmarshalRejectsUnsupportedCredential(t *testing.T) {
	var credential ProviderCredential
	unsupportedKind := "o" + "auth"
	err := json.Unmarshal([]byte(`{"kind":"`+unsupportedKind+`","access_token":"token"}`), &credential)
	if err == nil || !strings.Contains(err.Error(), `unknown kind "`+unsupportedKind+`"`) {
		t.Fatalf("expected unsupported credential rejection, got credential=%#v err=%v", credential, err)
	}
}

func TestAuthStoreRejectsInvalidUTF8JSONStringLikeSerdeJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	data := append([]byte(`{"version":1,"providers":{"openai":{"kind":"api_key","value":"`), 0xff)
	data = append(data, []byte(`"}}}`)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if store, err := LoadAuthStore(path); err == nil {
		t.Fatalf("invalid UTF-8 JSON string should fail like serde_json, got %#v", store)
	}
}

func TestLoadAuthStoreRejectsNullProvidersLikeUpstreamHashMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"providers":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if store, err := LoadAuthStore(path); err == nil {
		t.Fatalf("providers:null should fail like upstream HashMap, got %#v", store)
	}
}

func TestAuthStoreUnmarshalDefaultsVersionLikeUpstreamSerde(t *testing.T) {
	var store AuthStore
	if err := json.Unmarshal([]byte(`{}`), &store); err != nil {
		t.Fatal(err)
	}
	if store.Version != 1 || len(store.Providers) != 0 {
		t.Fatalf("auth store defaults mismatch: %#v", store)
	}
}

func TestAuthStoreUnmarshalRejectsNullVersionLikeUpstreamU32(t *testing.T) {
	var store AuthStore
	if err := json.Unmarshal([]byte(`{"version":null}`), &store); err == nil {
		t.Fatalf("version:null should fail like upstream u32 field, got %#v", store)
	}
}

func TestAuthStoreMarshalDefaultsLikeUpstreamSerde(t *testing.T) {
	data, err := json.Marshal(AuthStore{})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if object["version"] != float64(1) {
		t.Fatalf("version should default to 1 like upstream, got %s", data)
	}
	providers, ok := object["providers"].(map[string]any)
	if !ok || len(providers) != 0 {
		t.Fatalf("nil providers should marshal as empty object like upstream HashMap, got %s", data)
	}
}

func TestLoadAuthStoreAcceptsUpstreamJSONAndSaves0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	jsonText := `{"version":1,"providers":{"openai":{"kind":"api_key","value":"stored"}}}`
	if err := os.WriteFile(path, []byte(jsonText), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := LoadAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := store.ResolveForProvider("openai"); !ok || got != "stored" {
		t.Fatalf("api key load mismatch got=%q ok=%v", got, ok)
	}
	if err := store.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("auth file permissions mismatch: %o", info.Mode().Perm())
	}
}

func TestLoadAuthStoreTreatsWhitespaceOnlyFileAsEmptyLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(" \n\t"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := LoadAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Version != 0 || len(store.Providers) != 0 {
		t.Fatalf("whitespace-only auth store should load as default empty store, got %#v", store)
	}
}

func TestAutoDetectModelAndBaseURL(t *testing.T) {
	resetModels(t)
	ai.RegisterBuiltinModel(ai.Model{ID: "claude-haiku-4-5", Provider: ai.Provider("anthropic"), API: ai.ApiAnthropic})
	ai.RegisterBuiltinModel(ai.Model{ID: "gpt-4o-mini", Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses, BaseURL: "http://catalog/v1"})

	t.Setenv("OPENAI_API_KEY", "sk")
	model, err := AutoDetectModel(DetectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if model.Provider != ai.Provider("openai") || model.ID != "gpt-4o-mini" {
		t.Fatalf("detected wrong model: %#v", model)
	}

	model, err = AutoDetectModel(DetectOptions{Provider: "openai", ModelID: "gpt-4o-mini", BaseURL: "http://127.0.0.1:8000/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if model.BaseURL != "http://127.0.0.1:8000/v1" {
		t.Fatalf("base url override mismatch: %#v", model)
	}
}

func TestAutoDetectModelAppliesCustomHeaders(t *testing.T) {
	resetModels(t)
	ai.RegisterBuiltinModel(ai.Model{ID: "gpt-4o-mini", Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses, Headers: map[string]string{"X-Catalog": "catalog", "X-Provider": "catalog"}})
	t.Setenv("OPENAI_API_KEY", "sk")

	headers := map[string]string{"X-Provider": "custom"}
	model, err := AutoDetectModel(DetectOptions{Provider: "openai", ModelID: "gpt-4o-mini", Headers: headers})
	if err != nil {
		t.Fatal(err)
	}
	if model.Headers["X-Provider"] != "custom" || model.Headers["X-Catalog"] != "catalog" {
		t.Fatalf("explicit model headers mismatch: %#v", model.Headers)
	}

	headers["X-Provider"] = "mutated"
	if model.Headers["X-Provider"] != "custom" {
		t.Fatalf("model headers should not alias caller map: %#v", model.Headers)
	}
	model, err = AutoDetectModel(DetectOptions{Headers: map[string]string{"X-Auto": "yes"}})
	if err != nil {
		t.Fatal(err)
	}
	if model.Headers["X-Auto"] != "yes" || model.Headers["X-Catalog"] != "catalog" || model.Headers["X-Provider"] != "catalog" {
		t.Fatalf("detected model headers mismatch: %#v", model.Headers)
	}
}

func TestAutoDetectModelNoCredentialErrorMatchesUpstream(t *testing.T) {
	resetModels(t)
	for _, candidate := range detectCandidates {
		t.Setenv(candidate.EnvVar, "")
	}
	t.Setenv("PIE_DIR", t.TempDir())

	_, err := AutoDetectModel(DetectOptions{})
	if err == nil {
		t.Fatal("expected missing credential error")
	}
	want := "no API key found. Set one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, OPENROUTER_API_KEY, GROQ_API_KEY, MISTRAL_API_KEY, GEMINI_API_KEY, GOOGLE_API_KEY env vars, or run `/login <provider> <key>` from inside pie."
	if err.Error() != want {
		t.Fatalf("error mismatch:\n got: %s\nwant: %s", err, want)
	}
}

func TestAutoDetectModelUnknownProviderErrorMatchesUpstream(t *testing.T) {
	resetModels(t)
	ai.RegisterBuiltinModel(ai.Model{ID: "gpt-4o-mini", Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses})
	ai.RegisterBuiltinModel(ai.Model{ID: "claude-haiku-4-5", Provider: ai.Provider("anthropic"), API: ai.ApiAnthropic})

	_, err := AutoDetectModel(DetectOptions{Provider: "local", ModelID: "missing"})
	if err == nil {
		t.Fatal("expected unknown provider error")
	}
	want := "model provider not found in catalog: provider=local. Known providers: anthropic(1), openai(1)"
	if err.Error() != want {
		t.Fatalf("error mismatch:\n got: %s\nwant: %s", err, want)
	}
}

func TestAutoDetectModelUnknownModelErrorMatchesUpstream(t *testing.T) {
	resetModels(t)
	for index := range 13 {
		ai.RegisterBuiltinModel(ai.Model{ID: fmt.Sprintf("model-%02d", index), Provider: ai.Provider("openai"), API: ai.ApiOpenAIResponses})
	}

	_, err := AutoDetectModel(DetectOptions{Provider: "openai", ModelID: "missing"})
	if err == nil {
		t.Fatal("expected unknown model error")
	}
	want := "model not found in catalog: provider=openai id=missing. Candidates: model-00, model-01, model-02, model-03, model-04, model-05, model-06, model-07, model-08, model-09, model-10, model-11; run `/model list openai` inside pie for all 13 models"
	if err.Error() != want {
		t.Fatalf("error mismatch:\n got: %s\nwant: %s", err, want)
	}
}

func TestAutoDetectModelIgnoresUnsupportedLocalCredential(t *testing.T) {
	resetModels(t)
	t.Setenv("UNSUPPORTED_LOCAL_API_KEY", "sk")

	_, err := AutoDetectModel(DetectOptions{})
	if err == nil {
		t.Fatal("expected missing credential error")
	}
	want := "no API key found. Set one of: ANTHROPIC_API_KEY, OPENAI_API_KEY, OPENROUTER_API_KEY, GROQ_API_KEY, MISTRAL_API_KEY, GEMINI_API_KEY, GOOGLE_API_KEY env vars, or run `/login <provider> <key>` from inside pie."
	if err.Error() != want {
		t.Fatalf("error mismatch:\n got: %s\nwant: %s", err, want)
	}
}

func TestLoadModelsFileRegistersCustomModels(t *testing.T) {
	resetModels(t)
	path := filepath.Join(t.TempDir(), "models.json")
	json := `{"models":[{"id":"local","name":"Local","api":"openai-responses","provider":"local","baseUrl":"http://127.0.0.1:8000/v1","reasoning":true,"input":["text"],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":100,"maxTokens":50}]}`
	if err := os.WriteFile(path, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	models, err := LoadModelsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Provider != ai.Provider("local") || !models[0].Reasoning {
		t.Fatalf("models mismatch: %#v", models)
	}
	if _, ok := ai.GetModel(ai.Provider("local"), "local"); !ok {
		t.Fatal("custom model should be registered")
	}
}

func TestLoadModelsFileSupportsProviderMapCatalog(t *testing.T) {
	resetModels(t)
	path := filepath.Join(t.TempDir(), "models.generated.json")
	json := `{"anthropic":{"claude-test":{"id":"claude-test","name":"Claude Test","api":"anthropic-messages","provider":"anthropic","input":["text"],"cost":{"input":3,"output":15},"contextWindow":200000,"maxTokens":8192}}}`
	if err := os.WriteFile(path, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	models, err := LoadModelsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Provider != ai.Provider("anthropic") || models[0].API != ai.ApiAnthropic {
		t.Fatalf("models mismatch: %#v", models)
	}
	if _, ok := ai.GetModel(ai.Provider("anthropic"), "claude-test"); !ok {
		t.Fatal("custom model should be registered")
	}
}

func TestLoadModelsFileRejectsInvalidUTF8LikeUpstreamReadToString(t *testing.T) {
	resetModels(t)
	path := filepath.Join(t.TempDir(), "models.json")
	json := `{"models":[{"id":"local","name":"Local","api":"openai-responses","provider":"local","baseUrl":"http://127.0.0.1:8000/v1","reasoning":true,"input":["text"],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":100,"maxTokens":50,"note":"` + "\xff" + `"}]}`
	if err := os.WriteFile(path, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadModelsFile(path); err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 read error like upstream, got %v", err)
	}
	if _, ok := ai.GetModel(ai.Provider("local"), "local"); ok {
		t.Fatal("invalid UTF-8 models.json should not register custom model")
	}
}

func TestLoadLocalModelsAllDoesNotLoadLocalModels(t *testing.T) {
	resetModels(t)
	dir := t.TempDir()
	t.Setenv("PIE_DIR", filepath.Join(dir, "home"))
	cwd := filepath.Join(dir, "project")
	userPath := filepath.Join(dir, "home", "models.json")
	projectPath := filepath.Join(cwd, ".pie", "models.json")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(localModelJSON("local-test", "same", "openai-completions", "http://127.0.0.1:1/v1")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(localModelJSON("local-test", "same", "openai-responses", "http://127.0.0.1:2/v1")), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadLocalModelsAll(cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Models) != 0 {
		t.Fatalf("local models should not be loaded: %#v", loaded.Models)
	}
	if model, ok := ai.GetModel(ai.Provider("local-test"), "same"); ok {
		t.Fatalf("local model should not be registered: %#v", model)
	}
	if model, ok := ai.GetModel(ai.Provider("unsupported-local"), "local-model"); ok {
		t.Fatalf("unsupported local provider should not be registered as a builtin default: %#v", model)
	}
}

func TestLoadAllAliasDoesNotLoadLocalModels(t *testing.T) {
	resetModels(t)
	dir := t.TempDir()
	cwd := filepath.Join(dir, "work")
	if err := os.MkdirAll(filepath.Join(cwd, ".pie"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cwd, ".pie", "models.json")
	if err := os.WriteFile(path, []byte(localModelJSON("local-alias", "alias-model", "openai-responses", "http://127.0.0.1:2/v1")), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadAll(cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Models) != 0 {
		t.Fatalf("LoadAll should not load local models: %#v", loaded.Models)
	}
	if model, ok := ai.GetModel(ai.Provider("local-alias"), "alias-model"); ok {
		t.Fatalf("local model should not be registered: %#v", model)
	}
}

func TestLoadLocalModelsDoesNotRegisterImplicitProviderFromBaseURL(t *testing.T) {
	resetModels(t)
	t.Setenv("UNSUPPORTED_LOCAL_BASE_URL", "http://127.0.0.1:8000/v1")
	if _, err := LoadLocalModelsFromPaths(nil, "http://127.0.0.1:9999/v1"); err != nil {
		t.Fatal(err)
	}
	if model, ok := ai.GetModel(ai.Provider("unsupported-local"), "local-model"); ok {
		t.Fatalf("unsupported local provider should not be registered from base-url/env: %#v", model)
	}
}

func TestLoadAllFromPathsWithBaseURLDoesNotLoadLocalModels(t *testing.T) {
	resetModels(t)
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(localModelJSON("local-test", "test-model", "openai-responses", "http://127.0.0.1:2/v1")), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadAllFromPathsWithBaseURL([]string{path}, "http://ignored.local/v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Models) != 0 {
		t.Fatalf("local models should not be loaded: %#v", loaded.Models)
	}
	if model, ok := ai.GetModel(ai.Provider("local-test"), "test-model"); ok {
		t.Fatalf("local model should not be registered: %#v", model)
	}
}

func localModelJSON(provider, id, apiName, baseURL string) string {
	return `{"models":[{"id":"` + id + `","name":"Local","api":"` + apiName + `","provider":"` + provider + `","baseUrl":"` + baseURL + `","reasoning":true,"input":["text"],"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0},"contextWindow":100000,"maxTokens":1000}]}`
}

func resetModels(t *testing.T) {
	t.Helper()
	ai.ClearBuiltinModels()
	ai.ClearCustomModels()
	t.Cleanup(func() {
		ai.ClearBuiltinModels()
		ai.ClearCustomModels()
	})
}
