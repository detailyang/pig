package commands

import (
	"context"
	"testing"
)

func TestCompactCommandReturnsRunCompactionOutcome(t *testing.T) {
	registry := DefaultRegistry()
	plain := Dispatch(context.Background(), "/compact", registry, Context{})
	if plain.Kind != OutcomeRunCompaction || plain.Custom != "" || plain.HasCustom {
		t.Fatalf("plain compact mismatch: %#v", plain)
	}
	custom := Dispatch(context.Background(), `/compact "keep decisions" and risks`, registry, Context{})
	if custom.Kind != OutcomeRunCompaction || custom.Custom != "keep decisions and risks" || !custom.HasCustom {
		t.Fatalf("custom compact mismatch: %#v", custom)
	}
}
