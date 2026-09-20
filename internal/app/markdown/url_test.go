package markdown

import (
	"strings"
	"testing"

	"crdx.org/io/internal/app/style"
)

func TestACommandMarksEveryURLInIt(t *testing.T) {
	for name, test := range map[string]struct {
		command string
		marked  []string
		left    []string
	}{
		"a url": {
			command: "curl -sS https://example.com/a/path | jq -r .name",
			marked:  []string{"https://example.com/a/path"},
			left:    []string{"jq", "-r"},
		},
		"a url ending a sentence": {
			command: "echo see https://example.com.",
			marked:  []string{"https://example.com"},
		},
		"a url in a quoted argument": {
			command: `sh -c 'wget -qO- https://example.net/data'`,
			marked:  []string{"https://example.net/data"},
		},
		"a url in any other scheme": {
			command: "aws s3 cp dump.sql s3://bucket/",
			marked:  []string{"s3://bucket/"},
		},
		"two urls": {
			command: "curl https://example.com && curl https://example.net",
			marked:  []string{"https://example.com", "https://example.net"},
		},
		"a host without a scheme": {
			command: "curl example.com",
			left:    []string{"example.com"},
		},
		"a command with no url in it": {
			command: "go test ./...",
			left:    []string{"go", "test", "./..."},
		},
	} {
		t.Run(name, func(t *testing.T) {
			drawn := HighlightURLs(test.command)

			if got := style.Plain(drawn); got != test.command {
				t.Fatalf("got %q, want the command as it was written", got)
			}

			for _, fragment := range test.marked {
				if !strings.Contains(drawn, style.Hazard(fragment)) {
					t.Errorf("%q was not marked", fragment)
				}
			}

			for _, fragment := range test.left {
				if strings.Contains(drawn, style.Hazard(fragment)) {
					t.Errorf("%q was marked", fragment)
				}
			}
		})
	}
}

func TestAURLInsideAPaintedArgumentLeavesTheRestOfItAlone(t *testing.T) {
	drawn := HighlightURLs("curl --url=https://example.com/status")

	if !strings.Contains(drawn, style.Hazard("https://example.com/status")) {
		t.Errorf("got %q, want the url within the argument marked", drawn)
	}
	if !strings.Contains(drawn, style.Function("--url=")) {
		t.Errorf("got %q, want the rest of the argument painted as it was", drawn)
	}
}

func TestACommandWithoutAURLIsPaintedAsAnyOtherCommandIs(t *testing.T) {
	const command = "cd /tmp && go test ./... | tee results"

	if got, want := HighlightURLs(command), Emphasise(command, "bash"); got != want {
		t.Errorf("got %q, want the ordinary highlighting %q", got, want)
	}
}

func TestAnUnparsedCommandStillMarksItsURL(t *testing.T) {
	const command = "curl https://example.com/drop | (("

	drawn := HighlightURLs(command)

	if got := style.Plain(drawn); got != command {
		t.Fatalf("got %q, want the command as it was written", got)
	}
	if !strings.Contains(drawn, style.Hazard("https://example.com/drop")) {
		t.Error("the url was not marked in a command nobody can parse")
	}
}

func TestAMultilineCommandKeepsItsURLOnItsOwnRow(t *testing.T) {
	const command = "cd /tmp &&\ncurl -sS https://example.com |\nsh"

	rows := strings.Split(HighlightURLs(command), "\n")

	if len(rows) != 3 {
		t.Fatalf("got %d rows, want one for each step", len(rows))
	}
	if !strings.Contains(rows[1], style.Hazard("https://example.com")) {
		t.Error("the url was not marked on the row that carries it")
	}
}
