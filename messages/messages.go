package messages

import (
	"time"

	"github.com/detailyang/pig/agent"
)

func CompactionSummary(summary string) agent.AgentMessage {
	return Custom("compaction_summary", map[string]any{"summary": summary})
}

func BranchSummary(summary string) agent.AgentMessage {
	return Custom("branch_summary", map[string]any{"summary": summary})
}

func Custom(role string, payload any) agent.AgentMessage {
	return agent.AgentMessage{
		Kind: agent.MessageKindCustom,
		Custom: &agent.CustomMessage{
			Role:      role,
			Timestamp: time.Now().UnixMilli(),
			Payload:   payload,
		},
	}
}
