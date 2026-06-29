package agentsession

import (
	"context"
	"fmt"
	"time"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/harness"
)

type AgentSession struct {
	harness  *harness.AgentHarness
	settings RetrySettings
}

func New(h *harness.AgentHarness, settings RetrySettings) *AgentSession {
	return &AgentSession{harness: h, settings: settings}
}

func (session *AgentSession) Harness() *harness.AgentHarness {
	if session == nil {
		return nil
	}
	return session.harness
}

func (session *AgentSession) Subscribe(listener agent.AgentListener) func() {
	if session == nil || session.harness == nil {
		return func() {}
	}
	return session.harness.Subscribe(listener)
}

func (session *AgentSession) Prompt(ctx context.Context, text string) error {
	return session.run(ctx, func(first bool) error {
		if first {
			return session.harness.Prompt(ctx, text)
		}
		return session.harness.Continue(ctx)
	})
}

func (session *AgentSession) Continue(ctx context.Context) error {
	return session.run(ctx, func(bool) error { return session.harness.Continue(ctx) })
}

func (session *AgentSession) LastAssistant() *ai.AssistantMessage {
	if session == nil || session.harness == nil || session.harness.Agent() == nil {
		return nil
	}
	state := session.harness.Agent().State()
	for index := len(state.Messages) - 1; index >= 0; index-- {
		message := state.Messages[index]
		if message.LLM != nil && message.LLM.Role == ai.RoleAssistant {
			assistant := ai.AssistantMessage{Role: ai.AssistantRoleAssistant, Content: append([]ai.ContentBlock(nil), message.LLM.Content...), API: message.LLM.API, Provider: message.LLM.Provider, Model: message.LLM.Model, ResponseModel: message.LLM.ResponseModel, ResponseID: message.LLM.ResponseID, Diagnostics: message.LLM.Diagnostics, Usage: message.LLM.Usage, StopReason: message.LLM.StopReason, ErrorMessage: message.LLM.ErrorMessage, Timestamp: message.LLM.Timestamp}
			return &assistant
		}
	}
	return nil
}

func AssistantErrorMessage(message *ai.AssistantMessage) string {
	if message == nil || message.StopReason != ai.StopReasonError {
		return ""
	}
	if message.ErrorMessage != "" {
		return message.ErrorMessage
	}
	return "assistant stopped with an error"
}

func (session *AgentSession) RewindFailedAssistant() error {
	return session.prepareRetry()
}

func (session *AgentSession) run(ctx context.Context, run func(first bool) error) error {
	if session == nil || session.harness == nil {
		return fmt.Errorf("agent session is not configured")
	}
	policy := NewRetryPolicy(session.settings)
	first := true
	for {
		err := run(first)
		first = false
		if err == nil {
			err = session.lastAssistantError()
			if err == nil {
				return nil
			}
		}
		decision := policy.Next(err.Error())
		switch decision.Action {
		case RetryActionRetry:
			if rewindErr := session.prepareRetry(); rewindErr != nil {
				return rewindErr
			}
			if decision.DelayMS > 0 {
				select {
				case <-time.After(time.Duration(decision.DelayMS) * time.Millisecond):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		case RetryActionFallback:
			if decision.Fallback == nil {
				return err
			}
			model, ok := ai.GetModel(ai.Provider(decision.Fallback.Provider), decision.Fallback.ModelID)
			if !ok {
				return err
			}
			if rewindErr := session.prepareRetry(); rewindErr != nil {
				return rewindErr
			}
			if _, setErr := session.harness.SetModel(model); setErr != nil {
				return fmt.Errorf("fallback set_model failed: %w", setErr)
			}
		default:
			return err
		}
	}
}

func (session *AgentSession) lastAssistantError() error {
	message := session.LastAssistant()
	if errorMessage := AssistantErrorMessage(message); errorMessage != "" {
		return fmt.Errorf("%s", errorMessage)
	}
	return nil
}

func (session *AgentSession) prepareRetry() error {
	state := session.harness.Agent().State()
	for len(state.Messages) > 0 {
		last := state.Messages[len(state.Messages)-1]
		if last.LLM == nil || last.LLM.Role != ai.RoleAssistant || last.LLM.StopReason != ai.StopReasonError {
			break
		}
		state.Messages = state.Messages[:len(state.Messages)-1]
	}
	state.Messages = append([]agent.AgentMessage(nil), state.Messages...)
	session.harness.Agent().ReplaceState(state)
	if session.harness.Session() == nil {
		return nil
	}
	leafID, err := session.harness.Session().LeafID()
	if err != nil {
		return err
	}
	if leafID == nil {
		return nil
	}
	entry, err := session.harness.Session().GetEntry(*leafID)
	if err != nil {
		return err
	}
	if entry == nil || entry.Message == nil || entry.Message.LLM == nil || entry.Message.LLM.Role != ai.RoleAssistant || entry.Message.LLM.StopReason != ai.StopReasonError {
		return nil
	}
	previousID := ""
	if entry.ParentID != nil {
		previousID = *entry.ParentID
	}
	_, err = session.harness.Session().MoveTo(previousID, nil)
	return err
}
