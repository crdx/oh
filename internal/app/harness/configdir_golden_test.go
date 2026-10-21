package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/location"
)

const (
	xdgConfigMarker       = "The XDG config speaks."
	relocatedConfigMarker = "The relocated config speaks."
)

type configDirectoryRun struct {
	*startupRun

	xdgDirectory string
}

func newConfigDirectoryRun(t *testing.T, answers ...string) *configDirectoryRun {
	t.Helper()

	run := &configDirectoryRun{startupRun: newStartupRun(t, "[agent]\nround_robin = [\"opencode-go/fake@low\"]\n", answers...)}
	for _, variable := range run.rig.environment {
		if value, isConfig := strings.CutPrefix(variable, "XDG_CONFIG_HOME="); isConfig {
			run.xdgDirectory = filepath.Join(value, "org.crdx", "oh")
		}
	}
	writeConfigDirectoryExtras(t, run.xdgDirectory, "xdg", xdgConfigMarker)

	return run
}

func writeConfigDirectoryExtras(t *testing.T, directory string, name string, marker string) {
	t.Helper()

	files := map[string]string{
		"SYSTEM.md": marker,
		filepath.Join("skills", name+"-skill", "SKILL.md"):   "---\nname: " + name + "-skill\ndescription: Belongs to the " + name + " config.\n---\nBody",
		filepath.Join("snippets", name+"-snippet.md"):        "A snippet from the " + name + " config.",
		filepath.Join("snippets", name+"-second-snippet.md"): "Another snippet from the " + name + " config.",
	}
	for path, body := range files {
		writeConfigDirectoryFile(t, filepath.Join(directory, path), body)
	}
}

func writeConfigDirectoryFile(t *testing.T, path string, body string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (self *configDirectoryRun) relocate(value string) {
	self.rig.environment = append(self.rig.environment, location.ConfigDirVariable+"="+value)
}

func (self *configDirectoryRun) report(t *testing.T, output string, relocatedDirectory string) string {
	t.Helper()

	var report strings.Builder
	report.WriteString(output)
	report.WriteString(self.sessionModels(t))
	for _, request := range self.endpoint.Requests() {
		for _, marker := range []string{xdgConfigMarker, relocatedConfigMarker, "xdg-skill", "relocated-skill"} {
			if strings.Contains(request.Instructions, marker) {
				fmt.Fprintf(&report, "instructions carry: %s\n", marker)
			}
		}
	}

	text := strings.ReplaceAll(report.String(), self.xdgDirectory, "<xdg-config>")
	if relocatedDirectory != "" {
		text = strings.ReplaceAll(text, relocatedDirectory, "<relocated-config>")
	}

	return text
}

func TestGoldenTheConfigDirectoryIsResolvedFromTheEnvironment(t *testing.T) {
	compareWithGolden(t, "config-directory", ".txt", map[string]func() string{
		"the XDG config when OH_CONFIG_DIR is unset": func() string {
			run := newConfigDirectoryRun(t, "XDG answer.")
			return run.report(t, run.run(t, "-p", "--yolo", "a question"), "")
		},
		"the XDG config when OH_CONFIG_DIR is empty": func() string {
			run := newConfigDirectoryRun(t, "Empty answer.")
			run.relocate("")
			return run.report(t, run.run(t, "-p", "--yolo", "a question"), "")
		},
		"an absolute OH_CONFIG_DIR replaces the XDG config": func() string {
			run := newConfigDirectoryRun(t, "Relocated answer.")
			relocated := t.TempDir()
			run.writeRelocatedConfig(t, relocated)
			run.relocate(relocated)
			return run.report(t, run.run(t, "-p", "--yolo", "a question"), relocated)
		},
		"an absolute OH_CONFIG_DIR with nothing in it takes nothing from the XDG config": func() string {
			run := newConfigDirectoryRun(t, "Bare answer.")
			relocated := t.TempDir()
			run.relocate(relocated)
			return run.report(t, run.run(t, "-p", "--yolo", "-m", "opencode-go/fake@medium", "a question"), relocated)
		},
		"a broken config in an absolute OH_CONFIG_DIR is named by its path": func() string {
			run := newConfigDirectoryRun(t)
			relocated := t.TempDir()
			writeConfigDirectoryFile(t, filepath.Join(relocated, "config.toml"), "not = = toml\n")
			run.relocate(relocated)
			return run.report(t, run.run(t, "-p", "--yolo", "a question"), relocated)
		},
		"a relative OH_CONFIG_DIR is ignored": func() string {
			run := newConfigDirectoryRun(t, "Relative answer.")
			run.writeRelocatedConfig(t, filepath.Join(run.rig.workspace, "relocated"))
			run.relocate("relocated")
			return run.report(t, run.run(t, "-p", "--yolo", "a question"), "")
		},
	})
}

func (self *configDirectoryRun) writeRelocatedConfig(t *testing.T, directory string) {
	t.Helper()

	writeConfigDirectoryFile(t, filepath.Join(directory, "config.toml"), fmt.Sprintf("version = %d\n[agent]\nround_robin = [\"opencode-go/fake@high\"]\n", config.Format))
	writeConfigDirectoryExtras(t, directory, "relocated", relocatedConfigMarker)
	writeConfigDirectoryFile(t, filepath.Join(directory, "snippets", "relocated-third-snippet.md"), "A third snippet from the relocated config.")
}
