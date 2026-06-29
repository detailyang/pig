package commands

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionCommandExportAndImportOutcomes(t *testing.T) {
	registry := DefaultRegistry()
	cwd := t.TempDir()
	sessionPath := filepath.Join(cwd, "sessions", "sess-1.jsonl")
	exported := Dispatch(context.Background(), "/session export backups/out.piesession --exclude-triggers", registry, Context{CWD: cwd, SessionID: "sess-1", SessionPath: sessionPath})
	wantArchive := filepath.Join(cwd, "backups", "out.piesession")
	if exported.Kind != OutcomeExportSessionArchive || exported.SessionPath != sessionPath || exported.Path != wantArchive || !exported.ExcludeTriggers || !strings.Contains(exported.Message, "warning: .piesession archives include transcript") {
		t.Fatalf("export mismatch: %#v", exported)
	}
	imported := Dispatch(context.Background(), "/session import backups/out.piesession", registry, Context{CWD: cwd})
	if imported.Kind != OutcomeImportSessionArchive || imported.Path != wantArchive || imported.ActivateAutomation != "off" || !strings.Contains(imported.Message, "warning: .piesession archives include transcript") {
		t.Fatalf("import mismatch: %#v", imported)
	}
}

func TestSessionCommandDefaultsAndUsageErrors(t *testing.T) {
	registry := DefaultRegistry()
	cwd := t.TempDir()
	exported := Dispatch(context.Background(), "/session export", registry, Context{CWD: cwd, SessionID: "0123456789abcdef-extra", SessionPath: filepath.Join(cwd, "sess-1.jsonl")})
	if exported.Kind != OutcomeExportSessionArchive || filepath.Base(exported.Path) != "pie-session-0123456789abcdef.piesession" {
		t.Fatalf("default export mismatch: %#v", exported)
	}
	missingPath := Dispatch(context.Background(), "/session export", registry, Context{CWD: cwd, SessionID: "sess-1"})
	if missingPath.Kind != OutcomeError || missingPath.Message != "session metadata is missing transcript path" {
		t.Fatalf("missing path mismatch: %#v", missingPath)
	}
	badExport := Dispatch(context.Background(), "/session export one two", registry, Context{CWD: cwd, SessionPath: "s.jsonl"})
	if badExport.Kind != OutcomeError || badExport.Message != "usage: /session export [path] [--exclude-triggers]" {
		t.Fatalf("bad export mismatch: %#v", badExport)
	}
	badImport := Dispatch(context.Background(), "/session import", registry, Context{CWD: cwd})
	if badImport.Kind != OutcomeError || badImport.Message != "usage: /session import <path>" {
		t.Fatalf("bad import mismatch: %#v", badImport)
	}
	extraImportArg := Dispatch(context.Background(), "/session import backups/out.piesession --activate-automation", registry, Context{CWD: cwd})
	if extraImportArg.Kind != OutcomeError || extraImportArg.Message != "usage: /session import <path>" {
		t.Fatalf("extra import arg mismatch: %#v", extraImportArg)
	}
	activationArg := Dispatch(context.Background(), "/session import backups/out.piesession --activate-automation ask", registry, Context{CWD: cwd})
	if activationArg.Kind != OutcomeError || activationArg.Message != "usage: /session import <path>" {
		t.Fatalf("activation import arg mismatch: %#v", activationArg)
	}
	activationEquals := Dispatch(context.Background(), "/session import --activate-automation=on backups/out.piesession", registry, Context{CWD: cwd})
	if activationEquals.Kind != OutcomeError || activationEquals.Message != "usage: /session import <path>" {
		t.Fatalf("activation equals mismatch: %#v", activationEquals)
	}
	unknown := Dispatch(context.Background(), "/session fork", registry, Context{CWD: cwd})
	if unknown.Kind != OutcomeError || !strings.Contains(unknown.Message, "unknown /session subcommand: fork") {
		t.Fatalf("unknown mismatch: %#v", unknown)
	}
}
