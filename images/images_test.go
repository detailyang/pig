package images

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestImagesPackageInferMimeAndLoadBytes(t *testing.T) {
	if MAX_PER_IMAGE_BYTES != MaxPerImageBytes || MAX_IMAGES_PER_MESSAGE != MaxImagesPerMessage {
		t.Fatalf("limit aliases mismatch")
	}
	cases := []struct {
		data []byte
		mime string
	}{
		{[]byte("\x89PNG\r\n\x1a\nmore"), "image/png"},
		{[]byte("\xff\xd8\xffmore"), "image/jpeg"},
		{append(append([]byte("RIFF"), []byte{0, 0, 0, 0}...), []byte("WEBPmore")...), "image/webp"},
		{[]byte("GIF89amore"), "image/gif"},
	}
	for _, tc := range cases {
		if got, ok := InferMime(tc.data); !ok || got != tc.mime {
			t.Fatalf("mime mismatch got=%q ok=%v want=%q", got, ok, tc.mime)
		}
	}
	if got, ok := InferMime([]byte("not image")); ok || got != "" {
		t.Fatalf("unknown image should not infer got=%q ok=%v", got, ok)
	}
	bytes := []byte("\x89PNG\r\n\x1a\nhello")
	block, err := LoadBytes("clipboard", bytes)
	if err != nil {
		t.Fatal(err)
	}
	if block.Type != ai.ContentImage || block.MimeType != "image/png" || block.Data != base64.StdEncoding.EncodeToString(bytes) {
		t.Fatalf("image block mismatch: %#v", block)
	}
}

func TestImagesPackageLoadOneAndAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.png")
	bytes := []byte("\x89PNG\r\n\x1a\nhello")
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	one, err := LoadOne(path)
	if err != nil {
		t.Fatal(err)
	}
	all, err := LoadAll([]string{path})
	if err != nil || len(all) != 1 || all[0] != one {
		t.Fatalf("load all mismatch all=%#v err=%v", all, err)
	}
}

func TestImagesPackageRejectsInvalidInputs(t *testing.T) {
	if _, err := LoadBytes("bad", []byte("hello")); err == nil || !strings.Contains(err.Error(), "unsupported image format") {
		t.Fatalf("expected unsupported format, got %v", err)
	}
	oversized := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, MaxPerImageBytes+1)...)
	if _, err := LoadBytes("big", oversized); err == nil || !strings.Contains(err.Error(), "exceeds 10MB cap") {
		t.Fatalf("expected oversized error, got %v", err)
	}
	paths := make([]string, MaxImagesPerMessage+1)
	if _, err := LoadAll(paths); err == nil || !strings.Contains(err.Error(), "exceeds per-message cap") {
		t.Fatalf("expected too many images error, got %v", err)
	}
}
