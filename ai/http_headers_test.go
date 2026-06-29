package ai

import (
	"net/http"
	"testing"
)

func TestUserAgentMatchesUpstreamPackage(t *testing.T) {
	if got := UserAgent(); got != "pie-ai-rs/0.75.0" {
		t.Fatalf("user agent mismatch: %q", got)
	}
}

func TestMergeHeadersRightSideWins(t *testing.T) {
	base := map[string]string{"X-Test": "base", "X-Keep": "keep"}
	overrides := map[string]string{"X-Test": "override", "X-New": "new"}
	got := MergeHeaders(base, overrides)
	if got["X-Test"] != "override" || got["X-Keep"] != "keep" || got["X-New"] != "new" {
		t.Fatalf("merged headers mismatch: %#v", got)
	}
	if base["X-Test"] != "base" {
		t.Fatalf("base headers mutated: %#v", base)
	}
}

func TestApplyExtraHeadersAllowsEmptyOverrideLikeUpstreamMerge(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Test", "base")

	applyExtraHeaders(request, map[string]string{"X-Test": ""})

	if values, ok := request.Header["X-Test"]; !ok || len(values) != 1 || values[0] != "" {
		t.Fatalf("empty override should replace existing header like upstream merge, got %#v", request.Header["X-Test"])
	}
}

func TestApplyModelAndOptionHeadersUsesCatalogHeadersWithOptionOverride(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}

	applyModelAndOptionHeaders(request, Model{Headers: map[string]string{"X-Model": "model", "X-Shared": "model"}}, StreamOptions{Headers: map[string]string{"X-Shared": "options"}})

	if request.Header.Get("X-Model") != "model" || request.Header.Get("X-Shared") != "options" {
		t.Fatalf("model/options headers mismatch: %#v", request.Header)
	}
}
