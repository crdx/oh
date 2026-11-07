package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/tr"
)

func loadWithSettings(t *testing.T, global string, settings ...string) (Config, string, error) {
	t.Helper()

	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(globalPath, global); err != nil {
		t.Fatal(err)
	}
	workspaceDir := filepath.Join(directory, "workspace")

	sources := []Source{{Path: globalPath}, {Path: filepath.Join(workspaceDir, "oh.toml"), IsOverride: true}}
	for _, setting := range settings {
		sources = append(sources, CommandLineSetting(workspaceDir, setting))
	}
	config, err := LoadSources(sources...)
	return config, directory, err
}

func TestACommandLineSettingReplacesAConfiguredValue(t *testing.T) {
	settings, _, err := loadWithSettings(t, "[ui]\ncurrency = \"EUR\"\n", "ui.currency=GBP", `default.caps="rwg"`)
	if err != nil {
		t.Fatal(err)
	}

	if settings.Ui.Currency != "GBP" {
		t.Errorf("got currency %q", settings.Ui.Currency)
	}
	if got := string(settings.Defaults.Caps); got != "rwg" {
		t.Errorf("got default capabilities %q", got)
	}
	if got := settings.CommandLineSettings(); !slices.Equal(got, []string{"ui.currency", "default.caps"}) {
		t.Errorf("got command line settings %q", got)
	}
}

func TestACommandLineSettingClearsAndAppendsGrants(t *testing.T) {
	global := "[grants]\nread = [\"shared\", \"notes\"]\nwrite = [\"scratch\"]\n"

	settings, directory, err := loadWithSettings(t, global, "grants.write=[]", "grants.read+=~/extra", `grants.read+=["src", "/abs"]`)
	if err != nil {
		t.Fatal(err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	wantRead := []string{
		filepath.Join(directory, "shared"),
		filepath.Join(directory, "notes"),
		filepath.Join(home, "extra"),
		filepath.Join(directory, "workspace", "src"),
		"/abs",
	}
	if !slices.Equal(settings.Grants.Read, wantRead) {
		t.Errorf("got read paths %#v, want %#v", settings.Grants.Read, wantRead)
	}
	if len(settings.Grants.Write) != 0 {
		t.Errorf("got write paths %#v, want none", settings.Grants.Write)
	}
}

func TestACommandLineSettingMaySetWhatAWorkspaceMayNot(t *testing.T) {
	settings, _, err := loadWithSettings(t, "", `tool.weather.command=["forecast"]`)
	if err != nil {
		t.Fatal(err)
	}

	if command := settings.Tools["weather"].Command; !slices.Equal(command, []string{"forecast"}) {
		t.Errorf("got weather command %#v", command)
	}
}

func TestACommandLineSettingAppendsToAnUnsetList(t *testing.T) {
	settings, _, err := loadWithSettings(t, "", "input.speed_dial=[]", "input.speed_dial+=carry on", "input.speed_dial+=stop")
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(settings.Input.SpeedDial, []string{"carry on", "stop"}) {
		t.Errorf("got speed dial %#v", settings.Input.SpeedDial)
	}
}

func TestACommandLineSettingKeepsTextThatIsNotTOML(t *testing.T) {
	settings, _, err := loadWithSettings(t, "", `input.nudge=say "go" \ now`)
	if err != nil {
		t.Fatal(err)
	}

	if settings.Input.Nudge != `say "go" \ now` {
		t.Errorf("got nudge %q", settings.Input.Nudge)
	}
}

func TestACommandLineSettingTakesOneSettingOnly(t *testing.T) {
	settings, _, err := loadWithSettings(t, "", "input.nudge=\"go\"\nui.currency = \"GBP\"")
	if err != nil {
		t.Fatal(err)
	}

	if settings.Input.Nudge != "\"go\"\nui.currency = \"GBP\"" {
		t.Errorf("got nudge %q", settings.Input.Nudge)
	}
	if settings.Ui.Currency != "" {
		t.Errorf("got currency %q", settings.Ui.Currency)
	}
}

func TestACommandLineSettingIsRefusedWhenMalformed(t *testing.T) {
	tests := []struct {
		setting string
		want    string
	}{
		{"ui.currency", fmt.Sprintf(tr.SettingShape, settingFlag)},
		{"=GBP", fmt.Sprintf(tr.SettingNameInvalid, "")},
		{"ui currency=GBP", fmt.Sprintf(tr.SettingNameInvalid, "ui currency")},
		{"ui.currency+=GBP", fmt.Sprintf(tr.AppendedNotList, "ui.currency")},
		{"grants.read+=[1]", fmt.Sprintf(tr.AppendedNotText, "grants.read")},
		{"subagent.concurrency=0", fmt.Sprintf(tr.SettingMinimum, "subagent.concurrency", 1, 0)},
	}

	for _, test := range tests {
		_, _, err := loadWithSettings(t, "", test.setting)
		want := settingFlag + " " + test.setting + ": " + test.want
		if err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", test.setting, err, want)
		}
	}
}

func TestACommandLineSettingNamesAnUnknownSetting(t *testing.T) {
	settings, _, err := loadWithSettings(t, "", "ui.colour=red")
	if err != nil {
		t.Fatal(err)
	}

	reports := settings.UnknownSettings()
	want := settingFlag + " ui.colour=red: " + fmt.Sprintf(tr.UnknownSettingNames, "ui.colour")
	if !slices.Contains(reports, want) {
		t.Errorf("got reports %q, want %q", reports, want)
	}
}

func TestACommandLineSettingCannotChangeTheVersion(t *testing.T) {
	settings, _, err := loadWithSettings(t, "", "version=1")
	if err != nil {
		t.Fatal(err)
	}

	if settings.Version != Format {
		t.Errorf("got version %d, want %d", settings.Version, Format)
	}
	if got := settings.CommandLineSettings(); len(got) != 0 {
		t.Errorf("got command line settings %q", got)
	}
}

func TestACommandLineSettingSurvivesAReload(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	if err := writeConfigFile(globalPath, "[ui]\ncurrency = \"EUR\"\n"); err != nil {
		t.Fatal(err)
	}

	settings, observer, err := ObserveSources(Source{Path: globalPath}, CommandLineSetting(directory, "ui.currency=GBP"))
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if settings.Ui.Currency != "GBP" {
		t.Fatalf("got currency %q", settings.Ui.Currency)
	}

	if err := writeConfigFile(globalPath, "[ui]\ncurrency = \"JPY\"\n"); err != nil {
		t.Fatal(err)
	}
	reloaded, changes, err := observer.refresh(nil)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Ui.Currency != "GBP" {
		t.Errorf("got currency %q after a reload", reloaded.Ui.Currency)
	}
	for _, change := range changes {
		if strings.HasPrefix(change.Path, settingFlag) {
			t.Errorf("a command line setting was named as changed: %+v", change)
		}
	}
}
