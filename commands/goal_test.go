package commands

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/detailyang/pig/goal"
)

func TestGoalCommandShowsAndMutatesGoalState(t *testing.T) {
	registry := DefaultRegistry()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	current := goal.State{Condition: "ship port", Status: goal.StatusPursuing, Iterations: 2, UpdatedAt: now.Format(time.RFC3339)}
	show := Dispatch(context.Background(), "/goal", registry, Context{GoalState: &current})
	if show.Kind != OutcomeHandled || !strings.Contains(show.Message, "goal: ship port") || !strings.Contains(show.Message, "status: pursuing") || !strings.Contains(show.Message, "iterations: 2") {
		t.Fatalf("show mismatch: %#v", show)
	}
	set := Dispatch(context.Background(), "/goal finish the Go port", registry, Context{})
	if set.Kind != OutcomeSetGoal || set.Goal.Condition != "finish the Go port" || set.Goal.Status != goal.StatusPursuing || !strings.Contains(set.Message, "goal set: finish the Go port") {
		t.Fatalf("set mismatch: %#v", set)
	}
	paused := Dispatch(context.Background(), "/goal pause", registry, Context{GoalState: &current})
	if paused.Kind != OutcomeSetGoal || paused.Goal.Status != goal.StatusPaused || !strings.Contains(paused.Message, "goal paused: ship port") {
		t.Fatalf("pause mismatch: %#v", paused)
	}
	pausedState := paused.Goal
	resumed := Dispatch(context.Background(), "/goal resume", registry, Context{GoalState: &pausedState})
	if resumed.Kind != OutcomeSetGoal || resumed.Goal.Status != goal.StatusPursuing || !strings.Contains(resumed.Message, "goal resumed: ship port") {
		t.Fatalf("resume mismatch: %#v", resumed)
	}
	cleared := Dispatch(context.Background(), "/goal clear", registry, Context{GoalState: &current})
	if cleared.Kind != OutcomeSetGoal || cleared.Goal.Status != goal.StatusCleared || cleared.Message != "goal cleared" {
		t.Fatalf("clear mismatch: %#v", cleared)
	}
}

func TestGoalStartCommandsReturnRunPromptOnlyWhenActive(t *testing.T) {
	registry := DefaultRegistry()
	current := goal.State{Condition: "ship port", Status: goal.StatusPursuing, UpdatedAt: "now"}
	start := Dispatch(context.Background(), "/goal start inspect repo", registry, Context{GoalState: &current})
	if start.Kind != OutcomeRunPrompt || start.Prompt != "inspect repo" || start.ErrorContext != "goal start: " {
		t.Fatalf("start mismatch: %#v", start)
	}
	alias := Dispatch(context.Background(), "/goal-start inspect repo", registry, Context{GoalState: &current})
	if alias.Kind != OutcomeRunPrompt || alias.Prompt != "inspect repo" {
		t.Fatalf("goal-start mismatch: %#v", alias)
	}
	noGoal := Dispatch(context.Background(), "/goal start inspect repo", registry, Context{})
	if noGoal.Kind != OutcomeError || noGoal.Message != "no active goal; set one with /goal <condition>" {
		t.Fatalf("no goal mismatch: %#v", noGoal)
	}
	badUsage := Dispatch(context.Background(), "/goal-start", registry, Context{GoalState: &current})
	if badUsage.Kind != OutcomeError || badUsage.Message != "usage: /goal-start <prompt>" {
		t.Fatalf("usage mismatch: %#v", badUsage)
	}
}
