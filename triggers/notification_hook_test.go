package triggers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNotificationHookStatusPendingMatchesUpstream(t *testing.T) {
	status := (NotificationHookStatus{}).Pending()
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(data)
	if !strings.Contains(jsonText, `"kind":"disconnected"`) || !strings.Contains(jsonText, `"reason":"not yet started"`) {
		t.Fatalf("pending status should use upstream disconnected state JSON, got %s", jsonText)
	}
	if status.QueuedCount != 0 || status.RequiresAttention != nil || len(status.SubscriptionLabels) != 0 {
		t.Fatalf("pending status defaults mismatch: %#v", status)
	}
}

func TestNotificationHookStatusZeroValueSerializesEmptySubscriptionLabels(t *testing.T) {
	data, err := json.Marshal(NotificationHookStatus{State: HookState{Kind: HookStateConnected}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"subscription_labels":[]`) {
		t.Fatalf("subscription_labels should serialize as [] like upstream Vec, got %s", string(data))
	}
}

func TestNotificationHookStatusNoHTMLEscapeHelperPreservesNestedCustomMarshalJSONLikeSerdeJSON(t *testing.T) {
	status := NotificationHookStatus{State: HookState{Kind: HookStateDisconnected, Reason: "a < b && c > d"}, SubscriptionLabels: []string{"mcp <fs>"}}
	data, err := marshalJSONNoHTMLEscape(status)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("notification hook status JSON should not HTML-escape nested custom MarshalJSON output, got %s", text)
	}
	if !strings.Contains(text, `"reason":"a < b && c > d"`) || !strings.Contains(text, `"mcp <fs>"`) {
		t.Fatalf("notification hook status JSON should preserve literal strings, got %s", text)
	}
}

func TestNotificationHookStatusRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	base := map[string]any{
		"state":               map[string]any{"kind": "connected"},
		"last_event_at":       nil,
		"last_ack_at":         nil,
		"last_error":          nil,
		"queued_count":        float64(0),
		"dropped_count":       float64(0),
		"deduped_count":       float64(0),
		"subscription_labels": []any{},
		"requires_attention":  nil,
	}
	for _, field := range []string{"state", "queued_count", "dropped_count", "deduped_count", "subscription_labels"} {
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
			var status NotificationHookStatus
			if err := json.Unmarshal(data, &status); err == nil {
				t.Fatalf("missing %s should fail like upstream required status field: %#v", field, status)
			}
		})
	}
}

func TestNotificationHookStatusAllowsMissingOptionalFieldsLikeUpstream(t *testing.T) {
	data := []byte(`{"state":{"kind":"connected"},"queued_count":1,"dropped_count":2,"deduped_count":3,"subscription_labels":["cron"]}`)
	var status NotificationHookStatus
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("missing Option fields should decode as nil like upstream serde: %v", err)
	}
	if status.LastEventAt != nil || status.LastAckAt != nil || status.LastError != nil || status.RequiresAttention != nil {
		t.Fatalf("missing optional fields should stay nil, got %#v", status)
	}
}

func TestNotificationHookStatusRejectsNullNonOptionalFieldsLikeUpstream(t *testing.T) {
	base := map[string]any{
		"state":               map[string]any{"kind": "connected"},
		"last_event_at":       nil,
		"last_ack_at":         nil,
		"last_error":          nil,
		"queued_count":        float64(0),
		"dropped_count":       float64(0),
		"deduped_count":       float64(0),
		"subscription_labels": []any{},
		"requires_attention":  nil,
	}
	for _, field := range []string{"state", "queued_count", "dropped_count", "deduped_count", "subscription_labels"} {
		t.Run(field, func(t *testing.T) {
			object := map[string]any{}
			for key, value := range base {
				object[key] = value
			}
			object[field] = nil
			data, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			var status NotificationHookStatus
			if err := json.Unmarshal(data, &status); err == nil {
				t.Fatalf("null %s should fail like upstream non-Option field: %#v", field, status)
			}
		})
	}
}

func TestHookStateJSONRoundTripUsesSnakeCaseKind(t *testing.T) {
	cases := []struct {
		state HookState
		kind  string
	}{
		{HookState{Kind: HookStateConnected}, "connected"},
		{HookState{Kind: HookStateReconnecting}, "reconnecting"},
		{HookState{Kind: HookStateDisconnected, Reason: "broken pipe"}, "disconnected"},
		{HookState{Kind: HookStateDisabled}, "disabled"},
		{HookState{Kind: HookStateAuthFailed, Reason: "401 unauthorized"}, "auth_failed"},
	}
	for _, tc := range cases {
		data, err := json.Marshal(tc.state)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"kind":"`+tc.kind+`"`) {
			t.Fatalf("%#v marshaled as %s, expected kind %q", tc.state, data, tc.kind)
		}
		var decoded HookState
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded != tc.state {
			t.Fatalf("hook state round trip mismatch: got %#v want %#v", decoded, tc.state)
		}
	}
}

func TestHookStateMarshalPayloadVariantsIncludeReasonLikeUpstream(t *testing.T) {
	data, err := json.Marshal(HookState{Kind: HookStateDisconnected})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"reason":""`) {
		t.Fatalf("disconnected payload variant should serialize empty reason like upstream, got %s", string(data))
	}
	data, err = json.Marshal(HookState{Kind: HookStateAuthFailed})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"reason":""`) {
		t.Fatalf("auth_failed payload variant should serialize empty reason like upstream, got %s", string(data))
	}
	data, err = json.Marshal(HookState{Kind: HookStateConnected, Reason: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"reason"`) {
		t.Fatalf("connected unit variant should not serialize reason like upstream, got %s", string(data))
	}
}

func TestHookStateUnmarshalDropsUnitVariantReasonLikeUpstream(t *testing.T) {
	for _, kind := range []string{"connected", "reconnecting", "disabled"} {
		t.Run(kind, func(t *testing.T) {
			var state HookState
			if err := json.Unmarshal([]byte(`{"kind":"`+kind+`","reason":"ignored"}`), &state); err != nil {
				t.Fatal(err)
			}
			if state.Reason != "" {
				t.Fatalf("unit variant %s should drop reason like upstream, got %#v", kind, state)
			}
		})
	}
}

func TestHookStateMarshalRejectsUnknownKindLikeUpstream(t *testing.T) {
	if data, err := json.Marshal(HookState{Kind: HookStateKind("paused")}); err == nil {
		t.Fatalf("unknown hook state kind should fail like upstream enum marshal, got %s", string(data))
	}
}

func TestHookStateRejectsUnknownKindAndMissingReasonLikeUpstream(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "missing_kind", data: `{}`},
		{name: "unknown_kind", data: `{"kind":"paused"}`},
		{name: "disconnected_missing_reason", data: `{"kind":"disconnected"}`},
		{name: "disconnected_null_reason", data: `{"kind":"disconnected","reason":null}`},
		{name: "auth_failed_missing_reason", data: `{"kind":"auth_failed"}`},
		{name: "auth_failed_null_reason", data: `{"kind":"auth_failed","reason":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var state HookState
			if err := json.Unmarshal([]byte(tt.data), &state); err == nil {
				t.Fatalf("%s should fail like upstream HookState serde: %#v", tt.name, state)
			}
		})
	}
}

func TestHookErrorMessagesMatchUpstreamKinds(t *testing.T) {
	cases := []struct {
		err  HookError
		text string
	}{
		{HookError{Kind: HookErrorAuthFailed, Reason: "401"}, "auth failed"},
		{HookError{Kind: HookErrorProtocolMismatch, Reason: "v=2 not supported"}, "protocol mismatch"},
		{HookError{Kind: HookErrorDisconnected, Reason: "closed"}, "disconnected"},
		{HookError{Kind: HookErrorSchemaInvalid, Reason: "bad frame"}, "schema invalid"},
		{HookError{Kind: HookErrorSinkClosed}, "sink closed"},
		{HookError{Kind: HookErrorOther, Reason: "boom"}, "hook error"},
	}
	for _, tc := range cases {
		if !strings.Contains(tc.err.Error(), tc.text) {
			t.Fatalf("expected %q in %q", tc.text, tc.err.Error())
		}
	}
}
