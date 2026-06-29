package agentsession

import "github.com/detailyang/pig/agent"

type AgentSessionEventType string

const (
	AgentSessionEventAutoRetryStart AgentSessionEventType = "auto_retry_start"
	AgentSessionEventAutoRetryEnd   AgentSessionEventType = "auto_retry_end"
)

type AgentSessionEvent struct {
	Type         AgentSessionEventType
	Attempt      uint32
	MaxAttempts  uint32
	DelayMS      uint64
	ErrorMessage string
	Success      bool
	FinalError   *string
}

func ForwardToListener(agent.AgentEvent) {}
