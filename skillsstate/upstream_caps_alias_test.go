package skillsstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpstreamCapsAlias(t *testing.T) {
	if STATEFILE != STATE_FILE || STATEFILE == "" {
		t.Fatalf("state file alias mismatch")
	}
}

func TestSkillsLoaderAliases(t *testing.T) {
	cwd := t.TempDir()
	userDir, projectDir := SkillsDirs(cwd)
	if userDir == "" || projectDir != filepath.Join(cwd, ".pie", "skills") {
		t.Fatalf("skills dirs alias mismatch user=%q project=%q", userDir, projectDir)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "SKILL.md"), []byte("---\nname: demo\ndescription: Demo\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := LoadAll(cwd)
	if len(loaded.Skills) != 1 || loaded.Skills[0].Name != "demo" || loaded.Skills[0].Source != SourceProject {
		t.Fatalf("load all alias mismatch: %#v", loaded)
	}
}
