package agentsession

import (
	"testing"

	"github.com/detailyang/pig/agent"
)

func TestAgentSessionEventCompatSurface(t *testing.T) {
	start := AgentSessionEvent{Type: AgentSessionEventAutoRetryStart, Attempt: 1, MaxAttempts: 5, DelayMS: 1000, ErrorMessage: "HTTP 503"}
	if start.Type != AgentSessionEventAutoRetryStart || start.Attempt != 1 || start.MaxAttempts != 5 || start.DelayMS != 1000 || start.ErrorMessage != "HTTP 503" {
		t.Fatalf("auto retry start mismatch: %#v", start)
	}
	finalError := "HTTP 503"
	end := AgentSessionEvent{Type: AgentSessionEventAutoRetryEnd, Success: false, Attempt: 5, FinalError: &finalError}
	if end.Type != AgentSessionEventAutoRetryEnd || end.Success || end.Attempt != 5 || end.FinalError == nil || *end.FinalError != "HTTP 503" {
		t.Fatalf("auto retry end mismatch: %#v", end)
	}
}

func TestForwardToListenerCompatNoop(t *testing.T) {
	ForwardToListener(agent.AgentEvent{Type: agent.EventTypeStart})
}
