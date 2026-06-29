package messages

import (
	"testing"

	"github.com/detailyang/pig/agent"
)

func TestCompactionSummaryMessageMatchesUpstream(t *testing.T) {
	message := CompactionSummary("short summary")
	assertCustomSummary(t, message, "compaction_summary", "short summary")
}

func TestBranchSummaryMessageMatchesUpstream(t *testing.T) {
	message := BranchSummary("branch summary")
	assertCustomSummary(t, message, "branch_summary", "branch summary")
}

func TestCustomMessageMatchesUpstream(t *testing.T) {
	message := Custom("notice", map[string]any{"ok": true})
	if message.Kind != agent.MessageKindCustom || message.Custom == nil {
		t.Fatalf("custom should return custom agent message: %#v", message)
	}
	if message.Custom.Role != "notice" || message.Custom.Timestamp == 0 {
		t.Fatalf("custom metadata mismatch: %#v", message.Custom)
	}
	payload, ok := message.Custom.Payload.(map[string]any)
	if !ok || payload["ok"] != true {
		t.Fatalf("custom payload mismatch: %#v", message.Custom.Payload)
	}
}

func assertCustomSummary(t *testing.T, message agent.AgentMessage, role string, summary string) {
	t.Helper()
	if message.Kind != agent.MessageKindCustom || message.Custom == nil {
		t.Fatalf("summary should return custom agent message: %#v", message)
	}
	if message.Custom.Role != role || message.Custom.Timestamp == 0 {
		t.Fatalf("summary metadata mismatch: %#v", message.Custom)
	}
	payload, ok := message.Custom.Payload.(map[string]any)
	if !ok || payload["summary"] != summary {
		t.Fatalf("summary payload mismatch: %#v", message.Custom.Payload)
	}
}
