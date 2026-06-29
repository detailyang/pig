package ai

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadGoogleVertexServiceAccountParsesMinimalJSON(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "adc-*.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString(`{"client_email":"svc@proj.iam.gserviceaccount.com","private_key":"KEY","project_id":"proj"}`)
	_ = file.Close()

	account, err := LoadGoogleVertexServiceAccount(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if account.ClientEmail != "svc@proj.iam.gserviceaccount.com" || account.PrivateKey != "KEY" || account.TokenURI != "https://oauth2.googleapis.com/token" || account.ProjectID == nil || *account.ProjectID != "proj" {
		t.Fatalf("account mismatch: %#v", account)
	}
	alias, err := LoadServiceAccount(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	var _ ServiceAccount = alias
	if alias.ClientEmail != account.ClientEmail || alias.PrivateKey != account.PrivateKey || alias.TokenURI != account.TokenURI {
		t.Fatalf("upstream service account alias mismatch: %#v want %#v", alias, account)
	}
}

func TestLoadGoogleVertexServiceAccountUsesEnvPath(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "adc-*.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString(`{"client_email":"svc@proj.iam.gserviceaccount.com","private_key":"KEY","token_uri":"https://token.example.test"}`)
	_ = file.Close()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", file.Name())

	account, err := LoadGoogleVertexServiceAccount("")
	if err != nil {
		t.Fatal(err)
	}
	if account.TokenURI != "https://token.example.test" {
		t.Fatalf("token uri = %q", account.TokenURI)
	}
}

func TestLoadGoogleVertexServiceAccountRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	base := map[string]any{
		"client_email": "svc@proj.iam.gserviceaccount.com",
		"private_key":  "KEY",
	}
	for _, field := range []string{"client_email", "private_key"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			delete(object, field)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			if account, err := LoadGoogleVertexServiceAccount(writeTempADCFile(t, data)); err == nil {
				t.Fatalf("missing %s should be rejected like upstream serde String: %#v", field, account)
			}
		})
	}
}

func TestLoadGoogleVertexServiceAccountRejectsNullStringFieldsLikeUpstream(t *testing.T) {
	base := map[string]any{
		"client_email": "svc@proj.iam.gserviceaccount.com",
		"private_key":  "KEY",
		"token_uri":    "https://token.example.test",
	}
	for _, field := range []string{"client_email", "private_key", "token_uri"} {
		t.Run(field, func(t *testing.T) {
			object := make(map[string]any, len(base))
			for key, value := range base {
				object[key] = value
			}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			if account, err := LoadGoogleVertexServiceAccount(writeTempADCFile(t, data)); err == nil {
				t.Fatalf("null %s should be rejected like upstream serde String: %#v", field, account)
			}
		})
	}
}

func TestBuildGoogleVertexJWTShapeAndClaims(t *testing.T) {
	account := GoogleVertexServiceAccount{ClientEmail: "svc@proj.iam.gserviceaccount.com", PrivateKey: testGoogleVertexPrivateKey(t), TokenURI: "https://oauth2.googleapis.com/token"}
	jwt, err := BuildGoogleVertexJWT(account, "", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt = %q", jwt)
	}
	var header map[string]any
	decodeJWTPart(t, parts[0], &header)
	if header["alg"] != "RS256" || header["typ"] != "JWT" {
		t.Fatalf("header = %#v", header)
	}
	var claims map[string]any
	decodeJWTPart(t, parts[1], &claims)
	if claims["iss"] != "svc@proj.iam.gserviceaccount.com" || claims["aud"] != "https://oauth2.googleapis.com/token" || claims["scope"] != "https://www.googleapis.com/auth/cloud-platform" || claims["iat"] != float64(1700000000) || claims["exp"] != float64(1700003600) {
		t.Fatalf("claims = %#v", claims)
	}
	if parts[2] == "" {
		t.Fatal("expected signature")
	}
	aliasJWT, err := BuildJWT(account, "", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(aliasJWT, ".")) != 3 {
		t.Fatalf("upstream build_jwt wrapper returned invalid jwt: %q", aliasJWT)
	}
}

func TestFetchGoogleVertexAccessTokenPostsJWTBearerForm(t *testing.T) {
	var formGrantType string
	var assertion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got, want := r.Header.Get("Content-Type"), "application/x-www-form-urlencoded"; !strings.HasPrefix(got, want) {
			t.Fatalf("content-type = %q want prefix %q", got, want)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		formGrantType = r.PostForm.Get("grant_type")
		assertion = r.PostForm.Get("assertion")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "vertex-access", "expires_in": 120, "scope": "scope-a"})
	}))
	defer server.Close()

	account := GoogleVertexServiceAccount{ClientEmail: "svc@proj.iam.gserviceaccount.com", PrivateKey: testGoogleVertexPrivateKey(t), TokenURI: server.URL}
	token, err := FetchGoogleVertexAccessToken(context.Background(), GoogleVertexADCOptions{HTTPClient: server.Client(), Now: func() time.Time { return time.Unix(1700000000, 0) }}, account, "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if formGrantType != "urn:ietf:params:oauth:grant-type:jwt-bearer" || assertion == "" {
		t.Fatalf("form grant=%q assertion empty=%v", formGrantType, assertion == "")
	}
	if token.Token != "vertex-access" || token.ExpiresAt != 1700000120 || token.Scope == nil || *token.Scope != "scope-a" {
		t.Fatalf("token = %#v", token)
	}
	var _ AccessToken = token
	aliasToken, err := FetchAccessToken(context.Background(), GoogleVertexADCOptions{HTTPClient: server.Client(), Now: func() time.Time { return time.Unix(1700000000, 0) }}, account, "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if aliasToken.Token != "vertex-access" || aliasToken.ExpiresAt != 1700000120 || aliasToken.Scope == nil || *aliasToken.Scope != "scope-a" {
		t.Fatalf("upstream fetch_access_token wrapper mismatch: %#v", aliasToken)
	}
}

func TestFetchGoogleVertexAccessTokenDefaultsExpiresIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "vertex-access"})
	}))
	defer server.Close()

	account := GoogleVertexServiceAccount{ClientEmail: "svc@proj.iam.gserviceaccount.com", PrivateKey: testGoogleVertexPrivateKey(t), TokenURI: server.URL}
	token, err := FetchGoogleVertexAccessToken(context.Background(), GoogleVertexADCOptions{HTTPClient: server.Client(), Now: func() time.Time { return time.Unix(1700000000, 0) }}, account, "")
	if err != nil {
		t.Fatal(err)
	}
	if token.ExpiresAt != 1700003600 {
		t.Fatalf("expires_at = %d", token.ExpiresAt)
	}
}

func decodeJWTPart(t *testing.T, raw string, out any) {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
}

func testGoogleVertexPrivateKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}))
}

func writeTempADCFile(t *testing.T, data []byte) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "adc-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}
