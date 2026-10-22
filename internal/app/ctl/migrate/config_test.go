package migrate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/ctl/migrate"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/style"
)

func currentVersionLine() string {
	return fmt.Sprintf("version = %d", config.Format)
}

func backupPath(path string) string {
	return fmt.Sprintf("%s.pre-v%d", path, config.Format)
}

func configFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestTheFirstConfigMigrationBuildsARoundRobinSelection(t *testing.T) {
	original := `# provider = "anthropic"
# model = "claude-opus-5"
# effort = "medium"

provider = "codex"
model = "gpt-5.6-sol"
effort = "medium"

[skills]
include = ["skills"]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.InitialFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, preserved := range []string{
		`# provider = "anthropic"`,
		`# model = "claude-opus-5"`,
		`# effort = "medium"`,
		`include = ["skills"]`,
	} {
		if !strings.Contains(written, preserved) {
			t.Errorf("migration dropped %q from:\n%s", preserved, written)
		}
	}

	var decoded struct {
		Version int `toml:"version"`
		Agent   struct {
			Model string `toml:"model"`
		} `toml:"agent"`
	}
	metadata, err := toml.Decode(written, &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != config.Format {
		t.Errorf("got version %d, want %d", decoded.Version, config.Format)
	}
	if decoded.Agent.Model != "codex/gpt-5.6-sol@medium" {
		t.Errorf("got agent model %q", decoded.Agent.Model)
	}
	for _, key := range []string{"provider", "effort"} {
		if metadata.IsDefined(key) {
			t.Errorf("legacy key %s survived", key)
		}
	}

	backup, err := os.ReadFile(backupPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Errorf("backup changed:\n%s", backup)
	}
}

func TestAnUnnumberedRoundRobinConfigMigratesThroughEveryFormat(t *testing.T) {
	path := configFile(t, "[model]\nround_robin = [\"anthropic/claude-opus-5@high\"]\n")

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	if strings.Count(written, "[agent]") != 1 || !strings.Contains(written, currentVersionLine()) {
		t.Errorf("unexpected migrated config:\n%s", written)
	}
}

func TestTheFirstNumberedConfigMigratesThroughEveryFormat(t *testing.T) {
	path := configFile(t, "version = 1\nmodel = \"gpt\"\neffort = \"high\"\n")

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "version =") != 1 || !strings.Contains(string(body), currentVersionLine()) {
		t.Errorf("unexpected migrated config:\n%s", body)
	}
}

func TestConfigMigrationUsesTheLegacyDefaultSelectionParts(t *testing.T) {
	path := configFile(t, "model = \"gpt-5.6-sol\"\n")

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `model = "codex/gpt-5.6-sol@high"`) {
		t.Errorf("legacy defaults were not carried forward:\n%s", body)
	}
}

func TestTheSecondConfigMigrationRenamesSegments(t *testing.T) {
	original := `version = 2 # the round-robin format
label = "current-session"

[bar.top]
left = [
    { segment = "current-session" },
    { segment='current-time', format = "15:04" },
]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.RoundRobinFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, expected := range []string{
		currentVersionLine() + " # the round-robin format",
		`label = "current-session"`,
		`segment = "session-name"`,
		`segment='local-time'`,
	} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
	for _, legacy := range []string{`segment = "current-session"`, `segment='current-time'`} {
		if strings.Contains(written, legacy) {
			t.Errorf("migration kept %q in:\n%s", legacy, written)
		}
	}

	backup, err := os.ReadFile(backupPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Errorf("backup changed:\n%s", backup)
	}
}

func TestTheThirdConfigMigrationMovesTheEditorIntoItsTable(t *testing.T) {
	original := `version = 3
editor = ["subl", "--wait"] # the editor

[model]
round_robin = ["codex/gpt@high"]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.SegmentNamesFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, expected := range []string{
		currentVersionLine(),
		"[editor]",
		`command = ["subl", "--wait"] # the editor`,
		`model = "codex/gpt@high"`,
	} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
	if strings.Contains(written, "editor =") {
		t.Errorf("migration kept the top-level editor key in:\n%s", written)
	}

	backup, err := os.ReadFile(backupPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Errorf("backup changed:\n%s", backup)
	}
}

func TestTheThirdConfigMigrationKeepsAStringEditor(t *testing.T) {
	path := configFile(t, "version = 3\neditor = \"subl\"\n")

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	if !strings.Contains(written, "[editor]\ncommand = \"subl\"") {
		t.Errorf("the string editor was not carried into the table:\n%s", written)
	}
}

func TestTheFourthConfigMigrationKeepsStringSnippets(t *testing.T) {
	original := `version = 4
[snippets]
review = "Review {{ .Arg }}"
`
	path := configFile(t, original)

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	if !strings.Contains(written, currentVersionLine()) ||
		!strings.Contains(written, `review = "Review {{ .Arg }}"`) {
		t.Errorf("got config:\n%s", written)
	}
}

func TestTheFourthConfigMigrationPreservesRichSnippets(t *testing.T) {
	original := `version = 4
[snippets]
ask = { prompt = "Ask {{ .Arg }}", description = "Ask a question", arguments = "required" }
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.EditorCommandFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}
	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	if !strings.Contains(written, currentVersionLine()) || !strings.Contains(written, `ask = { prompt =`) {
		t.Errorf("unexpected migrated config:\n%s", written)
	}
}

func TestTheFifthConfigFormatRemovesTpsSegments(t *testing.T) {
	original := `version = 5
[bar.top]
left = [
    { segment = "activity-spinner" },
    { segment = "last-tps" }, # obsolete
]
center = [{ segment = 'last-tps' }, { segment = "context-usage" }]
right = [{ segment = "working-directory" }, { segment = "last-tps" }]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.SnippetDefinitionFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, expected := range []string{currentVersionLine(), "activity-spinner", "context-usage", "workspace-dir"} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
	if strings.Contains(written, "last-tps") || strings.Contains(written, "obsolete") {
		t.Errorf("migration kept the TPS segment:\n%s", written)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("migrated config cannot be loaded: %v", err)
	}
}

func TestTheSixthConfigFormatRenamesTheBarSegments(t *testing.T) {
	original := `version = 6
[bar.bottom]
left = [
    { segment = "turn-elapsed" },
    { segment='working-directory' },
]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.RetiredTpsFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, expected := range []string{currentVersionLine(), `segment = "turn-timer"`, `segment='workspace-dir'`} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
	for _, legacy := range []string{"turn-elapsed", "working-directory"} {
		if strings.Contains(written, legacy) {
			t.Errorf("migration kept %q in:\n%s", legacy, written)
		}
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("migrated config cannot be loaded: %v", err)
	}
}

func TestTheSeventhConfigFormatMakesRoomForTheOllamaHost(t *testing.T) {
	original := `version = 7
[model]
round_robin = ["codex/gpt@high"]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.TurnTimerFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	if !strings.Contains(written, currentVersionLine()) || !strings.Contains(written, `model = "codex/gpt@high"`) {
		t.Errorf("unexpected migrated config:\n%s", written)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("migrated config cannot be loaded: %v", err)
	}
}

func TestTheEighthConfigMigrationMovesTheNudgeIntoTheInputTable(t *testing.T) {
	original := `version = 8
get_on_with_it_message = "carry on" # what an empty line sends

[model]
round_robin = ["codex/gpt@high"]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.OllamaHostFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, expected := range []string{
		currentVersionLine(),
		"[input]",
		`nudge = "carry on" # what an empty line sends`,
		`model = "codex/gpt@high"`,
	} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
	if strings.Contains(written, "get_on_with_it_message") {
		t.Errorf("migration kept the old key in:\n%s", written)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("migrated config cannot be loaded: %v", err)
	}

	backup, err := os.ReadFile(backupPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Errorf("backup changed:\n%s", backup)
	}
}

func TestTheEighthConfigMigrationLeavesAConfigWithoutTheMessageAlone(t *testing.T) {
	path := configFile(t, "version = 8\n[model]\nround_robin = [\"codex/gpt@high\"]\n")

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	if strings.Contains(written, "[input]") {
		t.Errorf("migration invented an input table in:\n%s", written)
	}
	if !strings.Contains(written, currentVersionLine()) {
		t.Errorf("migration left the version behind in:\n%s", written)
	}
}

func TestCurrentConfigIsLeftAlone(t *testing.T) {
	original := currentVersionLine() + "\n[agent]\nmodel = \"codex/gpt@high\"\n"
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.Format {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Errorf("current config changed:\n%s", body)
	}
	if _, err := os.Stat(backupPath(path)); !os.IsNotExist(err) {
		t.Errorf("current config kept an unexpected copy: %v", err)
	}
}

func TestConfigMigrationDryRunWritesNothing(t *testing.T) {
	original := "provider = \"codex\"\nmodel = \"gpt\"\neffort = \"high\"\n"
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.InitialFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Errorf("dry run changed the config:\n%s", body)
	}
	if _, err := os.Stat(backupPath(path)); !os.IsNotExist(err) {
		t.Errorf("dry run kept a copy: %v", err)
	}
}

func TestConfigFromANewerBuildIsRefused(t *testing.T) {
	path := configFile(t, "version = 99\n")

	_, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err == nil || !strings.Contains(err.Error(), "upgrade oh") {
		t.Fatalf("expected a newer-format error, got %v", err)
	}
}

func TestConfigMigrationDoesNotOverwriteItsCopy(t *testing.T) {
	path := configFile(t, "model = \"gpt\"\n")
	if err := os.WriteFile(backupPath(path), []byte("held"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err == nil || !strings.Contains(err.Error(), "move it aside") {
		t.Fatalf("expected the existing copy to stop migration, got %v", err)
	}
}

func TestAMissingConfigNeedsNoMigration(t *testing.T) {
	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{
		Path: filepath.Join(t.TempDir(), "missing.toml"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if isPresent || from != config.Format {
		t.Errorf("got present %t from format %d", isPresent, from)
	}
}

func TestTheNinthConfigMigrationRenamesTheStreamKey(t *testing.T) {
	original := `version = 9

[ui]
stream = "paced" # how an answer is laid out

[bar.top]
left = [{ segment = "jobs" }]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.ContinueMessageFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, expected := range []string{
		currentVersionLine(),
		`streaming = "paced" # how an answer is laid out`,
		`left = [{ segment = "tasks" }]`,
	} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
	if strings.Contains(written, "stream =") {
		t.Errorf("migration kept the old key in:\n%s", written)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("migrated config cannot be loaded: %v", err)
	}
	if loaded.Ui.StreamingMode != output.StreamingModePaced {
		t.Errorf("got streaming mode %d after migrating, want paced", loaded.Ui.StreamingMode)
	}

	backup, err := os.ReadFile(backupPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Errorf("backup changed:\n%s", backup)
	}
}

func TestTheTenthConfigMigrationRenamesTheContinueMessageToNudge(t *testing.T) {
	original := `version = 10

[input]
continue = "carry on" # what a double enter sends

[model]
round_robin = ["codex/gpt@high"]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.StreamingNameFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, expected := range []string{
		currentVersionLine(),
		`nudge = "carry on" # what a double enter sends`,
		`model = "codex/gpt@high"`,
	} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
	if strings.Contains(written, "continue =") {
		t.Errorf("migration kept the old key in:\n%s", written)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("migrated config cannot be loaded: %v", err)
	}
	if loaded.Input.Nudge != "carry on" {
		t.Errorf("got nudge %q after migrating", loaded.Input.Nudge)
	}

	backup, err := os.ReadFile(backupPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Errorf("backup changed:\n%s", backup)
	}
}

func TestTheNinthConfigMigrationLeavesAStreamKeyInAnotherTableAlone(t *testing.T) {
	path := configFile(t, `version = 9

[snippets.stream]
prompt = "say it"

[ui]
stream = "asap"
`)

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if written := string(body); !strings.Contains(written, "[snippet.stream]") {
		t.Errorf("migration disturbed another table in:\n%s", written)
	}
}

func TestTheEleventhConfigMigrationNestsToolThemesAndRenamesExposeToForward(t *testing.T) {
	original := `version = 11

[bar.top]
left = [
    { segment = "jobs" },
    { segment = "exposed-ports" },
]

[bar.bottom]
left = [{ segment = "path-grants", type = "base" }]

[ui.theme.tool]
skill = { name = "consult-chart", paint = "#e6a8ff bold", focus = "#e6a8ff" }
job = { name = "tasks" }
job_start = { name = "launch", paint = "status_info" } # starting
job_wait_any.name = "hold"
bash_host_network = { paint = "status_danger" }
expose = { name = "share" }
expose_add = { name = "open-gangway", paint = "status_warning" } # opening
expose_remove.name = "close-gangway"
deploy = { name = "ship" }

[ui.theme.tool.expose_list]
name = "peek"

[ui.theme.tool.read]
name = "look"

[model]
round_robin = ["codex/gpt@high"]
`
	path := configFile(t, original)

	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.NudgeFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, expected := range []string{
		currentVersionLine(),
		`{ segment = "forwards" }`,
		`{ segment = "grants", type = "base" }`,
		`skill.default = { name = "consult-chart", paint = "#e6a8ff bold", focus = "#e6a8ff" }`,
		`job.default = { name = "tasks" }`,
		`job.start = { name = "launch", paint = "status_info" } # starting`,
		`job.wait_any.name = "hold"`,
		`bash.host_network = { paint = "status_danger" }`,
		`forward.default = { name = "share" }`,
		`forward.add = { name = "open-gangway", paint = "status_warning" } # opening`,
		`forward.remove.name = "close-gangway"`,
		`deploy.default = { name = "ship" }`,
		`[ui.theme.tool.forward.list]`,
		`[ui.theme.tool.read.default]`,
		`model = "codex/gpt@high"`,
	} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
	for _, legacy := range []string{`exposed-ports`, `path-grants`, `expose`, `job_start`, `job_wait_any`, `bash_host_network`, "\nskill =", "\njob ="} {
		if strings.Contains(written, legacy) {
			t.Errorf("migration kept %q in:\n%s", legacy, written)
		}
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("migrated config cannot be loaded: %v", err)
	}
	resolved := loaded.Ui.Theme.Tool.Resolved()
	for kind, want := range map[string]struct{ name, paint string }{
		"skill":             {name: "consult-chart", paint: "#e6a8ff bold"},
		"job":               {name: "tasks", paint: ""},
		"job_start":         {name: "launch", paint: "status_info"},
		"job_wait_any":      {name: "hold", paint: ""},
		"bash_host_network": {name: "$", paint: "status_danger"},
		"forward":           {name: "share", paint: "status_warning"},
		"forward_add":       {name: "open-gangway", paint: "status_warning"},
		"forward_remove":    {name: "close-gangway", paint: "status_warning"},
		"forward_list":      {name: "peek", paint: "normal"},
		"read":              {name: "look", paint: ""},
		"deploy":            {name: "ship", paint: ""},
	} {
		if got := resolved[kind]; string(got.Name) != want.name || string(got.Paint) != want.paint {
			t.Errorf("%s resolves to %+v after migrating, want %+v", kind, got, want)
		}
	}

	backup, err := os.ReadFile(backupPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Errorf("backup changed:\n%s", backup)
	}
}

func TestTheEleventhConfigMigrationLeavesEverythingElseAlone(t *testing.T) {
	original := `version = 11

[bar.top]
left = [{ segment = 'exposed-ports' }, { segment = "jobs" }, { segment = 'path-grants' }]

[tools.expose]
command = "expose-thing"

[other]
expose_add = { name = "kept" }
job_start = { name = "kept" }
`
	path := configFile(t, original)

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	written := string(body)
	for _, expected := range []string{
		currentVersionLine(),
		`{ segment = 'forwards' }`,
		`{ segment = 'grants' }`,
		"[tool.expose]\ncommand = \"expose-thing\"",
		"[other]\nexpose_add = { name = \"kept\" }\njob_start = { name = \"kept\" }",
	} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
}

func TestTheEleventhConfigMigrationOnlyRaisesTheVersionWhereNothingNeedsRenaming(t *testing.T) {
	original := "version = 11 # current\n\n[ui]\ncurrency = \"gbp\"\n"
	path := configFile(t, original)

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if want := currentVersionLine() + " # current\n\n[ui]\ncurrency = \"gbp\"\n"; string(body) != want {
		t.Errorf("got:\n%s\nwant:\n%s", body, want)
	}
}

func TestAMigratedConfigDrawsTheForwardsAndGrantsSegments(t *testing.T) {
	original := "version = 11\n\n[bar.top]\nleft = [{ segment = \"exposed-ports\" }, { segment = \"path-grants\" }]\n"
	path := configFile(t, original)

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if left := fmt.Sprint(loaded.Bar.Top.Left); !strings.Contains(left, "forwards") || !strings.Contains(left, "grants") {
		t.Errorf("got the top left of the bar %+v", loaded.Bar.Top.Left)
	}
}

func migratedConfig(t *testing.T, original string) string {
	t.Helper()

	path := configFile(t, original)
	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}

func TestTheTwelfthConfigMigrationRenamesATableOfColoursToTheDarkPalette(t *testing.T) {
	written := migratedConfig(t, `version = 12

# my colours
[ui.theme] # warm
dim = "#7d756c"
accent = "#d8a566 bold"

[ui.theme.tool.job]
default = { paint = "accent" }
`)

	want := currentVersionLine() + `

# my colours
[ui.theme.dark] # warm
dim = "#7d756c"
accent = "#d8a566 bold"

[ui.theme.tool.job]
default = { paint = "accent" }
`
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheTwelfthConfigMigrationMovesColoursOutOfATableTheToolsShare(t *testing.T) {
	written := migratedConfig(t, `version = 12

[ui.theme]
  accent = "#010203"
tool.job.default = { paint = "accent" }
user = "#040506"
`)

	want := currentVersionLine() + `

[ui.theme]
  dark.accent = "#010203"
tool.job.default = { paint = "accent" }
dark.user = "#040506"
`
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheTwelfthConfigMigrationMovesDottedColours(t *testing.T) {
	written := migratedConfig(t, `version = 12
ui.theme.accent = "#010203"

[ui]
theme.dim = "#040506"
currency = "gbp"
`)

	want := currentVersionLine() + `
ui.theme.dark.accent = "#010203"

[ui]
theme.dark.dim = "#040506"
currency = "gbp"
`
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheTwelfthConfigMigrationLeavesCommentsAndOtherTablesAlone(t *testing.T) {
	original := `version = 12

[ui]
currency = "gbp"

# [ui.theme]
# dim = "#7d756c"

[other]
accent = "kept"
`
	written := migratedConfig(t, original)

	if want := strings.Replace(original, "version = 12", currentVersionLine(), 1); written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheTwelfthConfigMigrationRefusesColoursItCannotMove(t *testing.T) {
	original := "version = 12\n\n[ui]\ntheme = { accent = \"#010203\" }\n"
	path := configFile(t, original)

	_, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err == nil || !strings.Contains(err.Error(), "[ui.theme.dark] by hand") {
		t.Fatalf("got %v, want a refusal naming what to do", err)
	}
	if body, _ := os.ReadFile(path); string(body) != original { //nolint:gosec // the test's own path
		t.Errorf("a refused migration rewrote the config:\n%s", body)
	}
}

func TestAMigratedThemeIsDrawnInTheDarkPaletteAlone(t *testing.T) {
	path := configFile(t, "version = 12\n\n[ui.theme]\naccent = \"#010203\"\n")
	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Ui.Theme.Dark.Accent; got != "#010203" {
		t.Errorf("got dark accent %q", got)
	}
	if got, want := loaded.Ui.Theme.Light.Accent, style.DefaultTheme().Light.Accent; got != want {
		t.Errorf("got light accent %q, want the default %q", got, want)
	}
	if unknown := loaded.UnknownSettings(); len(unknown) > 0 {
		t.Errorf("the migrated config has unknown settings: %v", unknown)
	}
}

func TestTheThirteenthConfigMigrationTurnsASingleSelectionIntoTheAgentModel(t *testing.T) {
	written := migratedConfig(t, `version = 13

# what to talk to
[model] # models
round_robin = [ "anthropic/claude-opus-5@high" ] # the usual

[ui]
currency = "gbp"
`)

	want := currentVersionLine() + `

# what to talk to
[agent] # models
model = "anthropic/claude-opus-5@high" # the usual

[ui]
currency = "gbp"
`
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheThirteenthConfigMigrationMovesARotationOutOfTheDefaults(t *testing.T) {
	written := migratedConfig(t, `version = 13

[model]
effort = "medium"
round_robin = [
    "anthropic/claude-opus-5@high", # mostly
    "codex/gpt-5.6-sol",
]
fast = true

[ui]
currency = "gbp"
`)

	want := currentVersionLine() + `

[default]
effort = "medium"
fast = true

[agent]
round_robin = [
    "anthropic/claude-opus-5@high", # mostly
    "codex/gpt-5.6-sol",
]

[ui]
currency = "gbp"
`
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheThirteenthConfigMigrationMovesARotationFile(t *testing.T) {
	written := migratedConfig(t, "version = 13\n\n[model]\nround_robin = \"config.models\"\neffort = \"low\"\n")

	want := currentVersionLine() + "\n\n[default]\neffort = \"low\"\n\n[agent]\nround_robin = \"config.models\"\n"
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheThirteenthConfigMigrationMovesADottedRotation(t *testing.T) {
	written := migratedConfig(t, "version = 13\nmodel.round_robin = [\"a/b\", \"c/d\"]\nmodel . round_robin2 = 1\n")

	want := currentVersionLine() + "\nagent.round_robin = [\"a/b\", \"c/d\"]\nmodel . round_robin2 = 1\n"
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheThirteenthConfigMigrationLeavesAConfigWithoutARotationAlone(t *testing.T) {
	original := "version = 13\n\n[ui]\ncurrency = \"gbp\"\n# model.round_robin = [\"a/b\"]\n"
	written := migratedConfig(t, original)

	if want := strings.Replace(original, "version = 13", currentVersionLine(), 1); written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheThirteenthConfigMigrationRefusesARotationItCannotMove(t *testing.T) {
	original := "version = 13\nmodel = { round_robin = [\"a/b\"] }\n"
	path := configFile(t, original)

	_, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err == nil || !strings.Contains(err.Error(), "into [agent] by hand") {
		t.Fatalf("got %v, want a refusal naming what to do", err)
	}
	if body, _ := os.ReadFile(path); string(body) != original { //nolint:gosec // the test's own path
		t.Errorf("a refused migration rewrote the config:\n%s", body)
	}
}

func TestAMigratedRotationLoadsAsTheAgentsModels(t *testing.T) {
	path := configFile(t, "version = 13\n\n[model]\nround_robin = [\"a/b@high\", \"c/d\"]\neffort = \"low\"\n")
	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Agent.Rotation(); len(got) != 2 || got[0] != "a/b@high" || got[1] != "c/d" {
		t.Errorf("got rotation %q", got)
	}
	if got := loaded.Defaults.Effort; got != "low" {
		t.Errorf("got effort %q", got)
	}
	if unknown := loaded.UnknownSettings(); len(unknown) > 0 {
		t.Errorf("the migrated config has unknown settings: %v", unknown)
	}
}

func TestTheThirteenthConfigMigrationGathersTheDefaultsIntoOneTable(t *testing.T) {
	written := migratedConfig(t, `version = 13

# who may do what
[caps]
default = "rxw"

[model]
effort = "medium" # most of the time
fast = true

[ui]
currency = "gbp"

[tool]
output = "48K"

[editor]
command = []

[ports]
hostname = ""
`)

	want := currentVersionLine() + `

# who may do what
[default]
caps = "rxw"
effort = "medium" # most of the time
fast = true
tool_output = "48K"

[ui]
currency = "gbp"
`
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheThirteenthConfigMigrationGathersDottedDefaultsWhereTheFirstStood(t *testing.T) {
	written := migratedConfig(t, `version = 13
ui.currency = "gbp"
caps.default = "rx"
model.effort = "low"
editor.command = ""
tool.output = """
16K"""

[snippets]
review = """
[model]
effort = "not a setting"
"""

[model]
fast = true
`)

	want := currentVersionLine() + `
ui.currency = "gbp"
default.caps = "rx"
default.effort = "low"
default.fast = true
default.tool_output = """
16K"""

[snippet]
review = """
[model]
effort = "not a setting"
"""
`
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheThirteenthConfigMigrationLeavesSettingsThatSaySomethingAlone(t *testing.T) {
	original := "version = 13\n\n[ui]\ncurrency = \"gbp\"\n\n[ports]\nhostname = \"{session}.agent\"\n\n[editor]\ncommand = [\"subl\", \"--wait\"]\n"
	written := migratedConfig(t, original)

	want := strings.Replace(original, "version = 13", currentVersionLine(), 1)
	want = strings.Replace(want, "[ports]\nhostname", "[experimental]\nforward_hostname", 1)
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheThirteenthConfigMigrationRefusesDefaultsItCannotMove(t *testing.T) {
	original := "version = 13\ncaps = { default = \"rx\" }\n"
	path := configFile(t, original)

	_, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err == nil || !strings.Contains(err.Error(), "into [defaults]") {
		t.Fatalf("got %v, want a refusal naming what to do", err)
	}
	if body, _ := os.ReadFile(path); string(body) != original { //nolint:gosec // the test's own path
		t.Errorf("a refused migration rewrote the config:\n%s", body)
	}
}

func TestAMigratedConfigLoadsItsDefaultsWithNothingUnknown(t *testing.T) {
	path := configFile(t, "version = 13\n\n[caps]\ndefault = \"rxw\"\n\n[model]\neffort = \"low\"\nfast = true\n\n[tool]\noutput = \"48K\"\n\n[ui]\ncurrency = \"\"\n")
	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Defaults; got.Caps != "rxw" || got.Effort != "low" || !got.IsFast || got.ToolOutput.Bytes != 48*1024 {
		t.Errorf("got defaults %+v", got)
	}
	if unknown := loaded.UnknownSettings(); len(unknown) > 0 {
		t.Errorf("the migrated config has unknown settings: %v", unknown)
	}
}

func TestTheFifteenthConfigFormatRenamesCustomToolsWithoutTouchingComments(t *testing.T) {
	original := `version = 14
# [tools.comment]
[tools.offload]
description = "the tools table remains in this description"
command = ["true"]
enabled = true

[tools.commit]
description = "commit"
command = ["true"]
`
	path := configFile(t, original)
	from, isPresent, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if !isPresent || from != config.DefaultsFormat {
		t.Errorf("got present %t from format %d", isPresent, from)
	}

	migrated, err := os.ReadFile(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	written := string(migrated)
	for _, preserved := range []string{
		currentVersionLine(),
		"# [tools.comment]",
		"[tool.offload]",
		"[tool.commit]",
		`description = "the tools table remains in this description"`,
	} {
		if !strings.Contains(written, preserved) {
			t.Errorf("migration did not preserve %q in:\n%s", preserved, written)
		}
	}
	if _, err := config.Load(path); err != nil {
		t.Errorf("migrated config could not load: %v", err)
	}
}

func TestTheToolTableMigrationRenamesParameterArrays(t *testing.T) {
	original := `version = 14
[tools.weather]
description = "weather"
command = ["./forecast"]

[[tools.weather.parameters]]
name = "city"
kind = "string"
description = "the city"
`
	path := configFile(t, original)
	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}
	migrated, err := os.ReadFile(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migrated), "[[tool.weather.parameters]]") {
		t.Errorf("the parameter array was not renamed:\n%s", migrated)
	}
	if _, err := config.Load(path); err != nil {
		t.Errorf("migrated config could not load: %v", err)
	}
}

func TestTheSingularTableMigrationRenamesTheJobsSegment(t *testing.T) {
	for name, original := range map[string]string{
		"from fifteen":  "version = 15\n[bar.top]\nleft = [{ segment = \"jobs\" }, { segment = 'jobs' }]\n",
		"from fourteen": "version = 14\n[tools.weather]\ndescription = \"weather\"\ncommand = [\"./forecast\"]\n\n[bar.top]\nleft = [{ segment = \"jobs\" }]\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := configFile(t, original)
			if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
				t.Fatal(err)
			}
			migrated, err := os.ReadFile(path) //nolint:gosec // the test's own file
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(migrated), "jobs") || !strings.Contains(string(migrated), `segment = "tasks"`) {
				t.Errorf("the jobs segment was not renamed:\n%s", migrated)
			}
			if _, err := config.Load(path); err != nil {
				t.Errorf("migrated config could not load: %v", err)
			}
		})
	}
}

func TestTheToolTableMigrationRefusesConflictingTables(t *testing.T) {
	path := configFile(t, "version = 14\n[tools.foo]\ndescription = \"foo\"\n[tool.bar]\ndescription = \"bar\"\n")
	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err == nil || !strings.Contains(err.Error(), "both tool and tools") {
		t.Errorf("got migration failure %v", err)
	}
}

func TestMigratingACustomToolThatNowNamesABuiltinRefusesWithoutWriting(t *testing.T) {
	original := "version = 14\n[tools.wait]\ndescription = \"my own wait\"\ncommand = [\"true\"]\n"
	path := configFile(t, original)
	_, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
	if err == nil || !strings.Contains(err.Error(), `custom tool "wait" now names a built-in`) {
		t.Errorf("got migration result %v", err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Errorf("migration changed the refusing config: %q", body)
	}
}

func TestToolTableMigrationPreservesQuotedInlineAndMultilineForms(t *testing.T) {
	for _, test := range []struct {
		name, original, retained string
	}{
		{
			name: "quoted root and key",
			original: `version = 14
["tools".weather]
description = "weather"
command = ["true"]
"enabled" = true
`,
			retained: `["tool".weather]`,
		},
		{
			name: "inline root table",
			original: `version = 14
tools = { weather = { description = "weather", command = ["true"], enabled = true } }
`,
			retained: `tool = { weather = { description = "weather", command = ["true"], granted = true } }`,
		},
		{
			name: "inline tool in root table",
			original: `version = 14
[tools]
weather = { description = "weather", command = ["true"], enabled = true }
`,
			retained: `weather = { description = "weather", command = ["true"], granted = true }`,
		},
		{
			name: "multiline body containing old headers and keys",
			original: `version = 14
[tools.weather]
description = """
weather
[tools.example]
enabled = true
"""
command = ["true"]
enabled = true
`,
			retained: "[tools.example]\nenabled = true\n\"\"\"",
		},
		{
			name: "dotted root key",
			original: `version = 14
tools.weather.description = "weather"
tools.weather.command = ["true"]
tools.weather.enabled = true
`,
			retained: "tool.weather.granted = true",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := configFile(t, test.original)
			if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path) //nolint:gosec // the test's own file
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), test.retained) {
				t.Errorf("migrated config did not preserve %q:\n%s", test.retained, data)
			}
			if _, err := config.Load(path); err != nil {
				t.Errorf("migrated config cannot load: %v", err)
			}
		})
	}
}

func TestTheSixteenthConfigFormatSingularisesTablesAndMovesSettings(t *testing.T) {
	path := configFile(t, `version = 15

[permissions]
# network = "allow"
lookup = "ask"

[defaults]
caps = "rxs"

[experimental]
shared_history = true

[snippets]
gold = { prompt = "golden?", description = "check goldens" }
review = """
[sandbox]
read = []
"""

[debug]
wire = true

# the hostname forwards announce
[ports]
hostname = "{session}.agent" # per session

[provider.ollama]
host = "http://127.0.0.1:11434"

[skills]
include = ["~/agents"]

[sandbox]
read = [
    "~/reference",
]
home = ["~/.gitconfig"]
`)
	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	want := currentVersionLine() + `

[tool.lookup]
# network = "allow"
permission = "ask"

[default]
caps = "rxs"

[experimental]
shared_history = true
# the hostname forwards announce
forward_hostname = "{session}.agent" # per session

[snippet]
gold = { prompt = "golden?", description = "check goldens" }
review = """
[sandbox]
read = []
"""

[debug]
wire = true

[provider.ollama]
host = "http://127.0.0.1:11434"

[skill]
include = ["~/agents"]

[grants]
read = [
    "~/reference",
]
home = ["~/.gitconfig"]
`
	if written := string(body); written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.GetHostname("tame-impala", "127.0.0.1"); got != "tame-impala.agent" {
		t.Errorf("got hostname %q", got)
	}
	if got := loaded.Defaults.Caps; got != "rxs" {
		t.Errorf("got caps %q", got)
	}
	if got := loaded.Snippets["gold"].Prompt; got != "golden?" {
		t.Errorf("got snippet %q", got)
	}
	if got := loaded.Skills.Include; len(got) != 1 {
		t.Errorf("got skills %q", got)
	}
	permissions, err := loaded.BuildPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if permissions.Lookup.IsAllowed() {
		t.Error("the lookup permission was lost")
	}
	if unknown := loaded.UnknownSettings(); len(unknown) > 0 {
		t.Errorf("the migrated config has unknown settings: %v", unknown)
	}
}

func TestTheSixteenthConfigFormatSpreadsPermissionsAcrossTheirTools(t *testing.T) {
	path := configFile(t, `version = 15

[permissions] # what a granted capability buys
network = { rule = "ask", timeout = "2m" }
# fetching asks first
fetch = "ask"
lookup = "allow"

[tool.weather]
description = "weather"
command = ["true"]
`)
	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}

	want := currentVersionLine() + `

# what a granted capability buys

[tool.bash]
host_network = { rule = "ask", timeout = "2m" }

[tool.fetch]
# fetching asks first
permission = "ask"

[tool.lookup]
permission = "allow"

[tool.weather]
description = "weather"
command = ["true"]
`
	if written := string(body); written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	permissions, err := loaded.BuildPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if permissions.Network.Timeout != 2*time.Minute || permissions.Fetch.IsAllowed() || !permissions.Lookup.IsAllowed() {
		t.Errorf("got permissions %+v", permissions)
	}
	if unknown := loaded.UnknownSettings(); len(unknown) > 0 {
		t.Errorf("the migrated config has unknown settings: %v", unknown)
	}
}

func TestTheSixteenthConfigFormatSingularisesDottedAndQuotedTables(t *testing.T) {
	written := migratedConfig(t, `version = 15
defaults.caps = "rx"
"sandbox".read = ["/etc"]
'permissions' . lookup = "ask"
ports.hostname = "{session}.agent"

[ "snippets" . review ]
prompt = "review it"
`)

	want := currentVersionLine() + `
default.caps = "rx"
"grants".read = ["/etc"]
tool.lookup.permission = "ask"
experimental.forward_hostname = "{session}.agent"

[ "snippet" . review ]
prompt = "review it"
`
	if written != want {
		t.Errorf("got:\n%s\nwant:\n%s", written, want)
	}
}

func TestTheSixteenthConfigFormatKeepsTheFormOfEachLine(t *testing.T) {
	for _, test := range []struct {
		name, original, want string
	}{
		{
			name:     "final line without a newline",
			original: "version = 15\n\n[experimental]\nshared_history = true\n\n[ports]\nhostname = \"{session}\"",
			want:     "\n\n[experimental]\nshared_history = true\nforward_hostname = \"{session}\"\n",
		},
		{
			name:     "a comment beside the header",
			original: "version = 15\n[experimental]\nshared_history = true\n[ports] # forwarding names\nhostname = \"{session}\"\n",
			want:     "\n[experimental]\nshared_history = true\n# forwarding names\nforward_hostname = \"{session}\"\n",
		},
		{
			name:     "a header renamed in place",
			original: "version = 15\n[ports] # forwarding names\nhostname = \"{session}\"\n",
			want:     "\n[experimental] # forwarding names\nforward_hostname = \"{session}\"\n",
		},
		{
			name:     "windows line endings",
			original: "version = 15\r\n[experimental]\r\nshared_history = true\r\n[ports]\r\nhostname = \"{session}\"",
			want:     "\r\n[experimental]\r\nshared_history = true\r\nforward_hostname = \"{session}\"\r\n",
		},
		{
			name:     "a stray key beside the hostname",
			original: "version = 15\n[experimental]\nwait_tool = true\n[ports]\nhostname = \"{session}\"\nshared_history = true\n",
			want:     "\n[experimental]\nwait_tool = true\nforward_hostname = \"{session}\"\n[ports]\nshared_history = true\n",
		},
		{
			name:     "a stray key alone",
			original: "version = 15\n[ports]\nshared_history = true\n",
			want:     "\n[ports]\nshared_history = true\n",
		},
		{
			name:     "a literal string ending in a backslash",
			original: "version = 15\n[snippets]\npath = '''C:\\'''\n[sandbox]\nread = []\n",
			want:     "\n[snippet]\npath = '''C:\\'''\n[grants]\nread = []\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if written, want := migratedConfig(t, test.original), currentVersionLine()+test.want; written != want {
				t.Errorf("got:\n%q\nwant:\n%q", written, want)
			}
		})
	}
}

func TestTheSixteenthConfigFormatRefusesWhatItCannotSingularise(t *testing.T) {
	for _, test := range []struct {
		name, original, refusal string
	}{
		{
			name:     "both names of a table",
			original: "version = 15\n[snippets]\na = \"a\"\n[snippet]\nb = \"b\"\n",
			refusal:  "both snippets and snippet are defined",
		},
		{
			name:     "both hostnames",
			original: "version = 15\n[ports]\nhostname = \"{session}\"\n[experimental]\nforward_hostname = \"{session}.agent\"\n",
			refusal:  "both ports.hostname and experimental.forward_hostname are defined",
		},
		{
			name:     "experimental written as dotted keys",
			original: "version = 15\nexperimental.shared_history = true\n[ports]\nhostname = \"{session}\"\n",
			refusal:  "by hand, move ports.hostname to experimental.forward_hostname",
		},
		{
			name:     "a dotted hostname beside an experimental table",
			original: "version = 15\nports.hostname = \"{session}.agent\"\n\n[experimental]\nshared_history = true\n",
			refusal:  "by hand, move ports.hostname to experimental.forward_hostname",
		},
		{
			name:     "an inline tool table",
			original: "version = 15\ntool = { weather = { description = \"weather\", command = [\"true\"] } }\n[permissions]\nlookup = \"ask\"\n",
			refusal:  "by hand, move permissions.lookup to tool.lookup.permission",
		},
		{
			name:     "only what applies",
			original: "version = 15\nports = { hostname = \"{session}\" }\n[experimental]\nshared_history = true\n",
			refusal:  "the config could not be changed safely; by hand, move ports.hostname to experimental.forward_hostname",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := configFile(t, test.original)
			_, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path})
			if err == nil || !strings.Contains(err.Error(), test.refusal) {
				t.Fatalf("got %v, want a refusal containing %q", err, test.refusal)
			}
			if body, _ := os.ReadFile(path); string(body) != test.original { //nolint:gosec // the test's own path
				t.Errorf("a refused migration rewrote the config:\n%s", body)
			}
		})
	}
}
