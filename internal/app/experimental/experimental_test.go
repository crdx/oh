package experimental

import (
	"strings"
	"testing"
)

func TestEveryDeclaredToggleNamesItselfInSnakeCase(t *testing.T) {
	for name := range toggleKinds {
		if strings.ToLower(string(name)) != string(name) || strings.Contains(string(name), " ") {
			t.Errorf("toggle %q should be lowercase with underscores", name)
		}
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
