package onboarding

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"crdx.org/io/internal/app/config"
)

func FuzzAnInitialModelIsWrittenWhereItWillBeRead(fuzzer *testing.F) {
	for _, seed := range []string{
		"",
		"[model]\n",
		"[model] # models live here\n",
		"[editor]\ncommand = [\"vim\"]\n",
		"[snippets]\nx = \"\"\"\n[model]\n\"\"\"\n",
		"[snippets]\nx = '''\n[model]\n'''\n",
		"[snippets]\nx = \"a \\\"[model]\\\" word\"\n",
		"[ui]\ncurrency = \"GBP\"\n\n[model]\neffort = \"low\"\n",
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
		if err != nil || len(settings.Model.RoundRobin) > 0 {
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
		if len(settings.Model.RoundRobin) != 1 || settings.Model.RoundRobin[0] != "anthropic/one@high" {
			t.Fatalf("the config onboarding wrote names %q\n%s", settings.Model.RoundRobin, updated)
		}
	})
}
