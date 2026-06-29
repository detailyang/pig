package skills

import (
	"sync"
	"time"
)

type CatalogOptions struct {
	Dirs     []string
	StateDir string
	Clock    func() time.Time
}

type Catalog struct {
	dirs     []string
	stateDir string
	clock    func() time.Time
	mu       sync.RWMutex
	skills   []Skill
	audit    []CatalogAuditEntry
}

type CatalogAuditOperation string

const (
	CatalogAuditReload     CatalogAuditOperation = "reload"
	CatalogAuditSetEnabled CatalogAuditOperation = "set_enabled"
)

type CatalogAuditEntry struct {
	Operation  CatalogAuditOperation `json:"operation"`
	Name       string                `json:"name,omitempty"`
	Source     Source                `json:"source,omitempty"`
	Enabled    *bool                 `json:"enabled,omitempty"`
	SkillCount int                   `json:"skillCount,omitempty"`
	At         time.Time             `json:"at"`
}

func NewCatalog(options CatalogOptions) *Catalog {
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Catalog{dirs: append([]string(nil), options.Dirs...), stateDir: options.StateDir, clock: clock}
}

func (catalog *Catalog) Reload() (LoadOutput, error) {
	out := LoadSkills(catalog.dirs)
	state := LoadState(catalog.stateDir)
	state.Apply(out.Skills)
	catalog.mu.Lock()
	catalog.skills = append([]Skill(nil), out.Skills...)
	catalog.audit = append(catalog.audit, CatalogAuditEntry{Operation: CatalogAuditReload, SkillCount: len(out.Skills), At: catalog.clock()})
	catalog.mu.Unlock()
	return out, nil
}

func (catalog *Catalog) Skills() []Skill {
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	return append([]Skill(nil), catalog.skills...)
}

func (catalog *Catalog) AuditLog() []CatalogAuditEntry {
	catalog.mu.RLock()
	defer catalog.mu.RUnlock()
	return append([]CatalogAuditEntry(nil), catalog.audit...)
}

func (catalog *Catalog) SetEnabled(name string, source Source, enabled bool) error {
	if _, err := SetAndSave(catalog.stateDir, name, source, enabled); err != nil {
		return err
	}
	catalog.mu.Lock()
	enabledCopy := enabled
	catalog.audit = append(catalog.audit, CatalogAuditEntry{Operation: CatalogAuditSetEnabled, Name: name, Source: source, Enabled: &enabledCopy, At: catalog.clock()})
	catalog.mu.Unlock()
	_, err := catalog.Reload()
	return err
}
