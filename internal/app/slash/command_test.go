package slash_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/slash"
)

func TestRegistryFindsACommandAndItsArguments(t *testing.T) {
	registry := mustRegistry(t,
		mustSet(t, "/", slash.Command{Name: "open", Run: commandHandler}),
		mustSet(t, "//", slash.Command{Name: "open", Run: commandHandler}),
	)

	invocation, found := registry.Find("  //open session-chat  ")
	if !found {
		t.Fatal("expected //open to be found")
	}
	if invocation.Name != "//open" || invocation.Usage != "//open" || invocation.Command.Name != "open" {
		t.Errorf("got invocation %+v", invocation)
	}
	if !slices.Equal(invocation.Arguments.Fields, []string{"session-chat"}) || invocation.Arguments.Text != "session-chat" {
		t.Errorf("got arguments %+v", invocation.Arguments)
	}
	if _, found := registry.Find("/opening"); found {
		t.Error("expected /opening not to match /open")
	}
}

func TestRegistryKeepsTheWhitespaceWithinAnArgument(t *testing.T) {
	registry := mustRegistry(t, mustSet(t, "//", slash.Command{Name: "add", Run: commandHandler}))

	invocation, found := registry.Find("  //add  first line\n\n  - one\n  - two  \n")
	if !found {
		t.Fatal("expected //add to be found")
	}
	if !slices.Equal(invocation.Arguments.Fields, []string{"first", "line", "-", "one", "-", "two"}) {
		t.Errorf("got fields %q", invocation.Arguments.Fields)
	}
	want := "first line\n\n  - one\n  - two"
	if invocation.Arguments.Text != want {
		t.Errorf("got text %q, want %q", invocation.Arguments.Text, want)
	}
}

func TestAnAttachedArgumentMayFollowTheNameWithOrWithoutASpace(t *testing.T) {
	registry := mustRegistry(t, mustSet(t, "/",
		slash.Command{Name: "!", Run: commandHandler}.WithAttachedArgument("<command>"),
		slash.Command{Name: "open", Run: commandHandler},
	))

	for _, input := range []string{"/!ls -la /tmp", "/! ls -la /tmp", "  /!   ls -la /tmp  "} {
		invocation, found := registry.Find(input)
		if !found {
			t.Fatalf("Find(%q) did not find the command", input)
		}
		if invocation.Name != "/!" || invocation.Usage != "/! <command>" {
			t.Errorf("Find(%q) got invocation %+v", input, invocation)
		}
		if invocation.Arguments.Text != "ls -la /tmp" {
			t.Errorf("Find(%q) got text %q", input, invocation.Arguments.Text)
		}
		if !slices.Equal(invocation.Arguments.Fields, []string{"ls", "-la", "/tmp"}) {
			t.Errorf("Find(%q) got fields %q", input, invocation.Arguments.Fields)
		}
	}

	invocation, found := registry.Find("/!")
	if !found {
		t.Fatal("expected a bare /! to be found")
	}
	if invocation.Arguments.Text != "" || len(invocation.Arguments.Fields) != 0 {
		t.Errorf("got arguments %+v", invocation.Arguments)
	}
}

func TestAnAttachedArgumentDoesNotSwallowALongerName(t *testing.T) {
	registry := mustRegistry(t, mustSet(t, "/",
		slash.Command{Name: "!", Run: commandHandler}.WithAttachedArgument("<command>"),
		slash.Command{Name: "!!", Run: commandHandler}.WithAttachedArgument("<command>"),
	))

	invocation, found := registry.Find("/!!echo hello")
	if !found {
		t.Fatal("expected /!! to be found")
	}
	if invocation.Name != "/!!" || invocation.Arguments.Text != "echo hello" {
		t.Errorf("got invocation %+v with arguments %+v", invocation, invocation.Arguments)
	}
}

func TestAnAttachedArgumentIsNotOfferedAsACompletion(t *testing.T) {
	registry := mustRegistry(t, mustSet(t, "/",
		slash.Command{Name: "!", Run: commandHandler}.WithAttachedArgument("<command>"),
		slash.Command{Name: "conf", Run: commandHandler},
	))

	assertCompletionCycle(t, registry, "/", []string{"/conf", "/conf"})

	var completion slash.Completion
	if completed, found := completion.Next(registry, "/!"); found {
		t.Errorf(`Next("/!") unexpectedly got %q`, completed)
	}
}

func TestSetRejectsInvalidDefinitions(t *testing.T) {
	tests := map[string]struct {
		prefix  string
		command slash.Command
	}{
		"empty prefix":    {command: slash.Command{Name: "open", Run: commandHandler}},
		"spaced prefix":   {prefix: "/ ", command: slash.Command{Name: "open", Run: commandHandler}},
		"empty name":      {prefix: "/", command: slash.Command{Run: commandHandler}},
		"prefixed name":   {prefix: "/", command: slash.Command{Name: "/open", Run: commandHandler}},
		"spaced name":     {prefix: "/", command: slash.Command{Name: "open file", Run: commandHandler}},
		"missing handler": {prefix: "/", command: slash.Command{Name: "open"}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := slash.NewCommandSet(test.prefix, test.command); err == nil {
				t.Error("expected invalid definition to return an error")
			}
		})
	}
}

func TestSetRejectsDuplicateNames(t *testing.T) {
	if _, err := slash.NewCommandSet(
		"/",
		slash.Command{Name: "open", Run: commandHandler},
		slash.Command{Name: "open", Run: commandHandler},
	); err == nil {
		t.Error("expected duplicate command name to return an error")
	}
}

func TestRegistryRejectsDuplicatePrefixes(t *testing.T) {
	first := mustSet(t, "/", slash.Command{Name: "open", Run: commandHandler})
	second := mustSet(t, "/", slash.Command{Name: "copy", Run: commandHandler})
	if _, err := slash.NewRegistry(first, second); err == nil {
		t.Error("expected duplicate prefix to return an error")
	}
}

func TestReplacingACommandSetKeepsTheOtherPrefixes(t *testing.T) {
	registry := mustRegistry(t,
		mustSet(t, "/", slash.Command{Name: "open", Run: commandHandler}),
		mustSet(t, "//", slash.Command{Name: "old", Run: commandHandler}),
	)

	replacement := mustSet(t, "//", slash.Command{Name: "new", Run: commandHandler})
	if err := registry.ReplaceCommandSet(replacement); err != nil {
		t.Fatal(err)
	}
	if _, found := registry.Find("//old"); found {
		t.Error("found the replaced command")
	}
	for _, command := range []string{"/open", "//new"} {
		if _, found := registry.Find(command); !found {
			t.Errorf("did not find %s", command)
		}
	}
}

func TestReplacingAnUnregisteredCommandSetIsRefused(t *testing.T) {
	registry := mustRegistry(t, mustSet(t, "/"))
	if err := registry.ReplaceCommandSet(mustSet(t, "//")); err == nil {
		t.Error("expected the unregistered prefix to be refused")
	}
}

func commandHandler(slash.Context, slash.Arguments) error {
	return nil
}

func TestCompletionCyclesThroughNamesWithinTheLongestPrefix(t *testing.T) {
	registry := mustRegistry(t,
		mustSet(t, "/",
			slash.Command{Name: "conf", Run: commandHandler},
			slash.Command{Name: "copy", Run: commandHandler},
			slash.Command{Name: "open", Run: commandHandler},
		),
		mustSet(t, "//",
			slash.Command{Name: "review", Run: commandHandler},
			slash.Command{Name: "rewrite", Run: commandHandler},
		),
	)

	assertCompletionCycle(t, registry, "/co", []string{"/conf", "/copy", "/conf"})
	assertCompletionCycle(t, registry, "//re", []string{"//review", "//rewrite", "//review"})
	assertCompletionCycle(t, registry, "/op", []string{"/open", "/open"})

	for _, prefix := range []string{"", "hello", "/missing", "//missing", "/open argument"} {
		var completion slash.Completion
		if completed, found := completion.Next(registry, prefix); found {
			t.Errorf("Next(%q) unexpectedly got %q", prefix, completed)
		}
	}
}

func TestCompletionReadsDynamicArgumentsWhenAsked(t *testing.T) {
	arguments := []string{"first"}
	registry := mustRegistry(t, mustSet(t, "/",
		slash.Command{Name: "revoke", Run: commandHandler}.
			WithListedArguments(func() []string { return slices.Clone(arguments) }).
			WithArgumentUsage("<path>"),
	))

	assertCompletionCycle(t, registry, "/revoke ", []string{"/revoke first"})
	arguments = []string{"second"}
	assertCompletionCycle(t, registry, "/revoke ", []string{"/revoke second"})
}

func TestCompletionCyclesThroughMatchingArguments(t *testing.T) {
	registry := mustRegistry(t, mustSet(t, "/",
		slash.Command{Name: "ask", Run: commandHandler}.WithArgumentUsage("<args>"),
		slash.Command{Name: "browse", Run: commandHandler}.WithArguments("config-dir", "session-dir"),
		slash.Command{Name: "conf", Run: commandHandler},
		slash.Command{Name: "copy", Run: commandHandler}.WithArguments("session-name", "session-id", "session-dir"),
		slash.Command{Name: "open", Run: commandHandler}.WithArguments("session-log", "session-chat"),
	))

	assertCompletionCycle(t, registry, "/copy ", []string{
		"/copy session-dir",
		"/copy session-id",
		"/copy session-name",
		"/copy session-dir",
	})
	assertCompletionCycle(t, registry, "/open session-", []string{
		"/open session-chat",
		"/open session-log",
		"/open session-chat",
	})
	assertCompletionCycle(t, registry, "/browse c", []string{"/browse config-dir"})

	for _, prefix := range []string{
		"/ask ",
		"/conf ",
		"/open session-log extra",
		"/missing anything",
	} {
		var completion slash.Completion
		if completed, found := completion.Next(registry, prefix); found {
			t.Errorf("Next(%q) unexpectedly got %q", prefix, completed)
		}
	}
}

func assertCompletionCycle(t *testing.T, registry slash.Registry, prefix string, wants []string) {
	t.Helper()

	var completion slash.Completion
	current := prefix
	for _, want := range wants {
		completed, found := completion.Next(registry, current)
		if !found || completed != want {
			t.Fatalf("Next(%q) got %q and %t, want %q", current, completed, found, want)
		}
		current = completed
	}
}

func TestCommandNameRecognisesRegisteredPrefixes(t *testing.T) {
	registry := mustRegistry(t,
		mustSet(t, "/"),
		mustSet(t, "//"),
	)

	for input, want := range map[string]string{
		"/unknown":        "/unknown",
		" /unknown arg ":  "/unknown",
		"//unknown":       "//unknown",
		" //unknown arg ": "//unknown",
	} {
		name, found := registry.CommandName(input)
		if !found || name != want {
			t.Errorf("CommandName(%q) got %q and %t", input, name, found)
		}
	}

	for _, input := range []string{"", "hello", "not/a/command"} {
		if name, found := registry.CommandName(input); found {
			t.Errorf("CommandName(%q) unexpectedly got %q", input, name)
		}
	}
}

func TestUsagesComeFromSetMetadata(t *testing.T) {
	set := mustSet(t, "//",
		slash.Command{Name: "add", Description: "Add a task.", Run: commandHandler}.WithArgumentUsage("<args>"),
		slash.Command{Name: "review", Run: commandHandler},
		slash.Command{Name: "test", Run: commandHandler}.WithArguments("quick", "all"),
	)
	want := []string{"//add <args>", "//review", "//test {all|quick}"}
	if got := set.Usages(); !slices.Equal(got, want) {
		t.Errorf("got usages %v, want %v", got, want)
	}
	wantHelp := []slash.HelpEntry{
		{Usage: "//add <args>", Description: "Add a task."},
		{Usage: "//review"},
		{Usage: "//test {all|quick}"},
	}
	if got := set.GetHelpEntries(); !slices.Equal(got, wantHelp) {
		t.Errorf("got help entries %#v, want %#v", got, wantHelp)
	}
}

func TestArgumentUsageSummarisesArgumentsThatStillComplete(t *testing.T) {
	command := slash.Command{Name: "open", Run: commandHandler}.
		WithArguments("config-dir", "session-dir").
		WithArgumentUsage("<target>")
	set := mustSet(t, "/", command)
	registry := mustRegistry(t, set)

	want := []string{"/open <target>"}
	if got := set.Usages(); !slices.Equal(got, want) {
		t.Errorf("got usages %v, want %v", got, want)
	}

	assertCompletionCycle(t, registry, "/open s", []string{"/open session-dir", "/open session-dir"})
}

func TestUsageErrorIsRecognised(t *testing.T) {
	err := slash.Usage()
	if !slash.IsUsageError(err) {
		t.Errorf("got %T %q", err, err)
	}
}

func TestCommandErrorsAreFormattedForFeedback(t *testing.T) {
	invocation := slash.Invocation{Name: "/copy", Usage: "/copy {session-dir|session-id|session-name}"}
	if got := slash.FormatError(invocation, slash.Usage()); got != "Usage: /copy {session-dir|session-id|session-name}" {
		t.Errorf("got usage error %q", got)
	}
	invocation = slash.Invocation{Name: "/conf", Usage: "/conf"}
	if got := slash.FormatError(invocation, errors.New("editor is not configured")); got != "/conf: Editor is not configured" {
		t.Errorf("got operational error %q", got)
	}
}

func mustSet(t *testing.T, prefix string, commands ...slash.Command) slash.CommandSet {
	t.Helper()

	set, err := slash.NewCommandSet(prefix, commands...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func mustRegistry(t *testing.T, sets ...slash.CommandSet) slash.Registry {
	t.Helper()

	registry, err := slash.NewRegistry(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestUsageErrorsCanBeWrapped(t *testing.T) {
	if !slash.IsUsageError(errors.Join(errors.New("wrapper"), slash.Usage())) {
		t.Error("expected wrapped usage error to be recognised")
	}
}

func TestOperationalErrorCapitalisesItsFirstRune(t *testing.T) {
	invocation := slash.Invocation{Name: "/open"}
	if got := slash.FormatError(invocation, errors.New("über failure")); !strings.HasPrefix(got, "/open: Über") {
		t.Errorf("got %q", got)
	}
}
