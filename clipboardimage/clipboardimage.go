package clipboardimage

import "github.com/detailyang/pig/clipboard"

type ClipboardImage = clipboard.ClipboardImage
type ClipboardPaste = clipboard.ClipboardPaste
type Image = clipboard.Image
type Paste = clipboard.Paste
type PasteKind = clipboard.PasteKind

const PasteImage = clipboard.PasteImage
const PasteText = clipboard.PasteText
const PasteEmpty = clipboard.PasteEmpty

const ClipboardPasteText = clipboard.ClipboardPasteText
const ClipboardPasteEmpty = clipboard.ClipboardPasteEmpty

func EncodeRGBAImage(width int, height int, rgbaBytes []byte) (Image, error) {
	return clipboard.EncodeRGBAImage(width, height, rgbaBytes)
}

func EncodeRGBAClipboardImage(width int, height int, rgbaBytes []byte) (ClipboardImage, error) {
	return clipboard.EncodeRGBAClipboardImage(width, height, rgbaBytes)
}

func EncodeRgbaClipboardImage(width int, height int, rgbaBytes []byte) (ClipboardImage, error) {
	return clipboard.EncodeRgbaClipboardImage(width, height, rgbaBytes)
}

func ReadClipboard() (ClipboardPaste, error) {
	return clipboard.ReadClipboard()
}

func ReadClipboardSync() (ClipboardPaste, error) {
	return clipboard.ReadClipboardSync()
}
