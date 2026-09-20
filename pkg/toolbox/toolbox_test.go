package toolbox

import (
	"os"
	"slices"
	"testing"

	"crdx.org/io/internal/file"

	"crdx.org/io/pkg/tool"
)

func testRoot(t *testing.T, isWritable bool) *file.Root {
	t.Helper()

	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = root.Close() })

	return file.New(root, func(string) error {
		if isWritable {
			return nil
		}

		return file.ErrReadOnly
	})
}

func names(t *testing.T, isWritable bool) []string {
	t.Helper()

	var built []string

	for _, one := range Rummage(testRoot(t, isWritable), file.NewSnapshots()) {
		built = append(built, one.Name())
	}

	return built
}

func TestEveryPathShowingToolFocusesItsLastComponent(t *testing.T) {
	want := map[string]struct {
		arguments string
		focus     string
	}{
		"read":  {`{"path":"cmd/oh/draw.go"}`, "draw.go"},
		"ls":    {`{"path":"cmd/oh"}`, "oh"},
		"find":  {`{"pattern":"*.go","path":"cmd/oh"}`, "oh"},
		"write": {`{"path":"cmd/oh/new.go","content":""}`, "new.go"},
		"edit":  {`{"path":"cmd/oh/draw.go","old_text":"a","new_text":"b"}`, "draw.go"},
	}

	for _, subject := range Rummage(testRoot(t, true), file.NewSnapshots()) {
		test, ok := want[subject.Name()]
		if !ok {
			continue
		}

		call, err := subject.Parse(test.arguments)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", subject.Name(), err)
			continue
		}

		want := tool.Emphasis{Kind: tool.EmphasisFocus, Value: test.focus}
		if call.Emphasis() != want {
			t.Errorf("%s: expected %q to be focused, got %T", subject.Name(), test.focus, call)
		}
	}
}

func TestEveryToolIsBuiltByDefault(t *testing.T) {
	built := names(t, true)

	for _, name := range []string{"read", "ls", "find", "grep", "write", "edit"} {
		if !slices.Contains(built, name) {
			t.Errorf("expected %s, got %v", name, built)
		}
	}
}

func TestEveryToolIsBuiltEvenOverATreeThatCannotBeChanged(t *testing.T) {
	built := names(t, false)

	for _, name := range []string{"read", "ls", "find", "grep", "write", "edit"} {
		if !slices.Contains(built, name) {
			t.Errorf("expected %s, got %v", name, built)
		}
	}
}
