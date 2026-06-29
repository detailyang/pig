package session

import "sync"

type MemoryStorage struct {
	mu       sync.RWMutex
	metadata Metadata
	entries  []Entry
	leafID   *string
}

func NewMemoryStorage(metadata Metadata) *MemoryStorage {
	if metadata.ID == "" {
		metadata.ID = CreateSessionID()
	}
	if metadata.CreatedAt == "" {
		metadata.CreatedAt = CreateTimestamp()
	}
	return &MemoryStorage{metadata: metadata}
}

func NewMemorySessionStorage() *MemoryStorage { return NewMemoryStorage(Metadata{}) }

func NewMemorySessionStorageWithMetadata(metadata Metadata) *MemoryStorage {
	return NewMemoryStorage(metadata)
}

func (storage *MemoryStorage) WithMetadata(metadata Metadata) *MemoryStorage {
	return NewMemoryStorage(metadata)
}

func (storage *MemoryStorage) MetadataJSON() (map[string]any, error) {
	storage.mu.RLock()
	defer storage.mu.RUnlock()
	return map[string]any{"id": storage.metadata.ID, "createdAt": storage.metadata.CreatedAt}, nil
}

func (storage *MemoryStorage) GetLeafID() (*string, error) {
	storage.mu.RLock()
	defer storage.mu.RUnlock()
	return cloneStringPtr(storage.leafID), nil
}

func (storage *MemoryStorage) SetLeafID(id *string) error {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	storage.leafID = cloneStringPtr(id)
	return nil
}

func (storage *MemoryStorage) CreateEntryID() (string, error) { return CreateSessionID(), nil }

func (storage *MemoryStorage) AppendEntry(entry Entry) error {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	storage.entries = append(storage.entries, entry)
	id := entry.ID()
	storage.leafID = &id
	return nil
}

func (storage *MemoryStorage) ReplaceEntries(entries []Entry) error {
	storage.mu.Lock()
	defer storage.mu.Unlock()
	storage.entries = append([]Entry(nil), entries...)
	storage.leafID = latestLeaf(storage.entries)
	return nil
}

func (storage *MemoryStorage) GetEntry(id string) (*Entry, error) {
	storage.mu.RLock()
	defer storage.mu.RUnlock()
	for _, entry := range storage.entries {
		if entry.ID() == id {
			copyEntry := entry
			return &copyEntry, nil
		}
	}
	return nil, nil
}

func (storage *MemoryStorage) GetEntries() ([]Entry, error) {
	storage.mu.RLock()
	defer storage.mu.RUnlock()
	return append([]Entry(nil), storage.entries...), nil
}

func (storage *MemoryStorage) GetPathToRoot(leafID *string) ([]Entry, error) {
	storage.mu.RLock()
	defer storage.mu.RUnlock()
	return getPathToRoot(storage.entries, leafID)
}

func (storage *MemoryStorage) FindEntries(entryType EntryType) ([]Entry, error) {
	storage.mu.RLock()
	defer storage.mu.RUnlock()
	var out []Entry
	for _, entry := range storage.entries {
		if entry.Type() == entryType {
			out = append(out, entry)
		}
	}
	return out, nil
}

func (storage *MemoryStorage) GetLabel(id string) (*string, error) {
	storage.mu.RLock()
	defer storage.mu.RUnlock()
	var latest *string
	for _, entry := range storage.entries {
		if entry.Type() == EntryTypeLabel && entry.TargetID != nil && *entry.TargetID == id {
			latest = cloneStringPtr(entry.LabelValue)
		}
	}
	return latest, nil
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
