package toolresult

import (
	"flag"
	"strings"
	"testing"

	"crdx.org/io/internal/app/style"
	internaltoolresult "crdx.org/io/internal/toolresult"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/session"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestAnInternalRequestIsParsed(t *testing.T) {
	for name, test := range map[string]struct {
		arguments []string
		want      Request
	}{
		"direct": {arguments: []string{"--tool-result", "oh://tool-result?call=one"}, want: Request{URL: "oh://tool-result?call=one"}},
		"pager":  {arguments: []string{"--tool-result", "oh://tool-result?call=one", "--pager"}, want: Request{URL: "oh://tool-result?call=one", ShouldPage: true}},
	} {
		t.Run(name, func(t *testing.T) {
			request, isRequested, err := ParseRequest(test.arguments)
			if err != nil || !isRequested || request != test.want {
				t.Errorf("got %+v, requested %v, error %v", request, isRequested, err)
			}
		})
	}
}

func TestAnInternalRequestMustHaveItsExactShape(t *testing.T) {
	for _, arguments := range [][]string{
		{"--tool-result"},
		{"--tool-result", "--pager"},
		{"--tool-result", "oh://tool-result?call=one", "--other"},
		{"--tool-result", "oh://tool-result?call=one", "--pager", "extra"},
	} {
		if _, isRequested, err := ParseRequest(arguments); !isRequested || err == nil {
			t.Errorf("arguments %q produced requested %v and error %v", arguments, isRequested, err)
		}
	}
}

func TestAnOrdinaryInvocationIsNotAnInternalRequest(t *testing.T) {
	if _, isRequested, err := ParseRequest([]string{"--help"}); isRequested || err != nil {
		t.Errorf("got requested %v and error %v", isRequested, err)
	}
}

func TestShowWritesSafeToolOutput(t *testing.T) {
	directory, address := toolResultFixture(t)

	var output strings.Builder
	err := show(address, false, directory, &output, nil)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	want := "read · notes.txt\n\none\ntwo\n"
	if style.Plain(output.String()) != want {
		t.Errorf("got output %q", output.String())
	}
	if strings.Contains(output.String(), "example.test") {
		t.Errorf("unsafe hyperlink survived in %q", output.String())
	}
}

func TestPagerReceivesSafeToolOutput(t *testing.T) {
	directory, address := toolResultFixture(t)
	pagedText := ""
	openPager := func(text string) error {
		pagedText = text
		return nil
	}

	var output strings.Builder
	err := show(address, true, directory, &output, openPager)
	if err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
	if style.Plain(pagedText) != "read · notes.txt\n\none\ntwo\n" {
		t.Errorf("paged text is %q", pagedText)
	}
	if output.Len() != 0 {
		t.Errorf("standard output contains %q", output.String())
	}
}

func toolResultFixture(t *testing.T) (string, string) {
	t.Helper()

	directory := t.TempDir()
	writer, err := session.Create(directory, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{
		Kind:      agent.ToolCallRequestEvent,
		ID:        "call-1",
		Name:      "read",
		Arguments: `{"path":"notes.txt"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{
		Kind:   agent.ToolCallResultEvent,
		ID:     "call-1",
		Name:   "read",
		Status: agent.SuccessStatus,
		Text:   "one\n\x1b]8;;https://example.test\x1b\\two\x1b]8;;\x1b\\\n",
	}); err != nil {
		t.Fatal(err)
	}
	name := writer.Name()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return directory, internaltoolresult.URL(name, "call-1")
}
