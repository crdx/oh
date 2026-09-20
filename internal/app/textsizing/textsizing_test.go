package textsizing

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOnlyBothPartsOfTheTextSizingProtocolCountAsSupport(t *testing.T) {
	for name, test := range map[string]struct {
		reply       string
		isSupported bool
	}{
		"both":          {"\x1b[4;1R\x1b[4;3R\x1b[4;5R", true},
		"leading noise": {"noise\x1b[4;1Rmore\x1b[4;3R\x1b[4;5R", true},
		"neither":       {"\x1b[4;1R\x1b[4;1R\x1b[4;1R", false},
		"width only":    {"\x1b[4;1R\x1b[4;3R\x1b[4;4R", false},
		"scale only":    {"\x1b[4;1R\x1b[4;1R\x1b[4;3R", false},
		"changed row":   {"\x1b[4;1R\x1b[5;3R\x1b[5;5R", false},
		"incomplete":    {"\x1b[4;1R\x1b[4;3R", false},
		"malformed":     {"\x1b[nopeR\x1b[4;3R\x1b[4;5R", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := supports(test.reply); got != test.isSupported {
				t.Errorf("supports(%q) = %t, want %t", test.reply, got, test.isSupported)
			}
		})
	}
}

func TestDetectionLeavesTheScreenBeforeWaitingForTheReply(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "1")
	master, slave := openPTY(t)

	written := make(chan string, 1)
	go func() {
		sent := make([]byte, len(beginProbe)+len(endProbe))
		if _, err := io.ReadFull(master, sent); err != nil {
			written <- "read probe: " + err.Error()
			return
		}
		if _, err := io.WriteString(master, "\x1b[4;1R\x1b[4;3R\x1b[4;5R"); err != nil {
			written <- "write replies: " + err.Error()
			return
		}
		written <- string(sent)
	}()

	if !Detect(slave, slave) {
		t.Error("terminal replies were not recognised")
	}

	select {
	case got := <-written:
		if got != beginProbe+endProbe {
			t.Errorf("terminal was sent %q, want probe and restore", got)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal simulator did not finish")
	}
}

func TestDetectionTimesOutAndRestoresAnUnsupportedKitty(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "1")
	master, slave := openPTY(t)

	written := make(chan string, 1)
	go func() {
		probe := make([]byte, len(beginProbe))
		_, probeError := io.ReadFull(master, probe)
		restore := make([]byte, len(endProbe))
		_, restoreError := io.ReadFull(master, restore)
		if probeError != nil || restoreError != nil {
			written <- "terminal simulator failed"
			return
		}
		written <- string(probe) + string(restore)
	}()

	if Detect(slave, slave) {
		t.Error("an unanswered query reported text sizing support")
	}

	select {
	case got := <-written:
		if got != beginProbe+endProbe {
			t.Errorf("terminal was sent %q, want probe and restore", got)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal simulator did not finish")
	}
}

func TestDetectionRefusesAnythingOutsideKitty(t *testing.T) {
	t.Setenv("KITTY_WINDOW_ID", "")
	input, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()

	if Detect(input, input) {
		t.Error("non-Kitty input was reported to support text sizing")
	}
}

func openPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal to test against: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })

	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })

	return master, slave
}

func FuzzReplies(fuzzer *testing.F) {
	for _, reply := range []string{
		"",
		"\x1b[4;1R\x1b[4;3R\x1b[4;5R",
		"\x1b[999999999999999999;1R",
		"noise\x1b[1;2R",
	} {
		fuzzer.Add(reply)
	}

	fuzzer.Fuzz(func(t *testing.T, reply string) {
		_ = supports(reply)
	})
}
