package commands

import (
	"context"
	"path/filepath"
	"testing"
)

func TestShareCommandReturnsGistShareOutcome(t *testing.T) {
	registry := DefaultRegistry()
	out := Dispatch(context.Background(), "/share", registry, Context{SessionID: "sess-1"})
	if out.Kind != OutcomeShareSession || out.Public || out.SessionID != "sess-1" || out.Description != "pie session sess-1" || filepath.Base(out.Path) != "transcript.md" || filepath.Base(filepath.Dir(out.Path)) != "pie-share-sess-1" {
		t.Fatalf("share mismatch: %#v", out)
	}
	pub := Dispatch(context.Background(), "/share --public", registry, Context{SessionID: "sess-2"})
	if pub.Kind != OutcomeShareSession || !pub.Public || pub.Description != "pie session sess-2" {
		t.Fatalf("public share mismatch: %#v", pub)
	}
}

func TestShareCommandIgnoresUnknownArgsLikeUpstream(t *testing.T) {
	registry := DefaultRegistry()
	private := Dispatch(context.Background(), "/share --private", registry, Context{SessionID: "sess-3"})
	if private.Kind != OutcomeShareSession || private.Public || private.Path != shareTranscriptPath("sess-3") {
		t.Fatalf("unknown share arg should create private gist like upstream: %#v", private)
	}
	tooMany := Dispatch(context.Background(), "/share --private --public --public", registry, Context{SessionID: "sess-4"})
	if tooMany.Kind != OutcomeShareSession || !tooMany.Public || tooMany.Path != shareTranscriptPath("sess-4") {
		t.Fatalf("share should scan args for public like upstream: %#v", tooMany)
	}
}
