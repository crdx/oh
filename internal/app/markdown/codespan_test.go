package markdown

import "testing"

func TestCodeSpanUsesAFenceLongerThanItsText(t *testing.T) {
	for text, want := range map[string]string{
		"build":      "`build`",
		"build`fast": "`` build`fast ``",
		"build``now": "``` build``now ```",
	} {
		t.Run(text, func(t *testing.T) {
			if got := CodeSpan(text); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}
