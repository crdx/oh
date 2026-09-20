package startup

import (
	"errors"
	"strings"
	"testing"
)

func TestAPipedPromptIsReadWholeAndTrimmed(t *testing.T) {
	for name, piped := range map[string]struct {
		source string
		want   string
	}{
		"several lines": {source: "one\ntwo\n", want: "one\ntwo"},
		"a diff":        {source: "diff --git a/x b/x\nindex ab..cd 100644\n", want: "diff --git a/x b/x\nindex ab..cd 100644"},
		"blank lines":   {source: "\n\n", want: ""},
		"nothing":       {source: "", want: ""},
	} {
		got, err := ReadPipedPrompt(strings.NewReader(piped.source))
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if got != piped.want {
			t.Errorf("%s: got %q, want %q", name, got, piped.want)
		}
	}
}

type refusingReader struct{}

func (refusingReader) Read([]byte) (int, error) {
	return 0, errors.New("the pipe broke")
}

func TestAPipedPromptThatCannotBeReadIsReported(t *testing.T) {
	if _, err := ReadPipedPrompt(refusingReader{}); err == nil {
		t.Error("expected the unreadable pipe to be reported")
	}
}

func TestAJoinedPromptKeepsABlankLineBetweenItsParts(t *testing.T) {
	for name, joining := range map[string]struct {
		prompt   string
		addition string
		want     string
	}{
		"both":              {prompt: "review this", addition: "one\ntwo", want: "review this\n\none\ntwo"},
		"the prompt only":   {prompt: "review this", addition: "", want: "review this"},
		"the addition only": {prompt: "", addition: "one\ntwo", want: "one\ntwo"},
		"neither":           {prompt: "", addition: "", want: ""},
	} {
		if got := JoinPrompt(joining.prompt, joining.addition); got != joining.want {
			t.Errorf("%s: got %q, want %q", name, got, joining.want)
		}
	}
}

func TestPipedPromptIsFencedWhenItFollowsAGivenPrompt(t *testing.T) {
	for name, joining := range map[string]struct {
		prompt      string
		pipedPrompt string
		want        string
	}{
		"both": {
			prompt:      "review this",
			pipedPrompt: "one\ntwo",
			want:        "review this\n\n```\none\ntwo\n```",
		},
		"a distinctive language": {
			prompt:      "review this",
			pipedPrompt: "diff --git a/x b/x\n```",
			want:        "review this\n\n````diff\ndiff --git a/x b/x\n```\n````",
		},
		"the given prompt only": {prompt: "review this", pipedPrompt: "", want: "review this"},
		"the piped prompt only": {prompt: "", pipedPrompt: "one\ntwo", want: "one\ntwo"},
		"neither":               {prompt: "", pipedPrompt: "", want: ""},
	} {
		if got := JoinPipedPrompt(joining.prompt, joining.pipedPrompt); got != joining.want {
			t.Errorf("%s: got %q, want %q", name, got, joining.want)
		}
	}
}
