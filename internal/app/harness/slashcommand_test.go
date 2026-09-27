package harness

import (
	"testing"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/snippets"
)

func slashGoldenRegistry(t *testing.T) slash.Registry {
	t.Helper()

	systemSet, err := slash.NewCommandSet(
		"/",
		slash.Command{Name: "conf", Description: "edit the configuration file", Run: slashTestHandler},
		slash.Command{Name: "copy", Description: "copy a session target to the clipboard", Run: slashTestHandler}.
			WithArguments("session-name", "session-id", "session-dir"),
		slash.Command{Name: "grant", Description: "grant temporary path access", Run: slashTestHandler}.
			WithArguments("r", "rw", "rx", "rxw").
			WithPathArgumentAfterMatching(func(argument string) bool {
				access, err := shell.ParseAccess(argument)
				return err == nil && shell.IsAccess(access)
			}).
			WithArgumentUsage("{r|rx|rw|rxw} <path>").
			WithCompletionUsage("<access> <path>"),
		slash.Command{Name: "open", Run: slashTestHandler}.WithArguments("config-dir", "workspace-dir"),
		slash.Command{Name: "quit", Description: "leave the session, and say goodbye in a line long enough to be cut", Run: slashTestHandler},
		slash.Command{Name: "revoke", Description: "revoke a grant", Run: slashTestHandler}.
			WithListedArguments(func() []string { return nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	snippetSet, err := snippets.New(map[string]snippets.Definition{
		"review": {Prompt: "Review {{.Arg}}.", Description: "Review the named scope.", Arguments: snippets.ArgumentsRequired},
		"test":   {Prompt: `Run {{.Arg | default "the tests"}}.`, Arguments: snippets.ArgumentsOptional},
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
		"12 tab after a complete command is swallowed": commanding(func(rig *pathRefRig) {
			typing("/conf ")(rig)
			rig.press(tabKey)
			if got := rig.input.Text(); got != "/conf " {
				rig.t.Errorf("tab changed the input to %q", got)
			}
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
		"24 choosing a snippet leaves room for its arguments": commanding(func(rig *pathRefRig) {
			typing("//rev")(rig)
			rig.press(tabKey)
			rig.typeText("the tests")
		}),
		"25 tab after grant flags lists paths": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText("/grant rx ")
			rig.press(tabKey)
			rig.listingArrives()
		}),
		"26 tab chooses a grant path": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText("/grant rx ")
			rig.press(tabKey)
			rig.listingArrives()
			rig.press(pathRefDown, tabKey)
		}),
		"27 enter sends a whole command that takes arguments": commanding(func(rig *pathRefRig) {
			typing("/copy")(rig)
			rig.press(pathRefEnter)
			if got := rig.input.Text(); got != "" {
				rig.t.Errorf("enter left %q in the input rather than sending it", got)
			}
		}),
		"28 space after a whole command goes on to its arguments": commanding(typing("/copy ")),
		"29 tab after a chosen argument offers only the rest": {
			columns:  60,
			lines:    24,
			commands: manyArgumentsGoldenRegistry,
			steps: func(rig *pathRefRig) {
				typing("/revoke ")(rig)
				rig.press(tabKey, tabKey)
			},
		},
		"30 tab after arguments typed by hand offers only the rest": {
			columns:  60,
			lines:    24,
			commands: manyArgumentsGoldenRegistry,
			steps: func(rig *pathRefRig) {
				typing("/revoke 8080 /first ")(rig)
				rig.press(tabKey)
			},
		},
		"31 tab lists a tilde path after conventionally ordered access": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText("/grant rwx ~/doc")
			rig.press(tabKey)
			rig.listingArrives()
		}),
	}
}

func manyArgumentsGoldenRegistry(t *testing.T) slash.Registry {
	t.Helper()

	set, err := slash.NewCommandSet(
		"/",
		slash.Command{Name: "revoke", Description: "revoke grants", Run: slashTestHandler}.
			WithArguments("/first", "/second", "8080").
			WithManyArguments(),
	)
	if err != nil {
		t.Fatal(err)
	}

	return fixtureRegistry(t, set)
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
