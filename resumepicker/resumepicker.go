package resumepicker

import "fmt"

const SelectedPrefix = "→ "
const UnselectedPrefix = "  "

type Row struct {
	IDShort   string
	CreatedAt string
	Badge     string
	Preview   string
}

type PickerRow = Row

type ChoiceKind string

const (
	ChoiceNone      ChoiceKind = "none"
	ChoiceClean     ChoiceKind = "clean"
	ChoiceResume    ChoiceKind = "resume"
	ChoiceCancelled ChoiceKind = "cancelled"
)

type Choice struct {
	Kind  ChoiceKind
	Index int
}

type PickerChoice = Choice

var NoChoice = Choice{Kind: ChoiceNone}
var PickerChoiceClean = Choice{Kind: ChoiceClean}
var PickerChoiceCancelled = Choice{Kind: ChoiceCancelled}

func PickerChoiceResume(index int) PickerChoice {
	return Choice{Kind: ChoiceResume, Index: index}
}

func PickBlocking(rows []PickerRow) (PickerChoice, error) {
	return PickerChoiceCancelled, fmt.Errorf("interactive resume picker is not available in the library port")
}

type Action string

const (
	ActionUp       Action = "up"
	ActionDown     Action = "down"
	ActionPageUp   Action = "page_up"
	ActionPageDown Action = "page_down"
	ActionHome     Action = "home"
	ActionEnd      Action = "end"
	ActionSelect   Action = "select"
	ActionCancel   Action = "cancel"
	ActionNone     Action = "none"
)

type State struct {
	Rows     []Row
	Selected int
}

func NewState(rows []Row) *State {
	return &State{Rows: append([]Row(nil), rows...)}
}

func (state *State) Apply(action Action) Choice {
	total := EntryCount(len(state.Rows))
	switch action {
	case ActionUp:
		if state.Selected > 0 {
			state.Selected--
		}
	case ActionDown:
		if state.Selected+1 < total {
			state.Selected++
		}
	case ActionPageUp:
		state.Selected -= 10
		if state.Selected < 0 {
			state.Selected = 0
		}
	case ActionPageDown:
		state.Selected += 10
		if state.Selected >= total {
			state.Selected = total - 1
		}
	case ActionHome:
		state.Selected = 0
	case ActionEnd:
		state.Selected = total - 1
	case ActionSelect:
		if state.Selected == 0 {
			return PickerChoiceClean
		}
		return Choice{Kind: ChoiceResume, Index: state.Selected - 1}
	case ActionCancel:
		return PickerChoiceCancelled
	}
	if state.Selected < 0 {
		state.Selected = 0
	}
	return NoChoice
}

func EntryCount(rows int) int {
	return rows + 1
}

func VisibleWindow(selected int, total int, height int) (int, int) {
	if height < 1 {
		height = 1
	}
	if total <= height {
		return 0, total
	}
	start := selected - height/2
	if start < 0 {
		start = 0
	}
	if start > total-height {
		start = total - height
	}
	if start > selected {
		start = selected
	}
	return start, start + height
}

func RenderLines(rows []Row, selected int, width int, height int) []string {
	total := EntryCount(len(rows))
	start, end := VisibleWindow(selected, total, height)
	lines := []string{truncateLine(fmt.Sprintf("resume a session (%d total) — ↑/↓ move · Enter select · q cancel", len(rows)), width)}
	if start > 0 {
		lines = append(lines, truncateLine(fmt.Sprintf("  … %d more above", start), width))
	}
	for index := start; index < end; index++ {
		prefix := UnselectedPrefix
		if index == selected {
			prefix = SelectedPrefix
		}
		body := "✚ start a new session"
		if index > 0 {
			row := rows[index-1]
			badge := ""
			if row.Badge != "" {
				badge = "  [" + row.Badge + "]"
			}
			body = fmt.Sprintf("%s  %s%s  %s", row.IDShort, row.CreatedAt, badge, row.Preview)
		}
		line := truncateLine(prefix+body, width)
		if index == selected {
			line = "\x1b[7m" + line + "\x1b[0m"
		}
		lines = append(lines, line)
	}
	if end < total {
		lines = append(lines, truncateLine(fmt.Sprintf("  … %d more below", total-end), width))
	}
	return lines
}

func truncateLine(line string, width int) string {
	runes := []rune(line)
	if len(runes) <= width {
		return line
	}
	if width <= 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
