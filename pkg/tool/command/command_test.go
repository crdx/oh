package command_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/command"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "reporter")
	//nolint:gosec // a tool the test declares has to be runnable
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body+"\n"), 0o700); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return path
}

func echoingDeclaration(t *testing.T) command.Declaration {
	t.Helper()

	return command.Declaration{
		Name:        "deploy",
		Description: "deploy the site to the named environment",
		Command:     []string{writeScript(t, `printf '%s\n' "$@"`)},
		Parameters: []command.Parameter{
			{
				Name:        "environment",
				Kind:        command.KindEnum,
				Description: "where to deploy to",
				Values:      []string{"staging", "live"},
			},
			{
				Name:        "dry_run",
				Kind:        command.KindBoolean,
				Description: "whether to stop short of deploying",
				IsOptional:  true,
			},
			{
				Name:        "retries",
				Kind:        command.KindInteger,
				Description: "how many times to try again",
				IsOptional:  true,
			},
			{
				Name:        "tag",
				Kind:        command.KindStrings,
				Description: "the tags to deploy",
				IsOptional:  true,
			},
		},
	}
}

func call(t *testing.T, subject tool.Tool, arguments string) (string, error) {
	t.Helper()

	parsed, err := subject.Parse(arguments)
	if err != nil {
		return "", err
	}

	result, err := parsed.Exec(t.Context())

	return result.Output, err
}

func TestADeclarationCarriesItsNameDescriptionAndSchema(t *testing.T) {
	subject, err := command.New(echoingDeclaration(t), command.Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if subject.Name() != "deploy" {
		t.Errorf("got name %q", subject.Name())
	}
	if subject.Description() != "deploy the site to the named environment" {
		t.Errorf("got description %q", subject.Description())
	}

	encoded, err := json.Marshal(subject.Schema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, wanted := range []string{
		`"enum":["staging","live"]`,
		`"dry_run":{"type":"boolean"`,
		`"retries":{"type":"integer"`,
		`"tag":{"type":"array","description":"the tags to deploy","items":{"type":"string"}}`,
		`"required":["environment"]`,
	} {
		if !strings.Contains(string(encoded), wanted) {
			t.Errorf("schema %s does not contain %s", encoded, wanted)
		}
	}
}

func TestEverySuppliedParameterBecomesAnOptionInDeclarationOrder(t *testing.T) {
	subject, err := command.New(echoingDeclaration(t), command.Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := call(t, subject, `{"environment":"live","dry_run":true,"retries":2,"tag":["one","two"]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wanted := strings.Join([]string{
		"--environment", "live",
		"--dry-run",
		"--retries", "2",
		"--tag", "one",
		"--tag", "two",
	}, "\n")

	if output != wanted {
		t.Errorf("got %q, wanted %q", output, wanted)
	}
}

func TestAnAbsentParameterAndAFalseBooleanContributeNothing(t *testing.T) {
	subject, err := command.New(echoingDeclaration(t), command.Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := call(t, subject, `{"environment":"staging","dry_run":false}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "--environment\nstaging" {
		t.Errorf("got %q", output)
	}
}

func TestAnArgumentIsOneWordHoweverItIsWritten(t *testing.T) {
	declaration := echoingDeclaration(t)
	declaration.Parameters = []command.Parameter{
		{Name: "message", Kind: command.KindString, Description: "what to say"},
	}

	subject, err := command.New(declaration, command.Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := call(t, subject, `{"message":"; rm -rf / # $(whoami) 'quoted'"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output != "--message\n; rm -rf / # $(whoami) 'quoted'" {
		t.Errorf("got %q", output)
	}
}

func TestACommandThatFailsReportsItsExitCodeAndOutput(t *testing.T) {
	declaration := echoingDeclaration(t)
	declaration.Command = []string{writeScript(t, "echo nope >&2; exit 3")}
	declaration.Parameters = nil
	declaration.Subject = ""

	subject, err := command.New(declaration, command.Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := call(t, subject, `{}`)
	if !errors.Is(err, command.ErrCommandFailed) {
		t.Fatalf("got %v", err)
	}
	if output != "exit(3):\nnope" {
		t.Errorf("got %q", output)
	}
}

func TestTheCommandRunsInTheDirectoryItWasGiven(t *testing.T) {
	directory := t.TempDir()
	declaration := echoingDeclaration(t)
	declaration.Command = []string{writeScript(t, "pwd")}
	declaration.Parameters = nil

	subject, err := command.New(declaration, command.Options{Directory: directory})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output, err := call(t, subject, `{}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != resolved {
		t.Errorf("got %q, wanted %q", output, resolved)
	}
}

func TestTheApprovalSeesTheWholeCommandAndCanRefuseIt(t *testing.T) {
	refusal := errors.New("the user said no")
	var askedName, asked string

	declaration := echoingDeclaration(t)
	declaration.MustAsk = true

	subject, err := command.New(declaration, command.Options{
		Approve: func(_ context.Context, name string, command string) error {
			askedName = name
			asked = command
			return refusal
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := call(t, subject, `{"environment":"live","tag":["a b"]}`); !errors.Is(err, refusal) {
		t.Fatalf("got %v", err)
	}
	if !strings.HasSuffix(asked, "--environment live --tag 'a b'") {
		t.Errorf("got %q", asked)
	}
	if askedName != "deploy" {
		t.Errorf("got %q", askedName)
	}
}

func TestAToolTheDeclarationDoesNotGateIsNeverAskedAbout(t *testing.T) {
	subject, err := command.New(echoingDeclaration(t), command.Options{
		Approve: func(context.Context, string, string) error {
			t.Error("the tool was asked about")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := call(t, subject, `{"environment":"live"}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestACallTheSchemaRefusesNeverReachesTheCommand(t *testing.T) {
	declaration := echoingDeclaration(t)
	declaration.MustAsk = true

	subject, err := command.New(declaration, command.Options{
		Approve: func(context.Context, string, string) error {
			t.Error("the command was approved")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := call(t, subject, `{"environment":"moon"}`); err == nil ||
		err.Error() != "environment must be one of: staging, live" {
		t.Errorf("got %v", err)
	}
}

func TestACallIsHeadedByItsSubjectAndTrailedByTheRest(t *testing.T) {
	declaration := echoingDeclaration(t)
	declaration.Subject = "environment"

	subject, err := command.New(declaration, command.Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parsed, err := subject.Parse(`{"environment":"live","retries":2}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if parsed.Subject() != "live" {
		t.Errorf("got subject %q", parsed.Subject())
	}
	if parsed.Qualifier() != "2" {
		t.Errorf("got qualifier %q", parsed.Qualifier())
	}
}

func TestABooleanIsNamedRatherThanValuedInACallRow(t *testing.T) {
	declaration := echoingDeclaration(t)
	declaration.Subject = "environment"

	subject, err := command.New(declaration, command.Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for arguments, wanted := range map[string]string{
		`{"environment":"live","dry_run":true}`:  "dry_run",
		`{"environment":"live","dry_run":false}`: "",
	} {
		parsed, err := subject.Parse(arguments)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.Qualifier() != wanted {
			t.Errorf("got qualifier %q, wanted %q", parsed.Qualifier(), wanted)
		}
	}
}

func TestADeclarationIsRefusedForTheReasonItIsUnusable(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*command.Declaration)
		wording string
	}{
		{"no name", func(d *command.Declaration) { d.Name = "" }, `"" is not a usable tool name`},
		{"shouting name", func(d *command.Declaration) { d.Name = "Deploy" }, `"Deploy" is not a usable tool name`},
		{"no description", func(d *command.Declaration) { d.Description = " " }, "description is empty"},
		{"no command", func(d *command.Declaration) { d.Command = nil }, "command is empty"},
		{
			"missing command",
			func(d *command.Declaration) { d.Command = []string{"/nowhere/at/all"} },
			"could not find /nowhere/at/all",
		},
		{
			"unknown subject",
			func(d *command.Declaration) { d.Subject = "nothing" },
			"subject names nothing, which is not a parameter",
		},
		{
			"unknown kind",
			func(d *command.Declaration) { d.Parameters[0].Kind = "number" },
			`environment has kind "number", which is not one of: string, integer, boolean, strings, enum`,
		},
		{
			"enum without values",
			func(d *command.Declaration) { d.Parameters[0].Values = nil },
			"environment is an enum, so it needs values",
		},
		{
			"values on a plain kind",
			func(d *command.Declaration) { d.Parameters[2].Values = []string{"one"} },
			"retries is not an enum, so it takes no values",
		},
		{
			"no parameter description",
			func(d *command.Declaration) { d.Parameters[1].Description = "" },
			"dry_run has no description",
		},
		{
			"repeated parameter",
			func(d *command.Declaration) { d.Parameters[1].Name = "environment" },
			"environment is declared twice",
		},
		{
			"unusable parameter name",
			func(d *command.Declaration) { d.Parameters[1].Name = "dry run" },
			`"dry run" is not a usable parameter name`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			declaration := echoingDeclaration(t)
			test.change(&declaration)

			_, err := command.New(declaration, command.Options{})
			if err == nil || !strings.Contains(err.Error(), test.wording) {
				t.Errorf("got %v, wanted %s", err, test.wording)
			}
		})
	}
}
