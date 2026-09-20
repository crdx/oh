package session

import (
	"slices"
	"strings"
	"testing"
)

func TestEveryAnimalIsGivenAnEmoji(t *testing.T) {
	for _, animal := range slices.Concat(characters.Animals, characters.RetiredAnimals) {
		if animal.Emoji == "" || strings.TrimFunc(animal.Emoji, func(character rune) bool { return character > 0x7f }) != "" {
			t.Errorf("%q is given %q, which is not an emoji", animal.Name, animal.Emoji)
		}
		if got := Emoji("able-" + animal.Name); got != animal.Emoji {
			t.Errorf("%q is stood for by %q, want %q", animal.Name, got, animal.Emoji)
		}
	}
}

func TestANameWithoutAKnownAnimalHasNoEmoji(t *testing.T) {
	for _, name := range []string{"otter", "brave-unicorn"} {
		if got := Emoji(name); got != "" {
			t.Errorf("%q is stood for by %q, want nothing", name, got)
		}
	}
}

func TestARetiredAnimalKeepsItsEmojiForTheSessionsNamedAfterIt(t *testing.T) {
	if len(characters.RetiredAnimals) == 0 {
		t.Fatal("expected the animals taken out of the naming to be kept")
	}

	for _, animal := range characters.RetiredAnimals {
		if got := Emoji("able-" + animal.Name); got != animal.Emoji {
			t.Errorf("%q is stood for by %q, want %q", animal.Name, got, animal.Emoji)
		}
		if slices.Contains(animals, animal.Name) {
			t.Errorf("%q is retired, so nothing should be named after it", animal.Name)
		}
	}
}
