package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveCloudflareBaseURL(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "gateway")

	model := Model{Provider: Provider("cloudflare-ai-gateway"), BaseURL: CLOUDFLARE_AI_GATEWAY_OPENAI_BASE_URL}
	got, err := ResolveCloudflareBaseURL(model)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://gateway.ai.cloudflare.com/v1/acct/gateway/openai"
	if got != want {
		t.Fatalf("base url mismatch: %s", got)
	}
}

func TestCloudflareUpstreamConstantNames(t *testing.T) {
	if CLOUDFLARE_WORKERS_AI_BASE_URL != CloudflareWorkersAIBaseURL {
		t.Fatalf("workers base url alias mismatch: %s", CLOUDFLARE_WORKERS_AI_BASE_URL)
	}
	if CLOUDFLARE_AI_GATEWAY_COMPAT_BASE_URL != CloudflareAIGatewayCompatBaseURL {
		t.Fatalf("compat gateway alias mismatch: %s", CLOUDFLARE_AI_GATEWAY_COMPAT_BASE_URL)
	}
	if CLOUDFLARE_AI_GATEWAY_OPENAI_BASE_URL != CloudflareAIGatewayOpenAIBaseURL {
		t.Fatalf("openai gateway alias mismatch: %s", CLOUDFLARE_AI_GATEWAY_OPENAI_BASE_URL)
	}
	if CLOUDFLARE_AI_GATEWAY_ANTHROPIC_BASE_URL != CloudflareAIGatewayAnthropicBaseURL {
		t.Fatalf("anthropic gateway alias mismatch: %s", CLOUDFLARE_AI_GATEWAY_ANTHROPIC_BASE_URL)
	}
}

func TestResolveCloudflareBaseURLErrorsOnMissingEnv(t *testing.T) {
	model := Model{Provider: Provider("cloudflare-workers-ai"), BaseURL: "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_MISSING}/ai/v1"}
	_, err := ResolveCloudflareBaseURL(model)
	if err == nil {
		t.Fatal("expected missing env error")
	}
	if err.Error() != "CLOUDFLARE_MISSING is required for provider cloudflare-workers-ai but is not set." {
		t.Fatalf("error mismatch: %v", err)
	}

	model.BaseURL = "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_UNCLOSED"
	t.Setenv("CLOUDFLARE_UNCLOSED", "acct")
	got, err := ResolveCloudflareBaseURL(model)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.cloudflare.com/client/v4/accounts/acct" {
		t.Fatalf("unterminated placeholder should resolve like upstream, got %s", got)
	}
}

func TestResolveProviderBaseURLAppliesCloudflarePlaceholders(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	model := Model{Provider: Provider("cloudflare-workers-ai"), BaseURL: "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai/v1"}
	got, err := ResolveProviderBaseURL(model, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.cloudflare.com/client/v4/accounts/acct/ai/v1" {
		t.Fatalf("base url mismatch: %s", got)
	}
}

func TestOpenAIChatProviderUsesResolvedCloudflareBaseURL(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/v4/accounts/acct/ai/v1/chat/completions" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			"data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n",
			"data: [DONE]\n\n",
		}, "")))
	}))
	defer server.Close()

	provider := NewOpenAIChatProvider(WithOpenAIChatHTTPClient(server.Client()))
	model := Model{ID: "@cf/model", Provider: Provider("cloudflare-workers-ai"), API: ApiOpenAI, BaseURL: server.URL + "/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai/v1"}
	message, ok := provider.Stream(context.Background(), model, Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: ContentText, Text: "hi"}}}}}, StreamOptions{APIKey: "cf-key"}).Result()
	if !ok {
		t.Fatal("expected completed message")
	}
	if message.Text() != "ok" {
		t.Fatalf("message mismatch: %#v", message)
	}
}

func TestOpenAIResponsesProviderUsesResolvedCloudflareBaseURL(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "gateway")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/acct/gateway/openai/responses" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.output_text.delta\ndata: {\"delta\":\"ok\"}\n\n"))
		_, _ = w.Write([]byte("event: response.completed\ndata: {\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIResponsesProvider(WithHTTPClient(server.Client()))
	model := Model{ID: "gpt-test", Provider: Provider("cloudflare-ai-gateway"), API: ApiOpenAIResponses, BaseURL: server.URL + "/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai"}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "cf-key"}).Result()
	if !ok || message.Text() != "ok" {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
}

func TestAnthropicProviderUsesResolvedCloudflareBaseURL(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	t.Setenv("CLOUDFLARE_GATEWAY_ID", "gateway")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/acct/gateway/anthropic/v1/messages" {
			t.Fatalf("path mismatch: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	provider := NewAnthropicProvider(WithAnthropicHTTPClient(server.Client()))
	model := Model{ID: "claude-test", Provider: Provider("cloudflare-ai-gateway"), API: ApiAnthropic, BaseURL: server.URL + "/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/anthropic"}
	message, ok := provider.Stream(context.Background(), model, Context{}, StreamOptions{APIKey: "cf-key"}).Result()
	if !ok || message.Text() != "ok" {
		t.Fatalf("message mismatch: %#v ok=%v", message, ok)
	}
}
