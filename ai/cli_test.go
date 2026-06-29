package ai

import "testing"

func TestMainEntryCompatStub(t *testing.T) {
	want := "pie-ai-rs CLI is a TODO. Use `cargo run --example anthropic_hello` for now."
	if got := MainEntry(); got != want {
		t.Fatalf("MainEntry mismatch got=%q want=%q", got, want)
	}
	if got := main_entry(); got != want {
		t.Fatalf("main_entry alias mismatch got=%q want=%q", got, want)
	}
}
