package agent

import "context"

func RunAgentLoop(ctx context.Context, agent *Agent, newMessages []Message) (State, error) {
	return agent.Run(ctx, newMessages)
}

func RunAgentLoopContinue(ctx context.Context, agent *Agent) (State, error) {
	return agent.Continue(ctx)
}
