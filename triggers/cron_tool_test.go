package triggers

import (
	"strings"
	"testing"
)

func TestCronActionPreviewMatchesUpstream(t *testing.T) {
	if got := cronActionPreview("  keep surrounding spaces  "); got != "  keep surrounding spaces  " {
		t.Fatalf("preview should preserve surrounding spaces like upstream, got %q", got)
	}

	long := strings.Repeat("界", 121)
	got := cronActionPreview(long)
	if want := strings.Repeat("界", 120) + "……"; got != want {
		t.Fatalf("preview should truncate by chars at 120 like upstream, got chars=%d value=%q", len([]rune(got)), got)
	}
}
