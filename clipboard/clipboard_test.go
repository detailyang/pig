package clipboard

import (
	"encoding/base64"
	"runtime"
	"strings"
	"testing"

	"github.com/detailyang/pig/ai"
)

func TestEncodeRGBAImageAsPNG(t *testing.T) {
	img, err := EncodeRGBAImage(1, 1, []byte{255, 0, 0, 255})
	if err != nil {
		t.Fatal(err)
	}
	if img.Width != 1 || img.Height != 1 || img.EncodedBytes == 0 {
		t.Fatalf("unexpected dimensions: %#v", img)
	}
	if img.Image.Type != ai.ContentImage || img.Image.MimeType != "image/png" {
		t.Fatalf("unexpected image content: %#v", img.Image)
	}
	decoded, err := base64.StdEncoding.DecodeString(img.Image.Data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(decoded), "\x89PNG\r\n\x1a\n") {
		t.Fatalf("expected png bytes, got %q", decoded[:8])
	}
	if img.EncodedBytes != len(decoded) {
		t.Fatalf("encoded bytes mismatch: %d != %d", img.EncodedBytes, len(decoded))
	}
}

func TestClipboardPasteFromText(t *testing.T) {
	if got := clipboardPasteFromText("hello"); got.Kind != PasteText || got.Text != "hello" {
		t.Fatalf("text paste mismatch: %#v", got)
	}
	if got := clipboardPasteFromText(""); got.Kind != PasteEmpty || got.Text != "" {
		t.Fatalf("empty paste mismatch: %#v", got)
	}
}

func TestClipboardUpstreamExportedNames(t *testing.T) {
	img, err := EncodeRGBAClipboardImage(1, 1, []byte{255, 0, 0, 255})
	if err != nil {
		t.Fatal(err)
	}
	alias, err := EncodeRgbaClipboardImage(1, 1, []byte{255, 0, 0, 255})
	if err != nil || alias.Width != 1 || alias.Image.MimeType != "image/png" {
		t.Fatalf("Rgba alias mismatch: image=%#v err=%v", alias, err)
	}
	var clipboardImage ClipboardImage = img
	if clipboardImage.Width != 1 || clipboardImage.Height != 1 || clipboardImage.Image.MimeType != "image/png" {
		t.Fatalf("clipboard image alias mismatch: %#v", clipboardImage)
	}
	paste := ClipboardPaste{Kind: PasteImage, Image: &clipboardImage}
	if paste.Kind != PasteImage || paste.Image == nil {
		t.Fatalf("clipboard paste alias mismatch: %#v", paste)
	}
	if runtime.GOOS != "darwin" {
		if _, err := ReadClipboard(); err == nil {
			t.Fatal("ReadClipboard should return a platform error in unsupported test environment")
		}
		if _, err := ReadClipboardSync(); err == nil {
			t.Fatal("ReadClipboardSync should return a platform error in unsupported test environment")
		}
		if _, err := read_clipboard_sync(); err == nil {
			t.Fatal("read_clipboard_sync should return a platform error in unsupported test environment")
		}
	}
}

func TestEncodeRGBAImageRejectsInvalidBufferSize(t *testing.T) {
	_, err := EncodeRGBAImage(2, 2, []byte{0, 1, 2})
	if err == nil || !strings.Contains(err.Error(), "invalid RGBA buffer") {
		t.Fatalf("expected invalid buffer error, got %v", err)
	}
}

func TestEncodeRGBAImageRejectsOverflowingDimensions(t *testing.T) {
	_, err := EncodeRGBAImage(maxInt, 2, nil)
	if err == nil || !strings.Contains(err.Error(), "dimensions are too large") {
		t.Fatalf("expected dimension error, got %v", err)
	}
}

func TestEncodeRGBAImageRejectsDimensionsAbovePNGLimitLikeUpstream(t *testing.T) {
	_, err := EncodeRGBAImage(1<<32, 0, nil)
	if err == nil || err.Error() != "clipboard image width is too large" {
		t.Fatalf("expected width conversion error, got %v", err)
	}

	_, err = EncodeRGBAImage(0, 1<<32, nil)
	if err == nil || err.Error() != "clipboard image height is too large" {
		t.Fatalf("expected height conversion error, got %v", err)
	}
}
