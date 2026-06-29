package inbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInboxPackageMirrorsUpstreamPersistentAPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inbox.jsonl")
	entry, err := Append(path, strings.Repeat("source", 20), "  finding  ", "trace-1", "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Status != InboxStatusNew || entry.Text != "finding" || len([]rune(entry.Source)) != 80 || !strings.HasPrefix(entry.ID, "inb-") {
		t.Fatalf("append entry mismatch: %#v", entry)
	}
	entries, err := List(path)
	if err != nil || len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("list mismatch entries=%#v err=%v", entries, err)
	}
	newEntries, err := ListNew(path)
	if err != nil || len(newEntries) != 1 || NewCount(path) != 1 {
		t.Fatalf("new list/count mismatch entries=%#v count=%d err=%v", newEntries, NewCount(path), err)
	}
	claimed, err := SetStatus(path, entry.ID, InboxStatusClaimed)
	if err != nil || claimed == nil || claimed.Status != InboxStatusClaimed || NewCount(path) != 0 {
		t.Fatalf("set status mismatch claimed=%#v count=%d err=%v", claimed, NewCount(path), err)
	}
	second, err := Append(path, "cron:daily", strings.Repeat("界", MaxEntryTextChars+1), "trace-2", "sess-2")
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(second.Text)) != MaxEntryTextChars+1 || !strings.HasSuffix(second.Text, "…") {
		t.Fatalf("capped text mismatch: %#v", second.Text)
	}
	dismissed, err := DismissAllNew(path)
	if err != nil || dismissed != 1 || NewCount(path) != 0 {
		t.Fatalf("dismiss mismatch dismissed=%d count=%d err=%v", dismissed, NewCount(path), err)
	}
}

func TestInboxPackageDefaultPathUsesPieDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", dir)
	if got := DefaultInboxPath(); got != filepath.Join(dir, "inbox.jsonl") {
		t.Fatalf("default path mismatch: %q", got)
	}
}
