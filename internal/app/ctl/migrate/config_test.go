package migrate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		Model   struct {
			RoundRobin []string `toml:"round_robin"`
		} `toml:"model"`
	}
	metadata, err := toml.Decode(written, &decoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != config.Format {
		t.Errorf("got version %d, want %d", decoded.Version, config.Format)
	}
	if len(decoded.Model.RoundRobin) != 1 || decoded.Model.RoundRobin[0] != "codex/gpt-5.6-sol@medium" {
		t.Errorf("got round robin %#v", decoded.Model.RoundRobin)
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
	if strings.Count(written, "[model]") != 1 || !strings.Contains(written, currentVersionLine()) {
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
	if !strings.Contains(string(body), `round_robin = ["codex/gpt-5.6-sol@high"]`) {
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
		`round_robin = ["codex/gpt@high"]`,
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
	if !strings.Contains(written, currentVersionLine()) || !strings.Contains(written, `round_robin = ["codex/gpt@high"]`) {
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
		`round_robin = ["codex/gpt@high"]`,
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
	original := currentVersionLine() + "\n[model]\nround_robin = [\"codex/gpt@high\"]\n"
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
		`left = [{ segment = "jobs" }]`,
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
		`round_robin = ["codex/gpt@high"]`,
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
	if written := string(body); !strings.Contains(written, "[snippets.stream]") {
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
		`round_robin = ["codex/gpt@high"]`,
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
		"job":               {name: "tasks", paint: "status_warning"},
		"job_start":         {name: "launch", paint: "status_info"},
		"job_wait_any":      {name: "hold", paint: "normal"},
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
		"[tools.expose]\ncommand = \"expose-thing\"",
		"[other]\nexpose_add = { name = \"kept\" }\njob_start = { name = \"kept\" }",
	} {
		if !strings.Contains(written, expected) {
			t.Errorf("migration omitted %q from:\n%s", expected, written)
		}
	}
}

func TestTheEleventhConfigMigrationOnlyRaisesTheVersionWhereNothingNeedsRenaming(t *testing.T) {
	original := "version = 11 # current\n\n[model]\nround_robin = [\"codex/gpt@high\"]\n"
	path := configFile(t, original)

	if _, _, err := migrate.MigrateConfig(migrate.ConfigOptions{Path: path}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path) //nolint:gosec // the test's own path
	if err != nil {
		t.Fatal(err)
	}
	if want := currentVersionLine() + " # current\n\n[model]\nround_robin = [\"codex/gpt@high\"]\n"; string(body) != want {
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
