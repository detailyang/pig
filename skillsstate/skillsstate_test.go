package skillsstate

import (
	"os"
	"strings"
	"testing"

	"github.com/detailyang/pig/skills"
)

func TestSkillsStatePackageLoadSaveApply(t *testing.T) {
	dir := t.TempDir()
	if STATE_FILE != "skills-state.json" || StatePath(dir) == "" {
		t.Fatalf("state path mismatch: %q", STATE_FILE)
	}
	state := SkillsState{}
	state.Set("foo", SkillSourceUser, false)
	state.Set("bar", SkillSourceProject, true)
	if entry, ok := state.Lookup("foo", SkillSourceUser); !ok || entry.Enabled {
		t.Fatalf("lookup mismatch: %#v ok=%v", entry, ok)
	}
	if !state.Remove("bar", SkillSourceProject) {
		t.Fatal("expected remove")
	}
	if err := Save(dir, state); err != nil {
		t.Fatal(err)
	}
	loaded := Load(dir)
	skillList := []skills.Skill{{Name: "foo", Source: SkillSourceUser}, {Name: "foo", Source: SkillSourceProject}}
	Apply(loaded, skillList)
	if !skillList[0].DisableModelInvocation || skillList[1].DisableModelInvocation {
		t.Fatalf("apply mismatch: %#v", skillList)
	}
}

func TestSkillsStatePackageSetAndRemoveAndSave(t *testing.T) {
	dir := t.TempDir()
	state, err := SetAndSave(dir, "foo", SkillSourceUser, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Overrides) != 1 {
		t.Fatalf("set state mismatch: %#v", state)
	}
	if err := RemoveAndSave(dir, "foo", SkillSourceUser); err != nil {
		t.Fatal(err)
	}
	loaded := LoadState(dir)
	if len(loaded.Overrides) != 0 {
		t.Fatalf("remove state mismatch: %#v", loaded)
	}
}

func TestSkillsStatePackageInvalidUTF8AndNoHTMLEscape(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(StatePath(dir), []byte("{\"overrides\":[{\"name\":\"foo\",\"source\":\"user\",\"enabled\":false}],\"note\":\"\xff\"}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if loaded := LoadState(dir); len(loaded.Overrides) != 0 {
		t.Fatalf("invalid UTF-8 should be ignored: %#v", loaded)
	}
	state := SkillsState{}
	state.Set("<tag>&value", SkillSourceUser, false)
	if err := SaveState(dir, state); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(StatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `\u003c`) || strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) || !strings.Contains(text, `"name": "<tag>&value"`) {
		t.Fatalf("serialized state mismatch: %s", text)
	}
}
