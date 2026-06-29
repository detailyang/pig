package ai

import (
	"reflect"
	"testing"
)

func TestGoogleSharedThinkingPartAndSignatureHelpers(t *testing.T) {
	if !IsGoogleThinkingPart(map[string]any{"thought": true}) {
		t.Fatal("thought=true should be thinking part")
	}
	if !IsThinkingPart(map[string]any{"thought": true}) {
		t.Fatal("upstream thinking helper should detect thought=true")
	}
	if IsGoogleThinkingPart(map[string]any{"thought": "true"}) || IsGoogleThinkingPart(map[string]any{}) {
		t.Fatal("only boolean true should be thinking part")
	}
	if got := RetainGoogleThoughtSignature("old", "new"); got != "new" {
		t.Fatalf("signature = %q", got)
	}
	if got := RetainThoughtSignature("old", "new"); got != "new" {
		t.Fatalf("upstream signature = %q", got)
	}
	if got := RetainGoogleThoughtSignature("old", ""); got != "old" {
		t.Fatalf("signature = %q", got)
	}
}

func TestGoogleSharedStopReasonMapping(t *testing.T) {
	cases := map[string]StopReason{
		"STOP":               StopReasonEndTurn,
		"MAX_TOKENS":         StopReasonMaxTokens,
		"SAFETY":             StopReasonError,
		"RECITATION":         StopReasonError,
		"BLOCKLIST":          StopReasonError,
		"PROHIBITED_CONTENT": StopReasonError,
		"OTHER":              StopReasonEndTurn,
	}
	for raw, want := range cases {
		if got := MapGoogleStopReason(raw); got != want {
			t.Fatalf("%s => %s want %s", raw, got, want)
		}
		if got := MapStopReason(raw); got != want {
			t.Fatalf("upstream %s => %s want %s", raw, got, want)
		}
	}
}

func TestGoogleSharedUpstreamConversionWrappers(t *testing.T) {
	messages := []Message{{
		Role: RoleAssistant,
		Content: []ContentBlock{
			{Type: ContentThinking, Thinking: "thinking", ThinkingSignature: "sig"},
			{Type: ContentText, Text: "answer"},
		},
	}}
	if got, want := ConvertMessages(messages), ConvertMessagesForGoogle(messages); !reflect.DeepEqual(got, want) {
		t.Fatalf("ConvertMessages = %#v want %#v", got, want)
	}

	tools := []Tool{{Name: "lookup", Description: "Lookup data", Parameters: map[string]any{"type": "object"}}}
	if got, want := ConvertTools(tools), ConvertToolsForGoogle(tools); !reflect.DeepEqual(got, want) {
		t.Fatalf("ConvertTools = %#v want %#v", got, want)
	}
}
