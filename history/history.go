package history

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/detailyang/pig/config"
)

const MaxEntries = 1000

type Store struct {
	path    string
	entries []string
}

type HistoryStore = Store

func Load() *Store {
	return LoadFrom(DefaultPath())
}

func DefaultPath() string {
	return filepath.Join(config.BaseDir(), "history")
}

func LoadFrom(path string) *Store {
	data, err := os.ReadFile(path)
	if err != nil {
		return &Store{path: path}
	}
	if !utf8.Valid(data) {
		return &Store{path: path}
	}
	entries := make([]string, 0)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		entries = append(entries, line)
	}
	return &Store{path: path, entries: entries}
}

func (store *Store) Entries() []string {
	return append([]string(nil), store.entries...)
}

func (store *Store) Len() int {
	return len(store.entries)
}

func (store *Store) IsEmpty() bool {
	return len(store.entries) == 0
}

func (store *Store) Append(prompt string) error {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return nil
	}
	if len(store.entries) > 0 && store.entries[len(store.entries)-1] == trimmed {
		return nil
	}
	store.entries = append(store.entries, trimmed)
	if len(store.entries) > MaxEntries {
		overflow := len(store.entries) - MaxEntries
		store.entries = append([]string(nil), store.entries[overflow:]...)
	}
	return store.Save()
}

func (store *Store) Save() error {
	if parent := filepath.Dir(store.path); parent != "." && parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	body := strings.Join(store.entries, "\n") + "\n"
	return os.WriteFile(store.path, []byte(body), 0o644)
}
