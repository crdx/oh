package sandbox_test

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/sandbox"
)

func TestArgvRejectsCommandsThatWouldNeedAShell(t *testing.T) {
	for _, arguments := range [][]string{
		nil,
		{"printf", "hello"},
		{"/usr/bin/printf", "bad\x00argument"},
	} {
		if _, err := sandbox.DirectArgv().RunArgv(t.Context(), t.TempDir(), arguments, sandbox.Policy{}); err == nil {
			t.Errorf("accepted arguments %q", arguments)
		}
	}
}

func TestArgvRefusesUnconfinedExecution(t *testing.T) {
	_, err := sandbox.DirectArgv().RunArgv(
		t.Context(), t.TempDir(), []string{"/usr/bin/true"}, sandbox.Policy{Yolo: true},
	)
	if err == nil || !strings.Contains(err.Error(), "unconfined") {
		t.Errorf("got %v, want an unconfined refusal", err)
	}
}
