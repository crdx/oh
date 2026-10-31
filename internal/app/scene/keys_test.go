package scene

import (
	"errors"
	"slices"
	"testing"
)

func TestKeysAreTypedAsTextUnlessNamed(t *testing.T) {
	got, err := encodeKeys([]string{"/co", "<tab>", "<Enter>", "<ctrl+s>", "<ctrl+shift+s>", "<", "<>"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"/co", "\t", "\r", "\x13", "\x1b[115;6u", "<", "<>"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnUnknownKeyIsRefused(t *testing.T) {
	for _, key := range []string{"<hyper+x>", "<ctrl+1>", "<ctrl+ab>"} {
		if _, err := encodeKeys([]string{key}); !errors.Is(err, errUnknownKey) {
			t.Errorf("%s gave %v", key, err)
		}
	}
}
