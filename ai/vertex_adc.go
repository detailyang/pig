package ai

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultGoogleVertexADCScope = "https://www.googleapis.com/auth/cloud-platform"

type GoogleVertexServiceAccount struct {
	ClientEmail string  `json:"client_email"`
	PrivateKey  string  `json:"private_key"`
	TokenURI    string  `json:"token_uri"`
	ProjectID   *string `json:"project_id,omitempty"`
}

type ServiceAccount = GoogleVertexServiceAccount

type GoogleVertexAccessToken struct {
	Token     string
	ExpiresAt int64
	Scope     *string
}

type AccessToken = GoogleVertexAccessToken

type AdcError = error

func AdcErrorIo(message string) AdcError {
	return fmt.Errorf("io: %s", message)
}

func AdcErrorExchange(message string) AdcError {
	return fmt.Errorf("token exchange: %s", message)
}

type GoogleVertexADCOptions struct {
	HTTPClient *http.Client
	Now        func() time.Time
}

type googleVertexTokenResponse struct {
	AccessToken string  `json:"access_token"`
	ExpiresIn   *int64  `json:"expires_in"`
	Scope       *string `json:"scope"`
}

func LoadGoogleVertexServiceAccount(path string) (GoogleVertexServiceAccount, error) {
	if path == "" {
		path = os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
		if path == "" {
			return GoogleVertexServiceAccount{}, fmt.Errorf("io: GOOGLE_APPLICATION_CREDENTIALS not set")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return GoogleVertexServiceAccount{}, fmt.Errorf("io: %s: %w", path, err)
	}
	var account GoogleVertexServiceAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return GoogleVertexServiceAccount{}, fmt.Errorf("parse credentials: %w", err)
	}
	if err := validateGoogleVertexServiceAccountJSON(data); err != nil {
		return GoogleVertexServiceAccount{}, fmt.Errorf("parse credentials: %w", err)
	}
	if account.TokenURI == "" {
		account.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return account, nil
}

func LoadServiceAccount(path string) (ServiceAccount, error) {
	return LoadGoogleVertexServiceAccount(path)
}

func validateGoogleVertexServiceAccountJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range []string{"client_email", "private_key"} {
		raw, ok := object[field]
		if !ok {
			return fmt.Errorf("missing field %q", field)
		}
		if isJSONNull(raw) {
			return fmt.Errorf("invalid null for field %q", field)
		}
	}
	if raw, ok := object["token_uri"]; ok && isJSONNull(raw) {
		return fmt.Errorf("invalid null for field %q", "token_uri")
	}
	return nil
}

func BuildGoogleVertexJWT(account GoogleVertexServiceAccount, scope string, now time.Time) (string, error) {
	if scope == "" {
		scope = defaultGoogleVertexADCScope
	}
	iat := now.Unix()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":   account.ClientEmail,
		"scope": scope,
		"aud":   account.TokenURI,
		"iat":   iat,
		"exp":   iat + 3600,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	key, err := parseGoogleVertexPrivateKey(account.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("sign jwt: parse private key: %w", err)
	}
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func BuildJWT(account ServiceAccount, scope string, now time.Time) (string, error) {
	return BuildGoogleVertexJWT(account, scope, now)
}

func BuildJwt(account ServiceAccount, scope string, now time.Time) (string, error) {
	return BuildJWT(account, scope, now)
}

func FetchGoogleVertexAccessToken(ctx context.Context, options GoogleVertexADCOptions, account GoogleVertexServiceAccount, scope string) (GoogleVertexAccessToken, error) {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	jwt, err := BuildGoogleVertexJWT(account, scope, now())
	if err != nil {
		return GoogleVertexAccessToken{}, err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", jwt)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, account.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return GoogleVertexAccessToken{}, fmt.Errorf("token exchange: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return GoogleVertexAccessToken{}, fmt.Errorf("token exchange: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return GoogleVertexAccessToken{}, fmt.Errorf("token exchange: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return GoogleVertexAccessToken{}, fmt.Errorf("token exchange: %s: %s", response.Status, truncateRunes(string(body), 500))
	}
	var parsed googleVertexTokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return GoogleVertexAccessToken{}, fmt.Errorf("token exchange: parse: %w", err)
	}
	expiresIn := int64(3600)
	if parsed.ExpiresIn != nil {
		expiresIn = *parsed.ExpiresIn
	}
	return GoogleVertexAccessToken{Token: parsed.AccessToken, ExpiresAt: now().Unix() + expiresIn, Scope: parsed.Scope}, nil
}

func FetchAccessToken(ctx context.Context, options GoogleVertexADCOptions, account ServiceAccount, scope string) (AccessToken, error) {
	return FetchGoogleVertexAccessToken(ctx, options, account, scope)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func parseGoogleVertexPrivateKey(raw string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an RSA private key")
	}
	return rsaKey, nil
}
