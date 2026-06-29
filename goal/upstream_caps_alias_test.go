package goal

import "testing"

func TestUpstreamCapsAliases(t *testing.T) {
	if CUSTOMTYPE != CUSTOM_TYPE {
		t.Fatalf("custom type alias mismatch")
	}
	if CUSTOMTYPE != CustomType {
		t.Fatalf("custom type alias mismatch")
	}
	if MAXCONTINUATIONS != MAX_CONTINUATIONS {
		t.Fatalf("max continuations alias mismatch")
	}
	if MAXCONTINUATIONS != MaxContinuations {
		t.Fatalf("max continuations alias mismatch")
	}
}
