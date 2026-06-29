package mentions

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExtractMentionsSimpleMultipleAndEmail(t *testing.T) {
	if got := Extract("look at @src/foo.rs please"); !reflect.DeepEqual(got, []string{"src/foo.rs"}) {
		t.Fatalf("simple mention mismatch: %#v", got)
	}
	got := Extract("review @a.rs, @b/c.rs and (@d.rs) plus @e.rs! and @f.rs:")
	want := []string{"a.rs", "b/c.rs", "d.rs", "e.rs", "f.rs"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("punctuation mention mismatch: %#v", got)
	}
	if got := Extract("ping user@host.com or a.b@host"); len(got) != 0 {
		t.Fatalf("email should be ignored: %#v", got)
	}
}

func TestUpstreamHelperNames(t *testing.T) {
	if got := ExtractMentions("review @a.rs, @b/c.rs"); !reflect.DeepEqual(got, []string{"a.rs", "b/c.rs"}) {
		t.Fatalf("ExtractMentions mismatch: %#v", got)
	}
	text := strings.Repeat("界", MaxBytes)
	truncated, ok := Truncate(text)
	if !ok || len(truncated) > MaxBytes || !utf8.ValidString(truncated) {
		t.Fatalf("Truncate mismatch: ok=%v bytes=%d", ok, len(truncated))
	}
}

func TestExpandReadsFilesMissingAndKeepsOriginalText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hi there"), 0o644); err != nil {
		t.Fatal(err)
	}
	expanded, resolved := Expand("look at @hello.txt and @missing.txt", dir)
	if !strings.HasPrefix(expanded, "Files in context:\n") {
		t.Fatalf("missing header:\n%s", expanded)
	}
	for _, want := range []string{"<file path=\"hello.txt\">", "hi there", "</file>", "<file path=\"missing.txt\"", "look at @hello.txt and @missing.txt"} {
		if !strings.Contains(expanded, want) {
			t.Fatalf("missing %q in expanded prompt:\n%s", want, expanded)
		}
	}
	if len(resolved) != 1 || resolved[0] != path {
		t.Fatalf("resolved paths mismatch: %#v", resolved)
	}
}

func TestExpandInvalidUTF8FileUsesErrorBlockLikeUpstreamReadToString(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "binary.bin"), []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	expanded, resolved := Expand("look @binary.bin", dir)
	if len(resolved) != 0 {
		t.Fatalf("invalid UTF-8 file should not be resolved like upstream read_to_string: %#v", resolved)
	}
	if !strings.Contains(expanded, `<file path="binary.bin" error="`) || strings.Contains(expanded, "\ufffd") {
		t.Fatalf("invalid UTF-8 should render an error block, got:\n%s", expanded)
	}
}

func TestExpandNoMentionsReturnsInputUnchanged(t *testing.T) {
	expanded, resolved := Expand("just a regular prompt", t.TempDir())
	if expanded != "just a regular prompt" || len(resolved) != 0 {
		t.Fatalf("no mention mismatch: %q %#v", expanded, resolved)
	}
}

func TestExpandTruncatesAtCharBoundary(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("界", MaxBytes)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	expanded, resolved := Expand("read @big.txt", dir)
	if len(resolved) != 1 {
		t.Fatalf("expected resolved big file: %#v", resolved)
	}
	if !strings.Contains(expanded, "(truncated at 64 KiB)") || !strings.Contains(expanded, "</file>") {
		t.Fatalf("truncation marker missing:\n%s", expanded[len(expanded)-200:])
	}
	if !strings.Contains(expanded, strings.Repeat("界", 10)) {
		t.Fatalf("expected valid utf-8 prefix in expansion")
	}
}
