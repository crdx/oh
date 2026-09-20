package call

import (
	"testing"

	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

func TestModelScratchAliassAreShownThroughTheirHostAlias(t *testing.T) {
	label := Label{
		Subject:   "/tmp/io/main.go",
		Qualifier: "/tmp/io/detail.go",
		Emphasis:  tool.Emphasis{Source: "/tmp/io/main.go"},
		Continuation: []Label{{
			Subject: "/tmp/io/continued.go",
		}},
	}.WithHostPathAliases(link.Roots{Scratch: "/state/farm/session"})

	if label.Subject != "<s>/io/main.go" ||
		label.Qualifier != "<s>/io/detail.go" ||
		label.Emphasis.Source != "<s>/io/main.go" ||
		label.Continuation[0].Subject != "<s>/io/continued.go" {
		t.Errorf("got %#v, want every model scratch path shown through the host alias", label)
	}
}

func TestHostTmpPathsRemainLiteralWithoutAMappedScratch(t *testing.T) {
	label := Label{Subject: "/tmp/io/main.go"}.WithHostPathAliases(link.Roots{})

	if label.Subject != "/tmp/io/main.go" {
		t.Errorf("got %q, want the host path unchanged", label.Subject)
	}
}

func TestAContinuedCallHasItsPathPrefixesShortened(t *testing.T) {
	const workspaceDir = "/home/alice/project"
	rendering := agent.FallbackRendering{
		Continuation: []tool.CallRendering{{
			Name:      "bash",
			Subject:   workspaceDir + "/check",
			Qualifier: workspaceDir + "/detail",
			Emphasis:  tool.Emphasis{Source: workspaceDir + "/source"},
		}},
	}

	shortened := shortenPaths(rendering, work.At(workspaceDir))
	part := shortened.Continuation[0]
	if part.Subject != "check" || part.Qualifier != "detail" || part.Emphasis.Source != "source" {
		t.Errorf("got %#v, want every continuation path shortened", part)
	}
}

func TestWorkspacePathPrefixesAreShortened(t *testing.T) {
	const workspaceDir = "/home/alice/project"
	t.Setenv("HOME", "/home/alice")

	tests := map[string]string{
		workspaceDir:                      "",
		"~/project":                       "",
		workspaceDir + " **/*.go":         "**/*.go",
		"~/project **/*.go":               "**/*.go",
		workspaceDir + "/cmd/oh/draw.go":  "cmd/oh/draw.go",
		"~/project/cmd/oh/draw.go":        "cmd/oh/draw.go",
		"/home/alice/other.go":            "~/other.go",
		"/home/alice/projectile/other.go": "~/projectile/other.go",
	}
	for value, want := range tests {
		if got := shortenPathPrefix(value, work.At(workspaceDir)); got != want {
			t.Errorf("shortenPathPrefix(%q) = %q, want %q", value, got, want)
		}
	}
}
