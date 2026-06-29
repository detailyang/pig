package tools

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/ai"
)

func TestWebFetchToolFetchesAndStripsHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if got := r.Header.Get("User-Agent"); got != upstreamWebUserAgent {
			t.Fatalf("unexpected user agent %q", got)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><title>ignored</title><script>bad()</script></head><body><h1>Hello</h1><p>Go&nbsp;port</p></body></html>"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "GET "+server.URL) || !strings.Contains(result.Content, "status: 200 OK") || !strings.Contains(result.Content, "Hello") || !strings.Contains(result.Content, "Go port") {
		t.Fatalf("fetch mismatch: %q", result.Content)
	}
	if strings.Contains(result.Content, "bad()") || strings.Contains(result.Content, "<h1>") {
		t.Fatalf("html should be stripped: %q", result.Content)
	}
	if result.Details["url"] != server.URL || result.Details["status"] != 200 || result.Details["content_type"] != "text/html" || result.Details["bytes"] != 116 || result.Details["truncated"] != false {
		t.Fatalf("fetch details mismatch: %#v", result.Details)
	}
}

func TestWebFetchToolReportsOriginalURLString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	originalURL := "HTTP" + strings.TrimPrefix(server.URL, "http") + "/%7Euser?q=a%2fb#section"
	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": originalURL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Content, "GET "+originalURL+"\n") || result.Details["url"] != originalURL {
		t.Fatalf("web_fetch should report original URL, content=%q details=%#v", result.Content, result.Details)
	}
}

func TestWebToolDefinitionsMatchUpstream(t *testing.T) {
	fetch := WebFetchTool{}
	wantFetchDescription := "Fetch a URL via HTTP GET. Returns headers + body. For HTML pages, tags are stripped to plain text. Body cap 5 MiB; 15s timeout."
	if fetch.Description() != wantFetchDescription {
		t.Fatalf("web_fetch description mismatch:\nwant: %q\n got: %q", wantFetchDescription, fetch.Description())
	}
	fetchProperties := fetch.Parameters()["properties"].(map[string]any)
	fetchURL := fetchProperties["url"].(map[string]any)
	if fetchURL["type"] != "string" || fetchURL["description"] != "Absolute http(s) URL to fetch." {
		t.Fatalf("web_fetch url property mismatch: %#v", fetchURL)
	}

	search := NewWebSearchTool(WebSearchOptions{})
	if search.WithBaseUrl("http://example.test/search").BaseURL != "http://example.test/search" {
		t.Fatalf("web_search WithBaseUrl alias mismatch")
	}
	wantSearchDescription := "Search the web. v1 backend: Brave Search. Requires BRAVE_SEARCH_API_KEY env var. Returns ranked results with title, URL, and description."
	if search.Description() != wantSearchDescription {
		t.Fatalf("web_search description mismatch:\nwant: %q\n got: %q", wantSearchDescription, search.Description())
	}
	searchProperties := search.Parameters()["properties"].(map[string]any)
	query := searchProperties["query"].(map[string]any)
	if query["type"] != "string" || query["description"] != "Free-text search query." {
		t.Fatalf("web_search query property mismatch: %#v", query)
	}
	count := searchProperties["count"].(map[string]any)
	if count["type"] != "integer" || count["description"] != "How many results to request (1-20, default 10)." {
		t.Fatalf("web_search count property mismatch: %#v", count)
	}
}

func TestWebFetchToolPreservesHTMLListBoundaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<ul><li>Alpha</li><li>Beta</li></ul>"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Alpha\n\nBeta") {
		t.Fatalf("list boundaries should be preserved: %q", result.Content)
	}
}

func TestWebFetchToolPreservesParagraphBlankLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>First</p>\n   <p>Second</p>"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "First\n\nSecond") || strings.Contains(result.Content, "First\n\n\nSecond") {
		t.Fatalf("paragraph whitespace mismatch: %q", result.Content)
	}
}

func TestWebFetchToolIndentedParagraphsHaveSingleBlankLineBetweenLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>\n   <p>para1</p>\n   <p>para2</p>\n   <p>para3</p>\n</body></html>"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"para1", "para2", "para3"} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("missing paragraph %q in %q", want, result.Content)
		}
	}
	if strings.Contains(result.Content, "\n\n\n") {
		t.Fatalf("must not produce more than one blank line between paragraphs: %q", result.Content)
	}
	if !strings.Contains(result.Content, "para1\n\npara2\n\npara3") {
		t.Fatalf("paragraph spacing mismatch: %q", result.Content)
	}
}

func TestWebFetchToolKeepsTitleTextLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><title>Doc Title</title></head><body>Body</body></html>"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Doc Title") || !strings.Contains(result.Content, "Body") {
		t.Fatalf("title text should be preserved: %q", result.Content)
	}
}

func TestWebFetchToolSkipsUnclosedScriptLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("Visible<script>hidden text"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content, "hidden text") || !strings.Contains(result.Content, "Visible") {
		t.Fatalf("unclosed script should be skipped: %q", result.Content)
	}
}

func TestWebFetchToolOnlyDecodesUpstreamEntitySubset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>A&amp;B &copy; &#39;quote&#39;</p>"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "A&B &copy; 'quote'") || strings.Contains(result.Content, "©") {
		t.Fatalf("entity decode subset mismatch: %q", result.Content)
	}
}

func TestWebFetchToolCollapsesUnicodeWhitespaceLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>Hello\u2003world</p>"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Hello world") || strings.Contains(result.Content, "Hello\u2003world") {
		t.Fatalf("unicode whitespace should collapse like Rust char::is_whitespace, got %q", result.Content)
	}
}

func TestWebFetchToolOnlyStripsHTMLContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("literal <tag>keep me</tag> &amp; entity"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "literal <tag>keep me</tag> &amp; entity") {
		t.Fatalf("plain text body should not be html-stripped: %q", result.Content)
	}
}

func TestWebFetchToolLossyDecodesInvalidUTF8LikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte{'o', 'k', ' ', 0xff, '!'})
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "ok �!") || strings.Contains(result.Content, string([]byte{0xff})) {
		t.Fatalf("invalid UTF-8 should be lossy-decoded like upstream, got %q", result.Content)
	}
}

func TestWebFetchToolHTMLLowercasePreservesByteIndexesLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>K</p><script>hidden</script><p>done</p></body></html>"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "K") || !strings.Contains(result.Content, "done") || strings.Contains(result.Content, "hidden") {
		t.Fatalf("html lowercasing should preserve upstream byte indexes, got %q", result.Content)
	}
}

func TestWebFetchToolHTMLContentTypeMatchIsCaseSensitive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/HTML")
		_, _ = w.Write([]byte("<p>Keep Tags</p>"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "<p>Keep Tags</p>") || strings.Contains(result.Content, "\n\nKeep Tags") {
		t.Fatalf("uppercase HTML content-type should not be stripped: %q", result.Content)
	}
}

func TestWebFetchToolRejectsBadSchemeAndTruncatesLargeBody(t *testing.T) {
	if _, err := (WebFetchTool{}).Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": "file:///etc/passwd"}}, nil); err == nil || err.Error() != "fetch failed: builder error for url (file:///etc/passwd)" {
		t.Fatalf("expected scheme error, got %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", MaxWebFetchBodyBytes+1)))
	}))
	defer server.Close()
	result, err := (WebFetchTool{}).Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["bytes"] != MaxWebFetchBodyBytes || result.Details["truncated"] != true || !strings.Contains(result.Content, "bytes: 5242880 (truncated)") {
		t.Fatalf("expected truncated body result, got details=%#v content prefix=%q", result.Details, result.Content[:min(len(result.Content), 80)])
	}
}

func TestWebFetchToolMalformedHTTPURLMatchesUpstreamBuilderError(t *testing.T) {
	_, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": "http://"}}, nil)
	if err == nil || err.Error() != "fetch failed: builder error" {
		t.Fatalf("expected upstream malformed URL builder error, got %v", err)
	}
}

func TestWebFetchToolAllowsEmptyURLThroughFetchError(t *testing.T) {
	_, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": ""}}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "fetch failed:") || strings.Contains(err.Error(), "empty url") {
		t.Fatalf("expected fetch failed error, got %v", err)
	}
}

func TestWebFetchToolRelativeURLMatchesUpstreamBuilderError(t *testing.T) {
	_, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": "example.com/path"}}, nil)
	if err == nil || err.Error() != "fetch failed: builder error: relative URL without a base" {
		t.Fatalf("expected upstream relative URL builder error, got %v", err)
	}
}

func TestWebFetchToolNonStringURLReportsMissing(t *testing.T) {
	_, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": 123}}, nil)
	if err == nil || err.Error() != "missing required arg: url" {
		t.Fatalf("expected upstream-style non-string url error, got %v", err)
	}
}

func TestWebFetchToolRejectsInvalidUTF8URL(t *testing.T) {
	_, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "url must be valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 url error, got %v", err)
	}
}

func TestWebFetchToolCancelledContextReturnsCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := WebFetchTool{}.Execute(ctx, ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err == nil || err.Error() != "cancelled" {
		t.Fatalf("expected cancelled error, got %v", err)
	}
}

func TestWebFetchToolCancelledWhileReadingBodyReturnsCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := WebFetchTool{}.Execute(ctx, ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err == nil || err.Error() != "cancelled" {
		t.Fatalf("expected cancelled body read error, got %v", err)
	}
}

func TestWebFetchToolReturnsHTTPErrorStatusBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found body"))
	}))
	defer server.Close()

	result, err := WebFetchTool{}.Execute(context.Background(), ai.ToolCall{Name: "web_fetch", Arguments: map[string]any{"url": server.URL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "status: 404") || !strings.Contains(result.Content, "not found body") || result.Details["status"] != 404 {
		t.Fatalf("fetch error status mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestWebSearchToolParsesBraveResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Subscription-Token"); got != "test-key" {
			t.Fatalf("missing api key header: %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != upstreamWebUserAgent {
			t.Fatalf("unexpected user agent %q", got)
		}
		if got := r.URL.Query().Get("q"); got != "golang agents" {
			t.Fatalf("query mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"One","url":"https://example.com/1","description":"First"},{"title":"Two","url":"https://example.com/2"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang agents", "count": 2}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "web_search \"golang agents\"") || !strings.Contains(result.Content, "1. One") || !strings.Contains(result.Content, "https://example.com/2") {
		t.Fatalf("search mismatch: %q", result.Content)
	}
	if result.Details["query"] != "golang agents" || result.Details["results"] != 2 {
		t.Fatalf("search details mismatch: %#v", result.Details)
	}
}

func TestWebSearchToolAppendsQueryParamsLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.RawQuery; got != "token=abc&q=old&count=99&q=golang&count=2" {
			t.Fatalf("raw query mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"One","url":"https://example.com/1"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL + "?token=abc&q=old&count=99", APIKey: "test-key"})
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang", "count": 2}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWebSearchToolReadsAPIKeyEnvAtExecuteTime(t *testing.T) {
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Subscription-Token"); got != "late-key" {
			t.Fatalf("api key header mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"One","url":"https://example.com/1"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL})
	t.Setenv("BRAVE_SEARCH_API_KEY", "late-key")
	if _, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWebSearchToolAllowsEmptyEnvAPIKeyLikeUpstream(t *testing.T) {
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Subscription-Token"); got != "" {
			t.Fatalf("api key header mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"One","url":"https://example.com/1"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}, nil)
	if err != nil || !strings.Contains(result.Content, "One") {
		t.Fatalf("empty env api key should still be sent like upstream result=%#v err=%v", result, err)
	}
}

func TestWebSearchToolAllowsEmptyQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "" {
			t.Fatalf("query mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"Empty","url":"https://example.com/empty"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": ""}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "web_search \"\" — top 1 of 10") || result.Details["query"] != "" || result.Details["results"] != 1 {
		t.Fatalf("empty query search mismatch: content=%q details=%#v", result.Content, result.Details)
	}
}

func TestWebSearchToolNonStringQueryReportsMissing(t *testing.T) {
	tool := NewWebSearchTool(WebSearchOptions{BaseURL: "http://127.0.0.1", APIKey: "test-key"})
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": 123}}, nil)
	if err == nil || err.Error() != "missing required arg: query" {
		t.Fatalf("expected upstream-style non-string query error, got %v", err)
	}
}

func TestWebSearchToolRejectsInvalidUTF8Query(t *testing.T) {
	tool := NewWebSearchTool(WebSearchOptions{BaseURL: "http://127.0.0.1", APIKey: "test-key"})
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": string([]byte{0xff})}}, nil)
	if err == nil || err.Error() != "query must be valid UTF-8" {
		t.Fatalf("expected invalid UTF-8 query error, got %v", err)
	}
}

func TestWebSearchToolCountZeroClampsToOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("count"); got != "1" {
			t.Fatalf("count mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"One","url":"https://example.com/1"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang", "count": 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "web_search \"golang\" — top 1 of 1") {
		t.Fatalf("count clamp search mismatch: %q", result.Content)
	}
}

func TestWebSearchToolAcceptsJSONNumberCountLikeSerdeValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("count"); got != "2" {
			t.Fatalf("count mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"One","url":"https://example.com/1"},{"title":"Two","url":"https://example.com/2"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang", "count": json.Number("2")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "web_search \"golang\" — top 2 of 2") {
		t.Fatalf("json.Number count search mismatch: %q", result.Content)
	}
}

func TestWebSearchToolLargeUnsignedCountClampsLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("count"); got != "20" {
			t.Fatalf("count mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"One","url":"https://example.com/1"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang", "count": uint64(math.MaxUint64)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "web_search \"golang\" — top 1 of 20") {
		t.Fatalf("large unsigned count should clamp to 20 like upstream, got %q", result.Content)
	}
}

func TestWebSearchToolNegativeCountFallsBackToDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("count"); got != "10" {
			t.Fatalf("count mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"One","url":"https://example.com/1"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang", "count": -1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "web_search \"golang\" — top 1 of 10") {
		t.Fatalf("negative count search mismatch: %q", result.Content)
	}
}

func TestWebSearchToolFloatCountFallsBackToDefaultLikeSerdeValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("count"); got != "10" {
			t.Fatalf("count mismatch: %q", got)
		}
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"One","url":"https://example.com/1"}]}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang", "count": 2.0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "web_search \"golang\" — top 1 of 10") {
		t.Fatalf("float count should fall back to default like serde_json::Value::as_u64, got %q", result.Content)
	}
}

func TestWebSearchToolCancelledContextReturnsCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	_, err := tool.Execute(ctx, ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}, nil)
	if err == nil || err.Error() != "cancelled" {
		t.Fatalf("expected cancelled error, got %v", err)
	}
}

func TestWebSearchToolNetworkErrorKeepsSearchFailedPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	baseURL := server.URL
	server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: baseURL, APIKey: "test-key"})
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "search failed:") {
		t.Fatalf("expected search failed error, got %v", err)
	}
}

func TestWebSearchToolLossyDecodesErrorBodyLikeUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte{'b', 'a', 'd', ' ', 0xff})
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "bad �") || strings.Contains(err.Error(), string([]byte{0xff})) {
		t.Fatalf("expected lossy-decoded search error body, got %v", err)
	}
}

func TestWebSearchToolRejectsNullResultsLikeUpstreamSerde(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"web":{"results":null}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "parse response:") {
		t.Fatalf("expected serde-style parse response error for null results, got %v", err)
	}
}

func TestWebSearchToolDefaultsMissingResultsLikeUpstreamSerde(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"web":{}}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "no results for query: golang" || result.Details["results"] != 0 {
		t.Fatalf("expected missing results to default empty, got content=%q details=%#v", result.Content, result.Details)
	}
}

func TestWebSearchToolRejectsTopLevelNullLikeUpstreamSerde(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`null`))
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	_, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "parse response:") {
		t.Fatalf("expected serde-style parse response error for top-level null, got %v", err)
	}
}

func TestWebSearchToolDoesNotCapResponseBodyLikeWebFetch(t *testing.T) {
	largeTitle := strings.Repeat("x", MaxWebFetchBodyBytes+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("count"); got != "1" {
			t.Fatalf("count mismatch: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"web": map[string]any{"results": []map[string]string{{"title": largeTitle, "url": "https://example.com/large"}}}})
	}))
	defer server.Close()

	tool := NewWebSearchTool(WebSearchOptions{BaseURL: server.URL, APIKey: "test-key"})
	result, err := tool.Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "large", "count": 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Details["results"] != 1 || !strings.Contains(result.Content, "https://example.com/large") || !strings.Contains(result.Content, strings.Repeat("x", 1024)) {
		t.Fatalf("web_search should parse uncapped large response, content prefix=%q details=%#v", result.Content[:min(len(result.Content), 120)], result.Details)
	}
}

func TestWebSearchToolRequiresBackend(t *testing.T) {
	if _, err := NewWebSearchTool(WebSearchOptions{}).Execute(context.Background(), ai.ToolCall{Name: "web_search", Arguments: map[string]any{"query": "golang"}}, nil); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected not configured error, got %v", err)
	}
}

func TestWebSearchToolWithBaseURLMatchesUpstreamConstructor(t *testing.T) {
	tool := WebSearchTool{}.WithBaseURL("http://127.0.0.1/search")
	if tool.BaseURL != "http://127.0.0.1/search" {
		t.Fatalf("base url mismatch: %#v", tool)
	}
}
