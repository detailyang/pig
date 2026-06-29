package commands

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSaveCommandReturnsResolvedExportPath(t *testing.T) {
	registry := DefaultRegistry()
	cwd := t.TempDir()
	withPath := Dispatch(context.Background(), "/save reports/out.md", registry, Context{CWD: cwd, SessionID: "sess-1"})
	want := filepath.Join(cwd, "reports", "out.md")
	if withPath.Kind != OutcomeExportSession || withPath.Path != want || withPath.Message != "saved transcript: "+want {
		t.Fatalf("save path mismatch: %#v want=%s", withPath, want)
	}
	absolute := filepath.Join(t.TempDir(), "abs.md")
	absOut := Dispatch(context.Background(), "/save "+absolute, registry, Context{CWD: cwd, SessionID: "sess-1"})
	if absOut.Kind != OutcomeExportSession || absOut.Path != absolute {
		t.Fatalf("absolute path mismatch: %#v", absOut)
	}
}

func TestSaveCommandDefaultsAndIgnoresExtraArgsLikeUpstream(t *testing.T) {
	registry := DefaultRegistry()
	cwd := t.TempDir()
	out := Dispatch(context.Background(), "/save", registry, Context{CWD: cwd, SessionID: "sess-1"})
	if out.Kind != OutcomeExportSession || filepath.Base(out.Path) != "sess-1.md" {
		t.Fatalf("default path mismatch: %#v", out)
	}
	extra := Dispatch(context.Background(), "/save one.md two.md", registry, Context{CWD: cwd, SessionID: "sess-1"})
	if extra.Kind != OutcomeExportSession || extra.Path != filepath.Join(cwd, "one.md") {
		t.Fatalf("save should ignore extra args like upstream: %#v", extra)
	}
}
