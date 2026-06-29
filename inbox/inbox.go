package inbox

import "github.com/detailyang/pig/triggers"

const MaxEntryTextChars = triggers.MaxEntryTextChars
const MAX_ENTRY_TEXT_CHARS = triggers.MAX_ENTRY_TEXT_CHARS
const MAXENTRYTEXTCHARS = triggers.MAX_ENTRY_TEXT_CHARS

type InboxStatus = triggers.InboxStatus

const (
	InboxStatusNew       = triggers.InboxStatusNew
	InboxStatusClaimed   = triggers.InboxStatusClaimed
	InboxStatusDismissed = triggers.InboxStatusDismissed
)

type InboxEntry = triggers.InboxEntry

func DefaultInboxPath() string {
	return triggers.DefaultInboxPath()
}

func Append(path string, source string, text string, traceID string, sessionID string) (InboxEntry, error) {
	return triggers.Append(path, source, text, traceID, sessionID)
}

func List(path string) ([]InboxEntry, error) {
	return triggers.List(path)
}

func ListNew(path string) ([]InboxEntry, error) {
	return triggers.ListNew(path)
}

func NewCount(path string) int {
	return triggers.NewCount(path)
}

func SetStatus(path string, id string, status InboxStatus) (*InboxEntry, error) {
	return triggers.SetStatus(path, id, status)
}

func DismissAllNew(path string) (int, error) {
	return triggers.DismissAllNew(path)
}
