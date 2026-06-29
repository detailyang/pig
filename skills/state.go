package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

const stateFile = "skills-state.json"

const STATE_FILE = stateFile

type StateEntry struct {
	Name    string `json:"name"`
	Source  Source `json:"source"`
	Enabled bool   `json:"enabled"`
}

type SkillStateEntry = StateEntry

type SkillsState struct {
	Overrides []StateEntry `json:"overrides"`
}

func (state SkillsState) Lookup(name string, source Source) (StateEntry, bool) {
	for _, entry := range state.Overrides {
		if entry.Name == name && entry.Source == source {
			return entry, true
		}
	}
	return StateEntry{}, false
}

func (state *SkillsState) Set(name string, source Source, enabled bool) {
	for index := range state.Overrides {
		if state.Overrides[index].Name == name && state.Overrides[index].Source == source {
			state.Overrides[index].Enabled = enabled
			return
		}
	}
	state.Overrides = append(state.Overrides, StateEntry{Name: name, Source: source, Enabled: enabled})
}

func (state *SkillsState) Remove(name string, source Source) bool {
	before := len(state.Overrides)
	state.Overrides = filterStateEntries(state.Overrides, func(entry StateEntry) bool {
		return !(entry.Name == name && entry.Source == source)
	})
	return len(state.Overrides) != before
}

func (state SkillsState) Apply(skills []Skill) {
	for index := range skills {
		if entry, ok := state.Lookup(skills[index].Name, skills[index].Source); ok {
			skills[index].DisableModelInvocation = !entry.Enabled
		}
	}
}

func Apply(state SkillsState, skills []Skill) { state.Apply(skills) }

func StatePath(baseDir string) string {
	return filepath.Join(baseDir, stateFile)
}

func LoadState(baseDir string) SkillsState {
	data, err := os.ReadFile(StatePath(baseDir))
	if err != nil {
		return SkillsState{}
	}
	if !utf8.Valid(data) {
		return SkillsState{}
	}
	var state SkillsState
	if err := json.Unmarshal(data, &state); err != nil {
		return SkillsState{}
	}
	return state
}

func Load(baseDir string) SkillsState {
	return LoadState(baseDir)
}

func SaveState(baseDir string, state SkillsState) error {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return err
	}
	data, err := marshalJSONIndentNoHTMLEscape(state)
	if err != nil {
		return err
	}
	tmp := filepath.Join(baseDir, fmt.Sprintf(".%s.%d.%d.tmp", stateFile, os.Getpid(), time.Now().UnixNano()))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, StatePath(baseDir)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func Save(baseDir string, state SkillsState) error {
	return SaveState(baseDir, state)
}

func SetAndSave(baseDir, name string, source Source, enabled bool) (SkillsState, error) {
	state := LoadState(baseDir)
	state.Set(name, source, enabled)
	return state, SaveState(baseDir, state)
}

func RemoveAndSave(baseDir, name string, source Source) error {
	state := LoadState(baseDir)
	if state.Remove(name, source) {
		return SaveState(baseDir, state)
	}
	return nil
}

func filterStateEntries(entries []StateEntry, keep func(StateEntry) bool) []StateEntry {
	out := entries[:0]
	for _, entry := range entries {
		if keep(entry) {
			out = append(out, entry)
		}
	}
	return out
}
