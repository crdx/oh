package location_test

import (
	"os"
	"path/filepath"
	"testing"

	"crdx.org/oh/internal/app/location"
)

func TestTheStateDirectoryFollowsTheHomeDirectoryByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv(location.StateDirVariable, "")

	want := filepath.Join(home, ".local", "state", "org.crdx", "oh", "sessions")
	if got := location.GetSessionsDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnAbsoluteStateDirectoryIsTakenOverTheHomeDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(location.StateDirVariable, root)

	if got, want := location.GetSessionsDir(), filepath.Join(root, "sessions"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := location.GetTmpDir("chewy-raven"), filepath.Join(root, "farm", "chewy-raven"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheAnalysisCacheFollowsTheStateDirectory(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		stateDirectory string
		usesXDGState   bool
	}{
		{name: "home fallback"},
		{name: "XDG state", usesXDGState: true},
		{name: "absolute override", stateDirectory: "/isolated/state", usesXDGState: true},
		{name: "relative override", stateDirectory: "relative/state", usesXDGState: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", "")
			t.Setenv(location.StateDirVariable, scenario.stateDirectory)

			root := filepath.Join(home, ".local", "state")
			if scenario.usesXDGState {
				root = t.TempDir()
				t.Setenv("XDG_STATE_HOME", root)
			}
			want := filepath.Join(root, "org.crdx", "oh", "data", "analysis.json")
			if filepath.IsAbs(scenario.stateDirectory) {
				want = filepath.Join(scenario.stateDirectory, "data", "analysis.json")
			}
			if got := location.GetAnalysisCachePath(); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestARelativeStateDirectoryIsIgnored(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv(location.StateDirVariable, filepath.Join("inside", "workspace"))

	want := filepath.Join(home, ".local", "state", "org.crdx", "oh", "sessions")
	if got := location.GetSessionsDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheConfigDirectoryFollowsTheHomeDirectoryByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(location.ConfigDirVariable, "")

	want := filepath.Join(home, ".config", "org.crdx", "oh", "config.toml")
	if got := location.GetConfigFile(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAnAbsoluteConfigDirectoryIsTakenOverXDG(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(location.ConfigDirVariable, root)

	if got, want := location.GetConfigFile(), filepath.Join(root, "config.toml"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := location.GetGlobalContextPath(), filepath.Join(root, "SYSTEM.md"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestARelativeConfigDirectoryIsIgnored(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv(location.ConfigDirVariable, filepath.Join("inside", "workspace"))

	want := filepath.Join(configHome, "org.crdx", "oh", "config.toml")
	if got := location.GetConfigFile(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStateDataUsesOneDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv(location.StateDirVariable, root)

	paths := map[string]string{
		"analysis":             location.GetAnalysisCachePath(),
		"exchange rates":       location.GetExchangeRateCachePath(),
		"history":              location.GetHistoryPath(),
		"model cache":          location.GetModelCachePath(),
		"model rotation":       location.GetModelRoundRobinPath(),
		"seen models":          location.GetSeenModelsPath(),
		"subagent rotation":    location.GetSubagentRoundRobinPath(),
		"provider usage cache": location.GetUsageCachePath("codex", false),
	}
	want := map[string]string{
		"analysis":             filepath.Join(root, "data", "analysis.json"),
		"exchange rates":       filepath.Join(root, "data", "rates.json"),
		"history":              filepath.Join(root, "data", "history"),
		"model cache":          filepath.Join(root, "data", "models.json"),
		"model rotation":       filepath.Join(root, "data", "model-round-robin.json"),
		"seen models":          filepath.Join(root, "data", "seen_models.json"),
		"subagent rotation":    filepath.Join(root, "data", "subagent-round-robin.json"),
		"provider usage cache": filepath.Join(root, "usage", "codex.json"),
	}
	for name, path := range paths {
		if path != want[name] {
			t.Errorf("%s path is %q, want %q", name, path, want[name])
		}
	}
}

func TestLegacyStateDataIsMovedIntoTheDataDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv(location.StateDirVariable, root)

	files := map[string]string{
		"analysis.json":         "analysis",
		"history":               "history",
		"history.lock":          "",
		"seen_models.json":      "seen models",
		"seen_models.json.lock": "",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := location.RequireCurrentDataLayout(); err == nil {
		t.Fatal("legacy state data was accepted")
	}
	moved, err := location.MigrateDataLayout(false)
	if err != nil {
		t.Fatal(err)
	}
	if moved != len(files) {
		t.Fatalf("moved %d files, want %d", moved, len(files))
	}
	for name, body := range files {
		data, err := os.ReadFile(filepath.Join(root, "data", name)) //nolint:gosec // the test's own path
		if err != nil {
			t.Errorf("%s was not moved: %v", name, err)
			continue
		}
		if string(data) != body {
			t.Errorf("%s contains %q, want %q", name, data, body)
		}
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("legacy %s remains: %v", name, err)
		}
	}
	if err := location.RequireCurrentDataLayout(); err != nil {
		t.Errorf("migrated state data was refused: %v", err)
	}
}

func TestADryRunLeavesLegacyStateDataAlone(t *testing.T) {
	root := t.TempDir()
	t.Setenv(location.StateDirVariable, root)
	legacy := filepath.Join(root, "models.json")
	if err := os.WriteFile(legacy, []byte("models"), 0o600); err != nil {
		t.Fatal(err)
	}

	moved, err := location.MigrateDataLayout(true)
	if err != nil {
		t.Fatal(err)
	}
	if moved != 1 {
		t.Fatalf("would move %d files, want 1", moved)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("dry run changed the legacy file: %v", err)
	}
	if _, err := os.Stat(location.GetDataDir()); !os.IsNotExist(err) {
		t.Errorf("dry run created the data directory: %v", err)
	}
}

func TestConcurrentStateDataMigrationsShareOneLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv(location.StateDirVariable, root)
	if err := os.WriteFile(filepath.Join(root, "history"), []byte("history"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := make(chan int, 2)
	failures := make(chan error, 2)
	for range 2 {
		go func() {
			moved, err := location.MigrateDataLayout(false)
			results <- moved
			failures <- err
		}()
	}
	movedTotal := 0
	for range 2 {
		movedTotal += <-results
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	if movedTotal != 1 {
		t.Errorf("concurrent migrations moved %d files, want 1", movedTotal)
	}
	data, err := os.ReadFile(location.GetHistoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "history" {
		t.Errorf("history contains %q, want the original", data)
	}
}

func TestConflictingStateDataIsNotMoved(t *testing.T) {
	root := t.TempDir()
	t.Setenv(location.StateDirVariable, root)
	if err := os.MkdirAll(location.GetDataDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "rates.json")
	current := location.GetExchangeRateCachePath()
	if err := os.WriteFile(legacy, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := location.MigrateDataLayout(false); err == nil {
		t.Fatal("conflicting state data was moved")
	}
	for path, want := range map[string]string{legacy: "legacy", current: "current"} {
		data, err := os.ReadFile(path) //nolint:gosec // the test's own path
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("%s contains %q, want %q", path, data, want)
		}
	}
}
