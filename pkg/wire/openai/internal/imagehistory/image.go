package imagehistory

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"crdx.org/io/internal/util/imageutil"
	"crdx.org/io/pkg/tool"
)

type Cache struct {
	preparedItems []json.RawMessage
}

func (self *Cache) Prepare(items []json.RawMessage) []json.RawMessage {
	if len(items) < len(self.preparedItems) {
		self.Reset()
	}

	for _, item := range items[len(self.preparedItems):] {
		self.preparedItems = append(self.preparedItems, boundJSONImages(item))
	}
	return self.preparedItems
}

func (self *Cache) Reset() {
	self.preparedItems = nil
}

func boundJSONImages(payload []byte) []byte {
	var value any
	if json.Unmarshal(payload, &value) != nil {
		return payload
	}

	boundedValue, wasBounded := boundValueImages(value)
	if !wasBounded {
		return payload
	}

	encodedPayload, err := json.Marshal(boundedValue)
	if err != nil {
		return payload
	}

	return encodedPayload
}

func boundValueImages(value any) (any, bool) {
	switch typedValue := value.(type) {
	case map[string]any:
		wasBounded := false
		for key, item := range typedValue {
			var boundedItem any
			var wasItemBounded bool
			if key == "image_url" {
				boundedItem, wasItemBounded = boundImageURL(item)
			} else {
				boundedItem, wasItemBounded = boundValueImages(item)
			}
			typedValue[key] = boundedItem
			wasBounded = wasBounded || wasItemBounded
		}
		return typedValue, wasBounded
	case []any:
		wasBounded := false
		for index, item := range typedValue {
			boundedItem, wasItemBounded := boundValueImages(item)
			typedValue[index] = boundedItem
			wasBounded = wasBounded || wasItemBounded
		}
		return typedValue, wasBounded
	}

	return value, false
}

func boundImageURL(value any) (any, bool) {
	switch typedValue := value.(type) {
	case string:
		boundedURL, wasBounded := boundDataURL(typedValue)
		if wasBounded {
			return boundedURL, true
		}
	case map[string]any:
		dataURL, hasURL := typedValue["url"].(string)
		if !hasURL {
			return value, false
		}
		boundedURL, wasBounded := boundDataURL(dataURL)
		if wasBounded {
			typedValue["url"] = boundedURL
			return typedValue, true
		}
	}
	return value, false
}

func boundDataURL(dataURL string) (string, bool) {
	mediaType, encodedData, isDataURL := strings.Cut(dataURL, dataURLSeparator)
	if !isDataURL {
		return "", false
	}

	mediaType = strings.TrimPrefix(mediaType, dataURLPrefix)
	if !imageutil.IsSupported(mediaType) {
		return "", false
	}

	boundedImage, wasBounded := boundEncodedImage(mediaType, encodedData)
	if !wasBounded {
		return "", false
	}

	return dataURLPrefix + boundedImage.MediaType + dataURLSeparator +
		base64.StdEncoding.EncodeToString(boundedImage.Data), true
}

const (
	dataURLPrefix    = "data:"
	dataURLSeparator = ";base64,"
)

func boundEncodedImage(mediaType string, encodedData string) (tool.Image, bool) {
	data, err := base64.StdEncoding.DecodeString(encodedData)
	if err != nil {
		return tool.Image{}, false
	}

	subject := tool.Image{MediaType: mediaType, Data: data}
	boundedImage := imageutil.Bound(subject)
	if boundedImage.MediaType == subject.MediaType && len(boundedImage.Data) == len(subject.Data) {
		return tool.Image{}, false
	}

	return boundedImage, true
}
