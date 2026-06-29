package ai

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type SigV4Header struct {
	Name  string
	Value string
}

type SigV4SigningRequest struct {
	Method       string
	URL          *url.URL
	Headers      []SigV4Header
	Payload      []byte
	Region       string
	Service      string
	AccessKey    string
	SecretKey    string
	SessionToken string
	AMZDate      string
}

type SigningRequest = SigV4SigningRequest

type SigV4SignedRequest struct {
	Authorization string
	Headers       []SigV4Header
}

type SignedRequest = SigV4SignedRequest

func (request SigV4SignedRequest) AllHeaders() []SigV4Header {
	headers := append([]SigV4Header(nil), request.Headers...)
	headers = append(headers, SigV4Header{Name: "authorization", Value: request.Authorization})
	return headers
}

func (request SigV4SignedRequest) HeaderValue(name string) string {
	for _, header := range request.Headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

func SignSigV4(request SigV4SigningRequest) SigV4SignedRequest {
	payloadHash := sha256Hex(request.Payload)
	headers := map[string]string{}
	for _, header := range request.Headers {
		headers[strings.ToLower(header.Name)] = SigV4TrimCollapse(header.Value)
	}
	headers["host"] = sigV4HostWithPort(request.URL)
	headers["x-amz-date"] = request.AMZDate
	headers["x-amz-content-sha256"] = payloadHash
	if request.SessionToken != "" {
		headers["x-amz-security-token"] = request.SessionToken
	}
	signedNames := sortedHeaderNames(headers)
	canonicalRequest := strings.Join([]string{
		request.Method,
		sigV4CanonicalURI(request.URL),
		SigV4CanonicalQuery(request.URL),
		sigV4CanonicalHeaders(headers, signedNames),
		strings.Join(signedNames, ";"),
		payloadHash,
	}, "\n")
	date := request.AMZDate[:8]
	scope := date + "/" + request.Region + "/" + request.Service + "/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		request.AMZDate,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(hmacSHA256(sigV4SigningKey(request.SecretKey, date, request.Region, request.Service), []byte(stringToSign)))
	return SigV4SignedRequest{
		Authorization: fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", request.AccessKey, scope, strings.Join(signedNames, ";"), signature),
		Headers:       sigV4HeaderSlice(headers, signedNames),
	}
}

func Sign(request SigningRequest) SignedRequest {
	return SignSigV4(request)
}

func SigV4CanonicalQuery(value *url.URL) string {
	parts := make([]string, 0)
	for key, values := range value.Query() {
		for _, item := range values {
			parts = append(parts, sigV4EncodeStrict(key)+"="+sigV4EncodeStrict(item))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func SigV4TrimCollapse(value string) string {
	var builder strings.Builder
	lastSpace := false
	for _, item := range strings.Trim(value, " \t") {
		if item == ' ' || item == '\t' {
			if !lastSpace {
				builder.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		builder.WriteRune(item)
		lastSpace = false
	}
	return builder.String()
}

func sigV4CanonicalURI(value *url.URL) string {
	path := value.EscapedPath()
	if value.RawPath != "" {
		path = strings.ReplaceAll(value.RawPath, " ", "%20")
	}
	if path == "" {
		return "/"
	}
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		segments[index] = sigV4EncodeStrict(segment)
	}
	return strings.Join(segments, "/")
}

func sigV4HostWithPort(value *url.URL) string {
	host := value.Hostname()
	port := value.Port()
	if port == "" || value.Scheme == "http" && port == "80" || value.Scheme == "https" && port == "443" {
		return host
	}
	return host + ":" + port
}

func sigV4EncodeStrict(value string) string {
	var builder strings.Builder
	for _, item := range []byte(value) {
		if item >= 'A' && item <= 'Z' || item >= 'a' && item <= 'z' || item >= '0' && item <= '9' || item == '-' || item == '_' || item == '.' || item == '~' {
			builder.WriteByte(item)
			continue
		}
		builder.WriteString(fmt.Sprintf("%%%02X", item))
	}
	return builder.String()
}

func sigV4CanonicalHeaders(headers map[string]string, names []string) string {
	var builder strings.Builder
	for _, name := range names {
		builder.WriteString(name)
		builder.WriteByte(':')
		builder.WriteString(headers[name])
		builder.WriteByte('\n')
	}
	return builder.String()
}

func sortedHeaderNames(headers map[string]string) []string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sigV4HeaderSlice(headers map[string]string, names []string) []SigV4Header {
	out := make([]SigV4Header, 0, len(names))
	for _, name := range names {
		if name == "host" {
			continue
		}
		out = append(out, SigV4Header{Name: name, Value: headers[name]})
	}
	return out
}

func sigV4SigningKey(secret, date, region, service string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	regionKey := hmacSHA256(dateKey, []byte(region))
	serviceKey := hmacSHA256(regionKey, []byte(service))
	return hmacSHA256(serviceKey, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
