package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProxyFromEnvReExportsAIProxyHelper(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7890")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:8888")
	got, ok := ProxyFromEnv()
	if !ok || got != "http://127.0.0.1:7890" {
		t.Fatalf("proxy_from_env alias mismatch: %q ok=%v", got, ok)
	}
}

func TestBuildClientReExportsAIHTTPClientBuilder(t *testing.T) {
	proxied := false
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = true
		_, _ = io.WriteString(w, "proxied")
	}))
	defer proxyServer.Close()

	t.Setenv("HTTPS_PROXY", proxyServer.URL)
	t.Setenv("HTTP_PROXY", proxyServer.URL)
	t.Setenv("NO_PROXY", "")
	client, err := BuildClient(1500)
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 1500*time.Millisecond {
		t.Fatalf("timeout mismatch: %s", client.Timeout)
	}
	response, err := client.Get("http://example.test/resource")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if !proxied {
		t.Fatal("BuildClient should use the proxy configured in env")
	}
}
