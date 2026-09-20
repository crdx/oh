package messages

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/gif"
	"image/png"
	"testing"

	"crdx.org/io/internal/util/imageutil"
)

func TestStoredAnthropicImageIsBounded(t *testing.T) {
	stored := json.RawMessage(`{"source":{"type":"base64","media_type":"image/png","data":"` +
		base64.StdEncoding.EncodeToString(encodePNG(t, image.Rect(0, 0, 1600, 800))) + `"}}`)

	var history imageHistory
	bounded := history.prepare([]json.RawMessage{stored})[0]
	mediaType, data := getImageSource(t, bounded)
	width, height, isMeasured := imageutil.Dimensions(data)
	if mediaType != "image/png" || !isMeasured || width != imageutil.MaxEdge || height != imageutil.MaxEdge/2 {
		t.Errorf("got %s at %dx%d (%t), want image/png at %dx%d", mediaType, width, height, isMeasured, imageutil.MaxEdge, imageutil.MaxEdge/2)
	}
}

func TestStoredAnthropicGIFCarriesItsBoundedMediaType(t *testing.T) {
	var encoded bytes.Buffer
	if err := gif.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1600, 800)), nil); err != nil {
		t.Fatal(err)
	}
	stored := json.RawMessage(`{"source":{"type":"base64","media_type":"image/gif","data":"` +
		base64.StdEncoding.EncodeToString(encoded.Bytes()) + `"}}`)

	var history imageHistory
	bounded := history.prepare([]json.RawMessage{stored})[0]
	mediaType, data := getImageSource(t, bounded)
	width, _, isMeasured := imageutil.Dimensions(data)
	if mediaType != "image/png" || !isMeasured || width != imageutil.MaxEdge {
		t.Errorf("got %s at %d pixels (%t), want image/png at %d pixels", mediaType, width, isMeasured, imageutil.MaxEdge)
	}
}

func TestAnthropicHistoryWithoutAnOversizedImageIsLeftExactlyAsStored(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"zebra":1,"apple":2}`),
		json.RawMessage(`{"source":{"media_type":"image/png","data":"` +
			base64.StdEncoding.EncodeToString(encodePNG(t, image.Rect(0, 0, 64, 33))) + `"}}`),
		json.RawMessage(`{"arguments":{"media_type":"image/png","data":"` +
			base64.StdEncoding.EncodeToString(encodePNG(t, image.Rect(0, 0, 1600, 800))) + `"}}`),
		json.RawMessage(`not json at all`),
	}

	var history imageHistory
	for index, bounded := range history.prepare(items) {
		if !bytes.Equal(bounded, items[index]) {
			t.Errorf("item %d was rewritten:\n%s\n%s", index, bounded, items[index])
		}
	}
}

func TestAnthropicHistoryReusesPreparedItemsUntilReset(t *testing.T) {
	var history imageHistory
	items := []json.RawMessage{json.RawMessage(`{"source":{"type":"base64","media_type":"image/png","data":"` +
		base64.StdEncoding.EncodeToString(encodePNG(t, image.Rect(0, 0, 1600, 800))) + `"}}`)}

	prepared := history.prepare(items)
	if allocations := testing.AllocsPerRun(100, func() { history.prepare(items) }); allocations != 0 {
		t.Errorf("unchanged history allocated %.1f times per preparation", allocations)
	}

	firstPrepared := bytes.Clone(prepared[0])
	appended := json.RawMessage(`{"content":"appended"}`)
	items = append(items, appended)
	prepared = history.prepare(items)
	if len(prepared) != 2 || !bytes.Equal(prepared[0], firstPrepared) || !bytes.Equal(prepared[1], appended) {
		t.Errorf("appending rebuilt prepared history: %s", prepared)
	}

	replacement := []json.RawMessage{json.RawMessage(`{"content":"replacement"}`)}
	history.reset()
	prepared = history.prepare(replacement)
	if !bytes.Equal(prepared[0], replacement[0]) {
		t.Errorf("reset history retained the old item: %s", prepared[0])
	}
}

func encodePNG(t *testing.T, bounds image.Rectangle) []byte {
	t.Helper()

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(bounds)); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func getImageSource(t *testing.T, payload []byte) (string, []byte) {
	t.Helper()

	var stored struct {
		Source imageSource `json:"source"`
	}
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(stored.Source.Data)
	if err != nil {
		t.Fatal(err)
	}
	return stored.Source.MediaType, data
}
