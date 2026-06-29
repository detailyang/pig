package commands

import (
	"context"
	"strings"
	"testing"
)

func TestLoginCommandRequestsSecret(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/login openai", registry, Context{})
	if out.Kind != OutcomeLoginSecret || out.Provider != "openai" || out.StorageKey != "" || out.RecoveryCommand != "" {
		t.Fatalf("login mismatch: %#v", out)
	}
	bad := Dispatch(context.Background(), "/login", registry, Context{})
	if bad.Kind != OutcomeError || bad.Message != "usage: /login <provider>  (pie will prompt for the API key without echoing it)" {
		t.Fatalf("login usage mismatch: %#v", bad)
	}
	tooMany := Dispatch(context.Background(), "/login openai extra", registry, Context{})
	if tooMany.Kind != OutcomeError || tooMany.Message != "usage: /login <provider>  (pie will prompt for the API key without echoing it)" {
		t.Fatalf("login too many mismatch: %#v", tooMany)
	}
}

func TestLoginCommandRejectsInlineSecretWithoutEcho(t *testing.T) {
	registry := DefaultRegistry()
	secret := "sk-inline-secret-should-not-be-accepted"
	out := Dispatch(context.Background(), "/login openai "+secret, registry, Context{})
	if out.Kind != OutcomeError || out.Message != "usage: /login <provider>  (pie will prompt for the API key without echoing it)" {
		t.Fatalf("login inline secret mismatch: %#v", out)
	}
	if strings.Contains(out.Message, secret) {
		t.Fatalf("login error must not echo inline secret: %q", out.Message)
	}
}

func TestLoginRequiresTTYMessageMatchesUpstream(t *testing.T) {
	message := LoginRequiresTtyMessage("openai", "")
	if message != "/login requires an interactive terminal so the API key is not echoed; run pie in a TTY and use `/login openai`" {
		t.Fatalf("default tty message mismatch: %q", message)
	}
	custom := LoginRequiresTtyMessage("openai", "/login openai")
	if custom != message {
		t.Fatalf("explicit recovery command mismatch: %q", custom)
	}
}

func TestLogoutCommandReturnsRemoveCredentialOutcome(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/logout openai", registry, Context{})
	if out.Kind != OutcomeRemoveCredential || out.Provider != "openai" || out.Message != "remove credential for `openai`" {
		t.Fatalf("logout mismatch: %#v", out)
	}
	bad := Dispatch(context.Background(), "/logout", registry, Context{})
	if bad.Kind != OutcomeError || bad.Message != "usage: /logout <provider>" {
		t.Fatalf("logout usage mismatch: %#v", bad)
	}
	tooMany := Dispatch(context.Background(), "/logout openai extra", registry, Context{})
	if tooMany.Kind != OutcomeRemoveCredential || tooMany.Provider != "openai" {
		t.Fatalf("logout too many mismatch: %#v", tooMany)
	}
}
