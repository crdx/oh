package output

import (
	"strings"
	"testing"
)

func TestTheGroupingAScreenStartsWithIsTheDefaultOne(t *testing.T) {
	declared, err := ParseGrouping(DefaultGroups)
	if err != nil {
		t.Fatal(err)
	}

	var undeclared Grouping

	for first := range Group(groupCount) {
		for second := range Group(groupCount) {
			if isRunOn, isWanted := undeclared.runsOn(first, second), declared.runsOn(first, second); isRunOn != isWanted {
				t.Errorf("groups %d and %d run on %v undeclared and %v declared", first, second, isRunOn, isWanted)
			}
		}
	}
}

func TestEveryGroupNamedTogetherRunsOn(t *testing.T) {
	grouping, err := ParseGrouping([]string{"notice reasoning", "tool answer"})
	if err != nil {
		t.Fatal(err)
	}

	together := [][2]Group{
		{NoticeGroup, ReasoningGroup},
		{ReasoningGroup, NoticeGroup},
		{ToolGroup, AnswerGroup},
		{AnswerGroup, ToolGroup},
	}
	for _, pair := range together {
		if !grouping.runsOn(pair[0], pair[1]) {
			t.Errorf("%d and %d were named together but do not run on", pair[0], pair[1])
		}
	}

	apart := [][2]Group{
		{NoticeGroup, ToolGroup},
		{ReasoningGroup, AnswerGroup},
	}
	for _, pair := range apart {
		if grouping.runsOn(pair[0], pair[1]) {
			t.Errorf("%d and %d were named apart but run on", pair[0], pair[1])
		}
	}
}

func TestAGroupNamedOnItsOwnRunsOnNothingElse(t *testing.T) {
	grouping, err := ParseGrouping([]string{"notice", "reasoning", "tool", "answer"})
	if err != nil {
		t.Fatal(err)
	}

	for first := range Group(groupCount) {
		for second := range Group(groupCount) {
			if isRunOn, isWanted := grouping.runsOn(first, second), first == second; isRunOn != isWanted {
				t.Errorf("groups %d and %d run on %v, want %v", first, second, isRunOn, isWanted)
			}
		}
	}
}

func TestAGroupLeftUnnamedStandsOnItsOwn(t *testing.T) {
	grouping, err := ParseGrouping([]string{"reasoning tool"})
	if err != nil {
		t.Fatal(err)
	}

	if !grouping.runsOn(ReasoningGroup, ToolGroup) {
		t.Error("the named pair does not run on")
	}
	for _, group := range []Group{NoticeGroup, AnswerGroup} {
		if grouping.runsOn(group, ToolGroup) {
			t.Errorf("unnamed group %d runs on the named pair", group)
		}
	}
	if grouping.runsOn(NoticeGroup, AnswerGroup) {
		t.Error("two unnamed groups run on each other")
	}
}

func TestNamingNoGroupsAtAllPutsEveryGroupOnItsOwn(t *testing.T) {
	grouping, err := ParseGrouping(nil)
	if err != nil {
		t.Fatal(err)
	}

	for first := range Group(groupCount) {
		for second := range Group(groupCount) {
			if isRunOn, isWanted := grouping.runsOn(first, second), first == second; isRunOn != isWanted {
				t.Errorf("groups %d and %d run on %v, want %v", first, second, isRunOn, isWanted)
			}
		}
	}
}

func TestTheOrderGroupsAreNamedInDoesNotMatter(t *testing.T) {
	first, err := ParseGrouping([]string{"answer", "tool reasoning", "notice"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := ParseGrouping([]string{"notice", "reasoning tool", "answer"})
	if err != nil {
		t.Fatal(err)
	}

	for left := range Group(groupCount) {
		for right := range Group(groupCount) {
			if first.runsOn(left, right) != second.runsOn(left, right) {
				t.Errorf("groups %d and %d were read differently either way round", left, right)
			}
		}
	}
}

func TestAnUnknownGroupNamesTheOnesThatExist(t *testing.T) {
	_, err := ParseGrouping([]string{"notice", "thinking tool", "answer"})
	if err == nil {
		t.Fatal("expected an unknown group to be refused")
	}
	for _, name := range []string{"thinking", "answer", "notice", "reasoning", "tool"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("expected %q to be named, got %v", name, err)
		}
	}
}

func TestAGroupNamedTwiceIsRefused(t *testing.T) {
	if _, err := ParseGrouping([]string{"notice tool", "reasoning tool"}); err == nil {
		t.Fatal("expected a group named twice to be refused")
	}
}

func TestAnEmptyGroupIsRefused(t *testing.T) {
	for _, clauses := range [][]string{{"notice", "", "tool"}, {"notice", "   "}, {""}} {
		if _, err := ParseGrouping(clauses); err == nil {
			t.Errorf("expected %q to be refused", clauses)
		}
	}
}
