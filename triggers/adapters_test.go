package triggers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/mcp"
)

func TestCronIntervalAdapterFiresDueTriggersAndTracksSchedule(t *testing.T) {
	start := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	adapter := NewCronIntervalAdapter([]CronIntervalJob{{ID: "standup", Label: "Standup", Every: time.Minute, Prompt: "summarize status", Enabled: true}})
	if got := adapter.Poll(start.Add(30 * time.Second)); len(got) != 0 {
		t.Fatalf("not due yet: %#v", got)
	}
	fired := adapter.Poll(start.Add(time.Minute))
	if len(fired) != 1 {
		t.Fatalf("expected one trigger, got %#v", fired)
	}
	trigger := fired[0]
	if trigger.Source.Kind != SourceLocal || trigger.Source.Subkind != "cron" || trigger.SourceLabel != "Standup" || trigger.EventLabel != "cron due" {
		t.Fatalf("trigger envelope mismatch: %#v", trigger)
	}
	if trigger.IDempotencyKey == "" || !strings.Contains(trigger.IDempotencyKey, "standup") {
		t.Fatalf("idempotency mismatch: %#v", trigger)
	}
	payload, ok := trigger.Payload.(CronPayload)
	if !ok || payload.Prompt != "summarize status" || payload.JobID != "standup" {
		t.Fatalf("payload mismatch: %#v", trigger.Payload)
	}
	if got := adapter.Poll(start.Add(90 * time.Second)); len(got) != 0 {
		t.Fatalf("same interval should not refire early: %#v", got)
	}
	if got := adapter.Poll(start.Add(2 * time.Minute)); len(got) != 1 {
		t.Fatalf("next interval should fire, got %#v", got)
	}
}

func TestFilePollAdapterFiresOnCreateModifyAndDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watched.txt")
	adapter := NewFilePollAdapter([]FileWatch{{ID: "watch-doc", Path: path, Label: "Doc", Enabled: true}})
	if got := adapter.Poll(time.Now()); len(got) != 0 {
		t.Fatalf("missing file should not fire on first poll: %#v", got)
	}
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := adapter.Poll(time.Now())
	if len(created) != 1 {
		t.Fatalf("expected create trigger, got %#v", created)
	}
	if payload := created[0].Payload.(FilePayload); payload.Event != FileEventCreated || payload.Path != path {
		t.Fatalf("create payload mismatch: %#v", payload)
	}
	if got := adapter.Poll(time.Now()); len(got) != 0 {
		t.Fatalf("unchanged file should not fire: %#v", got)
	}
	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	modified := adapter.Poll(time.Now())
	if len(modified) != 1 || modified[0].Payload.(FilePayload).Event != FileEventModified {
		t.Fatalf("modify mismatch: %#v", modified)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	deleted := adapter.Poll(time.Now())
	if len(deleted) != 1 || deleted[0].Payload.(FilePayload).Event != FileEventDeleted {
		t.Fatalf("delete mismatch: %#v", deleted)
	}
}

func TestMapMCPNotificationMapsKnownMethods(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	trigger, ok := MapMCPNotification("filesystem", mcp.ServerNotification{Method: "notifications/tools/listChanged", Params: map[string]any{}}, now)
	if !ok {
		t.Fatal("expected trigger")
	}
	if trigger.Source != (Source{Kind: SourceMCP, ServerName: "filesystem", Method: "notifications/tools/listChanged"}) || trigger.SourceKind != SourceKindMCP || trigger.SourceLabel != "mcp:filesystem" || trigger.EventLabel != "notifications/tools/listChanged" {
		t.Fatalf("source mismatch: %#v", trigger)
	}
	if trigger.IDempotencyKey != "mcp:filesystem:tools" || trigger.ReplacementPolicy != ReplacementLatestReplaces {
		t.Fatalf("dedup mismatch: %#v", trigger)
	}
	if trigger.PayloadVisibility != PayloadLocal || trigger.Payload != nil || trigger.PayloadSummary == nil || *trigger.PayloadSummary != "notifications/tools/listChanged" {
		t.Fatalf("payload mismatch: %#v", trigger)
	}
	if trigger.Authority.PrincipalID != "mcp:filesystem" || trigger.Authority.PrincipalLabel != "filesystem" || trigger.Authority.CredentialScope != ScopeUser || trigger.ReceivedAt != now || trigger.TraceID == "" {
		t.Fatalf("runtime fields mismatch: %#v", trigger)
	}

	resource, ok := MapMCPNotification("filesystem", mcp.ServerNotification{Method: "notifications/resources/updated", Params: map[string]any{"uri": "file:///proj/README.md", "rev": 5}}, now)
	if !ok {
		t.Fatal("expected resource trigger")
	}
	if resource.IDempotencyKey != "mcp:filesystem:resources:file:///proj/README.md" || resource.PayloadSummary == nil || !strings.Contains(*resource.PayloadSummary, "uri=file:///proj/README.md") || strings.Contains(*resource.PayloadSummary, "rev") {
		t.Fatalf("resource trigger mismatch: %#v", resource)
	}

	missingURI, ok := MapMCPNotification("filesystem", mcp.ServerNotification{Method: "notifications/resources/updated", Params: map[string]any{}}, now)
	if !ok || missingURI.IDempotencyKey != "mcp:filesystem:resources:unknown" {
		t.Fatalf("missing uri fallback mismatch: ok=%v trigger=%#v", ok, missingURI)
	}
	rawResource, ok := MapMCPNotification("filesystem", mcp.ServerNotification{Method: "notifications/resources/updated", Params: json.RawMessage(`{"uri":"file:///proj/raw.md","rev":9007199254740993}`)}, now)
	if !ok || rawResource.IDempotencyKey != "mcp:filesystem:resources:file:///proj/raw.md" || rawResource.PayloadSummary == nil || !strings.Contains(*rawResource.PayloadSummary, "file:///proj/raw.md") {
		t.Fatalf("raw json params should map like upstream serde_json::Value: ok=%v trigger=%#v", ok, rawResource)
	}
}

func TestMapMCPNotificationCustomDedupAndPrivacy(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	trigger, ok := MapMCPNotification("remote-agent", mcp.ServerNotification{Method: "notifications/agent_message", Params: map[string]any{
		"_meta":   map[string]any{"pie_dedup_key": "tools", "pie_summary": "message ready"},
		"payload": map[string]any{"secret": "hub_agent_secret_should_not_leave_local_payload"},
	}}, now)
	if !ok {
		t.Fatal("expected custom trigger")
	}
	if trigger.IDempotencyKey != "mcp:remote-agent:custom:tools" || trigger.ReplacementPolicy != ReplacementDrop {
		t.Fatalf("custom dedup mismatch: %#v", trigger)
	}
	if trigger.PayloadSummary == nil || !strings.Contains(*trigger.PayloadSummary, "message ready") || strings.Contains(*trigger.PayloadSummary, "hub_agent_secret_should_not_leave_local_payload") {
		t.Fatalf("custom summary privacy mismatch: %#v", trigger.PayloadSummary)
	}
	if trigger.Payload != nil || trigger.PayloadVisibility != PayloadLocal {
		t.Fatalf("custom payload mismatch: %#v", trigger)
	}

	secretKey, ok := MapMCPNotification("filesystem", mcp.ServerNotification{Method: "notifications/custom/payload", Params: map[string]any{"_meta": map[string]any{"pie_dedup_key": "hub_agent_secret_should_not_persist"}}}, now)
	if !ok || !strings.HasPrefix(secretKey.IDempotencyKey, "mcp:filesystem:custom:hash:") || strings.Contains(secretKey.IDempotencyKey, "hub_agent_secret_should_not_persist") {
		t.Fatalf("secret custom key mismatch: ok=%v trigger=%#v", ok, secretKey)
	}
	if got := SafeIdempotencySegment("hub_agent_secret_should_not_persist"); !strings.HasPrefix(got, "hash:") || strings.Contains(got, "hub_agent_secret_should_not_persist") {
		t.Fatalf("upstream-named safe idempotency segment mismatch: %q", got)
	}

	redactedSummary, ok := MapMCPNotification("filesystem", mcp.ServerNotification{Method: "notifications/custom/build-finished", Params: map[string]any{"_meta": map[string]any{"pie_dedup_key": "build-100", "pie_summary": "build leaked hub_agent_secret_should_not_persist token=sk-secret"}}}, now)
	if !ok || redactedSummary.PayloadSummary == nil || !strings.Contains(*redactedSummary.PayloadSummary, "[redacted]") || strings.Contains(*redactedSummary.PayloadSummary, "hub_agent_secret_should_not_persist") || strings.Contains(*redactedSummary.PayloadSummary, "sk-secret") {
		t.Fatalf("redacted summary mismatch: ok=%v trigger=%#v", ok, redactedSummary)
	}
	if got := SafeDisplay("build leaked hub_agent_secret_should_not_persist\ntoken=sk-secret", 200); !strings.Contains(got, "[redacted]") || strings.Contains(got, "\n") || strings.Contains(got, "hub_agent_secret_should_not_persist") || strings.Contains(got, "sk-secret") {
		t.Fatalf("upstream-named safe display mismatch: %q", got)
	}

	rawCustom, ok := MapMCPNotification("remote-agent", mcp.ServerNotification{Method: "notifications/custom/raw", Params: json.RawMessage(`{"_meta":{"pie_dedup_key":"raw-1","pie_summary":"raw ready"},"id":9007199254740993}`)}, now)
	if !ok || rawCustom.IDempotencyKey != "mcp:remote-agent:custom:raw-1" || rawCustom.PayloadSummary == nil || !strings.Contains(*rawCustom.PayloadSummary, "raw ready") {
		t.Fatalf("raw custom notification mismatch: ok=%v trigger=%#v", ok, rawCustom)
	}

	if dropped, ok := MapMCPNotification("filesystem", mcp.ServerNotification{Method: "notifications/custom/event", Params: map[string]any{"detail": "missing key"}}, now); ok || dropped.TraceID != "" {
		t.Fatalf("custom notification without dedup key should drop: ok=%v trigger=%#v", ok, dropped)
	}
}
