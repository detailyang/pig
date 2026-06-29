package triggers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

type Trigger struct {
	Source            Source            `json:"source"`
	SourceKind        SourceKind        `json:"source_kind"`
	SourceLabel       string            `json:"source_label"`
	EventLabel        string            `json:"event_label"`
	PayloadVisibility PayloadVisibility `json:"payload_visibility"`
	PayloadSummary    *string           `json:"payload_summary"`
	Payload           any               `json:"payload,omitempty"`
	IDempotencyKey    string            `json:"idempotency_key"`
	ReplacementPolicy ReplacementPolicy `json:"replacement_policy"`
	TraceID           string            `json:"trace_id"`
	Authority         Authority         `json:"authority"`
	ReceivedAt        time.Time         `json:"received_at"`
}

func (trigger Trigger) MarshalJSON() ([]byte, error) {
	if !isKnownSourceKind(trigger.SourceKind) {
		return nil, fmt.Errorf("unknown trigger source_kind %s", trigger.SourceKind)
	}
	if !isKnownPayloadVisibility(trigger.PayloadVisibility) {
		return nil, fmt.Errorf("unknown trigger payload_visibility %s", trigger.PayloadVisibility)
	}
	if !isKnownReplacementPolicy(trigger.ReplacementPolicy) {
		return nil, fmt.Errorf("unknown trigger replacement_policy %s", trigger.ReplacementPolicy)
	}
	type wire Trigger
	return marshalJSONNoHTMLEscape(wire(trigger))
}

func (trigger *Trigger) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	type wire Trigger
	var in wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&in); err != nil {
		return err
	}
	for _, field := range []string{"source", "source_kind", "source_label", "event_label", "payload_visibility", "idempotency_key", "replacement_policy", "trace_id", "authority", "received_at"} {
		if !hasJSONField(object, field) {
			return fmt.Errorf("missing required trigger field %s", field)
		}
	}
	if !isKnownSourceKind(in.SourceKind) {
		return fmt.Errorf("unknown trigger source_kind %s", in.SourceKind)
	}
	if !isKnownPayloadVisibility(in.PayloadVisibility) {
		return fmt.Errorf("unknown trigger payload_visibility %s", in.PayloadVisibility)
	}
	if !isKnownReplacementPolicy(in.ReplacementPolicy) {
		return fmt.Errorf("unknown trigger replacement_policy %s", in.ReplacementPolicy)
	}
	if payload, ok := object["payload"]; ok && string(payload) != "null" {
		value, err := decodeJSONValue(payload)
		if err != nil {
			return err
		}
		in.Payload = value
	}
	*trigger = Trigger(in)
	return nil
}

func hasJSONField(object map[string]json.RawMessage, name string) bool {
	value, ok := object[name]
	return ok && string(value) != "null"
}

type Source struct {
	Kind         SourceType `json:"kind"`
	ServerName   string     `json:"server_name,omitempty"`
	Method       string     `json:"method,omitempty"`
	Subkind      string     `json:"subkind,omitempty"`
	AgentID      string     `json:"agent_id,omitempty"`
	DelegationID string     `json:"delegation_id,omitempty"`
}

func (source Source) MarshalJSON() ([]byte, error) {
	switch source.Kind {
	case SourceMCP:
		return marshalJSONNoHTMLEscape(struct {
			Kind       SourceType `json:"kind"`
			ServerName string     `json:"server_name"`
			Method     string     `json:"method"`
		}{Kind: source.Kind, ServerName: source.ServerName, Method: source.Method})
	case SourceLocal:
		return marshalJSONNoHTMLEscape(struct {
			Kind    SourceType `json:"kind"`
			Subkind string     `json:"subkind"`
		}{Kind: source.Kind, Subkind: source.Subkind})
	case SourceAgentDelegate:
		return marshalJSONNoHTMLEscape(struct {
			Kind         SourceType `json:"kind"`
			AgentID      string     `json:"agent_id"`
			DelegationID string     `json:"delegation_id"`
		}{Kind: source.Kind, AgentID: source.AgentID, DelegationID: source.DelegationID})
	default:
		return nil, fmt.Errorf("unknown trigger source kind %s", source.Kind)
	}
}

func (source *Source) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	type wire Source
	var in wire
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	if !hasJSONField(object, "kind") {
		return fmt.Errorf("missing required trigger source field kind")
	}
	var required []string
	switch in.Kind {
	case SourceMCP:
		required = []string{"server_name", "method"}
	case SourceLocal:
		required = []string{"subkind"}
	case SourceAgentDelegate:
		required = []string{"agent_id", "delegation_id"}
	default:
		return fmt.Errorf("unknown trigger source kind %s", in.Kind)
	}
	for _, field := range required {
		if !hasJSONField(object, field) {
			return fmt.Errorf("missing required trigger source field %s", field)
		}
	}
	switch in.Kind {
	case SourceMCP:
		*source = Source{Kind: in.Kind, ServerName: in.ServerName, Method: in.Method}
	case SourceLocal:
		*source = Source{Kind: in.Kind, Subkind: in.Subkind}
	case SourceAgentDelegate:
		*source = Source{Kind: in.Kind, AgentID: in.AgentID, DelegationID: in.DelegationID}
	}
	return nil
}

type TriggerSource = Source

type SourceType string

const (
	SourceMCP           SourceType = "mcp"
	SourceLocal         SourceType = "local"
	SourceAgentDelegate SourceType = "agent_delegate"

	TriggerSourceMcp           = SourceMCP
	TriggerSourceLocal         = SourceLocal
	TriggerSourceAgentDelegate = SourceAgentDelegate
)

type SourceKind string

const (
	SourceKindLocal SourceKind = "local"
	SourceKindMCP   SourceKind = "mcp"
)

func isKnownSourceKind(kind SourceKind) bool {
	switch kind {
	case SourceKindLocal, SourceKindMCP:
		return true
	default:
		return false
	}
}

type PayloadVisibility string

const (
	PayloadLocal    PayloadVisibility = "local"
	PayloadShared   PayloadVisibility = "shared"
	PayloadRedacted PayloadVisibility = "redacted"

	PayloadVisibilityLocal    = PayloadLocal
	PayloadVisibilityShared   = PayloadShared
	PayloadVisibilityRedacted = PayloadRedacted
)

func isKnownPayloadVisibility(visibility PayloadVisibility) bool {
	switch visibility {
	case PayloadLocal, PayloadShared, PayloadRedacted:
		return true
	default:
		return false
	}
}

type Authority struct {
	PrincipalID          string          `json:"principal_id"`
	PrincipalLabel       string          `json:"principal_label"`
	CredentialScope      CredentialScope `json:"credential_scope"`
	AllowedSourceActions []string        `json:"allowed_source_actions"`
	ExpiresAt            *time.Time      `json:"expires_at,omitempty"`
}

func (authority Authority) MarshalJSON() ([]byte, error) {
	if !isKnownCredentialScope(authority.CredentialScope) {
		return nil, fmt.Errorf("unknown trigger authority credential_scope %s", authority.CredentialScope)
	}
	type wire Authority
	out := wire(authority)
	if out.AllowedSourceActions == nil {
		out.AllowedSourceActions = []string{}
	}
	return marshalJSONNoHTMLEscape(out)
}

func (authority *Authority) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	type wire Authority
	var in wire
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	for _, field := range []string{"principal_id", "principal_label", "credential_scope"} {
		if !hasJSONField(object, field) {
			return fmt.Errorf("missing required trigger authority field %s", field)
		}
	}
	if !isKnownCredentialScope(in.CredentialScope) {
		return fmt.Errorf("unknown trigger authority credential_scope %s", in.CredentialScope)
	}
	if value, ok := object["allowed_source_actions"]; ok && string(value) == "null" {
		return fmt.Errorf("null trigger authority field allowed_source_actions")
	}
	*authority = Authority(in)
	if authority.AllowedSourceActions == nil {
		authority.AllowedSourceActions = []string{}
	}
	return nil
}

type TriggerAuthority = Authority

type CredentialScope string

const (
	ScopeUser    CredentialScope = "User"
	ScopeProject CredentialScope = "Project"
	ScopeTeam    CredentialScope = "Team"
	ScopeAgent   CredentialScope = "Agent"
	ScopeNone    CredentialScope = "None"

	CredentialScopeUser    = ScopeUser
	CredentialScopeProject = ScopeProject
	CredentialScopeTeam    = ScopeTeam
	CredentialScopeAgent   = ScopeAgent
	CredentialScopeNone    = ScopeNone
)

func isKnownCredentialScope(scope CredentialScope) bool {
	switch scope {
	case ScopeUser, ScopeProject, ScopeTeam, ScopeAgent, ScopeNone:
		return true
	default:
		return false
	}
}

type ReplacementPolicy string

const (
	ReplacementLatestReplaces ReplacementPolicy = "latest_replaces"
	ReplacementCoalesce       ReplacementPolicy = "coalesce"
	ReplacementDrop           ReplacementPolicy = "drop"

	ReplacementPolicyLatestReplaces = ReplacementLatestReplaces
	ReplacementPolicyCoalesce       = ReplacementCoalesce
	ReplacementPolicyDrop           = ReplacementDrop
)

func isKnownReplacementPolicy(policy ReplacementPolicy) bool {
	switch policy {
	case ReplacementLatestReplaces, ReplacementCoalesce, ReplacementDrop:
		return true
	default:
		return false
	}
}

type State string

type TriggerState = State

const (
	StateReceived         State = "received"
	StateAccepted         State = "accepted"
	StateDeduped          State = "deduped"
	StateCycleSuppressed  State = "cycle_suppressed"
	StatePermissionDenied State = "permission_denied"
	StateNeedsApproval    State = "needs_approval"
	StateRunning          State = "running"
	StateFailed           State = "failed"
	StateCompleted        State = "completed"

	TriggerStateReceived         = StateReceived
	TriggerStateAccepted         = StateAccepted
	TriggerStateDeduped          = StateDeduped
	TriggerStateCycleSuppressed  = StateCycleSuppressed
	TriggerStatePermissionDenied = StatePermissionDenied
	TriggerStateNeedsApproval    = StateNeedsApproval
	TriggerStateRunning          = StateRunning
	TriggerStateFailed           = StateFailed
	TriggerStateCompleted        = StateCompleted
)

func isKnownState(state State) bool {
	switch state {
	case StateReceived, StateAccepted, StateDeduped, StateCycleSuppressed, StatePermissionDenied, StateNeedsApproval, StateRunning, StateFailed, StateCompleted:
		return true
	default:
		return false
	}
}

func (state State) IsTerminal() bool {
	switch state {
	case StateDeduped, StateCycleSuppressed, StatePermissionDenied, StateNeedsApproval, StateFailed, StateCompleted:
		return true
	default:
		return false
	}
}

type Record struct {
	SchemaVersion     uint32            `json:"schema_version"`
	Source            Source            `json:"source"`
	SourceKind        SourceKind        `json:"source_kind"`
	SourceLabel       string            `json:"source_label"`
	EventLabel        string            `json:"event_label"`
	TraceID           string            `json:"trace_id"`
	Authority         Authority         `json:"authority"`
	IDempotencyKey    string            `json:"idempotency_key"`
	ReplacementPolicy ReplacementPolicy `json:"replacement_policy"`
	ReceivedAt        time.Time         `json:"received_at"`
	State             State             `json:"state"`
	PayloadVisibility PayloadVisibility `json:"payload_visibility"`
	PayloadSummary    *string           `json:"payload_summary,omitempty"`
	EvaluatorDecision any               `json:"evaluator_decision,omitempty"`
	ResultLink        *string           `json:"result_link,omitempty"`
	RuleName          *string           `json:"rule_name,omitempty"`
}

func (record Record) MarshalJSON() ([]byte, error) {
	if !isKnownSourceKind(record.SourceKind) {
		return nil, fmt.Errorf("unknown trigger record source_kind %s", record.SourceKind)
	}
	if !isKnownReplacementPolicy(record.ReplacementPolicy) {
		return nil, fmt.Errorf("unknown trigger record replacement_policy %s", record.ReplacementPolicy)
	}
	if !isKnownState(record.State) {
		return nil, fmt.Errorf("unknown trigger record state %s", record.State)
	}
	if !isKnownPayloadVisibility(record.PayloadVisibility) {
		return nil, fmt.Errorf("unknown trigger record payload_visibility %s", record.PayloadVisibility)
	}
	type wire Record
	return marshalJSONNoHTMLEscape(wire(record))
}

func (record *Record) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	type wire Record
	var in wire
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	for _, field := range []string{"schema_version", "source", "source_kind", "source_label", "event_label", "trace_id", "authority", "idempotency_key", "replacement_policy", "received_at", "state", "payload_visibility"} {
		if !hasJSONField(object, field) {
			return fmt.Errorf("missing required trigger record field %s", field)
		}
	}
	if !isKnownSourceKind(in.SourceKind) {
		return fmt.Errorf("unknown trigger record source_kind %s", in.SourceKind)
	}
	if !isKnownReplacementPolicy(in.ReplacementPolicy) {
		return fmt.Errorf("unknown trigger record replacement_policy %s", in.ReplacementPolicy)
	}
	if !isKnownState(in.State) {
		return fmt.Errorf("unknown trigger record state %s", in.State)
	}
	if !isKnownPayloadVisibility(in.PayloadVisibility) {
		return fmt.Errorf("unknown trigger record payload_visibility %s", in.PayloadVisibility)
	}
	if evaluatorDecision, ok := object["evaluator_decision"]; ok && string(evaluatorDecision) != "null" {
		value, err := decodeJSONValue(evaluatorDecision)
		if err != nil {
			return err
		}
		in.EvaluatorDecision = value
	}
	*record = Record(in)
	return nil
}

type TriggerRecord = Record

const TriggerRecordSchemaVersion uint32 = 1

const SCHEMA_VERSION = TriggerRecordSchemaVersion

const TriggerRecordCustomType = "trigger"

func RecordReceivedFrom(trigger Trigger) Record {
	return Record{SchemaVersion: TriggerRecordSchemaVersion, Source: trigger.Source, SourceKind: trigger.SourceKind, SourceLabel: trigger.SourceLabel, EventLabel: trigger.EventLabel, TraceID: trigger.TraceID, Authority: trigger.Authority, IDempotencyKey: trigger.IDempotencyKey, ReplacementPolicy: trigger.ReplacementPolicy, ReceivedAt: trigger.ReceivedAt, State: StateReceived, PayloadVisibility: trigger.PayloadVisibility, PayloadSummary: trigger.PayloadSummary}
}

func (Record) ReceivedFrom(trigger Trigger) Record {
	return RecordReceivedFrom(trigger)
}

func decodeJSONValue(data json.RawMessage) (any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}
