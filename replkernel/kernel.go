package replkernel

import (
	"context"

	"github.com/detailyang/pig/ai"
)

type TurnFunc func(context.Context) (string, error)

type TurnState struct {
	Fut     TurnFunc
	Aborted bool
	Prefix  string
}

func (state *TurnState) Poll(ctx context.Context) (string, error) {
	if state == nil || state.Fut == nil {
		return "", nil
	}
	fut := state.Fut
	state.Fut = nil
	return fut(ctx)
}

func CompletedTurn(message string) TurnFunc {
	return func(context.Context) (string, error) { return message, nil }
}

func CompletedTurnError(err error) TurnFunc {
	return func(context.Context) (string, error) { return "", err }
}

type QueuedTurnKind string

const (
	QueuedTurnUserPrompt     QueuedTurnKind = "user_prompt"
	QueuedTurnAgentPrompt    QueuedTurnKind = "agent_prompt"
	QueuedTurnPromptTemplate QueuedTurnKind = "prompt_template"
	QueuedTurnCompaction     QueuedTurnKind = "compaction"
)

type QueuedTurn struct {
	Kind         QueuedTurnKind
	DisplayText  string
	Prompt       string
	Images       []ai.ContentBlock
	ErrorContext string
	TemplateName string
	Vars         map[string]any
	Custom       string
}

func UserPromptTurn(display string, prompt string, images []ai.ContentBlock) QueuedTurn {
	return QueuedTurn{Kind: QueuedTurnUserPrompt, DisplayText: display, Prompt: prompt, Images: append([]ai.ContentBlock(nil), images...)}
}

func AgentPromptTurn(display string, prompt string, errorContext string) QueuedTurn {
	return QueuedTurn{Kind: QueuedTurnAgentPrompt, DisplayText: display, Prompt: prompt, ErrorContext: errorContext}
}

func PromptTemplateTurn(display string, name string, vars map[string]any) QueuedTurn {
	return QueuedTurn{Kind: QueuedTurnPromptTemplate, DisplayText: display, TemplateName: name, Vars: cloneMap(vars)}
}

func CompactionTurn(display string, custom string) QueuedTurn {
	return QueuedTurn{Kind: QueuedTurnCompaction, DisplayText: display, Custom: custom}
}

func (turn QueuedTurn) Display() string { return turn.DisplayText }

type Harness interface {
	Abort()
	IsStreaming() bool
	CurrentModelAcceptsImages() bool
	Prompt(ctx context.Context, prompt string) error
	PromptWithImages(ctx context.Context, prompt string, images []ai.ContentBlock) error
	PromptFromTemplate(ctx context.Context, name string, vars map[string]any) error
	ForceCompact(ctx context.Context, custom string) (bool, error)
	Continue(ctx context.Context) error
}

type Kernel struct {
	harness Harness
}

func New(harness Harness) *Kernel { return &Kernel{harness: harness} }

func (kernel *Kernel) Harness() Harness {
	if kernel == nil {
		return nil
	}
	return kernel.harness
}

func (kernel *Kernel) Abort() {
	if kernel != nil && kernel.harness != nil {
		kernel.harness.Abort()
	}
}

func (kernel *Kernel) IsStreaming() bool {
	return kernel != nil && kernel.harness != nil && kernel.harness.IsStreaming()
}

func (kernel *Kernel) CurrentModelAcceptsImages() bool {
	return kernel != nil && kernel.harness != nil && kernel.harness.CurrentModelAcceptsImages()
}

func (kernel *Kernel) PromptTurn(prompt string) TurnFunc {
	return func(ctx context.Context) (string, error) {
		return "", kernel.harness.Prompt(ctx, prompt)
	}
}

func (kernel *Kernel) UserPromptTurn(prompt string, images []ai.ContentBlock) TurnFunc {
	return func(ctx context.Context) (string, error) {
		if len(images) > 0 {
			return "", kernel.harness.PromptWithImages(ctx, prompt, images)
		}
		return "", kernel.harness.Prompt(ctx, prompt)
	}
}

func (kernel *Kernel) TemplateTurn(name string, vars map[string]any) TurnFunc {
	return func(ctx context.Context) (string, error) {
		return "", kernel.harness.PromptFromTemplate(ctx, name, vars)
	}
}

func (kernel *Kernel) CompactionTurn(custom string) TurnFunc {
	return func(ctx context.Context) (string, error) {
		ran, err := kernel.harness.ForceCompact(ctx, custom)
		if err != nil {
			return "", err
		}
		if ran {
			return "compaction ran", nil
		}
		return "nothing to compact", nil
	}
}

func (kernel *Kernel) ContinueTurn() TurnFunc {
	return func(ctx context.Context) (string, error) {
		return "", kernel.harness.Continue(ctx)
	}
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
