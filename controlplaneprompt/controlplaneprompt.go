package controlplaneprompt

import "github.com/detailyang/pig/agent"

type ControlPlanePromptRequest = agent.ControlPlanePromptRequest
type ControlPlanePromptDecision = agent.ControlPlanePromptDecision
type ControlPlanePromptResolution = agent.ControlPlanePromptResolution
type OnControlPlanePromptHook = agent.OnControlPlanePromptHook
type UIControlPlanePrompt = agent.UIControlPlanePrompt
type UiControlPlanePrompt = agent.UiControlPlanePrompt
type InteractiveControlPlanePromptQueue = agent.InteractiveControlPlanePromptQueue

const ControlPlaneAllow = agent.ControlPlaneAllow
const ControlPlaneDeny = agent.ControlPlaneDeny
const ControlPlaneTimeout = agent.ControlPlaneTimeout

const ControlPlanePromptDecisionAllow = agent.ControlPlanePromptDecisionAllow
const ControlPlanePromptDecisionDeny = agent.ControlPlanePromptDecisionDeny
const ControlPlanePromptDecisionTimeout = agent.ControlPlanePromptDecisionTimeout

func NewInteractiveControlPlanePromptHook(buffer int) (OnControlPlanePromptHook, *InteractiveControlPlanePromptQueue) {
	return agent.NewInteractiveControlPlanePromptHook(buffer)
}

func InteractiveHook() (OnControlPlanePromptHook, *InteractiveControlPlanePromptQueue) {
	return agent.InteractiveHook()
}

func AllowControlPlanePromptHook() OnControlPlanePromptHook {
	return agent.AllowControlPlanePromptHook()
}

func AllowHook() OnControlPlanePromptHook {
	return agent.AllowHook()
}

func DenyControlPlanePromptHook(reason string) OnControlPlanePromptHook {
	return agent.DenyControlPlanePromptHook(reason)
}

func DenyHook(reason string) OnControlPlanePromptHook {
	return agent.DenyHook(reason)
}
