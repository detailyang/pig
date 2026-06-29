package sessionarchive

import (
	"archive/tar"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/session"
)

func TestSessionArchivePackageExportsAndImports(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "source.jsonl")
	storage, err := session.CreateJSONLStorage(sessionPath, session.JSONLMetadata{Metadata: session.Metadata{ID: "source-session", CreatedAt: "2026-01-02T03:04:05Z"}, CWD: "/source", Path: sessionPath})
	if err != nil {
		t.Fatal(err)
	}
	entry := session.NewMessageEntry("entry-1", nil, "2026-01-02T03:04:06Z", agent.Message{Kind: agent.MessageKindLLM, LLM: &ai.Message{Role: ai.RoleUser, Content: []ai.ContentBlock{{Type: ai.ContentText, Text: "hello"}}}})
	if err := storage.AppendEntry(entry); err != nil {
		t.Fatal(err)
	}

	archivePath := DefaultExportPath(dir, "source-session-with-long-suffix")
	if archivePath != filepath.Join(dir, "pie-session-source-session-w.piesession") {
		t.Fatalf("default export path mismatch: %q", archivePath)
	}
	summary, err := ExportSession(sessionPath, archivePath, false, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	if summary.SessionID != "source-session" || summary.EntryCount != 1 || summary.OutputPath != archivePath {
		t.Fatalf("export summary mismatch: %#v", summary)
	}
	if manifest := readArchiveText(t, archivePath, "manifest.json"); !strings.Contains(manifest, `"schema": "pie.session_export.v1"`) || !strings.Contains(manifest, `"pie_version": "test-version"`) {
		t.Fatalf("manifest mismatch: %s", manifest)
	}

	repo := session.NewJSONLRepo(filepath.Join(dir, "imported"))
	imported, err := ImportSession(repo, archivePath, "/dest", ActivateTriggersOff)
	if err != nil {
		t.Fatal(err)
	}
	if imported.EntryCount != 1 || imported.AutomationEnabled || imported.SessionID == "" || imported.SessionPath == "" {
		t.Fatalf("import summary mismatch: %#v", imported)
	}
	data, err := os.ReadFile(imported.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cwd":"/dest"`) || !strings.Contains(string(data), `"content":"hello"`) {
		t.Fatalf("imported session mismatch: %s", data)
	}
}

func TestSessionArchivePackageHelperAliases(t *testing.T) {
	parsed, err := ParseSessionJSONL(`{"id":"s","createdAt":"t","cwd":"/cwd","path":"/tmp/s.jsonl"}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	rewritten, err := RewriteSessionJSONL(parsed, ArchiveManifestForImport{SourceSessionID: "s", SourceCWD: "/cwd"}, "new", "/new", "/tmp/new.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rewritten, `"id":"new"`) || !strings.Contains(rewritten, `"cwd":"/new"`) {
		t.Fatalf("rewrite mismatch: %s", rewritten)
	}
	triggerSidecar, err := RewriteTriggerSidecar([]byte(`{"version":1,"rules":[{"id":"r1","enabled":true}]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if triggerSidecar.Count != 1 || len(triggerSidecar.EnabledIDs) != 1 || !json.Valid(triggerSidecar.Bytes) {
		t.Fatalf("trigger sidecar mismatch: %#v", triggerSidecar)
	}
}

func readArchiveText(t *testing.T, path, name string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != name {
			continue
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Fatalf("archive entry %s not found", name)
	return ""
}
