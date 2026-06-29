package sessionrunner

import (
	"context"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

type SessionRunner struct {
	Model         ai.Model
	APIKey        string
	SystemPrompt  string
	Tools         []agent.Tool
	CodingAgent   *agent.CodingAgent
	Stream        agent.StreamFunc
	Events        agent.Listener
	StreamOptions ai.SimpleStreamOptions
	Config        agent.Config
	ToolExecution agent.ToolExecutionMode
}

type RunSessionInput = session.RunSessionInput

type RunSessionOutput = session.RunSessionOutput

func (runner SessionRunner) Run(ctx context.Context, input RunSessionInput) (RunSessionOutput, error) {
	return runner.sessionRunner().Run(ctx, input)
}

func (runner SessionRunner) sessionRunner() session.SessionRunner {
	systemPrompt := runner.SystemPrompt
	tools := append([]agent.Tool(nil), runner.Tools...)
	if runner.CodingAgent != nil {
		state := runner.CodingAgent.State()
		systemPrompt = state.SystemPrompt
		tools = append([]agent.Tool(nil), state.Tools...)
	}
	return session.SessionRunner{
		Model:         runner.Model,
		APIKey:        runner.APIKey,
		SystemPrompt:  systemPrompt,
		Tools:         tools,
		Stream:        runner.Stream,
		Events:        runner.Events,
		StreamOptions: runner.StreamOptions,
		Config:        runner.Config,
		ToolExecution: runner.ToolExecution,
	}
}
