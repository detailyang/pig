package ai

import "testing"

func TestGetEnvAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk")
	got, ok := GetEnvAPIKey("openai")
	if !ok || got != "sk" {
		t.Fatalf("env key mismatch got=%q ok=%v", got, ok)
	}
	if got, ok := GetEnvApiKey("openai"); !ok || got != "sk" {
		t.Fatalf("env key alias mismatch got=%q ok=%v", got, ok)
	}
	if names := EnvVarNames("azure-openai-responses"); len(names) != 1 || names[0] != "AZURE_OPENAI_API_KEY" {
		t.Fatalf("azure env names mismatch: %#v", names)
	}
	if names := EnvVarNames("openai-codex"); len(names) != 1 || names[0] != "OPENAI_API_KEY" {
		t.Fatalf("codex env names mismatch: %#v", names)
	}
	if names := EnvVarNames("google"); len(names) != 2 || names[0] != "GOOGLE_API_KEY" || names[1] != "GEMINI_API_KEY" {
		t.Fatalf("google env names mismatch: %#v", names)
	}
	if names := EnvVarNames("unknown-provider"); len(names) != 0 {
		t.Fatalf("unknown provider should have no fallback env names like upstream: %#v", names)
	}
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-key")
	if got, ok := GetEnvAPIKey("deepseek"); !ok || got != "deepseek-key" {
		t.Fatalf("deepseek env key mismatch got=%q ok=%v", got, ok)
	}
	t.Setenv("GROQ_API_KEY", "  groq-key  ")
	if got, ok := GetEnvAPIKey("groq"); !ok || got != "  groq-key  " {
		t.Fatalf("groq env key should preserve whitespace got=%q ok=%v", got, ok)
	}
}
