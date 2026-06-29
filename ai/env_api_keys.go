package ai

import (
	"os"
)

var envAPIKeyNames = map[string][]string{
	"anthropic":              {"ANTHROPIC_API_KEY"},
	"openai":                 {"OPENAI_API_KEY"},
	"openai-codex":           {"OPENAI_API_KEY"},
	"azure-openai-responses": {"AZURE_OPENAI_API_KEY"},
	"google":                 {"GOOGLE_API_KEY", "GEMINI_API_KEY"},
	"google-vertex":          {"GOOGLE_APPLICATION_CREDENTIALS"},
	"amazon-bedrock":         {"AWS_ACCESS_KEY_ID"},
	"mistral":                {"MISTRAL_API_KEY"},
	"xai":                    {"XAI_API_KEY"},
	"groq":                   {"GROQ_API_KEY"},
	"cerebras":               {"CEREBRAS_API_KEY"},
	"openrouter":             {"OPENROUTER_API_KEY"},
	"vercel-ai-gateway":      {"AI_GATEWAY_API_KEY"},
	"zai":                    {"ZAI_API_KEY"},
	"deepseek":               {"DEEPSEEK_API_KEY"},
	"fireworks":              {"FIREWORKS_API_KEY"},
	"together":               {"TOGETHER_API_KEY"},
	"github-copilot":         {"GITHUB_COPILOT_TOKEN"},
	"huggingface":            {"HUGGINGFACE_API_KEY", "HF_TOKEN"},
	"cloudflare-workers-ai":  {"CLOUDFLARE_API_TOKEN"},
}

func EnvVarNames(provider string) []string {
	if names, ok := envAPIKeyNames[provider]; ok {
		return append([]string(nil), names...)
	}
	return []string{}
}

func GetEnvAPIKey(provider string) (string, bool) {
	for _, name := range EnvVarNames(provider) {
		if value := os.Getenv(name); value != "" {
			return value, true
		}
	}
	return "", false
}

func GetEnvApiKey(provider string) (string, bool) { return GetEnvAPIKey(provider) }
