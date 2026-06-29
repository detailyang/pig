package clipboardimage

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestClipboardImagePackageEncodesRGBAImage(t *testing.T) {
	image, err := EncodeRGBAClipboardImage(1, 1, []byte{255, 0, 0, 255})
	if err != nil {
		t.Fatal(err)
	}
	if image.Width != 1 || image.Height != 1 || image.EncodedBytes == 0 {
		t.Fatalf("image metadata mismatch: %#v", image)
	}
	if image.Image.Type != ai.ContentImage || image.Image.MimeType != "image/png" {
		t.Fatalf("image content mismatch: %#v", image.Image)
	}
	decoded, err := base64.StdEncoding.DecodeString(image.Image.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(decoded), "\x89PNG\r\n\x1a\n") || image.EncodedBytes != len(decoded) {
		t.Fatalf("png encoding mismatch bytes=%d decoded=%d", image.EncodedBytes, len(decoded))
	}
	alias, err := EncodeRgbaClipboardImage(1, 1, []byte{255, 0, 0, 255})
	if err != nil || alias.Image.MimeType != "image/png" {
		t.Fatalf("rgba alias mismatch: %#v err=%v", alias, err)
	}
}

func TestClipboardImagePackagePasteVariants(t *testing.T) {
	image, err := EncodeRGBAImage(1, 1, []byte{255, 0, 0, 255})
	if err != nil {
		t.Fatal(err)
	}
	paste := ClipboardPaste{Kind: PasteImage, Image: &image}
	if paste.Kind != PasteImage || paste.Image == nil {
		t.Fatalf("image paste mismatch: %#v", paste)
	}
	text := ClipboardPaste{Kind: PasteText, Text: "hello"}
	if text.Kind != ClipboardPasteText || text.Text != "hello" {
		t.Fatalf("text paste mismatch: %#v", text)
	}
	empty := ClipboardPaste{Kind: PasteEmpty}
	if empty.Kind != ClipboardPasteEmpty {
		t.Fatalf("empty paste mismatch: %#v", empty)
	}
}

func TestClipboardImagePackageRejectsInvalidRGBA(t *testing.T) {
	_, err := EncodeRGBAClipboardImage(2, 2, []byte{0, 1, 2})
	if err == nil || !strings.Contains(err.Error(), "invalid RGBA buffer") {
		t.Fatalf("expected invalid RGBA error, got %v", err)
	}
}
