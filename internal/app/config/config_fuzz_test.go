package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func requireADrawableHostname(t *testing.T, config Config) {
	t.Helper()

	hostname := config.Ports.GetHostname("session", "127.0.0.1")

	for _, character := range hostname {
		if !isHostnameCharacter(character) {
			t.Fatalf("%q was accepted as a hostname, and holds %q", hostname, character)
		}
	}
}

func FuzzAConfigFileIsReadWithoutFallingOver(fuzzer *testing.F) {
	for _, seed := range []string{
		"",
		"[ui.theme]\nnormal = \"#010203 bold\"\n",
		"[ui.theme]\nnormal = \"underline:curly\"\n",
		"[ui]\nstreaming = \"asap\"\ngrouping = [\"answer\"]\n",
		"[bar.top]\nleft = [{ name = \"model\" }]\n",
		"[sandbox]\nread = [\"~/x\"]\n",
		"[permissions]\nnetwork = \"ask\"\n",
		"[snippets]\nx = \"y\"\n",
		"[tool]\noutput = \"12K\"\n",
		"[caps]\ndefault = \"rwxng\"\n",
		"[model]\nround_robin = [\"a\", \"b\"]\n",
		"[ports]\nhostname = \"{session}\"\n",
		"[ports]\nhostname = \"\x1b[2J{session}\"\n",
		"[experimental]\nx = 1\n",
	} {
		fuzzer.Add(seed)
	}

	for _, seed := range []string{
		"[ui.theme]\naccent = \"#040506 faint\"\n",
		"[bar.bottom]\nright = []\n",
		"[skills]\ninclude = [\"x\"]\n",
	} {
		fuzzer.Add(seed)
	}

	directory := fuzzer.TempDir()

	fuzzer.Fuzz(func(t *testing.T, body string) {
		if len(body) > 4096 {
			t.Skip("longer than a config is ever written")
		}

		path := filepath.Join(directory, "config.toml")
		if err := os.WriteFile(path, []byte(fmt.Sprintf("version = %d\n", Format)+body), 0o600); err != nil {
			t.Fatal(err)
		}

		overridePath := filepath.Join(directory, "oh.toml")
		if err := os.WriteFile(overridePath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		config, err := Load(path)
		requireASoundConfig(t, config, err)

		overridden, err := LoadSources(
			Source{Path: path},
			Source{Path: overridePath, IsOverride: true},
		)
		requireASoundConfig(t, overridden, err)
	})
}

func requireASoundConfig(t *testing.T, config Config, err error) {
	t.Helper()

	if err != nil {
		return
	}

	requireADrawableHostname(t, config)

	_, _ = config.BuildLayout(testSegments())
	_, _ = config.BuildPermissions()
	_, _ = config.BuildLive(testSegments())
	_ = config.UnknownSettings()
}
