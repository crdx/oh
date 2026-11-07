package toolbox

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"crdx.org/oh/internal/file"
)

const fifoDeadline = 10 * time.Second

func TestFileToolsRefuseAFIFOWithoutWaitingForAWriter(t *testing.T) {
	root := testRoot(t, true)
	if err := syscall.Mkfifo(filepath.Join(root.Name(), "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := Rummage(root, file.NewSnapshots())

	for name, arguments := range map[string]string{
		"read":  `{"path":"pipe"}`,
		"write": `{"path":"pipe","content":"text"}`,
		"edit":  `{"path":"pipe","old_text":"old","new_text":"new"}`,
	} {
		call, err := toolNamed(t, tools, name).Parse(arguments)
		if err != nil {
			t.Fatalf("unexpected %s parse error: %v", name, err)
		}

		finished := make(chan error, 1)
		go func() {
			_, err := call.Exec(t.Context())
			finished <- err
		}()

		select {
		case err := <-finished:
			if !errors.Is(err, file.ErrSpecialFile) {
				t.Errorf("%s got %v, want a special file refusal", name, err)
			}
		case <-time.After(fifoDeadline):
			t.Fatalf("%s waited for a writer to open the FIFO", name)
		}
	}
}
