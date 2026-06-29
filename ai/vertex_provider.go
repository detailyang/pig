package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type GoogleVertexCreds struct {
	Project     string
	Location    string
	AccessToken *string
	APIKey      *string
}

type VertexCreds = GoogleVertexCreds

type VertexErrorKind string

const (
	VertexErrorOther    VertexErrorKind = "other"
	VertexErrorExchange VertexErrorKind = "exchange"
)

type VertexError struct {
	Kind    VertexErrorKind
	Message string
	Err     error
}

func (err VertexError) Error() string {
	if err.Message != "" {
		return err.Message
	}
	if err.Err != nil {
		return err.Err.Error()
	}
	return "vertex error"
}

func (err VertexError) Unwrap() error { return err.Err }

func VertexErrorNetwork(message string) VertexError {
	return VertexError{Kind: VertexErrorExchange, Message: "network error: " + message}
}

func envFirst(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func optionalEnvString(name string) *string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil
	}
	return &value
}

func GoogleVertexCredsFromEnv() (GoogleVertexCreds, bool) {
	project := envFirst("GOOGLE_CLOUD_PROJECT", "GCLOUD_PROJECT")
	if project == "" {
		return GoogleVertexCreds{}, false
	}
	location := envFirst("GOOGLE_CLOUD_LOCATION")
	if location == "" {
		location = "us-central1"
	}
	apiKey := optionalEnvString("GOOGLE_API_KEY")
	if apiKey == nil {
		return GoogleVertexCreds{}, false
	}
	return GoogleVertexCreds{Project: project, Location: location, APIKey: apiKey}, true
}

func VertexCredsFromEnv() (VertexCreds, bool) {
	return GoogleVertexCredsFromEnv()
}

func (VertexCreds) FromEnv() (VertexCreds, bool) {
	return VertexCredsFromEnv()
}

func InvokeGoogleVertex(ctx context.Context, client *http.Client, baseURL string, creds GoogleVertexCreds, publisher string, modelID string, op string, body any) (map[string]any, error) {
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	if baseURL == "" {
		baseURL = GoogleVertexHost(creds.Location)
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/v1/projects/" + creds.Project + "/locations/" + creds.Location + "/publishers/" + publisher + "/models/" + modelID + ":" + op
	if creds.APIKey != nil {
		endpoint += "?key=" + *creds.APIKey
	}
	payload, err := marshalJSONNoHTMLEscape(body)
	if err != nil {
		return nil, VertexError{Kind: VertexErrorOther, Err: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, VertexError{Kind: VertexErrorOther, Err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	if creds.AccessToken != nil {
		request.Header.Set("Authorization", "Bearer "+*creds.AccessToken)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, VertexError{Kind: VertexErrorExchange, Message: fmt.Sprintf("network error: %s", err), Err: err}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, VertexError{Kind: VertexErrorExchange, Message: fmt.Sprintf("network error: %s", err), Err: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, VertexError{Kind: VertexErrorExchange, Message: fmt.Sprintf("HTTP %d: %s", response.StatusCode, truncateRunes(string(responseBody), 500))}
	}
	var parsed map[string]any
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return nil, VertexError{Kind: VertexErrorOther, Message: fmt.Sprintf("parse json body: %s", err), Err: err}
	}
	return parsed, nil
}

func InvokeVertex(ctx context.Context, client *http.Client, baseURL string, creds VertexCreds, publisher string, modelID string, op string, body any) (map[string]any, error) {
	return InvokeGoogleVertex(ctx, client, baseURL, GoogleVertexCreds(creds), publisher, modelID, op, body)
}
