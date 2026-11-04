package approvals

import (
	"errors"
	"slices"
	"testing"
	"time"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/pkg/question"
)

var startedAt = time.Date(2026, time.November, 3, 12, 0, 0, 0, time.UTC)

type noOptions struct{}

func (noOptions) Read(into any) error {
	if _, ok := into.(*struct{}); !ok {
		return errors.New("unexpected options type")
	}
	return nil
}

type refusingOptions struct{}

func (refusingOptions) Read(any) error {
	return errors.New("unknown option")
}

type clock struct {
	at time.Time
}

func (self *clock) now() time.Time {
	return self.at
}

func buildSegment(t *testing.T, approvals *[]question.RememberedAnswer, now *clock) state {
	t.Helper()

	built, err := New(func() []question.RememberedAnswer { return *approvals }, now.now)(noOptions{})
	if err != nil {
		t.Fatal(err)
	}
	builtState, isState := built.(state)
	if !isState {
		t.Fatalf("got %T", built)
	}
	return builtState
}

func timed(key string, lifetime time.Duration) question.RememberedAnswer {
	return question.RememberedAnswer{
		Key:       key,
		Option:    question.Option{Label: "Allow for a while", RememberFor: lifetime},
		ExpiresAt: startedAt.Add(lifetime),
	}
}

func forSession(key string) question.RememberedAnswer {
	return question.RememberedAnswer{
		Key:    key,
		Option: question.Option{Label: "Allow for session", RememberForSession: true},
	}
}

func TestNoApprovalsDrawNothingAndNeverAskForARedraw(t *testing.T) {
	approvals := []question.RememberedAnswer{}
	built := buildSegment(t, &approvals, &clock{at: startedAt})
	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("got %q", got)
	}
	if got := built.NextRefresh(segment.Phase{}); !got.IsZero() {
		t.Errorf("got a redraw at %v", got)
	}
}

func TestAMissingSourceDrawsNothing(t *testing.T) {
	built, err := New(nil, time.Now)(noOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestTheSegmentTakesNoOptions(t *testing.T) {
	if _, err := New(nil, time.Now)(refusingOptions{}); err == nil {
		t.Error("expected the options to be refused")
	}
}

func TestEachApprovalShowsItsKeyAndATimedOneItsTimeLeft(t *testing.T) {
	approvals := []question.RememberedAnswer{forSession("network:host"), timed("tool:fetch", 5*time.Minute)}
	built := buildSegment(t, &approvals, &clock{at: startedAt})

	got := built.Render(segment.Context{})
	if plain := style.Plain(got); plain != "network:host, tool:fetch 5m" {
		t.Errorf("got %q", plain)
	}
	want := style.Normal("network:host") + style.Subtle(", ") + style.Normal("tool:fetch") + " " + style.Dim("5m")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheTimeLeftRoundsUpToTheUnitShown(t *testing.T) {
	for _, test := range []struct {
		lifetime time.Duration
		want     string
	}{
		{lifetime: time.Second, want: "1m"},
		{lifetime: time.Minute, want: "1m"},
		{lifetime: time.Minute + time.Second, want: "2m"},
		{lifetime: 59 * time.Minute, want: "59m"},
		{lifetime: 59*time.Minute + time.Second, want: "1h"},
		{lifetime: time.Hour, want: "1h"},
		{lifetime: time.Hour + time.Second, want: "2h"},
		{lifetime: 26 * time.Hour, want: "26h"},
	} {
		approvals := []question.RememberedAnswer{timed("tool:fetch", test.lifetime)}
		got := style.Plain(buildSegment(t, &approvals, &clock{at: startedAt}).Render(segment.Context{}))
		if want := "tool:fetch " + test.want; got != want {
			t.Errorf("a lifetime of %v drew %q, want %q", test.lifetime, got, want)
		}
	}
}

func TestTheSegmentRedrawsWhenTheTimeLeftChangesAndWhenItEnds(t *testing.T) {
	approvals := []question.RememberedAnswer{timed("tool:fetch", 5*time.Minute)}
	now := &clock{at: startedAt}
	built := buildSegment(t, &approvals, now)

	var drawn []string
	for {
		drawn = append(drawn, style.Plain(built.Render(segment.Context{})))
		next := built.NextRefresh(segment.Phase{})
		if next.IsZero() {
			break
		}
		if !next.After(now.at) {
			t.Fatalf("at %v the next redraw is %v, which never moves on", now.at, next)
		}
		now.at = next
	}

	want := []string{"tool:fetch 5m", "tool:fetch 4m", "tool:fetch 3m", "tool:fetch 2m", "tool:fetch 1m", ""}
	if !slices.Equal(drawn, want) {
		t.Errorf("got %q, want %q", drawn, want)
	}
	if !now.at.Equal(startedAt.Add(5 * time.Minute)) {
		t.Errorf("the last redraw came at %v, want the expiry", now.at)
	}
}

func TestTheHourChangesToMinutesAtTheLastFullHour(t *testing.T) {
	approvals := []question.RememberedAnswer{timed("tool:fetch", 2*time.Hour)}
	now := &clock{at: startedAt}
	built := buildSegment(t, &approvals, now)

	var drawn []string
	for range 3 {
		drawn = append(drawn, style.Plain(built.Render(segment.Context{})))
		now.at = built.NextRefresh(segment.Phase{})
	}

	if want := []string{"tool:fetch 2h", "tool:fetch 1h", "tool:fetch 59m"}; !slices.Equal(drawn, want) {
		t.Errorf("got %q, want %q", drawn, want)
	}
}

func TestASessionApprovalNeverAsksForARedraw(t *testing.T) {
	approvals := []question.RememberedAnswer{forSession("network:host")}
	built := buildSegment(t, &approvals, &clock{at: startedAt})
	if got := built.NextRefresh(segment.Phase{}); !got.IsZero() {
		t.Errorf("got a redraw at %v", got)
	}
}

func TestTheSoonestChangeDecidesTheNextRedraw(t *testing.T) {
	approvals := []question.RememberedAnswer{timed("tool:lookup", time.Hour+30*time.Minute), timed("tool:fetch", 90*time.Second)}
	built := buildSegment(t, &approvals, &clock{at: startedAt})
	if got, want := built.NextRefresh(segment.Phase{}), startedAt.Add(30*time.Second); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestTheSegmentReadsTheCurrentApprovalsEveryTime(t *testing.T) {
	approvals := []question.RememberedAnswer{forSession("network:host")}
	built := buildSegment(t, &approvals, &clock{at: startedAt})
	if got := style.Plain(built.Render(segment.Context{})); got != "network:host" {
		t.Errorf("got %q", got)
	}

	approvals = nil
	if got := built.Render(segment.Context{}); got != "" {
		t.Errorf("got %q after the approval was revoked", got)
	}
}

func TestApprovalsAreHiddenBehindTheirCountWhenSpaceRunsOut(t *testing.T) {
	approvals := []question.RememberedAnswer{forSession("network:host"), forSession("tool:fetch")}
	ladder := buildSegment(t, &approvals, &clock{at: startedAt}).Ladder(segment.Context{})

	var got []string
	for _, rung := range ladder {
		got = append(got, style.Plain(rung))
	}
	if want := []string{"network:host, tool:fetch", "network:host, +1", "+2", "+"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
