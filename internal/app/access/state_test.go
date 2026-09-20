package access

import (
	"slices"
	"strconv"
	"testing"
)

func sliceDefinition() Definition[[]int] {
	return Definition[[]int]{
		Clone: slices.Clone[[]int],
		Describe: func(known []int, current []int) string {
			if slices.Equal(known, current) {
				return ""
			}
			return strconv.Itoa(len(known)) + " to " + strconv.Itoa(len(current))
		},
	}
}

func TestAChangeIsInjectedOnce(t *testing.T) {
	state := New([]int{1}, sliceDefinition())
	state.Change(func(current []int) []int { return append(current, 2) })

	if got := state.Inject(); got != "1 to 2" {
		t.Errorf("got first injection %q", got)
	}
	if got := state.Inject(); got != "" {
		t.Errorf("got repeated injection %q", got)
	}
}

func TestRestoredCurrentAndKnownValuesMayDiffer(t *testing.T) {
	state := NewRestored([]int{1}, []int{1, 2}, sliceDefinition())
	if got := state.Inject(); got != "2 to 1" {
		t.Errorf("got restored injection %q", got)
	}
}

func TestValuesAreCopiedAcrossTheBoundary(t *testing.T) {
	initial := []int{1}
	state := New(initial, sliceDefinition())
	initial[0] = 9

	current := state.GetCurrent()
	current[0] = 8
	if got := state.GetCurrent(); !slices.Equal(got, []int{1}) {
		t.Errorf("current value was aliased: %v", got)
	}

	state.Change(func(changing []int) []int {
		changing[0] = 2
		return changing
	})
	if got := state.GetCurrent(); !slices.Equal(got, []int{2}) {
		t.Errorf("changed value is %v", got)
	}
}

func TestAGroupJoinsChangesInImplementationOrder(t *testing.T) {
	first := NewRestored([]int{1}, nil, sliceDefinition())
	second := NewRestored([]int{1, 2}, nil, sliceDefinition())

	if got := NewGroup(first, second).Inject(); got != "0 to 1 0 to 2" {
		t.Errorf("got grouped injection %q", got)
	}
}
