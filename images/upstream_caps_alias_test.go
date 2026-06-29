package images

import "testing"

func TestUpstreamCapsAliases(t *testing.T) {
	if MAXPERIMAGEBYTES != MAX_PER_IMAGE_BYTES {
		t.Fatalf("max image bytes alias mismatch")
	}
	if MAXPERIMAGEBYTES != MaxPerImageBytes {
		t.Fatalf("max image bytes alias mismatch")
	}
	if MAXIMAGESPERMESSAGE != MAX_IMAGES_PER_MESSAGE {
		t.Fatalf("max images alias mismatch")
	}
	if MAXIMAGESPERMESSAGE != MaxImagesPerMessage {
		t.Fatalf("max images alias mismatch")
	}
}
