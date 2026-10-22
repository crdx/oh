package harness

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/input"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/modeToggle"
	"crdx.org/oh/internal/app/sessions"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tasks"
)

const goldenForkSourceName = "able-dolphin"

func storedGoldenForkSource(
	t *testing.T,
	workspaceDir string,
	meta store.Meta,
	events ...agent.Event,
) *sessions.ForkSource {
	t.Helper()

	directory := t.TempDir()
	meta.WorkspaceDir = workspaceDir
	log, err := store.Create(directory, meta)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range append([]agent.Event{{Kind: agent.UserMessageEvent, Text: "begin"}}, events...) {
		if err := log.Event(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	renamed := session.Dir(directory, goldenForkSourceName)
	if err := os.Rename(session.Dir(directory, log.Name()), renamed); err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(renamed, "meta.json")
	encoded, err := os.ReadFile(metaPath) //nolint:gosec // a session the test created
	if err != nil {
		t.Fatal(err)
	}
	var sessionMeta session.Meta
	if err := json.Unmarshal(encoded, &sessionMeta); err != nil {
		t.Fatal(err)
	}
	sessionMeta.Name = goldenForkSourceName
	if encoded, err = json.Marshal(sessionMeta); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	forkSource, err := sessions.GetForkSource(directory, work.At(workspaceDir), goldenForkSourceName, "")
	if err != nil {
		t.Fatal(err)
	}

	return forkSource
}

type forkGoldenPaths struct {
	reference string
	missing   string
}

type forkGoldenSource struct {
	events           func(paths forkGoldenPaths) []agent.Event
	isOutsideSandbox bool
	refusesPorts     bool
	isHeadless       bool
}

type forkGoldenDrawing struct {
	screen string
	told   string
}

func forkGoldenTasks() agent.Event {
	return taskrecord.ListingEvent([]tasks.Snapshot{
		{ID: "task-1", ToolName: "bash", Run: 2, State: tasks.Running, IsRespawning: true, Output: "previous run"},
		{ID: "task-2", ToolName: "bash", Run: 1, State: tasks.Queued},
		{ID: "task-3", ToolName: "bash", Run: 1, State: tasks.Done, Output: "build complete"},
	})
}

func forkGoldenGrant(t *testing.T, path string, access pathgrant.Access) agent.Event {
	t.Helper()

	event, err := pathgrant.ChangeEvent(path, []pathgrant.Grant{{Path: path, Access: access}})
	if err != nil {
		t.Fatal(err)
	}

	return event
}

func forkGoldenForward(t *testing.T, routes ...portgrant.Route) agent.Event {
	t.Helper()

	event, err := portgrant.ForwardChangeEvent("127.1.2.3", routes[len(routes)-1].Port, routes)
	if err != nil {
		t.Fatal(err)
	}

	return event
}

func drawForkGolden(t *testing.T, source forkGoldenSource) forkGoldenDrawing {
	t.Helper()

	paths := forkGoldenPaths{
		reference: stableGoldenGrantPath(t, "reference", true),
		missing:   stableGoldenGrantPath(t, "missing", false),
	}

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	provider := &messageCaptureProvider{}
	self.agent = agent.New("", provider, nil)
	sessionsDir := t.TempDir()
	log, err := store.Create(sessionsDir, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	self.recorder = record.New(log)

	workspaceDir := t.TempDir()
	preparePathGrantCommands(t, self, openTestWorkspace(t, workspaceDir))
	forwarder := portgrant.Forwarder{}
	if !source.isOutsideSandbox {
		forwarder = portgrant.Forwarder{
			Forward: func(uint16) error {
				if source.refusesPorts {
					return errors.New("address already in use")
				}
				return nil
			},
			Revoke: func(uint16) error { return nil },
		}
	}
	if !source.isHeadless {
		self.tasks = tasks.New(t.Context())
		t.Cleanup(self.tasks.Close)
	}
	self.forwards = portgrant.NewForwards(forwarder, "127.9.9.9")

	forkSource := storedGoldenForkSource(t, workspaceDir, store.Meta{}, source.events(paths)...)

	self.carryOver(forkSource)
	self.start(forkSource.GetInitialUserMessage(forkSource.DroppedChatName))
	self.waitForCurrentTurn()
	if sourceTasks, wasRecorded := taskrecord.LastRecorded(forkSource.GetEvents()); wasRecorded {
		carried, isRecorded := taskrecord.LastRecorded(self.recordedEvents)
		if !isRecorded || len(carried) != len(sourceTasks) {
			t.Fatalf("fork lost task snapshots: %+v", carried)
		}
		for index, snapshot := range carried {
			if snapshot.State == tasks.Running || snapshot.State == tasks.Queued || snapshot.State == tasks.Stopping || snapshot.IsRespawning {
				t.Errorf("fork kept a task live: %+v", snapshot)
			}
			if snapshot.Output != sourceTasks[index].Output {
				t.Errorf("fork changed task output: %+v", snapshot)
			}
		}
	}

	drawn := screenOutput.String()
	var replayOutput bytes.Buffer
	replayHarness := &App{
		agent:          self.agent,
		screen:         output.NewTerminalOfSize(&replayOutput, replayColumns, replayLines),
		recordedEvents: self.recordedEvents,
	}
	replayHarness.replay()
	requireSameVisibleScreen(t, "a fork's opening differs from its replay", drawn, replayOutput.String())

	inputLine := edit.NewInput(nil)
	self.inputLine = inputLine
	self.show(inputLine)

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	transcript, err := os.ReadFile(filepath.Join(sessionsDir, sessionName, "chat.md")) //nolint:gosec // the test's own session
	if err != nil {
		t.Fatal(err)
	}

	told := "--- told the model ---\n" + strings.Join(provider.messages, "\n---\n") +
		"\n--- chat.md ---\n" + canonicalSessionTranscript(string(transcript), sessionName)

	stable := func(text string) string {
		text = stableGoldenGrantText(text, filepath.Dir(paths.reference))
		return strings.ReplaceAll(text, goldenProcessIdentity(), stableProcessIdentity)
	}

	return forkGoldenDrawing{screen: stable(screenOutput.String()), told: stable(told)}
}

func TestGoldenAForkCarriesItsSourceOver(t *testing.T) {
	sources := map[string]forkGoldenSource{
		"a source holding nothing to carry": {
			events: func(forkGoldenPaths) []agent.Event { return nil },
		},
		"a grant carried over": {
			events: func(paths forkGoldenPaths) []agent.Event {
				return []agent.Event{forkGoldenGrant(t, paths.reference, pathgrant.ReadAccess|pathgrant.WriteAccess)}
			},
		},
		"a grant that no longer resolves": {
			events: func(paths forkGoldenPaths) []agent.Event {
				return []agent.Event{forkGoldenGrant(t, paths.missing, pathgrant.ReadAccess)}
			},
		},
		"a grant revoked in the source": {
			events: func(paths forkGoldenPaths) []agent.Event {
				revoked, err := pathgrant.ChangeEvent(paths.reference, nil)
				if err != nil {
					t.Fatal(err)
				}
				return []agent.Event{forkGoldenGrant(t, paths.reference, pathgrant.ReadAccess), revoked}
			},
		},
		"a forward carried over": {
			events: func(forkGoldenPaths) []agent.Event {
				return []agent.Event{forkGoldenForward(t, portgrant.Route{Port: 8080})}
			},
		},
		"a forward that cannot be opened again": {
			refusesPorts: true,
			events: func(forkGoldenPaths) []agent.Event {
				return []agent.Event{forkGoldenForward(t, portgrant.Route{Port: 8080})}
			},
		},
		"running tasks carried over": {
			events: func(forkGoldenPaths) []agent.Event {
				return []agent.Event{forkGoldenTasks()}
			},
		},
		"tasks stopped when the source session closed": {
			events: func(forkGoldenPaths) []agent.Event {
				stopped := tasks.Snapshot{ID: "task-1", ToolName: "bash", Run: 1, State: tasks.Stopped, Failure: "the task supervisor is closed"}
				return []agent.Event{
					taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: stopped, IsFinal: true, IsSilent: true, IsSessionEnded: true}),
					taskrecord.ListingEvent([]tasks.Snapshot{stopped}),
				}
			},
		},
		"tasks carried into a headless fork": {
			isHeadless: true,
			events: func(forkGoldenPaths) []agent.Event {
				return []agent.Event{forkGoldenTasks()}
			},
		},
		"only finished tasks carried over": {
			events: func(forkGoldenPaths) []agent.Event {
				return []agent.Event{taskrecord.ListingEvent([]tasks.Snapshot{
					{ID: "task-3", ToolName: "bash", Run: 1, State: tasks.Done, Output: "build complete"},
				})}
			},
		},
		"everything carried over at once": {
			events: func(paths forkGoldenPaths) []agent.Event {
				return []agent.Event{
					forkGoldenGrant(t, paths.reference, pathgrant.ReadAccess),
					forkGoldenForward(t, portgrant.Route{Port: 3000}, portgrant.Route{Port: 8080, TaskName: "task-1"}),
					forkGoldenTasks(),
				}
			},
		},
		"a sandboxed source forked outside the sandbox": {
			isOutsideSandbox: true,
			events: func(paths forkGoldenPaths) []agent.Event {
				return []agent.Event{
					forkGoldenGrant(t, paths.reference, pathgrant.ReadAccess),
					forkGoldenForward(t, portgrant.Route{Port: 8080, TaskName: "task-1"}),
					forkGoldenTasks(),
				}
			},
		},
	}

	screens := map[string]func() string{}
	told := map[string]func() string{}
	for name, source := range sources {
		screens[name] = func() string { return drawForkGolden(t, source).screen }
		told[name] = func() string { return drawForkGolden(t, source).told }
	}

	compareWithGolden(t, "fork-carry-over", ".ansi", screens)
	compareWithGolden(t, "fork-carry-over", ".screen", shownPasses(t, screens))
	compareWithGolden(t, "fork-carry-over", ".txt", told)
}

func TestGoldenAForkDrawsItsSourcesModeAndConfinement(t *testing.T) {
	modeOf := func(isChosen bool) func() string {
		return func() string {
			recordedCaps := caps.Read | caps.Shell | caps.Git
			forkSource := storedGoldenForkSource(t, t.TempDir(), store.Meta{}, caps.ModeEvent(recordedCaps))
			forkedCaps, _ := sessions.ForkedCaps(caps.Read|caps.Shell|caps.Write, "", isChosen, "", forkSource)

			forkedHarness := &App{mode: caps.NewMode(forkedCaps)}
			modeSegment, err := modeToggle.New(forkedHarness.grantedCaps, forkedHarness.isPrefixPending)(nil)
			if err != nil {
				t.Fatal(err)
			}

			return modeSegment.Render(segment.Context{})
		}
	}
	rulesOf := func(isSourceYolo bool, isYoloChosen bool) func() string {
		return func() string {
			forkSource := storedGoldenForkSource(t, t.TempDir(), store.Meta{Yolo: isSourceYolo})
			forkedHarness := &App{runMode: runMode{isYolo: sessions.ForkedConfinement(isYoloChosen, forkSource)}}
			block := input.Block{
				Top:    input.Ruler{Left: "oh"},
				Input:  edit.Frame{Rows: []string{"> carry on"}},
				Bottom: input.Ruler{Right: "rxw ngl"},
				Rule:   forkedHarness.ruleStyle(),
			}
			rows, _, _ := block.Rows(narrowColumns)

			return strings.Join(rows, "\n")
		}
	}

	compareWithGolden(t, "fork-mode", ".ansi", map[string]func() string{
		"source left in rxg, forked without -c":           modeOf(false),
		"source left in rxg, forked with -c rxw":          modeOf(true),
		"sandboxed source, forked without the flag":       rulesOf(false, false),
		"sandboxed source, forked with the flag":          rulesOf(false, true),
		"source outside the sandbox, forked without flag": rulesOf(true, false),
	})
}
