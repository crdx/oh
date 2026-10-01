package harness

import (
	"fmt"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/snippets"
)

func slashGoldenRegistry(t *testing.T) slash.Registry {
	t.Helper()

	systemSet, err := slash.NewCommandSet(
		"/",
		slash.Command{Name: "!", Description: "run a command on the host", Run: slashTestHandler}.
			WithAttachedArgument("<command>"),
		slash.Command{Name: "conf", Description: "edit the configuration file", Run: slashTestHandler},
		slash.Command{Name: "copy", Description: "copy a session target to the clipboard", Run: slashTestHandler}.
			WithArguments("session-name", "session-id", "session-dir"),
		slash.Command{Name: "grant", Description: "grant temporary path access", Run: slashTestHandler}.
			WithArguments("r", "rw").
			WithPathArgumentAfter("r", "rw").
			WithArgumentUsage("{r|rw} <path>...").
			WithCompletionUsage("<access> <path>..."),
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
		"08 enter submits rather than chooses": commanding(func(rig *pathRefRig) {
			typing("/qu")(rig)
			rig.press(pathRefEnter)
			if got := rig.input.Text(); got != "/qu" {
				rig.t.Errorf("enter changed the input to %q", got)
			}
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
			rig.typeText("/grant r ")
			rig.press(tabKey)
			rig.listingArrives()
		}),
		"26 tab chooses a grant path": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText("/grant rw ")
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
		"31 tab lists a tilde path after read access": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText("/grant r ~/doc")
			rig.press(tabKey)
			rig.listingArrives()
		}),
		"32 tab again after a command that takes nothing changes nothing": commanding(func(rig *pathRefRig) {
			typing("/con")(rig)
			rig.press(tabKey, tabKey)
			if got := rig.input.Text(); got != "/conf" {
				rig.t.Errorf("tab changed the input to %q", got)
			}
		}),
		"33 tab within a command with nothing to complete is swallowed": commanding(func(rig *pathRefRig) {
			typing("/copy anything")(rig)
			rig.press(tabKey)
			if got := rig.input.Text(); got != "/copy anything" {
				rig.t.Errorf("tab changed the input to %q", got)
			}
		}),
		"35 tab after a grant path lists another": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText("/grant r notes.txt ")
			rig.press(tabKey)
			rig.listingArrives()
		}),
		"36 tab quotes a grant path holding a space": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText("/grant r notes.txt ~/documents/")
			rig.press(tabKey)
			rig.listingArrives()
			rig.press(tabKey)
			if got, want := rig.input.Text(), `/grant r notes.txt "~/documents/meeting notes.txt" `; got != want {
				rig.t.Errorf("got input %q, want %q", got, want)
			}
		}),
		"37 tab completes within a quoted grant path": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText(`/grant r notes.txt "~/documents/mee`)
			rig.press(tabKey)
			rig.listingArrives()
			rig.press(tabKey)
			if got, want := rig.input.Text(), `/grant r notes.txt "~/documents/meeting notes.txt" `; got != want {
				rig.t.Errorf("got input %q, want %q", got, want)
			}
		}),
		"38 tab completes only the grant path under the cursor": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText("/grant r ~/no ~/documents/")
			for range len(" ~/documents/") {
				rig.press(pathRefLeft)
			}
			rig.press(tabKey)
			rig.listingArrives()
			rig.press(tabKey)
			if got, want := rig.input.Text(), "/grant r ~/notes.txt ~/documents/"; got != want {
				rig.t.Errorf("got input %q, want %q", got, want)
			}
		}),
		"39 a quoted grant directory goes on listing inside its quote": commanding(func(rig *pathRefRig) {
			rig.show()
			rig.typeText("/grant r notes.txt ~/documents/old")
			rig.press(tabKey)
			rig.listingArrives()
			rig.press(tabKey)
			rig.nextListingArrives()
			if got, want := rig.input.Text(), `/grant r notes.txt "~/documents/old drafts/`; got != want {
				rig.t.Errorf("got input %q, want %q", got, want)
			}
			rig.press(tabKey)
			if got, want := rig.input.Text(), `/grant r notes.txt "~/documents/old drafts/plan.txt" `; got != want {
				rig.t.Errorf("got input %q, want %q", got, want)
			}
		}),
		"34 tab within a snippet is swallowed": commanding(func(rig *pathRefRig) {
			typing("//review the tests")(rig)
			rig.press(tabKey)
			if got := rig.input.Text(); got != "//review the tests" {
				rig.t.Errorf("tab changed the input to %q", got)
			}
		}),
		"40 choosing a command goes on to the arguments it completes": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps: func(rig *pathRefRig) {
				typing("/ne")(rig)
				rig.press(tabKey)
			},
		},
		"41 completed arguments keep the order their command gives": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps:    typing("/new son"),
		},
		"42 tab chooses an argument that follows another": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps: func(rig *pathRefRig) {
				typing("/job stop ")(rig)
				rig.press(tabKey, pathRefDown, tabKey)
				if got := rig.input.Text(); got != "/job stop docs " {
					rig.t.Errorf("tab left %q in the input", got)
				}
			},
		},
		"43 a command taking an attached argument closes the dropdown": commanding(typing("/!ls")),
		"44 a bang alone closes the dropdown":                          commanding(typing("/!")),
		"45 a completed argument matching nothing closes the dropdown": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps:    typing("/new zz"),
		},
		"46 a completed argument typed whole closes the dropdown": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps:    typing("/new anthropic/claude-opus-5@high"),
		},
		"47 tab after a chosen completed argument is swallowed": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps: func(rig *pathRefRig) {
				typing("/new ")(rig)
				rig.press(tabKey, tabKey)
				if got := rig.input.Text(); got != "/new anthropic/claude-sonnet-5@medium " {
					rig.t.Errorf("tab left %q in the input", got)
				}
			},
		},
		"48 choosing an action that takes no name closes the dropdown": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps: func(rig *pathRefRig) {
				typing("/job pr")(rig)
				rig.press(tabKey)
				if got := rig.input.Text(); got != "/job prune " {
					rig.t.Errorf("tab left %q in the input", got)
				}
			},
		},
		"49 a long completed list windows around the selection": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps: func(rig *pathRefRig) {
				typing("/fork ")(rig)
				for range 9 {
					rig.press(pathRefDown)
				}
			},
		},
		"50 a long completed list stops at its last row": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps: func(rig *pathRefRig) {
				typing("/fork ")(rig)
				for range 14 {
					rig.press(pathRefDown)
				}
			},
		},
		"52 typing a provider keeps its models listed": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps:    typing("/new opencode-"),
		},
		"51 escape closes a completed argument's dropdown": {
			columns:  60,
			lines:    24,
			commands: completedArgumentsGoldenRegistry,
			steps: func(rig *pathRefRig) {
				typing("/new ")(rig)
				rig.press(pathRefEscape)
			},
		},
	}
}

var slashGoldenModels = []model.Choice{
	{Provider: model.AnthropicProvider, ID: "claude-sonnet-5", EffortLevels: []string{"medium", "high"}},
	{Provider: model.AnthropicProvider, ID: "claude-opus-5", EffortLevels: []string{"high"}},
	{Provider: model.OpencodeGoProvider, ID: "minimax-m3"},
}

func completedArgumentsGoldenRegistry(t *testing.T) slash.Registry {
	t.Helper()

	containing := func(candidates []string) func([]string, string) []string {
		return func(writtenArguments []string, partial string) []string {
			if len(writtenArguments) > 0 {
				return nil
			}
			var matches []string
			for _, candidate := range candidates {
				if strings.Contains(candidate, partial) {
					matches = append(matches, candidate)
				}
			}
			return matches
		}
	}
	manyModels := make([]string, 12)
	for i := range manyModels {
		manyModels[i] = fmt.Sprintf("model-%02d@high", i+1)
	}

	set, err := slash.NewCommandSet(
		"/",
		slash.Command{Name: "fork", Description: "fork the session", Run: slashTestHandler}.
			WithArgumentUsage("[<model>]").
			WithArgumentCompletion(containing(manyModels)),
		slash.Command{Name: "job", Description: "act on a job", Run: slashTestHandler}.
			WithArgumentUsage("{status|stop} <name> | prune").
			WithArgumentCompletion(func(writtenArguments []string, partial string) []string {
				switch {
				case len(writtenArguments) == 0:
					return slash.MatchingPrefixes(partial, []string{"status", "stop", "prune"})
				case len(writtenArguments) == 1 && writtenArguments[0] != "prune":
					return slash.MatchingPrefixes(partial, []string{"docs", "build"})
				default:
					return nil
				}
			}),
		slash.Command{Name: "new", Description: "start a new session", Run: slashTestHandler}.
			WithArgumentUsage("[<model>]").
			WithArgumentCompletion(func(writtenArguments []string, partial string) []string {
				if len(writtenArguments) > 0 {
					return nil
				}
				return model.SelectionsMatching(partial, slashGoldenModels)
			}),
	)
	if err != nil {
		t.Fatal(err)
	}

	return fixtureRegistry(t, set)
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
