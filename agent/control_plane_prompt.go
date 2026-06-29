package agent

import (
	"context"
	"sync"
)

type UIControlPlanePrompt struct {
	Request  ControlPlanePromptRequest
	response chan ControlPlanePromptResolution
	resolved sync.Once
}

type UiControlPlanePrompt = UIControlPlanePrompt

func (prompt *UIControlPlanePrompt) Resolve(decision ControlPlanePromptDecision) {
	prompt.ResolveWithReason(decision, "")
}

func (prompt *UIControlPlanePrompt) ResolveWithReason(decision ControlPlanePromptDecision, reason string) {
	prompt.resolved.Do(func() {
		prompt.response <- ControlPlanePromptResolution{Decision: decision, Reason: reason}
	})
}

type InteractiveControlPlanePromptQueue struct {
	prompts chan *UIControlPlanePrompt
	once    sync.Once
	mu      sync.Mutex
	pending map[*UIControlPlanePrompt]struct{}
}

func NewInteractiveControlPlanePromptHook(buffer int) (OnControlPlanePromptHook, *InteractiveControlPlanePromptQueue) {
	if buffer < 0 {
		buffer = 0
	}
	queue := &InteractiveControlPlanePromptQueue{prompts: make(chan *UIControlPlanePrompt, buffer), pending: map[*UIControlPlanePrompt]struct{}{}}
	hook := func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
		prompt := &UIControlPlanePrompt{Request: request, response: make(chan ControlPlanePromptResolution, 1)}
		if !queue.enqueue(ctx, prompt) {
			setControlPlanePromptReason(ctx, "control-plane prompt UI is unavailable")
			return ControlPlaneDeny, nil
		}
		select {
		case resolution := <-prompt.response:
			queue.forget(prompt)
			setControlPlanePromptReason(ctx, resolution.Reason)
			return resolution.Decision, nil
		case <-ctx.Done():
			queue.forget(prompt)
			setControlPlanePromptReason(ctx, "control-plane prompt cancelled")
			return ControlPlaneDeny, nil
		}
	}
	return hook, queue
}

func InteractiveHook() (OnControlPlanePromptHook, *InteractiveControlPlanePromptQueue) {
	return NewInteractiveControlPlanePromptHook(0)
}

func (queue *InteractiveControlPlanePromptQueue) enqueue(ctx context.Context, prompt *UIControlPlanePrompt) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case queue.prompts <- prompt:
		queue.remember(prompt)
		return true
	case <-ctx.Done():
		return false
	}
}

func (queue *InteractiveControlPlanePromptQueue) remember(prompt *UIControlPlanePrompt) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.pending[prompt] = struct{}{}
}

func (queue *InteractiveControlPlanePromptQueue) forget(prompt *UIControlPlanePrompt) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	delete(queue.pending, prompt)
}

func (queue *InteractiveControlPlanePromptQueue) Next(ctx context.Context) (*UIControlPlanePrompt, bool) {
	select {
	case prompt, ok := <-queue.prompts:
		return prompt, ok
	case <-ctx.Done():
		return nil, false
	}
}

func (queue *InteractiveControlPlanePromptQueue) Close() {
	queue.once.Do(func() {
		close(queue.prompts)
		queue.mu.Lock()
		pending := make([]*UIControlPlanePrompt, 0, len(queue.pending))
		for prompt := range queue.pending {
			pending = append(pending, prompt)
		}
		queue.pending = map[*UIControlPlanePrompt]struct{}{}
		queue.mu.Unlock()
		for _, prompt := range pending {
			prompt.ResolveWithReason(ControlPlaneDeny, "control-plane prompt UI closed before a decision")
		}
	})
}

func AllowControlPlanePromptHook() OnControlPlanePromptHook {
	return func(context.Context, ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
		return ControlPlaneAllow, nil
	}
}

func AllowHook() OnControlPlanePromptHook {
	return AllowControlPlanePromptHook()
}

func DenyControlPlanePromptHook(reason string) OnControlPlanePromptHook {
	return func(ctx context.Context, request ControlPlanePromptRequest) (ControlPlanePromptDecision, error) {
		setControlPlanePromptReason(ctx, reason)
		return ControlPlaneDeny, nil
	}
}

func DenyHook(reason string) OnControlPlanePromptHook {
	return DenyControlPlanePromptHook(reason)
}

type controlPlanePromptReasonContextKey struct{}

func withControlPlanePromptReason(ctx context.Context, reason *string) context.Context {
	return context.WithValue(ctx, controlPlanePromptReasonContextKey{}, reason)
}

func setControlPlanePromptReason(ctx context.Context, reason string) {
	if target, ok := ctx.Value(controlPlanePromptReasonContextKey{}).(*string); ok && target != nil {
		*target = reason
	}
}
