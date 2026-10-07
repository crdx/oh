package harness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/session"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/pictures"
)

var (
	startupBanner   = regexp.MustCompile(`Agent \S+ \S+ ready in \d+ms on \w+, \d+ \w+ \d+ with (.*) ⧸ ~[\d.]+Kt context\.`)
	resumeCommand   = regexp.MustCompile(`oh(?:\.covered)? -r [a-z]+-[a-z]+`)
	checkedAt       = regexp.MustCompile(`"checked":\s*"[^"]*"`)
	endpointAddress = regexp.MustCompile(`127\.0\.0\.1:\d+`)
	wrappedBanner   = regexp.MustCompile(`(?s)Agent [a-z]+-[a-z]+ \S+ ready in \d+ms on .*?context\.`)
)

func scrubStartup(text string) string {
	text = startupBanner.ReplaceAllString(text, "Agent <session> ready with $1 ⧸ <context>.")
	text = resumeCommand.ReplaceAllString(text, "oh -r <session>")
	return endpointAddress.ReplaceAllString(text, "127.0.0.1:<port>")
}

type startupRun struct {
	rig              *interactiveRig
	isRefusingModels atomic.Bool
}

func newStartupRun(t *testing.T, configBody string, answers ...string) *startupRun {
	t.Helper()

	turns := make([]sim.Turn, 0, len(answers))
	for _, answer := range answers {
		turns = append(turns, sim.Turn{Say: answer})
	}
	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: turns})

	self := &startupRun{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if self.isRefusingModels.Load() && !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.Error(writer, "listings are down", http.StatusServiceUnavailable)
			return
		}
		endpoint.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)

	stateDirectory := t.TempDir()
	self.rig = &interactiveRig{
		t:              t,
		binary:         buildTestBinary(t),
		workspace:      reachableWorkspaceDir(t),
		stateDirectory: stateDirectory,
		environment: append(
			interactiveEnvironment(t, stateDirectory),
			backend.EndpointVariable+"="+endpoint.Addresses(server.URL)[sim.Completions],
			"TERM=xterm-256color",
		),
	}
	self.writeConfig(t, configBody)

	return self
}

func (self *startupRun) writeConfig(t *testing.T, body string) {
	t.Helper()

	writeRigConfig(t, self.rig, body)
}

func writeRigConfig(t *testing.T, rig *interactiveRig, body string) {
	t.Helper()

	directory := ""
	for _, variable := range rig.environment {
		if value, isConfig := strings.CutPrefix(variable, "XDG_CONFIG_HOME="); isConfig {
			directory = value
		}
	}
	path := filepath.Join(directory, "org.crdx", "oh", "config.toml")
	if err := os.WriteFile(path, fmt.Appendf(nil, "version = %d\n%s", config.Format, body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (self *startupRun) statePath(parts ...string) string {
	return filepath.Join(append([]string{self.rig.stateDirectory, "org.crdx", "oh"}, parts...)...)
}

func (self *startupRun) run(t *testing.T, arguments ...string) string {
	t.Helper()

	command := exec.CommandContext(t.Context(), self.rig.binary, arguments...) //nolint:gosec // running the binary under test
	command.Env = self.rig.environment
	command.Dir = self.rig.workspace
	output, err := command.CombinedOutput()

	status := "exit 0"
	if exitError, isExit := errors.AsType[*exec.ExitError](err); isExit {
		status = fmt.Sprintf("exit %d", exitError.ExitCode())
	} else if err != nil {
		t.Fatal(err)
	}

	return scrubStartup(fmt.Sprintf("$ oh %s\n%s[%s]\n", strings.Join(arguments, " "), output, status))
}

func (self *startupRun) sessionModels(t *testing.T) string {
	t.Helper()

	var models strings.Builder
	for _, stored := range self.rig.storedSessions() {
		fmt.Fprintf(&models, "session model: %s/%s@%s\n", stored.Meta.Provider, stored.Meta.Model, stored.Meta.Effort)
	}

	return models.String()
}

func (self *startupRun) onlySessionName(t *testing.T) string {
	t.Helper()

	stored := self.rig.storedSessions()
	if len(stored) != 1 {
		t.Fatalf("got %d stored sessions, want one", len(stored))
	}

	return stored[0].Name
}

func (self *startupRun) ageModelList(t *testing.T) {
	t.Helper()

	path := self.statePath("models.json")
	stale := time.Now().Add(-30 * 24 * time.Hour)
	data, err := os.ReadFile(path) //nolint:gosec // the test's own state
	if err != nil {
		t.Fatal(err)
	}
	aged := checkedAt.ReplaceAll(data, fmt.Appendf(nil, `"checked":%q`, stale.Format(time.RFC3339)))
	if string(aged) == string(data) {
		t.Fatal("the model list holds no check time to age")
	}
	if err := os.WriteFile(path, aged, 0o600); err != nil { //nolint:gosec // the test's own state
		t.Fatal(err)
	}
}

func TestGoldenStartupResolvesTheModelToUse(t *testing.T) {
	compareWithGolden(t, "startup-model-selection", ".txt", map[string]func() string{
		"a model named in full": func() string {
			run := newStartupRun(t, "", "Full answer.")
			return run.run(t, "-p", "--yolo", "-m", "opencode-go/fake", "a question") + run.sessionModels(t)
		},
		"a model named with an effort": func() string {
			run := newStartupRun(t, "", "Effort answer.")
			return run.run(t, "-p", "--yolo", "-m", "opencode-go/fake@low", "a question") + run.sessionModels(t)
		},
		"a model several providers offer": func() string {
			run := newStartupRun(t, "")
			return run.run(t, "-p", "--yolo", "-m", "fake", "a question") + run.sessionModels(t)
		},
		"a model nobody offers": func() string {
			run := newStartupRun(t, "")
			return run.run(t, "-p", "--yolo", "-m", "nonesuch", "a question") + run.sessionModels(t)
		},
		"a rotation naming an offered model": func() string {
			run := newStartupRun(t, "[model]\nround_robin = [\"opencode-go/fake\"]\n", "Rotated answer.")
			return run.run(t, "-p", "--yolo", "a question") + run.sessionModels(t)
		},
		"a rotation naming a model nobody offers": func() string {
			run := newStartupRun(t, "[model]\nround_robin = [\"opencode-go/fake\", \"opencode-go/nonesuch\"]\n")
			return run.run(t, "-p", "--yolo", "a question") + run.sessionModels(t)
		},
		"a resumed session keeps its model over the rotation": func() string {
			run := newStartupRun(t, "", "First answer.", "Resumed answer.")
			first := run.run(t, "-p", "--yolo", "-m", "opencode-go/fake@low", "a question")
			name := run.onlySessionName(t)
			run.writeConfig(t, "[model]\nround_robin = [\"opencode-go/fake@high\"]\n")
			resumed := run.run(t, "-p", "-r", name, "another question")
			return strings.ReplaceAll(first+resumed, name, "<session>") + run.sessionModels(t)
		},
		"a stale model list refreshed before choosing": func() string {
			run := newStartupRun(t, "", "First answer.", "Refreshed answer.")
			first := run.run(t, "-p", "--yolo", "-m", "opencode-go/fake", "a question")
			run.ageModelList(t)
			return first + run.run(t, "-p", "--yolo", "-m", "opencode-go/fake", "a question") + run.sessionModels(t)
		},
		"a stale model list that cannot be refreshed": func() string {
			run := newStartupRun(t, "", "First answer.", "Unrefreshed answer.")
			first := run.run(t, "-p", "--yolo", "-m", "opencode-go/fake", "a question")
			run.ageModelList(t)
			run.isRefusingModels.Store(true)
			return first + run.run(t, "-p", "--yolo", "-m", "opencode-go/fake", "a question") + run.sessionModels(t)
		},
	})
}

func TestGoldenExecutableCompletionReadsTheConfiguredTools(t *testing.T) {
	compareWithGolden(t, "completion-sources", ".txt", map[string]func() string{
		"built-in tools without a custom declaration": func() string {
			run := newStartupRun(t, "")
			return run.run(t, "--complete", "tool", "re") + run.run(t, "--complete", "tool", "we")
		},
		"a declared tool and its capability group": func() string {
			run := newStartupRun(t, "[tools.weather]\ndescription = \"report the weather\"\ncommand = [\"true\"]\ngroup = \"a\"\n")
			return run.run(t, "--complete", "tool", "we") + run.run(t, "--complete", "caps", "rxwngl")
		},
		"invalid config keeps the built-in tools": func() string {
			run := newStartupRun(t, "[tools.weather]\ngroup = 5\n")
			return run.run(t, "--complete", "tool", "re") + run.run(t, "--complete", "tool", "we")
		},
	})
}

func TestGoldenThePickerListsStoredAndArchivedSessions(t *testing.T) {
	rig := newInteractiveRig(t)
	sessionsDirectory := filepath.Join(rig.stateDirectory, "org.crdx", "oh", "sessions")
	if err := os.MkdirAll(sessionsDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	threeHoursAgo := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	for _, name := range []string{"able-ant", "zany-zebra", "bold-bee", "tidy-tiger"} {
		writeStoredSession(t, sessionsDirectory, rig.workspace, name, threeHoursAgo)
	}
	for _, name := range []string{"bold-bee", "tidy-tiger"} {
		if err := session.Archive(sessionsDirectory, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Not-A-Name", "odd-owl.tgz"} {
		if err := os.Mkdir(filepath.Join(sessionsDirectory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Bad-Name.tgz", "loose-file", "calm-cat.tar"} {
		if err := os.WriteFile(filepath.Join(sessionsDirectory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	session := rig.start("-r")
	session.waitFor("zany-zebra")
	session.waitToSettle()
	stored := strings.Join(session.screen(), "\n")
	session.typeAndSettle("\x1b[C")
	session.waitFor("tidy-tiger")
	archived := strings.Join(session.screen(), "\n")
	session.typeText("\x03")
	session.waitToExit()

	compareWithGolden(t, "picker-listing", ".screen", map[string]func() string{
		"stored":   func() string { return stored },
		"archived": func() string { return archived },
	})
}

const (
	spendBar = `
[bar.top]
left = [{ segment = "session-spend" }]
center = []
right = []

[bar.bottom]
left = []
center = []
right = []
`
	emptyBar = `
[bar.top]
left = []
center = []
right = []

[bar.bottom]
left = []
center = []
right = []
`
)

func TestGoldenASessionCountsItsSpendInTheConfiguredCurrency(t *testing.T) {
	spendScreen := func(configBody string, rates string) string {
		run := newStartupRun(t, configBody+spendBar, "Priced answer.")
		if rates != "" {
			if err := os.MkdirAll(run.statePath(), 0o700); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"version":1,"fetched":%q,"base":"USD","rates":%s}`, time.Now().Format(time.RFC3339), rates)
			if err := os.WriteFile(run.statePath("rates.json"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}

		session := run.rig.start("--yolo", "-m", "opencode-go/fake")
		session.waitFor(readyBanner)
		session.typeText("a question" + pressEnter)
		session.waitFor("Priced answer.")
		session.waitToSettle()
		screen := wrappedBanner.ReplaceAllString(strings.Join(session.screen(), "\n"), "Agent <banner>")
		session.quit()

		return screen
	}

	compareWithGolden(t, "spend-currency", ".screen", map[string]func() string{
		"dollars by default": func() string { return spendScreen("", "") },
		"pounds from a current rate": func() string {
			return spendScreen("[ui]\ncurrency = \"gbp\"\n", `{"GBP":0.5}`)
		},
	})
}

func TestGoldenAFailedRateRefreshKeepsTheCachedCurrency(t *testing.T) {
	compareWithGolden(t, "rate-refresh", ".txt", map[string]func() string{
		"no cached rate": func() string {
			t.Setenv(location.StateDirVariable, t.TempDir())
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			var notice strings.Builder
			currency := ensureCurrency(ctx, &notice, "GBP", false)
			return notice.String() + "spend: " + currency.Format(2) + "\n"
		},
		"stale cached rate": func() string {
			directory := t.TempDir()
			t.Setenv(location.StateDirVariable, directory)
			body := fmt.Sprintf(`{"version":1,"fetched":%q,"base":"USD","rates":{"GBP":0.5}}`, time.Now().Add(-48*time.Hour).Format(time.RFC3339))
			if err := os.WriteFile(location.GetExchangeRateCachePath(), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			var notice strings.Builder
			currency := ensureCurrency(ctx, &notice, "GBP", false)
			return notice.String() + "spend: " + currency.Format(2) + "\n"
		},
	})
}

const (
	graphicsProbe  = "\x1b_Gi=1,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"
	graphicsAnswer = "\x1b_Gi=1;OK\x1b\\\x1b[?62;4c"
)

func answerGraphicsProbes(session *interactiveSession) {
	go func() {
		for asked := 1; session.WaitForCount(graphicsProbe, asked, interactiveDeadline); asked++ {
			if _, err := session.typing.WriteString(graphicsAnswer); err != nil {
				return
			}
		}
	}()
}

func TestGoldenAPictureIsDrawnOnlyWhereTheTerminalSaidItCould(t *testing.T) {
	picture, err := os.ReadFile(filepath.Join(
		"testdata", "input", "pictures", "9e0f33117e1831a53359e08b58884030cc8df3582cd71fffbe3fd794b1f355bb-w800.png",
	))
	if err != nil {
		t.Fatal(err)
	}

	type pictureDrawing struct {
		screen   string
		protocol string
	}
	pictureScreen := func(isAnswering bool) pictureDrawing {
		rig := newScriptedRig(t,
			sim.Turn{Calls: []sim.Call{{Name: "read", Arguments: `{"path":"picture.png"}`}}},
			sim.Turn{Say: "Seen it."},
		)
		if err := os.WriteFile(filepath.Join(rig.workspace, "picture.png"), picture, 0o600); err != nil { //nolint:gosec // the test's own workspace
			t.Fatal(err)
		}
		writeRigConfig(t, rig, emptyBar)

		session := rig.start("--yolo", "-m", "opencode-go/fake")
		if isAnswering {
			answerGraphicsProbes(session)
		}
		session.waitFor(readyBanner)
		session.typeText("look at the picture" + pressEnter)
		session.waitFor("Seen it.")
		session.waitToSettle()
		screen := wrappedBanner.ReplaceAllString(strings.Join(session.screen(), "\n"), "Agent <banner>")
		asked := session.Count(graphicsProbe)
		transmissions := regexp.MustCompile(`\x1b_Ga=T,[^\x1b]*\x1b\\`).FindAllString(session.String(), -1)
		var protocol strings.Builder
		fmt.Fprintf(&protocol, "graphics probes: %d\ntransmissions: %d\n", asked, len(transmissions))
		for _, transmission := range transmissions {
			header, payload, isFound := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(transmission, "\x1b_G"), "\x1b\\"), ";")
			if !isFound || !strings.Contains(header, "f=100,t=f,") {
				t.Fatalf("picture was not transmitted by file as PNG: %q", header)
			}
			path, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(picture)
			wantName := fmt.Sprintf("%x-w%d.png", digest, pictures.DisplayWidth)
			if filepath.Base(string(path)) != wantName || filepath.Base(filepath.Dir(string(path))) != "images" {
				t.Fatalf("transmitted %q instead of the stored picture", path)
			}
			stored, err := os.ReadFile(string(path))
			if err != nil {
				t.Fatal(err)
			}
			image, err := png.DecodeConfig(bytes.NewReader(stored))
			if err != nil || image.Width <= 0 || image.Height <= 0 {
				t.Fatalf("transmitted picture is not a valid PNG: %v", err)
			}
			fmt.Fprintf(&protocol, "format: png file\nimage: %dx%d\n", image.Width, image.Height)
		}
		session.quit()

		return pictureDrawing{screen: fmt.Sprintf("graphics probes: %d\n%s", asked, screen), protocol: protocol.String()}
	}

	answered := pictureScreen(true)
	ignored := pictureScreen(false)
	compareWithGolden(t, "picture-detection", ".screen", map[string]func() string{
		"a terminal that answers the graphics probe": func() string { return answered.screen },
		"a terminal that ignores the graphics probe": func() string { return ignored.screen },
	})
	compareWithGolden(t, "picture-detection", ".txt", map[string]func() string{
		"a terminal that answers the graphics probe": func() string { return answered.protocol },
		"a terminal that ignores the graphics probe": func() string { return ignored.protocol },
	})
}
