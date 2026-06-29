package history

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadFromMissingFileAndDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", dir)
	if DefaultPath() != filepath.Join(dir, "history") {
		t.Fatalf("default path mismatch: %s", DefaultPath())
	}
	store := LoadFrom(filepath.Join(dir, "missing"))
	if !store.IsEmpty() || store.Len() != 0 || len(store.Entries()) != 0 {
		t.Fatalf("missing file should load empty: %#v", store.Entries())
	}
}

func TestHistoryStoreAliasMatchesUpstreamName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	var store *HistoryStore = LoadFrom(path)
	if store == nil || !store.IsEmpty() {
		t.Fatalf("history store alias mismatch: %#v", store)
	}
	if err := store.Append("hello"); err != nil {
		t.Fatal(err)
	}
	if store.Len() != 1 || store.Entries()[0] != "hello" {
		t.Fatalf("history store methods mismatch: %#v", store.Entries())
	}
}

func TestAppendPersistsTrimsAndDedupesAdjacent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	store := LoadFrom(path)
	if err := store.Append(" first "); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("first"); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("second"); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("first"); err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "second", "first"}
	if !reflect.DeepEqual(store.Entries(), want) {
		t.Fatalf("entries mismatch: %#v", store.Entries())
	}
	reloaded := LoadFrom(path)
	if !reflect.DeepEqual(reloaded.Entries(), want) {
		t.Fatalf("persisted entries mismatch: %#v", reloaded.Entries())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\nsecond\nfirst\n" {
		t.Fatalf("file body mismatch: %q", string(data))
	}
}

func TestAppendSkipsWhitespaceOnly(t *testing.T) {
	store := LoadFrom(filepath.Join(t.TempDir(), "history"))
	for _, prompt := range []string{"", "   ", "\t\n"} {
		if err := store.Append(prompt); err != nil {
			t.Fatal(err)
		}
	}
	if !store.IsEmpty() {
		t.Fatalf("whitespace prompts should be skipped: %#v", store.Entries())
	}
}

func TestCapAtMaxEntriesDropsOldest(t *testing.T) {
	store := LoadFrom(filepath.Join(t.TempDir(), "history"))
	for i := 0; i < MaxEntries+50; i++ {
		if err := store.Append(fmt.Sprintf("entry-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if store.Len() != MaxEntries {
		t.Fatalf("len mismatch: %d", store.Len())
	}
	if got := store.Entries()[0]; got != "entry-50" {
		t.Fatalf("first entry mismatch: %s", got)
	}
}

func TestLoadFromFiltersEmptyLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	if err := os.WriteFile(path, []byte("one\n\n  \ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := LoadFrom(path)
	if !reflect.DeepEqual(store.Entries(), []string{"one", "two"}) {
		t.Fatalf("loaded entries mismatch: %#v", store.Entries())
	}
}

func TestLoadFromInvalidUTF8LoadsEmptyLikeUpstreamReadToString(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	if err := os.WriteFile(path, []byte("one\n\xff\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := LoadFrom(path)
	if !store.IsEmpty() {
		t.Fatalf("invalid UTF-8 history should load empty like upstream, got %#v", store.Entries())
	}
}
