package replkernel

import (
	"context"
	"errors"
	"testing"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/harness"
	"github.com/detailyang/pig/session"
)

func TestQueuedTurnDisplayMatchesUpstreamVariants(t *testing.T) {
	turns := []QueuedTurn{
		UserPromptTurn("show user", "prompt", nil),
		AgentPromptTurn("show agent", "agent", "triggered turn: "),
		PromptTemplateTurn("show template", "daily", map[string]any{"name": "Ada"}),
		CompactionTurn("show compaction", "custom"),
	}
	want := []string{"show user", "show agent", "show template", "show compaction"}
	for index, turn := range turns {
		if turn.Display() != want[index] {
			t.Fatalf("display mismatch index=%d got=%q want=%q", index, turn.Display(), want[index])
		}
	}
}

func TestTurnStatePollClearsCompletedTurn(t *testing.T) {
	state := TurnState{Fut: CompletedTurn("done")}
	message, err := state.Poll(context.Background())
	if err != nil || message != "done" || state.Fut != nil {
		t.Fatalf("poll mismatch message=%q err=%v state=%#v", message, err, state)
	}
	if message, err := state.Poll(context.Background()); err != nil || message != "" {
		t.Fatalf("empty poll mismatch message=%q err=%v", message, err)
	}
}

func TestKernelDelegatesToHarnessLikeUpstreamReplKernel(t *testing.T) {
	fake := &fakeHarness{acceptsImages: true, compacted: true}
	kernel := New(fake)
	if kernel.Harness() != fake || !kernel.CurrentModelAcceptsImages() {
		t.Fatalf("kernel accessors mismatch")
	}
	if _, err := kernel.PromptTurn("plain")(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := kernel.UserPromptTurn("with image", []ai.ContentBlock{{Type: ai.ContentImage}})(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := kernel.TemplateTurn("daily", map[string]any{"name": "Ada"})(context.Background()); err != nil {
		t.Fatal(err)
	}
	message, err := kernel.CompactionTurn("custom")(context.Background())
	if err != nil || message != "compaction ran" {
		t.Fatalf("compaction mismatch message=%q err=%v", message, err)
	}
	if _, err := kernel.ContinueTurn()(context.Background()); err != nil {
		t.Fatal(err)
	}
	kernel.Abort()
	if got := fake.calls; !equalStrings(got, []string{"prompt:plain", "images:with image:1", "template:daily", "compact", "continue", "abort"}) {
		t.Fatalf("calls mismatch: %#v", got)
	}
}

func TestCompactionTurnReturnsNothingToCompactMessage(t *testing.T) {
	kernel := New(&fakeHarness{})
	message, err := kernel.CompactionTurn("")(context.Background())
	if err != nil || message != "nothing to compact" {
		t.Fatalf("compaction mismatch message=%q err=%v", message, err)
	}
}

func TestAgentHarnessAdapterSatisfiesKernelHarness(t *testing.T) {
	h := harness.NewAgentHarness(harness.NewAgentHarnessOptions(ai.Model{ID: "test-model"}, session.NewSession(session.NewMemorySessionStorage())))
	adapter := NewAgentHarnessAdapter(h)
	var _ Harness = adapter
	kernel := New(adapter)
	if kernel.Harness() != adapter || kernel.IsStreaming() || kernel.CurrentModelAcceptsImages() {
		t.Fatalf("adapter initial state mismatch")
	}
	kernel.Abort()
}

func TestCompletedTurnPropagatesError(t *testing.T) {
	want := errors.New("boom")
	_, err := CompletedTurnError(want)(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("expected error %v, got %v", want, err)
	}
}

type fakeHarness struct {
	calls         []string
	acceptsImages bool
	compacted     bool
}

func (fake *fakeHarness) Abort() { fake.calls = append(fake.calls, "abort") }

func (fake *fakeHarness) IsStreaming() bool { return false }

func (fake *fakeHarness) CurrentModelAcceptsImages() bool { return fake.acceptsImages }

func (fake *fakeHarness) Prompt(ctx context.Context, prompt string) error {
	fake.calls = append(fake.calls, "prompt:"+prompt)
	return nil
}

func (fake *fakeHarness) PromptWithImages(ctx context.Context, prompt string, images []ai.ContentBlock) error {
	fake.calls = append(fake.calls, "images:"+prompt+":"+string(rune('0'+len(images))))
	return nil
}

func (fake *fakeHarness) PromptFromTemplate(ctx context.Context, name string, vars map[string]any) error {
	fake.calls = append(fake.calls, "template:"+name)
	return nil
}

func (fake *fakeHarness) ForceCompact(ctx context.Context, custom string) (bool, error) {
	fake.calls = append(fake.calls, "compact")
	return fake.compacted, nil
}

func (fake *fakeHarness) Continue(ctx context.Context) error {
	fake.calls = append(fake.calls, "continue")
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
