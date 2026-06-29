package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTriggerRecordReceivedFromEnvelope(t *testing.T) {
	trigger := sampleTrigger("key-1", "trace-1", time.Now())
	record := RecordReceivedFrom(trigger)
	if record.SchemaVersion != 1 || record.State != StateReceived || record.IDempotencyKey != "key-1" || record.PayloadSummary == nil || *record.PayloadSummary != "summary" {
		t.Fatalf("record mismatch: %#v", record)
	}
	if !StateCompleted.IsTerminal() || StateRunning.IsTerminal() {
		t.Fatalf("terminal state mismatch")
	}
}

func TestTriggerRecordJSONUsesUpstreamSnakeCaseFields(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	record := RecordReceivedFrom(sampleTrigger("key-1", "trace-1", now))
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "source_kind", "source_label", "event_label", "trace_id", "idempotency_key", "replacement_policy", "received_at", "payload_visibility", "payload_summary"} {
		if _, ok := object[key]; !ok {
			t.Fatalf("missing upstream field %q in %s", key, string(encoded))
		}
	}
	for _, key := range []string{"SchemaVersion", "SourceKind", "SourceLabel", "EventLabel", "TraceID", "IDempotencyKey", "ReplacementPolicy", "ReceivedAt", "PayloadVisibility", "PayloadSummary"} {
		if _, ok := object[key]; ok {
			t.Fatalf("unexpected Go field %q in %s", key, string(encoded))
		}
	}
	authority, ok := object["authority"].(map[string]any)
	if !ok {
		t.Fatalf("authority should be object in %s", string(encoded))
	}
	if actions, ok := authority["allowed_source_actions"].([]any); !ok || len(actions) != 0 {
		t.Fatalf("allowed_source_actions should default to [] like upstream: %#v in %s", authority["allowed_source_actions"], string(encoded))
	}
	var decoded Authority
	if err := json.Unmarshal([]byte(`{"principal_id":"p1","principal_label":"user","credential_scope":"User"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.AllowedSourceActions == nil || len(decoded.AllowedSourceActions) != 0 {
		t.Fatalf("missing allowed_source_actions should decode as empty slice: %#v", decoded.AllowedSourceActions)
	}
}

func TestTriggerRecordNoHTMLEscapeHelperPreservesNestedCustomMarshalJSONLikeSerdeJSON(t *testing.T) {
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	trigger := sampleTrigger("key-1", "trace-1", now)
	trigger.SourceLabel = "mcp <filesystem>"
	trigger.EventLabel = "a < b && c > d"
	trigger.Authority.PrincipalLabel = "user <ops>"
	record := RecordReceivedFrom(trigger)
	encoded, err := marshalJSONNoHTMLEscape(record)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("trigger record JSON should not HTML-escape nested custom MarshalJSON output, got %s", text)
	}
	if !strings.Contains(text, `"source_label":"mcp <filesystem>"`) || !strings.Contains(text, `"event_label":"a < b && c > d"`) || !strings.Contains(text, `"principal_label":"user <ops>"`) {
		t.Fatalf("trigger record JSON should preserve literal nested strings, got %s", text)
	}
}

func TestTriggerJSONSerializesNilPayloadSummaryAsNullLikeUpstream(t *testing.T) {
	trigger := sampleTrigger("key-1", "trace-1", time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	trigger.PayloadSummary = nil
	encoded, err := json.Marshal(trigger)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["payload_summary"]; !ok {
		t.Fatalf("payload_summary should serialize as null like upstream Option field, got %s", encoded)
	}
	if object["payload_summary"] != nil {
		t.Fatalf("payload_summary should be null, got %#v in %s", object["payload_summary"], encoded)
	}
}

func TestTriggerPayloadPreservesRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	data := []byte(`{"source":{"kind":"local","subkind":"cron"},"source_kind":"local","source_label":"Cron","event_label":"job","payload_visibility":"shared","payload":{"id":9007199254740993},"idempotency_key":"key-1","replacement_policy":"drop","trace_id":"trace-1","authority":{"principal_id":"p1","principal_label":"user","credential_scope":"User"},"received_at":"2026-06-20T12:00:00Z"}`)
	var trigger Trigger
	if err := json.Unmarshal(data, &trigger); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(trigger.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("payload should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestTriggerRecordEvaluatorDecisionPreservesRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	data := []byte(`{"schema_version":1,"source":{"kind":"local","subkind":"cron"},"source_kind":"local","source_label":"Cron","event_label":"job","trace_id":"trace-1","authority":{"principal_id":"p1","principal_label":"user","credential_scope":"User"},"idempotency_key":"key-1","replacement_policy":"drop","received_at":"2026-06-20T12:00:00Z","state":"accepted","payload_visibility":"local","evaluator_decision":{"ticket":9007199254740993}}`)
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record.EvaluatorDecision)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("evaluator_decision should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestTriggerAuthorityRejectsNullAllowedSourceActionsLikeUpstream(t *testing.T) {
	var authority Authority
	data := []byte(`{"principal_id":"p1","principal_label":"user","credential_scope":"User","allowed_source_actions":null}`)
	if err := json.Unmarshal(data, &authority); err == nil {
		t.Fatalf("null allowed_source_actions should fail like upstream Vec field: %#v", authority)
	}
}

func TestTriggerJSONRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	base := map[string]any{
		"source":             map[string]any{"kind": "local", "subkind": "cron"},
		"source_kind":        "local",
		"source_label":       "Cron",
		"event_label":        "job",
		"payload_visibility": "local",
		"idempotency_key":    "key-1",
		"replacement_policy": "drop",
		"trace_id":           "trace-1",
		"authority":          map[string]any{"principal_id": "p1", "principal_label": "user", "credential_scope": "User"},
		"received_at":        "2026-06-20T12:00:00Z",
	}
	for _, field := range []string{"source", "source_kind", "source_label", "event_label", "payload_visibility", "idempotency_key", "replacement_policy", "trace_id", "authority", "received_at"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{}
			for key, value := range base {
				if key != field {
					object[key] = value
				}
			}
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var trigger Trigger
			if err := json.Unmarshal(data, &trigger); err == nil {
				t.Fatalf("missing %s should fail like upstream required field: %#v", field, trigger)
			}
		})
	}
}

func TestTriggerNestedJSONRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	triggerBase := map[string]any{
		"source":             map[string]any{"kind": "local", "subkind": "cron"},
		"source_kind":        "local",
		"source_label":       "Cron",
		"event_label":        "job",
		"payload_visibility": "local",
		"idempotency_key":    "key-1",
		"replacement_policy": "drop",
		"trace_id":           "trace-1",
		"authority":          map[string]any{"principal_id": "p1", "principal_label": "user", "credential_scope": "User"},
		"received_at":        "2026-06-20T12:00:00Z",
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "local_subkind", mutate: func(object map[string]any) { object["source"] = map[string]any{"kind": "local"} }},
		{name: "mcp_server_name", mutate: func(object map[string]any) { object["source"] = map[string]any{"kind": "mcp", "method": "changed"} }},
		{name: "mcp_method", mutate: func(object map[string]any) { object["source"] = map[string]any{"kind": "mcp", "server_name": "fs"} }},
		{name: "delegate_agent_id", mutate: func(object map[string]any) {
			object["source"] = map[string]any{"kind": "agent_delegate", "delegation_id": "d1"}
		}},
		{name: "delegate_delegation_id", mutate: func(object map[string]any) {
			object["source"] = map[string]any{"kind": "agent_delegate", "agent_id": "a1"}
		}},
		{name: "authority_principal_id", mutate: func(object map[string]any) {
			object["authority"] = map[string]any{"principal_label": "user", "credential_scope": "User"}
		}},
		{name: "authority_principal_label", mutate: func(object map[string]any) {
			object["authority"] = map[string]any{"principal_id": "p1", "credential_scope": "User"}
		}},
		{name: "authority_credential_scope", mutate: func(object map[string]any) {
			object["authority"] = map[string]any{"principal_id": "p1", "principal_label": "user"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := map[string]any{}
			for key, value := range triggerBase {
				object[key] = value
			}
			tt.mutate(object)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var trigger Trigger
			if err := json.Unmarshal(data, &trigger); err == nil {
				t.Fatalf("%s should fail like upstream required nested field: %#v", tt.name, trigger)
			}
		})
	}
}

func TestTriggerSourceJSONRejectsNullRequiredFieldsLikeUpstream(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "kind", data: `{"kind":null,"subkind":"cron"}`},
		{name: "local_subkind", data: `{"kind":"local","subkind":null}`},
		{name: "mcp_server_name", data: `{"kind":"mcp","server_name":null,"method":"changed"}`},
		{name: "mcp_method", data: `{"kind":"mcp","server_name":"fs","method":null}`},
		{name: "delegate_agent_id", data: `{"kind":"agent_delegate","agent_id":null,"delegation_id":"d1"}`},
		{name: "delegate_delegation_id", data: `{"kind":"agent_delegate","agent_id":"a1","delegation_id":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var source Source
			if err := json.Unmarshal([]byte(tt.data), &source); err == nil {
				t.Fatalf("null %s should fail like upstream Source serde: %#v", tt.name, source)
			}
		})
	}
}

func TestTriggerSourceUnmarshalDropsInactiveVariantFieldsLikeUpstream(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		assert func(*testing.T, Source)
	}{
		{name: "local", data: `{"kind":"local","subkind":"cron","server_name":"ignored","method":"ignored","agent_id":"ignored","delegation_id":"ignored"}`, assert: func(t *testing.T, source Source) {
			if source.ServerName != "" || source.Method != "" || source.AgentID != "" || source.DelegationID != "" {
				t.Fatalf("local should drop inactive fields like upstream: %#v", source)
			}
		}},
		{name: "mcp", data: `{"kind":"mcp","server_name":"fs","method":"changed","subkind":"ignored","agent_id":"ignored"}`, assert: func(t *testing.T, source Source) {
			if source.Subkind != "" || source.AgentID != "" || source.DelegationID != "" {
				t.Fatalf("mcp should drop inactive fields like upstream: %#v", source)
			}
		}},
		{name: "agent_delegate", data: `{"kind":"agent_delegate","agent_id":"a1","delegation_id":"d1","server_name":"ignored","method":"ignored","subkind":"ignored"}`, assert: func(t *testing.T, source Source) {
			if source.ServerName != "" || source.Method != "" || source.Subkind != "" {
				t.Fatalf("agent_delegate should drop inactive fields like upstream: %#v", source)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var source Source
			if err := json.Unmarshal([]byte(tt.data), &source); err != nil {
				t.Fatal(err)
			}
			tt.assert(t, source)
		})
	}
}

func TestTriggerSourceMarshalIncludesOnlyActiveVariantFieldsLikeUpstream(t *testing.T) {
	tests := []struct {
		name       string
		source     Source
		wantFields []string
		omitFields []string
	}{
		{name: "local", source: Source{Kind: SourceLocal, Subkind: "cron", ServerName: "ignored", Method: "ignored", AgentID: "ignored", DelegationID: "ignored"}, wantFields: []string{"kind", "subkind"}, omitFields: []string{"server_name", "method", "agent_id", "delegation_id"}},
		{name: "mcp", source: Source{Kind: SourceMCP, ServerName: "", Method: "", Subkind: "ignored"}, wantFields: []string{"kind", "server_name", "method"}, omitFields: []string{"subkind", "agent_id", "delegation_id"}},
		{name: "agent_delegate", source: Source{Kind: SourceAgentDelegate, AgentID: "", DelegationID: "", ServerName: "ignored"}, wantFields: []string{"kind", "agent_id", "delegation_id"}, omitFields: []string{"server_name", "method", "subkind"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.source)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			if err := json.Unmarshal(data, &object); err != nil {
				t.Fatal(err)
			}
			for _, field := range tt.wantFields {
				if _, ok := object[field]; !ok {
					t.Fatalf("expected %s in %s source JSON, got %s", field, tt.name, string(data))
				}
			}
			for _, field := range tt.omitFields {
				if _, ok := object[field]; ok {
					t.Fatalf("unexpected %s in %s source JSON, got %s", field, tt.name, string(data))
				}
			}
		})
	}
}

func TestTriggerJSONRejectsUnknownEnumValuesLikeUpstream(t *testing.T) {
	base := map[string]any{
		"source":             map[string]any{"kind": "local", "subkind": "cron"},
		"source_kind":        "local",
		"source_label":       "Cron",
		"event_label":        "job",
		"payload_visibility": "local",
		"idempotency_key":    "key-1",
		"replacement_policy": "drop",
		"trace_id":           "trace-1",
		"authority":          map[string]any{"principal_id": "p1", "principal_label": "user", "credential_scope": "User"},
		"received_at":        "2026-06-20T12:00:00Z",
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "source_kind", mutate: func(object map[string]any) { object["source_kind"] = "webhook" }},
		{name: "payload_visibility", mutate: func(object map[string]any) { object["payload_visibility"] = "public" }},
		{name: "replacement_policy", mutate: func(object map[string]any) { object["replacement_policy"] = "replace" }},
		{name: "credential_scope", mutate: func(object map[string]any) {
			object["authority"] = map[string]any{"principal_id": "p1", "principal_label": "user", "credential_scope": "Workspace"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := map[string]any{}
			for key, value := range base {
				object[key] = value
			}
			tt.mutate(object)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var trigger Trigger
			if err := json.Unmarshal(data, &trigger); err == nil {
				t.Fatalf("unknown %s enum should fail like upstream serde: %#v", tt.name, trigger)
			}
		})
	}
}

func TestTriggerMarshalRejectsUnknownEnumValuesLikeUpstream(t *testing.T) {
	base := sampleTrigger("key-1", "trace-1", time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	tests := []struct {
		name   string
		mutate func(*Trigger)
	}{
		{name: "source_kind", mutate: func(trigger *Trigger) { trigger.SourceKind = SourceKind("webhook") }},
		{name: "payload_visibility", mutate: func(trigger *Trigger) { trigger.PayloadVisibility = PayloadVisibility("public") }},
		{name: "replacement_policy", mutate: func(trigger *Trigger) { trigger.ReplacementPolicy = ReplacementPolicy("replace") }},
		{name: "authority_credential_scope", mutate: func(trigger *Trigger) { trigger.Authority.CredentialScope = CredentialScope("Workspace") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trigger := base
			tt.mutate(&trigger)
			if data, err := json.Marshal(trigger); err == nil {
				t.Fatalf("unknown %s enum should fail like upstream marshal, got %s", tt.name, string(data))
			}
		})
	}
}

func TestTriggerRecordJSONRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	base := map[string]any{
		"schema_version":     float64(1),
		"source":             map[string]any{"kind": "local", "subkind": "cron"},
		"source_kind":        "local",
		"source_label":       "Cron",
		"event_label":        "job",
		"trace_id":           "trace-1",
		"authority":          map[string]any{"principal_id": "p1", "principal_label": "user", "credential_scope": "User"},
		"idempotency_key":    "key-1",
		"replacement_policy": "drop",
		"received_at":        "2026-06-20T12:00:00Z",
		"state":              "received",
		"payload_visibility": "local",
	}
	for _, field := range []string{"schema_version", "source", "source_kind", "source_label", "event_label", "trace_id", "authority", "idempotency_key", "replacement_policy", "received_at", "state", "payload_visibility"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{}
			for key, value := range base {
				if key != field {
					object[key] = value
				}
			}
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var record Record
			if err := json.Unmarshal(data, &record); err == nil {
				t.Fatalf("missing %s should fail like upstream required record field: %#v", field, record)
			}
		})
	}
}

func TestTriggerRecordJSONRejectsUnknownEnumValuesLikeUpstream(t *testing.T) {
	base := map[string]any{
		"schema_version":     float64(1),
		"source":             map[string]any{"kind": "local", "subkind": "cron"},
		"source_kind":        "local",
		"source_label":       "Cron",
		"event_label":        "job",
		"trace_id":           "trace-1",
		"authority":          map[string]any{"principal_id": "p1", "principal_label": "user", "credential_scope": "User"},
		"idempotency_key":    "key-1",
		"replacement_policy": "drop",
		"received_at":        "2026-06-20T12:00:00Z",
		"state":              "received",
		"payload_visibility": "local",
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "source_kind", mutate: func(object map[string]any) { object["source_kind"] = "webhook" }},
		{name: "replacement_policy", mutate: func(object map[string]any) { object["replacement_policy"] = "replace" }},
		{name: "state", mutate: func(object map[string]any) { object["state"] = "queued" }},
		{name: "payload_visibility", mutate: func(object map[string]any) { object["payload_visibility"] = "public" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := map[string]any{}
			for key, value := range base {
				object[key] = value
			}
			tt.mutate(object)
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var record Record
			if err := json.Unmarshal(data, &record); err == nil {
				t.Fatalf("unknown %s enum should fail like upstream record serde: %#v", tt.name, record)
			}
		})
	}
}

func TestTriggerRecordMarshalRejectsUnknownEnumValuesLikeUpstream(t *testing.T) {
	record := RecordReceivedFrom(sampleTrigger("key-1", "trace-1", time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)))
	record.State = StateReceived
	tests := []struct {
		name   string
		mutate func(*Record)
	}{
		{name: "source_kind", mutate: func(record *Record) { record.SourceKind = SourceKind("webhook") }},
		{name: "replacement_policy", mutate: func(record *Record) { record.ReplacementPolicy = ReplacementPolicy("replace") }},
		{name: "state", mutate: func(record *Record) { record.State = State("queued") }},
		{name: "payload_visibility", mutate: func(record *Record) { record.PayloadVisibility = PayloadVisibility("public") }},
		{name: "authority_credential_scope", mutate: func(record *Record) { record.Authority.CredentialScope = CredentialScope("Workspace") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := record
			tt.mutate(&candidate)
			if data, err := json.Marshal(candidate); err == nil {
				t.Fatalf("unknown %s enum should fail like upstream record marshal, got %s", tt.name, string(data))
			}
		})
	}
}

func TestTriggerUpstreamExportedNames(t *testing.T) {
	var source TriggerSource = Source{Kind: SourceMCP, ServerName: "server", Method: "tools/list"}
	if source.Kind != SourceMCP {
		t.Fatalf("trigger source alias mismatch: %#v", source)
	}

	var authority TriggerAuthority = Authority{PrincipalID: "user-1", CredentialScope: ScopeUser}
	if authority.CredentialScope != CredentialScope("User") {
		t.Fatalf("trigger authority alias mismatch: %#v", authority)
	}

	var state TriggerState = StateCompleted
	if !state.IsTerminal() {
		t.Fatalf("trigger state alias should expose IsTerminal")
	}

	trigger := sampleTrigger("key-upstream", "trace-upstream", time.Now())
	var record TriggerRecord = RecordReceivedFrom(trigger)
	if record.State != TriggerState(StateReceived) || record.SchemaVersion != TriggerRecordSchemaVersion {
		t.Fatalf("trigger record alias mismatch: %#v", record)
	}
	if SCHEMA_VERSION != TriggerRecordSchemaVersion {
		t.Fatalf("trigger record schema version alias mismatch: %d", SCHEMA_VERSION)
	}
	if received := (TriggerRecord{}).ReceivedFrom(trigger); !reflect.DeepEqual(received, record) {
		t.Fatalf("trigger record received-from method mismatch: %#v", received)
	}
	if TriggerRecordCustomType != "trigger" {
		t.Fatalf("trigger record custom type mismatch: %q", TriggerRecordCustomType)
	}

	var config TriggerRuntimeConfig = DefaultTriggerRuntimeConfig()
	if config.DedupWindow != TriggerRuntimeDefaultDedupWindow || config.CycleHopLimit != TriggerRuntimeDefaultCycleHopLimit {
		t.Fatalf("trigger runtime config defaults mismatch: %#v", config)
	}

	var runtime *TriggerRuntime = NewTriggerRuntimeWithConfig(config)
	if outcome := runtime.Evaluate(trigger); outcome.Kind != OutcomeAccept {
		t.Fatalf("trigger runtime alias evaluate mismatch: %#v", outcome)
	}

	var snapshot TriggerRuntimeSnapshot = runtime.Snapshot()
	if snapshot.AcceptedTotal != 1 {
		t.Fatalf("trigger runtime snapshot alias mismatch: %#v", snapshot)
	}
}

func TestTriggerRuntimeConfigUpstreamAssociatedNames(t *testing.T) {
	if DEFAULT_DEDUP_WINDOW != TriggerRuntimeDefaultDedupWindow {
		t.Fatalf("default dedup window mismatch: %s", DEFAULT_DEDUP_WINDOW)
	}
	if DEFAULT_CYCLE_HOP_LIMIT != TriggerRuntimeDefaultCycleHopLimit {
		t.Fatalf("default cycle hop limit mismatch: %d", DEFAULT_CYCLE_HOP_LIMIT)
	}
	if MAX_DEDUP_WINDOW != TriggerRuntimeMaxDedupWindow {
		t.Fatalf("max dedup window mismatch: %s", MAX_DEDUP_WINDOW)
	}

	runtime := NewTriggerRuntime().WithConfig(TriggerRuntimeConfig{DedupWindow: 48 * time.Hour, CycleHopLimit: 7})
	config := runtime.Config()
	if config.DedupWindow != MAX_DEDUP_WINDOW || config.CycleHopLimit != 7 {
		t.Fatalf("with-config should clamp only dedup window: %#v", config)
	}
}

func TestRuntimeDedupCycleAndSnapshot(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{DedupWindow: time.Minute, CycleHopLimit: 2})
	now := time.Now()
	first := sampleTrigger("key-1", "trace-1", now)
	if outcome := runtime.Evaluate(first); outcome.Kind != OutcomeAccept {
		t.Fatalf("expected accept, got %#v", outcome)
	}
	duplicate := sampleTrigger("key-1", "trace-other", now.Add(time.Second))
	if outcome := runtime.Evaluate(duplicate); outcome.Kind != OutcomeDeduped || outcome.ReplacementPolicy != ReplacementDrop || outcome.PreviousTraceID != "trace-1" {
		t.Fatalf("expected dedup, got %#v", outcome)
	}
	secondHop := sampleTrigger("key-2", "trace-1", now.Add(2*time.Second))
	if outcome := runtime.Evaluate(secondHop); outcome.Kind != OutcomeAccept {
		t.Fatalf("expected second accept, got %#v", outcome)
	}
	thirdHop := sampleTrigger("key-3", "trace-1", now.Add(3*time.Second))
	if outcome := runtime.Evaluate(thirdHop); outcome.Kind != OutcomeCycleSuppressed || outcome.HopCount != 2 {
		t.Fatalf("expected cycle suppression, got %#v", outcome)
	}
	snap := runtime.Snapshot()
	if snap.AcceptedTotal != 2 || snap.DedupedTotal != 1 || snap.CycleSuppressedTotal != 1 || snap.DedupEntries != 2 || snap.ActiveTraces != 1 {
		t.Fatalf("snapshot mismatch: %#v", snap)
	}
	if runtime.DedupEntryCount() != 2 || runtime.CycleEntryCount() != 1 {
		t.Fatalf("entry count helpers mismatch: dedup=%d cycle=%d", runtime.DedupEntryCount(), runtime.CycleEntryCount())
	}
}

func TestRuntimePrunesExpiredDedup(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{DedupWindow: time.Second, CycleHopLimit: 5})
	now := time.Now()
	if runtime.Evaluate(sampleTrigger("key", "trace", now)).Kind != OutcomeAccept {
		t.Fatal("first should accept")
	}
	if runtime.Evaluate(sampleTrigger("key", "trace2", now.Add(2*time.Second))).Kind != OutcomeAccept {
		t.Fatal("expired duplicate should accept")
	}
}

func TestRuntimeWithConfigKeepsExplicitZeroValuesLikeUpstream(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{})
	config := runtime.Config()
	if config.DedupWindow != 0 || config.CycleHopLimit != 0 {
		t.Fatalf("explicit zero config should be preserved like upstream: %#v", config)
	}
}

func TestRuntimeRecordFollowUpHopSuppressesNextTriggerLikeUpstream(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{DedupWindow: 5 * time.Minute, CycleHopLimit: 2})
	now := time.Now()
	trace := "trace-followup"
	if outcome := runtime.Evaluate(sampleTrigger("key-1", trace, now)); outcome.Kind != OutcomeAccept {
		t.Fatalf("expected first trigger to accept, got %#v", outcome)
	}
	runtime.RecordFollowUpHop(trace, now)
	if outcome := runtime.Evaluate(sampleTrigger("key-2", trace, now)); outcome.Kind != OutcomeCycleSuppressed || outcome.HopCount != 2 {
		t.Fatalf("expected follow-up hop to trigger suppression, got %#v", outcome)
	}
}

func TestRuntimeRecordFollowUpHopSaturatesLikeUpstream(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{DedupWindow: 5 * time.Minute, CycleHopLimit: ^uint32(0)})
	now := time.Now()
	trace := "trace-saturate"
	runtime.cycle[trace] = cycleEntry{HopCount: ^uint32(0), LastSeenAt: now}
	runtime.RecordFollowUpHop(trace, now)
	if got := runtime.cycle[trace].HopCount; got != ^uint32(0) {
		t.Fatalf("follow-up hop should saturate at max uint32 like upstream, got %d", got)
	}
}

func TestRuntimeLifetimeCountersSaturateLikeUpstream(t *testing.T) {
	now := time.Now()

	accepted := NewRuntime(RuntimeConfig{DedupWindow: 5 * time.Minute, CycleHopLimit: 5})
	accepted.snap.AcceptedTotal = ^uint64(0)
	accepted.Evaluate(sampleTrigger("key-accepted", "trace-accepted", now))
	if got := accepted.Snapshot().AcceptedTotal; got != ^uint64(0) {
		t.Fatalf("accepted_total should saturate like upstream, got %d", got)
	}

	deduped := NewRuntime(RuntimeConfig{DedupWindow: 5 * time.Minute, CycleHopLimit: 5})
	deduped.Evaluate(sampleTrigger("key-dedup", "trace-dedup", now))
	deduped.snap.DedupedTotal = ^uint64(0)
	deduped.Evaluate(sampleTrigger("key-dedup", "trace-other", now))
	if got := deduped.Snapshot().DedupedTotal; got != ^uint64(0) {
		t.Fatalf("deduped_total should saturate like upstream, got %d", got)
	}

	suppressed := NewRuntime(RuntimeConfig{DedupWindow: 5 * time.Minute, CycleHopLimit: 0})
	suppressed.snap.CycleSuppressedTotal = ^uint64(0)
	suppressed.Evaluate(sampleTrigger("key-suppressed", "trace-suppressed", now))
	if got := suppressed.Snapshot().CycleSuppressedTotal; got != ^uint64(0) {
		t.Fatalf("cycle_suppressed_total should saturate like upstream, got %d", got)
	}
}

func TestRuntimeDedupedOutcomeCarriesFirstPolicyLikeUpstream(t *testing.T) {
	runtime := NewRuntime(DefaultRuntimeConfig())
	now := time.Now()
	first := sampleTrigger("key-1", "trace-1", now)
	first.ReplacementPolicy = ReplacementLatestReplaces
	if outcome := runtime.Evaluate(first); outcome.Kind != OutcomeAccept {
		t.Fatalf("expected first trigger to accept, got %#v", outcome)
	}
	duplicate := sampleTrigger("key-1", "trace-2", now)
	duplicate.ReplacementPolicy = ReplacementDrop
	if outcome := runtime.Evaluate(duplicate); outcome.Kind != OutcomeDeduped || outcome.ReplacementPolicy != ReplacementLatestReplaces || outcome.PreviousTraceID != "trace-1" {
		t.Fatalf("deduped outcome should carry first arrival policy and trace, got %#v", outcome)
	}
}

func TestParseTriggerRule(t *testing.T) {
	parsed, err := ParseTriggerRule("if build fails, then run go test ./...")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "build fails," || parsed.Action != "go test ./..." {
		t.Fatalf("parse mismatch: %#v", parsed)
	}
	parsed, err = ParseTriggerRule("当 CI 失败时，执行通知我")
	if err != nil || parsed.Condition != "CI 失败" || parsed.Action != "通知我" {
		t.Fatalf("zh parse mismatch parsed=%#v err=%v", parsed, err)
	}
	if _, err := ParseTriggerRule("no separator"); err == nil {
		t.Fatal("expected missing action error")
	} else if err.Error() != "could not split the trigger into a condition and action. In normal chat, ask pie to create the trigger so the model can extract them, or use `/new-trigger if condition, then action`." {
		t.Fatalf("missing action error mismatch: %q", err.Error())
	}
}

func TestParseTriggerRuleStripsOnlyOneConditionPrefixLikeUpstream(t *testing.T) {
	parsed, err := ParseTriggerRule("when if build fails, then run run go test")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "if build fails," || parsed.Action != "run go test" {
		t.Fatalf("parse mismatch: %#v", parsed)
	}
}

func TestParseTriggerRuleStripsChineseWhenAndIfPrefixesLikeUpstream(t *testing.T) {
	parsed, err := ParseTriggerRule("当如果 CI 失败时，执行通知我")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "CI 失败" || parsed.Action != "通知我" {
		t.Fatalf("parse mismatch: %#v", parsed)
	}
}

func TestParseTriggerRuleStripsChineseThenEnglishPrefixLikeUpstream(t *testing.T) {
	parsed, err := ParseTriggerRule("当如果 when build fails, then run notify")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "build fails," || parsed.Action != "notify" {
		t.Fatalf("parse mismatch: %#v", parsed)
	}
}

func TestParseTriggerRuleStripsChineseThenEnglishActionPrefixLikeUpstream(t *testing.T) {
	parsed, err := ParseTriggerRule("如果 build fails, do 执行run notify")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "build fails" || parsed.Action != "notify" {
		t.Fatalf("parse mismatch: %#v", parsed)
	}
}

func TestParseTriggerRuleThenExecuteMarkerOrderLikeUpstream(t *testing.T) {
	parsed, err := ParseTriggerRule("when build fails then execute deploy")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "build fails" || parsed.Action != "deploy" {
		t.Fatalf("parse mismatch: %#v", parsed)
	}
}

func TestParseTriggerRulePreservesConditionTrailingCommaLikeUpstream(t *testing.T) {
	parsed, err := ParseTriggerRule("when build fails, run notify")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "build fails" || parsed.Action != "notify" {
		t.Fatalf("baseline parse mismatch: %#v", parsed)
	}

	parsed, err = ParseTriggerRule("when build fails , run notify")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "build fails" || parsed.Action != "notify" {
		t.Fatalf("space-before-comma should trim to condition without comma: %#v", parsed)
	}

	parsed, err = ParseTriggerRule("when build fails, then notify")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "build fails," || parsed.Action != "notify" {
		t.Fatalf("then marker should preserve condition comma like upstream: %#v", parsed)
	}
}

func TestParseTriggerRuleTrimsRepeatedChineseTimeSuffixLikeUpstream(t *testing.T) {
	parsed, err := ParseTriggerRule("当 CI 失败时时时，执行通知我")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Condition != "CI 失败" || parsed.Action != "通知我" {
		t.Fatalf("parse mismatch: %#v", parsed)
	}
}

func TestDynamicRegistryStorageAndFireOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
	registry := NewDynamicRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	rule, err := registry.AddFromSpec("if tests fail, then run go test")
	if err != nil {
		t.Fatal(err)
	}
	if rule.ID == "" || !rule.Enabled || !rule.FireOnce {
		t.Fatalf("rule mismatch: %#v", rule)
	}
	initialData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(initialData), `"fired_at": null`) {
		t.Fatalf("initial persisted rule should include fired_at null like upstream serde: %s", initialData)
	}
	if _, err := registry.SetRuleEnabled(rule.ID, false); err != nil {
		t.Fatal(err)
	}
	rules := registry.List()
	if len(rules) != 1 || rules[0].Enabled {
		t.Fatalf("disable mismatch: %#v", rules)
	}
	if _, err := registry.SetRuleEnabled(rule.ID, true); err != nil {
		t.Fatal(err)
	}
	changed, err := registry.MarkRulesFired([]string{rule.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0].Enabled || changed[0].FiredAt == nil {
		t.Fatalf("mark fired mismatch: %#v", changed)
	}
	reloaded := NewDynamicRegistry()
	if err := reloaded.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 1 || reloaded.List()[0].FiredAt == nil {
		t.Fatalf("reload mismatch: %#v", reloaded.List())
	}
}

func TestDynamicRegistryStorageDoesNotHTMLEscapeLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
	registry := NewDynamicRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.AddRule("a < b && c > d", "echo ok"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("dynamic trigger storage should match upstream serde_json pretty formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) {
		t.Fatalf("dynamic trigger storage should preserve literal condition, got %s", text)
	}
}

func TestDynamicRegistryLoadRejectsInvalidUTF8LikeUpstreamReadToString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "rules": [
    {
      "id": "dyn-1234567890abcdef1234567890abcdef",
      "condition": "tests fail",
      "action": "run go test",
      "enabled": true,
      "created_at": "2026-01-02T03:04:05Z",
      "note": "`+"\xff"+`"
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewDynamicRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "read dynamic triggers") || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 read error like upstream, got %v", err)
	}
}

func TestDynamicRegistryRemoveRuleReturnsRemovedRuleLikeUpstream(t *testing.T) {
	registry := NewDynamicRegistry()
	rule, err := registry.AddRule("tests fail", "run go test")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := registry.RemoveRule("  " + rule.ID + "  ")
	if err != nil {
		t.Fatal(err)
	}
	if removed == nil || removed.ID != rule.ID || removed.Condition != rule.Condition || removed.Action != rule.Action {
		t.Fatalf("removed rule mismatch: %#v", removed)
	}
	missing, err := registry.RemoveRule(rule.ID)
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("missing removal should return nil, got %#v", missing)
	}
}

func TestDynamicRegistrySetRuleEnabledTrimsIDLikeUpstream(t *testing.T) {
	registry := NewDynamicRegistry()
	rule, err := registry.AddRule("tests fail", "run go test")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := registry.SetRuleEnabled("  "+rule.ID+"  ", false)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || updated.ID != rule.ID || updated.Enabled {
		t.Fatalf("updated mismatch: %#v", updated)
	}
}

func TestDynamicRegistryLoadDefaultsMissingFireOnceLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "rules": [
    {
      "id": "dyn-1234567890abcdef1234567890abcdef",
      "condition": "tests fail",
      "action": "run go test",
      "enabled": true,
      "created_at": "2026-01-02T03:04:05Z"
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewDynamicRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	rules := registry.List()
	if len(rules) != 1 || !rules[0].FireOnce || rules[0].PromoteToChat || rules[0].FiredAt != nil {
		t.Fatalf("defaults mismatch: %#v", rules)
	}
}

func TestDynamicRegistryLoadRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "rules": [
    {
      "id": "dyn-1234567890abcdef1234567890abcdef",
      "condition": "tests fail",
      "action": "run go test",
      "enabled": true
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewDynamicRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "missing dynamic trigger field created_at") {
		t.Fatalf("expected missing created_at error, got %v", err)
	}
}

func TestDynamicRegistryLoadRejectsNullRequiredFieldsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "rules": [
    {
      "id": "dyn-1234567890abcdef1234567890abcdef",
      "condition": "tests fail",
      "action": "run go test",
      "enabled": true,
      "created_at": null
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewDynamicRegistry()
	if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), "null dynamic trigger field created_at") {
		t.Fatalf("expected null created_at error, got %v", err)
	}
}

func TestDynamicRegistryLoadRejectsNullDefaultFieldsLikeUpstream(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
	}{
		{name: "fire_once", field: "fire_once"},
		{name: "promote_to_chat", field: "promote_to_chat"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
			body := fmt.Sprintf(`{
  "version": 1,
  "rules": [
    {
      "id": "dyn-1234567890abcdef1234567890abcdef",
      "condition": "tests fail",
      "action": "run go test",
      "enabled": true,
      "created_at": "2026-01-02T03:04:05Z",
      "%s": null
    }
  ]
}`, tc.field)
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			registry := NewDynamicRegistry()
			want := "null dynamic trigger field " + tc.field
			if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %q error, got %v", want, err)
			}
		})
	}
}

func TestDynamicRegistryLoadRejectsMissingTopLevelFieldsLikeUpstream(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "version", body: `{"rules": []}`, want: "missing dynamic trigger file field version"},
		{name: "rules", body: `{"version": 1}`, want: "missing dynamic trigger file field rules"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			registry := NewDynamicRegistry()
			if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestDynamicRegistryLoadRejectsNullTopLevelFieldsLikeUpstream(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "version", body: `{"version": null, "rules": []}`, want: "null dynamic trigger file field version"},
		{name: "rules", body: `{"version": 1, "rules": null}`, want: "null dynamic trigger file field rules"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			registry := NewDynamicRegistry()
			if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestDynamicRegistryLoadRejectsDuplicateJSONFieldsLikeUpstream(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{name: "top-level", body: `{"version": 1, "version": 1, "rules": []}`, want: "duplicate field `version`"},
		{name: "rule", body: `{"version": 1, "rules": [{"id": "dyn-1234567890abcdef1234567890abcdef", "id": "dyn-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "condition": "tests fail", "action": "run go test", "enabled": true, "created_at": "2026-01-02T03:04:05Z"}]}`, want: "duplicate field `id`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			registry := NewDynamicRegistry()
			if err := registry.LoadFromPath(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestDynamicRegistryLoadIgnoresDuplicateUnknownJSONFieldsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "unknown": "a",
  "unknown": "b",
  "rules": [
    {
      "id": "dyn-1234567890abcdef1234567890abcdef",
      "condition": "tests fail",
      "action": "run go test",
      "enabled": true,
      "fire_once": true,
      "fired_at": null,
      "promote_to_chat": false,
      "created_at": "2026-01-02T03:04:05Z",
      "extra": 1,
      "extra": 2
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewDynamicRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatalf("duplicate unknown fields should be ignored like serde: %v", err)
	}
	if len(registry.List()) != 1 || registry.List()[0].ID != "dyn-1234567890abcdef1234567890abcdef" {
		t.Fatalf("rules mismatch: %#v", registry.List())
	}
}

func TestDynamicRegistryLoadNormalizesDateTimesToUTCLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-triggers.json")
	if err := os.WriteFile(path, []byte(`{
  "version": 1,
  "rules": [
    {
      "id": "dyn-1234567890abcdef1234567890abcdef",
      "condition": "tests fail",
      "action": "run go test",
      "enabled": true,
      "fire_once": true,
      "fired_at": "2026-01-02T11:04:05+08:00",
      "promote_to_chat": false,
      "created_at": "2026-01-02T11:04:05+08:00"
    }
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewDynamicRegistry()
	if err := registry.LoadFromPath(path); err != nil {
		t.Fatal(err)
	}
	rules := registry.List()
	if len(rules) != 1 || rules[0].FiredAt == nil {
		t.Fatalf("rules mismatch: %#v", rules)
	}
	if rules[0].CreatedAt.Format(time.RFC3339) != "2026-01-02T03:04:05Z" || rules[0].FiredAt.Format(time.RFC3339) != "2026-01-02T03:04:05Z" {
		t.Fatalf("times should be normalized to UTC: created=%s fired=%s", rules[0].CreatedAt.Format(time.RFC3339), rules[0].FiredAt.Format(time.RFC3339))
	}
	if _, err := registry.SetRuleEnabled(rules[0].ID, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "+08:00") || !strings.Contains(string(data), `"created_at": "2026-01-02T03:04:05Z"`) || !strings.Contains(string(data), `"fired_at": "2026-01-02T03:04:05Z"`) {
		t.Fatalf("persisted times should stay UTC: %s", data)
	}
}

func TestExtractDynamicRuleIDsFromSummary(t *testing.T) {
	text := "matched dyn-1234567890abcdef1234567890abcdef and dyn-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa plus dyn-short and dyn-1234567890abcdef1234567890abcdef"
	ids := ExtractDynamicRuleIDs(text)
	if len(ids) != 2 || ids[0] != "dyn-1234567890abcdef1234567890abcdef" || ids[1] != "dyn-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("ids mismatch: %#v", ids)
	}
}

func TestHandleDynamicTriggerCompletionMarksFireOnceRules(t *testing.T) {
	registry := NewDynamicRegistry()
	rule, err := registry.AddRule("event says run", "echo run")
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := registry.AddRuleWithOptions("event says repeat", "echo repeat", false)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := HandleDynamicTriggerCompletion(registry, "matched "+rule.ID+" and "+repeat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0].ID != rule.ID || changed[0].Enabled || changed[0].FiredAt == nil {
		t.Fatalf("changed mismatch: %#v", changed)
	}
	rules := registry.List()
	if len(rules) != 2 || rules[0].Enabled || rules[1].ID != repeat.ID || !rules[1].Enabled {
		t.Fatalf("rules mismatch: %#v", rules)
	}
}

func TestRenderDynamicTriggerPromptHonorsPayloadVisibility(t *testing.T) {
	secretTrigger := sampleTrigger("key-local", "trace-local", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	secretTrigger.PayloadVisibility = PayloadLocal
	secretTrigger.Payload = map[string]any{"secret": "do-not-leak"}
	secretTrigger.PayloadSummary = stringPtr("safe summary")
	rules := []DynamicRule{{ID: "dyn-1234567890abcdef1234567890abcdef", Condition: "file changed", Action: "summarize", Enabled: true, FireOnce: true, CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}
	prompt := RenderDynamicTriggerPrompt(secretTrigger, rules)
	if strings.Contains(prompt, "do-not-leak") || !strings.Contains(prompt, "safe summary") || !strings.Contains(prompt, "dyn-1234567890abcdef1234567890abcdef") {
		t.Fatalf("local payload prompt mismatch:\n%s", prompt)
	}
	sharedTrigger := secretTrigger
	sharedTrigger.PayloadVisibility = PayloadShared
	prompt = RenderDynamicTriggerPrompt(sharedTrigger, rules)
	if !strings.Contains(prompt, "do-not-leak") {
		t.Fatalf("shared payload should be included:\n%s", prompt)
	}
}

func TestDynamicTriggerActionUsesEnabledRules(t *testing.T) {
	registry := NewDynamicRegistry()
	disabled, err := registry.AddRule("disabled", "ignore")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetRuleEnabled(disabled.ID, false); err != nil {
		t.Fatal(err)
	}
	enabled, err := registry.AddRule("enabled", "run")
	if err != nil {
		t.Fatal(err)
	}
	action := DynamicTriggerAction(registry, sampleTrigger("key", "trace", time.Now()))
	if action.Delivery != TriggerDeliverySubAgent || action.Promote != PromoteNone || !strings.Contains(action.Prompt, enabled.ID) || strings.Contains(action.Prompt, disabled.ID) {
		t.Fatalf("dynamic action mismatch: %#v", action)
	}
}

func TestDynamicTriggerActionDefaultsWhenNoEnabledRulesLikeUpstream(t *testing.T) {
	registry := NewDynamicRegistry()
	disabled, err := registry.AddRule("disabled", "ignore")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetRuleEnabled(disabled.ID, false); err != nil {
		t.Fatal(err)
	}
	trigger := sampleTrigger("key", "trace", time.Now())
	action := DynamicTriggerAction(registry, trigger)
	if action.Delivery != TriggerDeliverySubAgent || action.Promote != PromoteNone || action.Prompt != "MCP filesystem fired: file changed" {
		t.Fatalf("default action mismatch: %#v", action)
	}
}

func TestDynamicTriggerActionPromotesRequestedRulesLikeUpstream(t *testing.T) {
	registry := NewDynamicRegistry()
	promoted, err := registry.AddRuleWithFlags("important", "tell chat", true, true)
	if err != nil {
		t.Fatal(err)
	}
	auditOnly, err := registry.AddRuleWithFlags("audit", "audit only", true, false)
	if err != nil {
		t.Fatal(err)
	}
	action := DynamicTriggerAction(registry, sampleTrigger("key", "trace", time.Now()))
	if action.Delivery != TriggerDeliverySubAgent || action.Promote != PromoteSummaryWhenSummaryContains || action.PromoteRequiresApproval {
		t.Fatalf("promotion action mismatch: %#v", action)
	}
	if len(action.PromoteRequiredSubstrings) != 1 || action.PromoteRequiredSubstrings[0] != promoted.ID || strings.Contains(strings.Join(action.PromoteRequiredSubstrings, "\n"), auditOnly.ID) || action.PromoteTemplateBody != "" {
		t.Fatalf("promotion condition should include only promoted rule id: %#v", action)
	}
}

func TestDynamicSummaryOnlyActionPromotesSummaryWhenPresent(t *testing.T) {
	trigger := sampleTrigger("key", "trace", time.Now())
	action := DynamicSummaryOnlyAction(trigger)
	if action.Delivery != TriggerDeliveryInjectSummary || action.Promote != PromoteSummaryNow || action.PromoteTemplateBody != "{{trigger.payload_summary}}" || action.PromoteRequiresApproval {
		t.Fatalf("summary action mismatch: %#v", action)
	}
	trigger.PayloadSummary = nil
	action = DynamicSummaryOnlyAction(trigger)
	if action.Delivery != TriggerDeliveryInjectSummary || action.Promote != PromoteNone || action.PromoteTemplateBody != "" {
		t.Fatalf("empty summary action mismatch: %#v", action)
	}
}

func TestDirectInjectDynamicTriggerActionMatchesMCPServersLikeUpstream(t *testing.T) {
	registry := NewDynamicRegistry()
	rule, err := registry.AddRule("fallback", "run fallback")
	if err != nil {
		t.Fatal(err)
	}
	trigger := sampleTrigger("key", "trace", time.Now())
	trigger.Source.ServerName = "filesystem"
	trigger.PayloadSummary = stringPtr("file changed")

	action := DirectInjectDynamicTriggerAction(registry, trigger, map[string]bool{"filesystem": true}, map[string]bool{"filesystem": true})
	if action.Delivery != TriggerDeliveryInjectAndRun || action.Prompt != "file changed" || action.Promote != PromoteNone {
		t.Fatalf("inject_and_run should win for matching server: %#v", action)
	}

	action = DirectInjectDynamicTriggerAction(registry, trigger, map[string]bool{"filesystem": true}, nil)
	if action.Delivery != TriggerDeliveryInjectSummary || action.Promote != PromoteSummaryNow || action.PromoteTemplateBody != "{{trigger.payload_summary}}" {
		t.Fatalf("summary action mismatch: %#v", action)
	}

	trigger.Source.ServerName = "other"
	action = DirectInjectDynamicTriggerAction(registry, trigger, map[string]bool{"filesystem": true}, map[string]bool{"filesystem": true})
	if action.Delivery != TriggerDeliverySubAgent || !strings.Contains(action.Prompt, rule.ID) {
		t.Fatalf("unmatched server should fall through to dynamic action: %#v", action)
	}
}

func TestDynamicCheckAdapterSkipsWhenNoEnabledRules(t *testing.T) {
	registry := NewDynamicRegistry()
	if _, err := registry.AddRule("disabled condition", "disabled action"); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.SetRuleEnabled(registry.List()[0].ID, false); err != nil {
		t.Fatal(err)
	}
	adapter := NewDynamicCheckAdapter(registry, "/repo", time.Minute)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if triggers := adapter.Poll(now); len(triggers) != 0 {
		t.Fatalf("expected no trigger on first poll, got %#v", triggers)
	}
	if triggers := adapter.Poll(now.Add(time.Minute)); len(triggers) != 0 {
		t.Fatalf("expected disabled rules to skip, got %#v", triggers)
	}
}

func TestDynamicCheckAdapterEmitsPeriodicLocalTrigger(t *testing.T) {
	registry := NewDynamicRegistry()
	if _, err := registry.AddRule("interesting local context", "evaluate dynamic rules"); err != nil {
		t.Fatal(err)
	}
	adapter := NewDynamicCheckAdapter(registry, "/repo", time.Minute)
	first := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if triggers := adapter.Poll(first); len(triggers) != 0 {
		t.Fatalf("expected first poll to prime, got %#v", triggers)
	}
	if triggers := adapter.Poll(first.Add(30 * time.Second)); len(triggers) != 0 {
		t.Fatalf("expected interval gate to skip, got %#v", triggers)
	}
	triggers := adapter.Poll(first.Add(time.Minute))
	if len(triggers) != 1 {
		t.Fatalf("expected one trigger, got %#v", triggers)
	}
	trigger := triggers[0]
	if trigger.Source != (Source{Kind: SourceLocal, Subkind: "dynamic"}) || trigger.SourceKind != SourceKindLocal || trigger.SourceLabel != "local:dynamic" || trigger.EventLabel != "dynamic periodic check" {
		t.Fatalf("trigger shape mismatch: %#v", trigger)
	}
	if trigger.PayloadVisibility != PayloadLocal || trigger.Payload != nil || trigger.PayloadSummary == nil {
		t.Fatalf("payload mismatch: %#v", trigger)
	}
	if !strings.Contains(*trigger.PayloadSummary, "1 enabled rule(s)") || !strings.Contains(*trigger.PayloadSummary, "cwd: /repo") || !strings.Contains(*trigger.PayloadSummary, "UTC 2026-01-02T03:05:05Z") {
		t.Fatalf("summary mismatch: %q", *trigger.PayloadSummary)
	}
	if trigger.IDempotencyKey != "local:dynamic:1767323105000" || trigger.ReplacementPolicy != ReplacementDrop || trigger.TraceID == "" || trigger.ReceivedAt != first.Add(time.Minute) {
		t.Fatalf("runtime fields mismatch: %#v", trigger)
	}
	if trigger.Authority.PrincipalID != "local:dynamic" || trigger.Authority.PrincipalLabel != "dynamic trigger checker" || trigger.Authority.CredentialScope != ScopeUser || len(trigger.Authority.AllowedSourceActions) != 0 || trigger.Authority.ExpiresAt != nil {
		t.Fatalf("authority mismatch: %#v", trigger.Authority)
	}
}

func TestDynamicTriggerCheckHookStatusMatchesUpstream(t *testing.T) {
	registry := NewDynamicRegistry()
	hook := NewDynamicTriggerCheckHook(registry)
	if hook.Label() != "local:dynamic" {
		t.Fatalf("label mismatch: %q", hook.Label())
	}
	status := hook.Status()
	if status.State.Kind != HookStateDisconnected || status.State.Reason != "not yet started" || len(status.SubscriptionLabels) != 1 || status.SubscriptionLabels[0] != "dynamic trigger periodic check" {
		t.Fatalf("status mismatch: %#v", status)
	}
}

func TestDynamicTriggerPollIntervalConfigMatchesUpstream(t *testing.T) {
	SetDynamicTriggerPollIntervalSecs(0)
	if got := DynamicTriggerPollIntervalSecs(); got != 1 {
		t.Fatalf("zero interval should clamp to 1 second, got %d", got)
	}
	SetDynamicTriggerPollIntervalSecs(42)
	if got := DynamicTriggerPollIntervalSecs(); got != 42 {
		t.Fatalf("interval mismatch: %d", got)
	}
	hook := NewDynamicTriggerCheckHook(NewDynamicRegistry())
	if hook.adapter.interval != 42*time.Second {
		t.Fatalf("hook interval mismatch: %s", hook.adapter.interval)
	}
	SetDynamicTriggerPollIntervalSecs(uint64(DefaultDynamicTriggerPollInterval / time.Second))
}

func TestDynamicTriggerUpstreamExportedNames(t *testing.T) {
	if DEFAULT_DYNAMIC_TRIGGER_POLL_INTERVAL_SECS != uint64(DefaultDynamicTriggerPollInterval/time.Second) {
		t.Fatalf("default poll interval mismatch: %d", DEFAULT_DYNAMIC_TRIGGER_POLL_INTERVAL_SECS)
	}
	registry := GlobalRegistry()
	if registry != global_registry() {
		t.Fatal("global registry aliases should return the same pointer")
	}
	registry.ClearForTests()
	if len(registry.List()) != 0 || registry.StoragePath() != "" {
		t.Fatalf("clear for tests should reset registry state")
	}
	var rule DynamicTriggerRule
	rule, err := registry.AddRule("build fails", "run tests")
	if err != nil {
		t.Fatal(err)
	}
	var parsed ParsedTriggerRule
	parsed, err = ParseTriggerRule("if build fails, then run tests")
	if err != nil {
		t.Fatal(err)
	}
	if rule.Condition != "build fails" || rule.Action != "run tests" || parsed.Condition == "" || parsed.Action == "" {
		t.Fatalf("rule/parsed alias mismatch: %#v %#v", rule, parsed)
	}
	hook := (&DynamicTriggerCheckHook{}).WithInterval(NewDynamicRegistry(), "/repo", 5*time.Second)
	if hook.adapter.interval != 5*time.Second || hook.adapter.cwd != "/repo" {
		t.Fatalf("with interval mismatch: %#v", hook.adapter)
	}
	var _ *DynamicTriggerRegistry = registry
	var _ DynamicTriggerStorageError = err
	var _ AddTriggerRuleError = err
	var _ ParseTriggerRuleError = err
}

func TestTriggerHookUpstreamFunctionNames(t *testing.T) {
	registry := NewDynamicRegistry()
	summary := "filesystem changed"
	trigger := sampleTrigger("key", "trace", time.Now())
	trigger.Source = Source{Kind: SourceMCP, ServerName: "filesystem", Method: "changed"}
	trigger.PayloadSummary = &summary

	inner := BeforeTriggerActionHook(func(ctx BeforeTriggerActionContext) CronAction {
		return DynamicTriggerAction(registry, ctx.Trigger)
	})
	hook := DirectInjectActionHook(map[string]bool{"filesystem": true}, nil, inner)
	action := hook(BeforeTriggerActionContext{Trigger: trigger})
	if action.Delivery != TriggerDeliveryInjectSummary || action.PromoteTemplateBody != "{{trigger.payload_summary}}" {
		t.Fatalf("direct inject action mismatch: %#v", action)
	}
	if direct_inject_action_hook(map[string]bool{"filesystem": true}, nil, inner)(BeforeTriggerActionContext{Trigger: trigger}).Delivery != TriggerDeliveryInjectSummary {
		t.Fatal("snake_case direct inject alias mismatch")
	}
	if before_trigger_action_hook(registry)(BeforeTriggerActionContext{Trigger: trigger}).Delivery != TriggerDeliverySubAgent {
		t.Fatal("snake_case dynamic trigger action hook mismatch")
	}

	rule, err := registry.AddRule("event says once", "echo once")
	if err != nil {
		t.Fatal(err)
	}
	listener := FireOnceHarnessListener(registry)
	changed := listener(HarnessEvent{Summary: "matched " + rule.ID})
	if len(changed.DynamicRules) != 1 || changed.DynamicRules[0].ID != rule.ID {
		t.Fatalf("fire once listener mismatch changed=%#v", changed)
	}
	if fire_once_harness_listener(registry)(HarnessEvent{Summary: "matched " + rule.ID}).DynamicRules != nil {
		t.Fatal("second fire should have no dynamic changes")
	}
}

func TestDynamicTriggerCheckHookRunChecksOnFirstTickLikeUpstream(t *testing.T) {
	registry := NewDynamicRegistry()
	if _, err := registry.AddRule("something changed", "summarize"); err != nil {
		t.Fatal(err)
	}
	hook := NewDynamicTriggerCheckHookWithInterval(registry, "/repo", time.Hour)
	sink := make(chan Trigger)
	close(sink)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := hook.Run(ctx, sink)
	if hookErr, ok := err.(HookError); !ok || hookErr.Kind != HookErrorSinkClosed {
		t.Fatalf("expected sink closed hook error, got %#v", err)
	}
	status := hook.Status()
	if status.State.Kind != HookStateDisconnected || status.State.Reason != "sink closed" || status.LastError != nil {
		t.Fatalf("status mismatch: %#v", status)
	}
}

func TestDynamicRegistryRepeatRulesAndReactivation(t *testing.T) {
	registry := NewDynamicRegistry()
	repeat, err := registry.AddRuleWithOptions("event says repeat", "echo repeat", false)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := registry.MarkRulesFired([]string{repeat.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 || !registry.List()[0].Enabled || registry.List()[0].FiredAt != nil {
		t.Fatalf("repeat rule should stay enabled: changed=%#v rules=%#v", changed, registry.List())
	}
	fireOnce, err := registry.AddRule("event says once", "echo once")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.MarkRulesFired([]string{fireOnce.ID}); err != nil {
		t.Fatal(err)
	}
	updated, err := registry.SetRuleEnabled(fireOnce.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || !updated.Enabled || updated.FiredAt != nil {
		t.Fatalf("reactivation mismatch: %#v", updated)
	}
}

func sampleTrigger(key, trace string, receivedAt time.Time) Trigger {
	summary := "summary"
	return Trigger{Source: Source{Kind: SourceMCP, ServerName: "fs", Method: "changed"}, SourceKind: SourceKindMCP, SourceLabel: "MCP filesystem", EventLabel: "file changed", PayloadVisibility: PayloadLocal, PayloadSummary: &summary, IDempotencyKey: key, ReplacementPolicy: ReplacementDrop, TraceID: trace, Authority: Authority{PrincipalID: "p1", PrincipalLabel: "user", CredentialScope: ScopeUser}, ReceivedAt: receivedAt}
}

func stringPtr(value string) *string { return &value }
