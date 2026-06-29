package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

type ErrorCode string

type SessionErrorCode = ErrorCode

const (
	ErrorNotFound       ErrorCode = "not_found"
	ErrorAlreadyExists  ErrorCode = "already_exists"
	ErrorStorageFailure ErrorCode = "storage_failure"
	ErrorCorrupted      ErrorCode = "corrupted"
	ErrorAborted        ErrorCode = "aborted"
	ErrorUnknown        ErrorCode = "unknown"

	SessionErrorNotFound       = ErrorNotFound
	SessionErrorAlreadyExists  = ErrorAlreadyExists
	SessionErrorStorageFailure = ErrorStorageFailure
	SessionErrorCorrupted      = ErrorCorrupted
	SessionErrorAborted        = ErrorAborted
	SessionErrorUnknown        = ErrorUnknown
)

type Error struct {
	Code    ErrorCode
	Message string
}

type SessionError = Error

func (err Error) Error() string { return err.Message }

type EntryType string

const (
	EntryTypeMessage             EntryType = "message"
	EntryTypeThinkingLevelChange EntryType = "thinking_level_change"
	EntryTypeModelChange         EntryType = "model_change"
	EntryTypeCompaction          EntryType = "compaction"
	EntryTypeBranchSummary       EntryType = "branch_summary"
	EntryTypeCustom              EntryType = "custom"
	EntryTypeCustomMessage       EntryType = "custom_message"
	EntryTypeLabel               EntryType = "label"
	EntryTypeSessionInfo         EntryType = "session_info"
	EntryTypeLeaf                EntryType = "leaf"

	SessionTreeEntryMessage             = EntryTypeMessage
	SessionTreeEntryThinkingLevelChange = EntryTypeThinkingLevelChange
	SessionTreeEntryModelChange         = EntryTypeModelChange
	SessionTreeEntryCompaction          = EntryTypeCompaction
	SessionTreeEntryBranchSummary       = EntryTypeBranchSummary
	SessionTreeEntryCustom              = EntryTypeCustom
	SessionTreeEntryCustomMessage       = EntryTypeCustomMessage
	SessionTreeEntryLabel               = EntryTypeLabel
	SessionTreeEntrySessionInfo         = EntryTypeSessionInfo
	SessionTreeEntryLeaf                = EntryTypeLeaf
)

type Entry struct {
	EntryType        EntryType      `json:"type"`
	EntryID          string         `json:"id"`
	ParentID         *string        `json:"parentId,omitempty"`
	Timestamp        string         `json:"timestamp"`
	Message          *agent.Message `json:"message,omitempty"`
	ThinkingLevel    string         `json:"thinkingLevel"`
	Provider         string         `json:"provider"`
	ModelID          string         `json:"modelId"`
	Summary          string         `json:"summary"`
	FirstKeptEntryID string         `json:"firstKeptEntryId"`
	TokensBefore     uint64         `json:"tokensBefore"`
	Details          map[string]any `json:"-"`
	DetailsValue     any            `json:"details,omitempty"`
	FromHook         *bool          `json:"fromHook,omitempty"`
	FromID           string         `json:"fromId"`
	CustomType       string         `json:"customType"`
	Data             any            `json:"data,omitempty"`
	Content          any            `json:"content"`
	Display          bool           `json:"display"`
	TargetID         *string        `json:"targetId,omitempty"`
	LabelValue       *string        `json:"label,omitempty"`
	Name             *string        `json:"name,omitempty"`
}

func (entry Entry) ID() string      { return entry.EntryID }
func (entry Entry) Parent() *string { return entry.ParentID }
func (entry Entry) Type() EntryType { return entry.EntryType }

func (entry Entry) ParentId() *string { return entry.ParentID }

func (entry Entry) TypeStr() string { return string(entry.EntryType) }

func NewMessageEntry(id string, parentID *string, timestamp string, message agent.Message) Entry {
	return Entry{EntryType: EntryTypeMessage, EntryID: id, ParentID: parentID, Timestamp: timestamp, Message: &message}
}

func NewCompactionEntry(id string, parentID *string, timestamp, summary, firstKeptEntryID string, tokensBefore uint64, details any, fromHook bool) Entry {
	entry := Entry{EntryType: EntryTypeCompaction, EntryID: id, ParentID: parentID, Timestamp: timestamp, Summary: summary, FirstKeptEntryID: firstKeptEntryID, TokensBefore: tokensBefore, DetailsValue: details}
	if details, ok := details.(map[string]any); ok {
		entry.Details = details
	}
	if fromHook {
		entry.FromHook = boolPtr(true)
	}
	return entry
}

type Context struct {
	Messages      []agent.Message
	ThinkingLevel string
	Model         *ContextModel
}

type ContextModel struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

type Metadata struct {
	ID        string `json:"id"`
	CreatedAt string `json:"createdAt"`
}

type JSONLMetadata struct {
	Metadata
	CWD               string               `json:"cwd"`
	Path              string               `json:"path"`
	ParentSessionPath string               `json:"parentSessionPath,omitempty"`
	ImportedFrom      *SessionImportOrigin `json:"importedFrom,omitempty"`
}

type SessionImportOrigin struct {
	SessionID  string `json:"sessionId"`
	CWD        string `json:"cwd"`
	ExportedAt string `json:"exportedAt"`
	PieVersion string `json:"pieVersion"`
}

type BranchSummaryInput struct {
	Summary  string
	Details  any
	FromHook bool
}

func BuildSessionContext(entries []Entry) Context {
	ctx := Context{ThinkingLevel: "off"}
	compactionIndex := -1
	for index, entry := range entries {
		switch entry.EntryType {
		case EntryTypeThinkingLevelChange:
			ctx.ThinkingLevel = entry.ThinkingLevel
		case EntryTypeModelChange:
			ctx.Model = &ContextModel{Provider: entry.Provider, ModelID: entry.ModelID}
		case EntryTypeMessage:
			if entry.Message != nil && entry.Message.Kind == agent.MessageKindLLM && entry.Message.LLM != nil && entry.Message.LLM.Role == ai.RoleAssistant {
				ctx.Model = &ContextModel{Provider: string(entry.Message.LLM.Provider), ModelID: entry.Message.LLM.Model}
			}
		case EntryTypeCompaction:
			compactionIndex = index
		}
	}
	appendEntry := func(entry Entry) {
		switch entry.EntryType {
		case EntryTypeMessage:
			if entry.Message != nil {
				ctx.Messages = append(ctx.Messages, *entry.Message)
			}
		case EntryTypeCustomMessage:
			timestamp := entryTimestampMillis(entry.Timestamp)
			details := entry.DetailsValue
			if details == nil && entry.Details != nil {
				details = entry.Details
			}
			ctx.Messages = append(ctx.Messages, agent.Message{Kind: agent.MessageKindCustom, Custom: &agent.CustomMessage{Role: entry.CustomType, Timestamp: time.Now().UTC().UnixMilli(), Payload: map[string]any{"content": entry.Content, "details": details, "timestamp": timestamp}}})
		case EntryTypeBranchSummary:
			if entry.Summary != "" {
				ctx.Messages = append(ctx.Messages, agent.Message{Kind: agent.MessageKindCustom, Custom: &agent.CustomMessage{Role: "branch_summary", Timestamp: time.Now().UTC().UnixMilli(), Payload: map[string]any{"summary": entry.Summary}}})
			}
		}
	}
	if compactionIndex >= 0 {
		compaction := entries[compactionIndex]
		ctx.Messages = append(ctx.Messages, agent.Message{Kind: agent.MessageKindCustom, Custom: &agent.CustomMessage{Role: "compaction_summary", Timestamp: time.Now().UTC().UnixMilli(), Payload: map[string]any{"summary": compaction.Summary}}})
		found := false
		for index, entry := range entries {
			if index >= compactionIndex {
				break
			}
			if entry.ID() == compaction.FirstKeptEntryID {
				found = true
			}
			if found {
				appendEntry(entry)
			}
		}
		for _, entry := range entries[compactionIndex+1:] {
			appendEntry(entry)
		}
		return ctx
	}
	for _, entry := range entries {
		appendEntry(entry)
	}
	return ctx
}

func entryTimestampMillis(timestamp string) int64 {
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return time.Now().UTC().UnixMilli()
	}
	return parsed.UnixMilli()
}

func (entry Entry) MarshalJSON() ([]byte, error) {
	if !isKnownEntryType(entry.EntryType) {
		return nil, fmt.Errorf("unknown session entry type %s", entry.EntryType)
	}
	type alias Entry
	data, err := json.Marshal(alias(entry))
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	if entry.DetailsValue == nil && entry.Details != nil {
		object["details"] = entry.Details
	}
	if entry.ParentID == nil {
		object["parentId"] = nil
	}
	if entry.EntryType != EntryTypeCustomMessage {
		delete(object, "content")
		delete(object, "display")
	}
	if entry.EntryType != EntryTypeCompaction {
		delete(object, "tokensBefore")
	}
	if entry.EntryType != EntryTypeBranchSummary {
		delete(object, "fromId")
	}
	if entry.EntryType != EntryTypeModelChange {
		delete(object, "provider")
		delete(object, "modelId")
	}
	if entry.EntryType != EntryTypeThinkingLevelChange {
		delete(object, "thinkingLevel")
	}
	if entry.EntryType != EntryTypeCompaction && entry.EntryType != EntryTypeBranchSummary {
		delete(object, "summary")
	}
	if entry.EntryType != EntryTypeCompaction {
		delete(object, "firstKeptEntryId")
	}
	if entry.EntryType == EntryTypeLabel && entry.TargetID == nil {
		object["targetId"] = ""
	}
	if entry.EntryType == EntryTypeLeaf && entry.TargetID == nil {
		object["targetId"] = nil
	}
	if entry.EntryType != EntryTypeLabel && entry.EntryType != EntryTypeLeaf {
		delete(object, "targetId")
	}
	if entry.EntryType != EntryTypeCustom && entry.EntryType != EntryTypeCustomMessage {
		delete(object, "customType")
	}
	if entry.EntryType != EntryTypeCompaction && entry.EntryType != EntryTypeBranchSummary && entry.EntryType != EntryTypeCustomMessage {
		delete(object, "details")
	}
	if entry.EntryType != EntryTypeCompaction && entry.EntryType != EntryTypeBranchSummary {
		delete(object, "fromHook")
	}
	if entry.EntryType == EntryTypeMessage && entry.Message == nil {
		return nil, fmt.Errorf("session message entry missing message")
	}
	if entry.EntryType != EntryTypeMessage {
		delete(object, "message")
	}
	if entry.EntryType != EntryTypeCustom {
		delete(object, "data")
	}
	if entry.EntryType != EntryTypeLabel {
		delete(object, "label")
	}
	if entry.EntryType != EntryTypeSessionInfo {
		delete(object, "name")
	}
	return marshalJSONNoHTMLEscape(object)
}

func (entry *Entry) UnmarshalJSON(data []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	type alias Entry
	var decoded alias
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if value, ok, err := decodeRawJSONValue(object, "details"); err != nil {
		return err
	} else if ok {
		decoded.DetailsValue = value
		if details, ok := value.(map[string]any); ok {
			decoded.Details = details
		}
	}
	if value, ok, err := decodeRawJSONValue(object, "data"); err != nil {
		return err
	} else if ok {
		decoded.Data = value
	}
	if value, ok, err := decodeRawJSONValue(object, "content"); err != nil {
		return err
	} else if ok {
		decoded.Content = value
	}
	if decoded.DetailsValue != nil {
		if details, ok := decoded.DetailsValue.(map[string]any); ok {
			decoded.Details = details
		}
	}
	if !hasField(object, "id") || !hasField(object, "timestamp") || !isKnownEntryType(decoded.EntryType) {
		return fmt.Errorf("invalid session entry")
	}
	if decoded.EntryType == EntryTypeMessage && !hasField(object, "message") {
		return fmt.Errorf("invalid session entry")
	}
	if decoded.EntryType == EntryTypeThinkingLevelChange && !hasField(object, "thinkingLevel") {
		return fmt.Errorf("invalid session entry")
	}
	if decoded.EntryType == EntryTypeModelChange && (!hasField(object, "provider") || !hasField(object, "modelId")) {
		return fmt.Errorf("invalid session entry")
	}
	if decoded.EntryType == EntryTypeCompaction && !hasFields(object, "summary", "firstKeptEntryId", "tokensBefore") {
		return fmt.Errorf("invalid session entry")
	}
	if decoded.EntryType == EntryTypeBranchSummary && !hasFields(object, "fromId", "summary") {
		return fmt.Errorf("invalid session entry")
	}
	if decoded.EntryType == EntryTypeCustom && !hasField(object, "customType") {
		return fmt.Errorf("invalid session entry")
	}
	if decoded.EntryType == EntryTypeCustomMessage && (!hasField(object, "customType") || !hasKey(object, "content") || !hasField(object, "display")) {
		return fmt.Errorf("invalid session entry")
	}
	if decoded.EntryType == EntryTypeLabel && !hasField(object, "targetId") {
		return fmt.Errorf("invalid session entry")
	}
	*entry = Entry(decoded)
	return nil
}

func hasFields(object map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		if !hasField(object, name) {
			return false
		}
	}
	return true
}

func hasKey(object map[string]json.RawMessage, name string) bool {
	_, ok := object[name]
	return ok
}

func hasField(object map[string]json.RawMessage, name string) bool {
	value, ok := object[name]
	return ok && string(value) != "null"
}

func decodeRawJSONValue(object map[string]json.RawMessage, name string) (any, bool, error) {
	value, ok := object[name]
	if !ok || string(value) == "null" {
		return nil, ok, nil
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, false, err
	}
	return decoded, true, nil
}

func isKnownEntryType(entryType EntryType) bool {
	switch entryType {
	case EntryTypeMessage,
		EntryTypeThinkingLevelChange,
		EntryTypeModelChange,
		EntryTypeCompaction,
		EntryTypeBranchSummary,
		EntryTypeCustom,
		EntryTypeCustomMessage,
		EntryTypeLabel,
		EntryTypeSessionInfo,
		EntryTypeLeaf:
		return true
	default:
		return false
	}
}

func boolPtr(value bool) *bool { return &value }
