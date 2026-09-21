package tool_test

import (
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/pkg/tool"
)

func declaredSchema() tool.Schema {
	return tool.Schema{
		tool.String("path", "the file to read"),
		tool.Integer("limit", "how many lines to return").Optional(),
		tool.Boolean("recursive", "whether to descend").Optional(),
		tool.StringArray("names", "the jobs to watch").Optional(),
		tool.Enum("network", "which network to use", "loopback", "host").Optional(),
	}
}

func TestEveryDeclaredTypeDecodesIntoItsOwnAccessor(t *testing.T) {
	arguments, err := declaredSchema().Decode(
		`{"path":"a.go","limit":12,"recursive":true,"names":["one","two"],"network":"host"}`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := arguments.GetString("path"); got != "a.go" {
		t.Errorf("got path %q", got)
	}
	if got := arguments.GetInteger("limit"); got != 12 {
		t.Errorf("got limit %d", got)
	}
	if !arguments.GetBoolean("recursive") {
		t.Error("expected recursive to be true")
	}
	if got := arguments.GetStrings("names"); !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("got names %q", got)
	}
	if got := arguments.GetString("network"); got != "host" {
		t.Errorf("got network %q", got)
	}
}

func TestAMissingRequiredParameterIsRefusedByName(t *testing.T) {
	_, err := declaredSchema().Decode(`{"limit":3}`)
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Errorf("got %v", err)
	}
}

func TestAMissingOptionalParameterIsAbsentAndReadsAsItsZeroValue(t *testing.T) {
	arguments, err := declaredSchema().Decode(`{"path":"a.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if arguments.IsPresent("limit") {
		t.Error("expected limit to be absent")
	}
	if got := arguments.GetInteger("limit"); got != 0 {
		t.Errorf("got limit %d", got)
	}
	if !arguments.IsPresent("path") {
		t.Error("expected path to be present")
	}
}

func TestANullValueCountsAsAnAbsentParameter(t *testing.T) {
	arguments, err := declaredSchema().Decode(`{"path":"a.go","limit":null}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if arguments.IsPresent("limit") {
		t.Error("expected limit to be absent")
	}
}

func TestANullRequiredParameterIsRefused(t *testing.T) {
	_, err := declaredSchema().Decode(`{"path":null}`)
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Errorf("got %v", err)
	}
}

func TestAValueOfTheWrongTypeNamesTheShapeItWanted(t *testing.T) {
	for _, test := range []struct {
		arguments string
		wording   string
	}{
		{`{"path":7}`, "path must be a string, not 7"},
		{`{"path":"a.go","limit":"12"}`, `limit must be an integer, not "12"`},
		{`{"path":"a.go","limit":1.5}`, "limit must be an integer, not 1.5"},
		{`{"path":"a.go","recursive":"yes"}`, `recursive must be true or false, not "yes"`},
		{`{"path":"a.go","names":"one"}`, `names must be a list of strings, not "one"`},
	} {
		t.Run(test.wording, func(t *testing.T) {
			_, err := declaredSchema().Decode(test.arguments)
			if err == nil || err.Error() != test.wording {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestAValueOutsideAnEnumIsRefusedWithTheWholeChoice(t *testing.T) {
	_, err := declaredSchema().Decode(`{"path":"a.go","network":"carrier-pigeon"}`)
	if err == nil || err.Error() != "network must be one of: loopback, host" {
		t.Errorf("got %v", err)
	}
}

func TestAParameterTheSchemaDoesNotDeclareIsRefused(t *testing.T) {
	_, err := declaredSchema().Decode(`{"path":"a.go","colour":"red"}`)
	if err == nil || err.Error() != "colour is not a parameter of this tool" {
		t.Errorf("got %v", err)
	}
}

func TestArgumentsThatAreNotAnObjectAreRefused(t *testing.T) {
	_, err := declaredSchema().Decode(`["a.go"]`)
	if err == nil || !strings.Contains(err.Error(), "could not parse the arguments") {
		t.Errorf("got %v", err)
	}
}

func TestAnEmptySchemaTakesEmptyArguments(t *testing.T) {
	arguments, err := tool.Schema{}.Decode("  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(arguments.Schema()) != 0 {
		t.Errorf("got schema %v", arguments.Schema())
	}
}

func TestEveryDeclaredTypeReadsAsText(t *testing.T) {
	arguments, err := declaredSchema().Decode(
		`{"path":"a.go","limit":12,"recursive":true,"names":["one","two"]}`,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for name, want := range map[string]string{
		"path":      "a.go",
		"limit":     "12",
		"recursive": "true",
		"names":     "one two",
		"network":   "",
	} {
		if got := arguments.GetText(name); got != want {
			t.Errorf("got %s as %q, wanted %q", name, got, want)
		}
	}
}

func TestReadingAParameterAsTheWrongTypeIsAProgrammingError(t *testing.T) {
	arguments, err := declaredSchema().Decode(`{"path":"a.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer func() {
		recovered, isText := recover().(string)
		if !isText || recovered != "path is string, not integer" {
			t.Errorf("got %v", recovered)
		}
	}()

	arguments.GetInteger("path")
}

func TestReadingAParameterNobodyDeclaredIsAProgrammingError(t *testing.T) {
	arguments, err := declaredSchema().Decode(`{"path":"a.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer func() {
		recovered, isText := recover().(string)
		if !isText || recovered != "no parameter named colour" {
			t.Errorf("got %v", recovered)
		}
	}()

	arguments.GetString("colour")
}
