package tool_test

import (
	"context"
	"errors"
	"testing"

	"crdx.org/oh/pkg/tool"
)

type Params struct {
	City string `json:"city"`
}

func TestOutputMetrics(t *testing.T) {
	for name, test := range map[string]struct {
		output string
		lines  int64
		bytes  int64
	}{
		"empty":            {output: "", lines: 0, bytes: 0},
		"one line":         {output: "hello", lines: 1, bytes: 5},
		"multiple lines":   {output: "hello\nworld", lines: 2, bytes: 11},
		"trailing newline": {output: "hello\nworld\n", lines: 2, bytes: 12},
	} {
		t.Run(name, func(t *testing.T) {
			got := tool.GetMetrics(test.output)
			want := tool.ToolCallMetrics{
				Kind:  tool.MetricOutput,
				Lines: test.lines,
				Bytes: test.bytes,
			}
			if got != want {
				t.Errorf("got %#v, want %#v", got, want)
			}
		})
	}
}

func newToolBuilder(t *testing.T) tool.Builder[Params] {
	t.Helper()

	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(args Params) tool.CallRendering {
			return tool.CallRendering{Subject: args.City, Qualifier: ""}
		},
	)
}

func newTool(t *testing.T, ran *bool) tool.Tool {
	t.Helper()

	return newToolBuilder(t).Plain(func(_ context.Context, args Params) (string, error) {
		*ran = true
		return "raining in " + args.City, nil
	})
}

func TestParseBindsTheArgumentsToTheCall(t *testing.T) {
	var didRun bool

	call, err := newTool(t, &didRun).Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if subject := call.Rendering().Subject; subject != "London" {
		t.Errorf("expected the bound arguments, got %q", subject)
	}

	if didRun {
		t.Error("expected rendering not to run the tool")
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output != "raining in London" {
		t.Errorf("expected the output, got %q", result.Output)
	}

	if !didRun {
		t.Error("expected the tool to have run")
	}
}

func TestARequiredAccessIsConsultedWhenTheCallExecutes(t *testing.T) {
	withheld := errors.New("access is not granted")
	isGranted := false
	didRun := false

	definedTool := newToolBuilder(t).
		Requires(func() bool { return isGranted }, withheld).
		Plain(func(_ context.Context, _ Params) (string, error) {
			didRun = true
			return "raining", nil
		})

	call, err := definedTool.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, withheld) {
		t.Errorf("expected the withheld error, got %v", err)
	}

	if didRun {
		t.Error("expected the tool not to have run")
	}

	isGranted = true

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output != "raining" || !didRun {
		t.Errorf("expected the tool to have run, got %q", result.Output)
	}
}

func TestParseDescribesTheCallOnce(t *testing.T) {
	descriptions := 0
	definedTool := tool.Implement(
		tool.Definition{Name: "weather", Schema: tool.Schema{tool.String("city", "the city to look up")}},
		func(args Params) tool.CallRendering {
			descriptions++
			return tool.CallRendering{Subject: args.City, Qualifier: "today"}
		},
	).Plain(func(_ context.Context, _ Params) (string, error) {
		return "", nil
	})

	call, err := definedTool.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject := call.Rendering().Subject; subject != "London" {
		t.Errorf("got subject %q, want London", subject)
	}
	if qualifier := call.Rendering().Qualifier; qualifier != "today" {
		t.Errorf("got qualifier %q, want today", qualifier)
	}
	if descriptions != 1 {
		t.Errorf("described call %d times, want 1", descriptions)
	}
}

func TestAnOuterEmphasisReplacesAnInnerOne(t *testing.T) {
	subject := newToolBuilder(t).
		Focuses(func(tool.CallRendering) string { return "London" }).
		Syntax("bash").
		Plain(func(_ context.Context, args Params) (string, error) {
			return "raining in " + args.City, nil
		})

	call, err := subject.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := tool.Emphasis{Kind: tool.EmphasisSyntax, Value: "bash"}
	if got := call.Rendering().Emphasis; got != want {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestSyntaxCanUseDecodedArgumentsAsItsSource(t *testing.T) {
	subject := newToolBuilder(t).
		SyntaxFrom("bash", func(args Params, subject string) string {
			return args.City + "\n" + subject
		}).
		Plain(func(_ context.Context, _ Params) (string, error) { return "", nil })

	call, err := subject.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := call.Rendering().Emphasis.Source, "London\nLondon"; got != want {
		t.Errorf("got source %q, want %q", got, want)
	}
}

func TestParseRefusesArgumentsItCannotRead(t *testing.T) {
	var didRun bool

	call, err := newTool(t, &didRun).Parse(`{"city":`)
	if err == nil {
		t.Fatal("expected malformed arguments to be refused")
	}

	if call != nil {
		t.Error("expected no call to be handed back")
	}

	if didRun {
		t.Error("expected the tool never to run")
	}
}

func newOptionalCityBuilder() tool.Builder[Params] {
	return tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up").Optional()},
		},
		func(args Params) tool.CallRendering { return tool.CallRendering{Subject: args.City} },
	)
}

func TestParseTakesAbsentArgumentsAsEmpty(t *testing.T) {
	call, err := newOptionalCityBuilder().Plain(func(context.Context, Params) (string, error) {
		return "", nil
	}).Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if subject := call.Rendering().Subject; subject != "" {
		t.Errorf("expected nothing rendered, got %q", subject)
	}
}

func TestValidationRunsForAbsentArguments(t *testing.T) {
	validationError := errors.New("city is required")
	wasValidated := false
	subject := newOptionalCityBuilder().Validate(func(args Params) error {
		wasValidated = true
		if args.City != "" {
			t.Errorf("expected empty arguments, got %#v", args)
		}
		return validationError
	}).Plain(func(context.Context, Params) (string, error) {
		return "", nil
	})

	call, err := subject.Parse("")
	if !errors.Is(err, validationError) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if !wasValidated {
		t.Error("empty arguments bypassed validation")
	}
	if call != nil {
		t.Error("validation failure produced a call")
	}
}

func TestDefineMetricsValidatesDecodedArgumentsWhenAsked(t *testing.T) {
	validationError := errors.New("London is unavailable")
	wasRendered := false
	wasExecuted := false

	subject := tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(_ Params) tool.CallRendering {
			wasRendered = true
			return tool.CallRendering{}
		},
	).Validate(func(args Params) error {
		if args.City != "London" {
			t.Fatalf("expected decoded arguments, got %#v", args)
		}
		return validationError
	}).Exec(func(_ context.Context, _ Params) (string, tool.ToolCallMetrics, error) {
		wasExecuted = true
		return "", tool.ToolCallMetrics{}, nil
	})

	call, err := subject.Parse(`{"city":"London"}`)
	if !errors.Is(err, validationError) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if call != nil {
		t.Error("expected validation to prevent call construction")
	}
	if wasRendered || wasExecuted {
		t.Error("expected validation not to render or execute the call")
	}
}

func TestDefineMetricsDoesNotRequireValidation(t *testing.T) {
	subject := tool.Implement(
		tool.Definition{
			Name:        "weather",
			Description: "report weather in a city",
			Schema:      tool.Schema{tool.String("city", "the city to look up")},
		},
		func(args Params) tool.CallRendering {
			return tool.CallRendering{Subject: args.City, Qualifier: ""}
		},
	).Exec(func(_ context.Context, args Params) (string, tool.ToolCallMetrics, error) {
		return args.City, tool.ToolCallMetrics{}, nil
	})

	call, err := subject.Parse(`{"city":"London"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject := call.Rendering().Subject; subject != "London" {
		t.Errorf("expected the bound arguments, got %q", subject)
	}
}

func TestParseRefusesArgumentsTheSchemaDoesNotDeclare(t *testing.T) {
	for name, test := range map[string]struct {
		arguments string
		want      string
	}{
		"an unknown parameter":    {arguments: `{"city":"London","country":"England"}`, want: "unknown parameter: country"},
		"a missing parameter":     {arguments: `{}`, want: "city is required"},
		"absent arguments":        {arguments: "", want: "city is required"},
		"a value of another type": {arguments: `{"city":7}`, want: "city must be a string, not 7"},
	} {
		t.Run(name, func(t *testing.T) {
			var didRun bool

			call, err := newTool(t, &didRun).Parse(test.arguments)
			if err == nil || err.Error() != test.want {
				t.Errorf("got %v, want %q", err, test.want)
			}
			if call != nil || didRun {
				t.Error("expected no call to be made")
			}
		})
	}
}

func TestParseRefusesAValueOutsideAnEnum(t *testing.T) {
	type Forecast struct {
		Sky string `json:"sky"`
	}

	subject := tool.Implement(
		tool.Definition{
			Name:   "forecast",
			Schema: tool.Schema{tool.Enum("sky", "the sky to expect", "clear", "cloudy")},
		},
		func(args Forecast) tool.CallRendering { return tool.CallRendering{Subject: args.Sky} },
	).Plain(func(context.Context, Forecast) (string, error) { return "", nil })

	if _, err := subject.Parse(`{"sky":"green"}`); err == nil || err.Error() != "sky must be one of: clear, cloudy" {
		t.Errorf("got %v, want the value refused", err)
	}
}
