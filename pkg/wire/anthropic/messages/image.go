package messages

import (
	"encoding/base64"
	"encoding/json"

	"crdx.org/io/internal/util/imageutil"
	"crdx.org/io/pkg/tool"
)

type imageHistory struct {
	preparedItems []json.RawMessage
}

func (self *imageHistory) prepare(items []json.RawMessage) []json.RawMessage {
	if len(items) < len(self.preparedItems) {
		self.reset()
	}

	for _, item := range items[len(self.preparedItems):] {
		self.preparedItems = append(self.preparedItems, boundJSONImages(item))
	}
	return self.preparedItems
}

func (self *imageHistory) reset() {
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
			if key == "source" {
				if source, isSource := item.(map[string]any); isSource && boundImageSource(source) {
					wasBounded = true
					continue
				}
			}
			boundedItem, wasItemBounded := boundValueImages(item)
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

func boundImageSource(source map[string]any) bool {
	mediaType, hasMediaType := source["media_type"].(string)
	encodedData, hasData := source["data"].(string)
	if !hasMediaType || !hasData || !imageutil.IsSupported(mediaType) {
		return false
	}

	boundedImage, wasBounded := boundEncodedImage(mediaType, encodedData)
	if !wasBounded {
		return false
	}

	source["media_type"] = boundedImage.MediaType
	source["data"] = base64.StdEncoding.EncodeToString(boundedImage.Data)

	return true
}

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
