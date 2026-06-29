package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

type JSONLStorage struct {
	mu       sync.Mutex
	path     string
	metadata JSONLMetadata
	cache    []Entry
	loaded   bool
}

func (storage *JSONLStorage) Path() string {
	if storage == nil {
		return ""
	}
	return storage.path
}

func (storage *JSONLStorage) Metadata() JSONLMetadata {
	if storage == nil {
		return JSONLMetadata{}
	}
	return storage.metadata
}

func CreateJSONLStorage(path string, metadata JSONLMetadata) (*JSONLStorage, error) {
	if metadata.ID == "" {
		metadata.ID = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if metadata.ID == "" {
			metadata.ID = CreateSessionID()
		}
	}
	if metadata.CreatedAt == "" {
		metadata.CreatedAt = CreateTimestamp()
	}
	if metadata.Path == "" {
		metadata.Path = path
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, Error{Code: ErrorAlreadyExists, Message: path + " already exists"}
		}
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	defer file.Close()
	data, err := marshalJSONNoHTMLEscape(metadata)
	if err != nil {
		return nil, Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	return &JSONLStorage{path: path, metadata: metadata, cache: []Entry{}, loaded: true}, nil
}

func CreateJsonlSessionStorage(path string, cwd string) (*JSONLStorage, error) {
	return CreateJSONLStorage(path, JSONLMetadata{CWD: cwd, Path: path})
}

func OpenJSONLStorage(path string) (*JSONLStorage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	if !utf8.Valid(raw) {
		return nil, Error{Code: ErrorStorageFailure, Message: "invalid UTF-8"}
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil, Error{Code: ErrorCorrupted, Message: "missing metadata header"}
	}
	var metadata JSONLMetadata
	if err := json.Unmarshal([]byte(lines[0]), &metadata); err != nil {
		return nil, Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		return nil, Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	if !hasJSONLMetadataField(header, "id") || !hasJSONLMetadataField(header, "createdAt") || !hasJSONLMetadataField(header, "cwd") || !hasJSONLMetadataField(header, "path") {
		return nil, Error{Code: ErrorCorrupted, Message: "missing required metadata field"}
	}
	return &JSONLStorage{path: path, metadata: metadata}, nil
}

func hasJSONLMetadataField(header map[string]json.RawMessage, name string) bool {
	value, ok := header[name]
	return ok && string(value) != "null"
}

func OpenJsonlSessionStorage(path string) (*JSONLStorage, error) {
	return OpenJSONLStorage(path)
}

func (storage *JSONLStorage) MetadataJSON() (map[string]any, error) {
	data, err := marshalJSONNoHTMLEscape(storage.metadata)
	if err != nil {
		return nil, Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	return out, nil
}

func (storage *JSONLStorage) GetLeafID() (*string, error) {
	entries, err := storage.loadEntries()
	if err != nil {
		return nil, err
	}
	return latestLeaf(entries), nil
}

func (storage *JSONLStorage) SetLeafID(id *string) error {
	parent, err := storage.GetLeafID()
	if err != nil {
		return err
	}
	entry := Entry{EntryType: EntryTypeLeaf, EntryID: CreateSessionID(), ParentID: parent, Timestamp: CreateTimestamp(), TargetID: id}
	return storage.AppendEntry(entry)
}

func (storage *JSONLStorage) CreateEntryID() (string, error) { return CreateSessionID(), nil }

func (storage *JSONLStorage) AppendEntry(entry Entry) error {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	file, err := os.OpenFile(storage.path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	defer file.Close()
	data, err := marshalJSONNoHTMLEscape(entry)
	if err != nil {
		return Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	storage.loaded = false
	storage.cache = nil
	return nil
}

func (storage *JSONLStorage) ReplaceEntries(entries []Entry) error {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	file, err := os.OpenFile(storage.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	defer file.Close()
	metadata, err := marshalJSONNoHTMLEscape(storage.metadata)
	if err != nil {
		return Error{Code: ErrorCorrupted, Message: err.Error()}
	}
	if _, err := file.Write(append(metadata, '\n')); err != nil {
		return Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	for _, entry := range entries {
		data, err := marshalJSONNoHTMLEscape(entry)
		if err != nil {
			return Error{Code: ErrorCorrupted, Message: err.Error()}
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			return Error{Code: ErrorStorageFailure, Message: err.Error()}
		}
	}
	storage.loaded = false
	storage.cache = nil
	return nil
}

func (storage *JSONLStorage) GetEntry(id string) (*Entry, error) {
	entries, err := storage.loadEntries()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.ID() == id {
			copyEntry := entry
			return &copyEntry, nil
		}
	}
	return nil, nil
}

func (storage *JSONLStorage) GetEntries() ([]Entry, error) {
	entries, err := storage.loadEntries()
	if err != nil {
		return nil, err
	}
	return append([]Entry(nil), entries...), nil
}

func (storage *JSONLStorage) GetPathToRoot(leafID *string) ([]Entry, error) {
	entries, err := storage.loadEntries()
	if err != nil {
		return nil, err
	}
	return getPathToRoot(entries, leafID)
}

func (storage *JSONLStorage) FindEntries(entryType EntryType) ([]Entry, error) {
	entries, err := storage.loadEntries()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, entry := range entries {
		if entry.Type() == entryType {
			out = append(out, entry)
		}
	}
	return out, nil
}

func (storage *JSONLStorage) GetLabel(id string) (*string, error) {
	entries, err := storage.loadEntries()
	if err != nil {
		return nil, err
	}
	var latest *string
	for _, entry := range entries {
		if entry.Type() == EntryTypeLabel && entry.TargetID != nil && *entry.TargetID == id {
			latest = cloneStringPtr(entry.LabelValue)
		}
	}
	return latest, nil
}

func (storage *JSONLStorage) loadEntries() ([]Entry, error) {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	if storage.loaded {
		return append([]Entry(nil), storage.cache...), nil
	}
	raw, err := os.ReadFile(storage.path)
	if err != nil {
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	if !utf8.Valid(raw) {
		return nil, Error{Code: ErrorStorageFailure, Message: "invalid UTF-8"}
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil, Error{Code: ErrorCorrupted, Message: "missing metadata header"}
	}
	var entries []Entry
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, Error{Code: ErrorCorrupted, Message: err.Error()}
		}
		entries = append(entries, entry)
	}
	storage.cache = entries
	storage.loaded = true
	return append([]Entry(nil), entries...), nil
}

func isNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
