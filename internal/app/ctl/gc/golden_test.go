package gc

import (
	"debug/buildinfo"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

const (
	goldenName         = "stored-session"
	homePlaceholder    = "<home>"
	binaryFixtureBytes = 32 << 20
	measuredBytes      = 4096
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func TestGoldenUsageMatchesTheGolden(t *testing.T) {
	assertGolden(t, "usage.txt", strings.ReplaceAll(usage, "$0", "oh"))
}

func TestGoldenEveryCacheIsRemovedAndReported(t *testing.T) {
	directories := populated(t)

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "removed.txt", report(screen.String(), failure.String()))
	assertGone(t, filepath.Join(directories.Farm, goldenName, ".cache"))
	assertGone(t, filepath.Join(directories.Home, ".cache"))
	assertGone(t, filepath.Join(directories.Farm, "able-dolphin"))

	for _, kept := range []string{
		filepath.Join(directories.Farm, goldenName, "checkout", ".cache"),
		filepath.Join(directories.Farm, "scratchpad", "notes.txt"),
	} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("a sweep of the roots took %s", kept)
		}
	}
}

func TestGoldenAnAggressiveSweepTakesTheCachesWithinACheckout(t *testing.T) {
	directories := populated(t)

	var screen, failure strings.Builder
	choice := options{isAggressive: true}
	if err := run(directories, choice, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "aggressive.txt", report(screen.String(), failure.String()))
	assertGone(t, filepath.Join(directories.Farm, goldenName, "checkout", ".cache"))
}

func TestGoldenADryRunReportsWhatItWouldRemoveAndRemovesNothing(t *testing.T) {
	directories := populated(t)

	var screen, failure strings.Builder
	if err := run(directories, options{isDryRun: true}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "dry-run.txt", report(screen.String(), failure.String()))

	kept := filepath.Join(directories.Farm, goldenName, ".cache")
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("a dry run removed %s", kept)
	}
}

func TestGoldenARunningSessionKeepsItsCachesAndTheHomeItShares(t *testing.T) {
	directories := populated(t)

	runningName := storedSession(t, directories.Sessions)
	write(t, filepath.Join(directories.Farm, runningName, ".cache", "still-warm"), 4096)

	heldLock, err := session.AcquireLock(directories.Sessions, runningName)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = heldLock.Release() }()

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "running.txt", report(screen.String(), failure.String()))

	kept := filepath.Join(directories.Farm, runningName, ".cache")
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("a running session lost %s", kept)
	}

	shared := filepath.Join(directories.Home, ".cache")
	if _, err := os.Stat(shared); err != nil {
		t.Errorf("a running session lost the shared %s", shared)
	}
}

func TestGoldenTheCachesTheGoToolchainLeavesBehindAreTaken(t *testing.T) {
	directories := leftBehind(t)

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "toolchain.txt", report(screen.String(), failure.String()))

	scratch := filepath.Join(directories.Farm, goldenName)
	for _, taken := range []string{
		"go-build2952174331",
		"go-build884213007",
		"go-build15",
		"gocache",
		"cold-cache",
		"go-mod",
	} {
		assertGone(t, filepath.Join(scratch, taken))
	}
	for _, kept := range []string{
		filepath.Join("checkout", "main.go"),
		filepath.Join("checkout", "oh"),
		filepath.Join("go-buildings", "notes.txt"),
		filepath.Join("go-build77", "report.json"),
		filepath.Join("site", "cache", "assets.css"),
		filepath.Join("corpus", "trim.txt"),
		filepath.Join("mirror", "cache", "download", "nightly", "index"),
	} {
		if _, err := os.Stat(filepath.Join(scratch, kept)); err != nil {
			t.Errorf("a sweep took %s", kept)
		}
	}
}

func leftBehind(t *testing.T) Directories {
	t.Helper()

	directories := Directories{Farm: t.TempDir(), Sessions: t.TempDir(), Home: t.TempDir()}
	storedSessionNamed(t, directories.Sessions, goldenName)
	scratch := filepath.Join(directories.Farm, goldenName)

	write(t, filepath.Join(scratch, "go-build2952174331", "b001", "_pkg_.a"), 4096)
	write(t, filepath.Join(scratch, "go-build884213007", "b002", "importcfg"), 2048)
	if err := os.MkdirAll(filepath.Join(scratch, "go-build15"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(scratch, "gocache", "trim.txt"), 32)
	write(t, filepath.Join(scratch, "gocache", "00", "00a1b2c3-d"), 2048)
	write(t, filepath.Join(scratch, "gocache", "ff", "ffe4f5a6-d"), 2048)
	write(t, filepath.Join(scratch, "cold-cache", "README"), 128)
	write(t, filepath.Join(scratch, "cold-cache", "00", "0099aabb-a"), 1024)
	write(t, filepath.Join(scratch, "cold-cache", "ff", "ff77eedd-a"), 1024)
	write(t, filepath.Join(scratch, "go-mod", "cache", "download", "crdx.org", "col", "@v", "list"), 64)
	write(t, filepath.Join(scratch, "go-mod", "crdx.org", "col@v1.0.0", "col.go"), 512)

	write(t, filepath.Join(scratch, "checkout", "main.go"), 512)
	write(t, filepath.Join(scratch, "checkout", "oh"), 8192)
	write(t, filepath.Join(scratch, "go-buildings", "notes.txt"), 256)
	write(t, filepath.Join(scratch, "go-build77", "b001", "keep"), 256)
	write(t, filepath.Join(scratch, "go-build77", "report.json"), 256)
	write(t, filepath.Join(scratch, "site", "cache", "assets.css"), 256)
	write(t, filepath.Join(scratch, "corpus", "trim.txt"), 256)
	write(t, filepath.Join(scratch, "mirror", "cache", "download", "nightly", "index"), 256)

	return directories
}

func TestAReadOnlyModuleCacheIsStillRemoved(t *testing.T) {
	directories := populated(t)

	module := filepath.Join(directories.Farm, goldenName, ".cache", "go-mod", "yaml.v3")
	write(t, filepath.Join(module, "writerc.go"), 128)
	if err := os.Chmod(module, 0o500); err != nil { //nolint:gosec // a read-only cache is the point
		t.Fatal(err)
	}

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGone(t, filepath.Join(directories.Farm, goldenName, ".cache"))
}

func TestTheDirectoriesHoldingACacheAreNotCountedAgainstIt(t *testing.T) {
	flat := t.TempDir()
	write(t, filepath.Join(flat, "one"), measuredBytes)

	nested := t.TempDir()
	write(t, filepath.Join(nested, "first", "second", "third", "one"), measuredBytes)

	flatBytes, err := size(flat)
	if err != nil {
		t.Fatal(err)
	}
	nestedBytes, err := size(nested)
	if err != nil {
		t.Fatal(err)
	}

	if flatBytes != nestedBytes {
		t.Errorf("a cache measured %d bytes flat and %d bytes nested", flatBytes, nestedBytes)
	}
	if flatBytes < measuredBytes {
		t.Errorf("a cache holding %d bytes measured %d bytes", measuredBytes, flatBytes)
	}
}

func TestGoldenAnArchivedSessionKeepsItsScratchAndLosesItsCaches(t *testing.T) {
	directories := Directories{Farm: t.TempDir(), Sessions: t.TempDir(), Home: t.TempDir()}
	archivedSession(t, directories.Sessions, goldenName)

	scratch := filepath.Join(directories.Farm, goldenName)
	write(t, filepath.Join(scratch, ".cache", "packed"), 4096)
	write(t, filepath.Join(scratch, "checkout", "source.go"), 512)

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "archived.txt", report(screen.String(), failure.String()))
	assertGone(t, filepath.Join(scratch, ".cache"))

	for _, kept := range []string{scratch, filepath.Join(scratch, "checkout", "source.go")} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("an archived session lost %s", kept)
		}
	}
}

func TestGoldenOneOfEachIsCountedInTheSingular(t *testing.T) {
	directories := Directories{Farm: t.TempDir(), Sessions: t.TempDir(), Home: t.TempDir()}

	runningName := storedSession(t, directories.Sessions)
	write(t, filepath.Join(directories.Farm, runningName, ".cache", "still-warm"), 4096)
	write(t, filepath.Join(directories.Farm, "able-dolphin", "left-behind"), 1024)

	heldLock, err := session.AcquireLock(directories.Sessions, runningName)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = heldLock.Release() }()

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "singular.txt", report(screen.String(), failure.String()))
}

func TestGoldenARootThatCannotBeReadIsNamedAndCountedAgainstTheTotal(t *testing.T) {
	directories := Directories{Farm: t.TempDir(), Sessions: t.TempDir(), Home: t.TempDir()}
	write(t, filepath.Join(directories.Farm, "able-dolphin", "left-behind"), 1024)

	unreadable := filepath.Join(t.TempDir(), "home")
	write(t, unreadable, 32)
	directories.Home = unreadable

	var screen, failure strings.Builder
	err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure})
	if err == nil {
		t.Fatal("a root that could not be read was not reported as an error")
	}

	drawn := report(screen.String(), failure.String()) + "=== error ===\n" + err.Error() + "\n"
	assertGolden(t, "unreadable.txt", strings.ReplaceAll(drawn, unreadable, homePlaceholder))
}

func TestGoldenAGoBinaryGoesWhenItsOwnSourceStandsAboveIt(t *testing.T) {
	directories, scratch := withBinaries(t)

	var screen, failure strings.Builder
	choice := options{isAggressive: true}
	if err := run(directories, choice, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "binaries.txt", report(screen.String(), failure.String()))
	assertGone(t, filepath.Join(scratch, "checkout", "dist", "oh"))

	for _, kept := range []string{
		filepath.Join("elsewhere", "tool"),
		filepath.Join("loose", "oh"),
		filepath.Join("checkout", "build.sh"),
		filepath.Join("checkout", "main.go"),
	} {
		if _, err := os.Stat(filepath.Join(scratch, kept)); err != nil {
			t.Errorf("a sweep took %s", kept)
		}
	}
}

func TestAPlainSweepLeavesEveryBinaryAlone(t *testing.T) {
	directories, scratch := withBinaries(t)

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(scratch, "checkout", "dist", "oh")); err != nil {
		t.Error("a sweep of the roots took a binary below one")
	}
}

func withBinaries(t *testing.T) (Directories, string) {
	t.Helper()

	directories := Directories{Farm: t.TempDir(), Sessions: t.TempDir(), Home: t.TempDir()}
	storedSessionNamed(t, directories.Sessions, goldenName)
	scratch := filepath.Join(directories.Farm, goldenName)

	writeText(t, filepath.Join(scratch, "checkout", "go.mod"), "module "+ownModule(t)+"\n")
	writeBinary(t, filepath.Join(scratch, "checkout", "dist", "oh"))
	write(t, filepath.Join(scratch, "checkout", "main.go"), 512)
	writeText(t, filepath.Join(scratch, "checkout", "build.sh"), "#!/bin/sh\ngo build .\n")
	if err := os.Chmod(filepath.Join(scratch, "checkout", "build.sh"), 0o700); err != nil { //nolint:gosec // the fixture is an executable script
		t.Fatal(err)
	}

	writeText(t, filepath.Join(scratch, "elsewhere", "go.mod"), "module crdx.org/somebody-else\n")
	writeBinary(t, filepath.Join(scratch, "elsewhere", "tool"))

	writeBinary(t, filepath.Join(scratch, "loose", "oh"))

	return directories, scratch
}

func ownModule(t *testing.T) string {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	information, err := buildinfo.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}

	return information.Main.Path
}

func writeBinary(t *testing.T, path string) {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	image, err := os.ReadFile(self) //nolint:gosec // the test binary is the only Go binary to hand
	if err != nil {
		t.Fatal(err)
	}
	if len(image) > binaryFixtureBytes {
		t.Fatalf("the test binary outgrew the fixture size of %d bytes", binaryFixtureBytes)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	padded := make([]byte, binaryFixtureBytes)
	copy(padded, image)
	if err := os.WriteFile(path, padded, 0o700); err != nil { //nolint:gosec // an executable is the point
		t.Fatal(err)
	}
}

func writeText(t *testing.T, path string, text string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenNothingToRemoveIsStillReported(t *testing.T) {
	directories := Directories{Farm: t.TempDir(), Sessions: t.TempDir(), Home: t.TempDir()}

	var screen, failure strings.Builder
	if err := run(directories, options{}, console.Output{Screen: &screen, Failure: &failure}); err != nil {
		t.Fatal(err)
	}

	assertGolden(t, "nothing.txt", report(screen.String(), failure.String()))
}

func populated(t *testing.T) Directories {
	t.Helper()

	directories := Directories{Farm: t.TempDir(), Sessions: t.TempDir(), Home: t.TempDir()}
	storedSessionNamed(t, directories.Sessions, goldenName)

	write(t, filepath.Join(directories.Farm, goldenName, ".cache", "npm", "packed"), 4096)
	write(t, filepath.Join(directories.Farm, goldenName, ".cache", "kept-below"), 1024)
	write(t, filepath.Join(directories.Farm, goldenName, "checkout", ".cache", "built"), 2048)
	write(t, filepath.Join(directories.Farm, goldenName, "checkout", "source.go"), 512)
	write(t, filepath.Join(directories.Farm, "able-dolphin", ".cache", "left-behind"), 8192)
	write(t, filepath.Join(directories.Farm, "scratchpad", "notes.txt"), 256)
	write(t, filepath.Join(directories.Home, ".cache", "go-build", "object"), 16384)
	write(t, filepath.Join(directories.Home, ".config", "settings"), 256)

	return directories
}

func archivedSession(t *testing.T, directory string, name string) {
	t.Helper()

	write(t, filepath.Join(directory, name+session.ArchiveSuffix), 64)
}

func storedSessionNamed(t *testing.T, directory string, name string) {
	t.Helper()

	created := storedSession(t, directory)
	if err := os.Rename(session.Dir(directory, created), session.Dir(directory, name)); err != nil {
		t.Fatal(err)
	}
}

func storedSession(t *testing.T, directory string) string {
	t.Helper()

	writer, err := store.Create(directory, store.Meta{
		WorkspaceDir: t.TempDir(),
		Model:        "gpt-5.6-sol",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "first question"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	return writer.Name()
}

func write(t *testing.T, path string, bytes int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, bytes), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertGone(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s is still there", path)
	}
}

func report(screen string, failure string) string {
	return strings.Join([]string{
		"=== screen ===\n", screen,
		"=== failure ===\n", failure,
	}, "")
}

func assertGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if drawn != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}
