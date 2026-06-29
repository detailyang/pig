package extensions

import (
	"context"
	"testing"

	"github.com/detailyang/pig/commands"
)

type extensionCommand struct{}

func (extensionCommand) Name() string        { return "ext" }
func (extensionCommand) Description() string { return "extension command" }
func (extensionCommand) Run(ctx context.Context, argv []string, commandContext commands.Context) commands.Outcome {
	return commands.HandledWithText(append([]string{commandContext.SessionID}, argv...))
}

type commandExtension struct{}

func (commandExtension) Name() string { return "command-ext" }
func (commandExtension) Init(Context) (Contribution, error) {
	return Contribution{SlashCommands: []commands.SlashCommand{extensionCommand{}}}, nil
}

func TestExtensionContributedCommandDispatches(t *testing.T) {
	extensionRegistry := NewRegistry()
	extensionRegistry.Register(commandExtension{})
	out := extensionRegistry.InitAll(Context{CWD: t.TempDir(), SessionID: "extension-session"})
	if len(out.Errors) != 0 || len(out.SlashCommands) != 1 {
		t.Fatalf("extension output mismatch: %#v", out)
	}
	if len(out.Commands) != 1 || out.Commands[0].Name() != out.SlashCommands[0].Name() {
		t.Fatalf("extension commands alias mismatch: %#v", out.Commands)
	}

	commandRegistry := commands.NewRegistry()
	for _, command := range out.SlashCommands {
		commandRegistry.Register(command)
	}
	commandOut := commands.Dispatch(context.Background(), "/ext hello", commandRegistry, commands.Context{CWD: t.TempDir(), SessionID: "command-session"})
	if commandOut.Kind != commands.OutcomeHandled || len(commandOut.Values) != 2 || commandOut.Values[0] != "command-session" || commandOut.Values[1] != "hello" {
		t.Fatalf("command output mismatch: %#v", commandOut)
	}
}
