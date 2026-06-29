package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/detailyang/pig/skills"
)

func TestDefaultRegistryDispatchesClearAndQuitAliases(t *testing.T) {
	registry := DefaultRegistry()
	if out := Dispatch(context.Background(), "/clear", registry, Context{}); out.Kind != OutcomeClearScreen {
		t.Fatalf("clear mismatch: %#v", out)
	}
	for _, input := range []string{"/quit", "/exit", "/q"} {
		if out := Dispatch(context.Background(), input, registry, Context{}); out.Kind != OutcomeQuit {
			t.Fatalf("%s mismatch: %#v", input, out)
		}
	}
}

func TestDefaultRegistryOrderMatchesUpstream(t *testing.T) {
	registry := DefaultRegistry()
	var names []string
	for _, command := range registry.Commands() {
		names = append(names, command.Name())
	}
	want := []string{"help", "clear", "skills", "skill", "quit", "model", "thinking", "cost", "diag", "template", "save", "compact", "undo", "bug-report", "name", "session", "sessions", "share", "login", "logout", "find", "history", "goal", "goal-start", "triggers", "new-trigger", "cron", "inbox"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("default registry order mismatch:\n got: %v\nwant: %v", names, want)
	}
}

func TestSkillCommandAttachesEnabledSkillAndSuggestsMatches(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Skills: []skills.Skill{
		{Name: "code-review", Source: skills.SourceUser},
		{Name: "code-search", Source: skills.SourceProject},
		{Name: "disabled", Source: skills.SourceUser, DisableModelInvocation: true},
	}}
	out := Dispatch(context.Background(), "/skill code-review", registry, ctx)
	if out.Kind != OutcomeAttachSkill || out.Name != "code-review" || !strings.Contains(out.Message, "using skill: code-review (user)") {
		t.Fatalf("attach mismatch: %#v", out)
	}
	missing := Dispatch(context.Background(), "/skill code", registry, ctx)
	if missing.Kind != OutcomeError || !strings.Contains(missing.Message, "Did you mean: code-review, code-search?") {
		t.Fatalf("missing hint mismatch: %#v", missing)
	}
	disabled := Dispatch(context.Background(), "/skill disabled", registry, ctx)
	if disabled.Kind != OutcomeError || !strings.Contains(disabled.Message, "skill 'disabled' is disabled") {
		t.Fatalf("disabled mismatch: %#v", disabled)
	}
	badUsage := Dispatch(context.Background(), "/skill", registry, ctx)
	if badUsage.Kind != OutcomeError || badUsage.Message != "usage: /skill <name>" {
		t.Fatalf("usage mismatch: %#v", badUsage)
	}
}

func TestAttachSkillPrompt(t *testing.T) {
	plain := AttachSkillPrompt("hello", "")
	if plain != "hello" {
		t.Fatalf("plain prompt mismatch: %q", plain)
	}
	withSkill := AttachSkillPrompt("hello", "code-review")
	if !strings.Contains(withSkill, `invoke the Skill tool with name "code-review"`) || !strings.Contains(withSkill, "User request:\nhello") {
		t.Fatalf("skill prompt mismatch: %q", withSkill)
	}
}

func TestHelpCommandRendersRegisteredCommands(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Skills: []skills.Skill{
		{Name: "review-code", Source: skills.SourceUser, Description: strings.Repeat("review ", 20)},
		{Name: "disabled-shortcut", Source: skills.SourceUser, DisableModelInvocation: true},
		{Name: "ambiguous", Source: skills.SourceUser},
		{Name: "ambiguous", Source: skills.SourceProject},
		{Name: "quit", Source: skills.SourceUser},
	}}
	out := Dispatch(context.Background(), "/help", registry, ctx)
	if out.Kind != OutcomeHandled || !strings.Contains(out.Message, "\nCommands:\n") || !strings.Contains(out.Message, "  /skill <name>    attach a loaded skill to the next prompt") || !strings.Contains(out.Message, "  /quit    exit the REPL (aliases: exit, q)") || !strings.Contains(out.Message, "  /help [models|<command>]    show available commands and model catalog help") || !strings.Contains(out.Message, "\nSkill commands:\n") || !strings.Contains(out.Message, "  /review-code [prompt]    use loaded skill (user) — review review") || !strings.Contains(out.Message, "…") || strings.Contains(out.Message, "/disabled-shortcut") || strings.Contains(out.Message, "/ambiguous [prompt]") || strings.Contains(out.Message, "/quit [prompt]") || !strings.Contains(out.Message, "\nModels:\n") || !strings.Contains(out.Message, "  Full list: /help models or /model list [provider]") || !strings.Contains(out.Message, "Anything else is sent as a prompt to the agent.") {
		t.Fatalf("help mismatch: %#v", out)
	}
	quit := Dispatch(context.Background(), "/help /quit", registry, Context{})
	if quit.Kind != OutcomeHandled || !strings.Contains(quit.Message, "/quit\n  exit the REPL") || !strings.Contains(quit.Message, "  aliases: /exit, /q") || !strings.Contains(quit.Message, "  more: /help quit") {
		t.Fatalf("quit help mismatch: %#v", quit)
	}
	clear := Dispatch(context.Background(), "/help clear", registry, Context{})
	if clear.Kind != OutcomeHandled || clear.Message != "/clear\n  clear screen (keeps conversation history)\n  more: /help clear" {
		t.Fatalf("clear help mismatch: %#v", clear)
	}
	help := Dispatch(context.Background(), "/help help", registry, Context{})
	if help.Kind != OutcomeHandled || !strings.Contains(help.Message, "/help [models|<command>]\n  show available commands and model catalog help") || !strings.Contains(help.Message, "  examples: /help model, /help /quit, /help models") {
		t.Fatalf("help topic mismatch: %#v", help)
	}
	unknown := Dispatch(context.Background(), "/help qui", registry, Context{})
	if unknown.Kind != OutcomeError || unknown.Message != "unknown help topic: qui\nDid you mean /quit?" {
		t.Fatalf("unknown help mismatch: %#v", unknown)
	}
	skillSuggestion := Dispatch(context.Background(), "/help rev", registry, ctx)
	if skillSuggestion.Kind != OutcomeError || skillSuggestion.Message != "unknown help topic: rev\nDid you mean /review-code?" {
		t.Fatalf("skill suggestion mismatch: %#v", skillSuggestion)
	}
	models := Dispatch(context.Background(), "/help models", registry, Context{})
	if models.Kind != OutcomeHandled || !strings.Contains(models.Message, "Supported providers/models:") || !strings.Contains(models.Message, "Custom models can be registered explicitly with config.LoadModelsFile/LoadModelsFiles; local models.json auto-loading is disabled.") {
		t.Fatalf("models help mismatch: %#v", models)
	}
	skillHelp := Dispatch(context.Background(), "/help review-code", registry, ctx)
	if skillHelp.Kind != OutcomeHandled || !strings.Contains(skillHelp.Message, "/review-code [prompt]\n  use loaded skill 'review-code' (user)") || !strings.Contains(skillHelp.Message, "  equivalent: /skill review-code") {
		t.Fatalf("skill help mismatch: %#v", skillHelp)
	}
}

func TestCLIModelHelpTextMatchesUpstreamShape(t *testing.T) {
	text := CliModelHelpText()
	if !strings.HasPrefix(text, "Model catalog:\n") || !strings.Contains(text, "  Supported providers (") || !strings.Contains(text, "  Full list: /help models or /model list [provider]") || !strings.Contains(text, "  Custom models: explicit config.LoadModelsFile/LoadModelsFiles only; local models.json auto-loading is disabled") || !strings.Contains(text, "  Credentials: set provider env vars or run /login <provider>.") {
		t.Fatalf("model help text mismatch:\n%s", text)
	}
}

func TestSkillShortcutDispatchesLikeUpstream(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Skills: []skills.Skill{
		{Name: "review-code", Source: skills.SourceUser},
		{Name: "disabled-shortcut", Source: skills.SourceUser, DisableModelInvocation: true},
		{Name: "ambiguous", Source: skills.SourceUser},
		{Name: "ambiguous", Source: skills.SourceProject},
	}}
	attach := Dispatch(context.Background(), "/review-code", registry, ctx)
	if attach.Kind != OutcomeAttachSkill || attach.Name != "review-code" || !strings.Contains(attach.Message, "using skill: review-code (user) for next turn") {
		t.Fatalf("attach shortcut mismatch: %#v", attach)
	}
	run := Dispatch(context.Background(), "/review-code inspect this", registry, ctx)
	if run.Kind != OutcomeRunPrompt || run.ErrorContext != "skill command failed: " || !strings.Contains(run.Prompt, `invoke the Skill tool with name "review-code"`) || !strings.Contains(run.Prompt, "User request:\ninspect this") {
		t.Fatalf("run shortcut mismatch: %#v", run)
	}
	disabled := Dispatch(context.Background(), "/disabled-shortcut", registry, ctx)
	if disabled.Kind != OutcomeError || disabled.Message != "skill 'disabled-shortcut' is disabled; run /skills enable disabled-shortcut [source] or /skills to list loaded skills" {
		t.Fatalf("disabled shortcut mismatch: %#v", disabled)
	}
	ambiguous := Dispatch(context.Background(), "/ambiguous", registry, ctx)
	if ambiguous.Kind != OutcomeError || ambiguous.Message != "multiple enabled skills named 'ambiguous'; use /skill ambiguous after resolving the source with /skills show ambiguous [source]" {
		t.Fatalf("ambiguous shortcut mismatch: %#v", ambiguous)
	}
}

func TestSkillShortcutsUpstreamPublicAPI(t *testing.T) {
	registry := DefaultRegistry()
	shortcuts := SkillShortcuts([]skills.Skill{
		{Name: "review-code", Source: skills.SourceUser, Description: strings.Repeat("review ", 20)},
		{Name: "disabled", Source: skills.SourceUser, DisableModelInvocation: true},
		{Name: "ambiguous", Source: skills.SourceUser},
		{Name: "ambiguous", Source: skills.SourceProject},
		{Name: "quit", Source: skills.SourceUser},
	}, registry)
	if len(shortcuts) != 1 {
		t.Fatalf("shortcut count mismatch: %#v", shortcuts)
	}
	var shortcut SkillShortcut = shortcuts[0]
	if shortcut.Command != "/review-code" || shortcut.Source != skills.SourceUser || !strings.HasSuffix(shortcut.Description, "…") || strings.Contains(shortcut.Description, "\n") {
		t.Fatalf("shortcut mismatch: %#v", shortcut)
	}
}
