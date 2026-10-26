package experimental

import (
	"strings"
	"testing"
	"time"
)

func TestEveryDeclaredToggleNamesItselfInSnakeCase(t *testing.T) {
	for name := range toggleKinds {
		if strings.ToLower(string(name)) != string(name) || strings.Contains(string(name), " ") {
			t.Errorf("toggle %q should be lowercase with underscores", name)
		}
	}
}

func TestBackgroundAfterSecondsIsAWholeNumberToggle(t *testing.T) {
	if complaints := Check(map[string]any{string(BackgroundAfterSeconds): int64(45)}); len(complaints) != 0 {
		t.Errorf("got complaints %v", complaints)
	}
	complaints := Check(map[string]any{string(BackgroundAfterSeconds): "45s"})
	if len(complaints) != 1 || !strings.Contains(complaints[0].Reason, "a whole number") {
		t.Errorf("got complaints %v, want the toggle to require a whole number", complaints)
	}
}

func TestMaximumRunningTasksIsAWholeNumberSetting(t *testing.T) {
	if complaints := Check(map[string]any{string(MaximumRunningTasks): int64(10)}); len(complaints) != 0 {
		t.Errorf("got complaints %v", complaints)
	}
	complaints := Check(map[string]any{string(MaximumRunningTasks): "ten"})
	if len(complaints) != 1 || !strings.Contains(complaints[0].Reason, "a whole number") {
		t.Errorf("got complaints %v, want the setting to require a whole number", complaints)
	}
}

func TestCommandStartsTurnIsABooleanToggle(t *testing.T) {
	if complaints := Check(map[string]any{string(CommandStartsTurn): true}); len(complaints) != 0 {
		t.Errorf("got complaints %v", complaints)
	}
	complaints := Check(map[string]any{string(CommandStartsTurn): "yes"})
	if len(complaints) != 1 || !strings.Contains(complaints[0].Reason, "true or false") {
		t.Errorf("got complaints %v, want the toggle to require a boolean", complaints)
	}
}

func TestEnterInterruptsIsABooleanToggle(t *testing.T) {
	if complaints := Check(map[string]any{string(EnterInterrupts): true}); len(complaints) != 0 {
		t.Errorf("got complaints %v", complaints)
	}
	complaints := Check(map[string]any{string(EnterInterrupts): "yes"})
	if len(complaints) != 1 || !strings.Contains(complaints[0].Reason, "true or false") {
		t.Errorf("got complaints %v, want the toggle to require a boolean", complaints)
	}
}

func TestRedrawTimingFeedbackIsABooleanToggle(t *testing.T) {
	if complaints := Check(map[string]any{string(RedrawTimingFeedback): true}); len(complaints) != 0 {
		t.Errorf("got complaints %v", complaints)
	}
	complaints := Check(map[string]any{string(RedrawTimingFeedback): "yes"})
	if len(complaints) != 1 || !strings.Contains(complaints[0].Reason, "true or false") {
		t.Errorf("got complaints %v, want the toggle to require a boolean", complaints)
	}
}

func TestAToggleNobodyDeclaresIsComplainedAboutAndDoesNothing(t *testing.T) {
	complaints := check(map[Name]Kind{}, map[string]any{"retired_thing": true})

	if len(complaints) != 1 {
		t.Fatalf("got %d complaints, want one", len(complaints))
	}
	if complaints[0].Name != "retired_thing" {
		t.Errorf("complaint names %q", complaints[0].Name)
	}
	if !strings.Contains(complaints[0].Reason, "does nothing") {
		t.Errorf("complaint reads %q", complaints[0].Reason)
	}
}

func TestAToggleWrittenAsTheWrongKindIsComplainedAbout(t *testing.T) {
	complaints := check(
		map[Name]Kind{"rounds": WholeNumberKind},
		map[string]any{"rounds": "three"},
	)

	if len(complaints) != 1 {
		t.Fatalf("got %d complaints, want one", len(complaints))
	}
	if !strings.Contains(complaints[0].Reason, "a whole number") {
		t.Errorf("complaint reads %q", complaints[0].Reason)
	}
}

func TestAToggleWrittenAsItsDeclaredKindIsAccepted(t *testing.T) {
	complaints := check(
		map[Name]Kind{
			"quiet":   BooleanKind,
			"label":   TextKind,
			"rounds":  WholeNumberKind,
			"portion": DecimalNumberKind,
		},
		map[string]any{
			"quiet":   true,
			"label":   "loud",
			"rounds":  int64(3),
			"portion": 0.5,
		},
	)

	if len(complaints) != 0 {
		t.Errorf("got complaints %v", complaints)
	}
}

func TestComplaintsArriveInNameOrder(t *testing.T) {
	complaints := check(map[Name]Kind{}, map[string]any{"second": true, "first": true})

	if len(complaints) != 2 || complaints[0].Name != "first" || complaints[1].Name != "second" {
		t.Errorf("got complaints %v", complaints)
	}
}

func TestTogglesReadEveryKindAndFallBackWhereNothingSaysOtherwise(t *testing.T) {
	toggles := New(map[string]any{
		"quiet":   true,
		"label":   "loud",
		"rounds":  int64(3),
		"portion": 0.5,
	})

	if !toggles.IsEnabled("quiet") {
		t.Error("quiet should be enabled")
	}
	if got := toggles.GetText("label", "quiet"); got != "loud" {
		t.Errorf("label is %q", got)
	}
	if got := toggles.GetWholeNumber("rounds", 1); got != 3 {
		t.Errorf("rounds is %d", got)
	}
	if got := toggles.GetDecimalNumber("portion", 1); got != 0.5 {
		t.Errorf("portion is %v", got)
	}

	if toggles.IsEnabled("absent") {
		t.Error("an absent toggle should be disabled")
	}
	if got := toggles.GetText("absent", "fallback"); got != "fallback" {
		t.Errorf("absent text is %q", got)
	}
	if got := toggles.GetWholeNumber("absent", 7); got != 7 {
		t.Errorf("absent whole number is %d", got)
	}
	if got := toggles.GetDecimalNumber("absent", 7); got != 7 {
		t.Errorf("absent decimal number is %v", got)
	}
}

func TestAToggleOfTheWrongKindReadsAsItsFallback(t *testing.T) {
	toggles := New(map[string]any{"quiet": "yes", "rounds": "three"})

	if toggles.IsEnabled("quiet") {
		t.Error("a toggle written as text should not be enabled")
	}
	if got := toggles.GetWholeNumber("rounds", 2); got != 2 {
		t.Errorf("rounds is %d", got)
	}
}

func TestReplacingTogglesIsSeenByLaterReads(t *testing.T) {
	toggles := New(map[string]any{"quiet": true})
	toggles.Replace(map[string]any{"quiet": false})

	if toggles.IsEnabled("quiet") {
		t.Error("the replaced toggle is still enabled")
	}
}

func TestTogglesReplacedFromTheirSourceAreNotChangedByLaterWritesToIt(t *testing.T) {
	values := map[string]any{"quiet": true}
	toggles := New(values)
	values["quiet"] = false

	if !toggles.IsEnabled("quiet") {
		t.Error("a write to the source changed the toggles")
	}
}

func TestNoTogglesAtAllReadAsTheirFallbacks(t *testing.T) {
	toggles := New(nil)

	if toggles.IsEnabled("quiet") {
		t.Error("an absent toggle should be disabled")
	}
	if got := toggles.GetText("label", "fallback"); got != "fallback" {
		t.Errorf("label is %q", got)
	}
}

func TestEveryKindSaysWhatAToggleOfItHolds(t *testing.T) {
	for kind, want := range map[Kind]string{
		BooleanKind:       "true or false",
		TextKind:          "a string",
		WholeNumberKind:   "a whole number",
		DecimalNumberKind: "a number",
		DurationKind:      "a positive duration string, such as 10m",
		Kind(-1):          "a value",
	} {
		if got := kind.String(); got != want {
			t.Errorf("kind %d says %q, want %q", int(kind), got, want)
		}
	}
}

func TestTimeoutSettingsRequireAPositiveDuration(t *testing.T) {
	for _, setting := range []Name{UserCommandTimeout, ResponseTimeout, StreamIdleTimeout} {
		t.Run(string(setting), func(t *testing.T) {
			for name, value := range map[string]any{
				"valid":      "10m",
				"fractional": "250ms",
				"empty":      "",
				"zero":       "0s",
				"negative":   "-1s",
				"invalid":    "tomorrow",
				"overflow":   "999999999999999999h",
				"number":     int64(600),
				"boolean":    true,
			} {
				t.Run(name, func(t *testing.T) {
					values := map[string]any{string(setting): value}
					isValid := name == "valid" || name == "fractional"
					complaints := Check(values)
					if (len(complaints) == 0) != isValid {
						t.Errorf("got complaints %v for %v", complaints, value)
					}
					want := time.Minute
					if isValid {
						var err error
						text, isText := value.(string)
						if !isText {
							t.Fatal("the valid duration is not text")
						}
						want, err = time.ParseDuration(text)
						if err != nil {
							t.Fatal(err)
						}
					}
					if got := New(values).GetDuration(setting, time.Minute); got != want {
						t.Errorf("got duration %s, want %s", got, want)
					}
				})
			}
			var absent *Toggles
			if got := absent.GetDuration(setting, time.Minute); got != time.Minute {
				t.Errorf("got absent duration %s", got)
			}
			toggles := New(nil)
			if got := toggles.GetDuration(setting, time.Minute); got != time.Minute {
				t.Errorf("got missing duration %s", got)
			}
			toggles.Replace(map[string]any{string(setting): "2m"})
			if got := toggles.GetDuration(setting, time.Minute); got != 2*time.Minute {
				t.Errorf("got replaced duration %s", got)
			}
		})
	}
}
