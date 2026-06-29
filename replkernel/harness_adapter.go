package replkernel

import (
	"context"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/compaction"
	"github.com/detailyang/pig/harness"
)

type AgentHarnessAdapter struct {
	harness *harness.AgentHarness
}

func NewAgentHarnessAdapter(h *harness.AgentHarness) *AgentHarnessAdapter {
	return &AgentHarnessAdapter{harness: h}
}

func (adapter *AgentHarnessAdapter) Abort() {
	if adapter != nil && adapter.harness != nil {
		adapter.harness.Abort()
	}
}

func (adapter *AgentHarnessAdapter) IsStreaming() bool {
	return adapter != nil && adapter.harness != nil && adapter.harness.Agent() != nil && adapter.harness.Agent().IsStreaming()
}

func (adapter *AgentHarnessAdapter) CurrentModelAcceptsImages() bool {
	if adapter == nil || adapter.harness == nil || adapter.harness.Agent() == nil {
		return false
	}
	state := adapter.harness.Agent().State()
	if state.Model == nil {
		return false
	}
	for _, modality := range state.Model.Input {
		if modality == ai.InputImage {
			return true
		}
	}
	return false
}

func (adapter *AgentHarnessAdapter) Prompt(ctx context.Context, prompt string) error {
	return adapter.harness.Prompt(ctx, prompt)
}

func (adapter *AgentHarnessAdapter) PromptWithImages(ctx context.Context, prompt string, images []ai.ContentBlock) error {
	return adapter.harness.PromptWithImages(ctx, prompt, images)
}

func (adapter *AgentHarnessAdapter) PromptFromTemplate(ctx context.Context, name string, vars map[string]any) error {
	return adapter.harness.PromptFromTemplate(ctx, name, vars)
}

func (adapter *AgentHarnessAdapter) ForceCompact(ctx context.Context, custom string) (bool, error) {
	if custom == "" {
		return adapter.harness.ForceCompact(ctx, nil)
	}
	return adapter.harness.ForceCompact(ctx, compaction.SummarizerFunc(func(context.Context, compaction.SummarizationRequest) (string, error) {
		return custom, nil
	}))
}

func (adapter *AgentHarnessAdapter) Continue(ctx context.Context) error {
	return adapter.harness.Continue(ctx)
}
