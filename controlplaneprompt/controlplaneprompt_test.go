package controlplaneprompt

import (
	"context"
	"testing"
	"time"
)

func TestControlPlanePromptPackageAllowAndDenyHooks(t *testing.T) {
	allow := AllowHook()
	decision, err := allow(context.Background(), ControlPlanePromptRequest{ToolName: "bash"})
	if err != nil || decision != ControlPlaneAllow {
		t.Fatalf("allow hook mismatch decision=%s err=%v", decision, err)
	}
	deny := DenyHook("no ui")
	decision, err = deny(context.Background(), ControlPlanePromptRequest{ToolName: "bash"})
	if err != nil || decision != ControlPlaneDeny {
		t.Fatalf("deny hook mismatch decision=%s err=%v", decision, err)
	}
}

func TestControlPlanePromptPackageInteractiveHook(t *testing.T) {
	hook, queue := InteractiveHook()
	defer queue.Close()
	result := make(chan ControlPlanePromptDecision, 1)
	go func() {
		decision, err := hook(context.Background(), ControlPlanePromptRequest{ToolCallID: "call-1", ToolName: "bash"})
		if err != nil {
			result <- ControlPlaneDeny
			return
		}
		result <- decision
	}()
	prompt, ok := queue.Next(context.Background())
	if !ok || prompt.Request.ToolCallID != "call-1" {
		t.Fatalf("prompt mismatch: %#v ok=%v", prompt, ok)
	}
	var alias *UiControlPlanePrompt = prompt
	alias.Resolve(ControlPlaneAllow)
	select {
	case decision := <-result:
		if decision != ControlPlaneAllow {
			t.Fatalf("interactive decision mismatch: %s", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt resolution")
	}
}

func TestControlPlanePromptPackageInteractiveHookFailsClosed(t *testing.T) {
	hook, queue := NewInteractiveControlPlanePromptHook(0)
	queue.Close()
	decision, err := hook(context.Background(), ControlPlanePromptRequest{ToolName: "bash"})
	if err != nil || decision != ControlPlaneDeny {
		t.Fatalf("closed hook mismatch decision=%s err=%v", decision, err)
	}
}
