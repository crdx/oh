package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/tool"
)

func TestOptionalParametersAreLeftOutOfRequired(t *testing.T) {
	schema := tool.Schema{
		tool.String("path", "path to the file"),
		tool.Integer("offset", "first line to return").Optional(),
	}

	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedJSON := `{"type":"object","properties":{` +
		`"offset":{"type":"integer","description":"first line to return"},` +
		`"path":{"type":"string","description":"path to the file"}},` +
		`"required":["path"],"additionalProperties":false}`

	if string(schemaJSON) != expectedJSON {
		t.Errorf("expected %s, got %s", expectedJSON, schemaJSON)
	}
}

func TestABooleanParameterHasABooleanSchema(t *testing.T) {
	schema := tool.Schema{
		tool.Boolean("networking", "whether to enable networking").Optional(),
	}

	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedJSON := `{"type":"object","properties":{` +
		`"networking":{"type":"boolean","description":"whether to enable networking"}},` +
		`"additionalProperties":false}`

	if string(schemaJSON) != expectedJSON {
		t.Errorf("expected %s, got %s", expectedJSON, schemaJSON)
	}
}

func TestAStringArrayDescribesItsItems(t *testing.T) {
	schema := tool.Schema{
		tool.StringArray("names", "jobs to watch"),
	}

	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedJSON := `{"type":"object","properties":{` +
		`"names":{"type":"array","description":"jobs to watch","items":{"type":"string"}}},` +
		`"required":["names"],"additionalProperties":false}`

	if string(schemaJSON) != expectedJSON {
		t.Errorf("expected %s, got %s", expectedJSON, schemaJSON)
	}
}

func TestASchemaWithNoParametersIsStillAnObject(t *testing.T) {
	schemaJSON, err := json.Marshal(tool.Schema{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedJSON := `{"type":"object","properties":{},"additionalProperties":false}`

	if string(schemaJSON) != expectedJSON {
		t.Errorf("expected %s, got %s", expectedJSON, schemaJSON)
	}
}

type titleArguments struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

func newDescriptionTool() tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "title",
			Description: "title the session",
			Schema: tool.Schema{
				tool.String("title", "the title"),
				tool.String("detail", "more detail").Optional(),
			},
		},
		func(titleArguments) (string, string) { return "", "" },
	).Plain(func(context.Context, titleArguments) (string, error) { return "", nil })
}

func TestUnparsedArgumentsKeepOnlyUsefulFallbackText(t *testing.T) {
	subject := newDescriptionTool()

	for _, test := range []struct {
		name      string
		arguments string
		want      string
	}{
		{"blank known argument", `{"title":""}`, ""},
		{"whitespace known argument", `{"title":" \n "}`, ""},
		{"several blank known arguments", `{"title":"","detail":" "}`, ""},
		{"missing arguments", `{}`, ""},
		{"null arguments", `null`, `null`},
		{"unknown argument", `{"unknown":""}`, `{"unknown":""}`},
		{"blank known and unknown arguments", `{"title":"","unknown":""}`, `{"title":"","unknown":""}`},
		{"non-string argument", `{"title":false}`, `false`},
		{"known and unknown arguments", `{"title":"useful","unknown":"hidden"}`, `useful`},
		{"malformed arguments", "{not json\nand more", `{not json`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := tool.DescribeUnparsedArguments(subject, test.arguments); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func FuzzDescribeUnparsedArguments(fuzzer *testing.F) {
	subject := newDescriptionTool()

	for _, arguments := range []string{
		`{"title":""}`,
		`{"title":"useful","unknown":"hidden"}`,
		`{"title":false}`,
		`{}`,
		`null`,
		"{not json\nand more",
	} {
		fuzzer.Add(arguments, "", "", "")
	}

	fuzzer.Fuzz(func(t *testing.T, arguments string, title string, detail string, unknown string) {
		got := tool.DescribeUnparsedArguments(subject, arguments)
		if strings.Contains(got, "\n") {
			t.Errorf("rendered more than one line: %q", got)
		}

		var decoded map[string]json.RawMessage
		if json.Unmarshal([]byte(arguments), &decoded) != nil {
			if want := strutil.FirstLine(arguments); got != want {
				t.Errorf("malformed arguments rendered as %q, want %q", got, want)
			}
		}

		type structuredArguments struct {
			Title   string `json:"title"`
			Detail  string `json:"detail"`
			Unknown string `json:"unknown,omitempty"`
		}
		structured, err := json.Marshal(structuredArguments{Title: title, Detail: detail, Unknown: unknown})
		if err != nil {
			t.Fatal(err)
		}

		var normalised structuredArguments
		if err := json.Unmarshal(structured, &normalised); err != nil {
			t.Fatal(err)
		}

		values := make([]string, 0, 2)
		for _, value := range []string{normalised.Title, normalised.Detail} {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}

		want := strutil.FirstLine(strings.Join(values, " "))
		if want == "" && normalised.Unknown != "" {
			want = strutil.FirstLine(string(structured))
		}
		if got := tool.DescribeUnparsedArguments(subject, string(structured)); got != want {
			t.Errorf("structured arguments %s rendered as %q, want %q", structured, got, want)
		}
	})
}
