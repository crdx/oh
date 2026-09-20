package drops

import (
	"fmt"
)

var imageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

const imageName = "image"

func SaveImage(sessionDirectory string, ensureSession func() error, mediaType string, data []byte) (string, error) {
	extension, isKnown := imageExtensions[mediaType]
	if !isKnown {
		return "", fmt.Errorf("the clipboard holds an unsupported image type %q", mediaType)
	}

	return writeDrop(sessionDirectory, ensureSession, imageName, extension, data)
}
