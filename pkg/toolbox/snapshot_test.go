package toolbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/internal/file"
	"crdx.org/io/pkg/tool"
)

func toolNamed(t *testing.T, tools []tool.Tool, name string) tool.Tool {
	t.Helper()

	for _, candidateTool := range tools {
		if candidateTool.Name() == name {
			return candidateTool
		}
	}

	t.Fatalf("tool %s was not built", name)
	return nil
}

func runTool(t *testing.T, builtTool tool.Tool, arguments string) error {
	t.Helper()

	call, err := builtTool.Parse(arguments)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	result, err := call.Exec(t.Context())
	if err != nil || len(result.State) == 0 {
		return err
	}

	return builtTool.Restore(result.State)
}

func writeTestFile(t *testing.T, root *file.Root, content string) {
	t.Helper()

	path := filepath.Join(root.Name(), "a.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEditRequiresTheCurrentFileToHaveBeenRead(t *testing.T) {
	root := testRoot(t, true)
	writeTestFile(t, root, "one\n")
	tools := Rummage(root, file.NewSnapshots())
	readTool := toolNamed(t, tools, "read")
	editTool := toolNamed(t, tools, "edit")
	arguments := `{"path":"a.txt","old_text":"one","new_text":"two"}`

	if err := runTool(t, editTool, arguments); !errors.Is(err, file.ErrNotRead) {
		t.Errorf("expected an unread file to be refused, got %v", err)
	}
	if err := runTool(t, readTool, `{"path":"a.txt"}`); err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	writeTestFile(t, root, "changed\n")
	arguments = `{"path":"a.txt","old_text":"changed","new_text":"two"}`
	if err := runTool(t, editTool, arguments); !errors.Is(err, file.ErrChangedSinceRead) {
		t.Errorf("expected a changed file to be refused, got %v", err)
	}
	if err := runTool(t, readTool, `{"path":"a.txt"}`); err != nil {
		t.Fatalf("unexpected reread error: %v", err)
	}
	if err := runTool(t, editTool, arguments); err != nil {
		t.Fatalf("unexpected edit error: %v", err)
	}
}

func TestGrepRecordsTheFilesWhoseContentsItExposes(t *testing.T) {
	root := testRoot(t, true)
	writeTestFile(t, root, "one\n")
	tools := Rummage(root, file.NewSnapshots())

	if err := runTool(t, toolNamed(t, tools, "grep"), `{"pattern":"one","path":"a.txt"}`); err != nil {
		t.Fatalf("unexpected grep error: %v", err)
	}
	if err := runTool(
		t,
		toolNamed(t, tools, "edit"),
		`{"path":"a.txt","old_text":"one","new_text":"two"}`,
	); err != nil {
		t.Fatalf("the grep did not authorise the edit: %v", err)
	}
}

func TestWriteRequiresTheCurrentFileToHaveBeenReadBeforeOverwriting(t *testing.T) {
	root := testRoot(t, true)
	writeTestFile(t, root, "one\n")
	tools := Rummage(root, file.NewSnapshots())
	readTool := toolNamed(t, tools, "read")
	writeTool := toolNamed(t, tools, "write")
	arguments := `{"path":"a.txt","content":"two\n"}`

	if err := runTool(t, writeTool, arguments); !errors.Is(err, file.ErrNotRead) ||
		err.Error() != file.ErrNotRead.Error() {
		t.Errorf("expected an unread file to be refused, got %v", err)
	}
	if err := runTool(t, readTool, `{"path":"a.txt"}`); err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}

	writeTestFile(t, root, "changed\n")
	if err := runTool(t, writeTool, arguments); !errors.Is(err, file.ErrChangedSinceRead) ||
		err.Error() != "file changed; re-read it" {
		t.Errorf("expected a changed file to be refused, got %v", err)
	}
	if err := runTool(t, readTool, `{"path":"a.txt"}`); err != nil {
		t.Fatalf("unexpected reread error: %v", err)
	}
	if err := runTool(t, writeTool, arguments); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
}

func TestWriteCanCreateAFileThatDoesNotExist(t *testing.T) {
	root := testRoot(t, true)
	writeTool := toolNamed(t, Rummage(root, file.NewSnapshots()), "write")

	if err := runTool(t, writeTool, `{"path":"new.txt","content":"new\n"}`); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
}

func TestAnEditUpdatesTheReadSnapshot(t *testing.T) {
	root := testRoot(t, true)
	writeTestFile(t, root, "one\n")
	tools := Rummage(root, file.NewSnapshots())
	readTool := toolNamed(t, tools, "read")
	editTool := toolNamed(t, tools, "edit")

	if err := runTool(t, readTool, `{"path":"a.txt"}`); err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if err := runTool(t, editTool, `{"path":"a.txt","old_text":"one","new_text":"two"}`); err != nil {
		t.Fatalf("unexpected first edit error: %v", err)
	}
	if err := runTool(t, editTool, `{"path":"a.txt","old_text":"two","new_text":"three"}`); err != nil {
		t.Fatalf("unexpected second edit error: %v", err)
	}
}

func TestAWriteUpdatesTheReadSnapshot(t *testing.T) {
	root := testRoot(t, true)
	writeTestFile(t, root, "one\n")
	tools := Rummage(root, file.NewSnapshots())
	readTool := toolNamed(t, tools, "read")
	writeTool := toolNamed(t, tools, "write")

	if err := runTool(t, readTool, `{"path":"a.txt"}`); err != nil {
		t.Fatalf("unexpected read error: %v", err)
	}
	if err := runTool(t, writeTool, `{"path":"a.txt","content":"two\n"}`); err != nil {
		t.Fatalf("unexpected first write error: %v", err)
	}
	if err := runTool(t, writeTool, `{"path":"a.txt","content":"three\n"}`); err != nil {
		t.Fatalf("unexpected second write error: %v", err)
	}
}
