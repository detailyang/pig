package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/skills"
)

func TestFormatSkillCompatSurface(t *testing.T) {
	formatted := FormatSkillInvocation(skills.Skill{Name: "review", FilePath: "/tmp/review/SKILL.md", Content: "Body"}, "extra")
	for _, want := range []string{"<skill name=\"review\"", "References are relative to /tmp/review.", "Body", "extra"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted skill missing %q: %s", want, formatted)
		}
	}
	if FormatSkill(skills.Skill{Name: "review", FilePath: "/tmp/review/SKILL.md", Content: "Body"}, "extra") != formatted {
		t.Fatalf("FormatSkill should remain an alias for FormatSkillInvocation")
	}
	if prompt := FormatSkillsForSystemPrompt([]Skill{{Name: "review", Description: "Review code", Source: SkillSourceProject}}); !strings.Contains(prompt, "review") || !strings.Contains(prompt, "Review code") {
		t.Fatalf("system prompt format mismatch: %s", prompt)
	}
}

func TestLoadSkillsCompatSurface(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: review\ndescription: Review code\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := LoadSkills([]string{dir})
	if len(loaded.Skills) != 1 || loaded.Skills[0].Name != "review" || len(loaded.Diagnostics) != 0 {
		t.Fatalf("LoadSkills mismatch: %#v", loaded)
	}
	sourced := LoadSourcedSkills([]skills.SourcedInput{{Dir: dir, Source: skills.SourceProject}})
	if len(sourced) != 1 || sourced[0].Skill.Name != "review" || sourced[0].Source != skills.SourceProject {
		t.Fatalf("LoadSourcedSkills mismatch: %#v", sourced)
	}
}
