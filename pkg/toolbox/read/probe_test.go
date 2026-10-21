package read

import (
	"errors"
	"strings"
	"testing"
)

func TestTheStreamingProbeRefusesANulByteOnlyWithinItsFirst8000Bytes(t *testing.T) {
	for _, test := range []struct {
		offset    int
		isRefused bool
	}{
		{offset: 4095, isRefused: true},
		{offset: 4096, isRefused: true},
		{offset: 7999, isRefused: true},
		{offset: 8000, isRefused: false},
	} {
		line := []byte(strings.Repeat("a", 9000))
		line[test.offset] = 0
		root, name := fuzzRangeRoot(t, string(line)+"\nsecond\n")

		got, err := loadRange(t.Context(), root, name, Args{Path: name, Offset: 2, Limit: 1})
		if test.isRefused != errors.Is(err, errBinaryFile) {
			t.Errorf("a NUL byte at offset %d gave %v, want refused %t", test.offset, err, test.isRefused)
		}
		if !test.isRefused && got.output != "second" {
			t.Errorf("a NUL byte at offset %d gave %q, want the second line", test.offset, got.output)
		}
	}
}
