package commands

import (
	"context"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/session"
)

func TestUndoCommandMovesToParentOfMostRecentUserMessage(t *testing.T) {
	registry := DefaultRegistry()
	rootID := "root"
	user1 := session.NewMessageEntry("u1", &rootID, "t1", agent.NewUserMessage("first"))
	assistant1 := session.NewMessageEntry("a1", stringPtr("u1"), "t2", agent.NewAssistantMessage("answer"))
	user2 := session.NewMessageEntry("u2", stringPtr("a1"), "t3", agent.NewUserMessage("second"))
	assistant2 := session.NewMessageEntry("a2", stringPtr("u2"), "t4", agent.NewAssistantMessage("answer 2"))
	out := Dispatch(context.Background(), "/undo", registry, Context{Branch: []session.Entry{rootEntry(rootID), user1, assistant1, user2, assistant2}})
	if out.Kind != OutcomeMoveTo || out.TargetID == nil || *out.TargetID != "a1" || out.Message != "undid last turn" {
		t.Fatalf("undo mismatch: %#v", out)
	}
}

func TestUndoCommandHandlesRootUserAndErrors(t *testing.T) {
	registry := DefaultRegistry()
	rootUser := session.NewMessageEntry("u1", nil, "t1", agent.NewUserMessage("first"))
	out := Dispatch(context.Background(), "/undo", registry, Context{Branch: []session.Entry{rootUser, session.NewMessageEntry("a1", stringPtr("u1"), "t2", agent.NewAssistantMessage("answer"))}})
	if out.Kind != OutcomeMoveTo || out.TargetID != nil {
		t.Fatalf("root undo should move to nil parent: %#v", out)
	}
	missing := Dispatch(context.Background(), "/undo", registry, Context{Branch: []session.Entry{session.NewMessageEntry("a1", nil, "t2", agent.NewAssistantMessage("answer"))}})
	if missing.Kind != OutcomeError || missing.Message != "no user message to undo" {
		t.Fatalf("missing user mismatch: %#v", missing)
	}
	extra := Dispatch(context.Background(), "/undo extra", registry, Context{})
	if extra.Kind != OutcomeError || extra.Message != "no user message to undo" {
		t.Fatalf("undo should ignore args like upstream: %#v", extra)
	}
}

func rootEntry(id string) session.Entry {
	return session.Entry{EntryType: session.EntryTypeSessionInfo, EntryID: id, Timestamp: "t0"}
}

func stringPtr(value string) *string { return &value }
