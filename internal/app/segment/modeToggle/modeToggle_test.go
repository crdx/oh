package modeToggle_test

import (
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/modeToggle"
	"crdx.org/oh/internal/app/style"
)

func TestEveryCapabilitySetKeepsTheSameLetters(t *testing.T) {
	for _, flags := range []string{"", "r", "rw", "rx", "rxw", "rxwn", "rxwng", "rxwngl", "rn", "rg", "rl"} {
		grantedCaps, err := caps.Parse(flags)
		if err != nil {
			t.Fatal(err)
		}

		if got := style.Plain(render(t, grantedCaps, false)); got != "rxw ngl" {
			t.Errorf("caps %q drew %q, want the letters to stand whatever is granted", flags, got)
		}
	}
}

func TestOnlyTheStylingSaysWhatIsGranted(t *testing.T) {
	refused := render(t, caps.Read, false)
	granted := render(t, caps.Read|caps.Write, false)

	if refused == granted {
		t.Errorf("granting the write capability drew nothing new: %q", granted)
	}

	pending := render(t, caps.Read, true)
	if pending == refused {
		t.Errorf("a pending prefix drew nothing new: %q", pending)
	}
	if got := style.Plain(pending); got != "rxw ngl" {
		t.Errorf("a pending prefix drew %q, want the same letters", got)
	}
}

func TestTheShellLetterFollowsWhateverItMayChange(t *testing.T) {
	for _, test := range []struct {
		flags string
		paint style.Style
	}{
		{"rx", style.Read},
		{"rxn", style.Read},
		{"rxl", style.Read},
		{"rxw", style.Write},
		{"rxg", style.Write},
		{"rxwg", style.Write},
	} {
		grantedCaps, err := caps.Parse(test.flags)
		if err != nil {
			t.Fatal(err)
		}

		if got := render(t, grantedCaps, false); !strings.Contains(got, test.paint("x")) {
			t.Errorf(
				"caps %q drew %q, want the shell letter painted %q",
				test.flags,
				got,
				test.paint("x"),
			)
		}
	}
}

type noOptions struct{}

func (noOptions) Read(any) error {
	return nil
}

func render(t *testing.T, grantedCaps caps.Set, isPrefixPending bool) string {
	t.Helper()

	built, err := modeToggle.New(
		func() caps.Set { return grantedCaps },
		func() bool { return isPrefixPending },
	)(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	return built.Render(segment.Context{})
}
