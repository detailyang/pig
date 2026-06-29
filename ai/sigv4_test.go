package ai

import (
	"net/url"
	"strings"
	"testing"
)

func TestSigV4SignDeterministicGet(t *testing.T) {
	parsed, err := url.Parse("https://example.amazonaws.com/")
	if err != nil {
		t.Fatal(err)
	}
	req := SigV4SigningRequest{
		Method:    "GET",
		URL:       parsed,
		Payload:   []byte{},
		Region:    "us-east-1",
		Service:   "service",
		AccessKey: "AKIDEXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		AMZDate:   "20150830T123600Z",
	}
	a := SignSigV4(req)
	b := SignSigV4(req)
	if a.Authorization != b.Authorization {
		t.Fatalf("signing is not deterministic: %q != %q", a.Authorization, b.Authorization)
	}
	if !strings.HasPrefix(a.Authorization, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, ") {
		t.Fatalf("authorization prefix mismatch: %s", a.Authorization)
	}
	if !strings.Contains(a.Authorization, "SignedHeaders=host;x-amz-content-sha256;x-amz-date,") {
		t.Fatalf("signed headers mismatch: %s", a.Authorization)
	}
	signature := a.Authorization[strings.LastIndex(a.Authorization, "Signature=")+len("Signature="):]
	if len(signature) != 64 {
		t.Fatalf("signature length mismatch: %s", signature)
	}
}

func TestSigV4UpstreamNamesAliasExistingSigner(t *testing.T) {
	parsed, err := url.Parse("https://example.amazonaws.com/")
	if err != nil {
		t.Fatal(err)
	}
	request := SigningRequest{Method: "GET", URL: parsed, Payload: []byte{}, Region: "us-east-1", Service: "service", AccessKey: "AKID", SecretKey: "secret", AMZDate: "20250101T000000Z"}
	var signed SignedRequest = Sign(request)
	if signed.Authorization == "" || signed.HeaderValue("x-amz-date") != "20250101T000000Z" {
		t.Fatalf("signed request mismatch: %#v", signed)
	}
}

func TestSigV4SignPostWithSessionToken(t *testing.T) {
	parsed, err := url.Parse("https://bedrock-runtime.us-east-1.amazonaws.com/model/anthropic.claude/invoke")
	if err != nil {
		t.Fatal(err)
	}
	signed := SignSigV4(SigV4SigningRequest{
		Method:       "POST",
		URL:          parsed,
		Headers:      []SigV4Header{{Name: "content-type", Value: "application/json"}},
		Payload:      []byte(`{"messages":[]}`),
		Region:       "us-east-1",
		Service:      "bedrock",
		AccessKey:    "AKIATEST",
		SecretKey:    "secret",
		SessionToken: "token123",
		AMZDate:      "20250101T000000Z",
	})
	if !strings.Contains(signed.Authorization, "Credential=AKIATEST/20250101/us-east-1/bedrock/aws4_request") {
		t.Fatalf("credential mismatch: %s", signed.Authorization)
	}
	if !strings.Contains(signed.Authorization, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date;x-amz-security-token") {
		t.Fatalf("signed headers mismatch: %s", signed.Authorization)
	}
	if signed.HeaderValue("x-amz-security-token") != "token123" || signed.HeaderValue("x-amz-date") != "20250101T000000Z" || signed.HeaderValue("x-amz-content-sha256") == "" {
		t.Fatalf("headers mismatch: %#v", signed.Headers)
	}
	if len(signed.AllHeaders()) != len(signed.Headers)+1 || signed.AllHeaders()[len(signed.Headers)].Name != "authorization" {
		t.Fatalf("all headers mismatch: %#v", signed.AllHeaders())
	}
}

func TestSigV4CanonicalQueryAndWhitespace(t *testing.T) {
	parsed, err := url.Parse("https://x.example.com/?b=2&a=hello%20world")
	if err != nil {
		t.Fatal(err)
	}
	if got := SigV4CanonicalQuery(parsed); got != "a=hello%20world&b=2" {
		t.Fatalf("query mismatch: %s", got)
	}
	if got := SigV4TrimCollapse("  foo   bar  "); got != "foo bar" {
		t.Fatalf("trim mismatch: %q", got)
	}
}

func TestSigV4TrimCollapseOnlyNormalizesSpaceAndTabLikeUpstream(t *testing.T) {
	if got := SigV4TrimCollapse("  foo\n\tbar  "); got != "foo\n bar" {
		t.Fatalf("trim mismatch: %q", got)
	}
}

func TestSigV4CanonicalQueryEncodesDecodedPairsLikeUpstream(t *testing.T) {
	parsed, err := url.Parse("https://x.example.com/?space=hello+world&slash=a%2Fb&empty=")
	if err != nil {
		t.Fatal(err)
	}
	if got := SigV4CanonicalQuery(parsed); got != "empty=&slash=a%2Fb&space=hello%20world" {
		t.Fatalf("query mismatch: %s", got)
	}
}

func TestSigV4CanonicalURIEncodesPathSegmentsLikeUpstream(t *testing.T) {
	parsed, err := url.Parse("https://x.example.com/a b/c%2Fd")
	if err != nil {
		t.Fatal(err)
	}
	if got := sigV4CanonicalURI(parsed); got != "/a%2520b/c%252Fd" {
		t.Fatalf("path mismatch: %s", got)
	}
}

func TestSigV4OmitsDefaultPortFromHostLikeUpstream(t *testing.T) {
	withDefaultPort, err := url.Parse("https://example.amazonaws.com:443/")
	if err != nil {
		t.Fatal(err)
	}
	withoutPort, err := url.Parse("https://example.amazonaws.com/")
	if err != nil {
		t.Fatal(err)
	}
	base := SigV4SigningRequest{Method: "GET", Region: "us-east-1", Service: "service", AccessKey: "AKID", SecretKey: "secret", AMZDate: "20250101T000000Z"}
	base.URL = withDefaultPort
	withPortAuth := SignSigV4(base).Authorization
	base.URL = withoutPort
	withoutPortAuth := SignSigV4(base).Authorization
	if withPortAuth != withoutPortAuth {
		t.Fatalf("default port should not affect signature like upstream:\nwith:    %s\nwithout: %s", withPortAuth, withoutPortAuth)
	}
}

func TestSigV4PreservesMethodCaseLikeUpstream(t *testing.T) {
	parsed, err := url.Parse("https://example.amazonaws.com/")
	if err != nil {
		t.Fatal(err)
	}
	base := SigV4SigningRequest{URL: parsed, Region: "us-east-1", Service: "service", AccessKey: "AKID", SecretKey: "secret", AMZDate: "20250101T000000Z"}
	base.Method = "GET"
	upper := SignSigV4(base).Authorization
	base.Method = "get"
	lower := SignSigV4(base).Authorization
	if upper == lower {
		t.Fatal("method case should affect signature like upstream")
	}
}
