package otlp

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const BatchSize = 64

type Interest string

const InterestAlways Interest = "always"

type Span struct {
	Name       string
	Target     string
	Start      time.Time
	End        time.Time
	Attributes map[string]string
}

type Exporter struct {
	endpoint    string
	serviceName string
	client      *http.Client
	pending     []Span
	open        map[uint64]openSpan
	mu          sync.Mutex
}

type openSpan struct {
	Name       string
	Target     string
	Start      time.Time
	Attributes map[string]string
}

type AttrCollector struct {
	attrs map[string]string
}

type OtlpLayer = Exporter

func TryExporter() *Exporter {
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return nil
	}
	return NewExporter(endpoint)
}

func TryLayer() *OtlpLayer {
	return TryExporter()
}

func NewExporter(endpoint string) *Exporter {
	return &Exporter{endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"), serviceName: "pie", client: &http.Client{Timeout: 5 * time.Second}, open: map[uint64]openSpan{}}
}

func (exporter *Exporter) WithServiceName(name string) *Exporter {
	copyExporter := *exporter
	if strings.TrimSpace(name) != "" {
		copyExporter.serviceName = name
	}
	copyExporter.pending = nil
	copyExporter.open = map[uint64]openSpan{}
	return &copyExporter
}

func (exporter *Exporter) CloneForRename() *Exporter {
	if exporter == nil {
		return nil
	}
	clone := *exporter
	clone.pending = nil
	clone.open = map[uint64]openSpan{}
	return &clone
}

func (exporter *Exporter) Endpoint() string {
	if exporter == nil {
		return ""
	}
	return exporter.endpoint
}

func (exporter *Exporter) RecordSpan(span Span) {
	if exporter == nil {
		return
	}
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	exporter.pending = append(exporter.pending, span)
}

func (exporter *Exporter) OnNewSpan(id uint64, name string, target string, attributes map[string]string) {
	if exporter == nil {
		return
	}
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	if exporter.open == nil {
		exporter.open = map[uint64]openSpan{}
	}
	exporter.open[id] = openSpan{Name: name, Target: target, Start: time.Now(), Attributes: cloneAttrs(attributes)}
}

func (exporter *Exporter) OnClose(id uint64) {
	if exporter == nil {
		return
	}
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	span, ok := exporter.open[id]
	if !ok {
		return
	}
	delete(exporter.open, id)
	exporter.pending = append(exporter.pending, Span{Name: span.Name, Target: span.Target, Start: span.Start, End: time.Now(), Attributes: cloneAttrs(span.Attributes)})
}

func (exporter *Exporter) PendingCount() int {
	if exporter == nil {
		return 0
	}
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return len(exporter.pending)
}

func (exporter *Exporter) OpenCount() int {
	if exporter == nil {
		return 0
	}
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return len(exporter.open)
}

func (exporter *Exporter) FlushOnce() error {
	return exporter.Flush()
}

func (exporter *Exporter) RegisterCallsite(metadata any) Interest {
	return RegisterCallsite(metadata)
}

func RegisterCallsite(metadata any) Interest {
	return InterestAlways
}

func (exporter *Exporter) Flush() error {
	if exporter == nil {
		return nil
	}
	exporter.mu.Lock()
	if len(exporter.pending) == 0 {
		exporter.mu.Unlock()
		return nil
	}
	spans := append([]Span(nil), exporter.pending...)
	exporter.pending = nil
	exporter.mu.Unlock()
	payload := exporter.payload(spans)
	body, err := marshalJSONNoHTMLEscape(payload)
	if err != nil {
		return err
	}
	client := exporter.client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Post(exporter.endpoint+"/v1/traces", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("otlp exporter status %d", resp.StatusCode)
	}
	return nil
}

func (exporter *Exporter) payload(spans []Span) map[string]any {
	encoded := make([]any, 0, len(spans))
	for _, span := range spans {
		encoded = append(encoded, encodeSpan(span))
	}
	return map[string]any{
		"resourceSpans": []any{map[string]any{
			"resource": map[string]any{"attributes": []any{
				stringAttribute("service.name", exporter.serviceName),
				stringAttribute("service.version", "dev"),
			}},
			"scopeSpans": []any{map[string]any{
				"scope": map[string]any{"name": "pie"},
				"spans": encoded,
			}},
		}},
	}
}

func encodeSpan(span Span) map[string]any {
	attrs := make([]any, 0, len(span.Attributes)+1)
	if span.Target != "" {
		attrs = append(attrs, stringAttribute("tracing.target", span.Target))
	}
	for key, value := range span.Attributes {
		attrs = append(attrs, stringAttribute(key, value))
	}
	return map[string]any{
		"traceId":           HexRandom(16),
		"spanId":            spanID(span),
		"name":              span.Name,
		"kind":              1,
		"startTimeUnixNano": fmt.Sprintf("%d", span.Start.UnixNano()),
		"endTimeUnixNano":   fmt.Sprintf("%d", span.End.UnixNano()),
		"attributes":        attrs,
		"status":            map[string]any{"code": 1},
	}
}

func stringAttribute(key string, value string) map[string]any {
	return map[string]any{"key": key, "value": map[string]any{"stringValue": value}}
}

func NewAttrCollector() *AttrCollector {
	return &AttrCollector{attrs: map[string]string{}}
}

func (collector *AttrCollector) RecordDebug(key string, value any) {
	collector.record(key, fmt.Sprintf("%v", value))
}

func (collector *AttrCollector) RecordStr(key string, value string) {
	collector.record(key, value)
}

func (collector *AttrCollector) RecordI64(key string, value int64) {
	collector.record(key, fmt.Sprintf("%d", value))
}

func (collector *AttrCollector) RecordU64(key string, value uint64) {
	collector.record(key, fmt.Sprintf("%d", value))
}

func (collector *AttrCollector) RecordBool(key string, value bool) {
	collector.record(key, fmt.Sprintf("%t", value))
}

func (collector *AttrCollector) Attrs() map[string]string {
	if collector == nil {
		return nil
	}
	return cloneAttrs(collector.attrs)
}

func (collector *AttrCollector) record(key string, value string) {
	if collector == nil {
		return
	}
	if collector.attrs == nil {
		collector.attrs = map[string]string{}
	}
	collector.attrs[key] = value
}

func cloneAttrs(attrs map[string]string) map[string]string {
	if attrs == nil {
		return nil
	}
	clone := make(map[string]string, len(attrs))
	for key, value := range attrs {
		clone[key] = value
	}
	return clone
}

var hexRandomSeed atomic.Uint64

func init() {
	hexRandomSeed.Store(0x9E3779B97F4A7C15)
}

func NowNS() uint64 {
	return uint64(time.Now().UnixNano())
}

func HexRandom(bytes int) string {
	if bytes <= 0 {
		return ""
	}
	state := hexRandomSeed.Add(0x6364136223846793) - 0x6364136223846793
	var out strings.Builder
	out.Grow(bytes * 2)
	for index := 0; index < bytes; index++ {
		state = state*6364136223846793005 + 1442695040888963407
		fmt.Fprintf(&out, "%02x", byte(state>>56))
	}
	return out.String()
}

func spanID(span Span) string {
	value := span.Start.UnixNano() ^ span.End.UnixNano() ^ int64(len(span.Name))
	if value < 0 {
		value = -value
	}
	return fmt.Sprintf("%016x", uint64(value))
}
