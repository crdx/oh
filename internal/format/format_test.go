package format_test

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/format"
)

func TestAFormatThisBuildWritesIsAccepted(t *testing.T) {
	if err := format.Check(2, 2); err != nil {
		t.Errorf("expected the current format to be read, got %v", err)
	}
	if err := format.Check(1, 2); err != nil {
		t.Errorf("expected an older format to be left to the caller, got %v", err)
	}
}

func TestANewerFormatIsNamedAsOne(t *testing.T) {
	err := format.Check(3, 2)
	if err == nil {
		t.Fatal("expected a newer format to be refused")
	}
	if !format.IsNewer(err) {
		t.Errorf("expected the error to be recognisable, got %v", err)
	}
	for _, wanted := range []string{"format 3", "newer build", "format 2"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("expected %q in %q", wanted, err)
		}
	}
}

func TestTheVersionIsReadWithoutTheRest(t *testing.T) {
	version, err := format.ReadJSON([]byte(`{"version":7,"model":{"round_robin":["a"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if version != 7 {
		t.Errorf("got JSON version %d, want 7", version)
	}

	version, err = format.ReadTOML([]byte("version = 7\n[model]\nround_robin = [\"a\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if version != 7 {
		t.Errorf("got TOML version %d, want 7", version)
	}
}

func TestAnUnnumberedDocumentReadsAsZero(t *testing.T) {
	version, err := format.ReadJSON([]byte(`{"model":"gpt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Errorf("got JSON version %d, want 0", version)
	}

	version, err = format.ReadTOML([]byte("model = \"gpt\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if version != 0 {
		t.Errorf("got TOML version %d, want 0", version)
	}
}
