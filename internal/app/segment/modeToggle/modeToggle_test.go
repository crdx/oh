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
	for _, flags := range []string{"", "r", "rw", "rx", "rxw", "rxws", "rxwsn", "rxwsng", "rxwsngl", "rn", "rg", "rl", "rs"} {
		grantedCaps, err := caps.Parse(flags)
		if err != nil {
			t.Fatal(err)
		}

		if got := style.Plain(render(t, grantedCaps, false)); got != "rxw sngl" {
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
	if got := style.Plain(pending); got != "rxw sngl" {
		t.Errorf("a pending prefix drew %q, want the same letters", got)
	}
}

func TestCustomToolGroupsJoinTheSecondSection(t *testing.T) {
	built, err := modeToggle.New(
		func() caps.Set { return caps.Read },
		func() bool { return false },
		func() caps.GroupStatus { return caps.GroupStatus{Flags: "abc", GrantedFlags: "b"} },
	)(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if got := style.Plain(built.Render(segment.Context{})); got != "rxw snglabc" {
		t.Errorf("got %q", got)
	}
}

func TestAnUnconfinedSessionDrawsOnlyWhatItCanStillToggle(t *testing.T) {
	for _, test := range []struct {
		grantedCaps    caps.Set
		subagentsPaint style.Style
		lookupPaint    style.Style
	}{
		{caps.All() &^ caps.Lookup &^ caps.Subagents, style.Dim, style.Dim},
		{caps.All() &^ caps.Lookup, style.Subagents, style.Dim},
		{caps.All() &^ caps.Subagents, style.Dim, style.Lookup},
		{caps.All(), style.Subagents, style.Lookup},
	} {
		built, err := modeToggle.NewUnconfined(
			func() caps.Set { return test.grantedCaps },
			func() bool { return false },
			func() caps.GroupStatus { return caps.GroupStatus{Flags: "ab", GrantedFlags: "a"} },
		)(noOptions{})
		if err != nil {
			t.Fatal(err)
		}

		got := built.Render(segment.Context{})
		lead := test.subagentsPaint("s") + test.lookupPaint("l")
		if style.Plain(got) != "slab" || !strings.HasPrefix(got, lead) {
			t.Errorf("caps %q drew %q, want %q led by %q", test.grantedCaps.Flags(), got, "slab", lead)
		}
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
