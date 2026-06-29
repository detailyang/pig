package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/config"
)

func TestLogsDirUsesConfiguredBaseDir(t *testing.T) {
	t.Setenv("PIE_DIR", t.TempDir())
	if got, want := LogsDir(), filepath.Join(config.BaseDir(), "logs"); got != want {
		t.Fatalf("LogsDir()=%q want %q", got, want)
	}
}

func TestInitCreatesShortSessionLogFileAndWrites(t *testing.T) {
	t.Setenv("PIE_DIR", t.TempDir())
	var handle *LoggingHandle = Init("019c6e27-e55b-73d1-87d8-4e01f1f75043")
	if handle == nil {
		t.Fatal("expected logging handle")
	}
	if got, want := handle.LogPath, filepath.Join(config.BaseDir(), "logs", "019c6e27-e55b-73.log"); got != want {
		t.Fatalf("LogPath=%q want %q", got, want)
	}
	if err := handle.Write("hello"); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(handle.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("log did not contain message: %q", data)
	}
}

func TestInitReturnsNilWhenLogPathCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PIE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "logs"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if handle := Init("session"); handle != nil {
		t.Fatalf("expected nil handle, got %#v", handle)
	}
}

func TestShortCapsAtSixteenBytes(t *testing.T) {
	if got := Short("1234567890abcdefZZZ"); got != "1234567890abcdef" {
		t.Fatalf("Short long=%q", got)
	}
	if got := Short("abc"); got != "abc" {
		t.Fatalf("Short short=%q", got)
	}
}
