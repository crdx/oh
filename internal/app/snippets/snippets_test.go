package snippets_test

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/io/internal/app/slash"
	"crdx.org/io/internal/app/snippets"
	"crdx.org/io/pkg/agent"
)

type snippetContext struct {
	sent   string
	notice string
}

func (self *snippetContext) Emit(agent.Event) {}
func (self *snippetContext) Send(prompt string) {
	self.sent = prompt
}

func (self *snippetContext) Notice(text string) {
	self.notice = text
}

func (self *snippetContext) PlainNotice(string) {}
func (self *snippetContext) Success(string)     {}

func TestSnippetSendsItsConfiguredPrompt(t *testing.T) {
	invocation := getInvocation(t, map[string]snippets.Definition{
		"review": {
			Prompt:    "  Review the changes.  ",
			Arguments: snippets.ArgumentsRequired,
		},
	}, "//review now")

	context := &snippetContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if context.sent != "Review the changes." {
		t.Errorf("sent %q", context.sent)
	}
}

func TestSnippetRendersArgumentsWithGoTemplates(t *testing.T) {
	configured := map[string]snippets.Definition{
		"add": {
			Prompt:    `{{- range $i, $argument := .Args }}{{if $i}}, {{end}}{{ $argument | printf "%q" }}{{end}}: {{.Arg}}`,
			Arguments: snippets.ArgumentsRequired,
		},
	}
	invocation := getInvocation(t, configured, "//add first second")

	context := &snippetContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if context.sent != `"first", "second": first second` {
		t.Errorf("sent %q", context.sent)
	}
}

func TestSnippetKeepsTheLayoutOfAMultiLineArgument(t *testing.T) {
	configured := map[string]snippets.Definition{
		"add": {
			Prompt:    "Add the following:\n\n{{.Arg}}",
			Arguments: snippets.ArgumentsRequired,
		},
	}
	invocation := getInvocation(t, configured, "//add first line\n\n  - one\n  - two\n")

	context := &snippetContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	want := "Add the following:\n\nfirst line\n\n  - one\n  - two"
	if context.sent != want {
		t.Errorf("sent %q, want %q", context.sent, want)
	}
}

func TestSnippetRequiredArgumentsAreEnforced(t *testing.T) {
	invocation := getInvocation(t, map[string]snippets.Definition{
		"review": {
			Prompt:    "Review: {{.Arg}}",
			Arguments: snippets.ArgumentsRequired,
		},
	}, "//review")

	err := invocation.Command.Run(&snippetContext{}, invocation.Arguments)
	if !slash.IsUsageError(err) {
		t.Errorf("got error %v", err)
	}
	if got := slash.FormatError(invocation, err); got != "Usage: //review <args>" {
		t.Errorf("got formatted error %q", got)
	}
}

func TestOptionalSnippetCanUseADefaultForMissingArguments(t *testing.T) {
	configured := map[string]snippets.Definition{
		"review": {
			Prompt:    `{{define "value"}}{{ . | default "the current changes" }}{{end}}Review {{template "value" .Arg}}.`,
			Arguments: snippets.ArgumentsOptional,
		},
	}
	invocation := getInvocation(t, configured, "//review")

	context := &snippetContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if context.sent != "Review the current changes." {
		t.Errorf("sent %q", context.sent)
	}
}

func TestSnippetTemplateErrorsAreReported(t *testing.T) {
	tests := map[string]struct {
		configured map[string]snippets.Definition
		input      string
		want       string
	}{
		"parse": {
			configured: map[string]snippets.Definition{
				"review": {Prompt: "{{", Arguments: snippets.ArgumentsRequired},
			},
			want: "review",
		},
		"execute": {
			configured: map[string]snippets.Definition{
				"review": {Prompt: "{{index .Args 2}}", Arguments: snippets.ArgumentsRequired},
			},
			input: "//review only-one",
			want:  "could not render template",
		},
		"empty result": {
			configured: map[string]snippets.Definition{
				"review": {Prompt: `{{default "" .Arg}}`, Arguments: snippets.ArgumentsOptional},
			},
			input: "//review",
			want:  "empty prompt",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			set, err := snippets.New(test.configured)
			if test.input == "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Errorf("got error %v, want %q", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			registry, err := slash.NewRegistry(set)
			if err != nil {
				t.Fatal(err)
			}
			invocation, found := registry.Find(test.input)
			if !found {
				t.Fatalf("expected %q to be registered", test.input)
			}
			context := &snippetContext{}
			err = invocation.Command.Run(context, invocation.Arguments)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("got error %v, want %q", err, test.want)
			}
			if context.sent != "" {
				t.Errorf("sent prompt %q after an error", context.sent)
			}
		})
	}
}

func TestSnippetDefinitionsAreValidated(t *testing.T) {
	valid := snippets.Definition{Prompt: "Prompt", Arguments: snippets.ArgumentsNone}
	for name, configured := range map[string]map[string]snippets.Definition{
		"empty name":        {"": valid},
		"spaced name":       {"code review": valid},
		"slash in name":     {"review/code": valid},
		"prefixed name":     {"//review": valid},
		"empty prompt":      {"review": {Arguments: snippets.ArgumentsNone}},
		"whitespace prompt": {"review": {Prompt: "  \t  ", Arguments: snippets.ArgumentsNone}},
		"invalid arguments": {"review": {Prompt: "Prompt", Arguments: "sometimes"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := snippets.New(configured); err == nil {
				t.Error("expected invalid snippet to return an error")
			}
		})
	}
}

func TestSnippetUsagesAreDeterministic(t *testing.T) {
	set, err := snippets.New(map[string]snippets.Definition{
		"test":     {Prompt: "Run tests.", Arguments: snippets.ArgumentsNone},
		"review":   {Prompt: "Review changes.", Arguments: snippets.ArgumentsRequired},
		"optional": {Prompt: `{{.Arg | default "Use the current context."}}`, Arguments: snippets.ArgumentsOptional},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"//optional [args]", "//review <args>", "//test", "//help"}
	if got := set.Usages(); !slices.Equal(got, want) {
		t.Errorf("got usages %v, want %v", got, want)
	}
}

func TestExplicitArgumentPoliciesControlUsageAndValidation(t *testing.T) {
	configured := map[string]snippets.Definition{
		"none": {
			Prompt:      "Use the current context.",
			Description: "Use no arguments.",
			Arguments:   snippets.ArgumentsNone,
		},
		"optional": {
			Prompt:      `Review {{.Arg | default "the current changes"}}.`,
			Description: "Review a scope.",
			Arguments:   snippets.ArgumentsOptional,
		},
		"required": {
			Prompt:      "Review {{.Arg}}.",
			Description: "Review the named scope.",
			Arguments:   snippets.ArgumentsRequired,
		},
	}
	set, err := snippets.New(configured)
	if err != nil {
		t.Fatal(err)
	}
	wantUsages := []string{"//none", "//optional [args]", "//required <args>", "//help"}
	if got := set.Usages(); !slices.Equal(got, wantUsages) {
		t.Errorf("got usages %v, want %v", got, wantUsages)
	}
	entries := set.GetHelpEntries()
	if len(entries) != 4 || entries[1].Description != "Review a scope." {
		t.Errorf("got help entries %#v", entries)
	}

	registry, err := slash.NewRegistry(set)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"//none unexpected", "//required"} {
		invocation, found := registry.Find(input)
		if !found {
			t.Fatalf("expected %q to be registered", input)
		}
		if err := invocation.Command.Run(&snippetContext{}, invocation.Arguments); !slash.IsUsageError(err) {
			t.Errorf("%s got error %v, want usage error", input, err)
		}
	}
	for _, input := range []string{"//none", "//optional"} {
		invocation, found := registry.Find(input)
		if !found {
			t.Fatalf("expected %q to be registered", input)
		}
		if err := invocation.Command.Run(&snippetContext{}, invocation.Arguments); err != nil {
			t.Errorf("%s got error %v", input, err)
		}
	}
}

func TestAnUnsetArgumentPolicyIsReadFromTheTemplate(t *testing.T) {
	for name, want := range map[string]string{
		"Review the current changes.":              "//review",
		`Review {{.Arg | default "the current"}}.`: "//review [args]",
		`Review {{default "the current" .Arg}}.`:   "//review [args]",
		"Review {{.Arg}}.":                         "//review <args>",
		"Review {{index .Args 0}}.":                "//review <args>",
		"{{if .Args}}Review {{.Arg}}.{{end}}":      "//review <args>",
		"{{range .Args}}Review {{.}}.{{end}}":      "//review <args>",
		"Review {{.Arg}} {{/* a comment */}}.":     "//review <args>",
	} {
		t.Run(name, func(t *testing.T) {
			set, err := snippets.New(map[string]snippets.Definition{"review": {Prompt: name}})
			if err != nil {
				t.Fatal(err)
			}
			if got := set.Usages(); !slices.Equal(got, []string{want, "//help"}) {
				t.Errorf("got usages %v, want [%s //help]", got, want)
			}
		})
	}
}

func TestAnInferredPolicyIsEnforced(t *testing.T) {
	set, err := snippets.New(map[string]snippets.Definition{
		"none":     {Prompt: "Review the current changes."},
		"required": {Prompt: "Review {{.Arg}}."},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := slash.NewRegistry(set)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"//none the tests", "//required"} {
		invocation, found := registry.Find(input)
		if !found {
			t.Fatalf("expected %q to be registered", input)
		}
		if err := invocation.Command.Run(&snippetContext{}, invocation.Arguments); !slash.IsUsageError(err) {
			t.Errorf("%s got error %v, want usage error", input, err)
		}
	}
}

func TestHelpListsTheConfiguredSnippets(t *testing.T) {
	invocation := getInvocation(t, map[string]snippets.Definition{
		"note":   {Prompt: "Note this."},
		"review": {Prompt: "Review {{.Arg}}.", Description: "Review the named scope."},
	}, "//help")

	context := &snippetContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{
		"Snippets:",
		"  //note",
		"  //review <args>  Review the named scope.",
	}, "\n")
	if context.notice != want {
		t.Errorf("got help\n%s\nwant\n%s", context.notice, want)
	}
}

func TestHelpSaysWhenNoSnippetsAreConfigured(t *testing.T) {
	invocation := getInvocation(t, nil, "//help")

	context := &snippetContext{}
	if err := invocation.Command.Run(context, invocation.Arguments); err != nil {
		t.Fatal(err)
	}
	if context.notice != "No snippets are configured." {
		t.Errorf("got help %q", context.notice)
	}
}

func TestHelpTakesNoArguments(t *testing.T) {
	invocation := getInvocation(t, nil, "//help extra")

	if err := invocation.Command.Run(&snippetContext{}, invocation.Arguments); !slash.IsUsageError(err) {
		t.Errorf("got error %v, want usage error", err)
	}
}

func TestASnippetCannotBeNamedHelp(t *testing.T) {
	_, err := snippets.New(map[string]snippets.Definition{"help": {Prompt: "Help me."}})
	if err == nil || !strings.Contains(err.Error(), "//help") {
		t.Errorf("got %v", err)
	}
}

func TestAnEmptySnippetSetStillOwnsItsPrefix(t *testing.T) {
	set, err := snippets.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := set.Usages(); !slices.Equal(got, []string{"//help"}) {
		t.Errorf("got usages %v", got)
	}
	registry, err := slash.NewRegistry(set)
	if err != nil {
		t.Fatal(err)
	}
	name, found := registry.CommandName("//missing")
	if !found || name != "//missing" {
		t.Errorf("got command name %q and %t", name, found)
	}
}

func getInvocation(t *testing.T, configured map[string]snippets.Definition, input string) slash.Invocation {
	t.Helper()

	set, err := snippets.New(configured)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := slash.NewRegistry(set)
	if err != nil {
		t.Fatal(err)
	}
	invocation, found := registry.Find(input)
	if !found {
		t.Fatalf("expected %q to be registered", input)
	}
	return invocation
}
