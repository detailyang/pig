package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleVertexCredsFromEnvRequiresAPIKeyAuth(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "")
	t.Setenv("GOOGLE_API_KEY", "")
	if _, ok := GoogleVertexCredsFromEnv(); ok {
		t.Fatal("expected no creds without project")
	}

	t.Setenv("GOOGLE_CLOUD_PROJECT", "project-a")
	if _, ok := GoogleVertexCredsFromEnv(); ok {
		t.Fatal("expected no creds without auth")
	}

}

func TestGoogleVertexCredsFromEnvFallbacksAndAPIKey(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("GCLOUD_PROJECT", "gcloud-project")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "europe-west4")
	t.Setenv("GOOGLE_API_KEY", "api-key")
	creds, ok := GoogleVertexCredsFromEnv()
	if !ok || creds.Project != "gcloud-project" || creds.Location != "europe-west4" || creds.APIKey == nil || *creds.APIKey != "api-key" || creds.AccessToken != nil {
		t.Fatalf("creds = %#v ok=%v", creds, ok)
	}
}

func TestGoogleVertexCredsFromEnvPreservesProjectAndLocationWhitespaceLikeUpstream(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", " project-a ")
	t.Setenv("GCLOUD_PROJECT", "")
	t.Setenv("GOOGLE_CLOUD_LOCATION", " europe-west4 ")
	t.Setenv("GOOGLE_API_KEY", " api-key ")

	creds, ok := GoogleVertexCredsFromEnv()
	if !ok || creds.Project != " project-a " || creds.Location != " europe-west4 " || creds.APIKey == nil || *creds.APIKey != "api-key" {
		t.Fatalf("creds = %#v ok=%v", creds, ok)
	}
}

func TestInvokeGoogleVertexSendsBearerAndParsesJSON(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/project-a/locations/us-central1/publishers/google/models/gemini-test:generateContent" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer access-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("headers auth=%q content-type=%q", r.Header.Get("Authorization"), r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	accessToken := "access-token"
	response, err := InvokeGoogleVertex(context.Background(), server.Client(), server.URL, GoogleVertexCreds{Project: "project-a", Location: "us-central1", AccessToken: &accessToken}, "google", "gemini-test", "generateContent", map[string]any{"prompt": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if response["ok"] != true || body["prompt"] != "hi" {
		t.Fatalf("response=%#v body=%#v", response, body)
	}
}

func TestInvokeGoogleVertexAddsAPIKeyQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "api-key" {
			t.Fatalf("query = %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	apiKey := "api-key"
	if _, err := InvokeGoogleVertex(context.Background(), server.Client(), server.URL, GoogleVertexCreds{Project: "project-a", Location: "global", APIKey: &apiKey}, "google", "gemini-test", "rawPredict", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

func TestVertexCredsAliasesMatchUpstreamNames(t *testing.T) {
	t.Setenv("GOOGLE_CLOUD_PROJECT", "project-a")
	t.Setenv("GOOGLE_CLOUD_LOCATION", "asia-east1")
	t.Setenv("GOOGLE_API_KEY", "api-key")

	creds, ok := (VertexCreds{}).FromEnv()
	if !ok || creds.Project != "project-a" || creds.Location != "asia-east1" || creds.APIKey == nil || *creds.APIKey != "api-key" {
		t.Fatalf("creds = %#v ok=%v", creds, ok)
	}
}

func TestInvokeVertexAliasMatchesUpstreamName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/project-a/locations/us-central1/publishers/google/models/gemini-test:generateContent" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	accessToken := "access-token"
	response, err := InvokeVertex(context.Background(), server.Client(), server.URL, VertexCreds{Project: "project-a", Location: "us-central1", AccessToken: &accessToken}, "google", "gemini-test", "generateContent", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if response["ok"] != true {
		t.Fatalf("response = %#v", response)
	}
}

func TestInvokeVertexStatusErrorTruncatesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("x", 600)))
	}))
	defer server.Close()

	accessToken := "access-token"
	_, err := InvokeVertex(context.Background(), server.Client(), server.URL, VertexCreds{Project: "project-a", Location: "us-central1", AccessToken: &accessToken}, "google", "gemini-test", "generateContent", map[string]any{})
	if err == nil {
		t.Fatal("expected status error")
	}
	if got := err.Error(); !strings.HasPrefix(got, "HTTP 502: ") || len([]rune(strings.TrimPrefix(got, "HTTP 502: "))) != 500 {
		t.Fatalf("status error mismatch: %q", got)
	}
	var vertexErr VertexError
	if !errors.As(err, &vertexErr) || vertexErr.Kind != VertexErrorExchange {
		t.Fatalf("expected VertexErrorExchange, got %#v", err)
	}
}
