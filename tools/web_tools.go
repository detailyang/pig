package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

const MaxWebFetchBodyBytes = 5 * 1024 * 1024

const webToolTimeout = 15 * time.Second

const upstreamWebUserAgent = "pie/0.75.0"

type WebFetchTool struct {
	Client *http.Client
}

func (WebFetchTool) Name() string { return "web_fetch" }
func (WebFetchTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (WebFetchTool) Description() string {
	return "Fetch a URL via HTTP GET. Returns headers + body. For HTML pages, tags are stripped to plain text. Body cap 5 MiB; 15s timeout."
}
func (WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{"type": "string", "description": "Absolute http(s) URL to fetch."},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}
func (tool WebFetchTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	rawURL, err := requiredToolArg(call, "url")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(rawURL) {
		return agent.ToolResult{}, fmt.Errorf("url must be valid UTF-8")
	}
	if rawURL == "" {
		return agent.ToolResult{}, fmt.Errorf("fetch failed: builder error: relative URL without a base")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" {
		return agent.ToolResult{}, fmt.Errorf("fetch failed: builder error: relative URL without a base")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return agent.ToolResult{}, fmt.Errorf("fetch failed: builder error for url (%s)", rawURL)
	}
	if parsed.Host == "" {
		return agent.ToolResult{}, fmt.Errorf("fetch failed: builder error")
	}
	client := tool.Client
	if client == nil {
		client = &http.Client{Timeout: webToolTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return agent.ToolResult{}, err
	}
	req.Header.Set("User-Agent", upstreamWebUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return agent.ToolResult{}, fmt.Errorf("cancelled")
		}
		return agent.ToolResult{}, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, MaxWebFetchBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		if ctx.Err() != nil {
			return agent.ToolResult{}, fmt.Errorf("cancelled")
		}
		return agent.ToolResult{}, fmt.Errorf("read body: %w", err)
	}
	truncated := false
	if len(data) > MaxWebFetchBodyBytes {
		data = data[:MaxWebFetchBodyBytes]
		truncated = true
	}
	contentType := resp.Header.Get("Content-Type")
	text := strings.ToValidUTF8(string(data), "�")
	if strings.Contains(contentType, "html") {
		text = htmlToText(text)
	}
	truncatedSuffix := ""
	if truncated {
		truncatedSuffix = " (truncated)"
	}
	content := fmt.Sprintf("GET %s\nstatus: %s\ncontent-type: %s\nbytes: %d%s\n\n%s", rawURL, resp.Status, contentType, len(data), truncatedSuffix, text)
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: content, Details: map[string]any{"url": rawURL, "status": resp.StatusCode, "content_type": contentType, "bytes": len(data), "truncated": truncated}}, nil
}

type WebSearchOptions struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

type WebSearchTool struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func NewWebSearchTool(options WebSearchOptions) WebSearchTool {
	baseURL := options.BaseURL
	if baseURL == "" {
		baseURL = "https://api.search.brave.com/res/v1/web/search"
	}
	return WebSearchTool{BaseURL: baseURL, APIKey: options.APIKey, Client: options.Client}
}

func (tool WebSearchTool) WithBaseURL(baseURL string) WebSearchTool {
	tool.BaseURL = baseURL
	return tool
}

func (tool WebSearchTool) WithBaseUrl(baseURL string) WebSearchTool { return tool.WithBaseURL(baseURL) }

func (WebSearchTool) Name() string { return "web_search" }
func (WebSearchTool) ExecutionMode() agent.ToolExecutionMode {
	return agent.ToolExecutionParallel
}
func (WebSearchTool) Description() string {
	return "Search the web. v1 backend: Brave Search. Requires BRAVE_SEARCH_API_KEY env var. Returns ranked results with title, URL, and description."
}
func (WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Free-text search query."},
			"count": map[string]any{"type": "integer", "description": "How many results to request (1-20, default 10)."},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}
func (tool WebSearchTool) Execute(ctx context.Context, call ai.ToolCall, update agent.ToolUpdateFunc) (agent.ToolResult, error) {
	query, err := requiredToolArg(call, "query")
	if err != nil {
		return agent.ToolResult{}, err
	}
	if !utf8.ValidString(query) {
		return agent.ToolResult{}, fmt.Errorf("query must be valid UTF-8")
	}
	apiKey := tool.APIKey
	if apiKey == "" {
		var ok bool
		apiKey, ok = os.LookupEnv("BRAVE_SEARCH_API_KEY")
		if !ok {
			return agent.ToolResult{}, fmt.Errorf("web_search backend not configured: set BRAVE_SEARCH_API_KEY env var")
		}
	}
	count := webSearchCountArg(call, "count", 10)
	client := tool.Client
	if client == nil {
		client = &http.Client{Timeout: webToolTimeout}
	}
	endpoint, err := url.Parse(tool.BaseURL)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("search base url: %w", err)
	}
	endpoint.RawQuery = appendRawQuery(endpoint.RawQuery, "q", query)
	endpoint.RawQuery = appendRawQuery(endpoint.RawQuery, "count", fmt.Sprintf("%d", count))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return agent.ToolResult{}, err
	}
	req.Header.Set("X-Subscription-Token", apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", upstreamWebUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return agent.ToolResult{}, fmt.Errorf("cancelled")
		}
		return agent.ToolResult{}, fmt.Errorf("search failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("read body: %w", err)
	}
	text := strings.ToValidUTF8(string(data), "�")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return agent.ToolResult{}, fmt.Errorf("search backend status %s: %s", resp.Status, takeRunes(text, 500))
	}
	var parsed braveResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return agent.ToolResult{}, fmt.Errorf("parse response: %w", err)
	}
	results := parsed.Web.Results
	if len(results) == 0 {
		return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: fmt.Sprintf("no results for query: %s", query), Details: map[string]any{"query": query, "results": 0}}, nil
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("web_search %q — top %d of %d:\n\n", query, len(results), count))
	for index, result := range results {
		title := result.Title
		if title == "" {
			title = "(no title)"
		}
		resultURL := result.URL
		if resultURL == "" {
			resultURL = "(no url)"
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n   %s\n", index+1, title, resultURL))
		if result.Description != "" {
			builder.WriteString("   ")
			builder.WriteString(result.Description)
			builder.WriteByte('\n')
		}
		builder.WriteByte('\n')
	}
	return agent.ToolResult{CallID: call.ID, Name: call.Name, Content: builder.String(), Details: map[string]any{"query": query, "results": len(results)}}, nil
}

func webSearchCountArg(call ai.ToolCall, key string, fallback int) int {
	value, ok := call.Arguments[key]
	if !ok {
		return fallback
	}
	var count uint64
	switch typed := value.(type) {
	case int:
		if typed >= 0 {
			count = uint64(typed)
		} else {
			return fallback
		}
	case int64:
		if typed >= 0 {
			count = uint64(typed)
		} else {
			return fallback
		}
	case uint64:
		count = typed
	case json.Number:
		parsed, err := strconv.ParseUint(typed.String(), 10, 64)
		if err != nil {
			return fallback
		}
		count = parsed
	default:
		return fallback
	}
	if count < 1 {
		return 1
	}
	if count > 20 {
		return 20
	}
	return int(count)
}

func appendRawQuery(rawQuery, key, value string) string {
	param := url.QueryEscape(key) + "=" + url.QueryEscape(value)
	if rawQuery == "" {
		return param
	}
	return rawQuery + "&" + param
}

type braveResponse struct {
	Web struct {
		Results nonNullBraveResults `json:"results"`
	} `json:"web"`
}

func (response *braveResponse) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return fmt.Errorf("invalid type: null, expected struct BraveResponse")
	}
	type rawBraveResponse braveResponse
	var parsed rawBraveResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*response = braveResponse(parsed)
	return nil
}

type nonNullBraveResults []braveResult

func (results *nonNullBraveResults) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return fmt.Errorf("invalid type: null, expected a sequence")
	}
	var parsed []braveResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*results = parsed
	return nil
}

type braveResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

func htmlToText(input string) string {
	var builder strings.Builder
	lower := asciiLower(input)
	inTag := false
	skipUntil := ""
	for index := 0; index < len(input); {
		if skipUntil != "" {
			if strings.HasPrefix(lower[index:], skipUntil) {
				index += len(skipUntil)
				skipUntil = ""
				continue
			}
			r, size := utf8.DecodeRuneInString(input[index:])
			index += size
			_ = r
			continue
		}
		if !inTag && strings.HasPrefix(lower[index:], "<script") {
			skipUntil = "</script>"
			index += len("<script")
			continue
		}
		if !inTag && strings.HasPrefix(lower[index:], "<style") {
			skipUntil = "</style>"
			index += len("<style")
			continue
		}
		r, size := utf8.DecodeRuneInString(input[index:])
		index += size
		if r == '<' {
			inTag = true
			if isHTMLBlockBoundary(lower[index-size:]) {
				builder.WriteByte('\n')
			}
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			builder.WriteRune(r)
		}
	}
	text := builder.String()
	text = decodeUpstreamHTMLEntities(text)
	return collapseHTMLWhitespace(text)
}

func asciiLower(input string) string {
	bytes := []byte(input)
	for index, char := range bytes {
		if char >= 'A' && char <= 'Z' {
			bytes[index] = char + ('a' - 'A')
		}
	}
	return string(bytes)
}

func isHTMLBlockBoundary(text string) bool {
	for _, prefix := range []string{"<br", "<p", "</p", "<div", "</div", "<li", "</li", "<h"} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func decodeUpstreamHTMLEntities(text string) string {
	return strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&nbsp;", " ",
	).Replace(text)
}

func takeRunes(text string, limit int) string {
	if limit < 0 {
		limit = 0
	}
	count := 0
	for index := range text {
		if count == limit {
			return text[:index]
		}
		count++
	}
	return text
}

func collapseHTMLWhitespace(text string) string {
	var builder strings.Builder
	lastWasSpace := false
	endsWithNewline := false
	consecutiveNewlines := 0
	for _, char := range text {
		if char == '\n' {
			consecutiveNewlines++
			if consecutiveNewlines <= 2 {
				builder.WriteByte('\n')
			}
			lastWasSpace = false
			endsWithNewline = true
			continue
		}
		if unicode.IsSpace(char) {
			if !lastWasSpace && !endsWithNewline {
				builder.WriteByte(' ')
				lastWasSpace = true
				consecutiveNewlines = 0
				endsWithNewline = false
			}
			continue
		}
		consecutiveNewlines = 0
		lastWasSpace = false
		endsWithNewline = false
		builder.WriteRune(char)
	}
	return strings.TrimSpace(builder.String())
}
