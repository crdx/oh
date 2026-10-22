package harness

import (
	"slices"
	"testing"

	"crdx.org/oh/internal/app/caps"
)

func TestAResumedBuiltinKeepsItsGroupWhenTheConfigRemovesIt(t *testing.T) {
	groups := caps.ToolGroups{"a": {"read"}}
	selected := selectedGroupToolNames(nil, []string{"read", "ls"}, groups, true)
	if !slices.Equal(selected, []string{"read"}) {
		t.Fatalf("resumed group lost read: %v", selected)
	}
	mode := caps.NewModeWithGroups(caps.Read, "", groups, groups.Only(selected))
	if mode.Allows("a") {
		t.Error("group was granted without being toggled")
	}
	if !mode.ToggleGroup("a") || !mode.Allows("a") {
		t.Error("resumed group cannot be toggled")
	}
}

func TestANewSessionDoesNotOfferAGroupAbsentFromItsConfig(t *testing.T) {
	selected := selectedGroupToolNames(nil, []string{"read"}, caps.ToolGroups{"a": {"read"}}, false)
	if len(selected) != 0 {
		t.Errorf("new session retained a removed group: %v", selected)
	}
}
