package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/config"
)

type MemoryRepo struct {
	mu       sync.RWMutex
	sessions []*Session
}

func NewMemoryRepo() *MemoryRepo { return &MemoryRepo{} }

func NewMemorySessionRepo() *MemoryRepo { return NewMemoryRepo() }

func ToSession(storage Storage) *Session { return NewSession(storage) }

func (repo *MemoryRepo) Create() *Session {
	sess := NewSession(NewMemoryStorage(Metadata{ID: CreateSessionID(), CreatedAt: CreateTimestamp()}))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.sessions = append(repo.sessions, sess)
	return sess
}

func (repo *MemoryRepo) List() []*Session {
	repo.mu.RLock()
	defer repo.mu.RUnlock()
	return append([]*Session(nil), repo.sessions...)
}

func (repo *MemoryRepo) Count() int {
	repo.mu.RLock()
	defer repo.mu.RUnlock()
	return len(repo.sessions)
}

func (repo *MemoryRepo) DeleteByID(id string) (bool, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	start := len(repo.sessions)
	kept := make([]*Session, 0, start)
	for _, sess := range repo.sessions {
		metadata, err := sess.Storage().MetadataJSON()
		if err != nil {
			return false, err
		}
		if metadata["id"] == id {
			continue
		}
		kept = append(kept, sess)
	}
	repo.sessions = kept
	return len(kept) != start, nil
}

func (repo *MemoryRepo) Fork(source *Session, options ForkOptions) (*Session, error) {
	entries, err := GetEntriesToFork(source.Storage(), options)
	if err != nil {
		return nil, err
	}
	storage := NewMemoryStorage(Metadata{ID: CreateSessionID(), CreatedAt: CreateTimestamp()})
	if err := storage.ReplaceEntries(entries); err != nil {
		return nil, err
	}
	fork := NewSession(storage)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	repo.sessions = append(repo.sessions, fork)
	return fork, nil
}

type JSONLRepo struct {
	root string
}

type SessionEntry struct {
	Path       string
	ID         string
	CreatedAt  string
	Preview    *string
	Automation AutomationCounts
}

func NewJSONLRepo(root string) *JSONLRepo { return &JSONLRepo{root: root} }

func NewJsonlSessionRepo(root string) *JSONLRepo { return NewJSONLRepo(root) }

func OpenRepo(cwd string) *JSONLRepo { return NewJSONLRepo(config.SessionsDirForCWD(cwd)) }

func (repo *JSONLRepo) Root() string { return repo.root }

func Create(repo *JSONLRepo, cwd string) (*Session, error) { return repo.Create(cwd) }

func Resume(repo *JSONLRepo, explicitID *string) (*Session, error) { return repo.Resume(explicitID) }

func (repo *JSONLRepo) Create(cwd string) (*Session, error) {
	if err := os.MkdirAll(repo.root, 0o755); err != nil {
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	id := CreateSessionID()
	path := filepath.Join(repo.root, id+".jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: id, CreatedAt: CreateTimestamp()}, CWD: cwd, Path: path})
	if err != nil {
		return nil, err
	}
	return NewSession(storage), nil
}

func (repo *JSONLRepo) Fork(source *Session, options ForkOptions, cwd string) (*Session, error) {
	entries, err := GetEntriesToFork(source.Storage(), options)
	if err != nil {
		return nil, err
	}
	metadata, err := source.Storage().MetadataJSON()
	if err != nil {
		return nil, err
	}
	parentPath, _ := metadata["path"].(string)
	if err := os.MkdirAll(repo.root, 0o755); err != nil {
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	id := CreateSessionID()
	path := filepath.Join(repo.root, id+".jsonl")
	storage, err := CreateJSONLStorage(path, JSONLMetadata{Metadata: Metadata{ID: id, CreatedAt: CreateTimestamp()}, CWD: cwd, Path: path, ParentSessionPath: parentPath})
	if err != nil {
		return nil, err
	}
	if err := storage.ReplaceEntries(entries); err != nil {
		return nil, err
	}
	return NewSession(storage), nil
}

func (repo *JSONLRepo) ImportJSONL(sourcePath, cwd string, origin SessionImportOrigin) (*Session, error) {
	source, err := OpenJSONLStorage(sourcePath)
	if err != nil {
		return nil, err
	}
	entries, err := source.GetEntries()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(repo.root, 0o755); err != nil {
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	id := CreateSessionID()
	path := filepath.Join(repo.root, id+".jsonl")
	metadata := JSONLMetadata{Metadata: Metadata{ID: id, CreatedAt: CreateTimestamp()}, CWD: cwd, Path: path, ImportedFrom: &origin}
	if err := commitImportedJSONL(repo, path, path+".tmp", metadata, entries); err != nil {
		return nil, err
	}
	return repo.Open(path)
}

func (repo *JSONLRepo) Open(path string) (*Session, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo.root, path)
	}
	storage, err := OpenJSONLStorage(path)
	if err != nil {
		return nil, err
	}
	return NewSession(storage), nil
}

func (repo *JSONLRepo) List() ([]string, error) {
	entries, err := os.ReadDir(repo.root)
	if err != nil {
		if isNotExist(err) {
			return []string{}, nil
		}
		return nil, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	var out []string
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".jsonl" {
			out = append(out, filepath.Join(repo.root, entry.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

func (repo *JSONLRepo) ListEntries() ([]SessionEntry, error) {
	paths, err := repo.List()
	if err != nil {
		return nil, err
	}
	out := make([]SessionEntry, 0, len(paths))
	for _, path := range paths {
		sess, err := repo.Open(path)
		if err != nil {
			return nil, err
		}
		metadata, err := sess.Storage().MetadataJSON()
		if err != nil {
			return nil, err
		}
		id, _ := metadata["id"].(string)
		if id == "" {
			id = "?"
		}
		createdAt, _ := metadata["createdAt"].(string)
		if createdAt == "" {
			createdAt = "?"
		}
		preview, err := firstUserText(sess)
		if err != nil {
			return nil, err
		}
		out = append(out, SessionEntry{Path: path, ID: id, CreatedAt: createdAt, Preview: preview, Automation: AutomationCountsForSession(path)})
	}
	return out, nil
}

func ListEntries(repo *JSONLRepo) ([]SessionEntry, error) { return repo.ListEntries() }

func firstUserText(sess *Session) (*string, error) {
	entries, err := sess.Entries()
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Type() != EntryTypeMessage || entry.Message == nil || entry.Message.Kind != agent.MessageKindLLM || entry.Message.LLM == nil || entry.Message.LLM.Role != ai.RoleUser {
			continue
		}
		var parts []string
		for _, block := range entry.Message.LLM.Content {
			if block.Type == ai.ContentText {
				parts = append(parts, block.Text)
			}
		}
		if len(parts) == 0 {
			continue
		}
		preview := truncateSessionEntryPreview(strings.ReplaceAll(strings.Join(parts, " "), "\n", " "))
		return &preview, nil
	}
	return nil, nil
}

func truncateSessionEntryPreview(text string) string {
	runes := []rune(text)
	if len(runes) <= 80 {
		return text
	}
	return string(runes[:80]) + "…"
}

func (repo *JSONLRepo) Delete(path string) (bool, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo.root, path)
	}
	if err := os.Remove(path); err != nil {
		if isNotExist(err) {
			return false, nil
		}
		return false, Error{Code: ErrorStorageFailure, Message: err.Error()}
	}
	for _, sidecar := range []string{TriggerSidecarPath(path), CronSidecarPath(path), EndpointSidecarPath(path)} {
		if err := os.Remove(sidecar); err != nil && !isNotExist(err) {
			return false, Error{Code: ErrorStorageFailure, Message: "delete " + sidecar + ": " + err.Error()}
		}
	}
	return true, nil
}

func (repo *JSONLRepo) DeleteByID(id string) (*string, error) {
	path, err := repo.FindPathByID(id)
	if err != nil || path == nil {
		return nil, err
	}
	deleted, err := repo.Delete(*path)
	if err != nil {
		return nil, err
	}
	if !deleted {
		return nil, nil
	}
	return path, nil
}

func DeleteByID(repo *JSONLRepo, id string) (*string, error) { return repo.DeleteByID(id) }

func DeleteById(repo *JSONLRepo, id string) (*string, error) { return DeleteByID(repo, id) }

func (repo *JSONLRepo) FindPathByID(id string) (*string, error) {
	paths, err := repo.List()
	if err != nil {
		return nil, err
	}
	return repo.findSessionPath(paths, id)
}

func FindPathByID(repo *JSONLRepo, id string) (*string, error) { return repo.FindPathByID(id) }

func FindPathById(repo *JSONLRepo, id string) (*string, error) { return FindPathByID(repo, id) }

func (repo *JSONLRepo) NewestPath() (*string, error) {
	paths, err := repo.List()
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	newest := paths[len(paths)-1]
	return &newest, nil
}

func NewestPath(repo *JSONLRepo) (*string, error) { return repo.NewestPath() }

func (repo *JSONLRepo) Resume(explicitID *string) (*Session, error) {
	var path *string
	var err error
	if explicitID != nil {
		path, err = repo.FindPathByID(*explicitID)
		if err != nil {
			return nil, err
		}
		if path == nil {
			return nil, fmt.Errorf("no session matches id %s", *explicitID)
		}
	} else {
		path, err = repo.NewestPath()
		if err != nil {
			return nil, err
		}
		if path == nil {
			return nil, fmt.Errorf("no sessions to resume in %s", repo.root)
		}
	}
	return repo.Open(*path)
}

func (repo *JSONLRepo) findSessionPath(paths []string, id string) (*string, error) {
	for _, path := range paths {
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if stem == id || strings.HasPrefix(stem, id) {
			matched := path
			return &matched, nil
		}
		session, err := repo.Open(path)
		if err != nil {
			return nil, err
		}
		metadata, err := session.Storage().MetadataJSON()
		if err != nil {
			return nil, err
		}
		metadataID, _ := metadata["id"].(string)
		if metadataID == id || strings.HasPrefix(metadataID, id) {
			matched := path
			return &matched, nil
		}
	}
	return nil, nil
}

type ForkPosition string

const (
	ForkBefore ForkPosition = "before"
	ForkAt     ForkPosition = "at"

	ForkPositionBefore = ForkBefore
	ForkPositionAt     = ForkAt
)

type ForkOptions struct {
	EntryID  *string
	Position ForkPosition
}

func GetEntriesToFork(storage Storage, options ForkOptions) ([]Entry, error) {
	if options.EntryID == nil {
		return storage.GetEntries()
	}
	target, err := storage.GetEntry(*options.EntryID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, Error{Code: ErrorNotFound, Message: "Entry " + *options.EntryID + " not found"}
	}
	position := options.Position
	if position == "" {
		position = ForkBefore
	}
	var effectiveLeaf *string
	if position == ForkAt {
		id := target.ID()
		effectiveLeaf = &id
	} else {
		if target.Type() != EntryTypeMessage || target.Message == nil || target.Message.Kind != agent.MessageKindLLM || target.Message.LLM == nil || target.Message.LLM.Role != "user" {
			return nil, Error{Code: ErrorNotFound, Message: "Entry " + *options.EntryID + " is not a user message"}
		}
		effectiveLeaf = target.ParentID
	}
	return storage.GetPathToRoot(effectiveLeaf)
}
