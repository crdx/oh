package session

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func first(int) int { return 0 }

func TestAChildSharesItsParentsAdjectiveAndNoEmojiInItsFamily(t *testing.T) {
	var siblings []string
	emojis := []string{Emoji("tame-impala")}
	for range 5 {
		name, err := ChildName("tame-impala", siblings, first)
		if err != nil {
			t.Fatal(err)
		}
		if adjective, _, _ := strings.Cut(name, "-"); adjective != "tame" || name == "tame-impala" {
			t.Fatalf("%s is no child of tame-impala", name)
		}
		if slices.Contains(siblings, name) || slices.Contains(emojis, Emoji(name)) {
			t.Fatalf("%s repeats a name or emoji already in %v", name, siblings)
		}
		siblings = append(siblings, name)
		emojis = append(emojis, Emoji(name))
	}
}

func TestAChildFallsBackToARepeatedEmojiOnlyWhenNoOtherIsLeft(t *testing.T) {
	var distinct []string
	for _, animal := range animals {
		if candidate := "tame-" + animal; candidate != "tame-impala" {
			distinct = append(distinct, candidate)
		}
	}
	last := distinct[len(distinct)-1]
	name, err := ChildName("tame-impala", distinct[:len(distinct)-1], first)
	if err != nil || name != last {
		t.Fatalf("got %s, %v; want the last name left, %s", name, err, last)
	}
	if _, err := ChildName("tame-impala", distinct, first); !errors.Is(err, ErrNoChildName) {
		t.Errorf("a parent with every name taken named another child: %v", err)
	}
}

func TestAParentWithoutAnAdjectiveNamesNoChild(t *testing.T) {
	if _, err := ChildName("impala", nil, first); err == nil {
		t.Error("a child was named after a parent with no adjective")
	}
}

func TestANamedSessionIsCreatedUnderItsNameAndNeverTwice(t *testing.T) {
	directory := ChildrenDir(t.TempDir(), "tame-impala")
	if _, err := CreateNamed(directory, "not a name", nil, nil); err == nil {
		t.Error("a session was created under an invalid name")
	}
	writer, err := CreateNamed(directory, "tame-otter", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if writer.Name() != "tame-otter" {
		t.Errorf("the session was named %s", writer.Name())
	}
	if err := writer.EnsurePersisted(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if !Exists(directory, "tame-otter") || filepath.Base(filepath.Dir(Dir(directory, "tame-otter"))) != ChildrenDirectoryName {
		t.Errorf("the session is not stored beneath its parent")
	}
	if _, err := CreateNamed(directory, "tame-otter", nil, nil); err == nil {
		t.Error("a second session took the same name")
	}
}
