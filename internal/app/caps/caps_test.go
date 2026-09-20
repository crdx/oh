package caps

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"crdx.org/oh/internal/file"
)

var (
	nowReadOnly     = workspaceNotice(false)
	nowReadWrite    = workspaceNotice(true)
	gitReadOnly     = repositoryNotice(false)
	gitWritable     = repositoryNotice(true)
	shellGranted    = shellNotice(true)
	shellWithheld   = shellNotice(false)
	lookupGranted   = lookupNotice(true)
	lookupWithheld  = lookupNotice(false)
	networkGranted  = hostNetworkNotice(true) + " " + fetchNotice(true)
	networkWithheld = hostNetworkNotice(false) + " " + fetchNotice(false)
)

func TestEveryClauseSaysSomethingAndSaysItBothWays(t *testing.T) {
	for name, clauses := range map[string][2]string{
		"workspace": {nowReadOnly, nowReadWrite},
		"history":   {gitReadOnly, gitWritable},
		"shell":     {shellWithheld, shellGranted},
		"lookup":    {lookupWithheld, lookupGranted},
		"network":   {networkWithheld, networkGranted},
	} {
		if clauses[0] == "" || clauses[1] == "" {
			t.Errorf("%s: expected a clause either way, got %q and %q", name, clauses[0], clauses[1])
		}

		if clauses[0] == clauses[1] {
			t.Errorf("%s: expected the two ways to read differently, both got %q", name, clauses[0])
		}
	}
}

func writable() Set { return Read | Write }

func TestAFreshConversationSaysNothingAboutTheModeItOpenedIn(t *testing.T) {
	for _, initialCaps := range []Set{Read, Read | Write, Read | Git, Read | Write | Git} {
		if got := NewMode(initialCaps).Inject(); got != "" {
			t.Errorf("expected nothing to be announced, got %q", got)
		}
	}
}

func TestASwappedModeIsAnnouncedOnceAndOnlyOnce(t *testing.T) {
	self := NewMode(writable())
	self.Toggle(Write)

	if got := self.Inject(); got != nowReadOnly {
		t.Errorf("expected %q, got %q", nowReadOnly, got)
	}

	if got := self.Inject(); got != "" {
		t.Errorf("expected nothing the second time, got %q", got)
	}

	self.Toggle(Write)

	if got := self.Inject(); got != nowReadWrite {
		t.Errorf("expected %q, got %q", nowReadWrite, got)
	}
}

func TestASwapIsAnnouncedOnceHoweverTheTurnCarryingItEnds(t *testing.T) {
	self := NewMode(writable())
	self.Toggle(Write)

	if got := self.Inject(); got != nowReadOnly {
		t.Fatalf("expected %q, got %q", nowReadOnly, got)
	}

	if got := self.Inject(); got != "" {
		t.Errorf("expected nothing more to be announced, got %q", got)
	}
}

func TestAnEarlierSwapIsNotRepeatedAlongsideALaterOne(t *testing.T) {
	self := NewMode(writable())

	self.Toggle(Write)
	_ = self.Inject()

	self.Toggle(Git)

	if got := self.Inject(); got != gitWritable {
		t.Errorf("expected only the later swap, got %q", got)
	}
}

func TestASwapBackIsStillAnnouncedAfterTheSwapBeforeIt(t *testing.T) {
	self := NewMode(Read)
	self.Toggle(Write)

	if got := self.Inject(); got != nowReadWrite {
		t.Fatalf("expected %q, got %q", nowReadWrite, got)
	}

	self.Toggle(Write)

	if got := self.Inject(); got != nowReadOnly {
		t.Errorf("expected the swap back to be announced, got %q", got)
	}
}

func TestAResumedConversationDoesNotReannounceItsRecordedMode(t *testing.T) {
	self := NewMode(writable())

	if got := self.Inject(); got != "" {
		t.Errorf("expected no change to be announced, got %q", got)
	}

	self.Toggle(Git)
	if got := self.Inject(); got != gitWritable {
		t.Errorf("expected only the new change to be announced, got %q", got)
	}
}

func TestOpeningTheHistoryIsAnnouncedOnItsOwn(t *testing.T) {
	self := NewMode(writable())
	self.Toggle(Git)

	if got := self.Inject(); got != gitWritable {
		t.Errorf("expected %q, got %q", gitWritable, got)
	}

	self.Toggle(Git)

	if got := self.Inject(); got != gitReadOnly {
		t.Errorf("expected %q, got %q", gitReadOnly, got)
	}
}

func TestBothSwapsBetweenTurnsAreAnnouncedTogether(t *testing.T) {
	self := NewMode(writable())

	self.Toggle(Write)
	self.Toggle(Git)

	if got := self.Inject(); got != nowReadOnly+" "+gitWritable {
		t.Errorf("expected both clauses, got %q", got)
	}
}

func TestAModeSwappedTwiceIsNotAnnounced(t *testing.T) {
	self := NewMode(writable())

	self.Toggle(Write)
	self.Toggle(Write)

	if got := self.Inject(); got != "" {
		t.Errorf("expected nothing to be announced, got %q", got)
	}
}

func TestSwitchingTheLookupToolsIsAnnounced(t *testing.T) {
	self := NewMode(Read)
	self.Toggle(Lookup)

	if got := self.Inject(); got != lookupGranted {
		t.Errorf("expected %q, got %q", lookupGranted, got)
	}

	self.Toggle(Lookup)

	if got := self.Inject(); got != lookupWithheld {
		t.Errorf("expected %q, got %q", lookupWithheld, got)
	}
}

func TestSwitchingTheShellIsAnnounced(t *testing.T) {
	self := NewMode(Read | Shell)
	self.Toggle(Shell)

	if got := self.Inject(); got != shellWithheld {
		t.Errorf("expected %q, got %q", shellWithheld, got)
	}

	self.Toggle(Shell)

	if got := self.Inject(); got != shellGranted {
		t.Errorf("expected %q, got %q", shellGranted, got)
	}
}

func TestSwitchingHostNetworkAccessIsAnnounced(t *testing.T) {
	self := NewMode(Read)
	self.Toggle(Network)

	if got := self.Inject(); got != networkGranted {
		t.Errorf("expected %q, got %q", networkGranted, got)
	}

	self.Toggle(Network)

	if got := self.Inject(); got != networkWithheld {
		t.Errorf("expected %q, got %q", networkWithheld, got)
	}
}

func TestEachToolAffectedByAChangeSaysSoOnItsOwn(t *testing.T) {
	notices, areSaid := Notice(Network, Read|Network)
	if !areSaid {
		t.Fatal("expected the network capability to say something")
	}

	want := []string{hostNetworkNotice(true), fetchNotice(true)}
	if !slices.Equal(notices, want) {
		t.Errorf("expected %q, got %q", want, notices)
	}

	for _, notice := range notices {
		if strings.Contains(notice, ", and") {
			t.Errorf("expected one fact to a notice, got %q", notice)
		}
	}
}

func TestSeveralCapabilitiesChangedAtOnceSayOneNoticeEach(t *testing.T) {
	notices, areSaid := Notice(Write|Git, Read)
	if !areSaid {
		t.Fatal("expected the changed capabilities to say something")
	}

	want := []string{workspaceNotice(false), repositoryNotice(false)}
	if !slices.Equal(notices, want) {
		t.Errorf("expected %q, got %q", want, notices)
	}
}

func TestAFlagNamesOneCapabilityWhileParsingItReadsAWholeMode(t *testing.T) {
	for flag := range strings.SplitSeq(AllFlags, "") {
		namedCaps, known := Named(flag)
		if !known {
			t.Fatalf("%s: expected the flag to name a capability", flag)
		}

		if got := namedCaps.Flag(); got != flag {
			t.Errorf("%s: expected the capability to name the flag back, got %q", flag, got)
		}

		if namedCaps != Read && namedCaps.Has(Read) {
			t.Errorf("%s: expected the flag to name one capability alone, got %q", flag, namedCaps.Flags())
		}

		parsedCaps, err := Parse(flag)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", flag, err)
		}

		if !parsedCaps.Has(Read) {
			t.Errorf("%s: expected a parsed mode to allow reading, got %q", flag, parsedCaps.Flags())
		}
	}

	if _, known := Named("z"); known {
		t.Error("expected an unknown flag to name nothing")
	}
}

func TestTheHistoryCountsAsChangingFilesJustAsWritingDoes(t *testing.T) {
	for flags, want := range map[string]bool{
		"":     false,
		"x":    false,
		"nl":   false,
		"w":    true,
		"g":    true,
		"xwg":  true,
		"xngl": true,
	} {
		parsedCaps, err := Parse(flags)
		if err != nil {
			t.Fatal(err)
		}

		if got := parsedCaps.CanChangeFiles(); got != want {
			t.Errorf("%q reported %t, want %t", parsedCaps.Flags(), got, want)
		}
	}
}

func TestTheModeSaysWhatTheWorkspaceAllows(t *testing.T) {
	self := NewMode(writable())

	if currentCaps := self.Current(); !currentCaps.Has(Write) || currentCaps.Has(Git) {
		t.Error("expected a mode opened writable to say so")
	}

	self.Toggle(Write)
	self.Toggle(Git)

	if currentCaps := self.Current(); currentCaps.Has(Write) || !currentCaps.Has(Git) {
		t.Error("expected a commit-only mode to say so")
	}
}

func TestTheRuleAnswersForBothHalvesOfTheMode(t *testing.T) {
	for name, want := range map[string]struct {
		currentCaps Set
		workspace   error
		history     error
	}{
		"nothing":     {Read, file.ErrReadOnly, file.ErrGitDir},
		"the lot":     {Read | Write | Git, nil, nil},
		"the tree":    {Read | Write, nil, file.ErrGitDir},
		"commit only": {Read | Git, file.ErrReadOnly, nil},
	} {
		refuse := RefuseWrite(NewMode(want.currentCaps))

		if got := refuse("main.go"); !errors.Is(got, want.workspace) {
			t.Errorf("%s: writing a file got %v, want %v", name, got, want.workspace)
		}

		if got := refuse(".git/config"); !errors.Is(got, want.history) {
			t.Errorf("%s: writing the metadata got %v, want %v", name, got, want.history)
		}
	}
}

func TestTheModeIsSafeToSwapWhileItIsBeingRead(t *testing.T) {
	self := NewMode(writable())

	var waiting sync.WaitGroup

	for range 100 {
		waiting.Add(3)

		go func() {
			defer waiting.Done()

			self.Toggle(Write)
		}()

		go func() {
			defer waiting.Done()

			self.Toggle(Git)
		}()

		go func() {
			defer waiting.Done()

			_ = self.Current()
		}()
	}

	waiting.Wait()

	if currentCaps := self.Current(); !currentCaps.Has(Write) || currentCaps.Has(Git) {
		t.Error("expected an even number of swaps to leave the mode where it started")
	}
}
