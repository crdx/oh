package onboarding

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/config"
)

func FuzzAnInitialModelIsWrittenWhereItWillBeRead(fuzzer *testing.F) {
	for _, seed := range []string{
		"",
		"[agent]\n",
		"[agent] # models live here\n",
		"[model]\n",
		"[editor]\ncommand = [\"vim\"]\n",
		"[snippets]\nx = \"\"\"\n[agent]\n\"\"\"\n",
		"[snippets]\nx = '''\n[agent]\n'''\n",
		"[snippets]\nx = \"a \\\"[agent]\\\" word\"\n",
		"[ui]\ncurrency = \"GBP\"\n\n[defaults]\neffort = \"low\"\n",
	} {
		fuzzer.Add(seed)
	}

	directory := fuzzer.TempDir()

	fuzzer.Fuzz(func(t *testing.T, body string) {
		if len(body) > 4096 {
			t.Skip("longer than a config is ever written")
		}

		path := filepath.Join(directory, "config.toml")
		contents := fmt.Sprintf("version = %d\n", config.Format) + body
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}

		settings, err := config.Load(path)
		if err != nil || len(settings.Agent.Rotation()) > 0 {
			return
		}

		updated := addInitialModel([]byte(contents), "anthropic/one@high")
		if err := os.WriteFile(path, updated, 0o600); err != nil {
			t.Fatal(err)
		}

		settings, err = config.Load(path)
		if err != nil {
			t.Fatalf("the config onboarding wrote does not load: %v\n%s", err, updated)
		}
		if settings.Agent.Model != "anthropic/one@high" {
			t.Fatalf("the config onboarding wrote names %q\n%s", settings.Agent.Model, updated)
		}
	})
}
