package otlp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTryExporterUsesEnvEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://collector.example/")
	exporter := TryExporter()
	if exporter == nil {
		t.Fatal("expected exporter")
	}
	if exporter.Endpoint() != "https://collector.example" {
		t.Fatalf("endpoint=%q", exporter.Endpoint())
	}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", " ")
	if got := TryExporter(); got != nil {
		t.Fatalf("expected nil exporter, got %#v", got)
	}
}

func TestOtlpLayerCompatSurface(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://collector.example/")
	var layer *OtlpLayer = TryLayer()
	if layer == nil || layer.Endpoint() != "https://collector.example" {
		t.Fatalf("layer mismatch: %#v", layer)
	}
}

func TestHexRandomAndNowNSMatchUpstreamHelpers(t *testing.T) {
	first := HexRandom(8)
	second := HexRandom(16)
	if len(first) != 16 || len(second) != 32 || first == second[:16] || !regexp.MustCompile(`^[0-9a-f]+$`).MatchString(first+second) {
		t.Fatalf("hex random mismatch: first=%q second=%q", first, second)
	}
	if NowNS() == 0 {
		t.Fatal("NowNS should return unix nanoseconds")
	}
}

func TestFlushPostsOTLPTracePayload(t *testing.T) {
	var path string
	var rawBody string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		rawBody = string(body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	exporter := NewExporter(server.URL).WithServiceName("pig-test")
	exporter.RecordSpan(Span{Name: "tool.call", Target: "tools <core>", Start: time.Unix(10, 0), End: time.Unix(12, 0), Attributes: map[string]string{"tool": "bash <shell> && go > test"}})
	if err := exporter.Flush(); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/traces" {
		t.Fatalf("path=%q", path)
	}
	span := firstSpan(t, payload)
	if span["name"] != "tool.call" || span["kind"] != float64(1) {
		t.Fatalf("bad span: %#v", span)
	}
	if span["traceId"] == "00000000000000000000000000000000" {
		t.Fatalf("trace id should be synthesized like upstream: %#v", span)
	}
	if span["startTimeUnixNano"] != "10000000000" || span["endTimeUnixNano"] != "12000000000" {
		t.Fatalf("bad span timing: %#v", span)
	}
	attrs := attrsByKey(span["attributes"].([]any))
	if attrs["tool"] != "bash <shell> && go > test" || attrs["tracing.target"] != "tools <core>" {
		t.Fatalf("bad attrs: %#v", attrs)
	}
	if strings.Contains(rawBody, `\u003c`) || strings.Contains(rawBody, `\u003e`) || strings.Contains(rawBody, `\u0026`) {
		t.Fatalf("OTLP JSON should not HTML-escape like serde_json/reqwest .json(), got %s", rawBody)
	}
	if status := span["status"].(map[string]any); status["code"] != float64(1) {
		t.Fatalf("bad status: %#v", status)
	}
	resource := payload["resourceSpans"].([]any)[0].(map[string]any)["resource"].(map[string]any)
	resourceAttrs := attrsByKey(resource["attributes"].([]any))
	if resourceAttrs["service.name"] != "pig-test" {
		t.Fatalf("bad resource attrs: %#v", resourceAttrs)
	}
}

func TestFlushNoopsWhenNoPendingSpans(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()
	if err := NewExporter(server.URL).Flush(); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("empty flush should not post")
	}
}

func TestOTLPLifecycleAndAttrHelpers(t *testing.T) {
	collector := NewAttrCollector()
	collector.RecordDebug("debug", []string{"x"})
	collector.RecordStr("str", "value")
	collector.RecordI64("i64", -7)
	collector.RecordU64("u64", 9)
	collector.RecordBool("bool", true)
	attrs := collector.Attrs()
	if attrs["debug"] != "[x]" || attrs["str"] != "value" || attrs["i64"] != "-7" || attrs["u64"] != "9" || attrs["bool"] != "true" {
		t.Fatalf("attrs mismatch: %#v", attrs)
	}

	exporter := NewExporter("https://collector.example")
	exporter.RecordSpan(Span{Name: "old", Start: time.Unix(1, 0), End: time.Unix(2, 0)})
	renamed := exporter.CloneForRename().WithServiceName("renamed")
	if renamed.Endpoint() != exporter.Endpoint() || renamed.PendingCount() != 0 || exporter.PendingCount() != 1 {
		t.Fatalf("clone for rename mismatch exporter=%d renamed=%d", exporter.PendingCount(), renamed.PendingCount())
	}

	id := uint64(42)
	exporter.OnNewSpan(id, "tool.call", "tools", attrs)
	if exporter.OpenCount() != 1 {
		t.Fatalf("open count mismatch: %d", exporter.OpenCount())
	}
	exporter.OnClose(id)
	if exporter.OpenCount() != 0 || exporter.PendingCount() != 2 {
		t.Fatalf("close should move span to pending: open=%d pending=%d", exporter.OpenCount(), exporter.PendingCount())
	}
}

func TestRegisterCallsiteAlwaysInterests(t *testing.T) {
	exporter := NewExporter("https://collector.example")
	if got := exporter.RegisterCallsite("metadata"); got != InterestAlways {
		t.Fatalf("RegisterCallsite()=%q", got)
	}
	if got := RegisterCallsite("metadata"); got != InterestAlways {
		t.Fatalf("RegisterCallsite package helper=%q", got)
	}
}

func firstSpan(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	resourceSpans := payload["resourceSpans"].([]any)
	scopeSpans := resourceSpans[0].(map[string]any)["scopeSpans"].([]any)
	spans := scopeSpans[0].(map[string]any)["spans"].([]any)
	return spans[0].(map[string]any)
}

func attrsByKey(raw []any) map[string]string {
	out := map[string]string{}
	for _, item := range raw {
		attr := item.(map[string]any)
		value := attr["value"].(map[string]any)
		out[attr["key"].(string)] = value["stringValue"].(string)
	}
	return out
}
