package inbox

import "testing"

func TestUpstreamCapsAlias(t *testing.T) {
	if MAXENTRYTEXTCHARS != MAX_ENTRY_TEXT_CHARS {
		t.Fatalf("max entry text chars alias mismatch")
	}
	if MAXENTRYTEXTCHARS != MaxEntryTextChars {
		t.Fatalf("max entry text chars alias mismatch")
	}
}
