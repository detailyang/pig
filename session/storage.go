package session

type Storage interface {
	MetadataJSON() (map[string]any, error)
	GetLeafID() (*string, error)
	SetLeafID(id *string) error
	CreateEntryID() (string, error)
	AppendEntry(entry Entry) error
	GetEntry(id string) (*Entry, error)
	GetEntries() ([]Entry, error)
	GetPathToRoot(leafID *string) ([]Entry, error)
	FindEntries(entryType EntryType) ([]Entry, error)
	GetLabel(id string) (*string, error)
}

type Rewriter interface {
	ReplaceEntries(entries []Entry) error
}

func getPathToRoot(entries []Entry, leafID *string) ([]Entry, error) {
	if leafID == nil {
		return []Entry{}, nil
	}
	byID := map[string]Entry{}
	for _, entry := range entries {
		byID[entry.ID()] = entry
	}
	var chain []Entry
	seen := map[string]bool{}
	current := *leafID
	for current != "" {
		if seen[current] {
			return nil, Error{Code: ErrorCorrupted, Message: "cycle in parent chain at " + current}
		}
		seen[current] = true
		entry, ok := byID[current]
		if !ok {
			return nil, Error{Code: ErrorCorrupted, Message: "parent " + current + " not found"}
		}
		chain = append(chain, entry)
		if entry.ParentID == nil {
			break
		}
		current = *entry.ParentID
	}
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}
	return chain, nil
}

func latestLeaf(entries []Entry) *string {
	var leaf *string
	for _, entry := range entries {
		if entry.EntryType == EntryTypeLeaf {
			leaf = entry.TargetID
			continue
		}
		id := entry.ID()
		leaf = &id
	}
	return leaf
}
