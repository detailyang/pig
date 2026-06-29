package commands

import (
	"context"
	"testing"
)

type echoCommand struct{}

func (echoCommand) Name() string        { return "echo" }
func (echoCommand) Aliases() []string   { return []string{"say"} }
func (echoCommand) Description() string { return "echo args" }
func (echoCommand) Usage() string       { return "<text>" }
func (echoCommand) Run(ctx context.Context, argv []string, cmdCtx Context) Outcome {
	if cmdCtx.SessionID != "session-1" || cmdCtx.CWD == "" {
		return Error("context mismatch")
	}
	return HandledWithText(argv)
}

func TestParseSlashCommandHandlesQuotesAndWhitespace(t *testing.T) {
	name, args, ok := Parse(`  /echo alpha "two words" beta`)
	if !ok {
		t.Fatal("expected slash command")
	}
	if name != "echo" || len(args) != 3 || args[0] != "alpha" || args[1] != "two words" || args[2] != "beta" {
		t.Fatalf("parse mismatch name=%q args=%#v", name, args)
	}
	if _, _, ok := Parse("not a command"); ok {
		t.Fatal("non slash input should not parse")
	}
	if _, _, ok := Parse("/"); ok {
		t.Fatal("bare slash should not parse")
	}
}

func TestParseSlashCommandTrimsUnicodeWhitespaceLikeUpstream(t *testing.T) {
	name, args, ok := Parse("\u2003/echo hello")
	if !ok || name != "echo" || len(args) != 1 || args[0] != "hello" {
		t.Fatalf("unicode whitespace parse mismatch name=%q args=%#v ok=%v", name, args, ok)
	}
	name, args, ok = Parse("/echo alpha\u2003beta")
	if !ok || name != "echo" || len(args) != 1 || args[0] != "alpha\u2003beta" {
		t.Fatalf("unicode argument whitespace parse mismatch name=%q args=%#v ok=%v", name, args, ok)
	}
}

func TestRegistryFindsByNameAndAliasAndReturnsSnapshot(t *testing.T) {
	registry := NewRegistry()
	registry.Register(echoCommand{})
	if registry.Find("echo") == nil || registry.Find("say") == nil || registry.Find("missing") != nil {
		t.Fatalf("find mismatch")
	}
	commands := registry.Commands()
	commands[0] = nil
	if registry.Find("echo") == nil {
		t.Fatal("commands snapshot should not mutate registry")
	}
}

func TestCommandConsoleSinkAndWithBuiltinsMatchUpstream(t *testing.T) {
	var lines []string
	SetSink(func(line string) { lines = append(lines, line) })
	EmitLine("hello")
	EmitLine("")
	ClearSink()
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "" {
		t.Fatalf("sink lines mismatch: %#v", lines)
	}

	registry := WithBuiltins()
	if registry.Find("help") == nil || registry.Find("q") == nil || registry.Find("inbox") == nil {
		t.Fatalf("with builtins registry missing commands")
	}
}

func TestDispatchRunsRegisteredCommand(t *testing.T) {
	registry := NewRegistry()
	registry.Register(echoCommand{})
	out := Dispatch(context.Background(), `/say hello "go port"`, registry, Context{CWD: t.TempDir(), SessionID: "session-1"})
	if out.Kind != OutcomeHandled || len(out.Values) != 2 || out.Values[0] != "hello" || out.Values[1] != "go port" {
		t.Fatalf("dispatch mismatch: %#v", out)
	}
	unknown := Dispatch(context.Background(), "/missing", registry, Context{})
	if unknown.Kind != OutcomeError || unknown.Message != "unknown command: /missing (try /help)" {
		t.Fatalf("unknown mismatch: %#v", unknown)
	}
	notSlash := Dispatch(context.Background(), "hello", registry, Context{})
	if notSlash.Kind != OutcomeError || notSlash.Message != "not a slash command" {
		t.Fatalf("not slash mismatch: %#v", notSlash)
	}
}

func TestThinkingLevelUpstreamPublicConstants(t *testing.T) {
	want := []string{"off", "minimal", "low", "medium", "high", "xhigh"}
	if len(THINKING_LEVEL_VALUES) != len(want) {
		t.Fatalf("thinking values length mismatch: %#v", THINKING_LEVEL_VALUES)
	}
	for index, value := range want {
		if THINKING_LEVEL_VALUES[index] != value {
			t.Fatalf("thinking value %d mismatch: %#v", index, THINKING_LEVEL_VALUES)
		}
	}
	if THINKING_LEVEL_USAGE != "[off|minimal|low|medium|high|xhigh]" {
		t.Fatalf("thinking usage mismatch: %q", THINKING_LEVEL_USAGE)
	}
}

func TestCommandsUpstreamTypeNames(t *testing.T) {
	var ctx CommandCtx = Context{SessionID: "session-1"}
	if ctx.SessionID != "session-1" {
		t.Fatalf("command context alias mismatch: %#v", ctx)
	}
	var outcome CommandOutcome = Handled()
	if outcome.Kind != OutcomeHandled {
		t.Fatalf("command outcome alias mismatch: %#v", outcome)
	}
}
