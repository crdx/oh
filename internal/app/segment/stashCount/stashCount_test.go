package stashCount_test

import (
	"testing"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/stashCount"
	"crdx.org/oh/internal/app/style"
)

type options struct{}

func (options) Read(any) error { return nil }

func render(t *testing.T, count int) string {
	t.Helper()

	instance, err := stashCount.New(func() int { return count })(options{})
	if err != nil {
		t.Fatal(err)
	}
	return style.Plain(instance.Render(segment.Context{}))
}

func TestAnEmptyStashDrawsNothing(t *testing.T) {
	if got := render(t, 0); got != "" {
		t.Errorf("got %q, want no segment", got)
	}
}

func TestAStashDrawsItsEmojiAndCount(t *testing.T) {
	for count, want := range map[int]string{
		1:  "🗃️ 1",
		12: "🗃️ 12",
	} {
		if got := render(t, count); got != want {
			t.Errorf("count %d drew %q, want %q", count, got, want)
		}
	}
}
