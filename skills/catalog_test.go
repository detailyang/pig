package skills

import (
	"path/filepath"
	"testing"
)

func TestCatalogReloadAppliesStateAndAudits(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	mustWrite(t, filepath.Join(root, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: Alpha skill\n---\nAlpha body\n")
	state := SkillsState{}
	state.Set("alpha", SourceUser, false)
	if err := SaveState(stateDir, state); err != nil {
		t.Fatal(err)
	}

	catalog := NewCatalog(CatalogOptions{Dirs: []string{root}, StateDir: stateDir})
	out, err := catalog.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "alpha" || !out.Skills[0].DisableModelInvocation {
		t.Fatalf("reload mismatch: %#v", out)
	}
	if skills := catalog.Skills(); len(skills) != 1 || skills[0].Name != "alpha" || !skills[0].DisableModelInvocation {
		t.Fatalf("snapshot mismatch: %#v", skills)
	}
	audit := catalog.AuditLog()
	if len(audit) != 1 || audit[0].Operation != CatalogAuditReload || audit[0].SkillCount != 1 {
		t.Fatalf("audit mismatch: %#v", audit)
	}
}

func TestCatalogSetEnabledPersistsAndReloads(t *testing.T) {
	root := t.TempDir()
	stateDir := t.TempDir()
	mustWrite(t, filepath.Join(root, "alpha", "SKILL.md"), "---\nname: alpha\ndescription: Alpha skill\n---\nAlpha body\n")
	catalog := NewCatalog(CatalogOptions{Dirs: []string{root}, StateDir: stateDir})
	if _, err := catalog.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SetEnabled("alpha", SourceUser, false); err != nil {
		t.Fatal(err)
	}
	loaded := LoadState(stateDir)
	if entry, ok := loaded.Lookup("alpha", SourceUser); !ok || entry.Enabled {
		t.Fatalf("state mismatch: %#v ok=%v", entry, ok)
	}
	skills := catalog.Skills()
	if len(skills) != 1 || !skills[0].DisableModelInvocation {
		t.Fatalf("catalog should reload disabled state: %#v", skills)
	}
	audit := catalog.AuditLog()
	if len(audit) < 3 || audit[len(audit)-2].Operation != CatalogAuditSetEnabled || audit[len(audit)-1].Operation != CatalogAuditReload {
		t.Fatalf("audit sequence mismatch: %#v", audit)
	}
}
