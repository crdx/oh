package toolbox

import (
	"errors"
	"testing"

	"crdx.org/io/internal/file"
)

func TestAStoredReadSnapshotAllowsTheSameEditAfterResume(t *testing.T) {
	root := testRoot(t, true)
	writeTestFile(t, root, "one\n")

	initialTools := Rummage(root, file.NewSnapshots())
	readCall, err := toolNamed(t, initialTools, "read").Parse(`{"path":"a.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	readResult, err := readCall.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(readResult.State) == 0 {
		t.Fatal("expected the read to return durable state")
	}

	resumedTools := Rummage(root, file.NewSnapshots())
	if err := toolNamed(t, resumedTools, "read").Restore(readResult.State); err != nil {
		t.Fatal(err)
	}
	if err := runTool(
		t,
		toolNamed(t, resumedTools, "edit"),
		`{"path":"a.txt","old_text":"one","new_text":"two"}`,
	); err != nil {
		t.Fatalf("the restored read did not authorise the edit: %v", err)
	}
}

func TestAStoredReadSnapshotAllowsTheSameOverwriteAfterResume(t *testing.T) {
	root := testRoot(t, true)
	writeTestFile(t, root, "one\n")

	initialTools := Rummage(root, file.NewSnapshots())
	readCall, err := toolNamed(t, initialTools, "read").Parse(`{"path":"a.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	readResult, err := readCall.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	resumedTools := Rummage(root, file.NewSnapshots())
	if err := toolNamed(t, resumedTools, "read").Restore(readResult.State); err != nil {
		t.Fatal(err)
	}
	if err := runTool(
		t,
		toolNamed(t, resumedTools, "write"),
		`{"path":"a.txt","content":"two\n"}`,
	); err != nil {
		t.Fatalf("the restored read did not authorise the overwrite: %v", err)
	}
}

func TestAStoredReadSnapshotStillRefusesAFileChangedWhileStopped(t *testing.T) {
	root := testRoot(t, true)
	writeTestFile(t, root, "one\n")

	initialTools := Rummage(root, file.NewSnapshots())
	readCall, err := toolNamed(t, initialTools, "read").Parse(`{"path":"a.txt"}`)
	if err != nil {
		t.Fatal(err)
	}
	readResult, err := readCall.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, root, "changed\n")
	resumedTools := Rummage(root, file.NewSnapshots())
	if err := toolNamed(t, resumedTools, "read").Restore(readResult.State); err != nil {
		t.Fatal(err)
	}
	err = runTool(
		t,
		toolNamed(t, resumedTools, "edit"),
		`{"path":"a.txt","old_text":"changed","new_text":"two"}`,
	)
	if !errors.Is(err, file.ErrChangedSinceRead) {
		t.Errorf("expected the restored snapshot to detect the change, got %v", err)
	}
}
