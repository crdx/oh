package output

import (
	"fmt"
	"slices"
	"strings"
)

var DefaultGroups = []string{"notice", "reasoning tool", "answer"}

var groups = map[string]Group{
	"answer":    AnswerGroup,
	"notice":    NoticeGroup,
	"reasoning": ReasoningGroup,
	"tool":      ToolGroup,
}

var defaultGrouping = parseDefaultGrouping()

func parseDefaultGrouping() Grouping {
	grouping, err := ParseGrouping(DefaultGroups)
	if err != nil {
		panic(err)
	}

	return grouping
}

type Grouping struct {
	representatives []Group
}

func ParseGrouping(clauses []string) (Grouping, error) {
	representatives := make([]Group, groupCount)
	for group := range representatives {
		representatives[group] = Group(group)
	}

	isNamed := make([]bool, groupCount)

	for _, clause := range clauses {
		names := strings.Fields(clause)
		if len(names) == 0 {
			return Grouping{}, fmt.Errorf("grouping holds an empty group, and wants %s", namedGroups())
		}

		clauseGroups := make([]Group, 0, len(names))

		for _, name := range names {
			group, isKnown := groups[name]
			if !isKnown {
				return Grouping{}, fmt.Errorf("grouping names %q, and wants %s", name, namedGroups())
			}
			if isNamed[group] {
				return Grouping{}, fmt.Errorf("grouping names %q twice, so it cannot say which group it is in", name)
			}

			isNamed[group] = true
			clauseGroups = append(clauseGroups, group)
		}

		representative := slices.Min(clauseGroups)
		for _, group := range clauseGroups {
			representatives[group] = representative
		}
	}

	return Grouping{representatives: representatives}, nil
}

func (self *Grouping) UnmarshalTOML(value any) error {
	entries, isList := value.([]any)
	if !isList {
		return fmt.Errorf("grouping is not a list of groups, each naming %s", namedGroups())
	}

	clauses := make([]string, 0, len(entries))

	for _, entry := range entries {
		clause, isText := entry.(string)
		if !isText {
			return fmt.Errorf("grouping holds a group that is not text, and wants %s", namedGroups())
		}

		clauses = append(clauses, clause)
	}

	grouping, err := ParseGrouping(clauses)
	if err != nil {
		return err
	}

	*self = grouping

	return nil
}

func (self *Grouping) resolve(group Group) Group {
	if self.representatives == nil {
		return defaultGrouping.representatives[group]
	}

	return self.representatives[group]
}

func (self *Grouping) runsOn(first Group, second Group) bool {
	return self.resolve(first) == self.resolve(second)
}

func namedGroups() string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}

	slices.Sort(names)

	return "one or more of " + strings.Join(names, ", ")
}
