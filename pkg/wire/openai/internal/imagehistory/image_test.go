package imagehistory_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"crdx.org/oh/internal/util/imageutil"
	"crdx.org/oh/pkg/wire/openai/internal/imagehistory"
)

func TestStoredOpenAIImagesAreBounded(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(oversizedPNG(t))
	tests := map[string]string{
		"string": `{"image_url":"data:image/png;base64,` + encoded + `"}`,
		"object": `{"image_url":{"url":"data:image/png;base64,` + encoded + `"}}`,
	}

	for name, stored := range tests {
		t.Run(name, func(t *testing.T) {
			var history imagehistory.Cache
			bounded := history.Prepare([]json.RawMessage{json.RawMessage(stored)})[0]
			dataURL := getDataURL(t, bounded)
			_, encoded, isDataURL := strings.Cut(dataURL, ";base64,")
			if !isDataURL {
				t.Fatalf("image is not a data URL: %s", dataURL)
			}
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			width, height, isMeasured := imageutil.Dimensions(data)
			if !isMeasured || width != imageutil.MaxEdge || height != imageutil.MaxEdge/2 {
				t.Errorf("got %dx%d (%t), want %dx%d", width, height, isMeasured, imageutil.MaxEdge, imageutil.MaxEdge/2)
			}
		})
	}
}

func TestOpenAIHistoryWithoutAnOversizedImageIsLeftExactlyAsStored(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"zebra":1,"apple":2}`),
		json.RawMessage(`{"image_url":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(smallPNG(t)) + `"}`),
		json.RawMessage(`{"content":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(oversizedPNG(t)) + `"}`),
		json.RawMessage(`not json at all`),
	}

	var history imagehistory.Cache
	for index, bounded := range history.Prepare(items) {
		if !bytes.Equal(bounded, items[index]) {
			t.Errorf("item %d was rewritten:\n%s\n%s", index, bounded, items[index])
		}
	}
}

func TestOpenAIHistoryReusesPreparedItemsUntilReset(t *testing.T) {
	var history imagehistory.Cache
	items := []json.RawMessage{json.RawMessage(`{"image_url":"data:image/png;base64,` +
		base64.StdEncoding.EncodeToString(oversizedPNG(t)) + `"}`)}

	prepared := history.Prepare(items)
	if allocations := testing.AllocsPerRun(100, func() { history.Prepare(items) }); allocations != 0 {
		t.Errorf("unchanged history allocated %.1f times per preparation", allocations)
	}

	firstPrepared := bytes.Clone(prepared[0])
	appended := json.RawMessage(`{"content":"appended"}`)
	items = append(items, appended)
	prepared = history.Prepare(items)
	if len(prepared) != 2 || !bytes.Equal(prepared[0], firstPrepared) || !bytes.Equal(prepared[1], appended) {
		t.Errorf("appending rebuilt prepared history: %s", prepared)
	}

	replacement := []json.RawMessage{json.RawMessage(`{"content":"replacement"}`)}
	history.Reset()
	prepared = history.Prepare(replacement)
	if !bytes.Equal(prepared[0], replacement[0]) {
		t.Errorf("reset history retained the old item: %s", prepared[0])
	}
}

func oversizedPNG(t *testing.T) []byte {
	t.Helper()
	return encodePNG(t, image.Rect(0, 0, 1600, 800))
}

func smallPNG(t *testing.T) []byte {
	t.Helper()
	return encodePNG(t, image.Rect(0, 0, 64, 33))
}

func encodePNG(t *testing.T, bounds image.Rectangle) []byte {
	t.Helper()

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(bounds)); err != nil {
		t.Fatal(err)
	}

	return encoded.Bytes()
}

func getDataURL(t *testing.T, payload []byte) string {
	t.Helper()

	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	if dataURL, isFound := findDataURL(value); isFound {
		return dataURL
	}
	t.Fatalf("no data URL in %s", payload)
	return ""
}

func findDataURL(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for _, item := range typed {
			if dataURL, isFound := findDataURL(item); isFound {
				return dataURL, true
			}
		}
	case []any:
		for _, item := range typed {
			if dataURL, isFound := findDataURL(item); isFound {
				return dataURL, true
			}
		}
	case string:
		return typed, strings.HasPrefix(typed, "data:image/")
	}
	return "", false
}
