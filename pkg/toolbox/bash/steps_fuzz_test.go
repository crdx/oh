package bash_test

import (
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"crdx.org/io/pkg/toolbox/bash"
)

func FuzzStepsMeanWhatTheModelSent(fuzzer *testing.F) {
	for _, seed := range []string{
		"",
		" ",
		"curl example.com",
		"cd /tmp && curl example.com",
		"a && b || c && d",
		"a|b&&c",
		"cat <<EOF > note\nbody\nEOF",
		"cat <<'EOF'\n$notexpanded\nEOF",
		"for f in *.go; do gofmt -w $f && echo $f; done",
		"if [ -f go.mod ]; then go build ./...; fi",
		"echo one; echo two",
		"echo 'quoted && word'",
		"echo \"double && word\"",
		"curl example.com && ",
		"&& echo",
		"echo $(date) && echo `date`",
		"echo ünïcödé && echo 日本語",
		"echo \x00 && echo",
		"echo \xff && echo",
		"a &&\n  b &&\n    c",
		"a \\\n && b",
		"a \\\n",
		"0 &&\v",
		"{ a; } && { b; }",
		"(a && b) || c",
		"a & b",
		"a && # comment\nb",
		"while read -r line; do echo \"$line\" && true; done < file",
	} {
		fuzzer.Add(seed)
	}

	fuzzer.Fuzz(func(t *testing.T, command string) {
		steps := bash.Steps(command)

		if len(steps) == 0 {
			t.Fatalf("Steps(%q) accounted for nothing", command)
		}

		for _, step := range steps {
			if strings.Trim(step, shellSpace) == "" && strings.Trim(command, shellSpace) != "" {
				t.Errorf("Steps(%q) produced a step nobody can read", command)
			}
		}

		drawn := strings.Join(steps, "\n")

		sent, err := canonical(command)
		if err != nil {
			if drawn != command {
				t.Errorf("Steps(%q) drew %q, want a command nobody can parse left alone", command, drawn)
			}

			return
		}

		drawnMeaning, err := canonical(drawn)
		if err != nil {
			t.Fatalf("Steps(%q) drew something no shell would take: %v", command, err)
		}

		if sent != drawnMeaning {
			t.Errorf("Steps(%q) drew %q, want it to mean %q", command, drawnMeaning, sent)
		}
	})
}

const shellSpace = " \t\n\r"

func canonical(command string) (string, error) {
	parsed, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil {
		return "", err
	}

	var out strings.Builder
	if err := syntax.NewPrinter(syntax.SingleLine(true)).Print(&out, parsed); err != nil {
		return "", err
	}

	return out.String(), nil
}
