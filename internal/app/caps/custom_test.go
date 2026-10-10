package caps

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/pkg/agent"
)

func TestConfiguredGroupFlagsJoinTheBuiltInMode(t *testing.T) {
	grantedCaps, grantedGroups, err := ParseWithGroups("arx", "ba")
	if err != nil {
		t.Fatal(err)
	}
	if grantedCaps != Read|Shell {
		t.Errorf("got built-in capabilities %q", grantedCaps.Flags())
	}
	if grantedGroups != "a" {
		t.Errorf("got custom groups %q", grantedGroups)
	}

	if _, _, err := ParseWithGroups("z", "ba"); err == nil || !strings.Contains(err.Error(), "rxwsnglba") {
		t.Errorf("got %v, want the configured flags named", err)
	}
}

func TestACustomToolGroupSharesOneSwitch(t *testing.T) {
	mode := NewModeWithGroups(Read, "", ToolGroups{
		"a": {"deploy", "weather"},
	})

	if mode.Allows("a") {
		t.Fatal("a custom group opened granted")
	}
	if status := mode.Groups(); status.Flags != "a" || status.GrantedFlags != "" {
		t.Errorf("got group status %#v", status)
	}
	if !mode.ToggleGroup("a") {
		t.Fatal("the configured group was not toggled")
	}
	if !mode.Allows("a") {
		t.Fatal("the custom group remained refused")
	}
	if got := mode.Inject(); got != "The deploy tool is now available. The weather tool is now available." {
		t.Errorf("got notice %q", got)
	}
}

func TestACustomToolCanShareABuiltInCapability(t *testing.T) {
	mode := NewModeWithGroups(Read, "", ToolGroups{
		"n": {"publish"},
	})

	mode.Toggle(Network)
	if !mode.Allows("n") {
		t.Fatal("the grouped tool did not follow network access")
	}
	notice := mode.Inject()
	for _, wanted := range []string{
		hostNetworkNotice(true),
		fetchNotice(true),
		"The publish tool is now available.",
	} {
		if !strings.Contains(notice, wanted) {
			t.Errorf("notice %q does not contain %q", notice, wanted)
		}
	}
}

func TestDisabledToolsLeaveOnlyCompatibleGroupMembersActive(t *testing.T) {
	mode := NewModeWithGroups(Read, "", ToolGroups{
		"a": {"deploy", "weather"},
		"b": {"publish"},
	})
	mode.RestrictTools([]string{"deploy"})

	if status := mode.Groups(); status.Flags != "a" || status.GrantedFlags != "" {
		t.Errorf("got group status %#v", status)
	}
	if !mode.ToggleGroup("a") {
		t.Fatal("the compatible group member could not be toggled")
	}
	if got := mode.Inject(); got != "The deploy tool is now available." {
		t.Errorf("got notice %q", got)
	}
}

func TestAMissingToolGroupStaysFrozenWithoutBeingShown(t *testing.T) {
	frozenGroups := ToolGroups{"a": {"weather"}}
	mode := NewModeWithGroups(Read, "a", frozenGroups, ToolGroups{})

	if status := mode.Groups(); status.Flags != "" || status.GrantedFlags != "" {
		t.Errorf("got visible group status %#v", status)
	}
	grantedGroups, restoredGroups, found := LastRecordedToolGroups([]agent.Event{mode.Event("")})
	if !found || grantedGroups != "a" || !slices.Equal(restoredGroups["a"], []string{"weather"}) {
		t.Errorf("got restored groups %q, %#v and found %v", grantedGroups, restoredGroups, found)
	}
}

func TestToolGroupsCanBeRestrictedToTheSessionToolbox(t *testing.T) {
	groups := ToolGroups{
		"a": {"deploy", "weather"},
		"b": {"publish"},
	}

	filtered := groups.Only([]string{"weather", "publish"})
	if !slices.Equal(filtered["a"], []string{"weather"}) || !slices.Equal(filtered["b"], []string{"publish"}) {
		t.Errorf("got groups %#v", filtered)
	}
	if group := filtered.GroupOf("weather"); group != "a" {
		t.Errorf("got group %q", group)
	}
}
