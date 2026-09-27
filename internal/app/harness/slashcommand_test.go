package harness

import (
	"testing"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/snippets"
)

func slashGoldenRegistry(t *testing.T) slash.Registry {
	t.Helper()

	systemSet, err := slash.NewCommandSet(
		"/",
		slash.Command{Name: "conf", Description: "Edit the configuration file.", Run: slashTestHandler},
		slash.Command{Name: "copy", Description: "Copy a session target to the clipboard.", Run: slashTestHandler}.
			WithArguments("session-name", "session-id", "session-dir"),
		slash.Command{Name: "grant", Description: "Grant temporary path access.", Run: slashTestHandler}.
			WithArguments("r", "rw", "rx", "rxw"),
		slash.Command{Name: "open", Run: slashTestHandler}.WithArguments("config-dir", "workspace-dir"),
		slash.Command{Name: "quit", Description: "Leave the session, and say goodbye in a line long enough to be cut.", Run: slashTestHandler},
		slash.Command{Name: "revoke", Description: "Revoke a grant.", Run: slashTestHandler}.
			WithListedArguments(func() []string { return nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	snippetSet, err := snippets.New(map[string]snippets.Definition{
		"review": {Prompt: "Review changes.", Description: "Review the working tree.", Arguments: snippets.ArgumentsNone},
		"test":   {Prompt: "Run tests.", Arguments: snippets.ArgumentsNone},
	})
	if err != nil {
		t.Fatal(err)
	}

	return fixtureRegistry(t, systemSet, snippetSet)
}

func slashCommandScenarios(t *testing.T) map[string]pathRefScenario {
	t.Helper()

	commanding := func(steps func(rig *pathRefRig)) pathRefScenario {
		return pathRefScenario{columns: 60, lines: 24, commands: slashGoldenRegistry, steps: steps}
	}
	typing := func(text string) func(rig *pathRefRig) {
		return func(rig *pathRefRig) {
			rig.show()
			rig.typeText(text)
		}
	}

	return map[string]pathRefScenario{
		"01 a slash lists the commands with what they do": commanding(typing("/")),
		"02 a name narrows the commands":                  commanding(typing("/co")),
		"03 a name matching nothing":                      commanding(typing("/zz")),
		"04 tab chooses a command that takes nothing": commanding(func(rig *pathRefRig) {
			typing("/con")(rig)
			rig.press(tabKey)
		}),
		"05 choosing a command goes on to its arguments": commanding(func(rig *pathRefRig) {
			typing("/co")(rig)
			rig.press(pathRefDown, tabKey)
		}),
		"06 an argument narrows the arguments": commanding(typing("/copy session-n")),
		"07 tab chooses an argument": commanding(func(rig *pathRefRig) {
			typing("/copy ")(rig)
			rig.press(pathRefDown, tabKey)
		}),
		"08 enter chooses rather than sends": commanding(func(rig *pathRefRig) {
			typing("/qu")(rig)
			rig.press(pathRefEnter)
		}),
		"09 two slashes list the snippets":                  commanding(typing("//")),
		"10 a path closes the dropdown at its second slash": commanding(typing("/tmp/notes")),
		"11 a slash within prose opens nothing":             commanding(typing("see /co")),
		"12 tab after a command that takes nothing is left to the input": commanding(func(rig *pathRefRig) {
			typing("/conf ")(rig)
			rig.press(tabKey)
		}),
		"13 escape closes the dropdown": commanding(func(rig *pathRefRig) {
			typing("/")(rig)
			rig.press(pathRefEscape)
		}),
		"14 recalling a command opens nothing": {
			columns:      60,
			lines:        24,
			commands:     slashGoldenRegistry,
			historyLines: []string{"/copy session-id"},
			steps: func(rig *pathRefRig) {
				rig.show()
				rig.press(pathRefUp)
			},
		},
		"15 tab over a recalled argument opens its arguments": {
			columns:      60,
			lines:        24,
			commands:     slashGoldenRegistry,
			historyLines: []string{"/copy session-"},
			steps: func(rig *pathRefRig) {
				rig.show()
				rig.press(pathRefUp, tabKey)
			},
		},
		"16 a path reference within a command completes as a path": commanding(func(rig *pathRefRig) {
			rig.typeAndList("/grant rw @harn")
		}),
		"17 a narrow terminal cuts what a command does": {
			columns:  20,
			lines:    24,
			commands: slashGoldenRegistry,
			steps:    typing("/q"),
		},
		"18 alt with a slash opens nothing": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.press(key.Key{Code: key.Rune, Value: '/', Mod: key.Alt})
		}),
		"19 a command with nothing to list closes once chosen": commanding(func(rig *pathRefRig) {
			typing("/rev")(rig)
			rig.press(tabKey)
		}),
		"20 an argument matching nothing closes the dropdown": commanding(typing("/copy x")),
		"21 typing on after a closed command opens nothing": commanding(func(rig *pathRefRig) {
			typing("/rev")(rig)
			rig.press(tabKey)
			rig.typeText("/tmp/notes")
		}),
		"22 a command typed whole that takes nothing closes the dropdown": commanding(typing("/quit")),
		"23 an argument typed whole closes the dropdown":                  commanding(typing("/copy session-id")),
	}
}

func TestGoldenSlashCommandsDrawWhatTheyDrewBefore(t *testing.T) {
	scenarios := slashCommandScenarios(t)
	passes := streamPasses(t, pathRefStream, scenarios)

	shownAt := map[string]func() string{}
	for name, scenario := range scenarios {
		shownAt[name] = func() string {
			return shown(t, passes[name](), scenario.columns)
		}
	}

	compareWithGolden(t, "slashcommands", ".ansi", passes)
	compareWithGolden(t, "slashcommands", ".screen", shownAt)
}
