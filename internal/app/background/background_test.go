package background

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/style"
)

const deviceReply = "\x1b[?62;4c"

func TestABackgroundReplyIsReadAtEveryPrecisionATerminalAnswersIn(t *testing.T) {
	for reply, want := range map[string]style.Background{
		"\x1b]11;rgb:2424/2424/2424\x1b\\":     style.DarkBackground,
		"\x1b]11;rgb:ffff/ffff/ffff\x1b\\":     style.LightBackground,
		"\x1b]11;rgb:fd/f6/e3\x07":             style.LightBackground,
		"\x1b]11;rgb:0/2/3\x1b\\":              style.DarkBackground,
		"\x1b]11;rgb:f/f/f\x07":                style.LightBackground,
		"\x1b]11;rgba:fdfd/f6f6/e3e3/ffff\x07": style.LightBackground,
		"\x1b]11;rgb:FFFF/FFFF/FFFF\x1b\\":     style.LightBackground,
		"\x1b]11;rgb:1e1e/1e1e/2e2e\x1b\\":     style.DarkBackground,
		"\x1b]11;rgb:5050/5050/5050\x1b\\":     style.GreyDarkBackground,
		"\x1b]11;rgb:a0/a0/a0\x07":             style.GreyLightBackground,
	} {
		got, isKnown := Read([]string{reply, deviceReply})
		if !isKnown || got != want {
			t.Errorf("%q was read as %s (known: %t), want %s", reply, got, isKnown, want)
		}
	}
}

func TestATerminalThatOnlyAnswersTheDeviceQueryLeavesTheBackgroundUnknown(t *testing.T) {
	for _, replies := range [][]string{
		{deviceReply},
		nil,
		{"\x1b]11;?\x1b\\", deviceReply},
		{"\x1b]11;#ffffff\x07", deviceReply},
		{"\x1b]11;rgb:fffff/0/0\x07", deviceReply},
	} {
		if got, isKnown := Read(replies); isKnown || got != style.DarkBackground {
			t.Errorf("%q was read as %s (known: %t), want an unknown dark", replies, got, isKnown)
		}
	}
}

func TestTheReplyPatternFindsTheBackgroundWithEitherTerminator(t *testing.T) {
	arrival := "typed\x1b]11;rgb:ffff/ffff/ffff\x07more\x1b]11;rgb:0/0/0\x1b\\" + deviceReply
	if got := probeReply.FindAllString(arrival, -1); len(got) != 3 {
		t.Errorf("found %q, want both backgrounds and the device reply", got)
	}
}

func TestSomethingThatIsNotATerminalIsNeverAsked(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "screen"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	if got, isKnown := Detect(file, file); isKnown || got != style.DarkBackground {
		t.Errorf("got %s (known: %t), want an unknown dark", got, isKnown)
	}
	if written, _ := os.ReadFile(file.Name()); len(written) != 0 {
		t.Errorf("wrote %q to something that is not a terminal", written)
	}
}
