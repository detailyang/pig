package commands

import (
	"context"
	"testing"
)

func TestNameCommandShowsCurrentName(t *testing.T) {
	registry := DefaultRegistry()
	unnamed := Dispatch(context.Background(), "/name", registry, Context{})
	if unnamed.Kind != OutcomeHandled || unnamed.Message != "(unnamed session)" {
		t.Fatalf("unnamed mismatch: %#v", unnamed)
	}
	current := "release notes"
	shown := Dispatch(context.Background(), "/name", registry, Context{SessionName: &current})
	if shown.Kind != OutcomeHandled || shown.Message != "session name: release notes" {
		t.Fatalf("shown mismatch: %#v", shown)
	}
}

func TestNameCommandReturnsSetNameOutcome(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/name sprint planning", registry, Context{})
	if out.Kind != OutcomeSetSessionName || out.Name != "sprint planning" || out.Message != "session name set to: sprint planning" {
		t.Fatalf("set mismatch: %#v", out)
	}
	quoted := Dispatch(context.Background(), `/name "quoted name"`, registry, Context{})
	if quoted.Kind != OutcomeSetSessionName || quoted.Name != "quoted name" {
		t.Fatalf("quoted mismatch: %#v", quoted)
	}
}

func TestNameCommandRejectsEmptyName(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/name   ", registry, Context{})
	if out.Kind != OutcomeHandled || out.Message != "(unnamed session)" {
		t.Fatalf("empty input should show current name, got %#v", out)
	}
	quoted := Dispatch(context.Background(), `/name "   "`, registry, Context{})
	if quoted.Kind != OutcomeError || quoted.Message != "empty name" {
		t.Fatalf("quoted empty mismatch: %#v", quoted)
	}
}
