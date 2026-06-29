package clipboard

import "testing"

func TestClipboardPasteUpstreamVariantAliases(t *testing.T) {
	if ClipboardPasteText != PasteText || ClipboardPasteEmpty != PasteEmpty {
		t.Fatalf("clipboard paste aliases mismatch")
	}
}
