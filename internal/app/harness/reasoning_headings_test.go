package harness

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/ansi"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
)

func TestGoldenReasoningHeadingsKeepOnlyTheirConversationalBody(t *testing.T) {
	entries := readJournal(t, filepath.Join("testdata", "input", "reasoning-headings.jsonl"))
	screenPasses := map[string]func() string{}
	ansiPasses := map[string]func() string{}

	for renderingName, rendering := range everyReasoningRendering() {
		for widthName, columns := range everyThinkingWidth() {
			name := renderingName + " " + widthName
			screenPasses[name] = func() string {
				return shown(t, replayInto(newThinkingRig(t, rendering, false, columns), entries), columns)
			}
		}
		printed := replayInto(newThinkingRig(t, rendering, true, replayColumns), entries)
		replayed := replayInto(newThinkingRig(t, rendering, false, replayColumns), entries)
		requireNothingWasDrawnOver(t, printed)
		requireSameVisibleScreen(t, "printed reasoning headings differ from replay", replayed, printed)
		screenPasses[renderingName+" printed"] = func() string { return shown(t, printed, replayColumns) }
		ansiPasses[renderingName+" printed"] = func() string { return printed }
		ansiPasses[renderingName+" replayed"] = func() string { return replayed }

		for streamingName, streamingMode := range everyStreamingMode() {
			for stage, drawn := range arrivingReasoningHeading(t, rendering, streamingMode) {
				name := renderingName + " " + streamingName + " " + stage
				screenPasses[name] = func() string { return shownInLines(t, drawn, shortLines) }
			}
			for _, lines := range scrollingTerminalLines {
				rig := newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
					return output.NewTerminalOfSize(written, replayColumns, lines).
						LinkPathsUnder(link.Roots{Workspace: workspaceDir})
				})
				rig.chat.display.reasoningRendering = rendering
				rig.chat.display.streamingMode = streamingMode
				drawn := streamThrough(t, rig, entries)
				if strings.Contains(drawn, ansi.EraseScrollback) {
					t.Fatal("streaming reasoning headings cleared scrollback")
				}
				requireNothingDrawnAboveTheScreen(t, "streaming reasoning headings", drawn, lines)
				requireSameVisibleScreen(t, "streamed reasoning headings differ from replay", replayed, drawn)
				if lines == shortLines {
					name := renderingName + " streamed " + streamingName
					screenPasses[name] = func() string { return shown(t, drawn, replayColumns) }
					ansiPasses[name] = func() string { return drawn }
				}
			}
		}
	}

	compareWithGolden(t, "reasoning-heading", ".screen", screenPasses)
	compareWithGolden(t, "reasoning-heading", ".ansi", ansiPasses)
}

func arrivingReasoningHeading(t *testing.T, rendering output.ReasoningRendering, streamingMode output.StreamingMode) map[string]string {
	t.Helper()

	rig := newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.NewTerminalOfSize(written, replayColumns, shortLines).
			LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
	rig.chat.display.reasoningRendering = rendering
	rig.chat.display.streamingMode = streamingMode
	rig.chat.currentTurn = Turn{Stream: testRunningTurnStream(), painter: rig.chat.newPainter(true)}
	stages := []struct {
		name string
		text string
	}{
		{"1 open title", "**" + strings.TrimSpace(strings.Repeat("A provisional title ", 40))},
		{"2 closed title", "**"},
		{"3 first newline", "\n"},
		{"4 blank line", "\n"},
		{"5 whitespace only", " \t"},
		{"6 body arriving", "I"},
		{"7 complete body", " am keeping only the conversational body."},
	}
	passes := map[string]string{}
	var thought strings.Builder
	for _, stage := range stages {
		thought.WriteString(stage.text)
		streamDelta(rig.chat, agent.Delta{Kind: agent.ModelReasoningEvent, Text: stage.text})
		drawn := rig.drawn()
		if strings.Contains(drawn, ansi.EraseScrollback) {
			t.Fatal("an arriving reasoning heading cleared scrollback")
		}
		requireNothingDrawnAboveTheScreen(t, "an arriving reasoning heading", drawn, shortLines)
		passes[stage.name] = drawn
	}
	event := agent.Event{Kind: agent.ModelReasoningEvent, Text: thought.String()}
	rig.chat.currentTurn.painter.DrawEvent(event)
	passes["8 body settled"] = rig.drawn()
	replayed := replayInto(newThinkingRig(t, rendering, false, replayColumns), []replayEntry{{Event: &event}})
	requireSameVisibleScreen(t, "an arriving heading differs from replay", replayed, rig.drawn())

	return passes
}

func TestReasoningTitlesAreHiddenOnlyOnScreenAndResumeTheSameWay(t *testing.T) {
	thoughts := []string{
		"**Title-only reasoning**",
		"**Title-only reasoning**",
		"**Title-only reasoning**",
		"**Title-only reasoning**",
		"**A hidden title**\n\nI am keeping the conversational reasoning.",
		"An ordinary thought with **inline bold** stays.",
	}
	rig, _ := newEndpointRigOn(t, sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{{Think: thoughts, Say: "Finished thinking."}},
	}), sim.Responses)
	runTestBinary(t, rig.binary, rig.workspace, rig.environment, "-u")
	live := rig.start("--yolo", "-m", "codex/fake", "show title-only and conversational reasoning")
	live.waitFor("Finished thinking.")
	liveScreen := live.screen()
	live.quit()
	shown := strings.Join(liveScreen, "\n")
	if !strings.Contains(shown, strings.Repeat("Title-only reasoning\n", 4)) {
		t.Fatalf("title-only thoughts did not stay on four separate lines:\n%s", shown)
	}
	if strings.Contains(shown, "A hidden title") || !strings.Contains(shown, "I am keeping the conversational reasoning.") {
		t.Fatalf("the conversational reasoning kept its title or lost its body:\n%s", shown)
	}

	sessions := rig.storedSessions()
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want one", len(sessions))
	}
	stored, err := store.Read(filepath.Join(rig.stateDirectory, "org.crdx", "oh", "sessions"), sessions[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	var recorded []string
	for _, event := range stored.Events {
		if event.Kind == agent.ModelReasoningEvent {
			recorded = append(recorded, event.Text)
		}
	}
	if !slices.Equal(recorded, thoughts) {
		t.Fatalf("recorded reasoning %q, want the original %q", recorded, thoughts)
	}

	resumed := rig.start("-r", sessions[0].Name)
	resumed.waitFor("Finished thinking.")
	resumedScreen := resumed.screen()
	resumed.quit()
	if !slices.Equal(visibleRows(liveScreen), visibleRows(resumedScreen)) {
		t.Fatalf("live reasoning differs from resume:\nlive:\n%s\nresumed:\n%s", shown, strings.Join(resumedScreen, "\n"))
	}
}

func TestALongProvisionalReasoningTitleNeverReachesScrollback(t *testing.T) {
	title := "**" + strings.TrimSpace(strings.Repeat("A provisional title which wraps over many rows ", 30)) + "**\n\n"
	for renderingName, rendering := range everyReasoningRendering() {
		for streamingName, streamingMode := range everyStreamingMode() {
			for _, ending := range []string{"", " \t\n", "I\u2019m keeping only the conversational body."} {
				t.Run(renderingName+"/"+streamingName+"/"+ending, func(t *testing.T) {
					rig := newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
						return output.NewTerminalOfSize(written, replayColumns, shortLines).
							LinkPathsUnder(link.Roots{Workspace: workspaceDir})
					})
					rig.chat.display.reasoningRendering = rendering
					rig.chat.display.streamingMode = streamingMode
					thought := title + ending
					entries := []replayEntry{
						{Event: &agent.Event{Kind: agent.UserMessageEvent, Text: "think it over"}},
						{Event: &agent.Event{Kind: agent.ModelReasoningEvent, Text: thought}},
						{Event: &agent.Event{Kind: agent.ModelMessageEvent, Text: "Finished."}},
					}
					drawn := streamThrough(t, rig, entries)
					if strings.Contains(drawn, ansi.EraseScrollback) {
						t.Fatal("removing a provisional title cleared scrollback")
					}
					requireNothingDrawnAboveTheScreen(t, "streaming a long provisional title", drawn, shortLines)
					replayed := replayInto(newThinkingRig(t, rendering, false, replayColumns), entries)
					requireSameVisibleScreen(t, "a long title streamed differently from replay", replayed, drawn)
				})
			}
		}
	}
}
