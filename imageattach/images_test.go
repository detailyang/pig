package imageattach

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestInferMimeSupportedFormats(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		mime string
	}{
		{"png", []byte("\x89PNG\r\n\x1a\n0000more"), "image/png"},
		{"jpeg", []byte("\xff\xd8\xff\xe000more"), "image/jpeg"},
		{"webp", append(append([]byte("RIFF"), []byte{0, 0, 0, 0}...), []byte("WEBPmore")...), "image/webp"},
		{"gif87", []byte("GIF87amore"), "image/gif"},
		{"gif89", []byte("GIF89amore"), "image/gif"},
	}
	for _, tc := range cases {
		if got, ok := InferMime(tc.data); !ok || got != tc.mime {
			t.Fatalf("%s mime mismatch got=%q ok=%v", tc.name, got, ok)
		}
	}
	if got, ok := InferMime([]byte("not an image")); ok || got != "" {
		t.Fatalf("unknown format should fail got=%q ok=%v", got, ok)
	}
}

func TestLoadBytesReturnsImageContentBlock(t *testing.T) {
	bytes := []byte("\x89PNG\r\n\x1a\nhello")
	block, err := LoadBytes("clipboard", bytes)
	if err != nil {
		t.Fatal(err)
	}
	if block.Type != ai.ContentImage || block.MimeType != "image/png" || block.Data != base64.StdEncoding.EncodeToString(bytes) {
		t.Fatalf("image block mismatch: %#v", block)
	}
}

func TestLoadOneAndLoadAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.png")
	bytes := []byte("\x89PNG\r\n\x1a\nhello")
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	block, err := LoadOne(path)
	if err != nil {
		t.Fatal(err)
	}
	fromBytes, err := LoadBytes("x", bytes)
	if err != nil {
		t.Fatal(err)
	}
	if block != fromBytes {
		t.Fatalf("load one mismatch: %#v %#v", block, fromBytes)
	}
	blocks, err := LoadAll([]string{path})
	if err != nil || len(blocks) != 1 || blocks[0] != block {
		t.Fatalf("load all mismatch blocks=%#v err=%v", blocks, err)
	}
}

func TestRejectsUnknownOversizedAndTooMany(t *testing.T) {
	if MAX_PER_IMAGE_BYTES != MaxPerImageBytes || MAX_IMAGES_PER_MESSAGE != MaxImagesPerMessage {
		t.Fatalf("upstream image limit aliases mismatch: max=%d count=%d", MAX_PER_IMAGE_BYTES, MAX_IMAGES_PER_MESSAGE)
	}
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
