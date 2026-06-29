package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/skills"
)

func TestSkillsCommandListsLoadedSkills(t *testing.T) {
	registry := DefaultRegistry()
	empty := Dispatch(context.Background(), "/skills", registry, Context{})
	if empty.Kind != OutcomeHandled || !strings.Contains(empty.Message, "(no skills loaded") || !strings.Contains(empty.Message, "~/.pie/skills/<name>") {
		t.Fatalf("empty mismatch: %#v", empty)
	}
	ctx := Context{Skills: []skills.Skill{
		{Name: "project-plan", Description: "Plan work", FilePath: "/repo/.pie/skills/project-plan/SKILL.md", Source: skills.SourceProject},
		{Name: "user-review", Description: "Review code", FilePath: "/home/me/.pie/skills/user-review/SKILL.md", Source: skills.SourceUser, DisableModelInvocation: true},
	}}
	listed := Dispatch(context.Background(), "/skills list", registry, ctx)
	if listed.Kind != OutcomeHandled || !strings.Contains(listed.Message, "Loaded skills (2):") || !strings.Contains(listed.Message, "  - project-plan  (project)") || !strings.Contains(listed.Message, "      Plan work") || !strings.Contains(listed.Message, "      path: /repo/.pie/skills/project-plan/SKILL.md") || !strings.Contains(listed.Message, "  - user-review  (user)  [disabled: disable_model_invocation=true]") {
		t.Fatalf("list mismatch: %#v", listed)
	}
	alias := Dispatch(context.Background(), "/skills ls", registry, ctx)
	if alias.Kind != OutcomeHandled || alias.Message != listed.Message {
		t.Fatalf("ls mismatch: %#v", alias)
	}
}

func TestSkillsCommandShowsSkillAndHandlesAmbiguity(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Skills: []skills.Skill{
		{Name: "review", Description: "User review", FilePath: "/user/review/SKILL.md", Content: "body", Source: skills.SourceUser},
		{Name: "review", Description: "Project review", FilePath: "/project/review/SKILL.md", Content: "body", Source: skills.SourceProject, DisableModelInvocation: true},
		{Name: "memory", Description: "No path", Content: "body", Source: skills.SourceBuiltin},
	}}
	shown := Dispatch(context.Background(), "/skills show review project", registry, ctx)
	if shown.Kind != OutcomeHandled || !strings.Contains(shown.Message, "Skill: review (project)") || !strings.Contains(shown.Message, "Status: disabled") || !strings.Contains(shown.Message, "Description: Project review") || !strings.Contains(shown.Message, "Path: /project/review/SKILL.md") || !strings.Contains(shown.Message, "Body: not shown; use the file path if you need to inspect the full skill.") {
		t.Fatalf("show mismatch: %#v", shown)
	}
	ambiguous := Dispatch(context.Background(), "/skills show review", registry, ctx)
	if ambiguous.Kind != OutcomeError || ambiguous.Message != "multiple active skills named 'review'; pass source: builtin, user, or project" {
		t.Fatalf("ambiguous mismatch: %#v", ambiguous)
	}
	missing := Dispatch(context.Background(), "/skills show missing", registry, ctx)
	if missing.Kind != OutcomeError || missing.Message != "no active skill named 'missing'. Run /skills to list loaded skills." {
		t.Fatalf("missing mismatch: %#v", missing)
	}
	missingSource := Dispatch(context.Background(), "/skills show missing user", registry, ctx)
	if missingSource.Kind != OutcomeError || missingSource.Message != "no active user skill named 'missing'. Run /skills to list loaded skills." {
		t.Fatalf("missing source mismatch: %#v", missingSource)
	}
	badSource := Dispatch(context.Background(), "/skills show review workspace", registry, ctx)
	if badSource.Kind != OutcomeError || badSource.Message != "invalid skill source; expected one of: builtin, user, project" {
		t.Fatalf("source mismatch: %#v", badSource)
	}
	noPath := Dispatch(context.Background(), "/skills show memory builtin", registry, ctx)
	if noPath.Kind != OutcomeHandled || !strings.Contains(noPath.Message, "Path: ") {
		t.Fatalf("missing path line mismatch: %#v", noPath)
	}
}

func TestSkillsCommandReturnsReloadAndStateOutcomes(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Skills: []skills.Skill{{Name: "review", Source: skills.SourceUser, DisableModelInvocation: true}}}
	reload := Dispatch(context.Background(), "/skills reload extra", registry, ctx)
	if reload.Kind != OutcomeReloadSkills || reload.Message != "reload skills" {
		t.Fatalf("reload mismatch: %#v", reload)
	}
	enable := Dispatch(context.Background(), "/skills enable review user extra", registry, ctx)
	if enable.Kind != OutcomeSetSkillState || enable.Name != "review" || enable.Source != skills.SourceUser || !enable.Enabled || !strings.Contains(enable.Message, "set skill review (user) enabled=true") {
		t.Fatalf("enable mismatch: %#v", enable)
	}
	disable := Dispatch(context.Background(), "/skills disable review project extra", registry, Context{Skills: []skills.Skill{{Name: "review", Source: skills.SourceProject}}})
	if disable.Kind != OutcomeSetSkillState || disable.Source != skills.SourceProject || disable.Enabled {
		t.Fatalf("disable mismatch: %#v", disable)
	}
	alreadyEnabled := Dispatch(context.Background(), "/skills enable review user", registry, Context{Skills: []skills.Skill{{Name: "review", Source: skills.SourceUser}}})
	if alreadyEnabled.Kind != OutcomeHandled || alreadyEnabled.Message != "skill already enabled: review (user)" {
		t.Fatalf("already enabled mismatch: %#v", alreadyEnabled)
	}
	alreadyDisabled := Dispatch(context.Background(), "/skills disable review user", registry, ctx)
	if alreadyDisabled.Kind != OutcomeHandled || alreadyDisabled.Message != "skill already disabled: review (user)" {
		t.Fatalf("already disabled mismatch: %#v", alreadyDisabled)
	}
}

func TestSkillsCommandInstallAndRemoveOutcomes(t *testing.T) {
	registry := DefaultRegistry()
	installed := Dispatch(context.Background(), "/skills install --confirm --overwrite ./my-skill", registry, Context{CWD: "/repo"})
	if installed.Kind != OutcomeInstallSkill || installed.Path != "/repo/my-skill" || !installed.Confirm || !installed.Overwrite {
		t.Fatalf("install mismatch: %#v", installed)
	}
	url := Dispatch(context.Background(), "/skills install https://example.com/skill.tar.gz", registry, Context{CWD: "/repo"})
	if url.Kind != OutcomeInstallSkill || url.Path != "https://example.com/skill.tar.gz" || url.Confirm || url.Overwrite {
		t.Fatalf("url install mismatch: %#v", url)
	}
	removed := Dispatch(context.Background(), "/skills remove --confirm review user", registry, Context{})
	if removed.Kind != OutcomeRemoveSkill || removed.Name != "review" || removed.Source != skills.SourceUser || !removed.Confirm {
		t.Fatalf("remove mismatch: %#v", removed)
	}
	withoutSource := Dispatch(context.Background(), "/skills remove review", registry, Context{})
	if withoutSource.Kind != OutcomeRemoveSkill || withoutSource.Name != "review" || withoutSource.Source != "" || withoutSource.Confirm {
		t.Fatalf("remove without source mismatch: %#v", withoutSource)
	}
}

func TestSkillsCommandInstallAndRemoveUsageErrors(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Skills: []skills.Skill{{Name: "review", Source: skills.SourceUser}}}
	cases := map[string]string{
		"/skills install":                          "usage: /skills install [--confirm] [--overwrite] <https-url|path>",
		"/skills install --unknown ./my-skill":     "unknown option for /skills install: --unknown",
		"/skills install one two":                  "usage: /skills install [--confirm] [--overwrite] <https-url|path>",
		"/skills remove":                           "usage: /skills remove [--confirm] <name> [source]",
		"/skills remove --overwrite review":        "unknown option for /skills remove: --overwrite",
		"/skills remove --confirm review user bad": "usage: /skills remove [--confirm] <name> [source]",
	}
	for input, want := range cases {
		out := Dispatch(context.Background(), input, registry, ctx)
		if out.Kind != OutcomeError || out.Message != want {
			t.Fatalf("%s mismatch: %#v", input, out)
		}
	}
}

func TestSkillsCommandIgnoresExtraArgsWhereUpstreamDoes(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Skills: []skills.Skill{{Name: "review", Source: skills.SourceUser, Description: "Review code"}}}
	listed := Dispatch(context.Background(), "/skills list extra", registry, ctx)
	if listed.Kind != OutcomeHandled || !strings.Contains(listed.Message, "Loaded skills (1):") {
		t.Fatalf("list mismatch: %#v", listed)
	}
	shown := Dispatch(context.Background(), "/skills show review user extra", registry, ctx)
	if shown.Kind != OutcomeHandled || !strings.Contains(shown.Message, "Skill: review (user)") {
		t.Fatalf("show mismatch: %#v", shown)
	}
}

func TestSkillsCommandUsageErrors(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Skills: []skills.Skill{{Name: "review", Source: skills.SourceUser}}}
	cases := map[string]string{
		"/skills nope":   "usage: /skills [install [--confirm] [--overwrite] <url|path>|show <name>|reload|enable <name> [source]|disable <name> [source]|remove [--confirm] <name> [source]]",
		"/skills show":   "usage: /skills show <name> [source]",
		"/skills enable": "usage: /skills enable <name> [source]",
	}
	for input, want := range cases {
		out := Dispatch(context.Background(), input, registry, ctx)
		if out.Kind != OutcomeError || out.Message != want {
			t.Fatalf("%s mismatch: %#v", input, out)
		}
	}
}
