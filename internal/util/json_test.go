package util_test

import (
	"encoding/json"
	"testing"

	"crdx.org/oh/internal/util"
)

func TestJSONScalar(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "absent", raw: "", want: ""},
		{name: "string", raw: `"rate_limit_exceeded"`, want: "rate_limit_exceeded"},
		{name: "empty string", raw: `""`, want: ""},
		{name: "integer", raw: "400", want: "400"},
		{name: "float", raw: "1.5", want: "1.5"},
		{name: "negative", raw: "-1", want: "-1"},
		{name: "true", raw: "true", want: "true"},
		{name: "false", raw: "false", want: "false"},
		{name: "null", raw: "null", want: ""},
		{name: "object", raw: `{"code":400}`, want: ""},
		{name: "array", raw: `[400]`, want: ""},
		{name: "malformed", raw: "{", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := util.JSONScalar(json.RawMessage(test.raw)); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}
