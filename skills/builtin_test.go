package skills

import (
	"reflect"
	"strings"
	"testing"
)

func TestAvailableBuiltinNamesSortedAndContainsKarpathy(t *testing.T) {
	names := AvailableBuiltinNames()
	if !reflect.DeepEqual(names, []string{"karpathy-guidelines"}) {
		t.Fatalf("names=%#v", names)
	}
}

func TestResolveBuiltinsKnownUnknownAndDedup(t *testing.T) {
	empty, err := ResolveBuiltins(nil, nil)
	if err != nil || len(empty.Skills) != 0 || len(empty.Diagnostics) != 0 {
		t.Fatalf("empty mismatch resolved=%#v err=%v", empty, err)
	}
	resolved, err := ResolveBuiltins([]string{"karpathy-guidelines", "karpathy-guidelines"}, []string{"karpathy-guidelines"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Skills) != 1 || resolved.Skills[0].Name != "karpathy-guidelines" || resolved.Skills[0].Source != SourceBuiltin || resolved.Skills[0].FilePath != "<builtin>/karpathy-guidelines/SKILL.md" {
		t.Fatalf("resolved skill mismatch: %#v", resolved.Skills)
	}
	if strings.HasPrefix(resolved.Skills[0].Content, "---") || !strings.Contains(resolved.Skills[0].Content, "Think Before Coding") {
		t.Fatalf("frontmatter was not stripped: %.80q", resolved.Skills[0].Content)
	}
	_, err = ResolveBuiltins([]string{"missing-a", "missing-b", "missing-a"}, nil)
	if err == nil {
		t.Fatal("expected unknown CLI error")
	}
	unknown := err.(*UnknownBuiltinError)
	if !reflect.DeepEqual(unknown.Unknown, []string{"missing-a", "missing-b"}) || !reflect.DeepEqual(unknown.Available, []string{"karpathy-guidelines"}) || !strings.Contains(err.Error(), "Available: karpathy-guidelines") || unknown.String() != unknown.Error() {
		t.Fatalf("unknown mismatch: %#v message=%v", unknown, err)
	}
}

func TestBuiltinKarpathyGuidelinesVendoredVerbatim(t *testing.T) {
	resolved, err := ResolveBuiltins([]string{"karpathy-guidelines"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Skills) != 1 {
		t.Fatalf("expected one builtin skill, got %#v", resolved.Skills)
	}
	content := resolved.Skills[0].Content
	for _, want := range []string{
		"derived from [Andrej Karpathy's observations](https://x.com/karpathy/status/2015883857489522876)",
		"**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.",
		"- No \"flexibility\" or \"configurability\" that wasn't requested.",
		"Strong success criteria let you loop independently. Weak criteria (\"make it work\") require constant clarification.",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("builtin karpathy-guidelines missing upstream text %q in:\n%s", want, content)
		}
	}
}

func TestResolveBuiltinsConfigUnknownIsDiagnostic(t *testing.T) {
	resolved, err := ResolveBuiltins(nil, []string{"karpathy-guidelines", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Skills) != 1 || resolved.Skills[0].Name != "karpathy-guidelines" {
		t.Fatalf("known config skill missing: %#v", resolved.Skills)
	}
	if len(resolved.Diagnostics) != 1 || !strings.Contains(resolved.Diagnostics[0], "missing") || !strings.Contains(resolved.Diagnostics[0], "Available: karpathy-guidelines") {
		t.Fatalf("diagnostic mismatch: %#v", resolved.Diagnostics)
	}
}

func TestMergeBuiltinsWithUserProjectShadowing(t *testing.T) {
	builtin := Skill{Name: "karpathy-guidelines", Source: SourceBuiltin, FilePath: "<builtin>/karpathy-guidelines/SKILL.md"}
	user := Skill{Name: "karpathy-guidelines", Source: SourceUser, FilePath: "/home/me/.pie/skills/karpathy-guidelines/SKILL.md"}
	other := Skill{Name: "mine", Source: SourceProject, FilePath: "/repo/.pie/skills/mine/SKILL.md"}
	merged := MergeBuiltinsWithUserProject([]Skill{builtin}, []Skill{user, other})
	if len(merged) != 2 || merged[0].FilePath != user.FilePath || merged[1].Name != "mine" {
		t.Fatalf("merge mismatch: %#v", merged)
	}
	upstreamNamed := MergeWithUserProject([]Skill{builtin}, []Skill{user, other})
	if !reflect.DeepEqual(upstreamNamed, merged) {
		t.Fatalf("upstream merge wrapper mismatch: %#v", upstreamNamed)
	}
}

func TestParseBuiltinSkillsConfig(t *testing.T) {
	text := `
[builtin_skills]
enabled = ["karpathy-guidelines", "future-other-skill"]
`
	if got := ParseBuiltinSkillsConfig(text); !reflect.DeepEqual(got, []string{"karpathy-guidelines", "future-other-skill"}) {
		t.Fatalf("enabled=%#v", got)
	}
	multiline := `
[builtin_skills]
enabled = [
  "karpathy-guidelines",
  "future-other-skill",
]
`
	if got := ParseBuiltinSkillsConfig(multiline); !reflect.DeepEqual(got, []string{"karpathy-guidelines", "future-other-skill"}) {
		t.Fatalf("multiline enabled=%#v", got)
	}
	for _, bad := range []string{"", "[builtin_skills]\n", "this is not valid toml [ [ [", "[other]\nkey = \"value\""} {
		if got := ParseBuiltinSkillsConfig(bad); len(got) != 0 {
			t.Fatalf("expected empty for %q, got %#v", bad, got)
		}
	}
	duplicate := `
[builtin_skills]
enabled = ["karpathy-guidelines"]
enabled = ["future-other-skill"]
`
	if got := ParseBuiltinSkillsConfig(duplicate); len(got) != 0 {
		t.Fatalf("expected duplicate keys to degrade to empty, got %#v", got)
	}
	duplicateTable := `
[builtin_skills]
enabled = ["karpathy-guidelines"]
[builtin_skills]
enabled = ["future-other-skill"]
`
	if got := ParseBuiltinSkillsConfig(duplicateTable); len(got) != 0 {
		t.Fatalf("expected duplicate tables to degrade to empty, got %#v", got)
	}
	nonString := `
[builtin_skills]
enabled = [1]
`
	if got := ParseBuiltinSkillsConfig(nonString); len(got) != 0 {
		t.Fatalf("expected non-string enabled array to degrade to empty like upstream serde, got %#v", got)
	}
}

func TestStripFrontmatterMatchesBuiltinBehavior(t *testing.T) {
	content := "---\nname: review\ndescription: Review code\n---\n\nBody\n"
	if got := StripFrontmatter(content); got != "Body\n" {
		t.Fatalf("stripped content=%q", got)
	}
	withoutFrontmatter := "Body without metadata"
	if got := StripFrontmatter(withoutFrontmatter); got != withoutFrontmatter {
		t.Fatalf("content without frontmatter should be unchanged: %q", got)
	}
}
