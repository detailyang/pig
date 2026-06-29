package images

import (
	"github.com/detailyang/pig/ai"
	"github.com/detailyang/pig/imageattach"
)

const MaxPerImageBytes = imageattach.MaxPerImageBytes
const MaxImagesPerMessage = imageattach.MaxImagesPerMessage

const MAX_PER_IMAGE_BYTES = imageattach.MAX_PER_IMAGE_BYTES
const MAX_IMAGES_PER_MESSAGE = imageattach.MAX_IMAGES_PER_MESSAGE
const MAXPERIMAGEBYTES = imageattach.MAX_PER_IMAGE_BYTES
const MAXIMAGESPERMESSAGE = imageattach.MAX_IMAGES_PER_MESSAGE

func InferMime(bytes []byte) (string, bool) {
	return imageattach.InferMime(bytes)
}

func LoadOne(path string) (ai.ContentBlock, error) {
	return imageattach.LoadOne(path)
}

func LoadBytes(label string, bytes []byte) (ai.ContentBlock, error) {
	return imageattach.LoadBytes(label, bytes)
}

func LoadAll(paths []string) ([]ai.ContentBlock, error) {
	return imageattach.LoadAll(paths)
}
