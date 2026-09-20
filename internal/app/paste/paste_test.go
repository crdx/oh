package paste_test

import (
	"bytes"
	"strings"
	"testing"

	"crdx.org/io/internal/app/key"
	"crdx.org/io/internal/app/paste"
)

func opened(password string, location string) *key.ClipboardReport {
	return &key.ClipboardReport{
		Status:   key.ClipboardOpened,
		Password: password,
		Location: location,
	}
}

func chunk(mediaType string, payload string) *key.ClipboardReport {
	return &key.ClipboardReport{
		Status:    key.ClipboardChunk,
		MediaType: mediaType,
		Payload:   []byte(payload),
	}
}

func closed() *key.ClipboardReport {
	return &key.ClipboardReport{Status: key.ClipboardClosed}
}

func TestAPasteEventAsksForTheImageOverTheTextBesideIt(t *testing.T) {
	var exchange paste.Exchange

	exchange.Receive(opened("token", ""))
	result := exchange.Receive(chunk(key.MediaTypeList, "text/html text/plain image/png\n"))

	if result.Kind != paste.Requested {
		t.Fatalf("kind is %v, want a request", result.Kind)
	}

	want := "\x1b]5522;type=read:pw=dG9rZW4=:name=UGFzdGUgZXZlbnQ=;aW1hZ2UvcG5n\x1b\\"
	if result.Sequence != want {
		t.Errorf("sequence is %q, want %q", result.Sequence, want)
	}
}

func TestAPasteEventFromThePrimarySelectionIsReadBackFromThere(t *testing.T) {
	var exchange paste.Exchange

	exchange.Receive(opened("token", "primary"))
	result := exchange.Receive(chunk(key.MediaTypeList, "text/plain"))

	if !strings.Contains(result.Sequence, ":loc=primary:") {
		t.Errorf("sequence is %q, want it to read from the primary selection", result.Sequence)
	}
}

func TestAPasteEventWithoutATokenAsksWithoutOne(t *testing.T) {
	var exchange paste.Exchange

	exchange.Receive(opened("", ""))
	result := exchange.Receive(chunk(key.MediaTypeList, "text/plain"))

	want := "\x1b]5522;type=read;dGV4dC9wbGFpbg==\x1b\\"
	if result.Sequence != want {
		t.Errorf("sequence is %q, want %q", result.Sequence, want)
	}
}

func TestAPasteOfNothingUsefulAsksForNothing(t *testing.T) {
	var exchange paste.Exchange

	exchange.Receive(opened("token", ""))
	result := exchange.Receive(chunk(key.MediaTypeList, "application/x-nautilus-clipboard"))

	if result.Kind != paste.Ignored {
		t.Errorf("kind is %v, want it ignored", result.Kind)
	}
}

func TestTextIsSettledOnlyOnceTheTerminalHasFinishedSendingIt(t *testing.T) {
	var exchange paste.Exchange

	exchange.Receive(opened("", ""))
	if got := exchange.Receive(chunk("text/plain", "hello ")).Kind; got != paste.Ignored {
		t.Errorf("a chunk settled as %v, want it held", got)
	}
	if got := exchange.Receive(chunk("text/plain", "world")).Kind; got != paste.Ignored {
		t.Errorf("a chunk settled as %v, want it held", got)
	}

	result := exchange.Receive(closed())
	if result.Kind != paste.Text {
		t.Fatalf("kind is %v, want text", result.Kind)
	}
	if result.Text != "hello world" {
		t.Errorf("text is %q, want the chunks joined", result.Text)
	}
}

func TestAnImageIsSettledWithTheTypeItArrivedAs(t *testing.T) {
	var exchange paste.Exchange

	exchange.Receive(opened("", ""))
	exchange.Receive(chunk("image/webp", "RIFF"))
	result := exchange.Receive(closed())

	if result.Kind != paste.Image {
		t.Fatalf("kind is %v, want an image", result.Kind)
	}
	if result.MediaType != "image/webp" || string(result.Data) != "RIFF" {
		t.Errorf("got %q of %q, want the pasted WebP", result.MediaType, result.Data)
	}
}

func TestTheListingThatOpensAPasteIsNotMistakenForContent(t *testing.T) {
	var exchange paste.Exchange

	exchange.Receive(opened("token", ""))
	exchange.Receive(chunk(key.MediaTypeList, "image/png"))

	if got := exchange.Receive(closed()).Kind; got != paste.Ignored {
		t.Errorf("the notification settled as %v, want it ignored", got)
	}
}

func TestAnImageArrivesAsTheChunksTheTerminalSplitItInto(t *testing.T) {
	const chunkSize = 4096

	image := make([]byte, chunkSize*2+17)
	for i := range image {
		image[i] = byte(i)
	}

	var exchange paste.Exchange
	exchange.Receive(opened("", ""))

	for from := 0; from < len(image); from += chunkSize {
		report := &key.ClipboardReport{
			Status:    key.ClipboardChunk,
			MediaType: "image/png",
			Payload:   image[from:min(from+chunkSize, len(image))],
		}
		if got := exchange.Receive(report).Kind; got != paste.Ignored {
			t.Fatalf("a chunk at %d settled as %v, want it held", from, got)
		}
	}

	result := exchange.Receive(closed())
	if result.Kind != paste.Image {
		t.Fatalf("kind is %v, want an image", result.Kind)
	}
	if !bytes.Equal(result.Data, image) {
		t.Errorf("reassembled %d bytes, want the %d that were sent", len(result.Data), len(image))
	}
}

func TestAnOversizedPasteIsRefusedRatherThanHeldWhole(t *testing.T) {
	var exchange paste.Exchange

	exchange.Receive(opened("", ""))
	result := exchange.Receive(chunk("image/png", strings.Repeat("x", (32<<20)+1)))

	if result.Kind != paste.Failed {
		t.Fatalf("kind is %v, want a failure", result.Kind)
	}
	if !strings.Contains(result.Message, "32M") {
		t.Errorf("message is %q, want it to name the limit", result.Message)
	}
}

func TestARefusedClipboardIsExplainedRatherThanNamedByItsCode(t *testing.T) {
	for failure, want := range map[string]string{
		"EPERM":  "permission to read the clipboard was refused",
		"ENOSYS": "the terminal has no clipboard to read",
		"EBUSY":  "another window is using the clipboard",
		"EWEIRD": "the terminal refused the clipboard with EWEIRD",
	} {
		t.Run(failure, func(t *testing.T) {
			var exchange paste.Exchange

			result := exchange.Receive(&key.ClipboardReport{
				Status:  key.ClipboardFailed,
				Failure: failure,
			})

			if result.Kind != paste.Failed {
				t.Fatalf("kind is %v, want a failure", result.Kind)
			}
			if result.Message != want {
				t.Errorf("message is %q, want %q", result.Message, want)
			}
		})
	}
}

func TestAFailureLeavesNothingBehindForTheNextPaste(t *testing.T) {
	var exchange paste.Exchange

	exchange.Receive(opened("", ""))
	exchange.Receive(chunk("text/plain", "abandoned"))
	exchange.Receive(&key.ClipboardReport{Status: key.ClipboardFailed, Failure: "EBUSY"})

	if got := exchange.Receive(closed()).Kind; got != paste.Ignored {
		t.Errorf("the abandoned paste settled as %v, want it forgotten", got)
	}
}
