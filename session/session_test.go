package session

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/config"
	"github.com/detailyang/pig/triggers"
)

func TestSessionErrorUpstreamExportedNames(t *testing.T) {
	var code SessionErrorCode = SessionErrorAborted
	data, err := json.Marshal(code)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"aborted"` {
		t.Fatalf("session error code should marshal snake_case, got %s", data)
	}

	var sessionErr SessionError = Error{Code: SessionErrorUnknown, Message: "boom"}
	if sessionErr.Error() != "boom" {
		t.Fatalf("session error alias mismatch: %q", sessionErr.Error())
	}
}

func TestUUIDv7ProducesTimeOrderedIDsLikeUpstream(t *testing.T) {
	if id := CreateSessionId(); len(id) != 36 || id[14] != '7' {
		t.Fatalf("CreateSessionId alias mismatch: %s", id)
	}
	ids := make([]string, 1024)
	for i := range ids {
		ids[i] = UUIDv7()
		if len(ids[i]) != 36 || ids[i][14] != '7' || !strings.Contains("89ab", string(ids[i][19])) {
			t.Fatalf("uuidv7 format mismatch: %s", ids[i])
		}
		if i > 0 && ids[i-1] >= ids[i] {
			t.Fatalf("uuidv7 should be strictly increasing at index %d: %s >= %s", i, ids[i-1], ids[i])
		}
	}
}

func TestUuidv7UpstreamNameAlias(t *testing.T) {
	id := Uuidv7()
	if len(id) != 36 || id[14] != '7' || !strings.Contains("89ab", string(id[19])) {
		t.Fatalf("Uuidv7 format mismatch: %s", id)
	}
}

func TestCreateTimestampUsesChronoRFC3339UTCOffsetLikeUpstream(t *testing.T) {
	timestamp := CreateTimestamp()
	if !strings.HasSuffix(timestamp, "+00:00") {
		t.Fatalf("timestamp should use chrono UTC offset suffix like upstream, got %q", timestamp)
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		t.Fatalf("timestamp should parse as RFC3339Nano, got %q err=%v", timestamp, err)
	}
}

func TestMemorySessionAppendBranchAndContext(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "s1", CreatedAt: "now"}))
	first, err := sess.AppendMessage(agent.NewUserMessage("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendThinkingLevelChange("high"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendModelChange("openai", "gpt-test"); err != nil {
		t.Fatal(err)
	}
	second, err := sess.AppendMessage(agent.NewAssistantMessage("world"))
	if err != nil {
		t.Fatal(err)
	}

	leaf, err := sess.LeafID()
	if err != nil || leaf == nil || *leaf != second {
		t.Fatalf("leaf mismatch leaf=%v err=%v", leaf, err)
	}
	branch, err := sess.Branch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 4 || branch[0].ID() != first || branch[3].ID() != second {
		t.Fatalf("branch mismatch: %#v", branch)
	}
	ctx, err := sess.BuildContext()
	if err != nil {
		t.Fatal(err)
	}
	if ctx.ThinkingLevel != "high" || ctx.Model == nil || ctx.Model.ModelID != "" {
		t.Fatalf("context metadata mismatch: %#v", ctx)
	}
	if len(ctx.Messages) != 2 {
		t.Fatalf("expected only message entries in context, got %#v", ctx.Messages)
	}
}

func TestBuildSessionContextUsesAssistantMessageModel(t *testing.T) {
	assistant := agent.NewAssistantMessage("served")
	assistant.LLM.Provider = ai.Provider("anthropic")
	assistant.LLM.Model = "claude-test"
	ctx := BuildSessionContext([]Entry{
		NewMessageEntry("u1", nil, "t", agent.NewUserMessage("hello")),
		NewMessageEntry("a1", stringPtr("u1"), "t", assistant),
	})
	if ctx.Model == nil || ctx.Model.Provider != "anthropic" || ctx.Model.ModelID != "claude-test" {
		t.Fatalf("assistant model should update context model: %#v", ctx.Model)
	}
}

func TestBuildSessionContextAssistantMessageModelOverridesEvenWhenEmptyLikeUpstream(t *testing.T) {
	ctx := BuildSessionContext([]Entry{
		{EntryType: EntryTypeModelChange, EntryID: "model", Timestamp: "t", Provider: "openai", ModelID: "gpt-test"},
		NewMessageEntry("assistant", stringPtr("model"), "t", agent.NewAssistantMessage("served")),
	})
	if ctx.Model == nil || ctx.Model.Provider != "" || ctx.Model.ModelID != "" {
		t.Fatalf("assistant messages should override context model even with empty metadata like upstream, got %#v", ctx.Model)
	}
}

func TestBuildSessionContextCustomMessageTimestampMillis(t *testing.T) {
	ctx := BuildSessionContext([]Entry{{EntryType: EntryTypeCustomMessage, EntryID: "c1", Timestamp: "1970-01-01T00:00:01Z", CustomType: "notice", Content: "hello", Details: map[string]any{"level": "info"}}})
	if len(ctx.Messages) != 1 || ctx.Messages[0].Custom == nil {
		t.Fatalf("custom message missing: %#v", ctx.Messages)
	}
	payload := ctx.Messages[0].Custom.Payload.(map[string]any)
	if payload["timestamp"] != int64(1000) {
		t.Fatalf("timestamp should be millis, got %#v", payload)
	}
}

func TestBuildSessionContextCustomMessageUsesCurrentCustomTimestampLikeUpstream(t *testing.T) {
	before := time.Now().UTC().UnixMilli()
	ctx := BuildSessionContext([]Entry{{EntryType: EntryTypeCustomMessage, EntryID: "c1", Timestamp: "1970-01-01T00:00:01Z", CustomType: "notice", Content: "hello"}})
	after := time.Now().UTC().UnixMilli()
	if len(ctx.Messages) != 1 || ctx.Messages[0].Custom == nil {
		t.Fatalf("custom message missing: %#v", ctx.Messages)
	}
	if ctx.Messages[0].Custom.Timestamp < before || ctx.Messages[0].Custom.Timestamp > after {
		t.Fatalf("custom message timestamp should use current helper time like upstream, got %d outside [%d,%d]", ctx.Messages[0].Custom.Timestamp, before, after)
	}
}

func TestBuildSessionContextCustomMessageNilDetailsPayloadLikeUpstream(t *testing.T) {
	ctx := BuildSessionContext([]Entry{{EntryType: EntryTypeCustomMessage, EntryID: "c1", Timestamp: "1970-01-01T00:00:01Z", CustomType: "notice", Content: "hello"}})
	if len(ctx.Messages) != 1 || ctx.Messages[0].Custom == nil {
		t.Fatalf("custom message missing: %#v", ctx.Messages)
	}
	payload := ctx.Messages[0].Custom.Payload.(map[string]any)
	if details, ok := payload["details"]; !ok || details != nil {
		t.Fatalf("custom message payload should include details:null like upstream JSON payload, got %#v", payload)
	}
}

func TestBuildSessionContextCustomMessageUsesArbitraryDetailsValueLikeUpstream(t *testing.T) {
	ctx := BuildSessionContext([]Entry{{EntryType: EntryTypeCustomMessage, EntryID: "c1", Timestamp: "1970-01-01T00:00:01Z", CustomType: "notice", Content: "hello", DetailsValue: []any{"trace"}}})
	if len(ctx.Messages) != 1 || ctx.Messages[0].Custom == nil {
		t.Fatalf("custom message missing: %#v", ctx.Messages)
	}
	payload := ctx.Messages[0].Custom.Payload.(map[string]any)
	details, ok := payload["details"].([]any)
	if !ok || len(details) != 1 || details[0] != "trace" {
		t.Fatalf("custom details should replay arbitrary JSON Value like upstream, got %#v", payload)
	}
}

func TestSessionMessageEntryRejectsNilMessageLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeMessage, EntryID: "m1", Timestamp: "now"}
	if data, err := json.Marshal(entry); err == nil {
		t.Fatalf("message entry without message should fail like upstream AgentMessage field, got %s", data)
	}
}

func TestSessionUpstreamEnumVariantAliases(t *testing.T) {
	if SessionTreeEntryMessage != EntryTypeMessage || SessionTreeEntryThinkingLevelChange != EntryTypeThinkingLevelChange || SessionTreeEntryModelChange != EntryTypeModelChange || SessionTreeEntryBranchSummary != EntryTypeBranchSummary || SessionTreeEntryCustomMessage != EntryTypeCustomMessage || SessionTreeEntrySessionInfo != EntryTypeSessionInfo || SessionTreeEntryLeaf != EntryTypeLeaf {
		t.Fatalf("session tree entry aliases mismatch")
	}
	if ForkPositionBefore != ForkBefore || ForkPositionAt != ForkAt {
		t.Fatalf("fork position aliases mismatch")
	}
}

func TestSessionMessageEntryKeepsMessageWhenPresent(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"message"`) {
		t.Fatalf("message entries should keep message when present, got %s", data)
	}
}

func TestSessionMessageEntrySerializesLLMMessageRoleLikeUpstream(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	message := object["message"].(map[string]any)
	if message["role"] != "user" {
		t.Fatalf("message should serialize as upstream role-tagged LLM message, got %s", data)
	}
	if _, ok := message["Kind"]; ok {
		t.Fatalf("message should not expose Go wrapper Kind field, got %s", data)
	}
	if _, ok := message["LLM"]; ok {
		t.Fatalf("message should not expose Go wrapper LLM field, got %s", data)
	}
}

func TestSessionMessageEntrySerializesLLMMessageTimestampLikeUpstream(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	message := object["message"].(map[string]any)
	if timestamp, ok := message["timestamp"]; !ok || timestamp != float64(0) {
		t.Fatalf("message should serialize required timestamp zero like upstream i64, got %s", data)
	}
}

func TestSessionMessageEntrySerializesSimpleUserContentStringLikeUpstream(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	message := object["message"].(map[string]any)
	if message["content"] != "hello" {
		t.Fatalf("simple user message content should serialize as string like upstream UserContent::Text, got %s", data)
	}
}

func TestSessionMessageEntrySerializesAssistantUsageLikeUpstream(t *testing.T) {
	message := agent.NewAssistantMessage("hello")
	message.LLM.Usage = &ai.Usage{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4}
	entry := NewMessageEntry("m1", nil, "now", message)
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	wireMessage := object["message"].(map[string]any)
	usage, ok := wireMessage["usage"].(map[string]any)
	if !ok {
		t.Fatalf("assistant message should serialize required usage like upstream, got %s", data)
	}
	if usage["input"] != float64(1) || usage["output"] != float64(2) || usage["cacheRead"] != float64(3) || usage["cacheWrite"] != float64(4) || usage["totalTokens"] != float64(0) {
		t.Fatalf("assistant usage should use upstream field names and totals, got %s", data)
	}
	cost, ok := usage["cost"].(map[string]any)
	if !ok {
		t.Fatalf("assistant usage should serialize required cost object like upstream, got %s", data)
	}
	if cost["input"] != float64(0) || cost["output"] != float64(0) || cost["cacheRead"] != float64(0) || cost["cacheWrite"] != float64(0) || cost["total"] != float64(0) {
		t.Fatalf("assistant usage cost should include upstream zero fields, got %s", data)
	}
}

func TestSessionMessageEntrySerializesAssistantStopReasonLikeUpstream(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewAssistantMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	wireMessage := object["message"].(map[string]any)
	if wireMessage["stopReason"] != "stop" {
		t.Fatalf("assistant message should serialize required default stopReason like upstream, got %s", data)
	}
}

func TestSessionMessageEntrySerializesAssistantRequiredCoreFieldsLikeUpstream(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewAssistantMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	wireMessage := object["message"].(map[string]any)
	for _, field := range []string{"api", "provider", "model"} {
		if value, ok := wireMessage[field]; !ok || value != "" {
			t.Fatalf("assistant message should serialize required empty %s like upstream, got %s", field, data)
		}
	}
}

func TestSessionMessageEntrySerializesAssistantDiagnosticsLikeUpstream(t *testing.T) {
	message := agent.NewAssistantMessage("hello")
	message.LLM.Diagnostics = []any{map[string]any{"kind": "trace", "value": float64(1)}}
	entry := NewMessageEntry("m1", nil, "now", message)
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	wireMessage := object["message"].(map[string]any)
	diagnostics := wireMessage["diagnostics"].([]any)
	if len(diagnostics) != 1 || diagnostics[0].(map[string]any)["kind"] != "trace" {
		t.Fatalf("assistant diagnostics should serialize like upstream, got %s", data)
	}
}

func TestSessionMessageEntrySerializesToolCallContentBlockLikeUpstream(t *testing.T) {
	message := agent.NewAssistantMessage("")
	message.LLM.Content = []ai.ContentBlock{{Type: ai.ContentToolCall, ToolCall: &ai.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}}}}
	entry := NewMessageEntry("m1", nil, "now", message)
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	wireMessage := object["message"].(map[string]any)
	content := wireMessage["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "toolCall" || block["id"] != "call-1" || block["name"] != "read" {
		t.Fatalf("tool call block should flatten upstream fields, got %s", data)
	}
	if _, ok := block["toolCall"]; ok {
		t.Fatalf("tool call block should not nest under toolCall field, got %s", data)
	}
}

func TestSessionMessageEntrySerializesToolResultRoleLikeUpstream(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewToolResultMessage(agent.ToolResult{CallID: "call-1", Name: "read", Content: "ok"}))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	message := object["message"].(map[string]any)
	if message["role"] != "toolResult" {
		t.Fatalf("tool result should serialize with upstream role toolResult, got %s", data)
	}
}

func TestSessionMessageEntrySerializesToolResultEmptyTextLikeUpstream(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewToolResultMessage(agent.ToolResult{CallID: "call-1", Name: "read"}))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	message := object["message"].(map[string]any)
	content := message["content"].([]any)
	block := content[0].(map[string]any)
	if value, ok := block["text"]; !ok || value != "" {
		t.Fatalf("tool result text block should include required empty text like upstream, got %s", data)
	}
}

func TestSessionMessageEntryDeserializesToolResultRoleLikeUpstream(t *testing.T) {
	data := []byte(`{"type":"message","id":"m1","timestamp":"now","message":{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[{"type":"text","text":"ok"}],"details":{"exit_code":1},"isError":true,"timestamp":0}}`)
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Message == nil || entry.Message.Kind != agent.MessageKindToolResult || entry.Message.ToolResult == nil {
		t.Fatalf("tool result wire should deserialize as agent tool result, got %#v", entry.Message)
	}
	result := entry.Message.ToolResult
	if result.CallID != "call-1" || result.Name != "read" || result.Content != "ok" || !result.IsError || result.Details["exit_code"] != json.Number("1") {
		t.Fatalf("tool result mismatch: %#v", result)
	}
}

func TestSessionMessageEntryPreservesToolResultContentBlocksLikeUpstream(t *testing.T) {
	data := []byte(`{"type":"message","id":"m1","timestamp":"now","message":{"role":"toolResult","toolCallId":"call-1","toolName":"read","content":[{"type":"text","text":"ok"},{"type":"image","data":"aW1n","mimeType":"image/png"}],"isError":false,"timestamp":0}}`)
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Message == nil || entry.Message.ToolResult == nil || len(entry.Message.ToolResult.ContentBlocks) != 2 {
		t.Fatalf("tool result should preserve content blocks: %#v", entry.Message)
	}
	converted := agent.DefaultConvertToLLM([]agent.Message{*entry.Message})
	if len(converted) != 1 || len(converted[0].Content) != 2 || converted[0].Content[1].Type != ai.ContentImage || converted[0].Content[1].MimeType != "image/png" {
		t.Fatalf("tool result content blocks should survive LLM conversion: %#v", converted)
	}
}

func TestSessionEntryDeserializesEmptyIDLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"message","id":"","timestamp":"now","message":{"Kind":"llm","LLM":{"role":"user","content":[{"type":"text","text":"hello"}],"timestamp":0}}}`), &entry); err != nil {
		t.Fatalf("empty id should deserialize like upstream String field: %v", err)
	}
	if entry.ID() != "" || entry.Type() != EntryTypeMessage {
		t.Fatalf("entry mismatch: %#v", entry)
	}
}

func TestSessionEntryRejectsMissingIDLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"message","timestamp":"now","message":null}`), &entry); err == nil {
		t.Fatalf("missing id should fail like upstream required String field: %#v", entry)
	}
}

func TestSessionEntryRejectsMissingTimestampLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"message","id":"m1","message":null}`), &entry); err == nil {
		t.Fatalf("missing timestamp should fail like upstream required String field: %#v", entry)
	}
}

func TestSessionEntryRejectsNullIDAndTimestampLikeUpstream(t *testing.T) {
	for _, data := range []string{
		`{"type":"message","id":null,"timestamp":"now","message":{"Kind":"llm","LLM":{"role":"user","content":[{"type":"text","text":"hello"}],"timestamp":0}}}`,
		`{"type":"message","id":"m1","timestamp":null,"message":{"Kind":"llm","LLM":{"role":"user","content":[{"type":"text","text":"hello"}],"timestamp":0}}}`,
	} {
		var entry Entry
		if err := json.Unmarshal([]byte(data), &entry); err == nil {
			t.Fatalf("null required String fields should fail like upstream: %#v", entry)
		}
	}
}

func TestSessionEntryRejectsUnknownTypeLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"future_entry","id":"x1","timestamp":"now"}`), &entry); err == nil {
		t.Fatalf("unknown type should fail like upstream tagged enum: %#v", entry)
	}
}

func TestSessionMessageRejectsMissingMessageLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"message","id":"m1","timestamp":"now"}`), &entry); err == nil {
		t.Fatalf("missing message should fail like upstream required AgentMessage field: %#v", entry)
	}
}

func TestSessionCustomMessageAllowsNullContentLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"custom_message","id":"c1","timestamp":"now","customType":"notice","content":null,"display":false}`), &entry); err != nil {
		t.Fatalf("null custom_message content should deserialize like upstream Value field: %v", err)
	}
	if entry.Type() != EntryTypeCustomMessage || entry.Content != nil {
		t.Fatalf("custom message mismatch: %#v", entry)
	}
}

func TestSessionCustomMessageAllowsArrayDetailsLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"custom_message","id":"c1","timestamp":"now","customType":"notice","content":{},"details":["trace"],"display":false}`), &entry); err != nil {
		t.Fatalf("array custom_message details should deserialize like upstream Value field: %v", err)
	}
	if entry.Type() != EntryTypeCustomMessage || len(entry.DetailsValue.([]any)) != 1 {
		t.Fatalf("custom message details mismatch: %#v", entry)
	}
}

func TestSessionAppendCustomMessageAcceptsArrayDetailsLikeUpstream(t *testing.T) {
	sess := NewSession(NewMemorySessionStorage())
	id, err := sess.AppendCustomMessage("notice", "hello", []any{"trace"}, false)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := sess.GetEntry(id)
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil || len(entry.DetailsValue.([]any)) != 1 {
		t.Fatalf("unexpected appended custom_message details: %#v", entry)
	}
}

func TestSessionAppendCompactionAcceptsArrayDetailsLikeUpstream(t *testing.T) {
	sess := NewSession(NewMemorySessionStorage())
	id, err := sess.AppendCompaction("summary", "m1", 10, []any{"trace"}, false)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := sess.GetEntry(id)
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil || len(entry.DetailsValue.([]any)) != 1 {
		t.Fatalf("unexpected appended compaction details: %#v", entry)
	}
}

func TestSessionMoveToAcceptsArraySummaryDetailsLikeUpstream(t *testing.T) {
	sess := NewSession(NewMemorySessionStorage())
	first, err := sess.AppendMessage(agent.NewUserMessage("hello"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := sess.MoveTo(first, &BranchSummaryInput{Summary: "branch", Details: []any{"trace"}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := sess.GetEntry(*id)
	if err != nil {
		t.Fatal(err)
	}
	if entry == nil || len(entry.DetailsValue.([]any)) != 1 {
		t.Fatalf("unexpected branch_summary details: %#v", entry)
	}
}

func TestSessionThinkingLevelChangeRejectsMissingThinkingLevelLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"thinking_level_change","id":"t1","timestamp":"now"}`), &entry); err == nil {
		t.Fatalf("missing thinkingLevel should fail like upstream required String field: %#v", entry)
	}
}

func TestSessionModelChangeRejectsMissingProviderLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"model_change","id":"m1","timestamp":"now","modelId":"gpt-test"}`), &entry); err == nil {
		t.Fatalf("missing provider should fail like upstream required String field: %#v", entry)
	}
}

func TestSessionModelChangeRejectsNullProviderLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"model_change","id":"m1","timestamp":"now","provider":null,"modelId":"gpt-test"}`), &entry); err == nil {
		t.Fatalf("null provider should fail like upstream required String field: %#v", entry)
	}
}

func TestSessionModelChangeRejectsMissingModelIDLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"model_change","id":"m1","timestamp":"now","provider":"openai"}`), &entry); err == nil {
		t.Fatalf("missing modelId should fail like upstream required String field: %#v", entry)
	}
}

func TestSessionCompactionRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "summary", data: `{"type":"compaction","id":"c1","timestamp":"now","firstKeptEntryId":"m1","tokensBefore":0}`},
		{name: "firstKeptEntryId", data: `{"type":"compaction","id":"c1","timestamp":"now","summary":"summary","tokensBefore":0}`},
		{name: "tokensBefore", data: `{"type":"compaction","id":"c1","timestamp":"now","summary":"summary","firstKeptEntryId":"m1"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entry Entry
			if err := json.Unmarshal([]byte(tt.data), &entry); err == nil {
				t.Fatalf("missing %s should fail like upstream required field: %#v", tt.name, entry)
			}
		})
	}
}

func TestSessionBranchSummaryRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "fromId", data: `{"type":"branch_summary","id":"b1","timestamp":"now","summary":"summary"}`},
		{name: "summary", data: `{"type":"branch_summary","id":"b1","timestamp":"now","fromId":"root"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entry Entry
			if err := json.Unmarshal([]byte(tt.data), &entry); err == nil {
				t.Fatalf("missing %s should fail like upstream required String field: %#v", tt.name, entry)
			}
		})
	}
}

func TestSessionCustomRejectsMissingCustomTypeLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"custom","id":"c1","timestamp":"now"}`), &entry); err == nil {
		t.Fatalf("missing customType should fail like upstream required String field: %#v", entry)
	}
}

func TestSessionCustomMessageRejectsMissingRequiredFieldsLikeUpstream(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "customType", data: `{"type":"custom_message","id":"c1","timestamp":"now","content":null,"display":false}`},
		{name: "content", data: `{"type":"custom_message","id":"c1","timestamp":"now","customType":"notice","display":false}`},
		{name: "display", data: `{"type":"custom_message","id":"c1","timestamp":"now","customType":"notice","content":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entry Entry
			if err := json.Unmarshal([]byte(tt.data), &entry); err == nil {
				t.Fatalf("missing %s should fail like upstream required field: %#v", tt.name, entry)
			}
		})
	}
}

func TestSessionLabelRejectsMissingTargetIDLikeUpstream(t *testing.T) {
	var entry Entry
	if err := json.Unmarshal([]byte(`{"type":"label","id":"l1","timestamp":"now"}`), &entry); err == nil {
		t.Fatalf("missing targetId should fail like upstream required String field: %#v", entry)
	}
}

func TestSessionNonMessageEntryOmitsNilMessage(t *testing.T) {
	entry := Entry{EntryType: EntryTypeCustom, EntryID: "c1", Timestamp: "now"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["message"]; ok {
		t.Fatalf("non-message entries should not serialize nil message field, got %s", data)
	}
}

func TestSessionNonMessageEntryOmitsNonNilMessage(t *testing.T) {
	message := agent.NewUserMessage("hello")
	entry := Entry{EntryType: EntryTypeCustom, EntryID: "c1", Timestamp: "now", Message: &message}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["message"]; ok {
		t.Fatalf("non-message entries should not serialize message field even when populated, got %s", data)
	}
}

func TestSessionCustomEntryOmitsNilDataLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeCustom, EntryID: "c1", Timestamp: "now", CustomType: "audit"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"data"`) {
		t.Fatalf("nil custom data should be omitted like upstream Option::None, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedCustomData(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	entry.Data = map[string]any{"key": "value"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["data"]; ok {
		t.Fatalf("message entries should not serialize root custom data field, got %s", data)
	}
}

func TestSessionCustomEntryKeepsDataWhenPresent(t *testing.T) {
	entry := Entry{EntryType: EntryTypeCustom, EntryID: "c1", Timestamp: "now", Data: map[string]any{"key": "value"}}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"data"`) {
		t.Fatalf("custom entries should keep data when present, got %s", data)
	}
}

func TestSessionCustomEntryPreservesRawJSONNumbersLikeUpstreamSerdeValue(t *testing.T) {
	var entry Entry
	data := []byte(`{"type":"custom","id":"c1","parentId":null,"timestamp":"now","customType":"audit","data":{"ticket":9007199254740993}}`)
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(entry.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `9007199254740993`) {
		t.Fatalf("custom data should preserve raw JSON number like upstream serde_json::Value, got %s", encoded)
	}
}

func TestSessionCustomEntrySerializesEmptyCustomTypeLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeCustom, EntryID: "c1", Timestamp: "now"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"customType":""`) {
		t.Fatalf("custom customType is required and should serialize empty string like upstream String, got %s", data)
	}
}

func TestSessionCustomMessageSerializesEmptyCustomTypeLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeCustomMessage, EntryID: "c1", Timestamp: "now"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"customType":""`) {
		t.Fatalf("custom_message customType is required and should serialize empty string like upstream String, got %s", data)
	}
}

func TestSessionCustomMessageSerializesNilContentLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeCustomMessage, EntryID: "c1", Timestamp: "now", CustomType: "notice"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"content":null`) {
		t.Fatalf("custom_message content is required and should serialize null like upstream Value, got %s", data)
	}
}

func TestSessionCustomMessageSerializesFalseDisplayLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeCustomMessage, EntryID: "c1", Timestamp: "now", CustomType: "notice"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"display":false`) {
		t.Fatalf("custom_message display should serialize false like upstream bool, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedCustomMessageContent(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["content"]; ok {
		t.Fatalf("message entries should not serialize root custom_message content field, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedCustomMessageDisplay(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["display"]; ok {
		t.Fatalf("message entries should not serialize root custom_message display field, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedCustomType(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["customType"]; ok {
		t.Fatalf("message entries should not serialize root custom/custom_message customType field, got %s", data)
	}
}

func TestSessionCompactionSerializesZeroTokensBeforeLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeCompaction, EntryID: "c1", Timestamp: "now", Summary: "summary", FirstKeptEntryID: "m1"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"tokensBefore":0`) {
		t.Fatalf("compaction tokensBefore is required and should serialize zero like upstream u64, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedCompactionTokensBefore(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["tokensBefore"]; ok {
		t.Fatalf("message entries should not serialize root compaction tokensBefore field, got %s", data)
	}
}

func TestSessionBranchSummarySerializesEmptyFromIDLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeBranchSummary, EntryID: "b1", Timestamp: "now", Summary: "summary"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"fromId":""`) {
		t.Fatalf("branch_summary fromId is required and should serialize empty string like upstream String, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedBranchSummaryFromID(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["fromId"]; ok {
		t.Fatalf("message entries should not serialize root branch_summary fromId field, got %s", data)
	}
}

func TestSessionModelChangeSerializesEmptyProviderAndModelIDLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeModelChange, EntryID: "mc1", Timestamp: "now"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"provider":""`) || !strings.Contains(string(data), `"modelId":""`) {
		t.Fatalf("model_change provider and modelId are required and should serialize empty strings like upstream String, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedModelChangeFields(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["provider"]; ok {
		t.Fatalf("message entries should not serialize root model_change provider field, got %s", data)
	}
	if _, ok := object["modelId"]; ok {
		t.Fatalf("message entries should not serialize root model_change modelId field, got %s", data)
	}
}

func TestSessionThinkingLevelChangeSerializesEmptyThinkingLevelLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeThinkingLevelChange, EntryID: "tl1", Timestamp: "now"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"thinkingLevel":""`) {
		t.Fatalf("thinking_level_change thinkingLevel is required and should serialize empty string like upstream String, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedThinkingLevelField(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["thinkingLevel"]; ok {
		t.Fatalf("message entries should not serialize root thinking_level_change thinkingLevel field, got %s", data)
	}
}

func TestSessionCompactionSerializesEmptySummaryAndFirstKeptEntryIDLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeCompaction, EntryID: "c1", Timestamp: "now"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"summary":""`) || !strings.Contains(string(data), `"firstKeptEntryId":""`) {
		t.Fatalf("compaction summary and firstKeptEntryId are required and should serialize empty strings like upstream String, got %s", data)
	}
}

func TestSessionBranchSummarySerializesEmptySummaryLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeBranchSummary, EntryID: "b1", Timestamp: "now"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"summary":""`) {
		t.Fatalf("branch_summary summary is required and should serialize empty string like upstream String, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedSummaryFields(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["summary"]; ok {
		t.Fatalf("message entries should not serialize root summary field, got %s", data)
	}
	if _, ok := object["firstKeptEntryId"]; ok {
		t.Fatalf("message entries should not serialize root compaction firstKeptEntryId field, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedDetailsAndFromHook(t *testing.T) {
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	entry.Details = map[string]any{"level": "debug"}
	entry.FromHook = boolPtr(true)
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["details"]; ok {
		t.Fatalf("message entries should not serialize root details field, got %s", data)
	}
	if _, ok := object["fromHook"]; ok {
		t.Fatalf("message entries should not serialize root fromHook field, got %s", data)
	}
}

func TestSessionAllowedEntriesKeepDetailsAndFromHook(t *testing.T) {
	entries := []Entry{
		{EntryType: EntryTypeCompaction, EntryID: "c1", Timestamp: "now", Details: map[string]any{"level": "debug"}, FromHook: boolPtr(true)},
		{EntryType: EntryTypeBranchSummary, EntryID: "b1", Timestamp: "now", Details: map[string]any{"level": "debug"}, FromHook: boolPtr(true)},
		{EntryType: EntryTypeCustomMessage, EntryID: "cm1", Timestamp: "now", Details: map[string]any{"level": "debug"}},
	}
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"details"`) {
			t.Fatalf("%s should keep details when present, got %s", entry.EntryType, data)
		}
		if entry.EntryType != EntryTypeCustomMessage && !strings.Contains(string(data), `"fromHook":true`) {
			t.Fatalf("%s should keep fromHook when present, got %s", entry.EntryType, data)
		}
	}
}

func TestSessionLabelSerializesEmptyTargetIDLikeUpstream(t *testing.T) {
	entry := Entry{EntryType: EntryTypeLabel, EntryID: "l1", Timestamp: "now"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"targetId":""`) {
		t.Fatalf("label targetId is required and should serialize empty string like upstream String, got %s", data)
	}
}

func TestSessionLeafSerializesNilTargetIDLikeUpstreamOption(t *testing.T) {
	entry := Entry{EntryType: EntryTypeLeaf, EntryID: "leaf1", Timestamp: "now"}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"targetId":null`) {
		t.Fatalf("leaf targetId should serialize nil as null like upstream Option field, got %s", data)
	}
}

func TestSessionLeafKeepsTargetIDWhenPresent(t *testing.T) {
	targetID := "m1"
	entry := Entry{EntryType: EntryTypeLeaf, EntryID: "leaf1", Timestamp: "now", TargetID: &targetID}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"targetId":"m1"`) {
		t.Fatalf("leaf should keep targetId when present, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedTargetID(t *testing.T) {
	targetID := "m0"
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	entry.TargetID = &targetID
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["targetId"]; ok {
		t.Fatalf("message entries should not serialize root label/leaf targetId field, got %s", data)
	}
}

func TestSessionMessageEntryOmitsUnrelatedLabelAndName(t *testing.T) {
	label := "start"
	name := "session"
	entry := NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello"))
	entry.LabelValue = &label
	entry.Name = &name
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["label"]; ok {
		t.Fatalf("message entries should not serialize root label field, got %s", data)
	}
	if _, ok := object["name"]; ok {
		t.Fatalf("message entries should not serialize root session_info name field, got %s", data)
	}
}

func TestSessionLabelAndSessionInfoKeepOptionalFields(t *testing.T) {
	label := "start"
	name := "session"
	entries := []Entry{
		{EntryType: EntryTypeLabel, EntryID: "l1", Timestamp: "now", LabelValue: &label},
		{EntryType: EntryTypeSessionInfo, EntryID: "s1", Timestamp: "now", Name: &name},
	}
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if entry.EntryType == EntryTypeLabel && !strings.Contains(string(data), `"label":"start"`) {
			t.Fatalf("label entries should keep label when present, got %s", data)
		}
		if entry.EntryType == EntryTypeSessionInfo && !strings.Contains(string(data), `"name":"session"`) {
			t.Fatalf("session_info entries should keep name when present, got %s", data)
		}
	}
}

func TestSessionMoveToAndLabels(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "s1", CreatedAt: "now"}))
	first, _ := sess.AppendMessage(agent.NewUserMessage("first"))
	second, _ := sess.AppendMessage(agent.NewUserMessage("second"))

	if _, err := sess.AppendLabel(first, stringPtr("start")); err != nil {
		t.Fatal(err)
	}
	label, err := sess.Label(first)
	if err != nil || label == nil || *label != "start" {
		t.Fatalf("label mismatch label=%v err=%v", label, err)
	}
	if _, err := sess.MoveTo(first, &BranchSummaryInput{Summary: "branch from first"}); err != nil {
		t.Fatal(err)
	}
	leaf, _ := sess.LeafID()
	if leaf == nil || *leaf == second {
		t.Fatalf("leaf should move away from second, got %v", leaf)
	}
	branch, err := sess.Branch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if branch[len(branch)-1].Type() != EntryTypeBranchSummary {
		t.Fatalf("expected branch summary at leaf, got %#v", branch[len(branch)-1])
	}
}

func TestSessionBranchWalksParentChainInRootToLeafOrderLikeUpstream(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "s1", CreatedAt: "now"}))
	idA, err := sess.AppendMessage(agent.NewUserMessage("a"))
	if err != nil {
		t.Fatal(err)
	}
	idB, err := sess.AppendMessage(agent.NewUserMessage("b"))
	if err != nil {
		t.Fatal(err)
	}
	idC, err := sess.AppendMessage(agent.NewUserMessage("c"))
	if err != nil {
		t.Fatal(err)
	}
	branch, err := sess.Branch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 3 || branch[0].ID() != idA || branch[1].ID() != idB || branch[2].ID() != idC {
		t.Fatalf("branch should walk parent chain in root-to-leaf order like upstream, got %#v", branch)
	}
}

func TestBuildSessionContextCompactionKeepsSummaryAndTail(t *testing.T) {
	entries := []Entry{
		NewMessageEntry("u1", nil, "t", agent.NewUserMessage("drop")),
		NewMessageEntry("u2", stringPtr("u1"), "t", agent.NewUserMessage("keep")),
		NewCompactionEntry("c1", stringPtr("u2"), "t", "summary", "u2", 100, nil, false),
		NewMessageEntry("a1", stringPtr("c1"), "t", agent.NewAssistantMessage("after")),
	}
	ctx := BuildSessionContext(entries)
	if len(ctx.Messages) != 3 {
		t.Fatalf("context messages mismatch: %#v", ctx.Messages)
	}
	if ctx.Messages[0].Kind != agent.MessageKindCustom || ctx.Messages[0].Custom.Role != "compaction_summary" {
		t.Fatalf("expected compaction summary first, got %#v", ctx.Messages[0])
	}
	if ctx.Messages[1].LLM.Content[0].Text != "keep" || ctx.Messages[2].LLM.Content[0].Text != "after" {
		t.Fatalf("unexpected kept messages: %#v", ctx.Messages)
	}
}

func TestBuildSessionContextCompactionKeepsBranchSummaryFromFirstKeptLikeUpstream(t *testing.T) {
	entries := []Entry{
		NewMessageEntry("u1", nil, "t", agent.NewUserMessage("drop")),
		{EntryType: EntryTypeBranchSummary, EntryID: "b1", ParentID: stringPtr("u1"), Timestamp: "t", FromID: "u1", Summary: "branch kept"},
		NewCompactionEntry("c1", stringPtr("b1"), "t", "compact", "b1", 100, nil, false),
	}
	ctx := BuildSessionContext(entries)
	if len(ctx.Messages) != 2 || ctx.Messages[1].Custom == nil || ctx.Messages[1].Custom.Role != "branch_summary" {
		t.Fatalf("compaction replay should keep branch summary from firstKeptEntryId like upstream, got %#v", ctx.Messages)
	}
}

func TestJsonlSessionStorageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}, CWD: "/tmp/project", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	sess := NewSession(storage)
	id, err := sess.AppendMessage(agent.NewUserMessage("hello"))
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenJSONLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := reopened.GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID() != id {
		t.Fatalf("entries mismatch: %#v", entries)
	}
	meta, err := reopened.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if meta["cwd"] != "/tmp/project" {
		t.Fatalf("metadata mismatch: %#v", meta)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected header + one entry, got %d lines: %s", len(lines), raw)
	}
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header["cwd"] != "/tmp/project" {
		t.Fatalf("header mismatch: %#v", header)
	}
}

func TestOpenJSONLStorageRejectsInvalidUTF8LikeUpstreamReadToString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	header := `{"id":"s1","createdAt":"now","cwd":"/tmp/project","path":"` + path + `","note":"` + "\xff" + `"}` + "\n"
	if err := os.WriteFile(path, []byte(header), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJSONLStorage(path); err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 storage error like upstream, got %v", err)
	}
}

func TestCreateJSONLStorageDefaultsIDToFileStemLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session-from-name.jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{CWD: "/tmp/project"})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := storage.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if metadata["id"] != "session-from-name" {
		t.Fatalf("metadata id should default to file stem like upstream, got %#v", metadata)
	}
}

func TestJSONLAppendMessageDoesNotHTMLEscapeLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}, CWD: "/tmp/project", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	sess := NewSession(storage)
	if _, err := sess.AppendMessage(agent.NewUserMessage("a < b && c > d")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("session JSONL should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) {
		t.Fatalf("session JSONL should preserve literal message text, got %s", text)
	}
}

func TestCreateJSONLStorageStartsWithEmptyEntriesCacheLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}, CWD: "/tmp/project", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	entry := NewMessageEntry("m1", nil, "ts", agent.NewUserMessage("external"))
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := storage.GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("created storage should use initial empty cache, got %#v", entries)
	}
	reopened, err := OpenJSONLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	reopenedEntries, err := reopened.GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(reopenedEntries) != 1 || reopenedEntries[0].ID() != "m1" {
		t.Fatalf("reopened storage should read file entries, got %#v", reopenedEntries)
	}
}

func TestOpenJSONLStorageRejectsMissingRequiredMetadataLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"id":"s1","createdAt":"now","cwd":"/tmp/project"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJSONLStorage(path); err == nil {
		t.Fatal("expected missing metadata path to fail like upstream required String field")
	}
}

func TestOpenJSONLStorageAcceptsEmptyRequiredMetadataStringsLikeUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"id":"","createdAt":"","cwd":"","path":""}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJSONLStorage(path); err != nil {
		t.Fatalf("empty metadata strings should be valid String fields like upstream serde, got %v", err)
	}
}

func TestJSONLExplicitLeafMovesAreOverriddenByNewEntriesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/some/cwd")
	if err != nil {
		t.Fatal(err)
	}
	idA, err := sess.AppendMessage(agent.NewUserMessage("a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("b")); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.MoveTo(idA, nil); err != nil {
		t.Fatal(err)
	}
	idC, err := sess.AppendMessage(agent.NewUserMessage("c"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := repo.Open(files[0])
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := reopened.LeafID()
	if err != nil {
		t.Fatal(err)
	}
	if leaf == nil || *leaf != idC {
		t.Fatalf("new entries should override explicit leaf moves like upstream, got %v want %s", leaf, idC)
	}
	branch, err := reopened.Branch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 2 || branch[0].ID() != idA || branch[1].ID() != idC {
		t.Fatalf("branch after explicit leaf move mismatch: %#v", branch)
	}
}

func TestJSONLCanMoveLeafToRootLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/some/cwd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.MoveTo("", nil); err != nil {
		t.Fatal(err)
	}
	files, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := repo.Open(files[0])
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := reopened.LeafID()
	if err != nil {
		t.Fatal(err)
	}
	if leaf != nil {
		t.Fatalf("leaf should move to root like upstream, got %v", leaf)
	}
	branch, err := reopened.Branch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(branch) != 0 {
		t.Fatalf("root branch should be empty like upstream, got %#v", branch)
	}
}

func TestJsonlSessionStorageReadsEntriesLargerThanScannerLimitLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}, CWD: "/tmp/project", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	largeData := strings.Repeat("x", 2*1024*1024)
	entry := Entry{EntryType: EntryTypeCustom, EntryID: "large", Timestamp: "now", Data: largeData}
	if err := storage.AppendEntry(entry); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenJSONLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := reopened.GetEntries()
	if err != nil {
		t.Fatalf("expected long JSONL entry to load like upstream read_to_string, got %v", err)
	}
	if len(entries) != 1 || entries[0].EntryID != "large" || entries[0].Data != largeData {
		t.Fatalf("long entry mismatch: %#v", entries)
	}
}

func TestCreateJSONLStorageExistingFileReturnsAlreadyExistsLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if _, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}, CWD: "/tmp/project", Path: path}); err != nil {
		t.Fatal(err)
	}
	_, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}, CWD: "/tmp/project", Path: path})
	var sessionErr Error
	if !errors.As(err, &sessionErr) || sessionErr.Code != ErrorAlreadyExists {
		t.Fatalf("expected already_exists error for existing session file, got %v", err)
	}
}

func TestJSONLRepoCreateUsesSameSessionIDForPathAndMetadata(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := sess.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	path, ok := metadata["path"].(string)
	if !ok || path == "" {
		t.Fatalf("metadata path missing: %#v", metadata)
	}
	id, ok := metadata["id"].(string)
	if !ok || id == "" {
		t.Fatalf("metadata id missing: %#v", metadata)
	}
	if filepath.Base(path) != id+".jsonl" {
		t.Fatalf("path/id mismatch path=%q id=%q", path, id)
	}
}

func TestJSONLRepoForkCreatesSessionFileFromSelectedEntries(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := sess.AppendMessage(agent.NewUserMessage("first"))
	second, _ := sess.AppendMessage(agent.NewUserMessage("second"))
	_, _ = sess.AppendMessage(agent.NewAssistantMessage("after"))

	fork, err := repo.Fork(sess, ForkOptions{EntryID: &second, Position: ForkBefore}, "/tmp/fork")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fork.Storage().GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID() != first {
		t.Fatalf("fork entries mismatch: %#v", entries)
	}
	metadata, err := fork.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if metadata["cwd"] != "/tmp/fork" || metadata["parentSessionPath"] == "" {
		t.Fatalf("fork metadata mismatch: %#v", metadata)
	}
	paths, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected source and fork files, got %#v", paths)
	}
}

func TestJSONLRepoDeleteSupportsRelativePath(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := sess.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	path := metadata["path"].(string)
	deleted, err := repo.Delete(filepath.Base(path))
	if err != nil || !deleted {
		t.Fatalf("delete mismatch deleted=%v err=%v", deleted, err)
	}
	deleted, err = repo.Delete(filepath.Base(path))
	if err != nil || deleted {
		t.Fatalf("second delete mismatch deleted=%v err=%v", deleted, err)
	}
	paths, err := repo.List()
	if err != nil || len(paths) != 0 {
		t.Fatalf("list mismatch paths=%#v err=%v", paths, err)
	}
}

func TestJSONLRepoDeleteRemovesSessionSidecarsLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := sess.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	path := metadata["path"].(string)
	for _, sidecar := range []string{TriggerSidecarPath(path), CronSidecarPath(path), EndpointSidecarPath(path)} {
		if err := os.WriteFile(sidecar, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	deleted, err := repo.Delete(filepath.Base(path))
	if err != nil || !deleted {
		t.Fatalf("delete mismatch deleted=%v err=%v", deleted, err)
	}
	for _, sidecar := range []string{TriggerSidecarPath(path), CronSidecarPath(path), EndpointSidecarPath(path)} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("sidecar %s should be removed, stat err=%v", sidecar, err)
		}
	}
}

func TestJSONLRepoFindPathByIDNewestPathAndDeleteByIDLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	first, err := repo.Create("/tmp/first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create("/tmp/second")
	if err != nil {
		t.Fatal(err)
	}
	firstMetadata, err := first.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	secondMetadata, err := second.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	firstID := firstMetadata["id"].(string)
	firstPath := firstMetadata["path"].(string)
	_ = secondMetadata["path"].(string)

	found, err := repo.FindPathByID(firstID)
	if err != nil || found == nil || *found != firstPath {
		t.Fatalf("id path mismatch found=%v err=%v want=%s", found, err, firstPath)
	}
	found, err = repo.FindPathByID(strings.TrimSuffix(filepath.Base(firstPath), filepath.Ext(firstPath)))
	if err != nil || found == nil || *found != firstPath {
		t.Fatalf("stem path mismatch found=%v err=%v want=%s", found, err, firstPath)
	}
	newest, err := repo.NewestPath()
	paths, listErr := repo.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	wantNewest := paths[len(paths)-1]
	if err != nil || newest == nil || *newest != wantNewest {
		t.Fatalf("newest path mismatch newest=%v err=%v want=%s", newest, err, wantNewest)
	}
	for _, sidecar := range []string{TriggerSidecarPath(firstPath), CronSidecarPath(firstPath), EndpointSidecarPath(firstPath)} {
		if err := os.WriteFile(sidecar, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deletedPath, err := repo.DeleteByID(firstID)
	if err != nil || deletedPath == nil || *deletedPath != firstPath {
		t.Fatalf("delete by id mismatch path=%v err=%v want=%s", deletedPath, err, firstPath)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("session file should be removed, stat err=%v", err)
	}
	for _, sidecar := range []string{TriggerSidecarPath(firstPath), CronSidecarPath(firstPath), EndpointSidecarPath(firstPath)} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("sidecar %s should be removed, stat err=%v", sidecar, err)
		}
	}
	missing, err := repo.FindPathByID("missing")
	if err != nil || missing != nil {
		t.Fatalf("missing path mismatch found=%v err=%v", missing, err)
	}
}

func TestJSONLRepoFindPathByIDMatchesMetadataIDLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	path := filepath.Join(dir, "renamed.jsonl")
	metadata := JSONLMetadata{Metadata: Metadata{ID: "019abc-custom-id", CreatedAt: "now"}, CWD: "/tmp/project", Path: path}
	if _, err := CreateJSONLStorage(path, metadata); err != nil {
		t.Fatal(err)
	}
	found, err := repo.FindPathByID("019abc")
	if err != nil || found == nil || *found != path {
		t.Fatalf("metadata id path mismatch found=%v err=%v want=%s", found, err, path)
	}
}

func TestOpenRepoUsesCWDScopedSessionsDirLikeUpstream(t *testing.T) {
	t.Setenv("PIE_DIR", t.TempDir())
	cwd := filepath.Join(t.TempDir(), "project")
	repo := OpenRepo(cwd)
	if got, want := repo.Root(), filepath.Join(os.Getenv("PIE_DIR"), "sessions", config.CWDHash(cwd)); got != want {
		t.Fatalf("repo root mismatch got=%s want=%s", got, want)
	}
}

func TestJSONLRepoResumeNewestAndExplicitIDLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	firstPath := filepath.Join(dir, "01900000-0000-7000-8000-000000000001.jsonl")
	secondPath := filepath.Join(dir, "01900000-0001-7000-8000-000000000002.jsonl")
	first, err := CreateJSONLStorage(firstPath, JSONLMetadata{Metadata: Metadata{ID: "01900000-0000-7000-8000-000000000001", CreatedAt: "now"}, CWD: "/tmp/first", Path: firstPath})
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateJSONLStorage(secondPath, JSONLMetadata{Metadata: Metadata{ID: "01900000-0001-7000-8000-000000000002", CreatedAt: "later"}, CWD: "/tmp/second", Path: secondPath})
	if err != nil {
		t.Fatal(err)
	}
	firstMetadata, err := first.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	secondMetadata, err := second.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}

	resumedNewest, err := repo.Resume(nil)
	if err != nil {
		t.Fatal(err)
	}
	newestMetadata, err := resumedNewest.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if newestMetadata["path"] != secondMetadata["path"] {
		t.Fatalf("newest resume mismatch got=%v want=%v", newestMetadata["path"], secondMetadata["path"])
	}

	explicitID := firstMetadata["id"].(string)[:16]
	resumedExplicit, err := repo.Resume(&explicitID)
	if err != nil {
		t.Fatal(err)
	}
	explicitMetadata, err := resumedExplicit.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if explicitMetadata["path"] != firstMetadata["path"] {
		t.Fatalf("explicit resume mismatch got=%v want=%v", explicitMetadata["path"], firstMetadata["path"])
	}
}

func TestJSONLRepoResumeErrorsLikeUpstream(t *testing.T) {
	repo := NewJSONLRepo(t.TempDir())
	if _, err := repo.Resume(nil); err == nil || !strings.Contains(err.Error(), "no sessions to resume") {
		t.Fatalf("empty resume should error like upstream, got %v", err)
	}
	if _, err := repo.Create("/cwd"); err != nil {
		t.Fatal(err)
	}
	missing := "missing"
	if _, err := repo.Resume(&missing); err == nil || !strings.Contains(err.Error(), "no session matches id missing") {
		t.Fatalf("missing id resume should error like upstream, got %v", err)
	}
}

func TestJSONLRepoDeleteByIDMissingReturnsNilLikeUpstream(t *testing.T) {
	repo := NewJSONLRepo(t.TempDir())
	path, err := repo.DeleteByID("missing")
	if err != nil || path != nil {
		t.Fatalf("missing delete mismatch path=%v err=%v", path, err)
	}
}

func TestJSONLRepoListEntriesMatchesUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/cwd")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := sess.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	path := metadata["path"].(string)
	id := metadata["id"].(string)
	createdAt := metadata["createdAt"].(string)
	longPrompt := "第一行\n" + strings.Repeat("界", 82)
	if _, err := sess.AppendMessage(agent.NewAssistantMessage("ignored")); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage(longPrompt)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TriggerSidecarPath(path), []byte(`{"version":1,"rules":[{"enabled":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := repo.ListEntries()
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatalf("entry count mismatch: %#v", entries)
	}
	preview := "第一行 " + strings.Repeat("界", 76) + "…"
	if entries[0].Path != path || entries[0].ID != id || entries[0].CreatedAt != createdAt || entries[0].Preview == nil || *entries[0].Preview != preview {
		t.Fatalf("entry mismatch: %#v want path=%s id=%s createdAt=%s preview=%q", entries[0], path, id, createdAt, preview)
	}
	if entries[0].Automation.TriggerEnabled != 1 || entries[0].Automation.TriggerTotal != 1 {
		t.Fatalf("automation mismatch: %#v", entries[0].Automation)
	}
}

func TestSessionEntryPreviewUsesFirstUserTextBlocksLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/cwd")
	if err != nil {
		t.Fatal(err)
	}
	message := ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{
		{Type: ai.ContentImage, Data: "abc", MimeType: "image/png"},
		{Type: ai.ContentText, Text: "hello"},
		{Type: ai.ContentText, Text: "world\nagain"},
	}}
	if _, err := sess.AppendMessage(agent.Message{Kind: agent.MessageKindLLM, LLM: &message}); err != nil {
		t.Fatal(err)
	}

	entries, err := repo.ListEntries()
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].Preview == nil || *entries[0].Preview != "hello world again" {
		t.Fatalf("preview mismatch: %#v", entries)
	}
}

func TestSessionEntryPreviewIsNilWithoutUserTextLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/cwd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewAssistantMessage("assistant only")); err != nil {
		t.Fatal(err)
	}

	entries, err := repo.ListEntries()
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 || entries[0].Preview != nil {
		t.Fatalf("preview should be nil without user text: %#v", entries)
	}
}

func TestAutomationCountsReadsSidecarsAndBadgesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	counts := AutomationCountsForSession(sessionPath)
	if !counts.IsEmpty() || counts.Badge() != "" {
		t.Fatalf("missing sidecars should count as empty: %#v badge=%q", counts, counts.Badge())
	}
	if err := os.WriteFile(TriggerSidecarPath(sessionPath), []byte(`{"version":1,"rules":[{"enabled":true},{"enabled":false}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CronSidecarPath(sessionPath), []byte("[[jobs]]\nenabled = true\n\n[[jobs]]\nenabled = false\n\n[[jobs]]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	counts = AutomationCountsForSession(sessionPath)
	if counts.CronTotal != 3 || counts.CronEnabled != 2 || counts.TriggerTotal != 2 || counts.TriggerEnabled != 1 || !counts.AnyEnabled() || counts.Badge() != "2 cron, 1 trigger" {
		t.Fatalf("automation counts mismatch: %#v badge=%q", counts, counts.Badge())
	}
	if alias := automation_counts(sessionPath); alias != counts {
		t.Fatalf("automation_counts alias mismatch: %#v vs %#v", alias, counts)
	}
	if err := os.WriteFile(TriggerSidecarPath(sessionPath), []byte(`{oops`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CronSidecarPath(sessionPath), []byte(`not toml [`), 0o644); err != nil {
		t.Fatal(err)
	}
	counts = AutomationCountsForSession(sessionPath)
	if !counts.IsEmpty() {
		t.Fatalf("corrupt sidecars should degrade to empty counts: %#v", counts)
	}
}

func TestAutomationCountsIgnoreInvalidUTF8SidecarsLikeUpstreamReadToString(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(TriggerSidecarPath(sessionPath), []byte(`{"version":1,"rules":[{"enabled":true,"note":"`+"\xff"+`"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CronSidecarPath(sessionPath), []byte("[[jobs]]\nenabled = true\nnote = \"\xff\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	counts := AutomationCountsForSession(sessionPath)
	if !counts.IsEmpty() {
		t.Fatalf("invalid UTF-8 sidecars should degrade to empty counts like upstream, got %#v", counts)
	}
}

func TestAutomationCountsReadsInlineCronJobsLikeUpstreamTOML(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(CronSidecarPath(sessionPath), []byte(`jobs = [{ enabled = true }, { enabled = false }, {}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	counts := AutomationCountsForSession(sessionPath)

	if counts.CronTotal != 3 || counts.CronEnabled != 1 {
		t.Fatalf("inline cron jobs mismatch: %#v", counts)
	}
}

func TestAutomationCountsBadgeShapesLikeUpstream(t *testing.T) {
	cases := []struct {
		counts AutomationCounts
		badge  string
	}{
		{counts: AutomationCounts{CronEnabled: 2, CronTotal: 2}, badge: "2 cron"},
		{counts: AutomationCounts{TriggerEnabled: 1, TriggerTotal: 3}, badge: "1 trigger"},
		{counts: AutomationCounts{CronTotal: 2, TriggerTotal: 1}, badge: "automation off"},
	}
	for _, tc := range cases {
		if got := tc.counts.Badge(); got != tc.badge {
			t.Fatalf("badge mismatch for %#v got=%q want=%q", tc.counts, got, tc.badge)
		}
	}
}

func TestAutomationElsewhereHintNamesNewestEnabledSessionLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	older, err := repo.Create("/cwd")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := repo.Create("/cwd")
	if err != nil {
		t.Fatal(err)
	}
	olderMetadata, err := older.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	newerMetadata, err := newer.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	olderPath := olderMetadata["path"].(string)
	newerPath := newerMetadata["path"].(string)
	olderID := olderMetadata["id"].(string)

	if hint := AutomationElsewhereHint(repo, &newerPath); hint != "" {
		t.Fatalf("no automation should produce no hint: %q", hint)
	}
	if err := os.WriteFile(CronSidecarPath(olderPath), []byte("[[jobs]]\nenabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hint := AutomationElsewhereHint(repo, &newerPath)
	short := olderID[:16]
	if !strings.Contains(hint, short) || !strings.Contains(hint, "--resume-id") || !strings.Contains(hint, "1 cron enabled") {
		t.Fatalf("automation hint mismatch: %q", hint)
	}
	if hint := AutomationElsewhereHint(repo, &olderPath); hint != "" {
		t.Fatalf("current automation holder should not hint itself: %q", hint)
	}
	if err := os.WriteFile(CronSidecarPath(olderPath), []byte("[[jobs]]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hint := AutomationElsewhereHint(repo, &newerPath); hint != "" {
		t.Fatalf("disabled-only automation should not hint: %q", hint)
	}
}

func TestEndpointSidecarPathMatchesUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-id.jsonl")
	if got, want := EndpointSidecarPath(path), strings.TrimSuffix(path, filepath.Ext(path))+".endpoints.json"; got != want {
		t.Fatalf("endpoint sidecar path mismatch got=%q want=%q", got, want)
	}
}

func TestSidecarPathForSessionUsesMetadataPathLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/cwd")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := sess.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	path := metadata["path"].(string)

	triggerPath, err := TriggerSidecarPathForSession(sess, repo)
	if err != nil {
		t.Fatal(err)
	}
	cronPath, err := CronSidecarPathForSession(sess, repo)
	if err != nil {
		t.Fatal(err)
	}

	if triggerPath != TriggerSidecarPath(path) || cronPath != CronSidecarPath(path) {
		t.Fatalf("sidecar path mismatch trigger=%s cron=%s session=%s", triggerPath, cronPath, path)
	}
}

func TestSidecarPathForSessionFallsBackToRepoRootLikeUpstream(t *testing.T) {
	repo := NewJSONLRepo(t.TempDir())
	sess := NewSession(NewMemoryStorage(Metadata{ID: "sess-123", CreatedAt: "now"}))

	triggerPath, err := TriggerSidecarPathForSession(sess, repo)
	if err != nil {
		t.Fatal(err)
	}
	cronPath, err := CronSidecarPathForSession(sess, repo)
	if err != nil {
		t.Fatal(err)
	}

	if triggerPath != filepath.Join(repo.Root(), "sess-123.triggers.json") || cronPath != filepath.Join(repo.Root(), "sess-123.cron.toml") {
		t.Fatalf("fallback sidecar mismatch trigger=%s cron=%s root=%s", triggerPath, cronPath, repo.Root())
	}
}

func TestJSONLRepoListIncludesJSONLDirectoriesLikeUpstream(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	if err := os.Mkdir(filepath.Join(dir, "folder.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || filepath.Base(paths[0]) != "file.jsonl" || filepath.Base(paths[1]) != "folder.jsonl" {
		t.Fatalf("list should include .jsonl directories like upstream, got %#v", paths)
	}
}

func TestJSONLRepoImportJSONLCopiesEntriesAndOriginMetadata(t *testing.T) {
	sourceDir := t.TempDir()
	sourceRepo := NewJSONLRepo(sourceDir)
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	entryID, _ := source.AppendMessage(agent.NewUserMessage("hello"))
	sourceMetadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := sourceMetadata["path"].(string)
	sourceID := sourceMetadata["id"].(string)

	targetDir := t.TempDir()
	targetRepo := NewJSONLRepo(targetDir)
	imported, err := targetRepo.ImportJSONL(sourcePath, "/tmp/imported", SessionImportOrigin{SessionID: sourceID, CWD: "/tmp/source", ExportedAt: "2026-06-18T00:00:00Z", PieVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := imported.Storage().GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID() != entryID {
		t.Fatalf("imported entries mismatch: %#v", entries)
	}
	metadata, err := imported.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	importedFrom := metadata["importedFrom"].(map[string]any)
	if metadata["cwd"] != "/tmp/imported" || importedFrom["sessionId"] != sourceID || importedFrom["cwd"] != "/tmp/source" || importedFrom["pieVersion"] != "test" {
		t.Fatalf("import metadata mismatch: %#v", metadata)
	}
	if metadata["id"] == sourceID {
		t.Fatalf("import should mint a new session id: %#v", metadata)
	}
}

func TestSessionArchiveExportImportRoundTrip(t *testing.T) {
	sourceDir := t.TempDir()
	sourceRepo := NewJSONLRepo(sourceDir)
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	entryID, _ := source.AppendMessage(agent.NewUserMessage("hello"))
	sourceMetadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := sourceMetadata["path"].(string)
	archivePath := filepath.Join(t.TempDir(), "session.piesession")

	exportSummary, err := ExportArchive(sourcePath, archivePath, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	if exportSummary.SessionID != sourceMetadata["id"] || exportSummary.EntryCount != 1 || exportSummary.OutputPath != archivePath {
		t.Fatalf("export summary mismatch: %#v", exportSummary)
	}
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	files := readTarFiles(t, archiveBytes)
	if len(files["manifest.json"]) == 0 || !bytes.Contains(files["session.jsonl"], []byte(entryID)) {
		t.Fatalf("archive files mismatch: %#v", files)
	}

	targetRepo := NewJSONLRepo(t.TempDir())
	importSummary, err := ImportArchive(targetRepo, archivePath, "/tmp/imported")
	if err != nil {
		t.Fatal(err)
	}
	if importSummary.EntryCount != 1 || importSummary.SessionID == sourceMetadata["id"] || importSummary.SessionPath == "" {
		t.Fatalf("import summary mismatch: %#v", importSummary)
	}
	info, err := os.Stat(importSummary.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("imported session mode mismatch: %o", info.Mode().Perm())
	}
	imported, err := targetRepo.Open(importSummary.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := imported.Storage().GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID() != entryID {
		t.Fatalf("imported entries mismatch: %#v", entries)
	}
	metadata, err := imported.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	importedFrom := metadata["importedFrom"].(map[string]any)
	if metadata["cwd"] != "/tmp/imported" || importedFrom["sessionId"] != sourceMetadata["id"] || importedFrom["cwd"] != "/tmp/source" || importedFrom["pieVersion"] != "test-version" {
		t.Fatalf("import metadata mismatch: %#v", metadata)
	}
}

func TestSessionArchiveManifestDoesNotHTMLEscapeLikeUpstream(t *testing.T) {
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/a < b && c > d")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(metadata["path"].(string), archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}

	files := readTarFiles(t, mustReadFile(t, archivePath))
	manifestText := string(files[archiveManifestPath])
	if strings.Contains(manifestText, `\u003c`) || strings.Contains(manifestText, `\u003e`) || strings.Contains(manifestText, `\u0026`) {
		t.Fatalf("manifest should match upstream serde_json formatting without HTML escaping, got %s", manifestText)
	}
	if !strings.Contains(manifestText, `/tmp/a < b && c > d`) {
		t.Fatalf("manifest should preserve literal cwd, got %s", manifestText)
	}
}

func TestSessionArchiveUpstreamAPIAliases(t *testing.T) {
	dir := t.TempDir()
	repo := NewJSONLRepo(dir)
	sess, err := repo.Create("/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage(agent.NewUserMessage("hello")); err != nil {
		t.Fatal(err)
	}
	metadata, err := sess.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	sessionPath := metadata["path"].(string)
	archivePath := filepath.Join(t.TempDir(), "session.piesession")

	exported, err := ExportSession(sessionPath, archivePath, true, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	if exported.OutputPath != archivePath || exported.EntryCount != 1 || exported.SessionID == "" {
		t.Fatalf("export summary mismatch: %#v", exported)
	}

	var activation ActivateTriggers = ActivateTriggersOff
	imported, err := ImportSession(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported", activation)
	if err != nil {
		t.Fatal(err)
	}
	if imported.EntryCount != 1 || imported.AutomationEnabled {
		t.Fatalf("import summary mismatch: %#v", imported)
	}
}

func TestSessionArchiveImportRejectsChecksumMismatch(t *testing.T) {
	sourceDir := t.TempDir()
	sourceRepo := NewJSONLRepo(sourceDir)
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(metadata["path"].(string), archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}
	files := readTarFiles(t, mustReadFile(t, archivePath))
	files["session.jsonl"] = append(files["session.jsonl"], []byte("\n")...)
	tamperedPath := filepath.Join(t.TempDir(), "tampered.piesession")
	writeTarFiles(t, tamperedPath, files)

	_, err = ImportArchive(NewJSONLRepo(t.TempDir()), tamperedPath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestSessionArchiveExportIncludesAutomationSidecars(t *testing.T) {
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := metadata["path"].(string)
	triggerSidecar := []byte(`{"version":1,"rules":[{"id":"dyn-1","condition":"new file","action":"summarize","enabled":true,"fire_once":true,"promote_to_chat":false,"created_at":"2026-01-02T03:04:05Z"}]}`)
	cronSidecar := []byte(`[{"id":"cron-1","kind":"cron","label":"Daily","prompt":"summarize","every":60000000000,"enabled":true,"created_at":"2026-01-02T03:04:05Z","updated_at":"2026-01-02T03:04:05Z"}]` + "\n")
	if err := os.WriteFile(TriggerSidecarPath(sourcePath), triggerSidecar, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CronSidecarPath(sourcePath), cronSidecar, 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")

	summary, err := ExportArchive(sourcePath, archivePath, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	if !summary.HasTriggers || !summary.HasCron {
		t.Fatalf("export summary should report sidecars: %#v", summary)
	}
	files := readTarFiles(t, mustReadFile(t, archivePath))
	if !bytes.Equal(files[archiveTriggersPath], triggerSidecar) || !bytes.Equal(files[archiveCronPath], cronSidecar) {
		t.Fatalf("archive sidecars mismatch: %#v", files)
	}
	var manifest archiveManifest
	if err := json.Unmarshal(files[archiveManifestPath], &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.Content.HasTriggers || !manifest.Content.HasCron {
		t.Fatalf("manifest should report sidecars: %#v", manifest.Content)
	}
}

func TestSessionArchiveExportCanExcludeAutomationSidecarsLikeUpstream(t *testing.T) {
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := metadata["path"].(string)
	if err := os.WriteFile(TriggerSidecarPath(sourcePath), []byte(`{"version":1,"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CronSidecarPath(sourcePath), []byte(`jobs = []`), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")

	summary, err := ExportArchiveWithOptions(sourcePath, archivePath, "test-version", ExportArchiveOptions{ExcludeTriggers: true})
	if err != nil {
		t.Fatal(err)
	}
	if summary.HasTriggers || summary.HasCron {
		t.Fatalf("summary should exclude sidecars: %#v", summary)
	}
	files := readTarFiles(t, mustReadFile(t, archivePath))
	if _, ok := files[archiveTriggersPath]; ok {
		t.Fatalf("archive should not include trigger sidecar: %#v", files)
	}
	if _, ok := files[archiveCronPath]; ok {
		t.Fatalf("archive should not include cron sidecar: %#v", files)
	}
	var manifest archiveManifest
	if err := json.Unmarshal(files[archiveManifestPath], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Content.HasTriggers || manifest.Content.HasCron {
		t.Fatalf("manifest should record excluded sidecars: %#v", manifest.Content)
	}
}

func TestSessionArchiveExportWritesPrettyManifestLikeUpstream(t *testing.T) {
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(metadata["path"].(string), archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}

	files := readTarFiles(t, mustReadFile(t, archivePath))
	manifest := string(files[archiveManifestPath])
	if !strings.Contains(manifest, "\n  \"schema\": ") || !strings.Contains(manifest, "\n  \"content\": {") {
		t.Fatalf("manifest should be pretty JSON like upstream, got %q", manifest)
	}
}

func TestSessionArchiveExportUsesGNUTarHeadersLikeUpstream(t *testing.T) {
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(metadata["path"].(string), archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}

	reader := tar.NewReader(bytes.NewReader(mustReadFile(t, archivePath)))
	header, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Format != tar.FormatGNU {
		t.Fatalf("archive entries should use GNU tar headers like upstream, got %v", header.Format)
	}
}

func TestDefaultExportPathMatchesUpstream(t *testing.T) {
	path := DefaultExportPath("/tmp/work", "1234567890abcdef-extra")
	if path != filepath.Join("/tmp/work", "pie-session-1234567890abcdef.piesession") {
		t.Fatalf("default export path mismatch: %q", path)
	}
	unicodePath := DefaultExportPath("/tmp/work", "会话会话会话会话会话会话会话会话-extra")
	if unicodePath != filepath.Join("/tmp/work", "pie-session-会话会话会话会话会话会话会话会话.piesession") {
		t.Fatalf("default export path should truncate by characters like upstream, got %q", unicodePath)
	}
}

func TestSessionArchiveExportManifestUsesLastEntryAsLeafLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "source.jsonl")
	metadata := JSONLMetadata{Metadata: Metadata{ID: "source-session", CreatedAt: "2026-01-02T03:04:05Z"}, CWD: "/tmp/source", Path: sessionPath}
	entry := NewMessageEntry("m1", nil, "2026-01-02T03:04:06Z", agent.NewUserMessage("hello"))
	writeSessionJSONL(t, sessionPath, metadata, []Entry{entry})

	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(sessionPath, archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}
	manifest := archiveManifestFromPath(t, archivePath)
	if manifest.Content.ActiveLeafID == nil || *manifest.Content.ActiveLeafID != "m1" {
		t.Fatalf("active leaf should default to last non-leaf entry like upstream: %#v", manifest.Content.ActiveLeafID)
	}
}

func TestSessionArchiveExportManifestUsesLeafTargetLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "source.jsonl")
	metadata := JSONLMetadata{Metadata: Metadata{ID: "source-session", CreatedAt: "2026-01-02T03:04:05Z"}, CWD: "/tmp/source", Path: sessionPath}
	entry := NewMessageEntry("m1", nil, "2026-01-02T03:04:06Z", agent.NewUserMessage("hello"))
	leaf := Entry{EntryType: EntryTypeLeaf, EntryID: "leaf-row", ParentID: stringPtr("m1"), Timestamp: "2026-01-02T03:04:07Z", TargetID: stringPtr("m1")}
	writeSessionJSONL(t, sessionPath, metadata, []Entry{entry, leaf})

	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(sessionPath, archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}
	manifest := archiveManifestFromPath(t, archivePath)
	if manifest.Content.ActiveLeafID == nil || *manifest.Content.ActiveLeafID != "m1" {
		t.Fatalf("active leaf should use leaf target like upstream: %#v", manifest.Content.ActiveLeafID)
	}
}

func TestSessionArchiveImportWritesDisabledAutomationSidecars(t *testing.T) {
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := metadata["path"].(string)
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	triggerSidecar, err := json.Marshal(struct {
		Version uint32                 `json:"version"`
		Rules   []triggers.DynamicRule `json:"rules"`
	}{Version: 1, Rules: []triggers.DynamicRule{{ID: "dyn-1", Condition: "new file", Action: "summarize", Enabled: true, FireOnce: true, CreatedAt: createdAt}}})
	if err != nil {
		t.Fatal(err)
	}
	cronSidecar := []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`)
	if err := os.WriteFile(TriggerSidecarPath(sourcePath), triggerSidecar, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CronSidecarPath(sourcePath), cronSidecar, 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(sourcePath, archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatal(err)
	}
	if importSummary.TriggersImported != 1 || importSummary.CronImported != 1 || importSummary.AutomationEnabled {
		t.Fatalf("import summary mismatch: %#v", importSummary)
	}
	if len(importSummary.OriginallyEnabledTriggers) != 1 || importSummary.OriginallyEnabledTriggers[0] != "dyn-1" || len(importSummary.OriginallyEnabledCron) != 1 || importSummary.OriginallyEnabledCron[0] != "cron-1" {
		t.Fatalf("originally enabled summary mismatch: %#v", importSummary)
	}

	var importedTriggers struct {
		Rules []triggers.DynamicRule `json:"rules"`
	}
	triggerBytes := mustReadFile(t, TriggerSidecarPath(importSummary.SessionPath))
	if bytes.HasSuffix(triggerBytes, []byte("\n")) {
		t.Fatalf("imported trigger sidecar should not end with newline like upstream")
	}
	if err := json.Unmarshal(triggerBytes, &importedTriggers); err != nil {
		t.Fatal(err)
	}
	if len(importedTriggers.Rules) != 1 || importedTriggers.Rules[0].Enabled {
		t.Fatalf("imported triggers should be disabled: %#v", importedTriggers.Rules)
	}
	importedCron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(importedCron, `id = "cron-1"`) || !strings.Contains(importedCron, `enabled = false`) {
		t.Fatalf("imported cron should be disabled:\n%s", importedCron)
	}
}

func TestSessionArchiveImportRejectsInvalidUTF8TriggerSidecarLikeUpstream(t *testing.T) {
	triggerSidecar := []byte("{\"version\":1,\"rules\":[{\"id\":\"dyn-1\",\"condition\":\"\xff\"}]}")
	archivePath := makeArchiveWithSidecars(t, triggerSidecar, nil)

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse trigger sidecar") || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 trigger sidecar parse error like upstream, got %v", err)
	}
}

func TestSessionArchiveImportRejectsLoneSurrogateTriggerSidecarLikeUpstreamSerde(t *testing.T) {
	triggerSidecar := []byte(`{"version":1,"rules":[{"id":"dyn-1","condition":"created\ud800"}]}`)
	archivePath := makeArchiveWithSidecars(t, triggerSidecar, nil)

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse trigger sidecar") {
		t.Fatalf("expected lone surrogate trigger sidecar parse error like upstream serde_json, got %v", err)
	}
}

func TestSessionArchiveImportCanActivateAutomation(t *testing.T) {
	archivePath := makeAutomationArchive(t)

	importSummary, err := ImportArchiveWithOptions(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported", ImportArchiveOptions{ActivateAutomation: true})
	if err != nil {
		t.Fatal(err)
	}
	if !importSummary.AutomationEnabled {
		t.Fatalf("import summary should report activated automation: %#v", importSummary)
	}
	var importedTriggers struct {
		Rules []triggers.DynamicRule `json:"rules"`
	}
	if err := json.Unmarshal(mustReadFile(t, TriggerSidecarPath(importSummary.SessionPath)), &importedTriggers); err != nil {
		t.Fatal(err)
	}
	if len(importedTriggers.Rules) != 1 || !importedTriggers.Rules[0].Enabled {
		t.Fatalf("imported triggers should remain enabled: %#v", importedTriggers.Rules)
	}
	importedCron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(importedCron, `id = "cron-1"`) || !strings.Contains(importedCron, `enabled = true`) {
		t.Fatalf("imported cron should remain enabled:\n%s", importedCron)
	}
}

func TestSessionArchiveActivationOnPreservesSourceDisabledAutomationLikeUpstream(t *testing.T) {
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	triggerSidecar, err := json.Marshal(struct {
		Version uint32                 `json:"version"`
		Rules   []triggers.DynamicRule `json:"rules"`
	}{Version: 1, Rules: []triggers.DynamicRule{
		{ID: "was-enabled", Condition: "new file", Action: "summarize", Enabled: true, FireOnce: true, FiredAt: &createdAt, CreatedAt: createdAt},
		{ID: "was-disabled", Condition: "new file", Action: "summarize", Enabled: false, CreatedAt: createdAt},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cronSidecar := []byte(`[[jobs]]
id = "job-on"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "2026-01-02T03:04:05Z"

[[jobs]]
id = "job-off"
schedule = "*/10 * * * *"
action = "summarize"
enabled = false
created_at = "2026-01-02T03:04:05Z"
`)
	archivePath := makeArchiveWithSidecars(t, triggerSidecar, cronSidecar)

	importSummary, err := ImportArchiveWithOptions(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported", ImportArchiveOptions{Activation: AutomationActivationOn})
	if err != nil {
		t.Fatal(err)
	}

	var importedTriggers struct {
		Rules []triggers.DynamicRule `json:"rules"`
	}
	if err := json.Unmarshal(mustReadFile(t, TriggerSidecarPath(importSummary.SessionPath)), &importedTriggers); err != nil {
		t.Fatal(err)
	}
	if len(importedTriggers.Rules) != 2 || !importedTriggers.Rules[0].Enabled || importedTriggers.Rules[0].FiredAt == nil || importedTriggers.Rules[1].Enabled {
		t.Fatalf("source disabled trigger should stay disabled while history survives: %#v", importedTriggers.Rules)
	}
	importedCron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(importedCron, `id = "job-on"`) || !strings.Contains(importedCron, `enabled = true`) || !strings.Contains(importedCron, `id = "job-off"`) || !strings.Contains(importedCron, `enabled = false`) {
		t.Fatalf("source disabled cron should stay disabled:\n%s", importedCron)
	}
}

func TestSessionArchiveImportRejectsJSONCronSidecarLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[{"id":"cron-1","enabled":true}]`))

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse cron sidecar") {
		t.Fatalf("expected JSON cron sidecar to fail like upstream TOML parser, got %v", err)
	}
}

func TestSessionArchiveImportRejectsInvalidUTF8CronSidecarLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte("[[jobs]]\nid = \"cron-1\"\nnote = \"\xff\"\n"))

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "cron sidecar is not UTF-8") {
		t.Fatalf("expected invalid UTF-8 cron sidecar error like upstream, got %v", err)
	}
}

func TestSessionArchiveImportRejectsInvalidCronTOMLEscapeLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "bad\qescape"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`))

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse cron sidecar") {
		t.Fatalf("expected invalid TOML escape to fail like upstream parser, got %v", err)
	}
}

func TestSessionArchiveImportRejectsInvalidCronTOMLUnicodeEscapeLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "bad\uD800"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`))

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse cron sidecar") {
		t.Fatalf("expected invalid TOML unicode escape to fail like upstream parser, got %v", err)
	}
}

func TestSessionArchiveImportAcceptsEscapedBackslashCronTOMLStringLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "path\\file"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`))

	if _, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported"); err != nil {
		t.Fatalf("escaped backslash TOML string should import like upstream parser, got %v", err)
	}
}

func TestSessionArchiveImportAcceptsCronTOMLLiteralStringsLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = 'cron-1'
schedule = '*/10 * * * *'
action = 'path\file'
enabled = true
created_at = '2026-01-02T03:04:05Z'
`))

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatalf("literal TOML strings should import like upstream parser, got %v", err)
	}
	cron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(cron, `id = 'cron-1'`) || !strings.Contains(cron, `action = 'path\file'`) {
		t.Fatalf("literal TOML strings should be preserved:\n%s", cron)
	}
}

func TestSessionArchiveImportPreservesHashInCronTOMLLiteralStringLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = 'cron-1'
schedule = '*/10 * * * *'
action = 'hash#inside' # action comment
enabled = true
created_at = '2026-01-02T03:04:05Z'
`))

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatalf("hash inside TOML literal string should import like upstream parser, got %v", err)
	}
	cron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(cron, `action = 'hash#inside'`) || strings.Contains(cron, "# action comment") {
		t.Fatalf("literal TOML string should preserve # while stripping comment:\n%s", cron)
	}
}

func TestSessionArchiveImportAcceptsCronTOMLInlineCommentsLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]] # table comment
id = "cron-1" # id comment
schedule = "*/10 * * * *" # schedule comment
action = "hash#inside" # action comment
enabled = true # enabled comment
created_at = "2026-01-02T03:04:05Z" # created comment
`))

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatalf("inline comments should import like upstream parser, got %v", err)
	}
	cron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(cron, `action = "hash#inside"`) || strings.Contains(cron, "# action comment") {
		t.Fatalf("inline comments should be stripped while preserving # inside strings:\n%s", cron)
	}
}

func TestSessionArchiveImportAcceptsWhitespaceInCronTOMLArrayHeaderLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[ jobs ]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`))

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatalf("whitespace in cron TOML array header should import like upstream parser, got %v", err)
	}
	if importSummary.CronImported != 1 {
		t.Fatalf("cron import count mismatch: %#v", importSummary)
	}
}

func TestSessionArchiveImportAcceptsTabsInCronTOMLArrayHeaderLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte("[[\tjobs\t]]\nid = \"cron-1\"\nschedule = \"*/10 * * * *\"\naction = \"summarize\"\nenabled = true\ncreated_at = \"2026-01-02T03:04:05Z\"\n"))

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatalf("tabs in cron TOML array header should import like upstream parser, got %v", err)
	}
	if importSummary.CronImported != 1 {
		t.Fatalf("cron import count mismatch: %#v", importSummary)
	}
}

func TestSessionArchiveImportRejectsInternalWhitespaceInCronTOMLArrayHeaderLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jo bs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`))

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse cron sidecar") {
		t.Fatalf("internal whitespace in cron TOML array header should fail like upstream parser, got %v", err)
	}
}

func TestSessionArchiveImportRejectsCronTOMLMissingRequiredFieldsLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`))

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse cron sidecar") {
		t.Fatalf("expected missing cron id to fail like upstream TOML parser, got %v", err)
	}
}

func TestSessionArchiveImportRejectsDuplicateCronTOMLKeyLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = "cron-1"
id = "cron-2"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`))

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse cron sidecar") {
		t.Fatalf("expected duplicate cron TOML key to fail like upstream parser, got %v", err)
	}
}

func TestSessionArchiveImportRejectsCronTOMLInvalidFieldTypesLikeUpstream(t *testing.T) {
	tests := []struct {
		name string
		cron string
	}{
		{
			name: "enabled string",
			cron: `[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = "yes"
created_at = "2026-01-02T03:04:05Z"
`,
		},
		{
			name: "invalid created_at",
			cron: `[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "not-a-time"
`,
		},
		{
			name: "skipped overlap string",
			cron: `[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
skipped_overlap_count = "2"
created_at = "2026-01-02T03:04:05Z"
`,
		},
		{
			name: "stateful string",
			cron: `[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
stateful = "true"
created_at = "2026-01-02T03:04:05Z"
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archivePath := makeArchiveWithSidecars(t, nil, []byte(test.cron))

			_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
			if err == nil || !strings.Contains(err.Error(), "parse cron sidecar") {
				t.Fatalf("expected invalid cron field type to fail like upstream TOML parser, got %v", err)
			}
		})
	}
}

func TestSessionArchiveImportAcceptsUpstreamCronTOMLSidecar(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
running_trace_id = "trace-secret"
last_due_at = "2026-01-02T03:00:00Z"
last_fired_at = "2026-01-02T03:00:01Z"
last_completed_at = "2026-01-02T03:00:02Z"
last_error = "old error"
skipped_overlap_count = 2
stateful = true
created_at = "2026-01-02T03:04:05Z"
`))

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatal(err)
	}
	if importSummary.CronImported != 1 || len(importSummary.OriginallyEnabledCron) != 1 || importSummary.OriginallyEnabledCron[0] != "cron-1" {
		t.Fatalf("cron summary mismatch: %#v", importSummary)
	}
	cron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(cron, `id = "cron-1"`) || !strings.Contains(cron, `enabled = false`) || !strings.Contains(cron, `last_fired_at = "2026-01-02T03:00:01Z"`) {
		t.Fatalf("imported cron TOML mismatch:\n%s", cron)
	}
	for _, cleared := range []string{"running_trace_id", "last_due_at", "last_error", "skipped_overlap_count"} {
		if strings.Contains(cron, cleared) {
			t.Fatalf("imported cron TOML should clear %s:\n%s", cleared, cron)
		}
	}
}

func TestSessionArchiveImportDropsUnknownCronTOMLFieldsLikeUpstreamSerde(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "2026-01-02T03:04:05Z"
unknown_field = "drop me"
`))

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatal(err)
	}
	cron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if strings.Contains(cron, "unknown_field") {
		t.Fatalf("unknown cron TOML fields should be dropped like upstream serde rewrite:\n%s", cron)
	}
}

func TestSessionArchiveImportAcceptsUnderscoreCronTOMLIntegerLikeUpstream(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
skipped_overlap_count = 1_000
created_at = "2026-01-02T03:04:05Z"
`))

	if _, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported"); err != nil {
		t.Fatalf("underscore integer should import like upstream TOML parser, got %v", err)
	}
}

func TestSessionArchiveImportAcceptsPrefixedCronTOMLIntegersLikeUpstream(t *testing.T) {
	for _, value := range []string{"0x10", "0o10", "0b10"} {
		t.Run(value, func(t *testing.T) {
			archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
skipped_overlap_count = `+value+`
created_at = "2026-01-02T03:04:05Z"
`))

			if _, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported"); err != nil {
				t.Fatalf("%s integer should import like upstream TOML parser, got %v", value, err)
			}
		})
	}
}

func TestActivateImportedEnablesUpstreamCronTOMLSidecar(t *testing.T) {
	archivePath := makeArchiveWithSidecars(t, nil, []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`))
	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatal(err)
	}

	triggersEnabled, cronEnabled, err := ActivateImported(importSummary.SessionPath, nil, importSummary.OriginallyEnabledCron)
	if err != nil {
		t.Fatal(err)
	}
	if triggersEnabled != 0 || cronEnabled != 1 {
		t.Fatalf("enabled counts mismatch: triggers=%d cron=%d", triggersEnabled, cronEnabled)
	}
	cron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(cron, `id = "cron-1"`) || !strings.Contains(cron, `enabled = true`) {
		t.Fatalf("cron should be enabled:\n%s", cron)
	}
}

func TestActivateImportedDropsUnknownCronTOMLFieldsLikeUpstreamSerde(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(CronSidecarPath(sessionPath), []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = false
created_at = "2026-01-02T03:04:05Z"
unknown_field = "drop me"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ActivateImported(sessionPath, nil, []string{"cron-1"})
	if err != nil {
		t.Fatal(err)
	}
	cron := string(mustReadFile(t, CronSidecarPath(sessionPath)))
	if strings.Contains(cron, "unknown_field") {
		t.Fatalf("unknown cron TOML fields should be dropped on activation like upstream serde rewrite:\n%s", cron)
	}
}

func TestActivateImportedRejectsInvalidUTF8CronSidecarLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(CronSidecarPath(sessionPath), []byte("[[jobs]]\nid = \"cron-1\"\nnote = \"\xff\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ActivateImported(sessionPath, nil, []string{"cron-1"})
	if err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("expected invalid UTF-8 cron sidecar activation error like upstream, got %v", err)
	}
}

func TestSessionArchiveImportAskRequiresInteractiveConfirmationLikeUpstream(t *testing.T) {
	archivePath := makeAutomationArchive(t)

	_, err := ImportArchiveWithOptions(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported", ImportArchiveOptions{Activation: AutomationActivationAsk})
	if err == nil || !strings.Contains(err.Error(), "activate-triggers=ask") || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("ask activation should fail like upstream, got %v", err)
	}
}

func TestSessionArchiveImportAskUsesConfirmationCallback(t *testing.T) {
	archivePath := makeAutomationArchive(t)
	called := false
	importSummary, err := ImportArchiveWithOptions(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported", ImportArchiveOptions{
		Activation: AutomationActivationAsk,
		ConfirmAutomationActivation: func(summary AutomationActivationSummary) (bool, error) {
			called = true
			if summary.Triggers != 1 || summary.Cron != 1 || len(summary.OriginallyEnabledTriggers) != 1 || len(summary.OriginallyEnabledCron) != 1 {
				t.Fatalf("confirmation summary mismatch: %#v", summary)
			}
			return true, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called || !importSummary.AutomationEnabled {
		t.Fatalf("ask callback should enable automation: called=%v summary=%#v", called, importSummary)
	}
	cron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(cron, `enabled = true`) {
		t.Fatalf("cron should be enabled after confirmation:\n%s", cron)
	}
}

func TestSessionArchiveImportAskCallbackCanKeepAutomationDisabled(t *testing.T) {
	archivePath := makeAutomationArchive(t)
	importSummary, err := ImportArchiveWithOptions(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported", ImportArchiveOptions{
		Activation: AutomationActivationAsk,
		ConfirmAutomationActivation: func(summary AutomationActivationSummary) (bool, error) {
			return false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if importSummary.AutomationEnabled {
		t.Fatalf("ask callback should be able to keep automation disabled: %#v", importSummary)
	}
	cron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(cron, `enabled = false`) {
		t.Fatalf("cron should stay disabled after declined confirmation:\n%s", cron)
	}
}

func TestSessionArchiveImportPreservesEntryLinesLikeUpstream(t *testing.T) {
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	entryID, _ := source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(metadata["path"].(string), archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}

	files := readTarFiles(t, mustReadFile(t, archivePath))
	lines := strings.Split(strings.TrimSpace(string(files["session.jsonl"])), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected metadata and one entry line, got %q", files["session.jsonl"])
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &raw); err != nil {
		t.Fatal(err)
	}
	raw["futureField"] = map[string]any{"kept": true}
	entryBytes, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	files["session.jsonl"] = []byte(lines[0] + "\n  " + string(entryBytes) + "  \n")

	var manifest archiveManifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Content.SessionJSONLSHA256 = sha256Hex(files["session.jsonl"])
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files["manifest.json"] = manifestBytes
	tamperedPath := filepath.Join(t.TempDir(), "tampered.piesession")
	writeTarFiles(t, tamperedPath, files)

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), tamperedPath, "/tmp/imported")
	if err != nil {
		t.Fatal(err)
	}
	importedLines := strings.Split(string(mustReadFile(t, importSummary.SessionPath)), "\n")
	if len(importedLines) != 3 || importedLines[2] != "" || !strings.Contains(importedLines[1], `"futureField"`) || !strings.Contains(importedLines[1], entryID) || !strings.HasPrefix(importedLines[1], "  ") || !strings.HasSuffix(importedLines[1], "  ") {
		t.Fatalf("import should preserve original entry line like upstream, got %q", importedLines)
	}
}

func TestActivateImportedEnablesRequestedAutomation(t *testing.T) {
	archivePath := makeAutomationArchive(t)
	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatal(err)
	}

	triggersEnabled, cronEnabled, err := ActivateImported(importSummary.SessionPath, importSummary.OriginallyEnabledTriggers, importSummary.OriginallyEnabledCron)
	if err != nil {
		t.Fatal(err)
	}
	if triggersEnabled != 1 || cronEnabled != 1 {
		t.Fatalf("enabled counts mismatch: triggers=%d cron=%d", triggersEnabled, cronEnabled)
	}
	var importedTriggers struct {
		Rules []triggers.DynamicRule `json:"rules"`
	}
	if err := json.Unmarshal(mustReadFile(t, TriggerSidecarPath(importSummary.SessionPath)), &importedTriggers); err != nil {
		t.Fatal(err)
	}
	if len(importedTriggers.Rules) != 1 || !importedTriggers.Rules[0].Enabled {
		t.Fatalf("trigger should be enabled: %#v", importedTriggers.Rules)
	}
	importedCron := string(mustReadFile(t, CronSidecarPath(importSummary.SessionPath)))
	if !strings.Contains(importedCron, `id = "cron-1"`) || !strings.Contains(importedCron, `enabled = true`) {
		t.Fatalf("cron should be enabled:\n%s", importedCron)
	}
}

func TestActivateImportedTriggerJSONHasNoTrailingNewlineLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	triggerPath := TriggerSidecarPath(sessionPath)
	triggerFile := struct {
		Version uint32                 `json:"version"`
		Rules   []triggers.DynamicRule `json:"rules"`
	}{Version: 1, Rules: []triggers.DynamicRule{{ID: "dyn-1", Enabled: false}}}
	data, err := json.Marshal(triggerFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(triggerPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	triggersEnabled, cronEnabled, err := ActivateImported(sessionPath, []string{"dyn-1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if triggersEnabled != 1 || cronEnabled != 0 {
		t.Fatalf("enabled counts mismatch: triggers=%d cron=%d", triggersEnabled, cronEnabled)
	}
	written := mustReadFile(t, triggerPath)
	if bytes.HasSuffix(written, []byte("\n")) {
		t.Fatalf("activated trigger sidecar should not end with newline like upstream, got %q", written)
	}
}

func TestActivateImportedRejectsInvalidUTF8TriggerSidecarLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(TriggerSidecarPath(sessionPath), []byte("{\"version\":1,\"rules\":[{\"id\":\"dyn-1\",\"condition\":\"\xff\"}]}"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ActivateImported(sessionPath, []string{"dyn-1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("expected invalid UTF-8 trigger sidecar activation error like upstream, got %v", err)
	}
}

func TestActivateImportedRejectsLoneSurrogateTriggerSidecarLikeUpstreamSerde(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(TriggerSidecarPath(sessionPath), []byte(`{"version":1,"rules":[{"id":"dyn-1","condition":"created\ud800"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ActivateImported(sessionPath, []string{"dyn-1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "parse trigger sidecar") {
		t.Fatalf("expected lone surrogate trigger sidecar activation parse error like upstream serde_json, got %v", err)
	}
}

func TestCommitImportedJSONLStagesThenRenames(t *testing.T) {
	repo := NewJSONLRepo(t.TempDir())
	path := filepath.Join(repo.root, "imported.jsonl")
	tmpPath := path + ".tmp"
	metadata := JSONLMetadata{Metadata: Metadata{ID: "imported", CreatedAt: "now"}, CWD: "/tmp/imported", Path: path}
	entries := []Entry{NewMessageEntry("m1", nil, "ts", agent.NewUserMessage("hello"))}

	if err := commitImportedJSONL(repo, path, tmpPath, metadata, entries); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temp file should be gone, err=%v", err)
	}
	paths, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != path {
		t.Fatalf("repo list mismatch: %#v", paths)
	}
	opened, err := repo.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := opened.Storage().GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "m1" {
		t.Fatalf("entries mismatch: %#v", got)
	}
}

func TestCommitImportStagesValidatesAndRenames(t *testing.T) {
	repo := NewJSONLRepo(t.TempDir())
	sessionPath := filepath.Join(repo.root, "imported.jsonl")
	tmpPath := sessionPath + ".tmp"
	sessionContent := string(makeArchiveSessionJSONL(t))
	sidecarPath := TriggerSidecarPath(sessionPath)

	if err := CommitImport(repo, sessionPath, tmpPath, sessionContent, map[string][]byte{sidecarPath: []byte(`{"version":1,"rules":[]}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temp file should be gone, err=%v", err)
	}
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("session should be committed: %v", err)
	}
	if data, err := os.ReadFile(sidecarPath); err != nil || string(data) != `{"version":1,"rules":[]}` {
		t.Fatalf("sidecar mismatch data=%s err=%v", data, err)
	}
}

func TestCommitImportCleansUpOnValidationFailure(t *testing.T) {
	repo := NewJSONLRepo(t.TempDir())
	sessionPath := filepath.Join(repo.root, "bad.jsonl")
	tmpPath := sessionPath + ".tmp"
	sidecarPath := TriggerSidecarPath(sessionPath)

	err := CommitImport(repo, sessionPath, tmpPath, "not json\n", map[string][]byte{sidecarPath: []byte(`{"version":1,"rules":[]}`)})
	if err == nil {
		t.Fatalf("expected validation failure")
	}
	for _, path := range []string{tmpPath, sessionPath, sidecarPath} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s should be cleaned up, stat err=%v", path, statErr)
		}
	}
}

func TestSessionArchiveImportCleansUpWhenSidecarWriteFails(t *testing.T) {
	archivePath := makeAutomationArchive(t)
	targetRepo := NewJSONLRepo(t.TempDir())
	blockerPath := filepath.Join(targetRepo.root, "blocked.triggers.json")
	if err := os.MkdirAll(blockerPath, 0o755); err != nil {
		t.Fatal(err)
	}
	oldCreateSessionID := createSessionID
	createSessionID = func() string { return "blocked" }
	t.Cleanup(func() { createSessionID = oldCreateSessionID })

	_, err := ImportArchive(targetRepo, archivePath, "/tmp/imported")
	if err == nil {
		t.Fatalf("expected sidecar write failure")
	}
	paths, listErr := targetRepo.List()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(paths) != 0 {
		t.Fatalf("import should not commit session on sidecar failure: %#v", paths)
	}
	if _, err := os.Stat(filepath.Join(targetRepo.root, "blocked.jsonl.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp session should be removed, err=%v", err)
	}
}

func TestSessionArchiveSuccessfulImportLeavesNoTempFilesLikeUpstream(t *testing.T) {
	archivePath := makeAutomationArchive(t)
	targetRepo := NewJSONLRepo(t.TempDir())
	if _, err := ImportArchive(targetRepo, archivePath, "/tmp/imported"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(targetRepo.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("successful import left temp file %s", entry.Name())
		}
	}
}

func makeAutomationArchive(t *testing.T) string {
	t.Helper()
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := metadata["path"].(string)
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	triggerSidecar, err := json.Marshal(struct {
		Version uint32                 `json:"version"`
		Rules   []triggers.DynamicRule `json:"rules"`
	}{Version: 1, Rules: []triggers.DynamicRule{{ID: "dyn-1", Condition: "new file", Action: "summarize", Enabled: true, FireOnce: true, CreatedAt: createdAt}}})
	if err != nil {
		t.Fatal(err)
	}
	cronSidecar := []byte(`[[jobs]]
id = "cron-1"
schedule = "*/10 * * * *"
action = "summarize"
enabled = true
created_at = "2026-01-02T03:04:05Z"
`)
	if err := os.WriteFile(TriggerSidecarPath(sourcePath), triggerSidecar, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CronSidecarPath(sourcePath), cronSidecar, 0o644); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(sourcePath, archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}
	return archivePath
}

func makeArchiveWithSidecars(t *testing.T, triggerSidecar, cronSidecar []byte) string {
	t.Helper()
	sessionBytes := makeArchiveSessionJSONL(t)
	var metadata JSONLMetadata
	if err := json.Unmarshal(bytes.SplitN(sessionBytes, []byte("\n"), 2)[0], &metadata); err != nil {
		t.Fatal(err)
	}
	entryCount := 0
	for _, line := range bytes.Split(sessionBytes, []byte("\n"))[1:] {
		if len(bytes.TrimSpace(line)) > 0 {
			entryCount++
		}
	}
	manifest := archiveManifest{
		Schema:     archiveSchema,
		CreatedAt:  "2026-01-02T03:04:05Z",
		PieVersion: "test-version",
		Source: archiveManifestSource{
			SessionID:   metadata.ID,
			CWD:         metadata.CWD,
			SessionPath: metadata.Path,
		},
		Content: archiveManifestContent{
			SessionJSONLSHA256: sha256Hex(sessionBytes),
			EntryCount:         entryCount,
			ActiveLeafID:       stringPtr("m1"),
			HasTriggers:        triggerSidecar != nil,
			HasCron:            cronSidecar != nil,
		},
		Sensitivity: archiveManifestSensitivity{SessionTranscriptPreserved: true},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{archiveManifestPath: manifestBytes, archiveSessionPath: sessionBytes}
	if triggerSidecar != nil {
		files[archiveTriggersPath] = triggerSidecar
	}
	if cronSidecar != nil {
		files[archiveCronPath] = cronSidecar
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	writeTarFiles(t, archivePath, files)
	return archivePath
}

func makeArchiveSessionJSONL(t *testing.T) []byte {
	t.Helper()
	metadata := JSONLMetadata{Metadata: Metadata{ID: "source-session", CreatedAt: "2026-01-02T03:04:05Z"}, CWD: "/tmp/source", Path: "/tmp/source/source-session.jsonl"}
	entry := NewMessageEntry("m1", nil, "2026-01-02T03:04:06Z", agent.NewUserMessage("hello"))
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	return append(append(metadataBytes, '\n'), append(entryBytes, '\n')...)
}

func writeSessionJSONL(t *testing.T, path string, metadata JSONLMetadata, entries []Entry) {
	t.Helper()
	var content []byte
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, metadataBytes...)
	content = append(content, '\n')
	for _, entry := range entries {
		entryBytes, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		content = append(content, entryBytes...)
		content = append(content, '\n')
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSessionArchiveExportRefusesToOverwrite(t *testing.T) {
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if err := os.WriteFile(archivePath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ExportArchive(metadata["path"].(string), archivePath, "test-version")
	wantErr := "output already exists: " + archivePath + " (remove it or pass a different path)"
	if err == nil || err.Error() != wantErr {
		t.Fatalf("expected exists error, got %v", err)
	}
	if string(mustReadFile(t, archivePath)) != "original" {
		t.Fatalf("archive was overwritten")
	}
}

func TestSessionArchiveExportUsesPrivateFileModes(t *testing.T) {
	sourceRepo := NewJSONLRepo(t.TempDir())
	source, err := sourceRepo.Create("/tmp/source")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = source.AppendMessage(agent.NewUserMessage("hello"))
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(metadata["path"].(string), archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode mismatch: %o", info.Mode().Perm())
	}
	reader := tar.NewReader(bytes.NewReader(mustReadFile(t, archivePath)))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Mode != 0o600 {
			t.Fatalf("tar entry %s mode mismatch: %o", header.Name, header.Mode)
		}
	}
}

func TestSessionArchiveExportRejectsNonUTF8SessionLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(sessionPath, []byte{0xff, '\n'}, 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")

	_, err := ExportArchive(sessionPath, archivePath, "test-version")
	if err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 error, got %v", err)
	}
}

func TestSessionArchiveRejectsWhitespaceMetadataIDLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "blank-id.jsonl")
	metadata := JSONLMetadata{Metadata: Metadata{ID: "   ", CreatedAt: "2026-01-02T03:04:05Z"}, CWD: "/tmp/source", Path: sessionPath}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath, append(metadataBytes, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = ExportArchive(sessionPath, filepath.Join(t.TempDir(), "session.piesession"), "test-version")
	if err == nil || !strings.Contains(err.Error(), "session metadata is missing id") {
		t.Fatalf("expected missing id error, got %v", err)
	}
}

func TestSessionArchiveReportsEntryParseLineLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "bad-entry.jsonl")
	metadata := JSONLMetadata{Metadata: Metadata{ID: "source-session", CreatedAt: "2026-01-02T03:04:05Z"}, CWD: "/tmp/source", Path: sessionPath}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	content := append(append(metadataBytes, '\n'), []byte("\n{bad json}\n")...)
	if err := os.WriteFile(sessionPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = ExportArchive(sessionPath, filepath.Join(t.TempDir(), "session.piesession"), "test-version")
	if err == nil || !strings.Contains(err.Error(), "parse session entry line 3") {
		t.Fatalf("expected entry parse line context, got %v", err)
	}
}

func TestSessionArchiveReportsMetadataParseContextLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "bad-metadata.jsonl")
	if err := os.WriteFile(sessionPath, []byte("{bad metadata}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ExportArchive(sessionPath, filepath.Join(t.TempDir(), "session.piesession"), "test-version")
	if err == nil || !strings.Contains(err.Error(), "parse session metadata") {
		t.Fatalf("expected metadata parse context, got %v", err)
	}
}

func TestSessionArchiveRejectsLoneSurrogateMetadataLikeUpstreamSerde(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "bad-metadata-surrogate.jsonl")
	entry := NewMessageEntry("m1", nil, "2026-01-02T03:04:06Z", agent.NewUserMessage("hello"))
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	content := append([]byte(`{"id":"source-session","created_at":"2026-01-02T03:04:05Z","cwd":"/tmp/source\ud800","path":"/tmp/source/source-session.jsonl"}`+"\n"), append(entryBytes, '\n')...)
	if err := os.WriteFile(sessionPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = ExportArchive(sessionPath, filepath.Join(t.TempDir(), "session.piesession"), "test-version")
	if err == nil || !strings.Contains(err.Error(), "parse session metadata") {
		t.Fatalf("expected lone surrogate metadata parse error like upstream serde_json, got %v", err)
	}
}

func TestSessionArchiveRejectsLoneSurrogateEntryLikeUpstreamSerde(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "bad-entry-surrogate.jsonl")
	metadata := JSONLMetadata{Metadata: Metadata{ID: "source-session", CreatedAt: "2026-01-02T03:04:05Z"}, CWD: "/tmp/source", Path: sessionPath}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	entry := NewMessageEntry("m1", nil, "2026-01-02T03:04:06Z", agent.NewUserMessage("hello"))
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	entryBytes = bytes.Replace(entryBytes, []byte("hello"), []byte(`hello\ud800`), 1)
	content := append(append(metadataBytes, '\n'), append(entryBytes, '\n')...)
	if err := os.WriteFile(sessionPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = ExportArchive(sessionPath, filepath.Join(t.TempDir(), "session.piesession"), "test-version")
	if err == nil || !strings.Contains(err.Error(), "parse session entry line 2") {
		t.Fatalf("expected lone surrogate entry parse error like upstream serde_json, got %v", err)
	}
}

func TestSessionArchiveAcceptsSingleEmptyEntryIDLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "empty-entry-id.jsonl")
	metadata := JSONLMetadata{Metadata: Metadata{ID: "source-session", CreatedAt: "2026-01-02T03:04:05Z"}, CWD: "/tmp/source", Path: sessionPath}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	entry := NewMessageEntry("", nil, "2026-01-02T03:04:06Z", agent.NewUserMessage("hello"))
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	content := append(append(metadataBytes, '\n'), append(entryBytes, '\n')...)
	if err := os.WriteFile(sessionPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := ExportArchive(sessionPath, filepath.Join(t.TempDir(), "session.piesession"), "test-version")
	if err != nil {
		t.Fatalf("single empty entry id should be accepted like upstream String id, got %v", err)
	}
	if summary.EntryCount != 1 {
		t.Fatalf("entry count mismatch: %#v", summary)
	}
}

func TestSessionArchiveImportNormalizesCRLFLinesLikeUpstream(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "crlf.jsonl")
	metadata := JSONLMetadata{Metadata: Metadata{ID: "source-session", CreatedAt: "2026-01-02T03:04:05Z"}, CWD: "/tmp/source", Path: sessionPath}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	entry := NewMessageEntry("m1", nil, "2026-01-02T03:04:06Z", agent.NewUserMessage("hello"))
	entryBytes, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath, append(append(metadataBytes, []byte("\r\n")...), append(entryBytes, []byte("\r\n")...)...), 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	if _, err := ExportArchive(sessionPath, archivePath, "test-version"); err != nil {
		t.Fatal(err)
	}

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err != nil {
		t.Fatal(err)
	}
	imported := string(mustReadFile(t, importSummary.SessionPath))
	if strings.Contains(imported, "\r") {
		t.Fatalf("import should normalize CRLF lines like upstream str::lines, got %q", imported)
	}
}

func TestSessionArchiveImportRejectsExistingDestinationLikeUpstream(t *testing.T) {
	archivePath := makeAutomationArchive(t)
	repo := NewJSONLRepo(t.TempDir())
	collisionID := "019eee64-0000-7000-8000-000000000000"
	destination := filepath.Join(repo.Root(), collisionID+".jsonl")
	if err := os.MkdirAll(repo.Root(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldCreateSessionID := createSessionID
	createSessionID = func() string { return collisionID }
	defer func() { createSessionID = oldCreateSessionID }()

	_, err := ImportArchive(repo, archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "import destination already exists") {
		t.Fatalf("expected existing destination error, got %v", err)
	}
	if string(mustReadFile(t, destination)) != "original" {
		t.Fatalf("existing destination was overwritten")
	}
}

func TestSessionArchiveImportIgnoresManifestSidecarFlagsWhenFilesMissingLikeUpstream(t *testing.T) {
	archivePath := makeAutomationArchive(t)
	files := readTarFiles(t, mustReadFile(t, archivePath))
	delete(files, archiveTriggersPath)
	delete(files, archiveCronPath)
	trimmedPath := filepath.Join(t.TempDir(), "trimmed.piesession")
	writeTarFiles(t, trimmedPath, files)

	importSummary, err := ImportArchive(NewJSONLRepo(t.TempDir()), trimmedPath, "/tmp/imported")
	if err != nil {
		t.Fatalf("missing sidecar files should be ignored like upstream, got %v", err)
	}
	if importSummary.TriggersImported != 0 || importSummary.CronImported != 0 || len(importSummary.OriginallyEnabledTriggers) != 0 || len(importSummary.OriginallyEnabledCron) != 0 {
		t.Fatalf("sidecars should not be imported: %#v", importSummary)
	}
}

func TestSessionArchiveImportRejectsOversizedManifest(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "oversized.piesession")
	writeTarFiles(t, archivePath, map[string][]byte{
		"manifest.json": bytes.Repeat([]byte("x"), maxArchiveManifestBytes+1),
		"session.jsonl": []byte("{}\n"),
	})
	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected too large error, got %v", err)
	}
}

func TestSessionArchiveImportReportsManifestParseContextLikeUpstream(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bad-manifest.piesession")
	writeTarFiles(t, archivePath, map[string][]byte{
		archiveManifestPath: []byte("{bad json}"),
		archiveSessionPath:  []byte("{}\n"),
	})

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse session archive manifest") {
		t.Fatalf("expected manifest parse context, got %v", err)
	}
}

func TestSessionArchiveImportRejectsInvalidUTF8ManifestLikeUpstream(t *testing.T) {
	sessionBytes := makeArchiveSessionJSONL(t)
	manifestBytes := []byte(`{"schema":"pie.session_export.v1","created_at":"2026-01-02T03:04:05Z","pie_version":"test-version","source":{"session_id":"source-session","cwd":"/tmp/source","session_path":"/tmp/source/session.jsonl` + string([]byte{0xff}) + `"},"content":{"session_jsonl_sha256":"` + sha256Hex(sessionBytes) + `","entry_count":1,"active_leaf_id":"m1"},"sensitivity":{"session_transcript_preserved":true}}`)
	archivePath := filepath.Join(t.TempDir(), "bad-manifest-utf8.piesession")
	writeTarFiles(t, archivePath, map[string][]byte{archiveManifestPath: manifestBytes, archiveSessionPath: sessionBytes})

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse session archive manifest") || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("expected invalid UTF-8 manifest parse error like upstream, got %v", err)
	}
}

func TestSessionArchiveImportRejectsLoneSurrogateManifestStringLikeUpstreamSerde(t *testing.T) {
	sessionBytes := makeArchiveSessionJSONL(t)
	manifestBytes := []byte(`{"schema":"pie.session_export.v1","created_at":"2026-01-02T03:04:05Z","pie_version":"test-version","source":{"session_id":"source-session","cwd":"/tmp/source","session_path":"/tmp/source/session.jsonl\ud800"},"content":{"session_jsonl_sha256":"` + sha256Hex(sessionBytes) + `","entry_count":1,"active_leaf_id":"m1"},"sensitivity":{"session_transcript_preserved":true}}`)
	archivePath := filepath.Join(t.TempDir(), "bad-manifest-surrogate.piesession")
	writeTarFiles(t, archivePath, map[string][]byte{archiveManifestPath: manifestBytes, archiveSessionPath: sessionBytes})

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "parse session archive manifest") {
		t.Fatalf("expected lone surrogate manifest parse error like upstream serde_json, got %v", err)
	}
}

func TestSessionArchiveImportAcceptsEscapedBackslashSurrogateTextLikeUpstreamSerde(t *testing.T) {
	sessionBytes := makeArchiveSessionJSONL(t)
	manifestBytes := []byte(`{"schema":"pie.session_export.v1","created_at":"2026-01-02T03:04:05Z","pie_version":"test-version","source":{"session_id":"source-session","cwd":"/tmp/source","session_path":"/tmp/source/session.jsonl\\ud800"},"content":{"session_jsonl_sha256":"` + sha256Hex(sessionBytes) + `","entry_count":1,"active_leaf_id":"m1"},"sensitivity":{"session_transcript_preserved":true}}`)
	archivePath := filepath.Join(t.TempDir(), "escaped-manifest-surrogate.piesession")
	writeTarFiles(t, archivePath, map[string][]byte{archiveManifestPath: manifestBytes, archiveSessionPath: sessionBytes})

	if _, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported"); err != nil {
		t.Fatalf("escaped backslash surrogate text should import like upstream serde_json, got %v", err)
	}
}

func TestSessionArchiveImportRejectsNonUTF8SessionJSONLLikeUpstream(t *testing.T) {
	sessionBytes := []byte{0xff, '\n'}
	manifest := archiveManifest{
		Schema:      archiveSchema,
		CreatedAt:   "2026-01-02T03:04:05Z",
		PieVersion:  "test-version",
		Source:      archiveManifestSource{SessionID: "source-session", CWD: "/tmp/source", SessionPath: "/tmp/source/session.jsonl"},
		Content:     archiveManifestContent{SessionJSONLSHA256: sha256Hex(sessionBytes)},
		Sensitivity: archiveManifestSensitivity{SessionTranscriptPreserved: true},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "bad-session.piesession")
	writeTarFiles(t, archivePath, map[string][]byte{archiveManifestPath: manifestBytes, archiveSessionPath: sessionBytes})

	_, err = ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "session.jsonl is not UTF-8") {
		t.Fatalf("expected session.jsonl UTF-8 error, got %v", err)
	}
}

func TestSessionArchiveImportRejectsUnsafePath(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "unsafe.piesession")
	writeTarFiles(t, archivePath, map[string][]byte{"../manifest.json": []byte("{}")})
	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
}

func TestSessionArchiveImportRejectsNonUTF8PathLikeUpstream(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "non-utf8-path.piesession")
	writeTarEntries(t, archivePath, []tarEntry{{Name: string([]byte{0xff}), Content: []byte("{}"), Typeflag: tar.TypeReg}})

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "session archive contains non-UTF-8 path") {
		t.Fatalf("expected non-UTF-8 path error, got %v", err)
	}
}

func TestSessionArchiveImportRejectsEmptyPathAsUnsafeLikeUpstream(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "empty-path.piesession")
	writeTarEntries(t, archivePath, []tarEntry{{Name: "", Content: []byte("{}"), Typeflag: tar.TypeReg}})

	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "session archive contains an unsafe path") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}
}

func TestSessionArchiveImportRejectsNonFileEntry(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "dir.piesession")
	writeTarEntries(t, archivePath, []tarEntry{{Name: "manifest.json", Typeflag: tar.TypeDir}})
	_, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported")
	if err == nil || !strings.Contains(err.Error(), "non-file entry") {
		t.Fatalf("expected non-file entry error, got %v", err)
	}
}

func TestSessionArchiveImportAcceptsLegacyRegularTarEntriesLikeUpstream(t *testing.T) {
	sessionBytes := makeArchiveSessionJSONL(t)
	parsed, err := parseJSONLTranscript(sessionBytes)
	if err != nil {
		t.Fatal(err)
	}
	var manifest archiveManifest
	if err := json.Unmarshal([]byte(`{"schema":"pie.session_export.v1","created_at":"2026-01-02T03:04:05Z","pie_version":"test-version","source":{"session_id":"source-session","cwd":"/tmp/source","session_path":"/tmp/source/source-session.jsonl"},"content":{"entry_count":1},"sensitivity":{"session_transcript_preserved":true}}`), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Content.SessionJSONLSHA256 = sha256Hex(sessionBytes)
	manifest.Content.ActiveLeafID = parsed.activeLeafID
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "legacy-regular.piesession")
	writeTarEntries(t, archivePath, []tarEntry{
		{Name: archiveManifestPath, Content: manifestBytes, Typeflag: tar.TypeRegA},
		{Name: archiveSessionPath, Content: sessionBytes, Typeflag: tar.TypeRegA},
	})

	if _, err := ImportArchive(NewJSONLRepo(t.TempDir()), archivePath, "/tmp/imported"); err != nil {
		t.Fatalf("legacy regular tar entries should import like upstream, got %v", err)
	}
}

func TestSessionArchiveUpstreamHelperNames(t *testing.T) {
	sessionBytes := makeArchiveSessionJSONL(t)
	parsed, err := ParseSessionJSONL(string(sessionBytes))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Metadata.ID == "" || len(parsed.Entries) != 1 || len(parsed.OriginalEntryLines) != 1 || parsed.ActiveLeafID == nil {
		t.Fatalf("parsed session mismatch: %#v", parsed)
	}

	if err := ValidateArchivePath("sidecars/triggers.json"); err != nil {
		t.Fatalf("valid archive path rejected: %v", err)
	}
	if err := ValidateArchivePath("../manifest.json"); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("expected unsafe path error, got %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	writeTarFiles(t, archivePath, map[string][]byte{archiveSessionPath: sessionBytes})
	files, err := ReadArchive(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(files[archiveSessionPath], sessionBytes) {
		t.Fatalf("read archive mismatch: %#v", files)
	}
}

func TestSessionArchiveRewriteSidecarHelpers(t *testing.T) {
	triggerBytes := []byte(`{"version":1,"rules":[{"id":"on","enabled":true},{"id":"off","enabled":false}]}`)
	triggerOff, err := RewriteTriggerSidecar(triggerBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	if triggerOff.Count != 2 || !reflect.DeepEqual(triggerOff.EnabledIDs, []string{"on"}) || bytes.Contains(triggerOff.Bytes, []byte(`"enabled": true`)) {
		t.Fatalf("trigger rewrite off mismatch: %#v text=%s", triggerOff, triggerOff.Bytes)
	}
	triggerOn, err := RewriteTriggerSidecar(triggerBytes, true)
	if err != nil {
		t.Fatal(err)
	}
	if triggerOn.Count != 2 || !reflect.DeepEqual(triggerOn.EnabledIDs, []string{"on"}) || !bytes.Contains(triggerOn.Bytes, []byte(`"enabled": true`)) {
		t.Fatalf("trigger rewrite on mismatch: %#v text=%s", triggerOn, triggerOn.Bytes)
	}

	cronBytes := []byte("[[jobs]]\nid = \"job-on\"\nschedule = \"* * * * *\"\naction = \"echo hi\"\nenabled = true\ncreated_at = \"2026-01-02T03:04:05Z\"\nrunning_trace_id = \"trace\"\nlast_error = \"boom\"\n")
	cronOff, err := RewriteCronSidecar(cronBytes, false)
	if err != nil {
		t.Fatal(err)
	}
	if cronOff.Count != 1 || !reflect.DeepEqual(cronOff.EnabledIDs, []string{"job-on"}) || bytes.Contains(cronOff.Bytes, []byte("enabled = true")) || bytes.Contains(cronOff.Bytes, []byte("running_trace_id")) || bytes.Contains(cronOff.Bytes, []byte("last_error")) {
		t.Fatalf("cron rewrite off mismatch: %#v text=%s", cronOff, cronOff.Bytes)
	}
}

func TestSessionArchiveRewriteTriggerSidecarDoesNotHTMLEscapeLikeUpstream(t *testing.T) {
	triggerBytes := []byte(`{"version":1,"rules":[{"id":"rule-1","condition":"a < b && c > d","action":"echo ok","enabled":true}]}`)
	rewritten, err := RewriteTriggerSidecar(triggerBytes, true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(rewritten.Bytes)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("trigger sidecar should match upstream serde_json pretty formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `a < b && c > d`) {
		t.Fatalf("trigger sidecar should preserve literal text, got %s", text)
	}
}

func TestSessionArchiveRewriteSessionJSONL(t *testing.T) {
	sessionBytes := makeArchiveSessionJSONL(t)
	parsed, err := ParseSessionJSONL(string(sessionBytes))
	if err != nil {
		t.Fatal(err)
	}
	manifest := ArchiveManifestForImport{
		SourceSessionID: "source-session",
		SourceCWD:       "/tmp/source",
		CreatedAt:       "2026-01-02T03:04:05Z",
		PieVersion:      "test-version",
	}
	rewritten, err := RewriteSessionJSONL(parsed, manifest, "new-session", "/tmp/imported", "/tmp/imported/new-session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(rewritten, "\n"), "\n")
	if len(lines) != 2 || lines[1] != parsed.OriginalEntryLines[0] {
		t.Fatalf("entry lines should be preserved, got %#v", lines)
	}
	var metadata JSONLMetadata
	if err := json.Unmarshal([]byte(lines[0]), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.ID != "new-session" || metadata.CWD != "/tmp/imported" || metadata.Path != "/tmp/imported/new-session.jsonl" || metadata.ImportedFrom == nil {
		t.Fatalf("metadata mismatch: %#v", metadata)
	}
	if metadata.ImportedFrom.SessionID != "source-session" || metadata.ImportedFrom.CWD != "/tmp/source" || metadata.ImportedFrom.ExportedAt != manifest.CreatedAt || metadata.ImportedFrom.PieVersion != manifest.PieVersion {
		t.Fatalf("import origin mismatch: %#v", metadata.ImportedFrom)
	}
}

func TestSessionArchiveRewriteSessionJSONLDoesNotHTMLEscapeLikeUpstream(t *testing.T) {
	parsed, err := ParseSessionJSONL(`{"id":"source-session","createdAt":"now","cwd":"/tmp/source","path":"/tmp/source/session.jsonl"}
{"type":"message","id":"m1","parentId":null,"timestamp":"now","message":{"role":"user","content":"a < b && c > d","timestamp":0}}
`)
	if err != nil {
		t.Fatal(err)
	}
	manifest := ArchiveManifestForImport{SourceSessionID: "source-session", SourceCWD: "/tmp/a < b && c > d", CreatedAt: "2026-01-02T03:04:05Z", PieVersion: "test-version"}
	rewritten, err := RewriteSessionJSONL(parsed, manifest, "new-session", "/tmp/a < b && c > d", "/tmp/imported/new-session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rewritten, `\u003c`) || strings.Contains(rewritten, `\u003e`) || strings.Contains(rewritten, `\u0026`) {
		t.Fatalf("rewritten session jsonl should match upstream serde_json formatting without HTML escaping, got %s", rewritten)
	}
	if !strings.Contains(rewritten, `/tmp/a < b && c > d`) || !strings.Contains(rewritten, `a < b && c > d`) {
		t.Fatalf("rewritten session jsonl should preserve literal text, got %s", rewritten)
	}
}

func TestSessionArchiveFileAndTarHelpers(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "session.piesession")
	file, err := CreateArchiveFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("ok"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode=%#o", info.Mode().Perm())
	}
	if _, err := CreateArchiveFile(archivePath); err == nil || !strings.Contains(err.Error(), "output already exists") {
		t.Fatalf("expected existing output error, got %v", err)
	}

	missing, present, err := ReadOptionalSidecar(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || present || missing != nil {
		t.Fatalf("missing sidecar mismatch: present=%v bytes=%v err=%v", present, missing, err)
	}
	sidecarPath := filepath.Join(t.TempDir(), "sidecar.json")
	if err := os.WriteFile(sidecarPath, []byte("sidecar"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, present, err := ReadOptionalSidecar(sidecarPath)
	if err != nil || !present || string(data) != "sidecar" {
		t.Fatalf("sidecar mismatch: present=%v bytes=%q err=%v", present, data, err)
	}

	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	if err := AppendBytes(writer, "session.jsonl", []byte("jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	files := readTarFiles(t, buf.Bytes())
	if string(files[archiveSessionPath]) != "jsonl" {
		t.Fatalf("tar files mismatch: %#v", files)
	}
}

func readTarFiles(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(data))
	files := map[string][]byte{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = content
	}
	return files
}

func archiveManifestFromPath(t *testing.T, path string) archiveManifest {
	t.Helper()
	files := readTarFiles(t, mustReadFile(t, path))
	var manifest archiveManifest
	if err := json.Unmarshal(files[archiveManifestPath], &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func writeTarFiles(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	entries := make([]tarEntry, 0, len(files))
	for name, content := range files {
		entries = append(entries, tarEntry{Name: name, Content: content, Typeflag: tar.TypeReg})
	}
	writeTarEntries(t, path, entries)
}

type tarEntry struct {
	Name     string
	Content  []byte
	Typeflag byte
}

func writeTarEntries(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := tar.NewWriter(file)
	defer writer.Close()
	for _, entry := range entries {
		if err := writer.WriteHeader(&tar.Header{Name: entry.Name, Typeflag: entry.Typeflag, Mode: 0o644, Size: int64(len(entry.Content))}); err != nil {
			t.Fatal(err)
		}
		if len(entry.Content) > 0 {
			if _, err := writer.Write(entry.Content); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestJSONLStorageReplaceEntriesRewritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}, CWD: "/tmp/project", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	first := NewMessageEntry("m1", nil, "ts1", agent.NewUserMessage("old"))
	second := NewMessageEntry("m2", stringPtr("m1"), "ts2", agent.NewUserMessage("keep"))
	if err := storage.AppendEntry(first); err != nil {
		t.Fatal(err)
	}
	if err := storage.ReplaceEntries([]Entry{second}); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenJSONLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := reopened.GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID() != "m2" {
		t.Fatalf("entries mismatch: %#v", entries)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"id":"m1"`) || !strings.Contains(string(raw), `"id":"m2"`) {
		t.Fatalf("file was not rewritten: %s", raw)
	}
}

func TestJSONLStoragePreservesAIMessageMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}, CWD: "/tmp/project", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	message := ai.Message{Role: ai.RoleTool, ResponseID: "resp-1", ResponseModel: "served", ToolCallID: "call-1", ToolName: "read", Details: map[string]any{"exit_code": float64(2)}, IsError: true, Timestamp: 123, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "failed"}}}
	if err := storage.AppendEntry(NewMessageEntry("m1", nil, "ts", agent.Message{Kind: agent.MessageKindLLM, LLM: &message})); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenJSONLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := reopened.GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	got := entries[0].Message.ToolResult
	if entries[0].Message.Kind != agent.MessageKindToolResult || got == nil || got.Name != "read" || !got.IsError || got.Details["exit_code"] != json.Number("2") {
		t.Fatalf("metadata mismatch: %#v", got)
	}
}

func TestMemoryRepoAndForkEntries(t *testing.T) {
	repo := NewMemoryRepo()
	sess := repo.Create()
	first, _ := sess.AppendMessage(agent.NewUserMessage("first"))
	second, _ := sess.AppendMessage(agent.NewUserMessage("second"))
	_, _ = sess.AppendMessage(agent.NewAssistantMessage("after"))

	fork, err := GetEntriesToFork(sess.Storage(), ForkOptions{EntryID: &second, Position: ForkBefore})
	if err != nil {
		t.Fatal(err)
	}
	if len(fork) != 1 || fork[0].ID() != first {
		t.Fatalf("fork-before mismatch: %#v", fork)
	}
	fork, err = GetEntriesToFork(sess.Storage(), ForkOptions{EntryID: &second, Position: ForkAt})
	if err != nil {
		t.Fatal(err)
	}
	if len(fork) != 2 || fork[1].ID() != second {
		t.Fatalf("fork-at mismatch: %#v", fork)
	}
	if len(repo.List()) != 1 {
		t.Fatalf("repo list mismatch")
	}
}

func TestToSessionWrapsStorageLikeUpstreamRepoUtils(t *testing.T) {
	storage := NewMemoryStorage(Metadata{ID: "s1", CreatedAt: "now"})
	sess := ToSession(storage)
	id, err := sess.AppendMessage(agent.NewUserMessage("hello"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := storage.GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID() != id {
		t.Fatalf("session should append through wrapped storage, got %#v", entries)
	}
}

func TestMemorySessionStorageConstructorsMatchUpstream(t *testing.T) {
	storage := NewMemorySessionStorage()
	metadata, err := storage.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if metadata["id"] == "" || metadata["createdAt"] == "" {
		t.Fatalf("new memory storage should mint metadata, got %#v", metadata)
	}
	withMetadata := NewMemorySessionStorageWithMetadata(Metadata{ID: "fixed", CreatedAt: "now"})
	metadata, err = withMetadata.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if metadata["id"] != "fixed" || metadata["createdAt"] != "now" {
		t.Fatalf("with metadata mismatch: %#v", metadata)
	}
	methodStorage := NewMemorySessionStorage().WithMetadata(Metadata{ID: "method", CreatedAt: "later"})
	metadata, err = methodStorage.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if metadata["id"] != "method" || metadata["createdAt"] != "later" {
		t.Fatalf("method with metadata mismatch: %#v", metadata)
	}
}

func TestSessionUpstreamExportedTypeAliases(t *testing.T) {
	var _ *MemorySessionRepo = NewMemoryRepo()
	var _ *JsonlSessionRepo = NewJSONLRepo(t.TempDir())
	var _ *MemorySessionStorage = NewMemorySessionStorage()
	var _ SessionStorage = NewMemorySessionStorage()
	var _ *JsonlSessionStorage = (*JSONLStorage)(nil)
	var _ SessionTreeEntry = NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hi"))
	var _ SessionMetadata = Metadata{ID: "s1", CreatedAt: "now"}
	var _ JsonlSessionMetadata = JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}}
	var _ SessionContext = Context{}
	var _ SessionContextModel = ContextModel{}
	parentID := "parent"
	entry := NewMessageEntry("m1", &parentID, "now", agent.NewUserMessage("hi"))
	if entry.ParentId() == nil || *entry.ParentId() != parentID || entry.TypeStr() != "message" {
		t.Fatalf("entry accessors mismatch: %#v", entry)
	}
}

func TestJsonlSessionStorageCreateOpenWrappersMatchUpstream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-name.jsonl")
	storage, err := CreateJsonlSessionStorage(path, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	if metadata := storage.Metadata(); metadata.ID != "session-name" || metadata.CWD != "/tmp/project" || metadata.Path != path {
		t.Fatalf("typed metadata mismatch: %#v", metadata)
	}
	metadata, err := storage.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if metadata["id"] != "session-name" || metadata["cwd"] != "/tmp/project" || metadata["path"] != path {
		t.Fatalf("metadata mismatch: %#v", metadata)
	}
	if storage.Path() != path {
		t.Fatalf("path accessor mismatch: %q", storage.Path())
	}
	reopened, err := OpenJsonlSessionStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata := reopened.Metadata(); metadata.ID != "session-name" || metadata.CWD != "/tmp/project" || metadata.Path != path {
		t.Fatalf("reopened typed metadata mismatch: %#v", metadata)
	}
	metadata, err = reopened.MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if metadata["id"] != "session-name" || metadata["cwd"] != "/tmp/project" || metadata["path"] != path {
		t.Fatalf("reopened metadata mismatch: %#v", metadata)
	}
}

func TestJSONLRepoRootAccessorMatchesUpstream(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	repo := NewJSONLRepo(root)
	if repo.Root() != root {
		t.Fatalf("root accessor mismatch: %q", repo.Root())
	}
}

func TestSessionRepoConstructorsMatchUpstreamNames(t *testing.T) {
	memoryRepo := NewMemorySessionRepo()
	if memoryRepo.Count() != 0 {
		t.Fatalf("new memory repo should be empty")
	}
	root := filepath.Join(t.TempDir(), "sessions")
	jsonlRepo := NewJsonlSessionRepo(root)
	if jsonlRepo.Root() != root {
		t.Fatalf("jsonl repo root mismatch: %q", jsonlRepo.Root())
	}
}

func TestJSONLRepoUpstreamFunctionAliases(t *testing.T) {
	repo := NewJSONLRepo(t.TempDir())
	first, err := Create(repo, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Create(repo, "/tmp/project")
	if err != nil {
		t.Fatal(err)
	}

	firstMeta, err := first.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	secondMeta, err := second.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	firstPath := firstMeta["path"].(string)
	secondPath := secondMeta["path"].(string)

	entries, err := ListEntries(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Path != firstPath || entries[1].Path != secondPath {
		t.Fatalf("entries mismatch: %#v", entries)
	}

	found, err := FindPathById(repo, firstMeta["id"].(string))
	if err != nil || found == nil || *found != firstPath {
		t.Fatalf("FindPathById mismatch path=%v err=%v", found, err)
	}
	found, err = FindPathByID(repo, secondMeta["id"].(string))
	if err != nil || found == nil || *found != secondPath {
		t.Fatalf("FindPathByID mismatch path=%v err=%v", found, err)
	}

	newest, err := NewestPath(repo)
	if err != nil || newest == nil || *newest != secondPath {
		t.Fatalf("NewestPath mismatch path=%v err=%v", newest, err)
	}
	resumed, err := Resume(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	resumedMeta, err := resumed.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if resumedMeta["id"] != secondMeta["id"] {
		t.Fatalf("Resume newest mismatch: %#v", resumedMeta)
	}
	explicitID := firstMeta["id"].(string)
	resumed, err = Resume(repo, &explicitID)
	if err != nil {
		t.Fatal(err)
	}
	resumedMeta, err = resumed.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	if resumedMeta["id"] != firstMeta["id"] {
		t.Fatalf("Resume explicit mismatch: %#v", resumedMeta)
	}

	deleted, err := DeleteById(repo, firstMeta["id"].(string))
	if err != nil || deleted == nil || *deleted != firstPath {
		t.Fatalf("DeleteById mismatch path=%v err=%v", deleted, err)
	}
	if found, err := FindPathById(repo, firstMeta["id"].(string)); err != nil || found != nil {
		t.Fatalf("deleted session should not be found path=%v err=%v", found, err)
	}
}

func TestMemoryRepoForkCreatesSessionFromSelectedEntries(t *testing.T) {
	repo := NewMemoryRepo()
	sess := repo.Create()
	first, _ := sess.AppendMessage(agent.NewUserMessage("first"))
	second, _ := sess.AppendMessage(agent.NewUserMessage("second"))
	_, _ = sess.AppendMessage(agent.NewAssistantMessage("after"))

	fork, err := repo.Fork(sess, ForkOptions{EntryID: &second, Position: ForkBefore})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fork.Storage().GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID() != first {
		t.Fatalf("fork entries mismatch: %#v", entries)
	}
	leaf, err := fork.LeafID()
	if err != nil || leaf == nil || *leaf != first {
		t.Fatalf("fork leaf mismatch leaf=%v err=%v", leaf, err)
	}
	if len(repo.List()) != 2 {
		t.Fatalf("repo list should include fork")
	}
}

func TestSessionEntryMarshalRejectsUnknownTypeLikeUpstreamEnum(t *testing.T) {
	_, err := json.Marshal(Entry{EntryType: EntryType("future"), EntryID: "e1", Timestamp: "now"})
	if err == nil {
		t.Fatal("expected unknown session entry type to fail marshal like upstream enum")
	}
}

func TestSessionEntryMarshalRejectsNilMessageLikeUpstreamStruct(t *testing.T) {
	_, err := json.Marshal(Entry{EntryType: EntryTypeMessage, EntryID: "m1", Timestamp: "now"})
	if err == nil {
		t.Fatal("expected message entry without message to fail marshal like upstream struct")
	}
}

func TestSessionEntrySerializesNilParentIDLikeUpstreamOption(t *testing.T) {
	data, err := json.Marshal(NewMessageEntry("m1", nil, "now", agent.NewUserMessage("hello")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"parentId":null`) {
		t.Fatalf("nil parentId should serialize as null like upstream Option field, got %s", data)
	}
}

func TestJSONLStorageDoesNotHTMLEscapeLikeUpstreamSerde(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: "s1", CreatedAt: "now"}, CWD: "/tmp/a < b && c > d", Path: path})
	if err != nil {
		t.Fatal(err)
	}
	message := agent.NewUserMessage("a < b && c > d")
	if err := storage.AppendEntry(NewMessageEntry("m1", nil, "now", message)); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("jsonl should match upstream serde_json formatting without HTML escaping, got %s", text)
	}
	if !strings.Contains(text, `/tmp/a < b && c > d`) || !strings.Contains(text, `a < b && c > d`) {
		t.Fatalf("jsonl should preserve literal text, got %s", text)
	}
}

func TestMemoryStorageSetLeafIDOnlyMovesPointerLikeUpstream(t *testing.T) {
	storage := NewMemoryStorage(Metadata{ID: "s1", CreatedAt: "now"})
	first := NewMessageEntry("m1", nil, "ts1", agent.NewUserMessage("hello"))
	if err := storage.AppendEntry(first); err != nil {
		t.Fatal(err)
	}
	if err := storage.SetLeafID(nil); err != nil {
		t.Fatal(err)
	}
	entries, err := storage.GetEntries()
	if err != nil {
		t.Fatal(err)
	}
	leafID, err := storage.GetLeafID()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || leafID != nil {
		t.Fatalf("MemoryStorage SetLeafID should only move pointer like upstream memory storage, entries=%#v leaf=%v", entries, leafID)
	}
}

func TestMemoryStorageAppendLeafEntryUsesEntryIDLikeUpstream(t *testing.T) {
	storage := NewMemoryStorage(Metadata{ID: "s1", CreatedAt: "now"})
	targetID := "m1"
	if err := storage.AppendEntry(Entry{EntryType: EntryTypeLeaf, EntryID: "leaf1", Timestamp: "ts", TargetID: &targetID}); err != nil {
		t.Fatal(err)
	}
	leafID, err := storage.GetLeafID()
	if err != nil {
		t.Fatal(err)
	}
	if leafID == nil || *leafID != "leaf1" {
		t.Fatalf("MemoryStorage append_entry should point at appended entry id like upstream memory storage, got %v", leafID)
	}
}

func TestSessionLabelRejectsNullTargetIDLikeUpstreamSerde(t *testing.T) {
	var entry Entry
	err := json.Unmarshal([]byte(`{"type":"label","id":"l1","parentId":null,"timestamp":"now","targetId":null}`), &entry)
	if err == nil {
		t.Fatal("expected label targetId null to fail like upstream non-Option string")
	}
}

func TestMemoryRepoCountAndDeleteByID(t *testing.T) {
	repo := NewMemoryRepo()
	first := repo.Create()
	second := repo.Create()
	metadata, err := first.Storage().MetadataJSON()
	if err != nil {
		t.Fatal(err)
	}
	firstID, _ := metadata["id"].(string)

	if repo.Count() != 2 {
		t.Fatalf("count mismatch: %d", repo.Count())
	}
	deleted, err := repo.DeleteByID(firstID)
	if err != nil || !deleted {
		t.Fatalf("delete mismatch deleted=%v err=%v", deleted, err)
	}
	if repo.Count() != 1 || repo.List()[0] != second {
		t.Fatalf("repo contents mismatch: %#v", repo.List())
	}
	deleted, err = repo.DeleteByID(firstID)
	if err != nil || deleted {
		t.Fatalf("second delete mismatch deleted=%v err=%v", deleted, err)
	}
}

func stringPtr(value string) *string { return &value }

func TestSessionAppendSystemPromptStoresLLMMessage(t *testing.T) {
	sess := NewSession(NewMemoryStorage(Metadata{ID: "s1", CreatedAt: "now"}))
	message := ai.Message{Role: ai.RoleSystem, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "system prompt"}}}
	if _, err := sess.AppendSystemPrompt(message); err != nil {
		t.Fatal(err)
	}
	entries, err := sess.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].EntryType != EntryTypeCustom || entries[0].CustomType != "system_prompt" {
		t.Fatalf("entries = %#v", entries)
	}
	data := entries[0].Data.(map[string]any)
	stored := data["message"].(ai.Message)
	if stored.Role != ai.RoleSystem || stored.Content[0].Text != "system prompt" {
		t.Fatalf("stored message = %#v", stored)
	}
}
