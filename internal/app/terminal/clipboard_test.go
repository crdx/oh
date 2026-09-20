package terminal_test

import (
	"bytes"
	"testing"

	"crdx.org/io/internal/app/terminal"
)

func TestCopyWritesAnOSC52ClipboardSequence(t *testing.T) {
	var output bytes.Buffer

	if err := terminal.Copy(&output, "tame-impala"); err != nil {
		t.Fatal(err)
	}

	if got, want := output.String(), "\x1b]52;c;dGFtZS1pbXBhbGE=\x07"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
