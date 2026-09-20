package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func builtinPath(directory string) string {
	return filepath.Join(directory, BuiltinName, filename)
}

func TestMaterialiseWritesTheBuiltinSkillWhereDiscoveryFindsIt(t *testing.T) {
	directory := t.TempDir()
	if err := Materialise(directory); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(builtinPath(directory))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(builtinSkill) {
		t.Error("the written skill is not the embedded one")
	}

	discoveredSkills, err := Discover(t.TempDir(), []string{directory}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(discoveredSkills) != 1 {
		t.Fatalf("got %d skills, want the built-in one: %#v", len(discoveredSkills), discoveredSkills)
	}
	if discoveredSkills[0].Name != BuiltinName || discoveredSkills[0].Description == "" {
		t.Errorf("got %#v", discoveredSkills[0])
	}
	if _, globalCount := Counts(discoveredSkills); globalCount != 1 {
		t.Errorf("the built-in skill is not global: %#v", discoveredSkills[0])
	}
}

func TestMaterialiseReplacesAnOlderBuiltinSkill(t *testing.T) {
	directory := t.TempDir()
	path := builtinPath(directory)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: oh\ndescription: An older skill.\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Materialise(directory); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(path) //nolint:gosec // the path is the test's own directory
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != string(builtinSkill) {
		t.Error("an older skill was left in place")
	}
}

func TestMaterialiseLeavesACurrentBuiltinSkillAlone(t *testing.T) {
	directory := t.TempDir()
	if err := Materialise(directory); err != nil {
		t.Fatal(err)
	}
	path := builtinPath(directory)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := Materialise(directory); err != nil {
		t.Fatal(err)
	}

	rewritten, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !rewritten.ModTime().Equal(info.ModTime()) {
		t.Error("a current skill was written again")
	}
}

func TestTheBuiltinSkillCanBeExcluded(t *testing.T) {
	directory := t.TempDir()
	if err := Materialise(directory); err != nil {
		t.Fatal(err)
	}
	discoveredSkills, err := Discover(t.TempDir(), []string{directory}, nil)
	if err != nil {
		t.Fatal(err)
	}

	kept := ExcludeGlobal(discoveredSkills, []string{filepath.Join(directory, BuiltinName)})
	if len(kept) != 0 {
		t.Errorf("got %#v, want the built-in skill excluded", kept)
	}
}
