package ai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestProxyURLFromEnvMatchesUpstreamPriority(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "https://upper-https.example:8443")
	t.Setenv("https_proxy", "https://lower-https.example:8443")
	t.Setenv("HTTP_PROXY", "http://upper-http.example:8080")
	t.Setenv("http_proxy", "http://lower-http.example:8080")
	proxyURL, ok := ProxyURLFromEnv()
	if !ok || proxyURL != "https://upper-https.example:8443" {
		t.Fatalf("proxy = %q ok=%v", proxyURL, ok)
	}
	aliasURL, ok := ProxyFromEnv()
	if !ok || aliasURL != proxyURL {
		t.Fatalf("upstream proxy_from_env alias = %q ok=%v", aliasURL, ok)
	}
}

func TestProxyURLFromEnvFallsBackThroughLowercaseAndHTTP(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "https://lower-https.example:8443")
	t.Setenv("HTTP_PROXY", "http://upper-http.example:8080")
	t.Setenv("http_proxy", "http://lower-http.example:8080")
	proxyURL, ok := ProxyURLFromEnv()
	if !ok || proxyURL != "https://lower-https.example:8443" {
		t.Fatalf("proxy = %q ok=%v", proxyURL, ok)
	}

	t.Setenv("https_proxy", "")
	proxyURL, ok = ProxyURLFromEnv()
	if !ok || proxyURL != "http://upper-http.example:8080" {
		t.Fatalf("proxy = %q ok=%v", proxyURL, ok)
	}

	t.Setenv("HTTP_PROXY", "")
	proxyURL, ok = ProxyURLFromEnv()
	if !ok || proxyURL != "http://lower-http.example:8080" {
		t.Fatalf("proxy = %q ok=%v", proxyURL, ok)
	}
}

func TestBuildHTTPClientFromEnvConfiguresProxyAndTimeout(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7890")
	t.Setenv("NO_PROXY", "")
	client, err := BuildHTTPClientFromEnv(2500)
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 2500*time.Millisecond {
		t.Fatalf("timeout = %s", client.Timeout)
	}
	transport, ok := baseHTTPTransport(client.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("transport/proxy mismatch: %#v", client.Transport)
	}
	request := &http.Request{URL: &url.URL{Scheme: "https", Host: "api.example.test"}}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy url = %s", proxyURL)
	}
	aliasClient, err := BuildClient(2500)
	if err != nil {
		t.Fatal(err)
	}
	if aliasClient.Timeout != client.Timeout {
		t.Fatalf("upstream build_client alias timeout = %s want %s", aliasClient.Timeout, client.Timeout)
	}
}

func TestBuildHTTPClientFromEnvHonorsNoProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7890")
	t.Setenv("NO_PROXY", "api.example.test")
	client, err := BuildHTTPClientFromEnv(0)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := baseHTTPTransport(client.Transport)
	if !ok {
		t.Fatalf("transport mismatch: %#v", client.Transport)
	}
	request := &http.Request{URL: &url.URL{Scheme: "https", Host: "api.example.test"}}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL != nil {
		t.Fatalf("expected NO_PROXY bypass, got %s", proxyURL)
	}
}

func baseHTTPTransport(roundTripper http.RoundTripper) (*http.Transport, bool) {
	if transport, ok := roundTripper.(*http.Transport); ok {
		return transport, true
	}
	if transport, ok := roundTripper.(userAgentTransport); ok {
		base, ok := transport.base.(*http.Transport)
		return base, ok
	}
	return nil, false
}

func TestBuildHTTPClientFromEnvSetsUserAgentLikeUpstream(t *testing.T) {
	var userAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	client, err := BuildHTTPClientFromEnv(0)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()

	if userAgent != UserAgent() {
		t.Fatalf("user agent should match upstream build_client default: %q", userAgent)
	}
}

func TestDefaultHTTPClientHonorsCurrentProxyEnvLikeUpstream(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7890")
	t.Setenv("NO_PROXY", "")

	client := DefaultHTTPClient()
	transport, ok := baseHTTPTransport(client.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("default client transport/proxy mismatch: %#v", client.Transport)
	}
	request := &http.Request{URL: &url.URL{Scheme: "https", Host: "api.example.test"}}
	proxyURL, err := transport.Proxy(request)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy url = %s", proxyURL)
	}
}
