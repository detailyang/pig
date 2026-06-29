package readline

import (
	"context"
	"testing"

	"github.com/detailyang/pig/commands"
	pigskills "github.com/detailyang/pig/skills"
)

func TestSlashCompleterListsCommandsAliasesAndFilters(t *testing.T) {
	completer := NewSlashCompleter([]Command{{Name: "help"}, {Name: "quit", Aliases: []string{"exit", "q"}}, {Name: "thinking"}, {Name: "goal-start"}}, nil)
	matches := completer.Matches("/")
	for _, want := range []string{"/help", "/quit", "/q"} {
		if !contains(matches, want) {
			t.Fatalf("missing %s in %#v", want, matches)
		}
	}
	if got := completer.Matches("/thi"); len(got) != 1 || got[0] != "/thinking" {
		t.Fatalf("prefix mismatch: %#v", got)
	}
	if got := completer.Matches("/goal-s"); len(got) != 1 || got[0] != "/goal-start" {
		t.Fatalf("goal prefix mismatch: %#v", got)
	}
}

func TestSlashCompleterIgnoresArgumentsAndExactUniqueMatch(t *testing.T) {
	completer := NewSlashCompleter([]Command{{Name: "skill"}, {Name: "thinking"}}, nil)
	for _, line := range []string{"/skill test", "hello", " /thinking"} {
		got := completer.Matches(line)
		if line == " /thinking" {
			if len(got) != 0 {
				t.Fatalf("exact unique match should be empty: %#v", got)
			}
			continue
		}
		if len(got) != 0 {
			t.Fatalf("expected no completions for %q, got %#v", line, got)
		}
	}
}

func TestSlashCompleterIncludesEnabledSkillShortcutsAndHidesDisabledOrConflicting(t *testing.T) {
	completer := NewSlashCompleter([]Command{{Name: "help"}, {Name: "skill"}}, []Skill{{Name: "db9"}, {Name: "hidden-skill", Disabled: true}, {Name: "help"}})
	if got := completer.Matches("/d"); len(got) != 1 || got[0] != "/db9" {
		t.Fatalf("skill shortcut mismatch: %#v", got)
	}
	if contains(completer.Matches("/hidden"), "/hidden-skill") {
		t.Fatalf("disabled skill should be hidden")
	}
	if contains(completer.Matches("/help"), "/help") {
		t.Fatalf("conflicting skill should not add duplicate exact completion")
	}
}

func TestSlashCompleterFromRegistryAndSkills(t *testing.T) {
	registry := commands.NewRegistry()
	registry.Register(testCommand{name: "help"})
	registry.Register(testCommand{name: "quit", aliases: []string{"exit", "q"}})
	completer := FromRegistryAndSkills(registry, []pigskills.Skill{{Name: "review", Source: pigskills.SourceUser}, {Name: "hidden", DisableModelInvocation: true, Source: pigskills.SourceUser}, {Name: "help", Source: pigskills.SourceUser}})
	for _, want := range []string{"/help", "/quit", "/q", "/review"} {
		if !contains(completer.Matches("/"), want) {
			t.Fatalf("missing %s in %#v", want, completer.Matches("/"))
		}
	}
	if contains(completer.Matches("/hidden"), "/hidden") || contains(completer.Matches("/help"), "/help") {
		t.Fatalf("disabled/conflicting skill shortcut should be hidden")
	}
	if got := FromRegistry(registry).Matches("/e"); len(got) != 1 || got[0] != "/exit" {
		t.Fatalf("from-registry aliases mismatch: %#v", got)
	}
}

func TestSlashTokenOnlyBareSlashToken(t *testing.T) {
	cases := map[string]string{"/": "/", "  /he": "/he", "\u2003/he": "/he", "/help now": "", "/help\u2003now": "", "hello": "", " /skill\tname": ""}
	for input, want := range cases {
		if got := SlashToken(input); got != want {
			t.Fatalf("SlashToken(%q)=%q want %q", input, got, want)
		}
	}
}

type testCommand struct {
	name    string
	aliases []string
}

func (command testCommand) Name() string { return command.name }

func (command testCommand) Description() string { return command.name }

func (command testCommand) Run(context.Context, []string, commands.Context) commands.Outcome {
	return commands.Outcome{}
}

func (command testCommand) Aliases() []string { return command.aliases }

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
