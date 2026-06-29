package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeEnvReExportMatchesUpstreamNodeEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := NewNativeEnv(root)
	path, err := env.CanonicalPath(context.Background(), "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(root, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if path != want {
		t.Fatalf("canonical path mismatch: %s", path)
	}
}
