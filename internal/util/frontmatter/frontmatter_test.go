package frontmatter_test

import (
	"errors"
	"testing"

	"crdx.org/oh/internal/util/frontmatter"
)

func TestSplit(t *testing.T) {
	for name, test := range map[string]struct {
		input         string
		wantHeader    string
		wantBody      string
		wantIsPresent bool
		wantErr       error
	}{
		"no frontmatter":         {input: "Just a body.", wantBody: "Just a body."},
		"empty input":            {input: ""},
		"header and body":        {input: "---\na: b\n---\nBody.\n", wantHeader: "a: b", wantBody: "Body.\n", wantIsPresent: true},
		"empty header":           {input: "---\n---\nBody.", wantBody: "Body.", wantIsPresent: true},
		"no body":                {input: "---\na: b\n---", wantHeader: "a: b", wantIsPresent: true},
		"byte order mark":        {input: "\xef\xbb\xbf---\na: b\n---\nBody.", wantHeader: "a: b", wantBody: "Body.", wantIsPresent: true},
		"carriage returns":       {input: "---\r\na: b\r\n---\r\nBody.", wantHeader: "a: b\r", wantBody: "Body.", wantIsPresent: true},
		"delimiter not first":    {input: "Body.\n---\na: b\n---\n", wantBody: "Body.\n---\na: b\n---\n"},
		"unterminated":           {input: "---\na: b\nBody.", wantBody: "---\na: b\nBody.", wantErr: frontmatter.ErrUnterminated},
		"later rule in the body": {input: "---\na: b\n---\nOne.\n---\nTwo.", wantHeader: "a: b", wantBody: "One.\n---\nTwo.", wantIsPresent: true},
	} {
		t.Run(name, func(t *testing.T) {
			header, body, isPresent, err := frontmatter.Split([]byte(test.input))
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("got error %v, want %v", err, test.wantErr)
			}
			if string(header) != test.wantHeader || string(body) != test.wantBody || isPresent != test.wantIsPresent {
				t.Errorf("got %q, %q, %v; want %q, %q, %v", header, body, isPresent, test.wantHeader, test.wantBody, test.wantIsPresent)
			}
		})
	}
}
