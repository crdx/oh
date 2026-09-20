package key_test

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/key"
)

func decodeOne(t *testing.T, input string) key.Key {
	t.Helper()

	keypress, err := key.NewDecoder(bufio.NewReader(strings.NewReader(input))).Next()
	if err != nil {
		t.Fatal(err)
	}

	return keypress
}

func TestAPasteNotificationIsDecodedWithItsTypesAndToken(t *testing.T) {
	keypress := decodeOne(t,
		"\x1b]5522;type=read:status=DATA:mime=Lg==:pw=dG9rZW4=;aW1hZ2UvcG5nCg==\x1b\\",
	)

	if keypress.Code != key.Clipboard {
		t.Fatalf("code is %v, want a clipboard report", keypress.Code)
	}

	report := keypress.Clipboard
	if report.Status != key.ClipboardChunk {
		t.Errorf("status is %v, want a chunk", report.Status)
	}
	if report.MediaType != key.MediaTypeList {
		t.Errorf("media type is %q, want the type listing", report.MediaType)
	}
	if report.Password != "token" {
		t.Errorf("password is %q, want %q", report.Password, "token")
	}
	if got, want := string(report.Payload), "image/png\n"; got != want {
		t.Errorf("payload is %q, want %q", got, want)
	}
}

func TestThePasteEventOfARealTerminalIsDecoded(t *testing.T) {
	captured := "\x1b]5522;type=read:status=OK:pw=UHRHUGhvY3ljdXFrTlNZcVJsODlyNA==\x1b\\" +
		"\x1b]5522;type=read:status=DATA:mime=Lg==:pw=UHRHUGhvY3ljdXFrTlNZcVJsODlyNA==;aW1hZ2UvcG5nCg==\x1b\\" +
		"\x1b]5522;type=read:status=DONE:pw=UHRHUGhvY3ljdXFrTlNZcVJsODlyNA==\x1b\\"

	decoder := key.NewDecoder(bufio.NewReader(strings.NewReader(captured)))

	wantStatuses := []key.ClipboardStatus{key.ClipboardOpened, key.ClipboardChunk, key.ClipboardClosed}
	for i, wantStatus := range wantStatuses {
		keypress, err := decoder.Next()
		if err != nil {
			t.Fatal(err)
		}
		if keypress.Code != key.Clipboard {
			t.Fatalf("packet %d is %v, want a clipboard report", i, keypress.Code)
		}
		if keypress.Clipboard.Status != wantStatus {
			t.Errorf("packet %d has status %v, want %v", i, keypress.Clipboard.Status, wantStatus)
		}
		if got := keypress.Clipboard.Password; got != "PtGPhocycuqkNSYqRl89r4" {
			t.Errorf("packet %d carries the token %q, want the captured one", i, got)
		}
	}
}

func TestAClipboardReportIsDecodedWhicheverTerminatorItCarries(t *testing.T) {
	for name, input := range map[string]string{
		"string terminator": "\x1b]5522;type=read:status=OK\x1b\\",
		"bell":              "\x1b]5522;type=read:status=OK\x07",
	} {
		t.Run(name, func(t *testing.T) {
			keypress := decodeOne(t, input)
			if keypress.Code != key.Clipboard {
				t.Fatalf("code is %v, want a clipboard report", keypress.Code)
			}
			if keypress.Clipboard.Status != key.ClipboardOpened {
				t.Errorf("status is %v, want opened", keypress.Clipboard.Status)
			}
		})
	}
}

func TestAPasteFromThePrimarySelectionKeepsItsLocation(t *testing.T) {
	keypress := decodeOne(t, "\x1b]5522;type=read:status=OK:loc=primary:pw=dG9rZW4=\x1b\\")

	if got := keypress.Clipboard.Location; got != "primary" {
		t.Errorf("location is %q, want %q", got, "primary")
	}
}

func TestARefusedClipboardCarriesTheCodeTheTerminalGave(t *testing.T) {
	keypress := decodeOne(t, "\x1b]5522;type=read:status=EPERM\x1b\\")

	if keypress.Clipboard.Status != key.ClipboardFailed {
		t.Fatalf("status is %v, want failed", keypress.Clipboard.Status)
	}
	if got := keypress.Clipboard.Failure; got != "EPERM" {
		t.Errorf("failure is %q, want %q", got, "EPERM")
	}
}

func TestEachChunkIsPaddedOnItsOwnAndStillJoinsUpWholeAgain(t *testing.T) {
	const chunkSize = 4096

	image := make([]byte, chunkSize+1)
	for i := range image {
		image[i] = byte(i)
	}

	var stream strings.Builder
	for from := 0; from < len(image); from += chunkSize {
		chunk := image[from:min(from+chunkSize, len(image))]
		stream.WriteString("\x1b]5522;type=read:status=DATA:mime=aW1hZ2UvcG5n;")
		stream.WriteString(base64.StdEncoding.EncodeToString(chunk))
		stream.WriteString("\x1b\\")
	}

	decoder := key.NewDecoder(bufio.NewReader(strings.NewReader(stream.String())))

	var joined []byte
	for range 2 {
		keypress, err := decoder.Next()
		if err != nil {
			t.Fatal(err)
		}
		joined = append(joined, keypress.Clipboard.Payload...)
	}

	if !bytes.Equal(joined, image) {
		t.Errorf("joined %d bytes, want the %d that were sent", len(joined), len(image))
	}
}

func TestAnUninterestingOperatingSystemCommandIsSwallowedRatherThanTyped(t *testing.T) {
	for name, input := range map[string]string{
		"a window title":     "\x1b]0;a title\x1b\\",
		"an OSC 52 response": "\x1b]52;c;dGV4dA==\x07",
		"a clipboard write":  "\x1b]5522;type=write:status=DONE\x1b\\",
	} {
		t.Run(name, func(t *testing.T) {
			if got := decodeOne(t, input); got.Code != key.Unknown {
				t.Errorf("got %+v, want it swallowed as unknown", got)
			}
		})
	}
}

func TestAnOverlongOperatingSystemCommandIsRefusedRatherThanHeld(t *testing.T) {
	input := "\x1b]5522;type=read:status=DATA:mime=Lg==;" + strings.Repeat("A", 1<<17) + "\x1b\\"

	if got := decodeOne(t, input); got.Code != key.Unknown {
		t.Errorf("got %+v, want it refused as unknown", got)
	}
}

func TestTheKeyboardAsksForPasteEventsAndGivesThemBack(t *testing.T) {
	if !strings.Contains(key.Enable, "\x1b[?5522h") {
		t.Error("enabling the keyboard does not ask for paste events")
	}
	if !strings.Contains(key.Disable, "\x1b[?5522l") {
		t.Error("disabling the keyboard does not give paste events back")
	}
	if !strings.Contains(key.Enable, "\x1b[?2004h") {
		t.Error("bracketed paste is no longer asked for as the fallback")
	}
}
