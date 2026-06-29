package ai

import (
	"fmt"
	"os"
	"strings"
)

const (
	CloudflareWorkersAIBaseURL          = "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai/v1"
	CloudflareAIGatewayCompatBaseURL    = "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/compat"
	CloudflareAIGatewayOpenAIBaseURL    = "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai"
	CloudflareAIGatewayAnthropicBaseURL = "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic"

	CLOUDFLARE_WORKERS_AI_BASE_URL           = CloudflareWorkersAIBaseURL
	CLOUDFLARE_AI_GATEWAY_COMPAT_BASE_URL    = CloudflareAIGatewayCompatBaseURL
	CLOUDFLARE_AI_GATEWAY_OPENAI_BASE_URL    = CloudflareAIGatewayOpenAIBaseURL
	CLOUDFLARE_AI_GATEWAY_ANTHROPIC_BASE_URL = CloudflareAIGatewayAnthropicBaseURL

	CLOUDFLAREWORKERSAIBASEURL          = CLOUDFLARE_WORKERS_AI_BASE_URL
	CLOUDFLAREAIGATEWAYCOMPATBASEURL    = CLOUDFLARE_AI_GATEWAY_COMPAT_BASE_URL
	CLOUDFLAREAIGATEWAYOPENAIBASEURL    = CLOUDFLARE_AI_GATEWAY_OPENAI_BASE_URL
	CLOUDFLAREAIGATEWAYANTHROPICBASEURL = CLOUDFLARE_AI_GATEWAY_ANTHROPIC_BASE_URL
)

func IsCloudflareProvider(provider Provider) bool {
	return provider == Provider("cloudflare-workers-ai") || provider == Provider("cloudflare-ai-gateway")
}

func ResolveCloudflareBaseURL(model Model) (string, error) {
	if !strings.Contains(model.BaseURL, "{") {
		return model.BaseURL, nil
	}
	var out strings.Builder
	for index := 0; index < len(model.BaseURL); index++ {
		if model.BaseURL[index] != '{' {
			out.WriteByte(model.BaseURL[index])
			continue
		}
		end := strings.IndexByte(model.BaseURL[index+1:], '}')
		nameEnd := len(model.BaseURL)
		if end >= 0 {
			nameEnd = index + 1 + end
		}
		name := model.BaseURL[index+1 : nameEnd]
		value := os.Getenv(name)
		if value == "" {
			return "", fmt.Errorf("%s is required for provider %s but is not set.", name, model.Provider)
		}
		out.WriteString(value)
		if end < 0 {
			break
		}
		index += end + 1
	}
	return out.String(), nil
}

func ResolveCloudflareBaseUrl(model Model) (string, error) { return ResolveCloudflareBaseURL(model) }

func ResolveProviderBaseURL(model Model, options StreamOptions) (string, error) {
	if IsCloudflareProvider(model.Provider) {
		return ResolveCloudflareBaseURL(model)
	}
	return model.BaseURL, nil
}
