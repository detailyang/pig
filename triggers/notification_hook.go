package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type TriggerSink chan<- Trigger

type NotificationHook interface {
	Label() string
	Run(context.Context, TriggerSink) error
	Status() NotificationHookStatus
}

type DynNotificationHook = NotificationHook

type HookFuture func(context.Context) error

type HookErrorKind string

const (
	HookErrorAuthFailed       HookErrorKind = "auth_failed"
	HookErrorProtocolMismatch HookErrorKind = "protocol_mismatch"
	HookErrorDisconnected     HookErrorKind = "disconnected"
	HookErrorSchemaInvalid    HookErrorKind = "schema_invalid"
	HookErrorSinkClosed       HookErrorKind = "sink_closed"
	HookErrorOther            HookErrorKind = "other"
)

type HookError struct {
	Kind   HookErrorKind
	Reason string
}

func (err HookError) Error() string {
	switch err.Kind {
	case HookErrorAuthFailed:
		return fmt.Sprintf("auth failed: %s", err.Reason)
	case HookErrorProtocolMismatch:
		return fmt.Sprintf("protocol mismatch: %s", err.Reason)
	case HookErrorDisconnected:
		return fmt.Sprintf("disconnected: %s", err.Reason)
	case HookErrorSchemaInvalid:
		return fmt.Sprintf("schema invalid: %s", err.Reason)
	case HookErrorSinkClosed:
		return "sink closed"
	default:
		if err.Reason == "" {
			return "hook error"
		}
		return fmt.Sprintf("hook error: %s", err.Reason)
	}
}

type NotificationHookStatus struct {
	State              HookState  `json:"state"`
	LastEventAt        *time.Time `json:"last_event_at"`
	LastAckAt          *time.Time `json:"last_ack_at"`
	LastError          *string    `json:"last_error"`
	QueuedCount        uint64     `json:"queued_count"`
	DroppedCount       uint64     `json:"dropped_count"`
	DedupedCount       uint64     `json:"deduped_count"`
	SubscriptionLabels []string   `json:"subscription_labels"`
	RequiresAttention  *string    `json:"requires_attention"`
}

func (status NotificationHookStatus) MarshalJSON() ([]byte, error) {
	type wire NotificationHookStatus
	out := wire(status)
	if out.SubscriptionLabels == nil {
		out.SubscriptionLabels = []string{}
	}
	return marshalJSONNoHTMLEscape(out)
}

func (status *NotificationHookStatus) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	type wire NotificationHookStatus
	var in wire
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	for _, field := range []string{"state", "queued_count", "dropped_count", "deduped_count", "subscription_labels"} {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("missing required notification hook status field %s", field)
		}
	}
	for _, field := range []string{"state", "queued_count", "dropped_count", "deduped_count", "subscription_labels"} {
		if string(object[field]) == "null" {
			return fmt.Errorf("null notification hook status field %s", field)
		}
	}
	*status = NotificationHookStatus(in)
	if status.SubscriptionLabels == nil {
		status.SubscriptionLabels = []string{}
	}
	return nil
}

func PendingNotificationHookStatus() NotificationHookStatus {
	return NotificationHookStatus{
		State:              HookState{Kind: HookStateDisconnected, Reason: "not yet started"},
		SubscriptionLabels: []string{},
	}
}

func (NotificationHookStatus) Pending() NotificationHookStatus {
	return PendingNotificationHookStatus()
}

type HookStateKind string

const (
	HookStateConnected    HookStateKind = "connected"
	HookStateReconnecting HookStateKind = "reconnecting"
	HookStateDisconnected HookStateKind = "disconnected"
	HookStateDisabled     HookStateKind = "disabled"
	HookStateAuthFailed   HookStateKind = "auth_failed"
)

type HookState struct {
	Kind   HookStateKind
	Reason string
}

func (state HookState) MarshalJSON() ([]byte, error) {
	object := map[string]any{"kind": state.Kind}
	switch state.Kind {
	case HookStateConnected, HookStateReconnecting, HookStateDisabled:
	case HookStateDisconnected, HookStateAuthFailed:
		object["reason"] = state.Reason
	default:
		return nil, fmt.Errorf("unknown hook state kind %s", state.Kind)
	}
	return marshalJSONNoHTMLEscape(object)
}

func (state *HookState) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var object struct {
		Kind   HookStateKind `json:"kind"`
		Reason string        `json:"reason"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if _, ok := fields["kind"]; !ok {
		return fmt.Errorf("missing required hook state field kind")
	}
	switch object.Kind {
	case HookStateConnected, HookStateReconnecting, HookStateDisabled:
		object.Reason = ""
	case HookStateDisconnected, HookStateAuthFailed:
		reason, ok := fields["reason"]
		if !ok {
			return fmt.Errorf("missing required hook state field reason")
		}
		if string(reason) == "null" {
			return fmt.Errorf("null hook state field reason")
		}
	default:
		return fmt.Errorf("unknown hook state kind %s", object.Kind)
	}
	state.Kind = object.Kind
	state.Reason = object.Reason
	return nil
}
