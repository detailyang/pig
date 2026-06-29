package resumepicker

import (
	"strings"
	"testing"
)

func TestVisibleWindowShowsEverythingAndFollowsSelection(t *testing.T) {
	if start, end := VisibleWindow(0, 5, 10); start != 0 || end != 5 {
		t.Fatalf("fit top=%d..%d", start, end)
	}
	start, end := VisibleWindow(50, 100, 10)
	if end-start != 10 || start > 50 || end <= 50 {
		t.Fatalf("selection not visible: %d..%d", start, end)
	}
	if start, end := VisibleWindow(99, 100, 10); start != 90 || end != 100 {
		t.Fatalf("tail=%d..%d", start, end)
	}
}

func TestRenderLinesIncludesPinnedCleanBadgesScrollAndTruncates(t *testing.T) {
	rows := make([]Row, 20)
	for i := range rows {
		rows[i] = row("session-" + twoDigits(i))
	}
	rows[0].Badge = "2 cron, 1 trigger"
	lines := RenderLines(rows, 0, 100, 5)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "resume a session (20 total)") || !strings.Contains(joined, "→ ✚ start a new session") || !strings.Contains(joined, "[2 cron, 1 trigger]") || !strings.Contains(joined, "more below") {
		t.Fatalf("bad render:\n%s", joined)
	}
	if !strings.Contains(joined, "\x1b[7m→ ✚ start a new session\x1b[0m") {
		t.Fatalf("selected line should use reverse video ANSI:\n%s", joined)
	}
	lines = RenderLines(rows, 15, 100, 5)
	joined = strings.Join(lines, "\n")
	if !strings.Contains(joined, "session-14") || !strings.Contains(joined, "→ session-14") || !strings.Contains(joined, "more above") {
		t.Fatalf("selected row not visible:\n%s", joined)
	}
	if !strings.Contains(joined, "\x1b[7m→ session-14") || !strings.Contains(joined, "\x1b[0m") {
		t.Fatalf("selected session should use reverse video ANSI:\n%s", joined)
	}
	long := row("session-long")
	long.Preview = strings.Repeat("x", 500)
	for _, line := range RenderLines([]Row{long}, 0, 60, 5) {
		if len([]rune(line)) > 60 {
			t.Fatalf("line too wide (%d): %q", len([]rune(line)), line)
		}
	}
}

func TestRenderLinesWidthZeroMatchesUpstreamSaturatingTruncate(t *testing.T) {
	lines := RenderLines(nil, 0, 0, 1)
	if len(lines) != 2 || lines[0] != "…" || lines[1] != "\x1b[7m…\x1b[0m" {
		t.Fatalf("width zero render mismatch: %#v", lines)
	}
}

func TestStateActionsAndChoices(t *testing.T) {
	var rows []PickerRow = []Row{row("s0"), row("s1")}
	if _, err := PickBlocking(rows); err == nil {
		t.Fatal("PickBlocking should report that interactive picker is unavailable in the library port")
	}
	state := NewState([]Row{row("s0"), row("s1")})
	if choice := state.Apply(ActionSelect); choice != PickerChoiceClean {
		t.Fatalf("clean choice=%#v", choice)
	}
	state = NewState([]Row{row("s0"), row("s1")})
	state.Apply(ActionDown)
	state.Apply(ActionDown)
	state.Apply(ActionDown)
	if state.Selected != 2 {
		t.Fatalf("selected should clamp at 2, got %d", state.Selected)
	}
	if choice := state.Apply(ActionSelect); choice.Kind != ChoiceResume || choice.Index != 1 {
		t.Fatalf("resume choice=%#v", choice)
	}
	if choice := PickerChoiceResume(1); choice.Kind != ChoiceResume || choice.Index != 1 {
		t.Fatalf("resume constructor=%#v", choice)
	}
	state.Apply(ActionHome)
	if state.Selected != 0 {
		t.Fatalf("home selected=%d", state.Selected)
	}
	state.Apply(ActionEnd)
	if state.Selected != 2 {
		t.Fatalf("end selected=%d", state.Selected)
	}
	state.Apply(ActionPageUp)
	if state.Selected != 0 {
		t.Fatalf("page up selected=%d", state.Selected)
	}
	if choice := state.Apply(ActionCancel); choice != PickerChoiceCancelled {
		t.Fatalf("cancel=%#v", choice)
	}
}

func row(id string) Row {
	return Row{IDShort: id, CreatedAt: "2026-06-11T03:43", Preview: "preview for " + id}
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return "1" + string(rune('0'+value-10))
}
