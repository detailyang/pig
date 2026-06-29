package ai

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func DefaultHTTPClient() *http.Client {
	client, err := BuildHTTPClientFromEnv(0)
	if err != nil {
		return &http.Client{Transport: userAgentTransport{base: http.DefaultTransport}}
	}
	return client
}

func DefaultHTTPClientForStream(options StreamOptions) (*http.Client, error) {
	return BuildHTTPClientFromEnv(int64(options.TimeoutMS))
}

func effectiveHTTPClient(client *http.Client, options StreamOptions) (*http.Client, error) {
	if client != nil {
		return client, nil
	}
	return DefaultHTTPClientForStream(options)
}

func ProxyURLFromEnv() (string, bool) {
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			return value, true
		}
	}
	return "", false
}

func ProxyFromEnv() (string, bool) {
	return ProxyURLFromEnv()
}

func BuildHTTPClientFromEnv(timeoutMS int64) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if rawProxyURL, ok := ProxyURLFromEnv(); ok {
		proxyURL, err := url.Parse(rawProxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = func(request *http.Request) (*url.URL, error) {
			if bypassProxy(request.URL.Hostname()) {
				return nil, nil
			}
			return proxyURL, nil
		}
	}
	client := &http.Client{Transport: userAgentTransport{base: transport}}
	if timeoutMS > 0 {
		client.Timeout = time.Duration(timeoutMS) * time.Millisecond
	}
	return client, nil
}

func BuildClient(timeoutMS int64) (*http.Client, error) {
	return BuildHTTPClientFromEnv(timeoutMS)
}

type userAgentTransport struct {
	base http.RoundTripper
}

func (transport userAgentTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Header.Get("User-Agent") == "" {
		request = request.Clone(request.Context())
		request.Header.Set("User-Agent", UserAgent())
	}
	return transport.base.RoundTrip(request)
}

func bypassProxy(host string) bool {
	patterns := os.Getenv("NO_PROXY")
	if patterns == "" {
		patterns = os.Getenv("no_proxy")
	}
	if patterns == "" {
		return false
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, pattern := range strings.Split(patterns, ",") {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		pattern = strings.TrimSuffix(pattern, ".")
		if pattern == "" {
			continue
		}
		if pattern == "*" || host == pattern {
			return true
		}
		if strings.HasPrefix(pattern, ".") && strings.HasSuffix(host, pattern) {
			return true
		}
		if strings.HasSuffix(host, "."+pattern) {
			return true
		}
	}
	return false
}
