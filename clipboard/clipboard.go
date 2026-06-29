package clipboard

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os/exec"
	"runtime"

	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/imageattach"
)

const maxInt = int(^uint(0) >> 1)

type Image struct {
	Image        ai.ContentBlock
	Width        int
	Height       int
	EncodedBytes int
}

type ClipboardImage = Image

type PasteKind string

const (
	PasteImage PasteKind = "image"
	PasteText  PasteKind = "text"
	PasteEmpty PasteKind = "empty"

	ClipboardPasteText  = PasteText
	ClipboardPasteEmpty = PasteEmpty
)

type Paste struct {
	Kind  PasteKind
	Image *Image
	Text  string
}

type ClipboardPaste = Paste

func EncodeRGBAImage(width int, height int, rgbaBytes []byte) (Image, error) {
	expected, err := expectedRGBALen(width, height)
	if err != nil {
		return Image{}, err
	}
	if len(rgbaBytes) != expected {
		return Image{}, fmt.Errorf("clipboard image has invalid RGBA buffer: expected %d bytes, got %d", expected, len(rgbaBytes))
	}
	if width > math.MaxUint32 {
		return Image{}, fmt.Errorf("clipboard image width is too large")
	}
	if height > math.MaxUint32 {
		return Image{}, fmt.Errorf("clipboard image height is too large")
	}
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	copy(rgba.Pix, rgbaBytes)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, rgba); err != nil {
		return Image{}, fmt.Errorf("encode clipboard image as PNG: %w", err)
	}
	pngBytes := buffer.Bytes()
	block, err := imageattach.LoadBytes("clipboard image", pngBytes)
	if err != nil {
		return Image{}, err
	}
	return Image{Image: block, Width: width, Height: height, EncodedBytes: len(pngBytes)}, nil
}

func EncodeRGBAClipboardImage(width int, height int, rgbaBytes []byte) (ClipboardImage, error) {
	return EncodeRGBAImage(width, height, rgbaBytes)
}

func EncodeRgbaClipboardImage(width int, height int, rgbaBytes []byte) (ClipboardImage, error) {
	return EncodeRGBAClipboardImage(width, height, rgbaBytes)
}

func ReadClipboard() (ClipboardPaste, error) {
	return ReadClipboardSync()
}

func ReadClipboardSync() (ClipboardPaste, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("pbpaste").Output()
		if err != nil {
			return ClipboardPaste{}, fmt.Errorf("read clipboard text: %w", err)
		}
		return clipboardPasteFromText(string(out)), nil
	}
	return ClipboardPaste{}, fmt.Errorf("clipboard reading is not implemented on %s", runtime.GOOS)
}

func clipboardPasteFromText(text string) ClipboardPaste {
	if text == "" {
		return ClipboardPaste{Kind: PasteEmpty}
	}
	return ClipboardPaste{Kind: PasteText, Text: text}
}

func read_clipboard() (ClipboardPaste, error) {
	return ReadClipboard()
}

func read_clipboard_sync() (ClipboardPaste, error) {
	return ReadClipboardSync()
}

func expectedRGBALen(width int, height int) (int, error) {
	if width < 0 || height < 0 {
		return 0, fmt.Errorf("clipboard image dimensions are too large")
	}
	if width != 0 && height > maxInt/width {
		return 0, fmt.Errorf("clipboard image dimensions are too large")
	}
	pixels := width * height
	if pixels != 0 && pixels > maxInt/4 {
		return 0, fmt.Errorf("clipboard image dimensions are too large")
	}
	return pixels * 4, nil
}
