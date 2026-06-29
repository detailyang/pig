package builtinskills

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/detailyang/pig/skills"
)

func TestBuiltinSkillsPackageMirrorsUpstreamResolve(t *testing.T) {
	if names := AvailableBuiltinNames(); !reflect.DeepEqual(names, []string{"karpathy-guidelines"}) {
		t.Fatalf("available builtins mismatch: %#v", names)
	}
	resolved, err := ResolveBuiltins([]string{"karpathy-guidelines", "karpathy-guidelines"}, []string{"future-skill", "karpathy-guidelines"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Skills) != 1 || resolved.Skills[0].Name != "karpathy-guidelines" || resolved.Skills[0].Source != skills.SourceBuiltin {
		t.Fatalf("resolved skills mismatch: %#v", resolved.Skills)
	}
	if len(resolved.Diagnostics) != 1 || !strings.Contains(resolved.Diagnostics[0], "future-skill") {
		t.Fatalf("diagnostics mismatch: %#v", resolved.Diagnostics)
	}
}

func TestBuiltinSkillsPackageRejectsUnknownCLIAndParsesConfig(t *testing.T) {
	_, err := ResolveBuiltins([]string{"missing"}, nil)
	var unknown *UnknownBuiltinError
	if !errors.As(err, &unknown) || !reflect.DeepEqual(unknown.Unknown, []string{"missing"}) {
		t.Fatalf("unknown error mismatch: %#v err=%v", unknown, err)
	}
	config := `
[builtin_skills]
enabled = ["karpathy-guidelines", "future-skill"]
`
	if got := ParseBuiltinSkillsConfig(config); !reflect.DeepEqual(got, []string{"karpathy-guidelines", "future-skill"}) {
		t.Fatalf("config parse mismatch: %#v", got)
	}
}

func TestBuiltinSkillsPackageMergeAndFrontmatter(t *testing.T) {
	builtin := skills.Skill{Name: "karpathy-guidelines", Source: skills.SourceBuiltin, FilePath: "builtin"}
	user := skills.Skill{Name: "karpathy-guidelines", Source: skills.SourceUser, FilePath: "user"}
	mine := skills.Skill{Name: "mine", Source: skills.SourceProject, FilePath: "project"}
	merged := MergeWithUserProject([]skills.Skill{builtin}, []skills.Skill{user, mine})
	if len(merged) != 2 || merged[0].FilePath != "user" || merged[1].Name != "mine" {
		t.Fatalf("merge mismatch: %#v", merged)
	}
	if got := StripFrontmatter("---\nname: x\n---\n\nBody\n"); got != "Body\n" {
		t.Fatalf("frontmatter mismatch: %q", got)
	}
}
