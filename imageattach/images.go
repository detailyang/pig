package imageattach

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/detailyang/pig/ai"
)

const MaxPerImageBytes = 10 * 1024 * 1024
const MaxImagesPerMessage = 10

const MAX_PER_IMAGE_BYTES = MaxPerImageBytes
const MAX_IMAGES_PER_MESSAGE = MaxImagesPerMessage

func InferMime(bytes []byte) (string, bool) {
	if len(bytes) >= 8 && string(bytes[:8]) == "\x89PNG\r\n\x1a\n" {
		return "image/png", true
	}
	if len(bytes) >= 3 && bytes[0] == 0xff && bytes[1] == 0xd8 && bytes[2] == 0xff {
		return "image/jpeg", true
	}
	if len(bytes) >= 12 && string(bytes[:4]) == "RIFF" && string(bytes[8:12]) == "WEBP" {
		return "image/webp", true
	}
	if len(bytes) >= 6 && (string(bytes[:6]) == "GIF87a" || string(bytes[:6]) == "GIF89a") {
		return "image/gif", true
	}
	return "", false
}

func LoadOne(path string) (ai.ContentBlock, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return ai.ContentBlock{}, fmt.Errorf("read image %s: %w", path, err)
	}
	return LoadBytes(path, bytes)
}

func LoadBytes(label string, bytes []byte) (ai.ContentBlock, error) {
	if len(bytes) > MaxPerImageBytes {
		return ai.ContentBlock{}, fmt.Errorf("image %s exceeds %dMB cap (%d bytes)", label, MaxPerImageBytes/1024/1024, len(bytes))
	}
	mime, ok := InferMime(bytes)
	if !ok {
		return ai.ContentBlock{}, fmt.Errorf("unsupported image format for %s; expected PNG/JPEG/WebP/GIF", label)
	}
	return ai.ContentBlock{Type: ai.ContentImage, MimeType: mime, Data: base64.StdEncoding.EncodeToString(bytes)}, nil
}

func LoadAll(paths []string) ([]ai.ContentBlock, error) {
	if len(paths) > MaxImagesPerMessage {
		return nil, fmt.Errorf("%d images exceeds per-message cap of %d", len(paths), MaxImagesPerMessage)
	}
	out := make([]ai.ContentBlock, 0, len(paths))
	for _, path := range paths {
		block, err := LoadOne(path)
		if err != nil {
			return nil, err
		}
		out = append(out, block)
	}
	return out, nil
}
