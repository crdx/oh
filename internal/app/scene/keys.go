package scene

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

var namedKeys = map[string]string{
	"enter":        "\r",
	"alt+enter":    "\x1b\r",
	"tab":          "\t",
	"shift+tab":    "\x1b[Z",
	"escape":       "\x1b",
	"backspace":    "\x7f",
	"delete":       "\x1b[3~",
	"ctrl+delete":  "\x1b[3;5~",
	"up":           "\x1b[A",
	"down":         "\x1b[B",
	"right":        "\x1b[C",
	"left":         "\x1b[D",
	"ctrl+right":   "\x1b[1;5C",
	"ctrl+left":    "\x1b[1;5D",
	"home":         "\x1b[H",
	"end":          "\x1b[F",
	"ctrl+.":       kittyKey('.', controlModifier),
	"ctrl+shift+s": kittyKey('s', controlModifier+shiftModifier),
}

const (
	shiftModifier   = 1
	controlModifier = 4
)

func kittyKey(key rune, modifiers int) string {
	return fmt.Sprintf("\x1b[%d;%du", key, 1+modifiers)
}

func encodeKeys(keys []string) ([]string, error) {
	sequences := make([]string, 0, len(keys))

	for _, key := range keys {
		if !strings.HasPrefix(key, "<") || !strings.HasSuffix(key, ">") || len(key) < 3 {
			sequences = append(sequences, key)
			continue
		}

		name := strings.ToLower(key[1 : len(key)-1])
		if bytes, isNamed := namedKeys[name]; isNamed {
			sequences = append(sequences, bytes)
			continue
		}
		if letter, isControl := strings.CutPrefix(name, "ctrl+"); isControl && utf8.RuneCountInString(letter) == 1 {
			character := letter[0]
			if character >= 'a' && character <= 'z' {
				sequences = append(sequences, string(rune(character&0x1f)))
				continue
			}
		}

		return nil, fmt.Errorf("%w %s: name one of %s, or ctrl+ and a letter", errUnknownKey, key, keyNames())
	}

	return sequences, nil
}

func keyNames() string {
	names := make([]string, 0, len(namedKeys))
	for name := range namedKeys {
		names = append(names, "<"+name+">")
	}
	slices.Sort(names)

	return strings.Join(names, ", ")
}
