package commands

import (
	"context"
	"strings"
	"testing"
)

func TestNewTriggerCommandReturnsAddOutcomeWhenParsable(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/new-trigger if tests fail, then run go test ./...", registry, Context{})
	if out.Kind != OutcomeAddTriggerRule || out.TriggerCondition != "tests fail," || out.TriggerAction != "go test ./..." || !out.FireOnce || out.PromoteToChat || out.Message != "add trigger rule: tests fail, -> go test ./..." {
		t.Fatalf("new trigger mismatch: %#v", out)
	}
}

func TestNewTriggerCommandFallsBackToAgentPromptForAmbiguousRequests(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/new-trigger watch the build", registry, Context{})
	if out.Kind != OutcomeRunPrompt || out.ErrorContext != "create trigger: " || !strings.Contains(out.Prompt, "The user asked pie to create a dynamic trigger") || !strings.Contains(out.Prompt, "User request:\nwatch the build") {
		t.Fatalf("fallback mismatch: %#v", out)
	}
}

func TestNewTriggerCommandUsage(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/new-trigger", registry, Context{})
	if out.Kind != OutcomeError || out.Message != "usage: /new-trigger <natural-language trigger request>" {
		t.Fatalf("usage mismatch: %#v", out)
	}
}
