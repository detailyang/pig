package triggers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/detailyang/pig/config"
)

const MaxInboxEntryTextChars = 500
const MaxEntryTextChars = MaxInboxEntryTextChars
const MAX_ENTRY_TEXT_CHARS = MaxInboxEntryTextChars

type InboxStatus string

const (
	InboxStatusNew       InboxStatus = "new"
	InboxStatusClaimed   InboxStatus = "claimed"
	InboxStatusDismissed InboxStatus = "dismissed"
)

type InboxEntry struct {
	ID        string      `json:"id"`
	CreatedAt string      `json:"created_at"`
	Source    string      `json:"source"`
	Text      string      `json:"text"`
	TraceID   string      `json:"trace_id"`
	SessionID string      `json:"session_id"`
	Status    InboxStatus `json:"status"`
}

var inboxWriteMu sync.Mutex

func DefaultInboxPath() string {
	return filepath.Join(config.BaseDir(), "inbox.jsonl")
}

func Append(path string, source string, text string, traceID string, sessionID string) (InboxEntry, error) {
	return AppendInbox(path, source, text, traceID, sessionID)
}

func AppendInbox(path string, source string, text string, traceID string, sessionID string) (InboxEntry, error) {
	entry := InboxEntry{ID: "inb-" + randomInboxHex(16), CreatedAt: time.Now().UTC().Format(time.RFC3339), Source: truncateRunes(source, 80), Text: capRunes(strings.TrimSpace(text), MaxInboxEntryTextChars), TraceID: traceID, SessionID: sessionID, Status: InboxStatusNew}
	inboxWriteMu.Lock()
	defer inboxWriteMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return InboxEntry{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return InboxEntry{}, err
	}
	defer file.Close()
	data, err := marshalJSONNoHTMLEscape(entry)
	if err != nil {
		return InboxEntry{}, err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return InboxEntry{}, err
	}
	return entry, nil
}

func List(path string) ([]InboxEntry, error) {
	return ListInbox(path)
}

func ListInbox(path string) ([]InboxEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	var entries []InboxEntry
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var entry InboxEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func ListNew(path string) ([]InboxEntry, error) {
	return ListNewInbox(path)
}

func ListNewInbox(path string) ([]InboxEntry, error) {
	entries, err := ListInbox(path)
	if err != nil {
		return nil, err
	}
	var out []InboxEntry
	for _, entry := range entries {
		if entry.Status == InboxStatusNew {
			out = append(out, entry)
		}
	}
	return out, nil
}

func NewCount(path string) int {
	return NewInboxCount(path)
}

func NewInboxCount(path string) int {
	entries, err := ListNewInbox(path)
	if err != nil {
		return 0
	}
	return len(entries)
}

func SetStatus(path string, id string, status InboxStatus) (*InboxEntry, error) {
	return SetInboxStatus(path, id, status)
}

func SetInboxStatus(path string, id string, status InboxStatus) (*InboxEntry, error) {
	inboxWriteMu.Lock()
	defer inboxWriteMu.Unlock()
	entries, err := ListInbox(path)
	if err != nil {
		return nil, err
	}
	var updated *InboxEntry
	for index := range entries {
		if entries[index].ID == id {
			entries[index].Status = status
			copyEntry := entries[index]
			updated = &copyEntry
		}
	}
	if updated == nil {
		return nil, nil
	}
	if err := writeInboxEntries(path, entries); err != nil {
		return nil, err
	}
	return updated, nil
}

func DismissAllNew(path string) (int, error) {
	return DismissAllNewInbox(path)
}

func DismissAllNewInbox(path string) (int, error) {
	inboxWriteMu.Lock()
	defer inboxWriteMu.Unlock()
	entries, err := ListInbox(path)
	if err != nil {
		return 0, err
	}
	count := 0
	for index := range entries {
		if entries[index].Status == InboxStatusNew {
			entries[index].Status = InboxStatusDismissed
			count++
		}
	}
	if count == 0 {
		return 0, nil
	}
	if err := writeInboxEntries(path, entries); err != nil {
		return 0, err
	}
	return count, nil
}

func writeInboxEntries(path string, entries []InboxEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var builder strings.Builder
	for _, entry := range entries {
		data, err := marshalJSONNoHTMLEscape(entry)
		if err != nil {
			return err
		}
		builder.Write(data)
		builder.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func capRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return value
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}

func randomInboxHex(size int) string {
	bytes := make([]byte, size)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
