package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/bar"
	"crdx.org/io/internal/app/caps"
	"crdx.org/io/internal/app/config"
	"crdx.org/io/internal/app/cycle"
	"crdx.org/io/internal/app/pathgrant"
	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/turn"
	"crdx.org/io/internal/app/usage"
	"crdx.org/io/internal/app/work"
	"crdx.org/io/internal/jobs"
	"crdx.org/io/internal/money"
)

type setting struct {
	name   string
	values []string
}

func soundSettings() []setting {
	return []setting{
		{"caps", []string{
			"",
			"[caps]\ndefault = \"r\"\n",
			"[caps]\ndefault = \"rwxngl\"\n",
			"[caps]\ndefault = \"\"\n",
		}},
		{"editor", []string{
			"",
			"[editor]\ncommand = [\"vim\"]\n",
			"[editor]\ncommand = []\n",
		}},
		{"input", []string{
			"",
			"[input]\ncontinue = \"carry on\"\n",
		}},
		{"model", []string{
			"",
			"[model]\nround_robin = [\"anthropic/one\"]\n",
			"[model]\nround_robin = [\"a\", \"a\", \"b\"]\neffort = \"low\"\nfast = true\n",
			"[model]\neffort = \"none\"\n",
		}},
		{"provider", []string{
			"",
			"[provider.ollama]\nhost = \"http://127.0.0.1:11434\"\n",
		}},
		{"ports", []string{
			"",
			"[ports]\nhostname = \"{session}\"\n",
			"[ports]\nhostname = \"{session}.oh.test\"\n",
			"[ports]\nhostname = \"\"\n",
		}},
		{"snippets", []string{
			"",
			"[snippets]\nreview = \"review {{ .Arg }}\"\n",
			"[snippets.plan]\nprompt = \"plan it\"\ndescription = \"plan\"\n",
		}},
		{"skills", []string{
			"",
			"[skills]\ninclude = [\"/tmp/skills\"]\nexclude = [\"one\"]\n",
			"[skills]\ninclude = []\nexclude = []\n",
		}},
		{"sandbox", []string{
			"",
			"[sandbox]\nread = [\"/etc\"]\nwrite = [\"/tmp/w\"]\nexec = [\"/usr/bin\"]\n",
			"[sandbox]\ndeny = [\"secrets.yml\"]\n",
			"[sandbox]\nread = [\"/etc\", \"/etc\"]\n",
		}},
		{"ui", []string{
			"",
			"[ui]\nstreaming = \"asap\"\n",
			"[ui]\nstreaming = \"paced\"\nreasoning = \"markdown\"\n",
			"[ui]\ngrouping = []\n",
			"[ui]\ngrouping = [\"notice reasoning tool answer\"]\n",
			"[ui]\ncurrency = \"GBP\"\n",
		}},
		{"theme", []string{
			"",
			"[ui.theme]\nnormal = \"#010203 bold\"\n",
			"[ui.theme]\ndim = \"default\"\naccent = \"underline:curly\"\n",
			"[ui.theme]\nuser = \"#ffffff\"\nharness = \"reverse\"\n",
		}},
		{"tool", []string{
			"",
			"[tool]\noutput = \"1024\"\n",
			"[tool]\noutput = \"64K\"\n",
			"[tool]\noutput = \"1M\"\n",
		}},
		{"permissions", []string{
			"",
			"[permissions]\nnetwork = \"allow\"\n",
			"[permissions]\nnetwork = \"ask\"\nlookup = \"ask\"\nfetch = \"allow\"\n",
		}},
		{"experimental", []string{
			"",
			"[experimental]\n",
		}},
		{"bar", []string{
			"",
			"[bar.top]\nleft = []\ncenter = []\nright = []\n",
			"[bar.bottom]\nleft = [{ segment = \"workspace-dir\", type = \"base\" }]\n",
			"[bar.bottom]\nleft = [{ segment = \"workspace-dir\", type = \"full\" }]\n",
			"[bar.top]\nleft = [{ segment = \"activity-spinner\", idle = \"…\", frames = [\"…\"], rate = \"1s\" }]\n",
			"[bar.top]\nright = [{ segment = \"scroll-overflow\", direction = \"down\" }]\n",
			"[bar.bottom]\nright = [{ segment = \"session-name\", emoji = true }, { segment = \"session-emoji\" }]\n",
			"[bar.top]\ncenter = [{ segment = \"local-time\", format = \"15:04\" }]\n",
			"[bar.top]\ncenter = [{ segment = \"git-branch\", rate = \"10s\" }]\n",
			"[bar.bottom]\ncenter = [{ segment = \"subscription-usage\", rate = \"1m\" }]\n",
			"[bar.top]\nleft = [{ segment = \"cache-usage\" }, { segment = \"context-usage\" }," +
				" { segment = \"turn-timer\" }, { segment = \"turn-count\" }," +
				" { segment = \"session-spend\" }, { segment = \"fast-mode\" }," +
				" { segment = \"mode-toggle\" }, { segment = \"path-grants\" }," +
				" { segment = \"exposed-ports\" }, { segment = \"jobs\" }," +
				" { segment = \"active-model\" }]\n",
		}},
	}
}

type chooser struct {
	data []byte
	at   int
}

func (self *chooser) pick(count int) int {
	if count <= 1 || self.at >= len(self.data) {
		return 0
	}
	choice := int(self.data[self.at]) % count
	self.at++
	return choice
}

func assemble(seed []byte, settings []setting) (string, []string) {
	pick := &chooser{data: seed}

	var body strings.Builder
	var chosen []string

	for _, entry := range settings {
		value := entry.values[pick.pick(len(entry.values))]
		if value == "" {
			continue
		}
		_, _ = body.WriteString(value)
		chosen = append(chosen, entry.name)
	}

	return body.String(), chosen
}

func testRegistry() segment.Registry {
	return bar.NewRegistry(bar.Options{
		Workspace:         work.At("/tmp/workspace"),
		Session:           cycle.Session{Name: "endless-bulldog", Model: "one", Effort: "high"},
		ModelEffortLevels: []string{"low", "high"},
		UsageCachePath:    filepath.Join(os.TempDir(), "usage-absent.json"),
		UsageGauges:       usage.FixedGauges(usage.Graphics{}),
		Currency:          money.Dollar(),
		SandboxHostname:   "127.0.0.1",
		Sources: bar.Sources{
			IsTurnRunning:         func() bool { return false },
			IsSessionPersisted:    func() bool { return true },
			GetContextUsage:       func() (int, int) { return 1, 2 },
			GetCacheUsage:         func() (int, int) { return 1, 2 },
			GetSessionSpend:       func() (float64, bool) { return 0, false },
			GetGrantedCaps:        caps.All,
			GetPathGrants:         func() []pathgrant.Grant { return nil },
			GetHostToSandboxPorts: func() []uint16 { return nil },
			GetSandboxToHostPorts: func() []uint16 { return nil },
			IsPrefixPending:       func() bool { return false },
			GetTurnTiming:         func() turn.Timing { return turn.Timing{} },
			GetTurnCount:          func() int { return 0 },
			GetJobs:               func() []jobs.Snapshot { return nil },
		},
	})
}

func exercise(t *testing.T, settings config.Config) {
	t.Helper()

	restoreTheme := style.ApplyTheme(settings.Ui.Theme)
	defer restoreTheme()

	live, err := settings.BuildLive(testRegistry())
	if err != nil {
		t.Fatalf("a sound config would not build: %v", err)
	}

	for _, position := range segment.Positions {
		for _, cells := range []int{0, 1, 2, 7, 40, 200} {
			_ = bar.RenderWithin(live.SegmentLayout, position, segment.Context{
				HiddenLinesAbove: 3,
				HiddenLinesBelow: 4,
			}, cells)
		}
	}

	_ = live.SegmentLayout.NextRefresh(segment.Phase{At: time.Now(), IsRunning: true})
	_ = settings.UnknownSettings()
}

func FuzzEverySoundCombinationStarts(fuzzer *testing.F) {
	for seed := range 64 {
		fuzzer.Add([]byte{
			byte(seed), byte(seed / 2), byte(seed / 3), byte(seed / 5), byte(seed / 7),
			byte(seed / 11), byte(seed / 13), byte(seed / 17), byte(seed / 19), byte(seed / 23),
			byte(seed / 29), byte(seed / 31), byte(seed / 37), byte(seed / 41), byte(seed / 43),
			byte(seed / 47),
		})
	}

	directory := fuzzer.TempDir()

	fuzzer.Fuzz(func(t *testing.T, seed []byte) {
		body, chosen := assemble(seed, soundSettings())

		path := filepath.Join(directory, "config.toml")
		written := fmt.Sprintf("version = %d\n", config.Format) + body
		if err := os.WriteFile(path, []byte(written), 0o600); err != nil {
			t.Fatal(err)
		}

		settings, err := config.Load(path)
		if err != nil {
			t.Fatalf("a sound config was refused: %v\nchosen: %s\n%s", err, strings.Join(chosen, ", "), written)
		}

		exercise(t, settings)
	})
}
