package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		version = 3
		concurrency = 4
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
	if tools[0].Revision() != "3" {
		t.Errorf("got version %q", tools[0].Revision())
	}
	if !tools[0].Concurrent() {
		t.Error("configured concurrency did not make the tool concurrent")
	}
}

func TestACustomToolConcurrencyMustBePositive(t *testing.T) {
	config, _ := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather"
		command = ["./forecast"]
		concurrency = -1
	`)

	if _, err := config.BuildCustomTools(command.Options{}); err == nil || !strings.Contains(err.Error(), "concurrency is not a positive integer") {
		t.Errorf("got %v", err)
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

func TestCustomToolsCanShareAModeGroup(t *testing.T) {
	config, _ := configWithACustomTool(t, `
		[defaults]
		caps = "rxa"

		[tools.weather]
		version = 2
		description = "report the weather for a city"
		command = ["true"]
		group = "a"

		[tools.forecast]
		description = "forecast the weather"
		command = ["true"]
		group = "a"
	`)

	groups, err := config.CustomToolGroups()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(groups["a"], ","); got != "forecast,weather" {
		t.Errorf("got grouped tools %q", got)
	}
	declaration, err := config.declare("weather")
	if err != nil {
		t.Fatal(err)
	}
	if declaration.Version != 2 {
		t.Errorf("got version %d", declaration.Version)
	}
	if _, grantedGroups, err := config.ParseCaps(string(config.Defaults.Caps)); err != nil {
		t.Fatal(err)
	} else if grantedGroups != "a" {
		t.Errorf("got granted groups %q", grantedGroups)
	}
}

func TestACustomToolCanEnableItsModeGroupByDefault(t *testing.T) {
	config, _ := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather"
		command = ["true"]
		group = "c"
		enabled = true

		[tools.forecast]
		description = "forecast the weather"
		command = ["true"]
		group = "d"
		enabled = true
	`)

	if got := config.DefaultToolGroupFlags(nil); got != "cd" {
		t.Errorf("got default groups %q", got)
	}
	if got := config.DefaultToolGroupFlags([]string{"weather"}); got != "c" {
		t.Errorf("got selected default groups %q", got)
	}
	if got := config.DefaultToolGroupFlags([]string{"read"}); got != "" {
		t.Errorf("got unselected default groups %q", got)
	}
}

func TestASessionCanPreserveAnUngroupedCustomTool(t *testing.T) {
	config, _ := configWithACustomTool(t, `
		[tools.weather]
		description = "report the weather"
		command = ["true"]
		group = "a"
	`)

	tools, err := config.BuildCustomTools(command.Options{
		GroupForTool: func(string) string { return "" },
	})
	if err != nil {
		t.Fatal(err)
	}
	call, err := tools[0].Parse("{}")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call.Exec(t.Context()); err != nil {
		t.Errorf("the session's ungrouped tool was refused: %v", err)
	}
}

func TestACustomToolGroupIsOneLowercaseLetter(t *testing.T) {
	for _, group := range []string{"ab", "A", "é"} {
		t.Run(group, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "config.toml")
			body := "[tools.weather]\ndescription = \"report the weather\"\ncommand = [\"true\"]\ngroup = \"" + group + "\"\n"
			if err := writeConfigFile(path, body); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "is not one lowercase letter") {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestACustomToolAsksUnlessItSaysOtherwise(t *testing.T) {
	tests := map[string]struct {
		written         string
		mustAsk         bool
		approvalTimeout time.Duration
	}{
		"omitted":         {mustAsk: true},
		"ask":             {written: `"ask"`, mustAsk: true},
		"allow":           {written: `"allow"`, mustAsk: false},
		"allow table":     {written: `{ rule = "allow" }`, mustAsk: false},
		"ask table":       {written: `{ rule = "ask" }`, mustAsk: true},
		"timed ask table": {written: `{ rule = "ask", timeout = "5m" }`, mustAsk: true, approvalTimeout: 5 * time.Minute},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			body := `
				[tools.weather]
				description = "report the weather for a city"
				command = ["true"]
			`
			if test.written != "" {
				body += "permission = " + test.written + "\n"
			}

			config, _ := configWithACustomTool(t, body)
			declaration, err := config.declare("weather")
			if err != nil {
				t.Fatal(err)
			}
			if declaration.MustAsk != test.mustAsk {
				t.Errorf("got must ask %v", declaration.MustAsk)
			}
			if declaration.ApprovalTimeout != test.approvalTimeout {
				t.Errorf("got approval timeout %s", declaration.ApprovalTimeout)
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

func TestAnInvalidCustomToolPermissionTableIsRefused(t *testing.T) {
	tests := map[string]struct {
		written string
		want    string
	}{
		"missing rule": {
			written: `{ timeout = "5m" }`,
			want:    "rule is missing",
		},
		"empty rule": {
			written: `{ rule = "" }`,
			want:    "rule is empty",
		},
		"invalid rule": {
			written: `{ rule = "whenever" }`,
			want:    `must be "ask" or "allow"`,
		},
		"timeout with allow": {
			written: `{ rule = "allow", timeout = "5m" }`,
			want:    `timeout applies only when rule is "ask"`,
		},
		"invalid timeout": {
			written: `{ rule = "ask", timeout = "later" }`,
			want:    "invalid duration",
		},
		"non-duration timeout": {
			written: `{ rule = "ask", timeout = 5 }`,
			want:    "timeout is not a duration",
		},
		"zero timeout": {
			written: `{ rule = "ask", timeout = "0s" }`,
			want:    "timeout must be positive",
		},
		"unknown field": {
			written: `{ rule = "ask", waiting = "5m" }`,
			want:    "unknown: waiting",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "config.toml")
			body := "[tools.weather]\ndescription = \"report the weather\"\ncommand = [\"true\"]\npermission = " + test.written + "\n"
			if err := writeConfigFile(path, body); err != nil {
				t.Fatal(err)
			}

			settings, err := Load(path)
			if err == nil {
				_, err = settings.BuildCustomTools(command.Options{})
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("got %v", err)
			}
		})
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
