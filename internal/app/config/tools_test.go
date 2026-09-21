package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/pkg/tool/command"
)

func writeRunnableFile(t *testing.T, path string) {
	t.Helper()

	//nolint:gosec // a tool the test supplies has to be runnable
	if err := os.WriteFile(path, []byte("#!/bin/bash\ntrue\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func configWithACustomTool(t *testing.T, body string) (Config, string) {
	t.Helper()

	directory := t.TempDir()
	writeRunnableFile(t, filepath.Join(directory, "forecast"))

	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, undent(body)); err != nil {
		t.Fatal(err)
	}

	config, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	return config, directory
}

func TestACustomToolBecomesAToolTheModelIsOffered(t *testing.T) {
	config, _ := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["./forecast"]
		parameters = [
			{ name = "city", kind = "string", description = "the city to report on" },
		]
	`)

	tools, err := config.BuildCustomTools(command.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if len(tools) != 1 {
		t.Fatalf("got %d tools", len(tools))
	}
	if tools[0].Name() != "weather" {
		t.Errorf("got name %q", tools[0].Name())
	}
	if tools[0].Description() != "report the weather for a city" {
		t.Errorf("got description %q", tools[0].Description())
	}
}

func TestACustomCommandIsResolvedAgainstTheConfigThatSuppliedIt(t *testing.T) {
	config, directory := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["./forecast", "--quietly"]
	`)

	declaration, err := config.declare("weather")
	if err != nil {
		t.Fatal(err)
	}

	wanted := []string{filepath.Join(directory, "forecast"), "--quietly"}
	if strings.Join(declaration.Command, " ") != strings.Join(wanted, " ") {
		t.Errorf("got %q, wanted %q", declaration.Command, wanted)
	}
}

func TestACustomCommandResolvesEveryPathItWritesAgainstTheConfig(t *testing.T) {
	config, directory := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["deno", "run", "--allow-sys", "./forecast", "--store", "./state.json"]
	`)

	declaration, err := config.declare("weather")
	if err != nil {
		t.Fatal(err)
	}

	wanted := []string{
		"deno", "run", "--allow-sys",
		filepath.Join(directory, "forecast"),
		"--store", filepath.Join(directory, "state.json"),
	}

	if got := strings.Join(declaration.Command, " "); got != strings.Join(wanted, " ") {
		t.Errorf("got %q, wanted %q", got, wanted)
	}
}

func TestACustomCommandLeavesAlonePlainWordsAndAbsolutePaths(t *testing.T) {
	config, _ := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["deno", "run", "--allow-read=/var/lib/weather", "/opt/forecast", "--fact", "uptime"]
	`)

	declaration, err := config.declare("weather")
	if err != nil {
		t.Fatal(err)
	}

	wanted := "deno run --allow-read=/var/lib/weather /opt/forecast --fact uptime"
	if got := strings.Join(declaration.Command, " "); got != wanted {
		t.Errorf("got %q, wanted %q", got, wanted)
	}
}

func TestACustomToolOnThePathIsLeftForThePathToFind(t *testing.T) {
	config, _ := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["true"]
	`)

	declaration, err := config.declare("weather")
	if err != nil {
		t.Fatal(err)
	}

	if declaration.Command[0] != "true" {
		t.Errorf("got %q", declaration.Command)
	}
}

func TestACustomToolAsksUnlessItSaysOtherwise(t *testing.T) {
	for written, mustAsk := range map[string]bool{"": true, "ask": true, "allow": false} {
		t.Run("permission "+written, func(t *testing.T) {
			body := `
				[tools.weather]
				description = "report the weather for a city"
				command = ["true"]
			`
			if written != "" {
				body += "permission = \"" + written + "\"\n"
			}

			config, _ := configWithACustomTool(t, body)
			declaration, err := config.declare("weather")
			if err != nil {
				t.Fatal(err)
			}
			if declaration.MustAsk != mustAsk {
				t.Errorf("got %v", declaration.MustAsk)
			}
		})
	}
}

func TestACustomToolNamesItsConfigWhenItCannotBeBuilt(t *testing.T) {
	config, _ := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["/nowhere/at/all"]
	`)

	_, err := config.BuildCustomTools(command.Options{})
	if err == nil || !strings.Contains(err.Error(), "tools.weather: could not find /nowhere/at/all") {
		t.Errorf("got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Errorf("the failure did not name the config: %v", err)
	}
}

func TestAnUnreadablePermissionIsRefusedByName(t *testing.T) {
	config, _ := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather for a city"
		command = ["true"]
		permission = "whenever"
	`)

	_, err := config.BuildCustomTools(command.Options{})
	if err == nil || !strings.Contains(err.Error(), "tools.weather: permission:") {
		t.Errorf("got %v", err)
	}
}

func TestAWorkspaceMayNotAddACustomTool(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(path, "[tools.weather]\ndescription = \"x\"\n"); err != nil {
		t.Fatal(err)
	}

	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[tools.weather]\ndescription = \"y\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSources(Source{Path: path}, Source{Path: overridePath, IsOverride: true})
	if err == nil || !strings.Contains(err.Error(), "cannot be overridden in oh.toml") {
		t.Errorf("got %v", err)
	}
}

func TestAWorkspaceMayNotOpenAToolTableAtAll(t *testing.T) {
	directory := t.TempDir()
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[tools.weather]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSources(Source{Path: overridePath, IsOverride: true})
	if err == nil || !strings.Contains(err.Error(), "cannot be overridden in oh.toml") {
		t.Errorf("got %v", err)
	}
}

func TestACustomToolReachesTheNextSession(t *testing.T) {
	if got := ReachOf("tools.weather"); got != ReachNextSession {
		t.Errorf("got reach %v", got)
	}
}
