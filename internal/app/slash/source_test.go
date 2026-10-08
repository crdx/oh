package slash_test

import (
	"reflect"
	"testing"

	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/trigger"
)

func sourceFixture(t *testing.T) *slash.Source {
	t.Helper()

	registry := mustRegistry(t,
		mustSet(t, "/",
			slash.Command{Name: "conf", Description: "Edit the config.\nIn your editor.", Run: commandHandler},
			slash.Command{Name: "copy", Description: "Copy a target.", Run: commandHandler}.
				WithArguments("session-name", "session-id").
				WithArgumentUsage("<target>"),
			slash.Command{Name: "grant", Description: "Grant path access.", Run: commandHandler}.
				WithArguments("r", "rx").
				WithPathArgumentAfter("r", "rx").
				WithArgumentUsage("{r|rx} <path>").
				WithCompletionUsage("<access> <path>"),
			slash.Command{Name: "quit", Run: commandHandler},
		),
		mustSet(t, "//",
			slash.Command{Name: "review", Description: "Review a scope.", Run: commandHandler}.WithArgumentUsage("<args>"),
		),
	)

	return slash.NewSource(func() slash.Registry { return registry })
}

func TestTheSourceFindsOnlyWhatTheRegistryCompletes(t *testing.T) {
	source := sourceFixture(t)

	for name, test := range map[string]struct {
		text      string
		cursor    int
		want      trigger.Word
		wantFound bool
	}{
		"a bare slash":                 {text: "/", cursor: 1, want: trigger.Word{End: 1, Query: "/"}, wantFound: true},
		"a name":                       {text: "/co", cursor: 3, want: trigger.Word{End: 3, Query: "/co"}, wantFound: true},
		"the middle of a name":         {text: "/copy x", cursor: 2, want: trigger.Word{End: 5, Query: "/c"}, wantFound: true},
		"an argument":                  {text: "/copy session-na", cursor: 16, want: trigger.Word{End: 16, Query: "/copy session-na"}, wantFound: true},
		"a snippet":                    {text: "//re", cursor: 4, want: trigger.Word{End: 4, Query: "//re"}, wantFound: true},
		"a command taking nothing":     {text: "/conf ", cursor: 6},
		"a second argument":            {text: "/copy session-id more", cursor: 21},
		"an argument matching nothing": {text: "/copy x", cursor: 7},
		"a path":                       {text: "/tmp/notes.md", cursor: 13},
		"prose":                        {text: "hello /co", cursor: 9},
		"nothing":                      {text: "", cursor: 0},
	} {
		t.Run(name, func(t *testing.T) {
			got, found := source.Find([]rune(test.text), test.cursor)
			if found != test.wantFound {
				t.Fatalf("found %v, want %v", found, test.wantFound)
			}
			if got != test.want {
				t.Errorf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestTheSourceDescribesEachCommand(t *testing.T) {
	source := sourceFixture(t)

	want := trigger.Results{
		Items: []trigger.Result{
			{Label: "/conf", Detail: "Edit the config.", Text: "/conf", IsFinal: true},
			{Label: "/copy <target>", Detail: "Copy a target.", Text: "/copy ", IsOpenEnded: true},
			{
				Label:       "/grant <access> <path>",
				Detail:      "Grant path access.",
				Text:        "/grant ",
				IsOpenEnded: true,
			},
			{Label: "/quit", Text: "/quit", IsFinal: true},
		},
		Total:       4,
		Placeholder: "no matching commands",
	}
	if got := source.Results(trigger.Word{Query: "/"}, 10); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTheSourceShowsFreeFormArgumentUsageWithoutInsertingIt(t *testing.T) {
	source := sourceFixture(t)

	want := []trigger.Result{{
		Label:       "//review <args>",
		Detail:      "Review a scope.",
		Text:        "//review ",
		IsOpenEnded: true,
	}}
	if got := source.Results(trigger.Word{Query: "//"}, 10).Items; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTheSourceCompletesArgumentsWhole(t *testing.T) {
	source := sourceFixture(t)

	want := []trigger.Result{
		{Label: "session-id", Text: "/copy session-id"},
		{Label: "session-name", Text: "/copy session-name"},
	}
	if got := source.Results(trigger.Word{Query: "/copy "}, 10).Items; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTheSourceHoldsNoMoreThanItIsAskedFor(t *testing.T) {
	source := sourceFixture(t)

	got := source.Results(trigger.Word{Query: "/"}, 2)
	if len(got.Items) != 2 || got.Total != 4 {
		t.Errorf("held %d of %d", len(got.Items), got.Total)
	}
}

func TestTheSourceAnswersToASlash(t *testing.T) {
	source := sourceFixture(t)

	if source.Symbol() != '/' || source.Elision() != dropdown.ElideEnd {
		t.Errorf("the source answers to %q and elides with %v", source.Symbol(), source.Elision())
	}
}

func TestAnArgumentTakingAValueKeepsCompletingIntoIt(t *testing.T) {
	registry := mustRegistry(t, mustSet(t, "/",
		slash.Command{Name: "new", Run: commandHandler}.
			WithArgumentChoices(func([]string, string) []slash.ArgumentChoice {
				return []slash.ArgumentChoice{
					{Text: "-m", Detail: "pick a model\nand more", TakesValue: true},
					{Text: "--yolo", Detail: "run outside the sandbox"},
				}
			}),
	))
	source := slash.NewSource(func() slash.Registry { return registry })

	got := source.Results(trigger.Word{End: 5, Query: "/new "}, 10).Items
	want := []trigger.Result{
		{Label: "-m", Detail: "pick a model", Text: "/new -m ", IsOpenEnded: true},
		{Label: "--yolo", Detail: "run outside the sandbox", Text: "/new --yolo"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
