package command_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/pkg/tool/command"
)

var updateGoldens = flag.Bool("update", false, "write command output summaries back to the golden file")

func TestGoldenAnOversizedCommandNamesItsOmittedOutput(t *testing.T) {
	subject, err := command.New(command.Declaration{
		Name:        "large",
		Description: "emit a large foreground result",
		Command:     []string{writeScript(t, `printf '%1048576s' ' ' | tr ' ' x; printf tail`)},
	}, command.Options{})
	if err != nil {
		t.Fatal(err)
	}
	output, err := call(t, subject, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	notice, retained, found := strings.Cut(output, "\n")
	if !found {
		t.Fatalf("bounded output has no notice: %q", output)
	}
	if !strings.HasSuffix(retained, "tail") {
		t.Fatal("bounded output did not retain the command's tail")
	}
	body := strings.TrimSuffix(retained, "tail")
	if body != strings.Repeat("x", len(body)) {
		t.Fatal("bounded output changed the retained command bytes")
	}
	golden := fmt.Sprintf("%s\nretained: %d bytes\nsuffix: %s\n", notice, len(retained), retained[len(retained)-len("tail"):])
	path := filepath.Join("testdata", "oversized-output.txt")
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(golden), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if golden != string(want) {
		t.Errorf("oversized output differs from %s\n--- got ---\n%s--- want ---\n%s", path, golden, want)
	}
}
