package startup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"crdx.org/io/internal/app/style"
	"crdx.org/io/pkg/agent"
)

func TestAStartupEventKeepsItsFactsForReplay(t *testing.T) {
	info := Info{
		Session:       "tame-impala",
		PromptBytes:   32_697,
		ProjectSkills: 2,
		GlobalSkills:  3,
		Snippets:      4,
		ToolBytes:     3373,
		LocalConfig: &LocalConfig{
			Name:     "oh.toml",
			Settings: []string{"ui.currency", "sandbox.write"},
		},
	}

	event := NewEvent(12*time.Millisecond, info)
	if facts := string(event.State); !strings.Contains(facts, `"local_config":{"name":"oh.toml","settings":["ui.currency","sandbox.write"]}`) {
		t.Errorf("local config facts were not grouped in %s", facts)
	}
	got := style.Plain(RenderEvent(event, 80, false))
	want := "Agent tame-impala 🦌 ready in 12ms with 5 skills ⧸ 4 snippets ⧸ ~13Kt context ⧸ oh.toml: ui.currency, sandbox.write."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStartupSeparatorsTakeTheAccentColour(t *testing.T) {
	line := RenderBanner(time.Millisecond, false, Info{}, 80, false)
	separator := style.Accent(startupDetailsSeparator)

	if count := strings.Count(line, separator); count != 2 {
		t.Errorf("got %d accent separators in %q, want 2", count, line)
	}
}

func TestALocalConfigAddsAnAccentSeparatorAndMutableFileColour(t *testing.T) {
	line := RenderBanner(time.Millisecond, false, Info{
		LocalConfig: &LocalConfig{Name: "oh.toml", Settings: []string{"ui.streaming"}},
	}, 80, false)

	if count := strings.Count(line, style.Accent(startupDetailsSeparator)); count != 3 {
		t.Errorf("got %d accent separators in %q, want 3", count, line)
	}
	if !strings.Contains(line, style.Change("oh.toml")) {
		t.Errorf("override file did not take the mutable colour in %q", line)
	}
}

func TestAnEmptyLocalConfigShowsAsEmpty(t *testing.T) {
	line := style.Plain(RenderBanner(time.Millisecond, false, Info{
		LocalConfig: &LocalConfig{Name: "oh.toml"},
	}, 80, false))
	want := "Agent ready in 1ms with 0 skills ⧸ 0 snippets ⧸ 0t context ⧸ oh.toml: (empty)."
	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

func TestMalformedStartupFactsRenderNothing(t *testing.T) {
	event := agent.Event{Kind: agent.StartupEvent, State: json.RawMessage("{")}
	if got := RenderEvent(event, 80, false); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
}

func TestTookReportsTheScaleAStartupHappensOn(t *testing.T) {
	for elapsed, want := range map[time.Duration]string{
		400 * time.Microsecond:  "400µs",
		12 * time.Millisecond:   "12ms",
		1500 * time.Millisecond: "1.5s",
	} {
		if got := timeTaken(elapsed); got != want {
			t.Errorf("took(%v) = %q, want %q", elapsed, got, want)
		}
	}
}

func TestStartupQuantitiesPutOnlyTheirNumbersInTheNormalForeground(t *testing.T) {
	for name, test := range map[string]struct {
		quantity   string
		unitNormal bool
		want       string
	}{
		"duration":       {"355µs", false, style.Normal("355") + style.Subtle("µs")},
		"file size":      {"1G", true, style.Normal("1G")},
		"token estimate": {"~1.22Kt", false, style.Subtle("~") + style.Normal("1.22") + style.Subtle("Kt")},
		"no number":      {"none", false, style.Subtle("none")},
	} {
		t.Run(name, func(t *testing.T) {
			var line startupLine
			line.quantity(test.quantity, test.unitNormal)
			if got := line.String(); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestOneSkillAndOneSnippetAreCountedInTheSingular(t *testing.T) {
	line := style.Plain(RenderBanner(
		time.Millisecond,
		false,
		Info{GlobalSkills: 1, Snippets: 1},
		80,
		false,
	))
	want := "Agent ready in 1ms with 1 skill ⧸ 1 snippet ⧸ 0t context."

	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

func TestAnEmptyStartupSaysTheSentenceAlone(t *testing.T) {
	line := style.Plain(RenderBanner(time.Millisecond, false, Info{}, 80, false))
	want := "Agent ready in 1ms with 0 skills ⧸ 0 snippets ⧸ 0t context."

	if line != want {
		t.Errorf("got %q, want %q", line, want)
	}
}

func TestAResumedConversationHasNoStartupLine(t *testing.T) {
	line := RenderBanner(time.Millisecond, true, Info{ProjectSkills: 2}, 80, false)

	if line != "" {
		t.Errorf("expected no startup line, got %q", line)
	}
}

func TestKittyGetsATwoRowStartupBannerWithASizedEmoji(t *testing.T) {
	line := RenderBanner(time.Millisecond, false, Info{Session: "tame-impala"}, 80, true)

	if !strings.HasPrefix(line, " \x1b]66;s=2:w=2;🦌\x1b\\") {
		t.Errorf("expected a sized impala, got %q", line)
	}
	want := " 🦌  Agent tame-impala ready in 1ms\n  0 skills ⧸ 0 snippets ⧸ 0t context"
	if got := style.Plain(line); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAHeadingThatDoesNotFitBesideTheEmojiGetsTheOrdinaryStartupSentence(t *testing.T) {
	info := Info{Session: "tame-impala"}
	headingWidth := style.Width(renderHeading(time.Millisecond, info, false))
	columns := bannerLeftPadding + sizedEmojiCells + bannerGap + headingWidth - 1
	line := RenderBanner(time.Millisecond, false, info, columns, true)

	if strings.Contains(line, "\x1b]66;") {
		t.Errorf("expected no sized text when the heading cannot fit, got %q", line)
	}
}

func TestWaitingOnAPersonIsNotTimeTheHarnessTook(t *testing.T) {
	before := Elapsed()

	Wait(func() { time.Sleep(20 * time.Millisecond) })

	if waited := Elapsed() - before; waited > 10*time.Millisecond {
		t.Errorf("expected the wait to be taken off the clock, got %s of it", waited)
	}
}
