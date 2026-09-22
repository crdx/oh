package world

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func undent(body string) string {
	lines := strings.Split(strings.TrimPrefix(body, "\n"), "\n")
	for at, line := range lines {
		lines[at] = strings.TrimPrefix(line, "\t\t")
	}

	return strings.Join(lines, "\n")
}

func writeWorld(t *testing.T, body string) string {
	t.Helper()

	directory := t.TempDir()
	writeRunnableFile(t, filepath.Join(directory, "forecast"))

	path := filepath.Join(directory, "world.toml")
	if err := os.WriteFile(path, []byte(undent(body)), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func writeRunnableFile(t *testing.T, path string) {
	t.Helper()

	//nolint:gosec // a tool the test supplies has to be runnable
	if err := os.WriteFile(path, []byte("#!/bin/bash\ntrue\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

const kitchen = `
		prompt = "You are the cook, and this kitchen is your world."

		[tools.weather]
		description = "report the weather for a city"
		command = ["./forecast", "--quietly"]
		parameters = [
		    { name = "city", kind = "string", description = "the city to report on" },
		]
`

func TestAWorldCarriesItsPromptAndTools(t *testing.T) {
	contents, err := Load(writeWorld(t, kitchen))
	if err != nil {
		t.Fatal(err)
	}

	if contents.Prompt.Text != "You are the cook, and this kitchen is your world." {
		t.Errorf("got %q", contents.Prompt.Text)
	}
	if got := contents.OfferedNames(); strings.Join(got, ",") != "weather" {
		t.Errorf("got %q", got)
	}
}

func TestAWorldResolvesEveryCommandPathAgainstItself(t *testing.T) {
	path := writeWorld(t, `
		prompt = "You are the cook."

		[tools.weather]
		description = "report the weather for a city"
		command = ["deno", "run", "--allow-sys", "./forecast", "--store", "./state.json"]
	`)

	declarations, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	built, err := declarations.Declarations()
	if err != nil {
		t.Fatal(err)
	}
	if len(built) != 1 {
		t.Fatalf("got %d declarations", len(built))
	}

	beside := filepath.Dir(path)
	wanted := []string{
		"deno", "run", "--allow-sys",
		filepath.Join(beside, "forecast"),
		"--store", filepath.Join(beside, "state.json"),
	}
	if got := built[0].Command; strings.Join(got, " ") != strings.Join(wanted, " ") {
		t.Errorf("got %q, wanted %q", got, wanted)
	}
}

func TestAWorldLeavesAlonePlainWordsAndAbsolutePaths(t *testing.T) {
	path := writeWorld(t, `
		prompt = "You are the cook."

		[tools.weather]
		description = "report the weather for a city"
		command = ["deno", "run", "--allow-read=/var/lib/weather", "/opt/forecast", "--fact", "uptime"]
	`)

	contents, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	built, err := contents.Declarations()
	if err != nil {
		t.Fatal(err)
	}

	wanted := "deno run --allow-read=/var/lib/weather /opt/forecast --fact uptime"
	if got := strings.Join(built[0].Command, " "); got != wanted {
		t.Errorf("got %q, wanted %q", got, wanted)
	}
}

func TestAnEntryWithNoCommandNamesAToolTheHarnessAlreadyOffers(t *testing.T) {
	contents, err := Load(writeWorld(t, `
		prompt = "You are the cook."
		files.root = "."

		[tools.weather]
		description = "report the weather for a city"
		command = ["./forecast"]

		[tools.read]
		[tools.notify]
	`))
	if err != nil {
		t.Fatal(err)
	}

	if got := contents.ReferenceNames(); strings.Join(got, ",") != "notify,read" {
		t.Errorf("got %q", got)
	}

	built, err := contents.Declarations()
	if err != nil {
		t.Fatal(err)
	}
	if len(built) != 1 || built[0].Name != "weather" {
		t.Errorf("a reference was declared as a command of its own: %v", built)
	}
}

func TestAnEntryNamingAnOfferedToolMaySayNothingElse(t *testing.T) {
	for setting, body := range map[string]string{
		"description": `description = "something else"`,
		"subject":     `subject = "query"`,
		"timeout":     `timeout = "5s"`,
		"permission":  `permission = "allow"`,
		"parameters":  `parameters = [{ name = "x", kind = "string", description = "x" }]`,
	} {
		t.Run(setting, func(t *testing.T) {
			path := writeWorld(t, `
				prompt = "You are the cook."

				[tools.notify]
			`+"\t\t\t\t"+body+"\n")

			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), setting+": not allowed when naming a built-in tool") {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestAWorldRefusesAToolThatWantsASandbox(t *testing.T) {
	for _, name := range sandboxBoundTools {
		t.Run(name, func(t *testing.T) {
			path := writeWorld(t, `
				prompt = "You are the cook."

				[tools.`+name+`]
			`)

			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), "sandbox unavailable") {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestAWorldCarriesItsPromptAsTextOrAsAFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "cook.md"), []byte("  You are the cook.  \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(directory, "world.toml")
	body := "prompt = { file = \"cook.md\" }\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	contents, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if contents.Prompt.Text != "You are the cook." {
		t.Errorf("got %q", contents.Prompt.Text)
	}
}

func TestAnUnusablePromptIsRefusedForTheReasonItIsUnusable(t *testing.T) {
	for name, test := range map[string]struct{ body, wording string }{
		"none":          {``, "prompt: required"},
		"empty text":    {`prompt = "  "`, "empty"},
		"table no file": {`prompt = { other = "x" }`, "table requires a file key"},
		"not text":      {`prompt = 7`, "expected a string or a table with a file key"},
		"missing file":  {`prompt = { file = "nowhere.md" }`, "prompt.file"},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeWorld(t, test.body+"\n")
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), test.wording) {
				t.Errorf("got %v, wanted %s", err, test.wording)
			}
		})
	}
}

func TestAWorldOfferingAPathToolNamesARoot(t *testing.T) {
	path := writeWorld(t, `
		prompt = "You are the cook."

		[tools.read]
		[tools.write]
	`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "files.root: required by read, write") {
		t.Errorf("got %v", err)
	}
}

func TestAWorldResolvesItsRootAgainstItself(t *testing.T) {
	path := writeWorld(t, `
		prompt = "You are the cook."
		files = { root = "kitchen", writable = true }

		[tools.read]
	`)

	contents, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if wanted := filepath.Join(filepath.Dir(path), "kitchen"); contents.Files.Root != wanted {
		t.Errorf("got %q, wanted %q", contents.Files.Root, wanted)
	}
	if !contents.Files.IsWritable {
		t.Error("the root was not writable")
	}
}

func TestAWorldCarriesNoSettingItDoesNotKnow(t *testing.T) {
	path := writeWorld(t, `
		prompt = "You are the cook."
		version = 10

		[ui]
		streaming = "asap"
	`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown: ui, ui.streaming, version") {
		t.Errorf("got %v", err)
	}
}

func TestAWorldThatIsNotThereIsRefused(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nowhere.toml"))
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("got %v", err)
	}
}
