package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"iter"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/bar"
	"crdx.org/oh/internal/app/call"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/cli"
	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/demo"
	"crdx.org/oh/internal/app/dispatch"
	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/escape"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/input"
	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/jobrecord"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/menu"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/internal/app/pictures"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/preview"
	"crdx.org/oh/internal/app/prompt"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/activeModel"
	"crdx.org/oh/internal/app/segment/activitySpinner"
	"crdx.org/oh/internal/app/segment/cacheUsage"
	"crdx.org/oh/internal/app/segment/contextUsage"
	"crdx.org/oh/internal/app/segment/exposedPorts"
	"crdx.org/oh/internal/app/segment/fastMode"
	"crdx.org/oh/internal/app/segment/gitBranch"
	"crdx.org/oh/internal/app/segment/jobNames"
	"crdx.org/oh/internal/app/segment/localTime"
	"crdx.org/oh/internal/app/segment/modeToggle"
	"crdx.org/oh/internal/app/segment/pathGrants"
	"crdx.org/oh/internal/app/segment/scrollOverflow"
	"crdx.org/oh/internal/app/segment/sessionEmoji"
	"crdx.org/oh/internal/app/segment/sessionName"
	"crdx.org/oh/internal/app/segment/sessionSpend"
	"crdx.org/oh/internal/app/segment/subUsage"
	"crdx.org/oh/internal/app/segment/turnCount"
	"crdx.org/oh/internal/app/segment/turnTimer"
	"crdx.org/oh/internal/app/segment/workspaceDir"
	"crdx.org/oh/internal/app/sessions"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/snippets"
	"crdx.org/oh/internal/app/startup"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/store/transcript"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/usage"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/auth"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/internal/req"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/internal/stop"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/imageutil"
	"crdx.org/oh/internal/util/pathutil"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/internal/waiting"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/ask"
	"crdx.org/oh/pkg/provider/anthropic"
	"crdx.org/oh/pkg/provider/codex"
	"crdx.org/oh/pkg/provider/ollama"
	"crdx.org/oh/pkg/provider/opencodego"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/command"
	"crdx.org/oh/pkg/tool/middleware/truncate"
	"crdx.org/oh/pkg/toolbox"
	"crdx.org/oh/pkg/toolbox/bash"
	"crdx.org/oh/pkg/toolbox/expose"
	"crdx.org/oh/pkg/toolbox/fetch"
	"crdx.org/oh/pkg/toolbox/job"
	"crdx.org/oh/pkg/toolbox/lookup"
	"crdx.org/oh/pkg/toolbox/notify"
	"crdx.org/oh/pkg/toolbox/read"
	"crdx.org/oh/pkg/toolbox/title"
	"crdx.org/oh/pkg/wire/openai/chatcompletions"
)

const terminalInputColumns = 40

func TestGoldenSpecialLinksDrawWhatTheyDrewBefore(t *testing.T) {
	passes := map[string]func() string{
		"email autolink": func() string {
			return drawEmailLink(t)
		},
		"pending message": func() string {
			return drawPendingLink(t)
		},
		"pending message with a heading and a list": func() string {
			return drawPendingMarkdown(t)
		},
		"a path under the scratch the model sees as /tmp": func() string {
			return drawScratchPathLink(t)
		},
		"a path the user typed under their own /tmp": func() string {
			return drawUserScratchPathLink(t)
		},
		"a notice naming a path under /tmp": func() string {
			return drawNoticeScratchPathLink(t)
		},
		"a path under the scratch in an answer only appended": func() string {
			return drawAppendedScratchPathLink(t)
		},
	}
	compareWithGolden(t, "special-links", ".ansi", passes)

	screenPasses := map[string]func() string{}
	for name, pass := range passes {
		screenPasses[name] = func() string {
			return shown(t, pass(), terminalInputColumns)
		}
	}
	compareWithGolden(t, "special-links", ".screen", screenPasses)
}

func drawEmailLink(t *testing.T) string {
	t.Helper()

	address := strings.Join([]string{"person", "example.test"}, "@")
	var screenOutput strings.Builder
	paint := painter.New(
		output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines),
		false,
		nil,
		nil,
		output.StreamingModeLine,
	)
	paint.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: address})

	return strings.ReplaceAll(screenOutput.String(), address, "person-at-example.test")
}

func scratchWithPicture(t *testing.T) string {
	t.Helper()

	scratch := t.TempDir()
	if err := os.WriteFile(filepath.Join(scratch, "zoom-on.png"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	return scratch
}

func drawScratchPath(t *testing.T, event agent.Event, isAppendOnly bool) string {
	t.Helper()

	scratch := scratchWithPicture(t)

	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)
	if isAppendOnly {
		screen.AppendOnly()
	}
	screen.LinkPathsUnder(link.Roots{Scratch: scratch})

	paint := painter.New(screen, false, nil, nil, output.StreamingModeLine)
	paint.DrawEvent(event)
	screen.Seal()

	return strings.ReplaceAll(screenOutput.String(), scratch, "/state/farm/tame-impala")
}

func drawScratchPathLink(t *testing.T) string {
	t.Helper()

	return drawScratchPath(t, agent.Event{
		Kind: agent.ModelMessageEvent,
		Text: "it is at /tmp/zoom-on.png",
	}, false)
}

func drawAppendedScratchPathLink(t *testing.T) string {
	t.Helper()

	return drawScratchPath(t, agent.Event{
		Kind: agent.ModelMessageEvent,
		Text: "it is at /tmp/zoom-on.png",
	}, true)
}

func drawUserScratchPathLink(t *testing.T) string {
	t.Helper()

	return drawScratchPath(t, agent.Event{
		Kind: agent.UserMessageEvent,
		Text: "look at /tmp/zoom-on.png",
	}, false)
}

func drawNoticeScratchPathLink(t *testing.T) string {
	t.Helper()

	return drawScratchPath(t, agent.Event{
		Kind: agent.FailureEvent,
		Text: "could not read /tmp/zoom-on.png",
	}, false)
}

func drawPendingLink(t *testing.T) string {
	t.Helper()

	var screenOutput strings.Builder
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)
	grantEvent, err := pathgrant.ChangeEvent("/reference", []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}})
	if err != nil {
		t.Fatal(err)
	}
	self.pendingNotices.add(grantEvent)
	self.refreshPendingMessages()

	return screenOutput.String()
}

func drawPendingMarkdown(t *testing.T) string {
	t.Helper()

	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)
	screen.Blank()
	screen.OpenPanel(painter.NewPendingMessages(
		[]string{"# Heading\n\n- first item\n- second item"}, true, link.Roots{},
	))
	screen.Seal()

	return screenOutput.String()
}

func TestGoldenTerminalEscapeDrawsWhatItDrewBefore(t *testing.T) {
	passes := map[string]func() string{
		"bare escape keeps the following key": func() string {
			return drawBareEscapeFollowedByKey(t)
		},
		"fragmented arrow remains one key": func() string {
			return drawFragmentedArrow(t)
		},
	}

	screenPasses := map[string]func() string{}
	for name, pass := range passes {
		screenPasses[name] = func() string {
			return shown(t, pass(), terminalInputColumns)
		}
	}

	compareWithGolden(t, "terminal-escape", ".ansi", passes)
	compareWithGolden(t, "terminal-escape", ".screen", screenPasses)
}

func drawBareEscapeFollowedByKey(t *testing.T) string {
	t.Helper()

	self, editor, history, screenOutput := terminalInputFixture(t, "draft", nil)
	terminalInput := newTerminalInput(t)
	terminalInput.apply(t, self, editor, history, "\x1b")
	terminalInput.apply(t, self, editor, history, "X")

	if editor.Text() != "draftX" {
		t.Fatalf("draft after Escape and X = %q", editor.Text())
	}

	return screenOutput.String()
}

func drawFragmentedArrow(t *testing.T) string {
	t.Helper()

	self, editor, history, screenOutput := terminalInputFixture(t, "draft", []string{"earlier"})
	terminalInput := newTerminalInput(t)
	terminalInput.apply(t, self, editor, history, "\x1b[A")

	if editor.Text() != "earlier" {
		t.Fatalf("draft after fragmented Up = %q", editor.Text())
	}

	return screenOutput.String()
}

func terminalInputFixture(t *testing.T, text string, historyLines []string) (*App, *edit.Input, *edit.History, *strings.Builder) {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	for _, line := range historyLines {
		history.Add(line)
	}
	editor := edit.NewInput(history)
	editor.SetText(text)
	self.show(editor)

	return self, editor, history, &screenOutput
}

type oneByteReader struct {
	reader io.Reader
}

func (self oneByteReader) Read(buffer []byte) (int, error) {
	return self.reader.Read(buffer[:min(len(buffer), 1)])
}

type terminalInput struct {
	decoder *key.Decoder
	writer  *os.File
}

type decodedTerminalKey struct {
	keypress key.Key
	err      error
}

func newTerminalInput(t *testing.T) terminalInput {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	t.Cleanup(func() { _ = writer.Close() })

	fragmentedReader := oneByteReader{reader: reader}
	return terminalInput{
		decoder: key.NewTerminalDecoder(bufio.NewReader(fragmentedReader), reader),
		writer:  writer,
	}
}

func (self terminalInput) apply(t *testing.T, app *App, editor *edit.Input, history *edit.History, input string) {
	t.Helper()

	if _, err := self.writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	decoded := make(chan decodedTerminalKey, 1)
	go func() {
		keypress, err := self.decoder.Next()
		decoded <- decodedTerminalKey{keypress: keypress, err: err}
	}()

	select {
	case result := <-decoded:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !app.handleKeypressAndShowInput(editor, history, result.keypress) {
			t.Fatal("terminal input unexpectedly closed the conversation")
		}
	case <-time.After(time.Second):
		t.Fatal("terminal input was not decoded")
	}
}

func TestGoldenLegacyAltEnterDrawsWhatItDrewBefore(t *testing.T) {
	compareWithGolden(t, "legacy-alt-enter", ".ansi", map[string]func() string{
		"force sends an unknown command": func() string {
			return drawLegacyAltEnter(t)
		},
	})
	compareWithGolden(t, "legacy-alt-enter", ".screen", map[string]func() string{
		"force sends an unknown command": func() string {
			return shown(t, drawLegacyAltEnter(t), terminalInputColumns)
		},
	})
}

func drawLegacyAltEnter(t *testing.T) string {
	t.Helper()

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, terminalInputColumns, replayLines)
	history := edit.NewHistory("", historyLimit)
	editor := edit.NewInput(history)
	editor.SetText("/nosuch")
	self.show(editor)

	terminalInput := newTerminalInput(t)
	terminalInput.apply(t, self, editor, history, "\x1b\r")

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()
	self.show(editor)
	self.screen.End()

	if !hasUserMessage(self.recordedEvents, "/nosuch") {
		t.Fatal("legacy Alt+Enter did not send the unknown command")
	}
	if strings.Contains(screenOutput.String(), "Command not found") {
		t.Fatalf("legacy Alt+Enter rejected the command: %q", screenOutput.String())
	}

	return screenOutput.String()
}

func hasUserMessage(events []agent.Event, text string) bool {
	for _, event := range events {
		if event.Kind == agent.UserMessageEvent && event.Text == text {
			return true
		}
	}

	return false
}

func TestEscapeAtRestDoesNotPanic(t *testing.T) {
	self := &App{screen: output.New(&bytes.Buffer{})}
	inputLine := edit.NewInput(nil)

	defer func() {
		if panicValue := recover(); panicValue != nil {
			t.Fatalf("escape at rest panicked: %v", panicValue)
		}
	}()

	if !self.apply(inputLine, nil, key.Key{Code: key.Escape}) {
		t.Error("expected escape at rest to leave the conversation open")
	}
}

func TestAConfirmationAcceptsYesAndDeniesNoEnterOrEscape(t *testing.T) {
	for name, test := range map[string]struct {
		keypress key.Key
		wantErr  error
	}{
		"yes":         {keypress: key.Key{Code: key.Rune, Value: 'y'}},
		"capital yes": {keypress: key.Key{Code: key.Rune, Value: 'Y'}},
		"no":          {keypress: key.Key{Code: key.Rune, Value: 'n'}, wantErr: ask.ErrDenied},
		"enter":       {keypress: key.Key{Code: key.Enter}},
		"escape":      {keypress: key.Key{Code: key.Escape}, wantErr: ask.ErrDenied},
	} {
		t.Run(name, func(t *testing.T) {
			broker := ask.New()
			closeBroker := broker.Open()
			defer closeBroker()

			result := make(chan error, 1)
			go func() {
				result <- ask.Confirm(t.Context(), broker, ask.Confirmation{Label: "Continue?"})
			}()
			<-broker.Changes()

			self := &App{question: questionState{broker: broker}}
			self.onQuestionChange()
			self.answerQuestion(test.keypress)

			if err := <-result; !errors.Is(err, test.wantErr) {
				t.Errorf("got %v, want %v", err, test.wantErr)
			}
			if self.question.request != nil {
				t.Error("the answered question remained visible")
			}
		})
	}
}

func TestAQuestionIgnoresOtherKeys(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	result := make(chan error, 1)
	go func() { result <- ask.Confirm(ctx, broker, ask.Confirmation{Label: "Continue?"}) }()
	<-broker.Changes()

	self := &App{question: questionState{broker: broker}}
	self.onQuestionChange()
	self.answerQuestion(key.Key{Code: key.Rune, Value: 'x'})

	select {
	case err := <-result:
		t.Fatalf("an unrelated key answered the question with %v", err)
	default:
	}
	if self.question.request == nil {
		t.Error("an unrelated key cleared the question")
	}
}

func TestFocusChangesAreTrackedWhileAQuestionStands(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	result := make(chan error, 1)
	go func() { result <- ask.Confirm(t.Context(), broker, ask.Confirmation{Label: "Continue?"}) }()
	<-broker.Changes()

	trackedTerminal := terminal.New(io.Discard, work.At("/workspace"))
	restoreTerminal := trackedTerminal.Begin(caps.Read)
	defer restoreTerminal()

	self := &App{question: questionState{broker: broker}, terminal: trackedTerminal}
	self.onQuestionChange()
	self.handleKeypressAndShowInput(nil, nil, key.Key{Code: key.FocusOut})
	if self.terminal.IsFocused() {
		t.Error("focus out did not reach the terminal while a question stood")
	}
	self.handleKeypressAndShowInput(nil, nil, key.Key{Code: key.FocusIn})
	if !self.terminal.IsFocused() {
		t.Error("focus in did not reach the terminal while a question stood")
	}

	self.answerQuestion(key.Key{Code: key.Rune, Value: 'y'})
	if err := <-result; err != nil {
		t.Fatalf("the question was answered with %v", err)
	}
}

func TestEveryQuestionSendsOneDesktopNotification(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	var notified []string
	self := &App{question: questionState{broker: broker}}
	self.onQuestion = func(question ask.Question) { notified = append(notified, question.Label) }

	first := make(chan error, 1)
	go func() { first <- ask.Confirm(t.Context(), broker, ask.Confirmation{Label: "Continue?"}) }()
	<-broker.Changes()
	self.onQuestionChange()

	second := make(chan error, 1)
	go func() { second <- ask.Confirm(t.Context(), broker, ask.Confirmation{Label: "Fetch this page?"}) }()
	<-broker.Changes()
	self.onQuestionChange()

	self.answerQuestion(key.Key{Code: key.Rune, Value: 'y'})
	if err := <-first; err != nil {
		t.Fatalf("the first question was answered with %v", err)
	}

	<-broker.Changes()
	self.onQuestionChange()
	self.answerQuestion(key.Key{Code: key.Rune, Value: 'y'})
	if err := <-second; err != nil {
		t.Fatalf("the second question was answered with %v", err)
	}

	want := []string{"Continue?", "Fetch this page?"}
	if !slices.Equal(notified, want) {
		t.Errorf("got notifications %q, want %q", notified, want)
	}
}

func TestACallIsNotTimedWhileItsQuestionStands(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rig := newWideRig(t)
		rig.chat.currentTurn.Stream = testRunningTurnStream()
		rig.chat.currentTurn.painter = rig.chat.newPainter(true)
		rig.chat.currentTurn.painter.DrawEvent(agent.Event{
			Kind: agent.ToolCallRequestEvent,
			ID:   "call-1",
			Name: "bash",
			Text: `{"command":"curl example.com"}`,
		})

		broker := ask.New()
		t.Cleanup(broker.Open())

		result := make(chan error, 1)
		go func() {
			result <- ask.Confirm(t.Context(), broker, ask.Confirmation{
				Label:  "Run this command with host networking?",
				Detail: "curl example.com",
			})
		}()
		<-broker.Changes()

		rig.chat.question.broker = broker
		rig.chat.onQuestionChange()

		time.Sleep(time.Minute)
		synctest.Wait()
		whileStanding := style.Plain(rig.drawn())

		rig.chat.answerQuestion(key.Key{Code: key.Rune, Value: 'y'})
		if err := <-result; err != nil {
			t.Fatalf("the question was answered with %v", err)
		}

		time.Sleep(8 * time.Second)
		synctest.Wait()
		afterAnswering := style.Plain(rig.drawn())

		rig.chat.currentTurn.painter.Close(dynamic.Cancelled)

		if strings.Contains(whileStanding, "1m") {
			t.Errorf("the call was timed while its question stood: %q", whileStanding)
		}
		if !strings.Contains(afterAnswering, "8s") {
			t.Errorf("expected the call to be timed once it ran, got %q", afterAnswering)
		}
		if strings.Contains(afterAnswering, "1m") {
			t.Errorf("the answered question was counted against the call: %q", afterAnswering)
		}
	})
}

func TestAQuestionMovesItsCursorAndAnswersWhereItRests(t *testing.T) {
	for name, test := range map[string]struct {
		keypresses []key.Key
		wantErr    error
	}{
		"left from the approving option": {
			keypresses: []key.Key{{Code: key.Left}, {Code: key.Enter}},
			wantErr:    ask.ErrDenied,
		},
		"tab from the approving option": {
			keypresses: []key.Key{{Code: key.Rune, Value: '\t'}, {Code: key.Enter}},
			wantErr:    ask.ErrDenied,
		},
		"right and back again": {
			keypresses: []key.Key{{Code: key.Right}, {Code: key.Up}, {Code: key.Enter}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			broker := ask.New()
			closeBroker := broker.Open()
			defer closeBroker()

			result := make(chan error, 1)
			go func() {
				result <- ask.Confirm(t.Context(), broker, ask.Confirmation{Label: "Continue?"})
			}()
			<-broker.Changes()

			self := &App{question: questionState{broker: broker}}
			self.onQuestionChange()
			for _, keypress := range test.keypresses {
				self.answerQuestion(keypress)
			}

			if err := <-result; !errors.Is(err, test.wantErr) {
				t.Errorf("got %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestARefusedHostNetworkSaysSoInWordsTheModelCanAct(t *testing.T) {
	for name, test := range map[string]struct {
		prepare func(t *testing.T, broker *ask.Broker) context.Context
		want    string
	}{
		"denied": {
			prepare: func(t *testing.T, broker *ask.Broker) context.Context {
				t.Helper()
				t.Cleanup(broker.Open())
				go func() {
					<-broker.Changes()
					broker.Current().Cancel()
				}()

				return t.Context()
			},
			want: "refused",
		},
		"unanswered": {
			prepare: func(t *testing.T, broker *ask.Broker) context.Context {
				t.Helper()
				t.Cleanup(broker.Open())
				ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)

				return ctx
			},
			want: "approval timed out after 1m",
		},
		"nobody to ask": {
			prepare: func(t *testing.T, _ *ask.Broker) context.Context {
				t.Helper()

				return t.Context()
			},
			want: "approval unavailable",
		},
	} {
		t.Run(name, func(t *testing.T) {
			broker := ask.New()
			ctx := test.prepare(t, broker)

			err := approveHostNetwork(ctx, broker, permission.Ask, "curl example.com")
			if err == nil {
				t.Fatal("a command nobody allowed was approved")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("got %q, want it to mention %q", err, test.want)
			}
			if strings.Contains(err.Error(), "context") {
				t.Errorf("got %q, want words rather than plumbing", err)
			}
		})
	}
}

func TestAnApprovedHostNetworkRunsTheCommand(t *testing.T) {
	broker := ask.New()
	t.Cleanup(broker.Open())

	go func() {
		<-broker.Changes()
		broker.Current().Choose(0)
	}()

	if err := approveHostNetwork(t.Context(), broker, permission.Ask, "curl example.com"); err != nil {
		t.Errorf("got %v, want the approved command to run", err)
	}
}

func TestAnApprovalChargesTheTimeItStoodToTheCallThatAskedForIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const deliberated = 30 * time.Second

		broker := ask.New()
		t.Cleanup(broker.Open())

		ctx, waitedTime := waiting.Track(t.Context())

		go func() {
			<-broker.Changes()
			time.Sleep(deliberated)
			broker.Current().Choose(0)
		}()

		if err := approveHostNetwork(ctx, broker, permission.Ask, "curl example.com"); err != nil {
			t.Fatalf("got %v, want the approved command to run", err)
		}

		if got := waitedTime(); got != deliberated {
			t.Errorf("got %s charged to the call, want the %s it stood", got, deliberated)
		}
	})
}

func TestAQuestionCountsDownToItsDeadline(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	deadline := time.Now().Add(time.Hour)
	askedAt := deadline.Add(-time.Minute)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()

	go func() { _ = ask.Confirm(ctx, broker, ask.Confirmation{Label: "Continue?"}) }()
	<-broker.Changes()

	self := &App{question: questionState{broker: broker}, now: func() time.Time { return askedAt }}
	self.onQuestionChange()

	if len(self.questionRows(80)) == 0 {
		t.Fatal("a question awaiting an answer drew nothing")
	}
	head := painter.QuestionHead(self.question.request.Question, self.remainingAnswerTime(askedAt))
	if got := style.Plain(head); !strings.Contains(got, "auto-denies in 1m") {
		t.Errorf("got head %q, want a countdown", got)
	}

	if got := self.nextAnswerRefresh(askedAt); !got.Equal(askedAt.Truncate(time.Second).Add(time.Second)) {
		t.Errorf("got next refresh %v, want the next second", got)
	}
	if got := self.nextAnswerRefresh(deadline.Add(time.Second)); !got.IsZero() {
		t.Errorf("got next refresh %v after the deadline, want none", got)
	}
}

func TestControlDStopsATurnBeforeItIsAWayOut(t *testing.T) {
	self := &App{screen: output.New(&bytes.Buffer{})}
	inputLine := edit.NewInput(nil)

	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	wasStopped := false

	self.currentTurn = Turn{Stream: testTurnStream(nil, func(error) { wasStopped = true }, turn.State{Running: true})}

	if !self.apply(inputLine, nil, keypress) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !wasStopped || !self.currentTurn.Cancelled() {
		t.Error("expected the turn to have been cancelled, as escape cancels it")
	}

	self.currentTurn = Turn{}

	if self.apply(inputLine, nil, keypress) {
		t.Error("expected ctrl+d at rest to be the way out")
	}

	inputLine.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	if !self.apply(inputLine, nil, keypress) {
		t.Error("expected ctrl+d on a line with something on it to leave the harness running")
	}
}

var dismissKeys = map[string]key.Key{
	"backspace": {Code: key.Backspace},
	"escape":    {Code: key.Escape},
	"ctrl+d":    {Code: key.Rune, Value: 'd', Mod: key.Ctrl},
}

var feedbackDismissKeys = map[feedbackScenario]key.Key{
	feedbackClearedByBackspace: dismissKeys["backspace"],
	feedbackClearedByEscape:    dismissKeys["escape"],
	feedbackClearedByControlD:  dismissKeys["ctrl+d"],
}

func TestADismissKeyTakesDismissableFeedbackAwayAndDoesNothingElse(t *testing.T) {
	for name, keypress := range dismissKeys {
		t.Run(name, func(t *testing.T) {
			cancellations := 0
			self := &App{
				screen: output.New(&bytes.Buffer{}),
				currentTurn: Turn{
					Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
				},
			}
			self.showFeedback(feedback.Command, feedback.Message{
				Text:   "Command not found: /unknown",
				Status: agent.ErrorStatus,
			})

			if !self.apply(edit.NewInput(nil), nil, keypress) {
				t.Fatal("the harness stopped instead of taking the feedback away")
			}
			if !self.feedback.IsEmpty() {
				t.Errorf("the feedback stayed: %+v", self.feedback.Message())
			}
			if cancellations != 0 {
				t.Errorf("the turn was cancelled %d times, want the key spent on the feedback", cancellations)
			}
		})
	}
}

func TestADismissKeyLeavesFeedbackNothingMayDismissAlone(t *testing.T) {
	for name, keypress := range dismissKeys {
		t.Run(name, func(t *testing.T) {
			cancellations := 0
			self := &App{
				screen: output.New(&bytes.Buffer{}),
				currentTurn: Turn{
					Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
				},
			}
			self.showFeedback(feedback.System, feedback.Message{
				Text:   "chat.md recording disabled",
				Status: agent.ErrorStatus,
			})

			self.apply(edit.NewInput(nil), nil, keypress)

			if self.feedback.IsEmpty() {
				t.Error("feedback nothing may dismiss was taken away")
			}

			wantCancellations := 1
			if keypress.Code == key.Backspace {
				wantCancellations = 0
			}
			if cancellations != wantCancellations {
				t.Errorf("the turn was cancelled %d times, want %d", cancellations, wantCancellations)
			}
		})
	}
}

func TestControlDIsTheWayOutAgainOnceTheFeedbackHasGone(t *testing.T) {
	self := &App{screen: output.New(&bytes.Buffer{})}
	inputLine := edit.NewInput(nil)
	keypress := key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}

	self.showFeedback(feedback.Command, feedback.Message{Text: "Copied", Status: agent.SuccessStatus})

	if !self.apply(inputLine, nil, keypress) {
		t.Fatal("ctrl+d left the harness while feedback was on screen")
	}

	if self.apply(inputLine, nil, keypress) {
		t.Error("expected the next ctrl+d at rest to be the way out")
	}
}

func TestTwoReturnsOnAnEmptyIdleLineSendTheContinueMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.continueMessage = "carry on"
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	if self.currentTurn.Running() {
		t.Error("expected the first return to do nothing")
	}

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	if !self.currentTurn.Running() {
		t.Fatal("expected the second return to start a turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	for _, record := range self.recordedEvents {
		if record.Kind == agent.UserMessageEvent && record.Text == "carry on" {
			return
		}
	}

	t.Error("expected the configured prompt")
}

func TestTwoReturnsWithAPendingNoticeSubmitItRatherThanTheContinueMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.continueMessage = "carry on"
	self.settleAccess()
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.apply(inputLine, history, key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl})
	self.apply(inputLine, history, key.Key{Code: key.Rune, Value: 'w'})

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})

	if !self.currentTurn.Running() {
		t.Fatal("expected the second return to start a turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	messages := submittedTexts(self.recordedEvents)
	wantMessages := []string{workspaceNowReadOnly()}
	if !slices.Equal(messages, wantMessages) {
		t.Errorf("got messages %q, want %q", messages, wantMessages)
	}
}

func TestTwoReturnsWithARestoredJobNoticeSubmitItRatherThanTheContinueMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.continueMessage = "carry on"
	self.settleAccess()
	self.jobs = jobState{manager: jobs.New(nil)}
	self.restoreJobs([]agent.Event{
		jobrecord.ListingEvent([]jobs.Snapshot{{Name: "docs", Command: "python3", State: jobs.StateRunning}}),
	})

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})

	if !self.currentTurn.Running() {
		t.Fatal("expected the second return to start a turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	notice, _ := jobrecord.EndedWithSessionNotice(jobrecord.EndedWithSessionEvent([]string{"docs"}))
	messages := submittedTexts(self.recordedEvents)
	if !slices.Equal(messages, []string{notice}) {
		t.Errorf("got messages %q, want %q", messages, []string{notice})
	}
}

func TestTwoReturnsWithAnEndedJobNoticeSubmitItRatherThanTheContinueMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.continueMessage = "carry on"
	self.settleAccess()
	self.jobs = jobState{manager: jobs.New(nil)}
	self.jobEnded(jobs.Conclusion{
		Snapshot: jobs.Snapshot{Name: "build", Command: "just build", State: jobs.StateFailed},
	})

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})

	if !self.currentTurn.Running() {
		t.Fatal("expected the second return to start a turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if messages := submittedTexts(self.recordedEvents); len(messages) != 1 ||
		!strings.Contains(messages[0], "build") ||
		strings.Contains(messages[0], "carry on") {
		t.Errorf("got messages %q, want the ended job notice alone", messages)
	}
}

func TestAcceptedInputCanImmediatelyBeRecalled(t *testing.T) {
	self := &App{currentTurn: Turn{Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true})}}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	for _, value := range "latest" {
		self.apply(inputLine, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Up})

	if inputLine.Text() != "latest" {
		t.Errorf("expected the latest input to be recalled, got %q", inputLine.Text())
	}
}

func submittedTexts(events []agent.Event) []string {
	var texts []string
	for _, event := range events {
		if event.Kind == agent.UserMessageEvent {
			texts = append(texts, event.Text)
			continue
		}
		if notices, areSaid := painter.HarnessNotices(event); areSaid {
			texts = append(texts, notices...)
		}
	}

	return texts
}

func TestChangingCapabilitiesRestartsTheTurnWithTheChangeAsItsPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.start("first")
	interruptedEvents := self.currentTurn.Events()

	self.apply(inputLine, history, key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl})
	self.apply(inputLine, history, key.Key{Code: key.Rune, Value: 'w'})

	if !self.currentTurn.Cancelled() {
		t.Fatal("expected the capability change to interrupt the turn")
	}

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if !self.currentTurn.Running() {
		t.Fatal("expected the capability change to start a replacement turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	messages := submittedTexts(self.recordedEvents)

	wantMessages := []string{"first", workspaceNowReadOnly()}
	if !slices.Equal(messages, wantMessages) {
		t.Errorf("got messages %q, want %q", messages, wantMessages)
	}
}

func workspaceNowReadOnly() string {
	withdrawn := caps.NewMode(caps.Read | caps.Write)
	withdrawn.Toggle(caps.Write)

	return withdrawn.Inject()
}

func TestTwoReturnsOnAnEmptyLineReplaceTheRunningTurnWithTheContinueMessage(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.start("first")
	interruptedEvents := self.currentTurn.Events()

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	if self.currentTurn.Cancelled() {
		t.Error("expected the first return to leave the turn running")
	}

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	if !self.currentTurn.Cancelled() {
		t.Error("expected the second return to cancel the turn")
	}

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()

	if !self.currentTurn.Running() {
		t.Fatal("expected a continuation to start after the interrupted turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	var messages []string
	var wasInterrupted bool
	for _, record := range self.recordedEvents {
		if record.Kind == agent.UserMessageEvent {
			messages = append(messages, record.Text)
		}
		wasInterrupted = wasInterrupted || record.Kind == agent.InterruptionEvent
	}

	wantMessages := []string{"first", builtInConfig(t).Input.Continue}
	if !slices.Equal(messages, wantMessages) {
		t.Errorf("got messages %q, want %q", messages, wantMessages)
	}
	if !wasInterrupted {
		t.Error("expected the replacement to record an interruption")
	}
}

type reasoningThenAnswerProvider struct {
	quietProvider

	sends int
}

func (self *reasoningThenAnswerProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	self.sends++
	if self.sends == 1 {
		if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: "provisional thought"}) {
			return agent.Reply{}, nil
		}
		<-ctx.Done()
		return agent.Reply{}, ctx.Err()
	}

	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "replacement answer"}) ||
		!yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
		return agent.Reply{}, nil
	}
	return agent.Reply{}, nil
}

type eventsAfterCancellationProvider struct {
	quietProvider
}

func (eventsAfterCancellationProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	for _, thought := range []string{"first", "second"} {
		if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: thought}) ||
			!yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true}) {
			return agent.Reply{}, nil
		}
	}

	<-ctx.Done()

	for _, message := range []string{"first", "second"} {
		if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: message}) ||
			!yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
			return agent.Reply{}, nil
		}
	}

	return agent.Reply{}, ctx.Err()
}

func TestCompletedEventsAreRenderedAfterCancellation(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = log.Close() }()

	self := &App{
		agent:    agent.New("", eventsAfterCancellationProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	events := self.currentTurn.Events()

	thoughts := 0
	for thoughts < 2 {
		report := <-events
		self.takeTurn(report)
		if report.Update.Event != nil && report.Update.Event.Kind == agent.ModelReasoningEvent {
			thoughts++
		}
	}

	self.interruptTurn(interrupt.Escape)

	for report := range events {
		self.takeTurn(report)
	}
	self.finish()

	messages := 0
	for _, record := range self.recordedEvents {
		if record.Kind == agent.ModelMessageEvent {
			messages++
		}
	}

	if messages != 2 {
		t.Errorf("expected both completed messages to be rendered, got %d", messages)
	}
}

func TestAnAcceptedMessageDisappearsIntoTheQueueWithoutStoppingTheTurn(t *testing.T) {
	self := &App{
		currentTurn: Turn{Stream: testTurnStream(make(chan TurnEvent), func(error) {}, turn.State{Running: true})},
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	typeMessage(t, self, inputLine, history, "dfd")

	if inputLine.Text() != "" {
		t.Fatalf("expected accepted input to disappear, got %q", inputLine.Text())
	}
	if !self.currentTurn.Running() {
		t.Fatal("expected the turn to keep running")
	}
	if self.currentTurn.Cancelled() {
		t.Fatal("expected a queued message to leave the turn alone")
	}
	if queued := self.currentTurn.GetInterjections(); !slices.Equal(queued, []string{"dfd"}) {
		t.Fatalf("expected dfd to be queued for the running turn, got %q", queued)
	}
	if pending := self.queuedTurn.Peek(); pending.Replacement {
		t.Fatal("expected nothing to be queued for a next turn")
	}
}

func TestTwoRapidMessagesBothQueueInTheOrderTheyWereTyped(t *testing.T) {
	cancellations := 0
	self := &App{
		currentTurn: Turn{
			Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
		},
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	for _, message := range []string{"first message", "second message"} {
		typeMessage(t, self, inputLine, history, message)
	}

	want := []string{"first message", "second message"}
	if queued := self.currentTurn.GetInterjections(); !slices.Equal(queued, want) {
		t.Errorf("queued %q, want %q", queued, want)
	}
	if cancellations != 0 {
		t.Errorf("cancelled %d times, want none", cancellations)
	}
}

func TestADoubleEnterSendsTheQueueAtOnceAndStopsTheTurn(t *testing.T) {
	cancellations := 0
	self := &App{
		currentTurn: Turn{
			Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
		},
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	for _, message := range []string{"first message", "second message"} {
		typeMessage(t, self, inputLine, history, message)
	}

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})

	if cancellations != 1 {
		t.Errorf("cancelled %d times, want 1", cancellations)
	}
	if !self.queuedTurn.Empty() {
		t.Errorf("queued a next turn before this one stopped: %+v", self.queuedTurn.Peek())
	}

	want := []string{"first message", "second message"}
	if queued := self.currentTurn.GetInterjections(); !slices.Equal(queued, want) {
		t.Errorf("queued %q, want %q standing until the turn stops", queued, want)
	}
}

func TestAFlushedQueueStandsOnScreenUntilTheTurnItStoppedGivesWay(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &App{
		screen: output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		currentTurn: Turn{
			Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true}),
		},
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	typeMessage(t, self, inputLine, history, "check the other path too")
	if drawn := style.Plain(strings.Join(self.statusRows(replayColumns), "\n")); !strings.Contains(
		drawn, "double-enter to send now",
	) {
		t.Errorf("drew %q, want the queue offering to send itself now", drawn)
	}

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})

	drawn := style.Plain(strings.Join(self.statusRows(replayColumns), "\n"))
	if !strings.Contains(drawn, "check the other path too") {
		t.Errorf("drew %q, want the flushed message still standing", drawn)
	}
	if !strings.Contains(drawn, "sending…") {
		t.Errorf("drew %q, want the queue saying what it is waiting for", drawn)
	}
}

func TestAMessageTypedWhileTheStoppingTurnLingersJoinsTheFlushedOne(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.start("first")
	stoppedEvents := self.currentTurn.Events()

	typeMessage(t, self, inputLine, history, "look at this instead")
	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})

	typeMessage(t, self, inputLine, history, "and this as well")

	for report := range stoppedEvents {
		self.takeTurn(report)
	}
	self.finish()
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	var messages []string
	for _, event := range self.recordedEvents {
		if event.Kind == agent.UserMessageEvent {
			messages = append(messages, event.Text)
		}
	}

	want := []string{"first", "look at this instead\n\nand this as well"}
	if !slices.Equal(messages, want) {
		t.Errorf("sent %q, want %q", messages, want)
	}
}

func TestAStopKeyTakesAFlushedMessageBackWhileTheTurnIsStillStopping(t *testing.T) {
	self := &App{
		currentTurn: Turn{
			Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true}),
		},
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	typeMessage(t, self, inputLine, history, "look at this instead")
	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Escape})

	if inputLine.Text() != "look at this instead" {
		t.Errorf("got input %q, want the flushed message handed back", inputLine.Text())
	}
	if queued := self.currentTurn.GetInterjections(); len(queued) != 0 {
		t.Errorf("left %q queued, want nothing", queued)
	}
}

func TestADoubleEnterWithNothingQueuedStillSendsTheContinueMessage(t *testing.T) {
	cancellations := 0
	self := &App{
		continueMessage: "carry on",
		currentTurn: Turn{
			Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
		},
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})

	pending := self.queuedTurn.Peek()
	if !pending.Replacement || pending.Message != "carry on" {
		t.Errorf("unexpected queued turn: %+v", pending)
	}
	if cancellations != 1 {
		t.Errorf("cancelled %d times, want 1", cancellations)
	}
}

func TestAStopKeyTakesTheLastQueuedMessageBackIntoTheInputLine(t *testing.T) {
	cancellations := 0
	self := &App{
		currentTurn: Turn{
			Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
		},
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	for _, message := range []string{"first message", "second message"} {
		typeMessage(t, self, inputLine, history, message)
	}

	self.apply(inputLine, history, key.Key{Code: key.Escape})

	if inputLine.Text() != "second message" {
		t.Errorf("took back %q, want the second message", inputLine.Text())
	}
	if queued := self.currentTurn.GetInterjections(); !slices.Equal(queued, []string{"first message"}) {
		t.Errorf("left %q queued, want only the first message", queued)
	}
	if cancellations != 0 {
		t.Errorf("cancelled %d times, want none", cancellations)
	}
}

func TestPeelingWhileTypingKeepsWhatIsBeingTypedAtTheEnd(t *testing.T) {
	cancellations := 0
	self := &App{
		currentTurn: Turn{
			Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
		},
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	typeMessage(t, self, inputLine, history, "queued")
	for _, value := range "half typed" {
		self.apply(inputLine, history, key.Key{Code: key.Rune, Value: value})
	}

	self.apply(inputLine, history, key.Key{Code: key.Escape})

	if inputLine.Text() != "queued\n\nhalf typed" {
		t.Errorf("input reads %q, want the taken-back message before what was being typed", inputLine.Text())
	}
	if cancellations != 0 {
		t.Errorf("cancelled %d times, want none", cancellations)
	}
}

func TestPeelingEveryQueuedMessageRebuildsWhatWouldHaveBeenSent(t *testing.T) {
	messages := []string{"first", "second", "third"}

	queueMessages := func(self *App) (*edit.Input, *edit.History) {
		self.currentTurn = Turn{Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true})}
		history := edit.NewHistory("", historyLimit)
		inputLine := edit.NewInput(history)
		for _, message := range messages {
			typeMessage(t, self, inputLine, history, message)
		}
		return inputLine, history
	}

	sent := &App{}
	queueMessages(sent)
	wouldHaveBeenSent, isQueued := sent.currentTurn.TakeInterjections()
	if !isQueued {
		t.Fatal("expected the queue to have something to deliver")
	}

	peeled := &App{}
	inputLine, history := queueMessages(peeled)
	for range messages {
		peeled.apply(inputLine, history, key.Key{Code: key.Escape})
	}

	if inputLine.Text() != wouldHaveBeenSent {
		t.Errorf("peeled back %q, want what delivery would have sent, %q", inputLine.Text(), wouldHaveBeenSent)
	}
	if queued := peeled.currentTurn.GetInterjections(); len(queued) != 0 {
		t.Errorf("left %q queued, want nothing", queued)
	}
}

func TestAStopKeyStopsTheTurnOnceTheQueueIsEmpty(t *testing.T) {
	for name, stopKey := range map[string]key.Key{
		"escape": {Code: key.Escape},
		"ctrl+d": {Code: key.Rune, Value: 'd', Mod: key.Ctrl},
	} {
		t.Run(name, func(t *testing.T) {
			cancellations := 0
			self := &App{
				currentTurn: Turn{
					Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
				},
			}
			history := edit.NewHistory("", historyLimit)
			inputLine := edit.NewInput(history)

			typeMessage(t, self, inputLine, history, "queued")

			self.apply(inputLine, history, stopKey)
			if cancellations != 0 {
				t.Fatalf("the first press cancelled %d times, want none", cancellations)
			}
			if inputLine.Text() != "queued" {
				t.Fatalf("the first press left %q in the input, want the message back", inputLine.Text())
			}

			self.apply(inputLine, history, stopKey)
			if cancellations != 1 {
				t.Errorf("the second press cancelled %d times, want 1", cancellations)
			}
		})
	}
}

func TestControlDTakesAQueuedMessageBackJustAsEscapeDoes(t *testing.T) {
	cancellations := 0
	self := &App{
		currentTurn: Turn{
			Stream: testTurnStream(nil, func(error) { cancellations++ }, turn.State{Running: true}),
		},
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	typeMessage(t, self, inputLine, history, "queued")

	self.apply(inputLine, history, key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl})

	if inputLine.Text() != "queued" {
		t.Errorf("input reads %q, want the message back", inputLine.Text())
	}
	if cancellations != 0 {
		t.Errorf("cancelled %d times, want none", cancellations)
	}
	if queued := self.currentTurn.GetInterjections(); len(queued) != 0 {
		t.Errorf("left %q queued, want nothing", queued)
	}
}

func typeMessage(t *testing.T, self *App, inputLine *edit.Input, history *edit.History, message string) {
	t.Helper()

	for _, value := range message {
		self.apply(inputLine, history, key.Key{Code: key.Rune, Value: value})
	}
	self.apply(inputLine, history, key.Key{Code: key.Enter})
}

func TestFlushingTheQueueCancelsProvisionalReasoningAndStartsTheNextTurn(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &reasoningThenAnswerProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.start("first")
	interruptedEvents := self.currentTurn.Events()
	for {
		report := <-interruptedEvents
		self.takeTurn(report)
		if report.Update.Delta != nil && report.Update.Delta.Kind == agent.ModelReasoningEvent {
			break
		}
	}
	typeMessage(t, self, inputLine, history, "replacement")
	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})

	for report := range interruptedEvents {
		self.takeTurn(report)
	}
	self.finish()
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	var messages []string
	var reasoningEvents int
	for _, event := range self.recordedEvents {
		if event.Kind == agent.UserMessageEvent || event.Kind == agent.ModelMessageEvent {
			messages = append(messages, event.Text)
		}
		if event.Kind == agent.ModelReasoningEvent {
			reasoningEvents++
		}
	}
	if !slices.Equal(messages, []string{"first", "replacement", "replacement answer"}) {
		t.Errorf("got messages %q", messages)
	}
	if reasoningEvents != 0 {
		t.Errorf("stored %d provisional reasoning events", reasoningEvents)
	}
}

func TestAQueuedMessageOpensTheNextTurnWithoutInterruptingThisOne(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&screenOutput),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.start("first")
	firstTurnEvents := self.currentTurn.Events()

	typeMessage(t, self, inputLine, history, "follow up")

	if self.currentTurn.Cancelled() {
		t.Fatal("expected the queued message to leave the turn alone")
	}

	for report := range firstTurnEvents {
		self.takeTurn(report)
	}
	self.finish()

	if !self.currentTurn.Running() {
		t.Fatal("expected the queued message to start another turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if strings.Contains(style.Plain(screenOutput.String()), "interrupted") {
		t.Errorf("expected the replaced turn to end silently, got %q", style.Plain(screenOutput.String()))
	}

	if err := log.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wasSent, wasInterruptionStored bool
	for _, event := range storedSession.Events {
		wasSent = wasSent || event.Kind == agent.UserMessageEvent && event.Text == "follow up"
		wasInterruptionStored = wasInterruptionStored || event.Kind == agent.InterruptionEvent
	}

	if !wasSent {
		t.Error("expected the queued message to be sent once the turn finished")
	}
	if wasInterruptionStored {
		t.Error("expected no interruption to be stored, because nothing was interrupted")
	}
}

func TestTheStopKeyNamesItselfAsTheInterruptionReason(t *testing.T) {
	for name, test := range map[string]struct {
		keypress key.Key
		want     interrupt.Cause
	}{
		"escape": {keypress: key.Key{Code: key.Escape}, want: interrupt.Escape},
		"ctrl+d": {keypress: key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}, want: interrupt.ControlD},
	} {
		t.Run(name, func(t *testing.T) {
			if got := stopKeyCause(test.keypress); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestAStoppedTurnIsStoredAsAnInterruption(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.interruptTurn(interrupt.Escape)

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if err := log.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, event := range storedSession.Events {
		if event.Kind == agent.InterruptionEvent {
			if got := interrupt.Reason(event); got != interrupt.Sentence(interrupt.Escape) {
				t.Errorf("got reason %q, want the one the turn was stopped with", got)
			}
			return
		}
	}

	t.Error("expected the interruption to be stored in the session log")
}

func TestEscapeTakesBackAQueuedMessageWithoutAnnouncingAnInterruption(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.start("first")
	turnEvents := self.currentTurn.Events()

	typeMessage(t, self, inputLine, history, "follow up")
	self.apply(inputLine, history, key.Key{Code: key.Escape})

	for report := range turnEvents {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Error("expected the finished turn to leave no turn running")
	}

	if inputLine.Text() != "follow up" {
		t.Errorf("input reads %q, want the taken-back message", inputLine.Text())
	}

	for _, record := range self.recordedEvents {
		if record.Kind == agent.UserMessageEvent && record.Text == "follow up" {
			t.Error("expected the taken-back message not to be sent")
		}
	}

	if strings.Contains(style.Plain(screenOutput.String()), "interrupted") {
		t.Errorf("expected no interruption in the scrollback, got %q", style.Plain(screenOutput.String()))
	}
}

func TestControlDStopsATurnWhateverHasBeenTyped(t *testing.T) {
	self := &App{screen: output.New(&bytes.Buffer{})}
	inputLine := edit.NewInput(nil)

	inputLine.Apply(key.Key{Code: key.Rune, Value: 'a'}, false)

	wasStopped := false

	self.currentTurn = Turn{Stream: testTurnStream(nil, func(error) { wasStopped = true }, turn.State{Running: true})}

	if !self.apply(inputLine, nil, key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}) {
		t.Error("expected ctrl+d during a turn to stop the turn rather than the harness")
	}

	if !wasStopped || !self.currentTurn.Cancelled() {
		t.Error("expected the turn to have been cancelled")
	}

	if inputLine.Text() != "a" {
		t.Errorf("expected what was typed to be left alone, got %q", inputLine.Text())
	}
}

func TestCancellingTwiceDropsTheQueueAndCancellingOnceKeepsIt(t *testing.T) {
	tests := map[string]struct {
		isCancelled bool
		wantKept    bool
	}{
		"a turn already cancelled": {isCancelled: true, wantKept: false},
		"a turn still running":     {isCancelled: false, wantKept: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			self := &App{
				currentTurn: Turn{Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true, IsCancelled: test.isCancelled})},
			}
			self.queuedTurn.Replace("later")
			self.queuedTurn.MarkAccessChange()

			self.cancelTurn(interrupt.Escape)

			pending := self.queuedTurn.Peek()
			wasKept := pending.Replacement && pending.AccessChange && pending.Message == "later"
			if wasKept != test.wantKept {
				t.Errorf(
					"expected the queue kept=%t, got queued=%t mode=%t prompt=%q",
					test.wantKept, pending.Replacement, pending.AccessChange, pending.Message,
				)
			}

			if pending.AccessNotice == test.wantKept {
				t.Errorf(
					"expected the dropped mode change to be left as a notice=%t, got %t",
					!test.wantKept, pending.AccessNotice,
				)
			}
		})
	}
}

func TestAQueuedPromptStartsAndTakesTheQueuedModeChangeWithIt(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.replaceTurn("second")

	pending := self.queuedTurn.Peek()
	if !pending.Replacement || !pending.AccessChange || pending.Message != "second" {
		t.Fatalf(
			"expected both a queued prompt and a queued mode change, got queued=%t mode=%t prompt=%q",
			pending.Replacement, pending.AccessChange, pending.Message,
		)
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if !self.queuedTurn.Empty() {
		t.Errorf("expected the whole queue emptied, got %+v", self.queuedTurn.Peek())
	}

	if !self.currentTurn.Running() {
		t.Error("expected the queued prompt to have started a turn")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}

	if !slices.ContainsFunc(provider.messages, isReadOnlyNote) {
		t.Errorf("expected the queued prompt to carry the mode change, got %q", provider.messages)
	}

	self.finish()

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages := submittedTexts(storedSession.Events)
	if !slices.Equal(messages, []string{"first", nowReadOnlyNote, "second"}) {
		t.Errorf("expected the notice to be stored before the queued prompt, got %q", messages)
	}
}

func TestAModeChangeDroppedByEscapeIsStillDrawnAndCarried(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var screenOutput bytes.Buffer
	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&screenOutput),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.cancelTurn(interrupt.Escape)

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Error("expected the dropped mode change not to start a turn of its own")
	}

	provider.messages = nil
	self.start("second")

	if drawn := style.Plain(screenOutput.String()); !strings.Contains(drawn, nowReadOnlyNote) {
		t.Errorf("expected the mode change to have been drawn, got %q", drawn)
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if !slices.ContainsFunc(provider.messages, isReadOnlyNote) {
		t.Errorf("expected the next turn to carry the mode change, got %q", provider.messages)
	}

	if err := log.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages := submittedTexts(storedSession.Events)
	if !slices.Equal(messages, []string{"first", nowReadOnlyNote, "second"}) {
		t.Errorf("expected the notice to be stored before the next prompt, got %q", messages)
	}
}

func TestATransitionLeavesAQueuedModeChangeUnsent(t *testing.T) {
	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)

	if err := self.requestTransition(cycle.Transition{Kind: cycle.NewSession}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Error("expected the session being closed to start no further turn")
	}

	if !slices.Equal(provider.messages, []string{"first"}) {
		t.Errorf("expected nothing sent while the session was closing, got %q", provider.messages)
	}
}

type refusingOnceProvider struct {
	failure  error
	failures int
	sent     int
	messages []string
}

func (self *refusingOnceProvider) Configure(string, []tool.Definition)   {}
func (self *refusingOnceProvider) AddToolResults([]agent.ToolCallResult) {}
func (self *refusingOnceProvider) Dump() []json.RawMessage               { return nil }
func (self *refusingOnceProvider) Load([]json.RawMessage)                {}

func (self *refusingOnceProvider) AddUserMessage(text string) {
	self.messages = append(self.messages, text)
}

func (self *refusingOnceProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	self.sent++

	if self.sent <= self.failures {
		if self.failure != nil {
			return agent.Reply{}, self.failure
		}
		return agent.Reply{}, errors.New("your prompt was flagged")
	}

	return agent.Reply{}, nil
}

func TestAModeChangeThatFailedIsSaidOnceAndNotRepeated(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{failures: 1}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.toggleCap(caps.Write)

	takeTestTurn := func(message string) {
		self.start(message)
		for report := range self.currentTurn.Events() {
			self.takeTurn(report)
		}
		self.finish()
	}

	takeTestTurn("first")
	takeTestTurn("second")
	takeTestTurn("third")

	if said := countModeNotes(provider.messages, nowReadOnlyNote); said != 1 {
		t.Errorf("expected the mode change to be carried once, said %d times in %q", said, provider.messages)
	}
}

func TestModeChangesAcrossFailedTurnsDoNotAccumulate(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{failures: 3}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	takeTestTurn := func(message string) []string {
		provider.messages = nil
		self.start(message)
		for report := range self.currentTurn.Events() {
			self.takeTurn(report)
		}
		self.finish()

		return provider.messages
	}

	self.toggleCap(caps.Write)
	takeTestTurn("first")

	self.toggleCap(caps.Git)
	said := takeTestTurn("second")

	if !slices.ContainsFunc(said, containsNote(historyNowReadWriteNote)) {
		t.Fatalf("expected the swap that had just been made, got %q", said)
	}

	if slices.ContainsFunc(said, containsNote(nowReadOnlyNote)) {
		t.Errorf("expected a swap said in an earlier turn not to be said again, got %q", said)
	}
}

const (
	nowReadOnlyNote         = "Workspace is now read-only."
	historyNowReadWriteNote = ".git is now read-write."
)

func containsNote(note string) func(message string) bool {
	return func(message string) bool { return strings.Contains(message, note) }
}

func isReadOnlyNote(message string) bool {
	return strings.Contains(message, nowReadOnlyNote)
}

func countModeNotes(messages []string, note string) int {
	said := 0
	for _, message := range messages {
		if strings.Contains(message, note) {
			said++
		}
	}

	return said
}

func TestPendingInputCanTakeBackAnyMessage(t *testing.T) {
	var pending pendingNotices
	pending.add(agent.Event{Kind: caps.ModeChange, Text: "first"})
	pending.add(agent.Event{Kind: pathgrant.Change, Text: "second"})

	pending.takeBack(0)

	remaining := pending.items
	if len(remaining) != 1 || remaining[0].state.Kind != pathgrant.Change || remaining[0].state.Text != "second" {
		t.Errorf("got %+v", pending.items)
	}
}

func TestAQueuedModeChangeCanBeTakenBackBeforeItStarts(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.toggleCap(caps.Write)

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Error("a taken-back mode change started another turn")
	}
	if !self.queuedTurn.Empty() || len(self.pendingNotices.items) != 0 {
		t.Errorf("taken-back mode change remained queued: %+v %v", self.queuedTurn.Peek(), self.pendingNotices.items)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.CanResume() {
		t.Error("taking back the queued mode change left the session unsafe")
	}
}

func TestAQueuedModeChangeStartsWithItsDisplayedMessage(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = log.Close() }()

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&screenOutput),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)

	if strings.Contains(screenOutput.String(), workspaceNowReadOnly()) {
		t.Errorf("expected the mode notice after the interrupted turn finishes, got %q", screenOutput.String())
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if !self.queuedTurn.Empty() {
		t.Error("expected the queued mode change to have been taken")
	}

	if !self.currentTurn.Running() {
		t.Fatal("expected the mode change to have started a turn of its own")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	modeMessage := workspaceNowReadOnly()
	if count := strings.Count(screenOutput.String(), modeMessage); count != 1 {
		t.Errorf("expected the mode message once, got %d in %q", count, screenOutput.String())
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.CanResume() {
		t.Error("expected the completed mode-message turn to be resumable")
	}

	if messages := submittedTexts(storedSession.Events); !slices.Equal(messages, []string{"first", modeMessage}) {
		t.Errorf("got submitted messages %q", messages)
	}
}

var (
	captureSecretPattern = regexp.MustCompile(`(?i)(authorization|bearer[[:space:]]|access_token|refresh_token|api[_-]?key|sk-[a-z0-9]{16,}|eyJ[a-z0-9_-]+\.)`)
	capturePIIPattern    = regexp.MustCompile(`(?i)(?:\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b|(?:^|[^0-9.])(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?)`)
)

func TestSanitisedWireCapturesContainNoCredentialsOrHostPaths(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "captures", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no sanitised wire captures")
	}

	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), func(t *testing.T) {
			capture, err := os.Open(path) //nolint:gosec // the path comes from a testdata glob
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = capture.Close() }()

			lines := bufio.NewScanner(capture)
			lines.Buffer(nil, 16*1024*1024)
			lineNumber := 0
			for lines.Scan() {
				lineNumber++
				line := lines.Text()
				if captureSecretPattern.MatchString(line) {
					t.Errorf("line %d contains a credential-shaped value", lineNumber)
				}
				if capturePIIPattern.MatchString(line) {
					t.Errorf("line %d contains an email or network address", lineNumber)
				}
				if strings.Contains(line, "/home/") {
					t.Errorf("line %d contains a host path", lineNumber)
				}

				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Errorf("line %d is not JSON: %v", lineNumber, err)
				} else {
					validateCapturePrivacy(t, record, "record")
				}
			}
			if err := lines.Err(); err != nil {
				t.Fatal(err)
			}
			if lineNumber < 2 {
				t.Errorf("capture has %d records, want a head and at least one exchange", lineNumber)
			}
		})
	}
}

func validateCapturePrivacy(t *testing.T, value any, location string) {
	t.Helper()

	switch typed := value.(type) {
	case map[string]any:
		if instructions, exists := typed["instructions"]; exists && instructions != "<system>" {
			t.Errorf("%s contains an unredacted system prompt", location)
		}
		role, _ := typed["role"].(string)
		if role == "system" && typed["content"] != "<system>" {
			t.Errorf("%s contains an unredacted system message", location)
		}
		if role == "assistant" {
			requireCapturePlaceholder(t, typed, "content", "<answer>", location)
			requireCapturePlaceholder(t, typed, "reasoning", "<reasoning>", location)
			requireCapturePlaceholder(t, typed, "reasoning_content", "<reasoning>", location)
		}
		if role == "tool" {
			requireCapturePlaceholder(t, typed, "content", "<tool-output>", location)
		}
		if workspace, exists := typed["workspace"]; exists && workspace != "<workspace>" {
			t.Errorf("%s contains an unredacted workspace", location)
		}
		if identifier, exists := typed["safety_identifier"]; exists && identifier != "<safety-identifier>" {
			t.Errorf("%s contains an unredacted safety identifier", location)
		}
		typeName, _ := typed["type"].(string)
		switch typeName {
		case "thinking", "thinking_delta":
			requireCapturePlaceholder(t, typed, "thinking", "<reasoning>", location)
		case "summary_text":
			requireCapturePlaceholder(t, typed, "text", "<reasoning>", location)
		case "output_text", "text_delta":
			requireCapturePlaceholder(t, typed, "text", "<answer>", location)
		case "input_json_delta":
			requireCapturePlaceholder(t, typed, "partial_json", "<arguments>", location)
		case "response.output_text.done":
			requireCapturePlaceholder(t, typed, "text", "<answer>", location)
		case "response.reasoning_summary_text.done":
			requireCapturePlaceholder(t, typed, "text", "<reasoning>", location)
		case "response.function_call_arguments.done":
			requireCapturePlaceholder(t, typed, "arguments", "<arguments>", location)
		}
		if role == "" && typeName == "" {
			requireCapturePlaceholder(t, typed, "content", "<answer>", location)
			requireCapturePlaceholder(t, typed, "reasoning", "<reasoning>", location)
			requireCapturePlaceholder(t, typed, "reasoning_content", "<reasoning>", location)
		}
		requireCapturePlaceholder(t, typed, "arguments", "<arguments>", location)
		requireCapturePlaceholder(t, typed, "partial_json", "<arguments>", location)
		requireCapturePlaceholder(t, typed, "output", "<tool-output>", location)
		for key, child := range typed {
			validateCapturePrivacy(t, child, location+"."+key)
		}
	case []any:
		for i, child := range typed {
			validateCapturePrivacy(t, child, location+"["+strconv.Itoa(i)+"]")
		}
	}
}

func requireCapturePlaceholder(
	t *testing.T,
	value map[string]any,
	field string,
	placeholder string,
	location string,
) {
	t.Helper()

	text, exists := value[field].(string)
	if exists && text != "" && text != placeholder {
		t.Errorf("%s.%s contains unredacted content", location, field)
	}
}

type wireCaptureRecord struct {
	Kind     string            `json:"kind"`
	Provider string            `json:"provider"`
	Response []json.RawMessage `json:"response"`
}

func TestSanitisedWireCaptureLifecyclesAreCoveredByScenarios(t *testing.T) {
	capturedFeatures := map[string]struct{}{}
	capturePaths, err := filepath.Glob(filepath.Join("testdata", "captures", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range capturePaths {
		capture, err := os.Open(path) //nolint:gosec // the path comes from a testdata glob
		if err != nil {
			t.Fatal(err)
		}

		provider := ""
		lines := bufio.NewScanner(capture)
		lines.Buffer(nil, 16*1024*1024)
		for lines.Scan() {
			var record wireCaptureRecord
			if err := json.Unmarshal(lines.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record.Kind == "capture" {
				provider = record.Provider
			}
			for _, payload := range record.Response {
				addWireLifecycleFeatures(capturedFeatures, provider, payload)
			}
		}
		if err := lines.Err(); err != nil {
			t.Fatal(err)
		}
		_ = capture.Close()
	}

	scenarioFeatures := map[string]struct{}{}
	scenarioPaths, err := filepath.Glob(filepath.Join("testdata", "scenarios", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range scenarioPaths {
		scenario := readSessionGoldenScenario(t, path)
		provider := scenario.Provider
		if provider == "chat" {
			provider = "opencode-go"
		}
		for _, turn := range []sessionGoldenTurn{scenario.FirstTurn, scenario.ResumeTurn} {
			for _, response := range turn.Responses {
				for _, payload := range response.Events {
					addWireLifecycleFeatures(scenarioFeatures, provider, json.RawMessage(payload))
				}
			}
		}
	}

	for feature := range capturedFeatures {
		if _, isCovered := scenarioFeatures[feature]; !isCovered {
			t.Errorf("captured lifecycle feature %q has no generated scenario", feature)
		}
	}
}

func addWireLifecycleFeatures(features map[string]struct{}, provider string, payload json.RawMessage) {
	var done string
	if json.Unmarshal(payload, &done) == nil && done == "[DONE]" || string(payload) == "[DONE]" {
		features[provider+"/done"] = struct{}{}
		return
	}

	var event map[string]any
	if json.Unmarshal(payload, &event) != nil {
		return
	}

	switch provider {
	case "opencode-go":
		choices, _ := event["choices"].([]any)
		for _, rawChoice := range choices {
			choice, _ := rawChoice.(map[string]any)
			delta, _ := choice["delta"].(map[string]any)
			for _, field := range []string{"content", "reasoning_content", "reasoning", "refusal", "tool_calls"} {
				if value := delta[field]; value != nil && value != "" {
					features["chat/"+field] = struct{}{}
				}
			}
			if reason, _ := choice["finish_reason"].(string); reason != "" {
				features["chat/finish/"+reason] = struct{}{}
			}
		}

	case "anthropic":
		eventType, _ := event["type"].(string)
		switch eventType {
		case "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop", "error":
			features["anthropic/"+eventType] = struct{}{}
		}
		for _, field := range []string{"content_block", "delta"} {
			value, _ := event[field].(map[string]any)
			if valueType, _ := value["type"].(string); valueType != "" {
				features["anthropic/"+field+"/"+valueType] = struct{}{}
			}
		}

	case "codex":
		eventType, _ := event["type"].(string)
		switch eventType {
		case "response.output_text.delta", "response.refusal.delta", "response.output_text.done", "response.reasoning_summary_text.delta", "response.reasoning_summary_part.done", "response.reasoning_text.delta", "response.reasoning_text.done", "response.output_item.done", "response.completed", "response.done", "response.incomplete", "response.failed", "error":
			features["codex/"+eventType] = struct{}{}
		}
		if eventType == "response.output_item.done" {
			item, _ := event["item"].(map[string]any)
			if itemType, _ := item["type"].(string); itemType != "" {
				features["codex/item/"+itemType] = struct{}{}
			}
		}
		if eventType == "response.reasoning_summary_part.done" {
			part, _ := event["part"].(map[string]any)
			if partType, _ := part["type"].(string); partType != "" {
				features["codex/part/"+partType] = struct{}{}
			}
		}
	}
}

func TestGoldenPickerMenuAlignmentMatchesTheGolden(t *testing.T) {
	stream := menu.RenderMenu("Choose your provider:", []string{"ChatGPT", "Anthropic", "OpenCode Go"}, 0)
	compareWithGolden(t, "picker-menu", ".ansi", map[string]func() string{
		"initial frame": func() string { return stream },
	})
	compareWithGolden(t, "picker-menu", ".screen", map[string]func() string{
		"initial frame": func() string {
			return strings.Join(visibleScreen(t, stream, 80), "\n")
		},
	})
}

func TestGoldenLongAuthorisationURLMatchesTheGolden(t *testing.T) {
	address := "https://auth.example.test/oauth/authorize?client_id=oh-desktop&code_challenge=abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ&code_challenge_method=S256&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&response_type=code&scope=openid%20profile%20email%20offline_access"
	stream := link.RenderURL(address, address)
	compareWithGolden(t, "authorisation-url", ".ansi", map[string]func() string{
		"long URL": func() string { return stream },
	})
	compareWithGolden(t, "authorisation-url", ".screen", map[string]func() string{
		"long URL": func() string {
			return strings.Join(visibleScreen(t, stream, 80), "\n")
		},
	})
}

func TestGoldenCompletionProtocolMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cachePath := location.GetModelCachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := checkedModelCache(`{"codex":{"models":[{"id":"gpt-5","efforts":["low","high"],"output":128000}]},"anthropic":{"models":[{"id":"claude-sonnet-5","efforts":["none","high"],"output":128000}]}}`)
	if err := os.WriteFile(cachePath, cache, 0o600); err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	workspaceDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	writeStoredSession(t, directory, workspaceDir, "older-badger", "2024-01-01T00:00:00Z")
	writeStoredSession(t, directory, workspaceDir, "newer-jaguar", "2025-01-01T00:00:00Z")
	sources := cli.Sources{ModelCachePath: cachePath, SessionsDir: directory, ToolNames: completableToolNames}

	requests := []struct {
		name string
		args []string
	}{
		{name: "options", args: []string{"--complete", "option", ""}},
		{name: "models", args: []string{"--complete", "model", "sonnet"}},
		{name: "models of a provider", args: []string{"--complete", "model", "anthropic/"}},
		{name: "efforts", args: []string{"--complete", "effort", "sonnet@"}},
		{name: "providers", args: []string{"--complete", "provider", ""}},
		{name: "capabilities", args: []string{"--complete", "caps", "rxw"}},
		{name: "tools", args: []string{"--complete", "tool", ""}},
		{name: "sessions", args: []string{"--complete", "session", ""}},
	}

	var output strings.Builder
	for _, request := range requests {
		completions, wanted := cli.Complete(request.args, sources)
		if !wanted {
			t.Fatalf("%s completion request was not recognised", request.name)
		}
		_, _ = output.WriteString("=== " + request.name + " ===\n")
		for _, completion := range completions {
			_, _ = output.WriteString(completion + "\n")
		}
	}

	goldenPath := filepath.Join("testdata", "output", "completion.txt")
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(output.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if output.String() != string(want) {
		t.Errorf("completion protocol differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, output.String(), want)
	}
}

func TestGoldenResumeArgumentsMatchTheGolden(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	cases := [][]string{
		{"-r"},
		{"--resume"},
		{"-r", "chosen-lobster"},
		{"--resume", "chosen-lobster"},
	}

	var output strings.Builder
	for _, arguments := range cases {
		os.Args = append([]string{"oh"}, arguments...)
		input := cli.Bind()
		fmt.Fprintf(
			&output,
			"%-28q picker=%-5t session=%q\n",
			strings.Join(arguments, " "),
			input.IsSessionPicker,
			input.Session,
		)
	}

	compareTextWithGolden(t, "resume-arguments.txt", output.String())
}

func TestGoldenPrintArgumentsMatchTheGolden(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	cases := []struct {
		arguments     []string
		isPromptPiped bool
	}{
		{arguments: []string{"have", "a", "look"}},
		{arguments: []string{"-p", "have", "a", "look"}},
		{arguments: []string{"--print", "have", "a", "look"}},
		{arguments: []string{"-p"}},
		{arguments: []string{"-p"}, isPromptPiped: true},
		{arguments: []string{"-p", "-r"}},
		{arguments: []string{"-p", "-m"}},
		{arguments: []string{"-p", "-r"}, isPromptPiped: true},
		{arguments: []string{"-p", "-r", "chosen-lobster", "have", "a", "look"}},
		{arguments: []string{"-p", "-m", "codex/gpt-5.3-codex@high", "have", "a", "look"}},
		{arguments: []string{"-p", "--from", "chosen-lobster"}},
	}

	var output strings.Builder
	for _, testCase := range cases {
		os.Args = append([]string{"oh"}, testCase.arguments...)
		input := cli.Bind()

		refusal := "allowed"
		if err := input.Check(testCase.isPromptPiped); err != nil {
			refusal = err.Error()
		}

		fmt.Fprintf(
			&output,
			"%-52q printing=%-5t piped=%-5t %s\n",
			strings.Join(testCase.arguments, " "),
			input.IsPrinting,
			testCase.isPromptPiped,
			refusal,
		)
	}

	compareTextWithGolden(t, "print-arguments.txt", output.String())
}

func TestGoldenModelArgumentsMatchTheGolden(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	cases := [][]string{
		{"-m"},
		{"--model"},
		{"-m", "codex/gpt-5.3-codex@high"},
		{"--model", "codex/gpt-5.3-codex@high"},
		{"-m", "-r"},
	}

	var output strings.Builder
	for _, arguments := range cases {
		os.Args = append([]string{"oh"}, arguments...)
		input := cli.Bind()
		fmt.Fprintf(
			&output,
			"%-32q picker=%-5t model=%q\n",
			strings.Join(arguments, " "),
			input.IsModelPicker,
			input.Model,
		)
	}

	compareTextWithGolden(t, "model-arguments.txt", output.String())
}

func TestGoldenRefusingAModelOnResumeMatchesTheGolden(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	cases := [][]string{
		{"-r"},
		{"-r", "chosen-lobster"},
		{"-m"},
		{"-m", "codex/gpt-5.3-codex@high"},
		{"-r", "-m"},
		{"-r", "-m", "codex/gpt-5.3-codex@high"},
		{"-r", "chosen-lobster", "-m"},
		{"-r", "chosen-lobster", "-m", "codex/gpt-5.3-codex@high"},
		{"--resume", "chosen-lobster", "--model", "codex/gpt-5.3-codex@high"},
		{"--from", "chosen-lobster", "-m", "codex/gpt-5.3-codex@high"},
		{"-t", "Read"},
		{"-r", "chosen-lobster", "-t", "Read"},
		{"--from", "chosen-lobster", "-t", "Read"},
	}

	var output strings.Builder
	for _, arguments := range cases {
		os.Args = append([]string{"oh"}, arguments...)
		input := cli.Bind()

		refusal := "allowed"
		if err := input.Check(false); err != nil {
			refusal = err.Error()
		}

		fmt.Fprintf(&output, "%-56q %s\n", strings.Join(arguments, " "), refusal)
	}

	compareTextWithGolden(t, "resume-model-arguments.txt", output.String())
}

func TestGoldenTheUsageArgumentsMatchTheGolden(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })

	cases := [][]string{
		{"-U"},
		{"--usage"},
		{"-U", "-J"},
		{"--usage", "--json"},
	}

	var output strings.Builder
	for _, arguments := range cases {
		os.Args = append([]string{"oh"}, arguments...)
		input := cli.Bind()
		fmt.Fprintf(
			&output,
			"%-32q usage=%-5t json=%t\n",
			strings.Join(arguments, " "),
			input.Usage,
			input.JSON,
		)
	}

	compareTextWithGolden(t, "usage-arguments.txt", output.String())
}

func TestGoldenTheUsageReportMatchesTheGolden(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	cachePath := location.GetUsageCachePath("codex", false)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}

	cache := `{"version":1,"fetched_at":"2026-01-02T12:00:00Z","windows":[` +
		`{"duration":18000000000000,"percent":41.2,"resets_at":"2026-01-02T14:00:00Z"},` +
		`{"duration":604800000000000,"percent":63.9,"resets_at":"2026-01-05T00:00:00Z"},` +
		`{"duration":18000000000000,"percent":12,"resets_at":"2026-01-02T14:00:00Z","scope":"gpt-5.3-codex-spark"}` +
		`]}`
	if err := os.WriteFile(cachePath, []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}

	var written strings.Builder
	if err := usage.Show(t.Context(), &written, usage.Options{JSON: true}); err != nil {
		t.Fatal(err)
	}

	var document bytes.Buffer
	if err := json.Indent(&document, []byte(strings.TrimSpace(written.String())), "", "    "); err != nil {
		t.Fatal(err)
	}

	written.Reset()
	for line := range strings.SplitSeq(document.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), `"age_seconds":`) {
			line = strings.Split(line, ":")[0] + `: <age>,`
		}

		written.WriteString(line)
		written.WriteString("\n")
	}

	compareTextWithGolden(t, "usage.json", strings.TrimSuffix(written.String(), "\n"))
}

func compareTextWithGolden(t *testing.T, name string, got string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", "output", name)
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("what was written differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}

func configFrom(t *testing.T, body string) config.Config {
	t.Helper()

	if body == "" {
		config, err := config.Load("")
		if err != nil {
			t.Fatal(err)
		}

		return config
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	version := fmt.Sprintf("version = %d\n", config.Format)
	if err := os.WriteFile(path, []byte(version+undentConfig(body)), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	return config
}

func undentConfig(body string) string {
	var output strings.Builder
	for row := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		output.WriteString(strings.TrimLeft(row, "\t"))
		output.WriteString("\n")
	}

	return output.String()
}

func builtInConfig(t *testing.T) config.Config {
	t.Helper()

	config, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}

	return config
}

func TestWhatWasAskedIsRenderedIntoTheConversation(t *testing.T) {
	for _, isLive := range []bool{true, false} {
		var screenOutput bytes.Buffer

		painter := newTestPainter(output.New(&screenOutput), isLive)
		painter.DrawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "**weather**\n\n- today"})

		plain := style.Plain(screenOutput.String())
		if !strings.Contains(plain, " weather\n \n • today") {
			t.Errorf("isLive=%v: expected the question's markdown to be rendered, got %q", isLive, plain)
		}

		if strings.Contains(plain, "> ") || strings.Contains(plain, "**") {
			t.Errorf("isLive=%v: expected no literal prompt or markdown markers, got %q", isLive, plain)
		}

		if !strings.HasSuffix(plain, "\n") {
			t.Errorf("isLive=%v: expected the submitted message to finish its line immediately, got %q", isLive, plain)
		}
	}
}

func TestASubmittedMessageHasBackgroundRowsAboveAndBelowIt(t *testing.T) {
	got := style.Plain(painter.RenderSubmittedMessage("hello", 8))
	want := "        \n hello  \n        "

	if got != want {
		t.Errorf("submitted message was %q, want %q", got, want)
	}
}

func TestReplayingACallThatWasNeverAnsweredLeavesNothingRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var screenOutput bytes.Buffer

		testConversation := &App{
			agent:    agent.New("", quietProvider{}, nil),
			screen:   output.New(&screenOutput),
			recorder: record.New(testLog(t)),
		}

		testConversation.restore(&store.Session{
			Events: []agent.Event{
				{Kind: agent.UserMessageEvent, Text: "read them both"},
				{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}},
				{Kind: agent.ToolCallRequestEvent, ID: "2", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "two.go"}},
				{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Text: "package one"},
			},
		})

		if plain := style.Plain(screenOutput.String()); !strings.Contains(plain, "read two.go –") {
			t.Errorf("expected the unanswered call to be closed on replay, got %q", plain)
		}
	})
}

func TestRestoringAConversationClearsTheTerminalBeforeReplaying(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, 80, 24),
		recorder: record.New(testLog(t)),
	}

	self.restore(&store.Session{Events: []agent.Event{{
		Kind: agent.UserMessageEvent,
		Text: "restored conversation",
	}}})

	const clearTerminal = "\x1b[H\x1b[2J\x1b[3J"
	if !strings.HasPrefix(screenOutput.String(), clearTerminal) {
		t.Errorf("expected the terminal to be cleared before replay, got %q", screenOutput.String())
	}
	if !strings.Contains(style.Plain(screenOutput.String()), "restored conversation") {
		t.Errorf("expected the conversation to be replayed after clearing, got %q", screenOutput.String())
	}
}

func TestRestoringAConversationRestoresStateBeforeReturning(t *testing.T) {
	var restored string
	statefulTool := tool.Implement(
		tool.Definition{Name: "stateful", Description: "", Schema: tool.Schema{}},
		func(struct{}) (string, string) { return "", "" },
	).State("test_state", func(state json.RawMessage) error {
		restored = string(state)
		return nil
	}).Plain(func(context.Context, struct{}) (string, error) { return "", nil })

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, []tool.Tool{statefulTool}),
		screen:   output.New(&screenOutput),
		recorder: record.New(testLog(t)),
	}
	self.restore(&store.Session{Events: []agent.Event{{
		Kind:  agent.StateChangeEvent,
		Name:  "test_state",
		State: json.RawMessage(`{"answer":42}`),
	}}})

	if restored != `{"answer":42}` {
		t.Errorf("got restored state %q", restored)
	}
}

func TestLastMessageIsTheMostRecentModelMessage(t *testing.T) {
	self := &App{recordedEvents: []agent.Event{
		{Kind: agent.ModelMessageEvent, Text: "first answer"},
		{Kind: agent.UserMessageEvent, Text: "follow up"},
		{Kind: agent.ModelMessageEvent, Text: "latest answer"},
		{Kind: agent.SilentTurnEvent},
	}}

	message, found := self.getLastMessage()
	if !found || message != "latest answer" {
		t.Errorf("got %q and %t", message, found)
	}
}

func TestLastMessageIsUnavailableBeforeAModelMessage(t *testing.T) {
	self := &App{recordedEvents: []agent.Event{{Kind: agent.UserMessageEvent, Text: "hello"}}}

	if message, found := self.getLastMessage(); found || message != "" {
		t.Errorf("got %q and %t", message, found)
	}
}

func TestReplayingSaysTheWholeConversationAgain(t *testing.T) {
	var screenOutput bytes.Buffer

	testConversation := &App{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}

	testConversation.recordedEvents = []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "what is the weather"},
		{Kind: agent.ModelMessageEvent, Text: "it is raining"},
		{Kind: agent.SilentTurnEvent},
	}

	testConversation.replay()

	for _, want := range []string{"what is the weather", "it is raining", agent.SilentTurnNotice} {
		if !strings.Contains(screenOutput.String(), want) {
			t.Errorf("expected %q to be drawn again, got %q", want, screenOutput.String())
		}
	}
}

func TestAReadOfASkillIsDrawnAsTheSkill(t *testing.T) {
	const skillPath = "/skills/golang/SKILL.md"

	tests := map[string]struct {
		tool    string
		path    string
		want    string
		painted string
	}{
		"a read of a skill": {
			tool:    "read",
			path:    skillPath,
			want:    "load " + skillPath,
			painted: style.Skill("golang"),
		},
		"a read of a file": {
			tool:    "read",
			path:    "cmd/oh/draw.go",
			want:    "read cmd/oh/draw.go",
			painted: style.Subject("draw.go"),
		},
		"another tool": {tool: "grep", path: skillPath, want: "grep " + skillPath},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			callPainter := newTestPainter(output.New(&screenOutput), false)

			callPainter.DrawEvent(agent.Event{
				Kind: agent.ToolCallRequestEvent,
				ID:   "1",
				Name: test.tool,
				FallbackRendering: agent.FallbackRendering{
					Subject:  test.path,
					Emphasis: tool.Emphasis{Kind: tool.EmphasisFocus, Value: path.Base(test.path)},
				},
			})
			callPainter.Close(dynamic.Done)

			if plain := style.Plain(screenOutput.String()); !strings.Contains(plain, test.want) {
				t.Errorf("got %q, want %q", plain, test.want)
			}
			if test.painted != "" && !strings.Contains(screenOutput.String(), test.painted) {
				t.Errorf("got %q, want %q painted", screenOutput.String(), test.painted)
			}
		})
	}
}

func TestTheFileASkillIsKeptInIsNotStoodOut(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := newTestPainter(output.New(&screenOutput), false)

	callPainter.DrawEvent(agent.Event{
		Kind: agent.ToolCallRequestEvent,
		ID:   "1",
		Name: "read",
		FallbackRendering: agent.FallbackRendering{
			Subject:  "/skills/golang/SKILL.md",
			Emphasis: tool.Emphasis{Kind: tool.EmphasisFocus, Value: "SKILL.md"},
		},
	})
	callPainter.Close(dynamic.Done)

	if strings.Contains(screenOutput.String(), style.Subject("SKILL.md")) {
		t.Errorf("got %q, want the file left dim", screenOutput.String())
	}
}

func TestWhetherACallChangedAnythingComesFromTheToolOfTheMoment(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &App{
		agent:  agent.New("", quietProvider{}, []tool.Tool{slowTool("write")}),
		screen: output.New(&screenOutput),
	}
	callPainter := self.newPainter(false)

	callPainter.DrawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "write",
		Arguments:         `{"path":"one.go"}`,
		FallbackRendering: agent.FallbackRendering{ReadOnly: true},
	})
	callPainter.Close(dynamic.Done)

	if want := style.Change("write"); !strings.Contains(screenOutput.String(), want) {
		t.Errorf("got %q, want %q", screenOutput.String(), want)
	}
}

func TestACallToAToolThatIsGoneKeepsWhatWasRecorded(t *testing.T) {
	var screenOutput bytes.Buffer
	self := &App{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}
	callPainter := self.newPainter(false)

	callPainter.DrawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "gone",
		FallbackRendering: agent.FallbackRendering{Subject: "one.go", ReadOnly: true},
	})
	callPainter.Close(dynamic.Done)

	if want := style.Call("gone"); !strings.Contains(screenOutput.String(), want) {
		t.Errorf("got %q, want %q", screenOutput.String(), want)
	}
}

func TestASilentTurnIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	var live strings.Builder
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&live, 80, 24),
		recorder: record.New(testLog(t)),
	}

	call := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}}

	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.recordedEvents = append(self.recordedEvents, call)
	self.currentTurn.painter.DrawEvent(call)

	self.notify(agent.Event{Kind: agent.SilentTurnEvent})

	self.currentTurn.painter.Close(dynamic.Cancelled)
	self.currentTurn = Turn{}
	self.screen.End()

	var replayOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&replayOutput, 80, 24)
	self.replay()

	if visibleScreen(t, live.String(), 80) == nil {
		t.Fatal("a notice said live left nothing on the screen")
	}

	requireSameVisibleScreenInColumns(
		t,
		"a notice said live leaves a different screen from one replayed",
		80,
		live.String(),
		replayOutput.String(),
	)
}

func TestTheWholeConversationIsDrawnTheSameLiveAndReplayed(t *testing.T) {
	tools := []tool.Tool{slowTool("read")}

	var live bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, tools),
		screen:   output.New(&live),
		recorder: record.New(testLog(t)),
	}
	livePainter := self.newPainter(true)

	events := []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "**Check** this"},
		{Kind: agent.ModelReasoningEvent, Text: "**Reading**\nLooking at the file. Need care."},
		{Kind: agent.ModelMessageEvent, Text: "The **first** answer.\n"},
		{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", Arguments: `{"path":"one.go"}`, FallbackRendering: agent.FallbackRendering{Subject: "old"}},
		{Kind: agent.ToolCallRequestEvent, ID: "2", Name: "gone", FallbackRendering: agent.FallbackRendering{Subject: "two.go"}},
		{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Took: 2 * time.Second},
		{Kind: agent.ToolCallResultEvent, ID: "2", Name: "gone", Status: agent.ErrorStatus, Took: 3 * time.Second},
		{Kind: agent.ModelMessageEvent, Text: "Done."},
	}

	for _, event := range events {
		self.recordedEvents = append(self.recordedEvents, event)
		livePainter.DrawEvent(event)
	}
	self.notifyFailure("The stored session could not be written (no space left)")

	unansweredCall := agent.Event{Kind: agent.ToolCallRequestEvent, ID: "3", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "left.go"}}
	self.recordedEvents = append(self.recordedEvents, unansweredCall)
	livePainter.DrawEvent(unansweredCall)
	livePainter.Close(dynamic.Cancelled)
	self.screen.End()

	var replayOutput bytes.Buffer
	self.screen = output.New(&replayOutput)
	self.replay()

	plain := style.Plain(live.String())

	for _, want := range []string{"Check", "Looking at the file.", "Need care.", "first answer.", "one.go", "Done."} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q on the screen, got %q", want, plain)
		}
	}
	if !strings.Contains(plain, "Need care.\n\nThe first") {
		t.Errorf("expected a blank line between reasoning and the answer, got %q", plain)
	}
	if self.feedback.Message().Text != "The stored session could not be written (no space left)" {
		t.Errorf("storage warning was not retained as interface feedback: %+v", self.feedback.Message())
	}

	if live.String() != replayOutput.String() {
		t.Errorf("live conversation %q differs from replayed conversation %q", live.String(), replayOutput.String())
	}
}

func TestAThoughtHasMarkdownStripped(t *testing.T) {
	rows := painter.RenderReasoning("## **Checking** `one.go`", 40, output.ReasoningPlain)
	for i := range rows {
		rows[i] = style.Plain(rows[i])
	}

	if got := strings.Join(rows, "\n"); got != "Checking one.go" {
		t.Errorf("got reasoning %q with markdown stripped", got)
	}
}

func TestAThoughtKeepsItsMarkdownWhenItIsAskedFor(t *testing.T) {
	rows := painter.RenderReasoning("## **Checking** `one.go`\n\n- first\n- second\n", 40, output.ReasoningMarkdown)

	if got := strings.Join(rows, "\n"); got == style.Plain(got) {
		t.Errorf("got reasoning %q with nothing styled", got)
	}
	if got := style.Plain(strings.Join(rows, "\n")); !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("got reasoning %q, want the list kept", got)
	}
	if got := style.Plain(strings.Join(rows, "\n")); strings.Count(got, "\n") < 2 {
		t.Errorf("got reasoning %q flattened onto one line", got)
	}
}

const (
	italicSequence = "\x1b[3m"
	dimSequence    = "\x1b[38;2;150;152;150m"
	resetSequence  = "\x1b[0m"
)

func TestAThoughtIsItalicAndDimWhicheverWayItIsDrawn(t *testing.T) {
	thought := "## Checking `one.go`\n\n- first\n- second\n"

	for name, rendering := range map[string]output.ReasoningRendering{
		"flattened": output.ReasoningPlain,
		"kept":      output.ReasoningMarkdown,
	} {
		drawn := strings.Join(painter.RenderReasoning(thought, 40, rendering), "\n")
		if !strings.Contains(drawn, italicSequence) {
			t.Errorf("a %s thought drew %q without the italic that says it is a thought", name, drawn)
		}
		if !strings.Contains(drawn, dimSequence) {
			t.Errorf("a %s thought drew %q without the dim that holds it back from the answer", name, drawn)
		}
	}
}

func TestAKeptThoughtResumesItsOwnStyleUnderTheMarkdown(t *testing.T) {
	rows := painter.RenderReasoning("## Checking `one.go`\n\n- first\n", 40, output.ReasoningMarkdown)

	for _, row := range rows {
		if !strings.HasPrefix(row, italicSequence+dimSequence) {
			t.Errorf("row %q does not open in the reasoning style", row)
		}
		for piece := range strings.SplitSeq(row, resetSequence) {
			if piece != "" && !strings.HasPrefix(piece, "\x1b[") {
				t.Errorf("row %q leaves %q unstyled after a reset", row, piece)
			}
		}
	}
}

func TestAThoughtRunsDirectlyIntoAToolCall(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := newTestPainter(output.New(&screenOutput), false)

	callPainter.DrawEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: "checking"})
	callPainter.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}})
	callPainter.Close(dynamic.Cancelled)

	plain := style.Plain(screenOutput.String())
	if !strings.Contains(plain, "checking\nread one.go") {
		t.Errorf("expected no blank line between reasoning and the tool call, got %q", plain)
	}
}

func TestAThoughtWrapsAtWordBoundaries(t *testing.T) {
	rows := painter.RenderReasoning("one two three four", 9, output.ReasoningPlain)
	for i := range rows {
		rows[i] = style.Plain(rows[i])
	}

	if got := strings.Join(rows, "\n"); got != "one two\nthree\nfour" {
		t.Errorf("got %q, want reasoning wrapped between words", got)
	}
}

func TestAStoredCallIsShownTheWayItsToolShowsItNow(t *testing.T) {
	var screenOutput bytes.Buffer

	current := truncate.Tool(buildSlowTool(
		slowToolBuilder("read").Focuses(func(tool.ToolCall) string { return "one.go" }),
	), truncate.NewLimit(12*1024))
	testConversation := &App{
		agent:  agent.New("", quietProvider{}, []tool.Tool{current}),
		screen: output.New(&screenOutput),
	}

	testConversation.recordedEvents = []agent.Event{{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "read",
		Arguments:         `{"path":"cmd/oh/one.go"}`,
		FallbackRendering: agent.FallbackRendering{Subject: "one.go:1-400"},
	}}

	testConversation.replay()

	if strings.Contains(screenOutput.String(), "one.go:1-400") {
		t.Errorf("expected the stored rendering to be redrawn, got %q", screenOutput.String())
	}

	want := style.Subtle("cmd/oh/") + style.Subject("one.go")
	if !strings.Contains(screenOutput.String(), want) {
		t.Errorf("expected the stored call to take the tool's current focus, got %q", screenOutput.String())
	}
}

func TestACallWhoseToolIsGoneKeepsWhatItLookedLike(t *testing.T) {
	var screenOutput bytes.Buffer

	testConversation := &App{
		agent:  agent.New("", quietProvider{}, nil),
		screen: output.New(&screenOutput),
	}

	testConversation.recordedEvents = []agent.Event{{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "divine",
		Arguments:         `{"path":"one.go"}`,
		FallbackRendering: agent.FallbackRendering{Subject: "one.go:1-400"},
	}}

	testConversation.replay()

	if !strings.Contains(screenOutput.String(), "one.go:1-400") {
		t.Errorf("expected what it looked like at the time, got %q", screenOutput.String())
	}
}

type messageCaptureProvider struct {
	messages []string
}

func (self *messageCaptureProvider) Configure(string, []tool.Definition) {}
func (self *messageCaptureProvider) AddUserMessage(message string) {
	self.messages = append(self.messages, message)
}
func (self *messageCaptureProvider) AddToolResults([]agent.ToolCallResult) {}
func (self *messageCaptureProvider) Dump() []json.RawMessage               { return nil }
func (self *messageCaptureProvider) Load([]json.RawMessage)                {}
func (self *messageCaptureProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

type quietProvider struct{}

func (quietProvider) Configure(string, []tool.Definition)   {}
func (quietProvider) AddUserMessage(string)                 {}
func (quietProvider) AddToolResults([]agent.ToolCallResult) {}
func (quietProvider) Dump() []json.RawMessage               { return nil }
func (quietProvider) Load([]json.RawMessage)                {}

func (quietProvider) Send(context.Context, agent.Yield) (agent.Reply, error) {
	return agent.Reply{}, nil
}

func testConversation(t *testing.T, screenOutput *bytes.Buffer) *App {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Cleanup(func() { _ = log.Close() })

	backend := quietProvider{}
	settings := builtInConfig(t)

	return &App{
		agent:           agent.New("", backend, nil),
		screen:          output.New(screenOutput),
		recorder:        record.New(log),
		mode:            caps.NewMode(caps.Read | caps.Write),
		slash:           slashState{commands: fixtureSnippetRegistry(t, nil)},
		continueMessage: settings.Input.Continue,
		editorConfig:    editor.NewConfiguration(settings.Editor.Command),
		toolOutputLimit: truncate.NewLimit(settings.Tool.Output.Bytes),
	}
}

func completeTurn(self *App) {
	self.start("are you there")

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}

	self.finish()
}

func TestATurnThatFinishedByItselfIsNotCalledCancelled(t *testing.T) {
	var screenOutput bytes.Buffer

	completeTurn(testConversation(t, &screenOutput))

	if strings.Contains(screenOutput.String(), "cancelled") {
		t.Errorf("expected a finished turn to say nothing about cancelling, got %q", screenOutput.String())
	}
}

func TestANonRetryableTurnErrorSendsADesktopNotification(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	turnFailure := errors.New("your prompt was flagged")
	self.agent = agent.New("", &refusingOnceProvider{failure: turnFailure, failures: 1}, nil)

	var notifiedFailure error
	wasTurnRunning := true
	self.onFailure = func(failure error) {
		notifiedFailure = failure
		wasTurnRunning = self.currentTurn.Running()
	}

	completeTurn(self)

	if !errors.Is(notifiedFailure, turnFailure) {
		t.Errorf("got notification error %v, want the original turn error", notifiedFailure)
	}
	if wasTurnRunning {
		t.Error("the turn was still running when its error notification was sent")
	}
}

func TestAStoppedTurnSaysWhyItStoppedInTheScrollback(t *testing.T) {
	var screenOutput bytes.Buffer

	self := testConversation(t, &screenOutput)

	self.start("are you there")
	self.currentTurn.SetCancelled(true)
	self.currentTurn.Interrupt(interrupt.Because(interrupt.Escape))

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}

	self.finish()

	plain := style.Plain(screenOutput.String())
	want := interrupt.Notice(interrupt.Event(interrupt.Escape))
	if !strings.Contains(plain, want) {
		t.Errorf("expected %q in the scrollback, got %q", want, plain)
	}

	if strings.Contains(screenOutput.String(), "context canceled") {
		t.Errorf("expected the stop to be reported as a stop, got %q", screenOutput.String())
	}
}

func TestAShellCallIsDrawnAsAShellPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	callPainter := newTestPainter(output.New(&screenOutput), false)

	callPainter.DrawEvent(agent.Event{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "bash", FallbackRendering: agent.FallbackRendering{Subject: "echo hello"}})
	callPainter.Close(dynamic.Done)

	plain := style.Plain(screenOutput.String())
	if !strings.Contains(plain, "$ echo hello") {
		t.Errorf("got %q, want a shell prompt", plain)
	}
	if strings.Contains(plain, "bash echo hello") {
		t.Errorf("got %q, want no tool name", plain)
	}
}

func TestARedrawDuringATurnHandsTheOpenBlockToTheTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var screenOutput bytes.Buffer

		testConversation := &App{
			agent:  agent.New("", quietProvider{}, nil),
			screen: output.New(&screenOutput),
		}

		testConversation.currentTurn = Turn{Stream: testRunningTurnStream(), painter: testConversation.newPainter(true)}

		testConversation.recordedEvents = []agent.Event{
			{Kind: agent.UserMessageEvent, Text: "read it"},
			{Kind: agent.ToolCallRequestEvent, ID: "1", Name: "read", FallbackRendering: agent.FallbackRendering{Subject: "one.go"}},
		}

		previousPainter := testConversation.currentTurn.painter

		testConversation.redraw()

		if testConversation.currentTurn.painter == previousPainter {
			t.Fatal("expected the turn to be given the painter that drew the replay")
		}

		testConversation.currentTurn.painter.DrawEvent(agent.Event{Kind: agent.ToolCallResultEvent, ID: "1", Name: "read", Took: time.Second})

		if plain := style.Plain(screenOutput.String()); !strings.Contains(plain, "read one.go ✓") {
			t.Errorf("expected the replayed call to be answered on the block that was handed over, got %q", plain)
		}
	})
}

func TestARedrawDuringProvisionalReasoningRestoresTheOpenBlock(t *testing.T) {
	var screenOutput bytes.Buffer
	testConversation := &App{
		agent:          agent.New("", quietProvider{}, nil),
		screen:         output.New(&screenOutput),
		recordedEvents: []agent.Event{{Kind: agent.UserMessageEvent, Text: "think"}},
	}
	testConversation.currentTurn = Turn{Stream: testRunningTurnStream(), painter: testConversation.newPainter(true)}
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{
		Kind: agent.ModelReasoningEvent,
		Text: "provisional thought",
	})

	testConversation.redraw()

	got := testConversation.currentTurn.painter.ProvisionalDelta()
	if got.Kind != agent.ModelReasoningEvent || got.Text != "provisional thought" {
		t.Errorf("got provisional delta %+v", got)
	}
}

func TestAStreamingMermaidDiagramKeepsItsLastValidRendering(t *testing.T) {
	const columns = 100
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, columns, 24), true)

	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	valid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if !strings.Contains(valid, "►") || strings.Contains(valid, "graph LR") {
		t.Fatalf("expected the first valid prefix to render, got %q", valid)
	}

	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	invalid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if invalid != valid {
		t.Errorf("invalid prefix replaced the last valid diagram\nvalid:\n%s\ninvalid:\n%s", valid, invalid)
	}

	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: " C"})
	nextValid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if !strings.Contains(nextValid, "C") || strings.Contains(nextValid, "graph LR") {
		t.Errorf("expected the next valid prefix to replace the cached diagram, got %q", nextValid)
	}
}

func TestARedrawKeepsTheLastValidStreamingMermaidDiagram(t *testing.T) {
	const columns = 100
	var screenOutput bytes.Buffer
	testConversation := &App{screen: output.NewTerminalOfSize(&screenOutput, columns, 24)}
	testConversation.currentTurn = Turn{Stream: testRunningTurnStream(), painter: testConversation.newPainter(true)}

	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	valid := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	testConversation.redraw()

	redrawn := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if redrawn != valid {
		t.Errorf("redraw replaced the last valid diagram\nvalid:\n%s\nredrawn:\n%s", valid, redrawn)
	}
}

func TestACompletedInvalidMermaidDiagramFallsBackToSource(t *testing.T) {
	const columns = 100
	const invalid = "```mermaid\ngraph LR\nA --> B\nB -->\n```"
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, columns, 24), true)

	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	painter.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: invalid})

	completed := strings.Join(visibleScreen(t, screenOutput.String(), columns), "\n")
	if !strings.Contains(completed, "graph LR") || !strings.Contains(completed, "B -->") {
		t.Errorf("expected completed invalid Mermaid to fall back to source, got %q", completed)
	}
}

func TestAnAnswerStreamedIsTheSameAsTheAnswerReplayed(t *testing.T) {
	const answer = "# Findings\n\nThe **first** thing is `read`.\n\n- one\n- two\n"

	var live bytes.Buffer

	livePainter := newTestPainter(output.New(&live), true)

	for _, delta := range deltas(answer, 10) {
		livePainter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
	}
	livePainter.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answer})
	livePainter.End()

	var replayOutput bytes.Buffer

	replayPainter := newTestPainter(output.New(&replayOutput), false)
	replayPainter.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answer})
	replayPainter.End()

	plain := style.Plain(live.String())

	for _, want := range []string{"Findings", "first thing is", "• one", "• two"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q drawn, got %q", want, plain)
		}
	}

	if live.String() != replayOutput.String() {
		t.Errorf("streamed %q, replayed %q", live.String(), replayOutput.String())
	}

	if strings.Contains(live.String(), "**") {
		t.Errorf("expected the markdown to be drawn rather than shown, got %q", live.String())
	}
}

func deltas(text string, count int) []string {
	var pieces []string

	size := (len(text) + count - 1) / count

	for at := 0; at < len(text); at += size {
		pieces = append(pieces, text[at:min(at+size, len(text))])
	}

	return pieces
}

func TestCallEmphasisSourceIsDerivedFromRecordedArguments(t *testing.T) {
	current := tool.Implement(
		tool.Definition{
			Name:        "bash",
			Description: "",
			Schema:      tool.Schema{tool.String("path", "command")},
		},
		func(args fakeArgs) (string, string) {
			subject, _, _ := strings.Cut(args.Path, "\n")
			return subject, ""
		},
	).SyntaxFrom("bash", func(args fakeArgs, _ string) string {
		return args.Path
	}).Plain(func(context.Context, fakeArgs) (string, error) { return "", nil })
	conversation := &App{agent: agent.New("", quietProvider{}, []tool.Tool{current})}

	fallback := describeHarnessCall(conversation, agent.Event{
		Name:      "bash",
		Arguments: `{"path":"cat <<EOF\none\nEOF"}`,
	})

	if got, want := fallback.Emphasis.Source, "cat <<EOF\none\nEOF"; got != want {
		t.Errorf("got source %q, want %q", got, want)
	}
}

func TestWorkspacePrefixIsOmittedFromRenderedCallPaths(t *testing.T) {
	const workspaceDir = "/home/alice/project"

	t.Setenv("HOME", "/home/alice")

	current := tool.Implement(
		tool.Definition{
			Name:        "read",
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, pathutil.Shorten(args.Path) },
	).FocusPath().Plain(func(context.Context, fakeArgs) (string, error) { return "", nil })
	testConversation := &App{
		agent:     agent.New("", quietProvider{}, []tool.Tool{current}),
		workspace: work.At(workspaceDir),
	}

	fallback := describeHarnessCall(testConversation, agent.Event{
		Name:      "read",
		Arguments: `{"path":"/home/alice/project/cmd/oh/draw.go"}`,
	})

	wantEmphasis := tool.Emphasis{Kind: tool.EmphasisFocus, Value: "draw.go"}
	if fallback.Emphasis != wantEmphasis {
		t.Fatalf("unexpected emphasis %#v", fallback.Emphasis)
	}
	if fallback.Subject != "cmd/oh/draw.go" || fallback.Note != "cmd/oh/draw.go" {
		t.Errorf("got rendering %q and detail %q, want the workspace prefixes omitted", fallback.Subject, fallback.Note)
	}
}

func TestRecordedCallPathsAreShortenedWithTheSameFunction(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	fallback := describeBareCall("/home/alice/project", agent.Event{
		Name: "removed",
		FallbackRendering: agent.FallbackRendering{
			Subject: "/home/alice/project/file.go",
			Note:    "/home/alice/reference/file.go",
		},
	})

	if fallback.Subject != "file.go" || fallback.Note != "~/reference/file.go" {
		t.Errorf("got subject %q and qualifier %q, want both path prefixes shortened", fallback.Subject, fallback.Note)
	}
}

func TestARefusedCallIsDescribedAgainRatherThanFromTheRecord(t *testing.T) {
	refusing := tool.Implement(
		tool.Definition{
			Name:        "shout",
			Description: "",
			Schema:      tool.Schema{tool.String("message", "what to shout")},
		},
		func(struct{}) (string, string) { return "", "" },
	).Validate(func(struct{}) error {
		return errors.New("not in the mood")
	}).Plain(func(context.Context, struct{}) (string, error) { return "", nil })

	testConversation := &App{
		agent: agent.New("", quietProvider{}, []tool.Tool{refusing}),
	}

	fallback := describeHarnessCall(testConversation, agent.Event{
		Name:              "shout",
		Arguments:         `{"message":"oi"}`,
		FallbackRendering: agent.FallbackRendering{Subject: ""},
	})

	if fallback.Note != "" || fallback.Emphasis != (tool.Emphasis{}) {
		t.Fatalf("unexpected call description %q, %#v", fallback.Note, fallback.Emphasis)
	}
	if fallback.Subject != "oi" {
		t.Errorf("got %q, want the arguments described again from the record", fallback.Subject)
	}
}

func testLog(t *testing.T) *store.Writer {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = log.Close() })

	return log
}

func TestAnAsideStandsBetweenTheCallsItArrivedAmong(t *testing.T) {
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, 80, 24), true)

	painter.DrawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "1",
		Name:              "read",
		FallbackRendering: agent.FallbackRendering{Subject: "one.txt"},
	})
	painter.DrawEvent(agent.Event{Kind: agent.SilentTurnEvent})

	painter.DrawEvent(agent.Event{Kind: agent.ToolCallResultEvent, ID: "1", Took: time.Second})

	rows := visibleScreen(t, screenOutput.String(), 80)
	rows = slices.DeleteFunc(rows, func(row string) bool { return strings.TrimSpace(row) == "" })

	if len(rows) != 2 {
		t.Fatalf("expected the call and the aside on rows of their own, got %q", rows)
	}
	if !strings.Contains(rows[0], "one.txt") || !strings.Contains(rows[0], "✓") {
		t.Errorf("expected the call to keep its result above the aside, got %q", rows[0])
	}
	if !strings.Contains(rows[1], agent.SilentTurnNotice) {
		t.Errorf("expected the aside under the call, got %q", rows[1])
	}
}

func TestCompletedReasoningBlocksRemainSeparateInTheJournal(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := &App{
		screen:   output.New(&screenOutput),
		recorder: record.New(log),
	}
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	userMessage := agent.Event{Kind: agent.UserMessageEvent, Text: "think twice"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &userMessage}})
	firstDelta := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "**First "}
	self.takeTurn(TurnEvent{Update: agent.Update{Delta: &firstDelta}})
	firstBlock := agent.Event{Kind: agent.ModelReasoningEvent, Text: "**First block**"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &firstBlock}})
	secondDelta := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "**Second block**"}
	self.takeTurn(TurnEvent{Update: agent.Update{Delta: &secondDelta}})
	secondBlock := agent.Event{Kind: agent.ModelReasoningEvent, Text: "**Second block**"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &secondBlock}})

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}

	var reasoning []string
	for _, event := range storedSession.Events {
		if event.Kind == agent.ModelReasoningEvent {
			reasoning = append(reasoning, event.Text)
		}
	}
	want := []string{"**First block**", "**Second block**"}
	if !slices.Equal(reasoning, want) {
		t.Errorf("got reasoning blocks %q, want %q", reasoning, want)
	}

	plain := style.Plain(screenOutput.String())
	if !strings.Contains(plain, "First block\nSecond block") {
		t.Errorf("expected separate reasoning lines, got %q", plain)
	}
}

func TestIncompleteReasoningIsErasedBeforeAFailure(t *testing.T) {
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.New(&screenOutput), true)

	painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: "half a thought"})
	painter.DrawEvent(agent.Event{Kind: agent.FailureEvent, Text: "stream failed"})
	painter.End()

	plain := style.Plain(screenOutput.String())
	if strings.Contains(plain, "half a thought") {
		t.Errorf("incomplete reasoning reached scrollback: %q", plain)
	}
	if !strings.Contains(plain, "Stream failed") {
		t.Errorf("expected the failure, got %q", plain)
	}
}

func TestDiscardedReasoningLeavesTheSameScreenAsReplay(t *testing.T) {
	var liveOutput bytes.Buffer
	self := &App{
		screen:   output.NewTerminalOfSize(&liveOutput, 80, 24),
		recorder: record.New(testLog(t)),
	}
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	completeThought := agent.Event{Kind: agent.ModelReasoningEvent, Text: "complete thought"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &completeThought}})
	incompleteThought := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "incomplete thought"}
	self.takeTurn(TurnEvent{Update: agent.Update{Delta: &incompleteThought}})
	failure := agent.Event{Kind: agent.FailureEvent, Text: "stream failed"}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &failure}})
	self.screen.End()

	var replayOutput bytes.Buffer
	replayPainter := newTestPainter(output.NewTerminalOfSize(&replayOutput, 80, 24), false)
	replayPainter.DrawEvent(completeThought)
	replayPainter.DrawEvent(failure)
	replayPainter.End()

	requireSameVisibleScreenInColumns(
		t,
		"discarded reasoning changed the settled screen",
		80,
		liveOutput.String(),
		replayOutput.String(),
	)
}

func TestASuccessfulNoticeUsesTheSuccessStyle(t *testing.T) {
	got := painter.NoticeStyle(agent.SuccessStatus)("Copied to clipboard.")
	want := style.Success("Copied to clipboard.")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoldenFixtureOutputsAreCompleteAndOwned(t *testing.T) {
	expected := map[string]string{}
	claimFixtureOutputs(t, expected, "replay input", "testdata/input", ".jsonl", []string{
		".ansi",
		".screen",
		".transcript",
	})
	claimFixtureOutputs(t, expected, "generated scenario", "testdata/scenarios", ".toml", []string{
		".ansi",
		".jsonl",
		".meta.json",
		".print",
		".requests.jsonl",
		".screen",
		".transcript",
	})
	for name, extensions := range map[string][]string{
		"app-plain-resume":       {".jsonl", ".transcript"},
		"app-plain-turn":         {".jsonl", ".transcript"},
		"authorisation-url":      {".ansi", ".screen"},
		"banner":                 {".ansi", ".screen"},
		"clearing":               {".ansi", ".screen"},
		"completion":             {".txt"},
		"config-reload":          {".ansi", ".screen"},
		"corrupt-session":        {".txt"},
		"default-bar":            {".ansi", ".screen"},
		"feedback":               {".ansi", ".screen", ".txt"},
		"feedback-frame":         {".ansi", ".screen"},
		"fork-message":           {".txt"},
		"context":                {".prompt"},
		"context-drops":          {".prompt"},
		"context-jobs":           {".prompt"},
		"context-network":        {".prompt"},
		"context-network-print":  {".prompt"},
		"context-print":          {".prompt"},
		"context-file-tools":     {".prompt"},
		"context-no-telling":     {".prompt"},
		"context-no-sockets":     {".prompt"},
		"context-no-paths":       {".prompt"},
		"context-path-kinds":     {".prompt"},
		"context-repository":     {".prompt"},
		"context-scratch-root":   {".prompt"},
		"context-simulation":     {".prompt"},
		"context-yolo":           {".prompt"},
		"host-command":           {".ansi", ".screen"},
		"inputblock":             {".ansi", ".screen"},
		"legacy-alt-enter":       {".ansi", ".screen"},
		"lifecycle":              {".ansi", ".screen"},
		"line-resize":            {".screen"},
		"short-terminal":         {".screen"},
		"streaming-modes":        {".screen"},
		"groupings":              {".screen"},
		"reasonings":             {".ansi", ".screen"},
		"mermaid-streaming":      {".screen"},
		"message-marks":          {".screen"},
		"mode-takeback":          {".ansi", ".screen"},
		"model-arguments":        {".txt"},
		"new-session":            {".txt"},
		"ordinary-tab":           {".ansi", ".screen"},
		"path-grant-lifecycle":   {".ansi", ".screen"},
		"port-directions":        {".ansi", ".screen"},
		"path-message":           {".ansi", ".screen"},
		"user-path-links":        {".ansi", ".screen"},
		"workspace-paths":        {".ansi", ".screen"},
		"pending-mode-messages":  {".ansi", ".screen"},
		"pending-notices":        {".ansi", ".screen"},
		"paste":                  {".ansi", ".screen"},
		"pictures":               {".ansi", ".screen"},
		"picker-menu":            {".ansi", ".screen"},
		"plain-input":            {".ansi", ".screen"},
		"print-arguments":        {".txt"},
		"queued-messages":        {".ansi", ".screen"},
		"readline-bindings":      {".ansi", ".screen"},
		"resume-arguments":       {".txt"},
		"resume-model-arguments": {".txt"},
		"resume-mode":            {".ansi"},
		"resume-confinement":     {".ansi"},
		"running":                {".ansi", ".screen"},
		"schedule":               {".ansi", ".screen"},
		"segments":               {".ansi", ".screen"},
		"signal-restoration":     {".ansi"},
		"special-links":          {".ansi", ".screen"},
		"startup":                {".ansi", ".screen"},
		"startup-local-config":   {".ansi", ".screen"},
		"startup-sized":          {".ansi", ".screen"},
		"startup-sized-output":   {".ansi", ".screen"},
		"terminal-escape":        {".ansi", ".screen"},
		"theme-reload":           {".ansi", ".screen"},
		"usage":                  {".json"},
		"usage-arguments":        {".txt"},
		"vertical-movement":      {".ansi", ".screen"},
	} {
		claimFixtureName(t, expected, "special replay", name, extensions)
	}

	outputDirectory := filepath.Join("testdata", "output")
	entries, err := os.ReadDir(outputDirectory)
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("unexpected output directory %s", entry.Name())
			continue
		}
		actual[entry.Name()] = struct{}{}
		if _, exists := expected[entry.Name()]; !exists {
			if *updateGoldens {
				if err := os.Remove(filepath.Join(outputDirectory, entry.Name())); err != nil {
					t.Error(err)
				}
				continue
			}
			t.Errorf("orphaned output %s", entry.Name())
		}
	}

	if *updateGoldens {
		return
	}
	for name, owner := range expected {
		if _, exists := actual[name]; !exists {
			t.Errorf("%s is missing output %s", owner, name)
		}
	}
}

func claimFixtureOutputs(
	t *testing.T,
	expected map[string]string,
	owner string,
	directory string,
	sourceExtension string,
	outputExtensions []string,
) {
	t.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != sourceExtension {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), sourceExtension)
		claimFixtureName(t, expected, owner+" "+entry.Name(), name, outputExtensions)
	}
}

func claimFixtureName(
	t *testing.T,
	expected map[string]string,
	owner string,
	name string,
	extensions []string,
) {
	t.Helper()

	for _, extension := range extensions {
		output := name + extension
		if previousOwner, isDuplicate := expected[output]; isDuplicate {
			t.Errorf("%s and %s both own %s", previousOwner, owner, output)
			continue
		}
		expected[output] = owner
	}
}

func TestFixtureSourceNamesAreUnique(t *testing.T) {
	owners := map[string]string{}
	for directory, extension := range map[string]string{
		"testdata/input":     ".jsonl",
		"testdata/scenarios": ".toml",
	} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != extension {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), extension)
			if previousOwner, isDuplicate := owners[name]; isDuplicate {
				t.Errorf("duplicate fixture name %q in %s and %s", name, previousOwner, directory)
			}
			owners[name] = directory
		}
	}

	if len(owners) == 0 {
		t.Fatal("no fixture sources")
	}
}

const hostAddressPattern = "host address"

var dottedQuadPattern = regexp.MustCompile(`(?:\d{1,3}\.){3}\d{1,3}`)

func isLoopbackAddress(match []byte) bool {
	address, err := netip.ParseAddr(string(dottedQuadPattern.Find(match)))

	return err == nil && address.IsLoopback()
}

func TestTestdataContainsNoPersonalOrSecretMaterial(t *testing.T) {
	patterns := map[string]*regexp.Regexp{
		"absolute home path": regexp.MustCompile(`/(?:home|Users)/[^\s"\\]+`),
		"credential":         regexp.MustCompile(`(?i)(?:authorization|bearer[[:space:]]+[A-Za-z0-9._~+/-]{12,}|access_token|refresh_token|api[_-]?key|sk-[A-Za-z0-9]{12,}|eyJ[A-Za-z0-9_-]{12,}\.)`),
		"email address":      regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`),
		hostAddressPattern:   regexp.MustCompile(`(?:^|[^0-9.])(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?`),
		"UUID":               regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`),
	}

	err := filepath.WalkDir("testdata", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := os.ReadFile(path) //nolint:gosec // the path comes from walking testdata
		if err != nil {
			return err
		}
		for name, pattern := range patterns {
			matches := pattern.FindAll(contents, -1)
			if name == hostAddressPattern {
				matches = slices.DeleteFunc(matches, isLoopbackAddress)
			}
			if len(matches) > 0 {
				t.Errorf("%s contains %s", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMain(testingMain *testing.M) {
	sandbox.Init()
	unsetInheritedStateDirectory()

	status := testingMain.Run()

	if testBinaryDirectory != "" {
		_ = os.RemoveAll(testBinaryDirectory)
	}

	os.Exit(status)
}

func unsetInheritedStateDirectory() {
	if err := os.Unsetenv(location.StateDirVariable); err != nil {
		panic(err)
	}
}

func TestGoldenAResumedConversationDrawsItsRecordedMode(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}

	recordedCaps := caps.Read | caps.Shell | caps.Git
	if err := log.Event(caps.ModeEvent(recordedCaps)); err != nil {
		t.Fatal(err)
	}
	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	restoredCaps, err := sessions.OpeningCaps(caps.Read|caps.Shell|caps.Write, false, storedSession)
	if err != nil {
		t.Fatal(err)
	}

	resumedHarness := &App{mode: caps.NewMode(restoredCaps)}
	modeSegment, err := modeToggle.New(resumedHarness.grantedCaps, resumedHarness.isPrefixPending)(nil)
	if err != nil {
		t.Fatal(err)
	}
	passes := map[string]func() string{
		"recorded rxg rather than default rxw": func() string {
			return modeSegment.Render(segment.Context{})
		},
	}

	compareWithGolden(t, "resume-mode", ".ansi", passes)
}

func TestGoldenAResumedConversationDrawsItsRecordedConfinement(t *testing.T) {
	drawnRules := func(isYolo bool) func() string {
		return func() string {
			directory := t.TempDir()
			log, err := store.Create(directory, store.Meta{Model: "gpt", Yolo: isYolo})
			if err != nil {
				t.Fatal(err)
			}
			if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
				t.Fatal(err)
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}

			storedSession, err := store.Read(directory, log.Name())
			if err != nil {
				t.Fatal(err)
			}

			restoredYolo, err := sessions.OpeningConfinement(false, storedSession)
			if err != nil {
				t.Fatal(err)
			}

			resumedHarness := &App{runMode: runMode{isYolo: restoredYolo}}
			block := input.Block{
				Top:    input.Ruler{Left: "oh"},
				Input:  edit.Frame{Rows: []string{"> carry on"}},
				Bottom: input.Ruler{Right: "rxw ngl"},
				Rule:   resumedHarness.ruleStyle(),
			}
			rows, _, _ := block.Rows(narrowColumns)

			return strings.Join(rows, "\n")
		}
	}

	compareWithGolden(t, "resume-confinement", ".ansi", map[string]func() string{
		"recorded as sandboxed, resumed without the flag": drawnRules(false),
		"recorded as yolo, resumed without the flag":      drawnRules(true),
	})
}

func modeFixture(t *testing.T) (*App, string) {
	t.Helper()

	currentCaps := caps.Read | caps.Write | caps.Shell
	directory := t.TempDir()

	log, err := store.Create(directory, store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	if err := log.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}

	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		recorder: record.New(log),
		screen:   output.New(&bytes.Buffer{}),
		mode:     caps.NewMode(currentCaps),
	}
	self.settleAccess()

	return self, directory
}

func recordedModes(t *testing.T, self *App, directory string) []agent.Event {
	t.Helper()

	storedSession, err := store.Read(directory, self.recorder.Name())
	if err != nil {
		t.Fatal(err)
	}

	var recorded []agent.Event
	for _, event := range storedSession.Events {
		if event.Kind == caps.ModeChange {
			recorded = append(recorded, event)
		}
	}

	return recorded
}

func TestAModeChangeIsWrittenDownOnceItSettles(t *testing.T) {
	self, directory := modeFixture(t)

	self.toggleCap(caps.Git)
	self.settleAccess()

	recorded := recordedModes(t, self, directory)
	if len(recorded) != 2 {
		t.Fatalf("expected the opening mode and the change, got %v", recorded)
	}

	want := caps.Read | caps.Write | caps.Shell | caps.Git
	if got, said := caps.LastRecordedMode(recorded); !said || got != want {
		t.Errorf("expected %s, got %s and %t", want.Flags(), got.Flags(), said)
	}
}

func TestACapabilitySwappedBackIsTakenBackRatherThanWrittenDown(t *testing.T) {
	self, directory := modeFixture(t)

	self.toggleCap(caps.Git)
	if len(self.pendingNotices.items) != 1 {
		t.Fatalf("expected the change to be shown, got %v", self.pendingNotices.items)
	}
	if recorded := recordedModes(t, self, directory); len(recorded) != 1 {
		t.Errorf("pending mode change was written down: %v", recorded)
	}

	self.toggleCap(caps.Git)
	if len(self.pendingNotices.items) != 0 {
		t.Errorf("expected the change to be taken back, got %v", self.pendingNotices.items)
	}

	self.settleAccess()
	if recorded := recordedModes(t, self, directory); len(recorded) != 1 {
		t.Errorf("expected the opening mode alone, got %v", recorded)
	}
}

func TestACapabilitySwappedBackLeavesTheOtherChangesSayingWhatTheySaid(t *testing.T) {
	self, directory := modeFixture(t)

	self.toggleCap(caps.Git)
	self.toggleCap(caps.Write)

	shown, isSaid := caps.ModeNotice(self.pendingNotices.items[1].state)
	if !isSaid {
		t.Fatal("expected the second change to say something")
	}

	self.toggleCap(caps.Git)
	if again, _ := caps.ModeNotice(self.pendingNotices.items[0].state); !slices.Equal(again, shown) {
		t.Errorf("expected %q, got %q", shown, again)
	}

	self.settleAccess()
	want := caps.Read | caps.Shell
	if got, _ := caps.LastRecordedMode(recordedModes(t, self, directory)); got != want {
		t.Errorf("expected %s, got %s", want.Flags(), got.Flags())
	}
}

func TestAnIdleModeMessageJoinsTheNextTurn(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &messageCaptureProvider{}
	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}
	self.settleAccess()

	self.toggleCap(caps.Write)
	modeMessage := workspaceNowReadOnly()
	if !strings.Contains(screenOutput.String(), modeMessage) {
		t.Errorf("pending mode message was not displayed: %q", screenOutput.String())
	}
	if len(provider.messages) != 0 || self.currentTurn.Running() {
		t.Errorf("pending mode message started a turn: messages=%q running=%t", provider.messages, self.currentTurn.Running())
	}

	self.start("next")
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if !slices.Equal(provider.messages, []string{modeMessage, "next"}) {
		t.Errorf("provider received %q", provider.messages)
	}
	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.CanResume() {
		t.Error("combined mode-message turn was not resumable")
	}
}

func TestAModeChangeTheSessionClosesOnIsTakenBack(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", &messageCaptureProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}
	self.settleAccess()

	self.start("first")
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	self.toggleCap(caps.Write)
	if !strings.Contains(screenOutput.String(), workspaceNowReadOnly()) {
		t.Fatalf("the pending mode message was not displayed: %q", screenOutput.String())
	}

	self.dropPendingInput()

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	for _, row := range visibleScreen(t, screenOutput.String(), replayColumns) {
		if strings.Contains(row, workspaceNowReadOnly()) {
			t.Errorf("the taken-back mode message was left on the screen: %q", row)
		}
	}

	storedSession, err := store.Read(directory, log.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !storedSession.CanResume() {
		t.Error("a session closed on a mode change was not resumable")
	}
	if got := len(storedSession.Events); got != 3 {
		t.Errorf("stored %d events, want the mode it opened in, the message it ran, and the silent turn it got back: %v", got, storedSession.Events)
	}
	want := caps.Read | caps.Write
	if got, said := caps.LastRecordedMode(storedSession.Events); !said || got != want {
		t.Errorf("expected the mode the turn ran in, %s, got %s and %t", want.Flags(), got.Flags(), said)
	}
}

func TestAModeChangeSaysItselfInTheScrollback(t *testing.T) {
	var screenOutput bytes.Buffer

	self, _ := modeFixture(t)
	self.screen = output.New(&screenOutput)
	self.toggleCap(caps.Git)
	self.settleAccess()

	if !strings.Contains(screenOutput.String(), ".git is now read-write.") {
		t.Errorf("expected the change to be said, got %q", screenOutput.String())
	}
}

func TestGoldenPendingModeMessagesAreSeparatedFromStartupAndJoinedToEachOther(t *testing.T) {
	requireSameVisibleScreen(
		t,
		"messages a turn has taken differ from independently submitted messages",
		sentModeMessagesStream(t, ""),
		submittedModeMessagesStream(),
	)

	compareWithGolden(t, "pending-mode-messages", ".ansi", map[string]func() string{
		"complete interaction":  func() string { return pendingModeMessagesStream(t, 2) },
		"carried by a new turn": func() string { return sentModeMessagesStream(t, "") },
		"carried by a new turn beside a later line": func() string {
			return sentModeMessagesStream(t, laterHarnessLine)
		},
	})
	compareWithGolden(t, "pending-mode-messages", ".screen", shownPasses(t, map[string]func() string{
		"1 startup":                    func() string { return pendingModeMessagesStream(t, 0) },
		"2 workspace mode message":     func() string { return pendingModeMessagesStream(t, 1) },
		"3 workspace and git messages": func() string { return pendingModeMessagesStream(t, 2) },
		"4 carried by a new turn":      func() string { return sentModeMessagesStream(t, "") },
		"5 carried by a new turn beside a later line": func() string {
			return sentModeMessagesStream(t, laterHarnessLine)
		},
	}))
}

const laterHarnessLine = "Job `build` exited: complete after 30s."

func pendingModeMessagesStream(t *testing.T, toggleCount int) string {
	t.Helper()

	return modeMessagesStream(t, toggleCount, false, "")
}

func sentModeMessagesStream(t *testing.T, laterLine string) string {
	t.Helper()

	return modeMessagesStream(t, 2, true, laterLine)
}

func modeMessagesStream(t *testing.T, toggleCount int, isSent bool, laterLine string) string {
	t.Helper()

	self, _ := modeFixture(t)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	self.screen.Line(startup.RenderBanner(time.Millisecond, false, startup.Info{Session: "tame-impala"}, replayColumns, false))

	if toggleCount > 0 {
		self.toggleCap(caps.Write)
	}
	if toggleCount > 1 {
		self.toggleCap(caps.Git)
	}
	if laterLine != "" {
		self.screen.Line(laterLine)
	}
	if isSent {
		self.settleAccess()
	}

	return screenOutput.String()
}

func submittedModeMessagesStream() string {
	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	screen.Line(startup.RenderBanner(time.Millisecond, false, startup.Info{Session: "tame-impala"}, replayColumns, false))
	picasso := painter.New(screen, false, nil, nil, defaultStreamingMode)
	picasso.DrawEvent(caps.ModeToggleEvent(caps.Write, caps.Read|caps.Shell|caps.Git))
	picasso.DrawEvent(caps.ModeToggleEvent(caps.Git, caps.Read|caps.Shell|caps.Git))
	return screenOutput.String()
}

func TestGoldenTakingBackAModeChangeDrawsWhatItDrewBefore(t *testing.T) {
	completeInteraction := func(letter rune, laterLine string) func() string {
		return func() string {
			stream := modeTakebackStream(t, letter, 2, laterLine)
			if strings.Contains(stream, "\x1b[H\x1b[2J") {
				t.Error("taking the mode change back cleared the screen")
			}
			return stream
		}
	}

	compareWithGolden(t, "mode-takeback", ".ansi", map[string]func() string{
		"lookup complete interaction":     completeInteraction('l', ""),
		"lookup taken back beside a line": completeInteraction('l', laterHarnessLine),
		"network complete interaction":    completeInteraction('n', ""),
		"one of several taken back":       func() string { return modeTakebackFromRunStream(t, 4) },
	})
	compareWithGolden(t, "mode-takeback", ".screen", shownPasses(t, map[string]func() string{
		"11 toggled once past a stopped job":   func() string { return modeTakebackPastAStoppedJob(t, 1) },
		"12 toggled twice past a stopped job":  func() string { return modeTakebackPastAStoppedJob(t, 2) },
		"13 toggled thrice past a stopped job": func() string { return modeTakebackPastAStoppedJob(t, 3) },
		"14 toggled four times past a stopped job": func() string {
			return modeTakebackPastAStoppedJob(t, 4)
		},
		"15 toggled past a job another capability stopped": func() string {
			return modeTakebackPastAJobAnotherCapabilityStopped(t)
		},
		"1 before either chord":  func() string { return modeTakebackStream(t, 'l', 0, "") },
		"2 after ctrl+x l":       func() string { return modeTakebackStream(t, 'l', 1, "") },
		"3 after ctrl+x l twice": func() string { return modeTakebackStream(t, 'l', 2, "") },
		"4 after ctrl+x n":       func() string { return modeTakebackStream(t, 'n', 1, "") },
		"5 after ctrl+x n twice": func() string { return modeTakebackStream(t, 'n', 2, "") },
		"6 taken back beside a later line": func() string {
			return modeTakebackStream(t, 'l', 2, laterHarnessLine)
		},
		"7 one notice standing":            func() string { return modeTakebackFromRunStream(t, 1) },
		"8 three notices standing":         func() string { return modeTakebackFromRunStream(t, 3) },
		"9 the middle notice taken back":   func() string { return modeTakebackFromRunStream(t, 4) },
		"10 the run carried by a new turn": func() string { return modeTakebackFromRunStream(t, 5) },
	}))
}

func modeTakebackPastAStoppedJob(t *testing.T, toggleCount int) string {
	t.Helper()

	var screenOutput strings.Builder
	self := &App{
		screen: output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		mode:   caps.NewMode(caps.Read),
	}
	self.pendingNotices.add(caps.ModeToggleEvent(caps.Write, caps.Read))
	self.pendingNotices.add(caps.JobStopEvent("web", caps.Write))
	self.refreshPendingMessages()

	for range toggleCount {
		self.toggleCap(caps.Write)
	}
	self.screen.End()

	return screenOutput.String()
}

func modeTakebackPastAJobAnotherCapabilityStopped(t *testing.T) string {
	t.Helper()

	var screenOutput strings.Builder
	self := &App{
		screen: output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		mode:   caps.NewMode(caps.Read | caps.Write | caps.Shell),
	}

	self.toggleCap(caps.Write)
	self.pendingNotices.add(caps.JobStopEvent("web", caps.Shell))
	self.toggleCap(caps.Write)
	self.screen.End()

	return screenOutput.String()
}

func modeTakebackFromRunStream(t *testing.T, stepCount int) string {
	t.Helper()

	self, _ := modeFixture(t)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine

	self.screen.Line("conversation remains in scrollback")
	self.show(inputLine)

	steps := []func(){
		func() { self.toggleCap(caps.Write) },
		func() { self.toggleCap(caps.Lookup) },
		func() { self.toggleCap(caps.Git) },
		func() { self.toggleCap(caps.Lookup) },
		func() { self.settleAccess() },
	}

	for _, step := range steps[:stepCount] {
		step()
		self.show(inputLine)
	}

	return screenOutput.String()
}

func modeTakebackStream(t *testing.T, letter rune, toggleCount int, laterLine string) string {
	t.Helper()

	self, _ := modeFixture(t)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine

	self.screen.Line("conversation remains in scrollback")
	self.show(inputLine)

	for chord := range toggleCount {
		if chord > 0 && laterLine != "" {
			self.screen.Line(laterLine)
		}
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Rune, Value: 'x', Mod: key.Ctrl})
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Rune, Value: letter})
	}

	return screenOutput.String()
}

func TestGoldenMermaidStreamingDrawsWhatItDrewBefore(t *testing.T) {
	compareWithGolden(t, "mermaid-streaming", ".screen", map[string]func() string{
		"1 first valid prefix": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B")
		},
		"2 tilde-fenced prefix": func() string {
			return mermaidStreamingScreen(t, "~~~mermaid\ngraph LR\nA --> B")
		},
		"3 completed fence": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B\n```")
		},
		"4 quoted prefix": func() string {
			return mermaidStreamingScreen(t, "> ```mermaid\n> graph LR\n> A --> B")
		},
		"5 listed prefix": func() string {
			return mermaidStreamingScreen(t, "- ```mermaid\n  graph LR\n  A --> B")
		},
		"6 invalid extension": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B", "\nB -->")
		},
		"7 invalid after redraw": func() string {
			return mermaidStreamingRedrawnScreen(t)
		},
		"8 next valid prefix": func() string {
			return mermaidStreamingScreen(t, "```mermaid\ngraph LR\nA --> B", "\nB -->", " C")
		},
		"9 completed invalid diagram": func() string {
			return completedInvalidMermaidScreen(t)
		},
		"10 outgrowing the terminal": func() string {
			return mermaidStreamingScreen(
				t,
				"```mermaid\nflowchart TD\nroot[\"Session\"] --> replay[\"Replay the journal\"]",
				"\nroot --> stream[\"Stream the turn\"]",
				"\nroot --> seal[\"Seal into scrollback\"]",
				"\nroot --> notice[\"Draw the harness notice\"]",
			)
		},
	})
}

const streamedAnswerTail = "pika."

func streamedAnswerText() string {
	var text strings.Builder
	for text.Len() < 900 {
		text.WriteString("A sentence of a model answer that goes on long enough to back the redrawing off. ")
	}
	text.WriteString("and this is the very last word: " + streamedAnswerTail)

	return text.String()
}

func streamText(painter *Painter, kind agent.Kind) {
	text := streamedAnswerText()
	for at := 0; at < len(text); at += 5 {
		painter.DrawDelta(agent.Delta{Kind: kind, Text: text[at:min(at+5, len(text))]})
	}
}

func interruptedStreamScreen(t *testing.T, kind agent.Kind, streamingMode output.StreamingMode) string {
	t.Helper()

	var screenOutput bytes.Buffer
	painter := newStreamedTestPainter(output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true, streamingMode)
	streamText(painter, kind)
	painter.Close(dynamic.Cancelled)

	return shown(t, screenOutput.String(), replayColumns)
}

func streamThenNoticeScreen(t *testing.T, streamingMode output.StreamingMode) string {
	t.Helper()

	var screenOutput bytes.Buffer
	painter := newStreamedTestPainter(output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true, streamingMode)
	streamText(painter, agent.ModelMessageEvent)
	painter.DrawEvent(agent.Event{Kind: agent.SilentTurnEvent})

	return shown(t, screenOutput.String(), replayColumns)
}

func everyStreamingMode() map[string]output.StreamingMode {
	return map[string]output.StreamingMode{
		"asap":  output.StreamingModeASAP,
		"line":  output.StreamingModeLine,
		"paced": output.StreamingModePaced,
	}
}

func TestGoldenLineStreamingDrawsEveryFrameAcrossANarrowerResize(t *testing.T) {
	compareWithGolden(t, "line-resize", ".screen", map[string]func() string{
		"wide to narrow": func() string { return lineResizeFrames(t) },
	})
}

func lineResizeFrames(t *testing.T) string {
	t.Helper()

	const wideColumns = 36
	const narrowColumns = 18
	const initial = "one two three four five six seven eight nine ten eleven twelve thirteen"

	var screenOutput bytes.Buffer
	wideScreen := output.NewTerminalOfSize(&screenOutput, wideColumns, replayLines)
	widePainter := newStreamedTestPainter(wideScreen, true, output.StreamingModeLine)
	widePainter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: initial})
	wideRows := visibleScreen(t, screenOutput.String(), wideColumns)

	var frames strings.Builder
	fmt.Fprintf(&frames, "--- before resize at %d columns ---\n%s\n", wideColumns, strings.Join(wideRows, "\n"))

	provisional := widePainter.ProvisionalDelta()
	narrowScreen := output.NewTerminalOfSize(&screenOutput, narrowColumns, replayLines)
	narrowScreen.Reset()
	narrowPainter := newStreamedTestPainter(narrowScreen, true, output.StreamingModeLine)
	narrowPainter.DrawRestoredDelta(provisional, widePainter)
	narrowRows := visibleScreen(t, screenOutput.String(), narrowColumns)
	fmt.Fprintf(&frames, "--- resize frame at %d columns ---\n%s\n", narrowColumns, strings.Join(narrowRows, "\n"))

	if len(narrowRows) < len(wideRows)+2 {
		t.Errorf("expected narrowing to reveal several newly complete rows, wide=%q narrow=%q", wideRows, narrowRows)
	}

	previousRows := narrowRows
	for _, delta := range []string{" fourteen", " fifteen", " sixteen", " seventeen", " eighteen", " nineteen", " twenty"} {
		narrowPainter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
		rows := visibleScreen(t, screenOutput.String(), narrowColumns)
		if slices.Equal(rows, previousRows) {
			continue
		}
		if len(rows) != len(previousRows)+1 {
			t.Errorf("delta %q revealed %d rows, want one: before=%q after=%q", delta, len(rows)-len(previousRows), previousRows, rows)
		}
		fmt.Fprintf(&frames, "--- after%q ---\n%s\n", delta, strings.Join(rows, "\n"))
		previousRows = rows
	}

	return strings.TrimSuffix(frames.String(), "\n")
}

func TestGoldenAStreamThatStopsStillShowsEverythingThatArrived(t *testing.T) {
	passes := map[string]func() string{}

	for name, streamingMode := range everyStreamingMode() {
		passes[name+" answer closed"] = func() string {
			return interruptedStreamScreen(t, agent.ModelMessageEvent, streamingMode)
		}
		passes[name+" answer then a notice"] = func() string {
			return streamThenNoticeScreen(t, streamingMode)
		}
		passes[name+" reasoning closed"] = func() string {
			return interruptedStreamScreen(t, agent.ModelReasoningEvent, streamingMode)
		}
	}

	compareWithGolden(t, "streaming-modes", ".screen", passes)
}

func TestNoStreamedAnswerIsLeftOffTheScreenWhenTheStreamStops(t *testing.T) {
	for name, streamingMode := range everyStreamingMode() {
		for conclusion, drawn := range map[string]func() string{
			"closed":        func() string { return interruptedStreamScreen(t, agent.ModelMessageEvent, streamingMode) },
			"then a notice": func() string { return streamThenNoticeScreen(t, streamingMode) },
		} {
			if !strings.Contains(drawn(), streamedAnswerTail) {
				t.Errorf("a %s answer %s dropped the end of what arrived:\n%s", name, conclusion, drawn())
			}
		}
	}
}

func streamedAnswerScreen(t *testing.T, streamingMode output.StreamingMode, deltas ...string) string {
	t.Helper()

	var screenOutput bytes.Buffer
	painter := newStreamedTestPainter(
		output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true, streamingMode,
	)

	for _, delta := range deltas {
		painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
	}

	return shown(t, screenOutput.String(), replayColumns)
}

func TestAnAnswerThatShowsEverythingIsOnScreenBeforeAnythingSettlesIt(t *testing.T) {
	for name, streamingMode := range map[string]output.StreamingMode{
		"asap":  output.StreamingModeASAP,
		"paced": output.StreamingModePaced,
	} {
		if drawn := streamedAnswerScreen(t, streamingMode, "still ", "working"); !strings.Contains(drawn, "still working") {
			t.Errorf("a %s answer was not on screen while it streamed, got:\n%s", name, drawn)
		}
	}
}

func TestALineIsOnScreenOnceTheOneAfterItHasBegun(t *testing.T) {
	drawn := streamedAnswerScreen(t, output.StreamingModeLine, "still ", "working", "\n\nand ", "more")

	if !strings.Contains(drawn, "still working") {
		t.Errorf("expected a settled line to be shown, got:\n%s", drawn)
	}
	if strings.Contains(drawn, "and more") {
		t.Errorf("expected the line still arriving to be held back, got:\n%s", drawn)
	}
}

func TestALineIsOnScreenAsSoonAsItEnds(t *testing.T) {
	drawn := streamedAnswerScreen(t, output.StreamingModeLine, "still ", "working", "\n")

	if !strings.Contains(drawn, "still working") {
		t.Errorf("expected a line to be shown the moment it ended, got:\n%s", drawn)
	}
}

func TestAWrappedLineIsShownAsSoonAsTheNextOneBegins(t *testing.T) {
	for name, kind := range map[string]agent.Kind{
		"answer":    agent.ModelMessageEvent,
		"reasoning": agent.ModelReasoningEvent,
	} {
		var screenOutput bytes.Buffer
		painter := newStreamedTestPainter(output.NewTerminalOfSize(&screenOutput, 12, replayLines), true, output.StreamingModeLine)
		painter.DrawDelta(agent.Delta{Kind: kind, Text: "one two thre"})
		painter.DrawDelta(agent.Delta{Kind: kind, Text: "e"})
		drawn := shown(t, screenOutput.String(), 12)

		if !strings.Contains(drawn, "one two") {
			t.Errorf("expected the complete %s line to be shown, got:\n%s", name, drawn)
		}
		if strings.Contains(drawn, "three") {
			t.Errorf("expected the incomplete %s line to be held back, got:\n%s", name, drawn)
		}
	}
}

func TestCodeIsShownLineByLineWhileADiagramIsShownWhole(t *testing.T) {
	code := streamedAnswerScreen(t, output.StreamingModeLine, "```go\n", "func main() {}\n", "\tmore")
	if !strings.Contains(code, "func main() {}") {
		t.Errorf("expected a settled line of code to be shown, got:\n%s", code)
	}

	diagram := streamedAnswerScreen(t, output.StreamingModeLine, "```mermaid\n", "graph LR\n", "A --> B")
	if !strings.Contains(diagram, "└") {
		t.Errorf("expected a diagram to keep its closing edge, got:\n%s", diagram)
	}
}

func TestATableKeepsItsShapeUntilARowIsWhole(t *testing.T) {
	table := []string{"| tool | cost |\n", "|------|------|\n", "| read | 1 |\n"}
	drawn := streamedAnswerScreen(t, output.StreamingModeLine, append(table, "| gr", "ep | 4")...)

	if !strings.Contains(drawn, "│ read │ 1    │") {
		t.Errorf("expected a settled table row to be shown, got:\n%s", drawn)
	}
	if strings.Contains(drawn, "grep") || strings.Contains(drawn, "│ gr") {
		t.Errorf("expected the row still arriving to be held back, got:\n%s", drawn)
	}
	if !strings.Contains(drawn, "└") {
		t.Errorf("expected the table to keep its closing edge, got:\n%s", drawn)
	}
}

func TestATableRowIsOnScreenAsSoonAsItEnds(t *testing.T) {
	table := []string{"| tool | cost |\n", "|------|------|\n", "| read | 1 |\n"}
	drawn := streamedAnswerScreen(t, output.StreamingModeLine, append(table, "| grep", " | 4 |\n")...)

	if !strings.Contains(drawn, "│ grep │ 4    │") {
		t.Errorf("expected a table row to be shown the moment it ended, got:\n%s", drawn)
	}
}

func TestALineOpeningWithAPipeOutsideATableIsHeldBackAsProse(t *testing.T) {
	drawn := streamedAnswerScreen(t, output.StreamingModeLine, "one two three\n", "| four ", "five")

	if !strings.Contains(drawn, "one two three") {
		t.Errorf("expected the settled line to be shown, got:\n%s", drawn)
	}
	if strings.Contains(drawn, "four") {
		t.Errorf("expected the line still arriving to be held back, got:\n%s", drawn)
	}
}

func TestALineStillArrivingIsHeldBackUntilSomethingSettlesIt(t *testing.T) {
	if drawn := streamedAnswerScreen(t, output.StreamingModeLine, "still ", "working"); strings.Contains(drawn, "still working") {
		t.Errorf("expected the only line to be held back while it arrived, got:\n%s", drawn)
	}
}

func TestAProvisionalThoughtIsTakenBackWhateverWasDrawnOfIt(t *testing.T) {
	for name, streamingMode := range everyStreamingMode() {
		if drawn := interruptedStreamScreen(t, agent.ModelReasoningEvent, streamingMode); strings.Contains(drawn, "sentence") {
			t.Errorf("expected a %s thought to be taken back, got:\n%s", name, drawn)
		}
	}
}

func mermaidStreamingScreen(t *testing.T, deltas ...string) string {
	t.Helper()
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true)
	for _, delta := range deltas {
		painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
	}
	return shown(t, screenOutput.String(), replayColumns)
}

func mermaidStreamingRedrawnScreen(t *testing.T) string {
	t.Helper()
	var screenOutput bytes.Buffer
	testConversation := &App{screen: output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)}
	testConversation.currentTurn = Turn{Stream: testRunningTurnStream(), painter: testConversation.newPainter(true)}
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	testConversation.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	testConversation.redraw()
	return shown(t, screenOutput.String(), replayColumns)
}

func completedInvalidMermaidScreen(t *testing.T) string {
	t.Helper()
	const invalid = "```mermaid\ngraph LR\nA --> B\nB -->\n```"
	var screenOutput bytes.Buffer
	painter := newTestPainter(output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines), true)
	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "```mermaid\ngraph LR\nA --> B"})
	painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nB -->"})
	painter.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: invalid})
	return shown(t, screenOutput.String(), replayColumns)
}

func checkedModelCache(providers string) []byte {
	return fmt.Appendf(
		nil,
		`{"version":5,"checked":%q,"providers":%s}`, time.Now().Format(time.RFC3339), providers,
	)
}

var testBinaryDirectory string

var testBinary = sync.OnceValues(func() (string, error) {
	directory, err := os.MkdirTemp("", "oh-binary")
	if err != nil {
		return "", err
	}

	testBinaryDirectory = directory
	binary := filepath.Join(directory, "oh")

	command := exec.Command("go", "build", "-o", binary, "crdx.org/oh") //nolint:gosec // building the binary under test
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build oh: %w\n%s", err, output)
	}

	return binary, nil
})

func buildTestBinary(t *testing.T) string {
	t.Helper()

	binary, err := testBinary()
	if err != nil {
		t.Fatal(err)
	}

	return binary
}

func runTestBinary(t *testing.T, binary string, workspaceDir string, environment []string, arguments ...string) string {
	t.Helper()

	command := exec.CommandContext(t.Context(), binary, arguments...) //nolint:gosec // running the binary under test
	command.Env = environment
	command.Dir = workspaceDir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("oh %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func TestTheToolResultFlagUsesTheMainBinary(t *testing.T) {
	stateDirectory := t.TempDir()
	writer, err := session.Create(filepath.Join(stateDirectory, "sessions"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{
		Kind:      agent.ToolCallRequestEvent,
		ID:        "call-1",
		Name:      "read",
		Arguments: `{"path":"notes.txt"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{
		Kind:   agent.ToolCallResultEvent,
		ID:     "call-1",
		Name:   "read",
		Status: agent.SuccessStatus,
		Text:   "one\ntwo\n",
	}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	binary := buildTestBinary(t)
	environment := append(testBinaryEnvironment(t, t.TempDir()), location.StateDirVariable+"="+stateDirectory)
	result := runTestBinary(
		t,
		binary,
		t.TempDir(),
		environment,
		"--tool-result",
		toolresult.URL(writer.Name(), "call-1"),
	)

	if result != "read · notes.txt\n\none\ntwo\n" {
		t.Errorf("tool result is %q", result)
	}
}

const reachableWorkspaceDirPrefix = "io-oh-test-"

func reachableWorkspaceDir(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	if _, isCovered := pathutil.RelativeTo(sandbox.TmpDir, directory); !isCovered {
		return directory
	}

	base, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("a scratch at %s covers %s and there is nowhere else to work: %v", sandbox.TmpDir, directory, err)
	}
	if _, isCovered := pathutil.RelativeTo(sandbox.TmpDir, base); isCovered {
		t.Skipf("a scratch at %s covers both %s and %s", sandbox.TmpDir, directory, base)
	}

	created, err := os.MkdirTemp(base, reachableWorkspaceDirPrefix) //nolint:usetesting // the scratch covers what t.TempDir gives
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(created); err != nil {
			t.Error(err)
		}
	})

	return created
}

const earlyExitDeadline = 10 * time.Second

var usageOption = regexp.MustCompile(`(?m)^\s+(?:-[A-Za-z], )?(--[a-z-]+)`)

var optionsThatExitEarly = map[string][]string{
	"--ctl":     {"--ctl"},
	"--login":   {"-L", "nobody"},
	"--usage":   {"-U", "-J"},
	"--list":    {"-l"},
	"--update":  {"-u"},
	"--ignored": {"-u", "-I"},
	"--version": {"-v"},
	"--help":    {"-h"},
}

var optionsThatOpenASession = []string{
	"--resume",
	"--model",
	"--caps",
	"--tool",
	"--print",
	"--demo",
	"--yolo",
	"--json",
}

func TestOnlyASessionThatOutlivesTheRunSaysHowToResume(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		isPersisted bool
		isSimulated bool
		kind        cycle.TransitionKind
		wants       bool
	}{
		{name: "a stored session that quit", isPersisted: true, kind: cycle.Quit, wants: true},
		{name: "a session that was never stored", kind: cycle.Quit},
		{name: "a simulated session", isPersisted: true, isSimulated: true, kind: cycle.Quit},
		{name: "a session that gave way to another", isPersisted: true, kind: cycle.NewSession},
		{name: "a session that restarted", isPersisted: true, kind: cycle.Restart},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := isSessionLeftToResume(testCase.isPersisted, testCase.isSimulated, testCase.kind)
			if got != testCase.wants {
				t.Errorf("said %v, want %v", got, testCase.wants)
			}
		})
	}
}

func TestFlagsThatExitEarlyNeverWaitOnStdin(t *testing.T) {
	t.Parallel()

	binary := buildTestBinary(t)

	requireEveryOptionSaysWhetherItExits(t, binary)

	for name, arguments := range optionsThatExitEarly {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = reader.Close()
				_ = writer.Close()
			})

			context, cancel := context.WithTimeout(t.Context(), earlyExitDeadline)
			defer cancel()

			command := exec.CommandContext(context, binary, arguments...) //nolint:gosec // running the binary under test
			command.Env = testBinaryEnvironment(t, t.TempDir())
			command.Stdin = reader

			output, err := command.CombinedOutput()

			if context.Err() != nil {
				t.Fatalf(
					"oh %s waited on a stdin nobody was going to close, having drawn %q",
					strings.Join(arguments, " "), string(output),
				)
			}
			if err != nil {
				if _, isExit := errors.AsType[*exec.ExitError](err); !isExit {
					t.Fatalf("oh %s: %v\n%s", strings.Join(arguments, " "), err, output)
				}
			}
		})
	}
}

func requireEveryOptionSaysWhetherItExits(t *testing.T, binary string) {
	t.Helper()

	command := exec.CommandContext(t.Context(), binary, "-h")
	command.Env = testBinaryEnvironment(t, t.TempDir())
	help, _ := command.CombinedOutput()

	matches := usageOption.FindAllStringSubmatch(string(help), -1)
	if len(matches) == 0 {
		t.Fatalf("no options were named, in:\n%s", help)
	}

	for _, match := range matches {
		option := match[1]
		if _, doesExit := optionsThatExitEarly[option]; doesExit {
			continue
		}
		if slices.Contains(optionsThatOpenASession, option) {
			continue
		}

		t.Errorf(
			"%s is neither known to exit early nor known to open a session, "+
				"so nothing says whether it may wait on stdin",
			option,
		)
	}
}

func testBinaryEnvironment(t *testing.T, stateDirectory string) []string {
	t.Helper()

	return append(
		os.Environ(),
		"HOME="+t.TempDir(),
		"XDG_CONFIG_HOME="+t.TempDir(),
		"XDG_STATE_HOME="+stateDirectory,
	)
}

func TestGoldenCorruptedSessionFailureMatchesGolden(t *testing.T) {
	binary := buildTestBinary(t)
	compareWithGolden(t, "corrupt-session", ".txt", map[string]func() string{
		"record split across lines": func() string {
			return corruptedSessionFailure(t, binary, "{\"kind\":\"event\",\n\"event\":{}}\n")
		},
		"records joined on one line": func() string {
			return corruptedSessionFailure(t, binary, "{\"kind\":\"event\"}{\"kind\":\"event\"}\n")
		},
		"terminated malformed record": func() string {
			return corruptedSessionFailure(t, binary, "not-json\n")
		},
	})
}

func corruptedSessionFailure(t *testing.T, binary string, suffix string) string {
	t.Helper()

	stateDirectory := t.TempDir()
	configDirectory := t.TempDir()
	applicationStateDirectory := filepath.Join(stateDirectory, "org.crdx", "oh")

	configPath := filepath.Join(configDirectory, "org.crdx", "oh", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, fmt.Appendf(nil, "version = %d\n", config.Format), 0o600); err != nil {
		t.Fatal(err)
	}

	modelCachePath := filepath.Join(applicationStateDirectory, "models.json")
	if err := os.MkdirAll(filepath.Dir(modelCachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	modelCache := checkedModelCache(`{"codex":{"models":[{"id":"gpt-5.6-sol","efforts":["high"],"output":128000}]}}`)
	if err := os.WriteFile(modelCachePath, modelCache, 0o600); err != nil {
		t.Fatal(err)
	}

	sessionsDirectory := filepath.Join(applicationStateDirectory, "sessions")
	writer, err := store.Create(sessionsDirectory, store.Meta{
		Model:        "gpt-5.6-sol",
		WorkspaceDir: "/",
		Provider:     "codex",
		Effort:       "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "keep this message"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.CompleteTurn(session.TurnSummary{}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	journalPath := filepath.Join(sessionsDirectory, writer.Name(), "session.jsonl")
	journal, err := os.OpenFile(journalPath, os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // test-owned path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.WriteString(suffix); err != nil {
		_ = journal.Close()
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	command := exec.CommandContext(t.Context(), binary, "-r", writer.Name()) //nolint:gosec // running the binary under test
	command.Dir = "/"
	command.Env = append(
		os.Environ(),
		"HOME="+t.TempDir(),
		"XDG_CONFIG_HOME="+configDirectory,
		"XDG_STATE_HOME="+stateDirectory,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("corrupted session unexpectedly opened: %s", output)
	}

	return strings.ReplaceAll(string(output), writer.Name(), "tame-impala")
}

func TestVersionDispatchRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	stateDirectory := t.TempDir()

	output := runTestBinary(t, binary, reachableWorkspaceDir(t), testBinaryEnvironment(t, stateDirectory), "--version")
	if strings.TrimSpace(output) == "" || strings.Count(output, "\n") != 1 {
		t.Errorf("got %q, want one non-empty line", output)
	}
}

func TestModelListDispatchRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	stateDirectory := t.TempDir()
	cachePath := filepath.Join(stateDirectory, "org.crdx", "oh", "models.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	cache := checkedModelCache(`{
		"codex":{"models":[{"id":"gpt-cli","efforts":["high"],"output":128000}]},
		"ollama":{"models":[{"id":"local-cli","efforts":["high"],"output":128000}]}
	}`)
	if err := os.WriteFile(cachePath, cache, 0o600); err != nil {
		t.Fatal(err)
	}

	output := runTestBinary(t, binary, reachableWorkspaceDir(t), testBinaryEnvironment(t, stateDirectory), "-l")
	if output != "ollama/local-cli\n" {
		t.Errorf("got %q", output)
	}
}

func TestModelUpdateDispatchRunsThroughTheBinary(t *testing.T) {
	binary := buildTestBinary(t)
	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Hello."}}})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]
	stateDirectory := t.TempDir()
	environment := append(testBinaryEnvironment(t, stateDirectory), backend.EndpointVariable+"="+address)

	workspaceDir := reachableWorkspaceDir(t)
	updated := runTestBinary(t, binary, workspaceDir, environment, "-u")
	if !strings.Contains(updated, "Stored model list") {
		t.Errorf("update output did not report storage: %q", updated)
	}

	listed := runTestBinary(t, binary, workspaceDir, environment, "-l")
	for _, providerName := range model.ProviderNames() {
		if !strings.Contains(listed, providerName+"/fake") {
			t.Errorf("listing omitted %s: %q", providerName, listed)
		}
	}
}

func TestOpenCodeRequestsUseTheStoredSessionIdentifier(t *testing.T) {
	binary := buildTestBinary(t)
	endpoint := sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Say: "First answer."},
			{Say: "Second answer."},
		},
	})

	var headerMutex sync.Mutex
	var sessionHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if sessionID := request.Header.Get("X-Opencode-Session"); sessionID != "" {
			headerMutex.Lock()
			sessionHeaders = append(sessionHeaders, sessionID)
			headerMutex.Unlock()
		}
		endpoint.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Completions]
	stateDirectory := t.TempDir()
	workspaceDir := reachableWorkspaceDir(t)
	environment := append(testBinaryEnvironment(t, stateDirectory), backend.EndpointVariable+"="+address)
	runTestBinary(t, binary, workspaceDir, environment, "-p", "--yolo", "-m", "opencode-go/fake", "first question")

	sessionsDirectory := filepath.Join(stateDirectory, "org.crdx", "oh", "sessions")
	storedSessions, err := store.List(sessionsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one", len(storedSessions))
	}
	storedSession := storedSessions[0]

	runTestBinary(t, binary, workspaceDir, environment, "-p", "-r", storedSession.Name, "second question")

	headerMutex.Lock()
	capturedHeaders := slices.Clone(sessionHeaders)
	headerMutex.Unlock()
	if !slices.Equal(capturedHeaders, []string{storedSession.ID, storedSession.ID}) {
		t.Errorf("got OpenCode session headers %q, want the stored ID %q twice", capturedHeaders, storedSession.ID)
	}
	if slices.Contains(capturedHeaders, storedSession.Name) {
		t.Errorf("sent the human-readable session name %q", storedSession.Name)
	}
}

func TestASessionResumesWithTheModelItWasCreatedWithAfterTheModelListForgetsIt(t *testing.T) {
	binary := buildTestBinary(t)
	endpoint := sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Say: "First answer."},
			{Say: "Second answer."},
		},
	})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Completions]
	stateDirectory := t.TempDir()
	workspaceDir := reachableWorkspaceDir(t)
	environment := append(testBinaryEnvironment(t, stateDirectory), backend.EndpointVariable+"="+address)
	runTestBinary(t, binary, workspaceDir, environment, "-p", "--yolo", "-m", "opencode-go/fake", "first question")

	sessionsDirectory := filepath.Join(stateDirectory, "org.crdx", "oh", "sessions")
	storedSessions, err := store.List(sessionsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one", len(storedSessions))
	}
	storedSession := storedSessions[0]

	frozenChoice := storedSession.Meta.ModelChoice
	if frozenChoice == nil || frozenChoice.Provider != model.OpencodeGoProvider || frozenChoice.ID != "fake" {
		t.Fatalf("expected the session to hold the model it was created with, got %+v", frozenChoice)
	}

	cachePath := filepath.Join(stateDirectory, "org.crdx", "oh", "models.json")
	successorCache := checkedModelCache(`{"opencode-go":{"models":[{"id":"fake-2","efforts":["high"],"output":128000}]}}`)
	if err := os.WriteFile(cachePath, successorCache, 0o600); err != nil {
		t.Fatal(err)
	}

	resumed := runTestBinary(t, binary, workspaceDir, environment, "-p", "-r", storedSession.Name, "second question")
	if !strings.Contains(resumed, "Second answer.") {
		t.Errorf("expected the session to resume on its own model, got %q", resumed)
	}
}

func TestForkingAStoredSessionOpensANewOneCarryingItsTranscript(t *testing.T) {
	binary := buildTestBinary(t)
	endpoint := sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Say: "First answer."},
			{Say: "Second answer."},
		},
	})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Completions]
	stateDirectory := t.TempDir()
	workspaceDir := reachableWorkspaceDir(t)
	environment := append(testBinaryEnvironment(t, stateDirectory), backend.EndpointVariable+"="+address)
	runTestBinary(t, binary, workspaceDir, environment, "-p", "--yolo", "-m", "opencode-go/fake", "first question")

	sessionsDirectory := filepath.Join(stateDirectory, "org.crdx", "oh", "sessions")
	storedSessions, err := store.List(sessionsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSessions) != 1 {
		t.Fatalf("got %d stored sessions, want one", len(storedSessions))
	}
	sourceName := storedSessions[0].Name

	runTestBinary(
		t, binary, workspaceDir, environment,
		"-p", "--yolo", "-m", "opencode-go/fake", "--from", sourceName, "carry on",
	)

	storedSessions, err = store.List(sessionsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSessions) != 2 {
		t.Fatalf("got %d stored sessions, want the source and the fork", len(storedSessions))
	}

	forkName := storedSessions[0].Name
	if forkName == sourceName {
		forkName = storedSessions[1].Name
	}
	transcript := filepath.Join(sessionsDirectory, forkName, "drops", sourceName+".chat.md")
	if _, err := os.Stat(transcript); err != nil {
		t.Errorf("the forked transcript is missing: %v", err)
	}
}

func TestAnEffortWrittenAsAnAliasInTheConfigIsResolved(t *testing.T) {
	modelCachePath := useRoundRobinModelCache(t)
	selections, err := model.ParseRoundRobin(modelCachePath, []string{"sol@off"}, model.Defaults{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if effort := selections[0].Effort; effort != "none" {
		t.Errorf("expected the level rather than the word that asked for it, got %q", effort)
	}
}

func TestGoldenWhatSessionTransitionsMakeOfAModelGlob(t *testing.T) {
	compareWithGolden(t, "new-session", ".txt", map[string]func() string{
		"command line selections": func() string { return resolveCommandLineSelections(t) },
		"effort autoselection":    resolveAutomaticEfforts,
		"fork model globs":        func() string { return resolveForkedSessionGlobs(t) },
		"new session model globs": func() string { return resolveNewSessionGlobs(t) },
	})
}

func useCommandLineModelCache(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := location.GetModelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	data := checkedModelCache(`{` +
		`"codex":{"models":[{"id":"gpt-5.6-sol","efforts":["none","high"],"output":128000},` +
		`{"id":"gpt-5.6-mystery","efforts":["whatever"],"output":128000}]},` +
		`"opencode-go":{"models":[{"id":"deepseek-v4-pro","efforts":["medium"],"output":128000}]},` +
		`"anthropic":{"models":[` +
		`{"id":"claude-opus-5","efforts":["medium","max"],"output":128000},` +
		`{"id":"claude-sonnet-5","efforts":["low","high"],"output":128000}` +
		`]}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func resolveCommandLineSelections(t *testing.T) string {
	t.Helper()

	path := useCommandLineModelCache(t)
	var written strings.Builder

	for _, selection := range []string{
		"opus-5",
		"opus5",
		"sonnet",
		"sonnet5",
		"sol",
		"deepseek",
		"anthropic/claude-opus-5",
		"opencode/deepseek",
		"anthropic",
		"codex",
		"opus-5@max",
		"sol@off",
		"sol@high+fast",
		"sonnet@high+fast",
		"sonnet@hi",
		"opus-5@high",
		"opus-5@",
		"@high",
		"sol@none@high",
		"claude",
		"mystery",
		"nope",
	} {
		chosen, err := model.ParseSelection(path, selection, model.Defaults{})
		if err != nil {
			fmt.Fprintf(&written, "%-28q error: %v\n", selection, err)

			continue
		}

		fmt.Fprintf(&written, "%-28q %s\n", selection, chosen)
	}

	return written.String()
}

func newSessionFixtureChoices() []model.Choice {
	return []model.Choice{
		{Provider: "anthropic", ID: "claude-haiku-5", EffortLevels: []string{"low", "medium", "high", "xhigh"}},
		{Provider: "anthropic", ID: "claude-opus-4-5", EffortLevels: []string{"low", "high"}},
		{Provider: "anthropic", ID: "claude-opus-5", EffortLevels: []string{"medium", "max"}},
		{Provider: "anthropic", ID: "claude-sonnet-4-5", EffortLevels: []string{"medium"}},
		{Provider: "codex", ID: "gpt-5.6-sol", EffortLevels: []string{"none", "high"}},
	}
}

func resolveForkedSessionGlobs(t *testing.T) string {
	t.Helper()

	choices := newSessionFixtureChoices()
	var written strings.Builder

	for _, glob := range []string{"", "opus-5", "opus-5@max", "haiku", "gpt", "gpt@high", "gpt@high+fast", "nope"} {
		transition, err := cycle.ForkedSessionTransition(glob, choices, model.Defaults{Effort: "medium", IsFast: true}, "able-dolphin")
		if err != nil {
			fmt.Fprintf(&written, "%-28q error: %v\n", glob, err)

			continue
		}

		fmt.Fprintf(&written, "%-28q %s %s\n", glob, transitionKindName(transition.Kind), strings.Join(transition.Arguments, " "))
	}

	return written.String()
}

func resolveNewSessionGlobs(t *testing.T) string {
	t.Helper()

	choices := newSessionFixtureChoices()
	var written strings.Builder

	for _, glob := range []string{
		"",
		"opus",
		"OPUS",
		"opus-5",
		"anthropic/claude-opus-4-5",
		"sonnet",
		"gpt",
		"opus-5@high",
		"opus-4-5@high",
		"opus-4-5@hi",
		"haiku",
		"gpt@off",
		"gpt@high+fast",
		"sonnet@high+fast",
		"sonnet@nope",
		"opus-5@",
		"@high",
		"nope",
		"nonsense",
	} {
		transition, err := cycle.NewSessionTransition(glob, choices, model.Defaults{Effort: "medium"})
		if err != nil {
			fmt.Fprintf(&written, "%-28q error: %v\n", glob, err)

			continue
		}

		fmt.Fprintf(&written, "%-28q %s %s\n", glob, transitionKindName(transition.Kind), strings.Join(transition.Arguments, " "))
	}

	return written.String()
}

func transitionKindName(kind cycle.TransitionKind) string {
	switch kind {
	case cycle.Quit:
		return "quit"
	case cycle.NewSession:
		return "new"
	case cycle.ResumeSession:
		return "resume"
	case cycle.Restart:
		return "restart"
	}

	return "unknown"
}

func resolveAutomaticEfforts() string {
	var written strings.Builder

	for _, available := range [][]string{
		{"low", "high"},
		{"medium", "high", "xhigh"},
		{"low", "xhigh", "max"},
		{"medium", "xhigh"},
		{"medium", "max"},
		{"xhigh", "max"},
		{"medium"},
		{"none", "minimal"},
	} {
		for _, wantedEffort := range []string{"", "none", "minimal", "low", "medium", "high", "xhigh", "max", "unrecognised"} {
			defaults := model.Defaults{Effort: model.Effort(wantedEffort)}
			fmt.Fprintf(&written, "%-14q of %-20s -> %q\n", wantedEffort, "{"+strings.Join(available, ",")+"}", defaults.EffortFor(available))
		}
	}

	return written.String()
}

func useRoundRobinModelCache(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	path := location.GetModelCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	data := checkedModelCache(`{"codex":{"models":[{"id":"gpt-5.6-sol","efforts":["none","high"],"output":128000}]},"opencode-go":{"models":[{"id":"deepseek-v4-pro","efforts":["high","max"],"output":128000}]},"anthropic":{"models":[{"id":"claude-opus-5","efforts":["high","max"],"output":128000}]}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

type Painter = painter.Picasso

const defaultStreamingMode = output.StreamingModeLine

func newTestPainter(screen *output.Screen, isRunning bool) *Painter {
	return newStreamedTestPainter(screen, isRunning, defaultStreamingMode)
}

func newStreamedTestPainter(screen *output.Screen, isRunning bool, streamingMode output.StreamingMode) *Painter {
	return painter.New(screen, isRunning, nil, nil, streamingMode)
}

func describeHarnessCall(harness *App, event agent.Event) agent.FallbackRendering {
	return call.Describe(event, harness.agent.Tool, harness.workspace)
}

func describeBareCall(workspaceDir string, event agent.Event) agent.FallbackRendering {
	return call.Describe(event, nil, work.At(workspaceDir))
}

func TestTmpWouldShadowAWorkspace(t *testing.T) {
	for _, workspaceDir := range []string{sandbox.TmpDir, filepath.Join(sandbox.TmpDir, "project")} {
		if !work.IsShadowed(workspaceDir) {
			t.Errorf("expected %q to be shadowed", workspaceDir)
		}
	}

	for _, workspaceDir := range []string{"/", "/tmp-project"} {
		if work.IsShadowed(workspaceDir) {
			t.Errorf("did not expect %q to be shadowed", workspaceDir)
		}
	}
}

func TestAWorkspaceUnderTmpIsRefused(t *testing.T) {
	if err := work.At(t.TempDir()).Validate(); !errors.Is(err, work.ErrShadowed) {
		t.Errorf("got %v, want the workspace shadowing error", err)
	}
}

func TestAWorkspaceOutsideTmpIsAccepted(t *testing.T) {
	if err := work.At("/").Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAWorkspaceNamedThroughTmpIsRefused(t *testing.T) {
	alias := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink("/", alias); err != nil {
		t.Fatalf("could not create workspace alias: %v", err)
	}

	if err := work.At(alias).Validate(); !errors.Is(err, work.ErrShadowed) {
		t.Errorf("got %v, want the workspace shadowing error", err)
	}
}

func TestAWorkspaceIsAtEveryPathNamingIt(t *testing.T) {
	directory := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatalf("could not create workspace alias: %v", err)
	}

	for _, named := range []*work.Space{work.At(directory), work.At(alias)} {
		for _, dir := range []string{directory, alias, directory + string(filepath.Separator)} {
			if !named.IsAt(dir) {
				t.Errorf("%q was not recognised as %q", dir, named.GetDir())
			}
		}

		if named.IsAt(t.TempDir()) {
			t.Errorf("%q was recognised as another directory", named.GetDir())
		}
	}
}

func TestAnAbsentWorkspaceIsAtNothing(t *testing.T) {
	var absent *work.Space

	if absent.IsAt(t.TempDir()) {
		t.Error("a workspace that is nowhere was recognised somewhere")
	}
}

func TestAWorkspaceDescribesItselfEveryWayItIsDrawn(t *testing.T) {
	t.Setenv("HOME", "/home/alice")

	described := work.At("/home/alice/florp/io")

	for name, got := range map[string]string{
		"dir":       described.GetDir(),
		"name":      described.GetName(),
		"short dir": described.GetShortDir(),
	} {
		want := map[string]string{
			"dir":       "/home/alice/florp/io",
			"name":      "io",
			"short dir": "~/florp/io",
		}[name]

		if got != want {
			t.Errorf("%s is %q, want %q", name, got, want)
		}
	}

	if root := described.GetRoot(); root != nil {
		t.Errorf("a described workspace opened a root: %v", root)
	}
}

func TestADescribedWorkspaceFollowsTheLinksItIsNamedThrough(t *testing.T) {
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatalf("could not create workspace alias: %v", err)
	}

	described := work.At(alias)

	if got := described.GetDir(); got != alias {
		t.Errorf("dir is %q, want the name it was given %q", got, alias)
	}

	if got, want := described.GetResolvedDir(), mustResolveLinks(t, target); got != want {
		t.Errorf("resolved dir is %q, want %q", got, want)
	}
}

func TestAnAbsentWorkspaceAnswersForItself(t *testing.T) {
	var absent *work.Space

	if got := absent.GetDir(); got != "" {
		t.Errorf("dir is %q, want nothing", got)
	}

	if got := absent.GetRoot(); got != nil {
		t.Errorf("root is %v, want nothing", got)
	}

	if err := absent.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAnOpenedWorkspaceCarriesARootOnItself(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "AGENTS.md"), []byte("rules"), 0o600); err != nil {
		t.Fatal(err)
	}

	body, err := openTestWorkspace(t, directory).GetRoot().ReadFile("AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}

	if got := string(body); got != "rules" {
		t.Errorf("read %q through the workspace root, want %q", got, "rules")
	}
}

func mustResolveLinks(t *testing.T, path string) string {
	t.Helper()

	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}

	return resolvedPath
}

func TestARelativeXDGStateHomeIsIgnored(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join("inside", "workspace"))

	got := location.GetSessionsDir()
	if !filepath.IsAbs(got) {
		t.Fatalf("got relative state path %q", got)
	}
	if strings.HasPrefix(got, filepath.Join("inside", "workspace")) {
		t.Errorf("used the relative XDG_STATE_HOME in %q", got)
	}
}

func TestAnAbsoluteXDGStateHomeIsUsed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)

	want := filepath.Join(root, "org.crdx", "oh", "sessions")
	if got := location.GetSessionsDir(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheConfigIsInTheXDGConfigDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	want := filepath.Join(root, "org.crdx", "oh", "config.toml")
	if got := location.GetConfigFile(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTheCurrentDirectoryConfigOverridesTheGlobalConfig(t *testing.T) {
	configRoot := t.TempDir()
	workspaceDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)

	got := getConfigSources(workspaceDir)
	want := []config.Source{
		{Path: filepath.Join(configRoot, "org.crdx", "oh", "config.toml")},
		{Path: filepath.Join(workspaceDir, "oh.toml"), IsOverride: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestTheSimulationAnswersInPlaceOfTheConfiguredRotation(t *testing.T) {
	settings := config.Config{Model: config.Model{RoundRobin: []string{"anthropic/claude-opus-5@medium"}}}

	if got := configuredRotation(settings, false); !slices.Equal(got, settings.Model.RoundRobin) {
		t.Errorf("an ordinary session was given %v", got)
	}
	if got := configuredRotation(settings, true); len(got) != 0 {
		t.Errorf("the simulation was asked for %v, which it knows nothing about", got)
	}
}

func TestConfiguredCapabilitiesReplaceTheCommandLineDefault(t *testing.T) {
	options := cli.Options{Caps: caps.Read | caps.Shell}
	settings := config.Config{Caps: config.Caps{Default: config.DefaultCaps(caps.Read | caps.Write | caps.Git)}}

	applyDefaultCaps(&options, settings)

	if got := options.Caps.Flags(); got != "rwg" {
		t.Errorf("got capabilities %q", got)
	}
}

func TestExplicitCommandLineCapabilitiesOverrideTheConfig(t *testing.T) {
	options := cli.Options{Caps: caps.Read | caps.Shell, WereCapsChosen: true}
	settings := config.Config{Caps: config.Caps{Default: config.DefaultCaps(caps.Read | caps.Write | caps.Git)}}

	applyDefaultCaps(&options, settings)

	if got := options.Caps.Flags(); got != "rx" {
		t.Errorf("got capabilities %q", got)
	}
}

func TestTheGlobalContextIsInTheXDGConfigDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	want := filepath.Join(root, "org.crdx", "oh", "SYSTEM.md")
	if got := location.GetGlobalContextPath(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoldenForkMessageMatchesGolden(t *testing.T) {
	directory := t.TempDir()
	workspaceDirectory := t.TempDir()
	writer, err := store.Create(directory, store.Meta{WorkspaceDir: workspaceDirectory})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	if err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "begin"}); err != nil {
		t.Fatal(err)
	}

	forkSource, err := sessions.GetForkSource(directory, work.At(workspaceDirectory), writer.Name(), "focus on tests")
	if err != nil {
		t.Fatal(err)
	}
	destinationPath := filepath.Join("/state/sessions/new-otter/drops", forkSource.DroppedChatName)
	message := forkSource.GetInitialUserMessage(forkSource.DroppedChatName)
	message = forkSource.GetMessageWithChatAt(message, destinationPath)
	collisionMessage := "added files\n\n- /tmp/" + forkSource.DroppedChatName + "\n\n" + forkSource.GetInitialUserMessage(forkSource.DroppedChatName)
	collisionMessage = forkSource.GetMessageWithChatAt(collisionMessage, destinationPath)
	message = strings.ReplaceAll(message, writer.Name(), "source-otter")
	collisionMessage = strings.ReplaceAll(collisionMessage, writer.Name(), "source-otter")

	compareWithGolden(t, "fork-message", ".txt", map[string]func() string{
		"forked chat in drops":  func() string { return message },
		"same-named added file": func() string { return collisionMessage },
	})
}

type promptGolden struct {
	isYolo              bool
	areJobsGiven        bool
	hasClipboardDrops   bool
	isNetworkGranted    bool
	isPrinting          bool
	offeredTools        []string
	hasNoSockets        bool
	isRepository        bool
	readsTheScratchRoot bool
	hasNoExtraPaths     bool
	hasEveryPathKind    bool
}

func TestGoldenTheCompleteSystemPromptMatchesTheGolden(t *testing.T) {
	for name, shape := range map[string]promptGolden{
		"context":               {},
		"context-yolo":          {isYolo: true},
		"context-jobs":          {areJobsGiven: true},
		"context-drops":         {hasClipboardDrops: true},
		"context-network":       {isNetworkGranted: true},
		"context-network-print": {isNetworkGranted: true, isPrinting: true},
		"context-print":         {isPrinting: true},
		"context-file-tools":    {offeredTools: []string{"read", "ls", "grep"}},
		"context-no-telling":    {offeredTools: []string{"read", "bash"}},
		"context-no-sockets":    {hasNoSockets: true},
		"context-no-paths":      {hasNoExtraPaths: true},
		"context-path-kinds":    {hasEveryPathKind: true},
		"context-repository":    {isRepository: true},
		"context-scratch-root":  {readsTheScratchRoot: true},
		"context-simulation": {
			isYolo:       true,
			offeredTools: []string{"read", "ls", "grep"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			compareSystemPromptWithGolden(t, name, shape)
		})
	}
}

func compareSystemPromptWithGolden(t *testing.T, name string, shape promptGolden) {
	t.Helper()
	t.Setenv("HOME", "/users/alice")

	workspaceDirectory := t.TempDir()
	for fileName, body := range map[string]string{
		"AGENTS.md":       "Follow the project rules.",
		"AGENTS.local.md": "Prefer the local rules.",
	} {
		if err := os.WriteFile(filepath.Join(workspaceDirectory, fileName), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if shape.isRepository {
		if err := os.MkdirAll(filepath.Join(workspaceDirectory, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	workspace := openTestWorkspace(t, workspaceDirectory)

	configDirectory := t.TempDir()
	globalPath := filepath.Join(configDirectory, "SYSTEM.md")
	if err := os.WriteFile(globalPath, []byte("You are the golden test assistant."), 0o600); err != nil {
		t.Fatal(err)
	}

	dropsDirectory := ""
	if shape.hasClipboardDrops {
		dropsDirectory = "/state/sessions/tame-impala/drops"
	}

	readPaths := []string{"/reference"}
	if shape.readsTheScratchRoot {
		readPaths = []string{"/state/farm", "/state/sessions"}
	}
	extraPaths := shell.Paths{
		Read:  readPaths,
		Write: []string{"/output"},
		Exec:  []string{"/commands"},
	}
	if shape.hasNoExtraPaths {
		extraPaths = shell.Paths{}
	}
	if shape.hasEveryPathKind {
		extraPaths.Path = []string{"/toolbox"}
		extraPaths.Home = []string{"/users/alice/.config/git/ignore", "/outside/git/ignore"}
	}

	currentCaps := caps.Read | caps.Write | caps.Git | caps.Shell
	if shape.isNetworkGranted {
		currentCaps |= caps.Network
	}

	got, _, err := prompt.Load(prompt.Config{
		GlobalPath:     globalPath,
		Workspace:      workspace,
		SessionName:    "tame-impala",
		SessionsDir:    "/state/sessions",
		SessionDir:     "/state/sessions/tame-impala",
		ConfigFile:     "/config/config.toml",
		TmpDir:         "/state/farm/tame-impala",
		HomeDir:        "/state/home",
		CurrentCaps:    currentCaps,
		ExtraPaths:     extraPaths,
		DropsDirectory: dropsDirectory,
		Skills: []skill.Skill{{
			Name:        "golden",
			Description: "Exercise complete prompt assembly.",
			Location:    "/skills/golden/SKILL.md",
		}},
		OfferedTools: shape.offeredTools,
		Conditions: conditions.Conditions{
			UnixSockets: !shape.hasNoSockets,
			IPv6:        !shape.hasNoSockets,
			Interactive: !shape.isPrinting,
		},
		JobsGranted:    shape.areJobsGiven,
		NetworkGranted: shape.isNetworkGranted,
		Yolo:           shape.isYolo,
	})
	if err != nil {
		t.Fatal(err)
	}
	got = strings.ReplaceAll(got, workspaceDirectory, "/workspace")
	got = strings.ReplaceAll(got, configDirectory, "/config")
	got = strings.ReplaceAll(got, "127.0.0.1", "<loopback>")

	goldenPath := filepath.Join("testdata", "output", name+".prompt")
	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("system prompt differs from %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, want)
	}
}

const (
	replayColumns = 100
	replayLines   = 24
	narrowColumns = 40
	tinyColumns   = 12
	oneColumn     = 1
	noColumns     = 0

	turnSoFar    = 9 * time.Second
	idleSoFar    = 4 * time.Second
	sessionSoFar = 2 * time.Minute
	spinnerSoFar = 250 * time.Millisecond

	workspaceMarker   = "/workspace"
	lifecycleScenario = "success@rxw.jsonl"

	retiredAnimalSession = "brave-vulture"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

type replayEntry struct {
	Event *agent.Event `json:"event,omitempty"`
}

func TestGoldenEveryScenarioShowsWhatItShowedBefore(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			compareWithGolden(t, journal.name, ".screen", map[string]func() string{
				"wide":       func() string { return shownAtWidth(t, entries, replayColumns) },
				"narrow":     func() string { return shownAtWidth(t, entries, narrowColumns) },
				"tiny":       func() string { return shownAtWidth(t, entries, tinyColumns) },
				"one column": func() string { return shownAtWidth(t, entries, oneColumn) },
				"printed":    func() string { return shown(t, replayAsPrinted(t, entries), replayColumns) },
			})
		})
	}
}

var markedScenarios = []string{"host-commands", "mid-call-message", "modes"}

func TestGoldenAMarkStandsWhereEachUserMessageBegins(t *testing.T) {
	passes := map[string]func() string{}

	for _, name := range markedScenarios {
		entries := readJournal(t, filepath.Join("testdata", "input", name+".jsonl"))

		passes[name+", wide"] = func() string {
			return shownWithMarks(t, replayAtWidth(t, entries, replayColumns), replayColumns)
		}
		passes[name+", narrow"] = func() string {
			return shownWithMarks(t, replayAtWidth(t, entries, narrowColumns), narrowColumns)
		}
		passes[name+", streamed"] = func() string {
			return shownWithMarks(t, streamIntoBuffer(t, entries, output.StreamingModeLine), replayColumns)
		}
		passes[name+", printed"] = func() string {
			return shownWithMarks(t, replayAsPrinted(t, entries), replayColumns)
		}
		passes[name+", plain"] = func() string {
			return shownWithMarks(t, replayPlainly(t, entries), replayColumns)
		}
		passes[name+", tiny"] = func() string {
			return shownWithMarks(t, replayAtWidth(t, entries, tinyColumns), tinyColumns)
		}
	}

	passes["queued, standing unsent"] = func() string {
		return shownWithMarks(t, queuedMessagesStream(t, queuedTwo), replayColumns)
	}
	passes["queued, delivered at the boundary"] = func() string {
		return shownWithMarks(t, queuedMessagesStream(t, queuedDelivered), replayColumns)
	}

	compareWithGolden(t, "message-marks", ".screen", passes)
}

const markGutter = "▸ "

func shownWithMarks(t *testing.T, stream string, columns int) string {
	t.Helper()

	drawn := playScreen(t, stream, columns)
	rows := drawn.text()

	for _, row := range drawn.marked() {
		for len(rows) <= row {
			rows = append(rows, "")
		}
	}

	shownRows := make([]string, 0, len(rows))
	for at, row := range rows {
		gutter := strings.Repeat(" ", width.Of(markGutter))
		if drawn.marks[at] {
			gutter = markGutter
		}

		shownRows = append(shownRows, strings.TrimRight(gutter+row, " "))
	}

	return strings.Join(shownRows, "\n")
}

func TestEveryUserMessageIsMarkedWhereItBegins(t *testing.T) {
	background := userBackground(t)

	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)
			drawn := playScreen(t, replayAtWidth(t, entries, replayColumns), replayColumns)

			marked := drawn.marked()
			if got, want := len(marked), countUserMessages(entries); got != want {
				t.Fatalf("%d rows were marked, want one per user message, of which there are %d: %v", got, want, marked)
			}

			for _, row := range marked {
				if drawn.backgroundAt(row) != background {
					t.Errorf("row %d was marked but holds no user message: %q", row, drawn.text()[row])
				}
				if row > 0 && drawn.backgroundAt(row-1) == background {
					t.Errorf("row %d was marked part way into a user message", row)
				}
			}
		})
	}
}

func TestNothingIsMarkedWhereAConversationIsPreviewedInsideAMenu(t *testing.T) {
	entries := readJournal(t, filepath.Join("testdata", "input", markedScenarios[0]+".jsonl"))

	events := make([]agent.Event, 0, len(entries))
	for _, entry := range entries {
		events = append(events, *entry.Event)
	}

	drawn := preview.Draw(events, layOutWorkspace(t), replayColumns)

	if found := strings.Contains(strings.Join(drawn, "\n"), escape.MessageMark); found {
		t.Error("a previewed conversation marked rows the menu draws wherever it likes")
	}
}

func userBackground(t *testing.T) string {
	t.Helper()

	drawn := playScreen(t, style.User(" "), replayColumns)

	return drawn.backgroundAt(0)
}

func countUserMessages(entries []replayEntry) int {
	count := 0

	for _, entry := range entries {
		if entry.Event != nil && entry.Event.Kind == agent.UserMessageEvent {
			count++
		}
	}

	return count
}

const groupingScenario = "groups.jsonl"

func everyGrouping() [][]string {
	names := []string{"notice", "tool", "answer", "reasoning"}
	memberships := make([]int, len(names))

	var groupings [][]string

	var walk func(int, int)
	walk = func(at int, classCount int) {
		if at == len(names) {
			clauses := make([]string, classCount)
			for member, class := range memberships {
				if clauses[class] != "" {
					clauses[class] += " "
				}
				clauses[class] += names[member]
			}
			groupings = append(groupings, clauses)

			return
		}

		for class := range classCount + 1 {
			memberships[at] = class
			walk(at+1, max(classCount, class+1))
		}
	}
	walk(0, 0)

	return groupings
}

func newGroupedRig(t *testing.T, groups []string, isPrinted bool) *replayRig {
	t.Helper()

	grouping, err := output.ParseGrouping(groups)
	if err != nil {
		t.Fatal(err)
	}

	rig := newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		screen := output.NewTerminalOfSize(written, replayColumns, replayLines)
		if isPrinted {
			screen.AppendOnly()
		}

		return screen.LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
	rig.chat.screen.SetGrouping(grouping)
	rig.chat.runMode.isPrinting = isPrinted

	return rig
}

func replayUnderGrouping(t *testing.T, groups []string, isPrinted bool) string {
	t.Helper()

	entries := readJournal(t, filepath.Join("testdata", "input", groupingScenario))

	return replayInto(newGroupedRig(t, groups, isPrinted), entries)
}

func TestGoldenEveryGroupingSpacesTheOutputAsItSays(t *testing.T) {
	passes := map[string]func() string{}

	for _, groups := range everyGrouping() {
		passes[strings.Join(groups, ", ")] = func() string {
			return shown(t, replayUnderGrouping(t, groups, false), replayColumns)
		}
	}

	if len(passes) != 15 {
		t.Fatalf("there are %d groupings, want all 15 partitions of the four groups", len(passes))
	}

	compareWithGolden(t, "groupings", ".screen", passes)
}

func TestEveryGroupingPrintsWhatTheInterfaceShowed(t *testing.T) {
	for _, groups := range everyGrouping() {
		t.Run(strings.Join(groups, ", "), func(t *testing.T) {
			printed := replayUnderGrouping(t, groups, true)
			requireNothingWasDrawnOver(t, printed)
			requireSameVisibleScreen(
				t,
				"the printed session differs from what the interface drew",
				replayUnderGrouping(t, groups, false),
				printed,
			)
		})
	}
}

const reasoningScenario = "reasoning-markdown.jsonl"

func everyReasoningRendering() map[string]output.ReasoningRendering {
	return map[string]output.ReasoningRendering{
		"markdown": output.ReasoningMarkdown,
		"plain":    output.ReasoningPlain,
	}
}

func newThinkingRig(t *testing.T, rendering output.ReasoningRendering, isPrinted bool, columns int) *replayRig {
	t.Helper()

	rig := newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		screen := output.NewTerminalOfSize(written, columns, replayLines)
		if isPrinted {
			screen.AppendOnly()
		}

		return screen.LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
	rig.chat.display.reasoningRendering = rendering
	rig.chat.runMode.isPrinting = isPrinted

	return rig
}

func thoughtEntries(t *testing.T) []replayEntry {
	t.Helper()

	return readJournal(t, filepath.Join("testdata", "input", reasoningScenario))
}

func replayThoughtsAtWidth(t *testing.T, rendering output.ReasoningRendering, isPrinted bool, columns int) string {
	t.Helper()

	return replayInto(newThinkingRig(t, rendering, isPrinted, columns), thoughtEntries(t))
}

func replayThoughts(t *testing.T, rendering output.ReasoningRendering, isPrinted bool) string {
	t.Helper()

	return replayThoughtsAtWidth(t, rendering, isPrinted, replayColumns)
}

func streamThoughts(t *testing.T, rendering output.ReasoningRendering, streamingMode output.StreamingMode) string {
	t.Helper()

	rig := newThinkingRig(t, rendering, false, replayColumns)
	rig.chat.display.streamingMode = streamingMode

	return streamThrough(t, rig, thoughtEntries(t))
}

const noticeMidThoughtScenario = "notice-mid-thought.jsonl"

func noticeMidThoughtEntries(t *testing.T) []replayEntry {
	t.Helper()

	return readJournal(t, filepath.Join("testdata", "input", noticeMidThoughtScenario))
}

func streamThroughHoldingTheNotice(t *testing.T, rig *replayRig, entries []replayEntry, isHeld bool) string {
	t.Helper()

	rig.chat.currentTurn = Turn{Stream: testRunningTurnStream(), painter: rig.chat.newPainter(true)}
	rig.chat.screen.ReportProgress(true)

	var held *agent.Event

	for _, entry := range entries {
		event := *entry.Event

		if isHeld && event.Kind == jobrecord.Ended {
			held = entry.Event
			continue
		}

		if event.Kind == agent.ModelMessageEvent || event.Kind == agent.ModelReasoningEvent {
			pieces := slices.Collect(deltaSized(event.Text))
			for at, piece := range pieces {
				if held != nil && at == len(pieces)/2 {
					rig.chat.recordedEvents = append(rig.chat.recordedEvents, *held)
					rig.chat.currentTurn.painter.DrawEvent(*held)
					held = nil
				}
				rig.chat.currentTurn.painter.DrawDelta(agent.Delta{Kind: event.Kind, Text: piece})
			}
		}

		rig.chat.recordedEvents = append(rig.chat.recordedEvents, event)
		rig.chat.currentTurn.painter.DrawEvent(event)

		if rig.chat.currentTurn.painter.Stale() {
			rig.chat.redraw()
		}
	}

	rig.chat.currentTurn.painter.Close(dynamic.Done)
	rig.chat.screen.End()
	rig.chat.screen.ReportProgress(false)

	return rig.drawn()
}

func streamOneThought(
	t *testing.T,
	rendering output.ReasoningRendering,
	streamingMode output.StreamingMode,
	isInterrupted bool,
) string {
	t.Helper()

	rig := newThinkingRig(t, rendering, false, replayColumns)
	rig.chat.display.streamingMode = streamingMode

	return streamThroughHoldingTheNotice(t, rig, noticeMidThoughtEntries(t), isInterrupted)
}

func everyThinkingWidth() map[string]int {
	return map[string]int{
		"wide":       replayColumns,
		"narrow":     narrowColumns,
		"tiny":       tinyColumns,
		"one column": oneColumn,
	}
}

func TestANoticeArrivingMidThoughtKeepsEveryWordOfIt(t *testing.T) {
	for streamingName, streamingMode := range everyStreamingMode() {
		t.Run(streamingName, func(t *testing.T) {
			whole := shown(t, streamOneThought(t, output.ReasoningMarkdown, streamingMode, false), replayColumns)
			drawn := shown(t, streamOneThought(t, output.ReasoningMarkdown, streamingMode, true), replayColumns)

			for row := range strings.SplitSeq(whole, "\n") {
				if row = strings.TrimSpace(row); row == "" {
					continue
				}
				if !strings.Contains(drawn, row) {
					t.Errorf("the notice took %q out of the thought:\n%s", row, drawn)
				}
			}
		})
	}
}

func TestANoticeArrivingMidThoughtIsRestoredWhereItArrived(t *testing.T) {
	for name, rendering := range everyReasoningRendering() {
		for streamingName, streamingMode := range everyStreamingMode() {
			t.Run(name+" "+streamingName, func(t *testing.T) {
				replayed := replayInto(
					newThinkingRig(t, rendering, false, replayColumns),
					noticeMidThoughtEntries(t),
				)

				requireSameVisibleScreen(
					t,
					"a thought a notice arrived during differs from the same conversation replayed",
					streamOneThought(t, rendering, streamingMode, true),
					replayed,
				)
			})
		}
	}
}

func TestGoldenEveryReasoningRenderingDrawsAThoughtAsItSays(t *testing.T) {
	shownPasses := map[string]func() string{}
	writtenPasses := map[string]func() string{}

	for name, rendering := range everyReasoningRendering() {
		for widthName, columns := range everyThinkingWidth() {
			shownPasses[name+" "+widthName] = func() string {
				return shown(t, replayThoughtsAtWidth(t, rendering, false, columns), columns)
			}
		}
		shownPasses[name+" printed"] = func() string {
			return shown(t, replayThoughts(t, rendering, true), replayColumns)
		}

		writtenPasses[name] = func() string { return replayThoughts(t, rendering, false) }
		writtenPasses[name+" printed"] = func() string { return replayThoughts(t, rendering, true) }

		for streamingName, streamingMode := range everyStreamingMode() {
			shownPasses[name+" streamed "+streamingName] = func() string {
				return shown(t, streamThoughts(t, rendering, streamingMode), replayColumns)
			}
			writtenPasses[name+" streamed "+streamingName] = func() string {
				return streamThoughts(t, rendering, streamingMode)
			}
			shownPasses[name+" under a notice streamed "+streamingName] = func() string {
				return shown(t, streamOneThought(t, rendering, streamingMode, true), replayColumns)
			}
		}
	}

	compareWithGolden(t, "reasonings", ".screen", shownPasses)
	compareWithGolden(t, "reasonings", ".ansi", writtenPasses)
}

func TestAKeptThoughtClosesEveryRowSoItCannotBleed(t *testing.T) {
	for name, columns := range everyThinkingWidth() {
		t.Run(name, func(t *testing.T) {
			rows := painter.RenderReasoning(structuredThought, columns, output.ReasoningMarkdown)
			if len(rows) == 0 {
				t.Fatal("a thought drew nothing")
			}
			for _, row := range rows {
				if !strings.HasSuffix(row, resetSequence) {
					t.Errorf("row %q does not close its styling, so it bleeds into what follows", row)
				}
			}
		})
	}
}

func shownAtWidth(t *testing.T, entries []replayEntry, columns int) string {
	t.Helper()

	return shown(t, replayAtWidth(t, entries, columns), columns)
}

func shown(t *testing.T, stream string, columns int) string {
	t.Helper()

	return strings.Join(visibleScreen(t, stream, columns), "\n")
}

func streamPasses[Scenario any](
	t *testing.T,
	stream func(*testing.T, Scenario) string,
	scenarios map[string]Scenario,
) map[string]func() string {
	t.Helper()

	passes := map[string]func() string{}

	for name, scenario := range scenarios {
		passes[name] = func() string { return stream(t, scenario) }
	}

	return passes
}

func shownPasses(t *testing.T, passes map[string]func() string) map[string]func() string {
	t.Helper()

	shownAt := map[string]func() string{}

	for name, pass := range passes {
		shownAt[name] = func() string { return shown(t, pass(), replayColumns) }
	}

	return shownAt
}

func TestGoldenEveryScenarioDrawsWhatItDrewBefore(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			compareWithGolden(t, journal.name, ".ansi", map[string]func() string{
				"wide":           func() string { return replayAtWidth(t, entries, replayColumns) },
				"narrow":         func() string { return replayAtWidth(t, entries, narrowColumns) },
				"tiny":           func() string { return replayAtWidth(t, entries, tinyColumns) },
				"unsized":        func() string { return replayAtWidth(t, entries, noColumns) },
				"one column":     func() string { return replayAtWidth(t, entries, oneColumn) },
				"streamed asap":  func() string { return streamIntoBuffer(t, entries, output.StreamingModeASAP) },
				"streamed line":  func() string { return streamIntoBuffer(t, entries, output.StreamingModeLine) },
				"streamed paced": func() string { return streamIntoBuffer(t, entries, output.StreamingModePaced) },
				"plain":          func() string { return replayPlainly(t, entries) },
				"printed":        func() string { return replayAsPrinted(t, entries) },
			})
		})
	}
}

const shortLines = 6

func TestGoldenATallRegionOnAShortTerminalIsRepairedRatherThanFrozen(t *testing.T) {
	scenarios := map[string]string{
		"streamed taller than the terminal":      tallRegionScenario,
		"a panel grown taller than the terminal": noticePanelScenario,
	}

	passes := map[string]func() string{}

	for name, scenario := range scenarios {
		entries := readJournal(t, filepath.Join("testdata", "input", scenario))

		drawn := streamThrough(t, newShortRig(t), entries)

		passes[name] = func() string { return shown(t, drawn, replayColumns) }

		requireSameVisibleScreen(
			t,
			"a conversation taller than the terminal differs from the same one with room to draw",
			replayAtWidth(t, entries, replayColumns),
			drawn,
		)
	}

	noticeBeside := func(openTurn func(*testing.T, *bytes.Buffer) (*App, *edit.Input)) string {
		var screenOutput bytes.Buffer
		self, inputLine := openTurn(t, &screenOutput)
		self.jobEnded(endedJobConclusion())
		self.show(inputLine)

		return screenOutput.String()
	}

	frozen := noticeBeside(frozenTallTurn)

	requireSameVisibleScreen(
		t,
		"a notice beside a region taller than the terminal differs from the same one with room to draw",
		noticeBeside(roomyTallTurn),
		frozen,
	)

	passes["a notice beside a frozen region"] = func() string { return shown(t, frozen, replayColumns) }

	compareWithGolden(t, "short-terminal", ".screen", passes)
}

const tallRegionScenario = "parallel@rxw.jsonl"

const noticePanelScenario = "notices-mid-round.jsonl"

func TestAPrintedSessionShowsWhatTheInterfaceShowed(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			requireSameVisibleScreen(
				t,
				"the printed session differs from what the interface drew",
				replayAtWidth(t, entries, replayColumns),
				replayAsPrinted(t, entries),
			)
		})
	}
}

func TestGoldenEverySessionIsWrittenDownTheSameWay(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			compareWithGolden(t, journal.name, ".transcript", map[string]func() string{
				"written down": func() string { return writeTranscript(t, entries) },
			})
		})
	}
}

func writeTranscript(t *testing.T, entries []replayEntry) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "chat.md")

	recorder, err := transcript.Open(path, transcript.Meta{
		Name:      "tame-impala",
		Model:     "gpt-5.6-sol",
		Effort:    "high",
		Provider:  "codex",
		Workspace: workspaceMarker,
		StartedAt: transcriptTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	for at, entry := range entries {
		if entry.Event == nil {
			continue
		}

		when := transcriptTime.Add(time.Duration(at) * time.Second)
		if err := recorder.Event(when, *entry.Event); err != nil {
			t.Fatal(err)
		}
	}

	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(path) //nolint:gosec // the path is the test's own transcript file
	if err != nil {
		t.Fatal(err)
	}

	return string(written)
}

var transcriptTime = time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)

func TestGoldenTheScreenAroundAConversationDrawsWhatItDrewBefore(t *testing.T) {
	entries := readJournal(t, filepath.Join("testdata", "input", lifecycleScenario))

	passes := map[string]func() string{}

	for _, screen := range []struct {
		name string
		open func(*testing.T) *replayRig
	}{
		{name: "", open: newWideRig},
		{name: " on a pipe", open: newPlainRig},
	} {
		passes["under a footer"+screen.name] = func() string {
			return replayUnderFooter(t, screen.open, entries)
		}
		passes["resized"+screen.name] = func() string {
			return replayThenRedraw(t, screen.open, entries, false)
		}
		passes["resized mid-turn"+screen.name] = func() string {
			return replayThenRedraw(t, screen.open, entries, true)
		}
		passes["progress refreshed mid-turn"+screen.name] = func() string {
			return replayThenRefreshProgress(t, screen.open, entries)
		}
		passes["released and kept"+screen.name] = func() string {
			return replayThenRelease(t, screen.open, entries, true)
		}
		passes["released and unused"+screen.name] = func() string {
			return replayThenRelease(t, screen.open, entries, false)
		}
	}

	compareWithGolden(t, "lifecycle", ".screen", shownPasses(t, onATerminal(passes)))

	passes["editing cursor lifecycle"] = drawEditingCursorLifecycle
	compareWithGolden(t, "lifecycle", ".ansi", passes)
}

const signalGoldenProcessVariable = "OH_TEST_SIGNAL_GOLDEN_PROCESS"

func TestGoldenFatalSignalTerminalRestorationMatchesTheGolden(t *testing.T) {
	if os.Getenv(signalGoldenProcessVariable) != "" {
		screen := output.NewTerminalOfSize(os.Stdout, replayColumns, replayLines)
		restoreCursor := screen.BeginEditing()
		screen.Footer([]string{footerPrompt}, 0, len(footerPrompt))

		restore := func() {
			restoreTerminalState(
				screen,
				false,
				restoreCursor,
				func() { _, _ = io.WriteString(os.Stdout, "\x1b[23;0t") },
				func() { _, _ = io.WriteString(os.Stdout, key.Disable) },
			)
		}
		stopListening := tty.RestoreOnSignal(restore)
		defer stopListening()

		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Second)
		return
	}

	//nolint:gosec // rerun this test binary as its signalled subprocess
	command := exec.CommandContext(
		t.Context(), os.Args[0], "-test.run=^TestGoldenFatalSignalTerminalRestorationMatchesTheGolden$",
	)
	command.Env = append(os.Environ(), signalGoldenProcessVariable+"=1")
	output, err := command.CombinedOutput()

	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("the signalled process returned %v, having drawn %q", err, string(output))
	}
	status, ok := exitError.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("the process ended with %v, want SIGTERM", exitError.Sys())
	}

	compareWithGolden(t, "signal-restoration", ".ansi", map[string]func() string{
		"fatal signal": func() string { return string(output) },
	})
}

func drawEditingCursorLifecycle() string {
	var drawn strings.Builder
	screen := output.NewTerminalOfSize(&drawn, replayColumns, replayLines)

	restore := screen.BeginEditing()
	screen.Footer([]string{footerPrompt}, 0, len(footerPrompt))
	screen.Release(false)
	restore()

	return drawn.String()
}

func onATerminal(passes map[string]func() string) map[string]func() string {
	kept := map[string]func() string{}

	for name, pass := range passes {
		if !strings.HasSuffix(name, " on a pipe") {
			kept[name] = pass
		}
	}

	return kept
}

func TestGoldenATurnStillRunningDrawsWhatItDrewBefore(t *testing.T) {
	entries := readJournal(t, filepath.Join("testdata", "input", lifecycleScenario))

	passes := map[string]func() string{
		"a call still running": func() string { return replayWhileRunning(t, entries) },
		"a call carrying on after its question": func() string {
			return replayAfterAQuestion(t, entries)
		},
		"discarded reasoning": func() string { return drawDiscardedReasoning(t) },
		"unknown command during answer": func() string {
			return drawAcceptedInputDuringStream(t, "/unknown", agent.ModelMessageEvent)
		},
		"unknown command during reasoning": func() string {
			return drawAcceptedInputDuringStream(t, "/unknown", agent.ModelReasoningEvent)
		},
		"unknown snippet during reasoning": func() string {
			return drawAcceptedInputDuringStream(t, "//unknown", agent.ModelReasoningEvent)
		},
		"ordinary message during answer": func() string {
			return drawAcceptedInputDuringStream(t, "check the other path too", agent.ModelMessageEvent)
		},
	}

	compareWithGolden(t, "running", ".ansi", passes)

	screenPasses := shownPasses(t, passes)
	screenPasses["help during reasoning"] = func() string { return shownHelpDuringReasoningFrames(t) }
	screenPasses["help on a short running terminal"] = func() string { return shownShortRunningHelpFrames(t) }
	screenPasses["reasoning discarded as the turn stops"] = func() string {
		return shownStoppedTurnAfterReasoningFrames(t)
	}
	for name, scenario := range everyAnswerStartScenario() {
		screenPasses["queued message as answer starts "+name] = func() string {
			return shownAnswerStartsAfterAcceptedInputFrames(t, scenario)
		}
	}
	compareWithGolden(t, "running", ".screen", screenPasses)
}

func drawDiscardedReasoning(t *testing.T) string {
	t.Helper()

	rig := newReplayRig(t, replayColumns)
	rig.chat.currentTurn = Turn{Stream: testRunningTurnStream(), painter: rig.chat.newPainter(true)}

	completeThought := agent.Event{Kind: agent.ModelReasoningEvent, Text: "complete thought"}
	rig.chat.takeTurn(TurnEvent{Update: agent.Update{Event: &completeThought}})
	incompleteThought := agent.Delta{Kind: agent.ModelReasoningEvent, Text: "incomplete thought"}
	rig.chat.takeTurn(TurnEvent{Update: agent.Update{Delta: &incompleteThought}})
	failure := agent.Event{Kind: agent.FailureEvent, Text: "stream failed"}
	rig.chat.takeTurn(TurnEvent{Update: agent.Update{Event: &failure}})
	rig.chat.screen.End()

	return strings.TrimSuffix(rig.drawn(), "\r\n")
}

func drawAcceptedInputDuringStream(t *testing.T, message string, kind agent.Kind) string {
	t.Helper()

	writer := &frameRecordingWriter{}
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(writer, replayColumns, replayLines)
	self.slash.commands = fixtureSnippetRegistry(t, nil)
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine
	inputLine.SetText(message)
	self.show(inputLine)
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: kind, Text: "still working"})
	framesBeforeCommand := len(writer.frames)

	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})

	frames := writer.frames[framesBeforeCommand:]
	self.currentTurn.painter.Close(dynamic.Cancelled)
	return strings.Join(frames, "")
}

const (
	answerStartTerminalLines = 12
	answerStartReasoning     = "thinking about the repaint while the queued message stays visible " +
		"through every intermediate frame and every rendering path the answer transition takes " +
		"before the first complete response reaches the conversation"
)

type answerStartScenario struct {
	streamingMode        output.StreamingMode
	reasoningRendering   output.ReasoningRendering
	terminalLines        int
	messages             []string
	visibleQueuedMessage string
}

func everyAnswerStartScenario() map[string]answerStartScenario {
	scenarios := make(map[string]answerStartScenario)
	for reasoningName, reasoningRendering := range everyReasoningRendering() {
		for streamingName, streamingMode := range everyStreamingMode() {
			name := reasoningName + " " + streamingName
			scenarios[name+" roomy"] = answerStartScenario{
				streamingMode:        streamingMode,
				reasoningRendering:   reasoningRendering,
				terminalLines:        replayLines,
				messages:             []string{"check the other path too"},
				visibleQueuedMessage: "⏳ check the other path too",
			}
			scenarios[name+" terminal-filling"] = answerStartScenario{
				streamingMode:        streamingMode,
				reasoningRendering:   reasoningRendering,
				terminalLines:        answerStartTerminalLines,
				messages:             tallQueue(),
				visibleQueuedMessage: "⏳ queued message 20",
			}
		}
	}
	return scenarios
}

func TestAQueuedMessageStaysVisibleAsAnAnswerStarts(t *testing.T) {
	for name, scenario := range everyAnswerStartScenario() {
		t.Run(name, func(t *testing.T) {
			frames := answerStartsAfterAcceptedInputFrames(t, scenario)
			if len(frames) == 0 {
				t.Fatal("the answer start drew nothing, so nothing kept the queued message")
			}

			for i, frame := range frames {
				visible := strings.Join(visibleScreen(t, frame, replayColumns), "\n")
				if !strings.Contains(visible, scenario.visibleQueuedMessage) {
					t.Errorf("frame %d dropped the queued message:\n%s", i+1, visible)
				}
			}
		})
	}
}

func answerStartsAfterAcceptedInputFrames(t *testing.T, scenario answerStartScenario) []string {
	t.Helper()

	writer := &frameRecordingWriter{}
	self := slashCommandFixture(t, caps.Read)
	self.agent = agent.New("", quietProvider{}, nil)
	self.workspace = work.At(t.TempDir())
	self.screen = output.NewTerminalOfSize(writer, replayColumns, scenario.terminalLines)
	self.display.streamingMode = scenario.streamingMode
	self.display.reasoningRendering = scenario.reasoningRendering
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine

	self.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "look into it"})
	self.currentTurn.painter.DrawDelta(agent.Delta{
		Kind: agent.ModelReasoningEvent,
		Text: answerStartReasoning,
	})
	for _, message := range scenario.messages {
		typeMessage(t, self, inputLine, history, message)
	}
	self.show(inputLine)
	framesBeforeAnswer := len(writer.frames)

	answer := agent.Delta{Kind: agent.ModelMessageEvent, Text: "The answer starts here."}
	self.takeTurn(TurnEvent{Update: agent.Update{Delta: &answer}})
	self.show(inputLine)

	frames := slices.Clone(writer.frames[framesBeforeAnswer:])
	self.currentTurn.painter.Close(dynamic.Cancelled)
	return frames
}

func shownAnswerStartsAfterAcceptedInputFrames(t *testing.T, scenario answerStartScenario) string {
	t.Helper()

	return shownFrames(t, answerStartsAfterAcceptedInputFrames(t, scenario))
}

func shownFrames(t *testing.T, frames []string) string {
	t.Helper()

	var shown strings.Builder
	var previous []string
	frameNumber := 0
	for _, frame := range frames {
		visible := visibleScreen(t, frame, replayColumns)
		if slices.Equal(visible, previous) {
			continue
		}
		frameNumber++
		fmt.Fprintf(&shown, "--- frame %d ---\n%s\n", frameNumber, strings.Join(visible, "\n"))
		previous = visible
	}
	return strings.TrimSuffix(shown.String(), "\n")
}

func shownStoppedTurnAfterReasoningFrames(t *testing.T) string {
	t.Helper()

	return shownFrames(t, stoppedTurnAfterReasoningFrames(t))
}

func stoppedTurnAfterReasoningFrames(t *testing.T) []string {
	t.Helper()

	writer := &frameRecordingWriter{}
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(writer, replayColumns, replayLines)
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	inputLine := edit.NewInput(edit.NewHistory("", historyLimit))
	self.inputLine = inputLine

	self.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "the migration should be reviewable"})
	self.currentTurn.painter.DrawDelta(agent.Delta{
		Kind: agent.ModelReasoningEvent,
		Text: "weighing up how flat the migration can be, and what a reviewer needs from it, and whether " +
			"the steps it takes can be read in one sitting without following anything back to where it " +
			"came from\n",
	})
	self.show(inputLine)
	framesBeforeStopping := len(writer.frames)

	self.recordEvent(interrupt.Event(interrupt.Escape))
	self.screen.End()
	self.show(inputLine)

	return slices.Clone(writer.frames[framesBeforeStopping:])
}

type journal struct {
	name string
	path string
}

func everyJournal(t *testing.T) []journal {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "input", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	if len(paths) == 0 {
		t.Fatal("expected the journals to be found")
	}

	journals := make([]journal, 0, len(paths))

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		journals = append(journals, journal{name: name, path: path})
	}

	return journals
}

func drawnOnAStoppedClock(t *testing.T, draw func(t *testing.T) string) string {
	t.Helper()

	var drawn string

	synctest.Test(t, func(t *testing.T) { drawn = draw(t) })

	return drawn
}

func compareWithGolden(t *testing.T, name string, suffix string, passes map[string]func() string) {
	t.Helper()

	var drawn strings.Builder

	for _, pass := range slices.Sorted(maps.Keys(passes)) {
		fmt.Fprintf(&drawn, "=== %s ===\n%s\n", pass, strutil.VisibleEscapes(anonymisePictures(passes[pass]())))
	}

	golden := strings.TrimRight(drawn.String(), "\n") + "\n"

	goldenPath := filepath.Join("testdata", "output", name+suffix)

	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(golden), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatalf("%v: write the goldens with `just -f rig.just goldens`", err)
	}

	if golden != string(want) {
		t.Errorf(
			"%s drew something else; write the goldens again and read the diff\n"+
				"--- drawn ---\n%s\n--- golden ---\n%s",
			name, golden, want,
		)
	}
}

func readJournal(t *testing.T, path string) []replayEntry {
	t.Helper()

	contents, err := os.ReadFile(path) //nolint:gosec // the path is the test's own journal
	if err != nil {
		t.Fatal(err)
	}

	var entries []replayEntry
	for number, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		var entry replayEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("%s: line %d: %v", path, number+1, err)
		}

		entries = append(entries, entry)
	}

	return entries
}

type replayRig struct {
	chat        *App
	written     *strings.Builder
	workspace   *work.Space
	sessionName string
}

func newReplayRig(t *testing.T, columns int) *replayRig {
	t.Helper()

	return newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.NewTerminalOfSize(written, columns, replayLines).LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
}

func newShortRig(t *testing.T) *replayRig {
	t.Helper()

	return newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.NewTerminalOfSize(written, replayColumns, shortLines).
			LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
}

func newWideRig(t *testing.T) *replayRig {
	t.Helper()

	return newReplayRig(t, replayColumns)
}

func newPlainRig(t *testing.T) *replayRig {
	t.Helper()

	return newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		return output.New(written).LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
}

func newPrintedRig(t *testing.T) *replayRig {
	t.Helper()

	return newRig(t, func(written *strings.Builder, workspaceDir string) *output.Screen {
		screen := output.NewTerminalOfSize(written, replayColumns, replayLines)
		return screen.AppendOnly().LinkPathsUnder(link.Roots{Workspace: workspaceDir})
	})
}

func newRig(t *testing.T, openScreen func(*strings.Builder, string) *output.Screen) *replayRig {
	t.Helper()

	workspace := layOutWorkspace(t)

	files := file.New(workspace.GetRoot(), caps.RefuseWrite(caps.NewMode(caps.All())))

	var written strings.Builder

	screen := openScreen(&written, workspace.GetDir())

	tools := toolbox.Rummage(files, file.NewSnapshots())
	tools = append(
		tools,
		bash.New(
			files,
			func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
			func(context.Context, string) error { return nil },
			sandbox.Direct(),
			true,
		),
		notify.New(screen.WriteEscape, func() bool { return false }),
		job.New(
			jobs.New(sandbox.Direct()),
			files,
			func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
			false,
		),
	)
	tools = append(tools,
		lookup.New(func() bool { return true }, allowApproval, sessionGoldenSearcher{}),
		fetch.New(func() bool { return true }, allowApproval, saveTestHTML),
	)
	log := testLog(t)

	chat := &App{
		agent:     agent.New("", quietProvider{}, tools),
		screen:    screen,
		workspace: workspace,
		recorder:  record.New(log),
	}
	chat.display.pictures = pictures.Display{
		SessionDirectory: sessionDirectoryWithPictures(t),
		CellWidth:        replayCellWidth,
		CellHeight:       replayCellHeight,
	}

	return &replayRig{
		written:     &written,
		workspace:   workspace,
		sessionName: log.Name(),
		chat:        chat,
	}
}

const (
	replayCellWidth  = 10
	replayCellHeight = 20
)

func sessionDirectoryWithPictures(t *testing.T) string {
	t.Helper()

	sessionDirectory := t.TempDir()

	if err := os.CopyFS(
		pictures.GetDirectory(sessionDirectory),
		os.DirFS(filepath.Join("testdata", "input", "pictures")),
	); err != nil {
		t.Fatal(err)
	}

	return sessionDirectory
}

func (self *replayRig) load(entries []replayEntry) {
	for _, entry := range entries {
		self.chat.recordedEvents = append(self.chat.recordedEvents, *entry.Event)
	}
}

func (self *replayRig) drawn() string {
	drawn := strings.ReplaceAll(self.written.String(), self.workspace.GetDir(), workspaceMarker)
	return strings.ReplaceAll(drawn, self.sessionName, "tame-impala")
}

func replayAtWidth(t *testing.T, entries []replayEntry, columns int) string {
	t.Helper()

	return replayInto(newReplayRig(t, columns), entries)
}

func replayPlainly(t *testing.T, entries []replayEntry) string {
	t.Helper()

	return replayInto(newPlainRig(t), entries)
}

func replayAsPrinted(t *testing.T, entries []replayEntry) string {
	t.Helper()

	rig := newPrintedRig(t)
	rig.chat.runMode.isPrinting = true
	drawn := replayInto(rig, entries)
	requireNothingWasDrawnOver(t, drawn)

	return drawn
}

func replayInto(rig *replayRig, entries []replayEntry) string {
	rig.load(entries)
	rig.chat.replay()

	return rig.drawn()
}

func streamIntoBuffer(t *testing.T, entries []replayEntry, streamingMode output.StreamingMode) string {
	t.Helper()

	return streamIntoBufferAtWidth(t, entries, streamingMode, replayColumns)
}

func streamIntoBufferAtWidth(
	t *testing.T,
	entries []replayEntry,
	streamingMode output.StreamingMode,
	columns int,
) string {
	t.Helper()

	rig := newReplayRig(t, columns)
	rig.chat.display.streamingMode = streamingMode

	return streamThrough(t, rig, entries)
}

func streamThrough(t *testing.T, rig *replayRig, entries []replayEntry) string {
	t.Helper()

	rig.chat.currentTurn = Turn{Stream: testRunningTurnStream(), painter: rig.chat.newPainter(true)}
	rig.chat.screen.ReportProgress(true)

	for _, entry := range entries {
		event := *entry.Event
		if event.Kind == agent.ModelMessageEvent || event.Kind == agent.ModelReasoningEvent {
			for piece := range deltaSized(event.Text) {
				rig.chat.currentTurn.painter.DrawDelta(agent.Delta{Kind: event.Kind, Text: piece})
			}
		}

		rig.chat.recordedEvents = append(rig.chat.recordedEvents, event)
		rig.chat.currentTurn.painter.DrawEvent(event)

		if rig.chat.currentTurn.painter.Stale() {
			rig.chat.redraw()
		}
	}

	rig.chat.currentTurn.painter.Close(dynamic.Done)
	if rig.chat.currentTurn.painter.Stale() {
		rig.chat.redraw()
	}
	rig.chat.screen.End()
	rig.chat.screen.ReportProgress(false)

	return rig.drawn()
}

func deltaSized(text string) iter.Seq[string] {
	return func(yield func(string) bool) {
		runes := []rune(text)

		for at := 0; at < len(runes); at += deltaRunes {
			if !yield(string(runes[at:min(at+deltaRunes, len(runes))])) {
				return
			}
		}
	}
}

const deltaRunes = 4

func replayUnderFooter(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry) string {
	t.Helper()

	rig := openRig(t)

	for range 2 {
		rig.chat.screen.Footer([]string{footerPrompt, "> and a second row"}, 1, 0)
	}

	rig.load(entries)
	rig.chat.replay()

	return rig.drawn()
}

const footerPrompt = "> ask something else"

func replayThenRedraw(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry, isRunning bool) string {
	t.Helper()

	rig := openRig(t)
	rig.chat.currentTurn.Stream = testTurnStreamForRunning(isRunning)
	rig.load(entriesWhile(entries, isRunning))
	rig.chat.replay()
	rig.chat.redraw()

	if isRunning {
		rig.chat.currentTurn.painter.Close(dynamic.Cancelled)
	}

	return rig.drawn()
}

func entriesWhile(entries []replayEntry, isRunning bool) []replayEntry {
	if !isRunning {
		return entries
	}

	return entriesUpToFirstCall(entries)
}

func replayThenRelease(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry, shouldKeep bool) string {
	t.Helper()

	rig := openRig(t)
	rig.chat.screen.ReportProgress(true)
	rig.chat.screen.ReportProgress(true)
	rig.load(entries)
	rig.chat.replay()
	rig.chat.screen.Footer([]string{footerPrompt}, 0, len(footerPrompt))
	rig.chat.screen.Release(shouldKeep)

	return rig.drawn()
}

func replayThenRefreshProgress(t *testing.T, openRig func(*testing.T) *replayRig, entries []replayEntry) string {
	t.Helper()

	rig := openRig(t)
	rig.chat.screen.ReportProgress(true)
	rig.load(entries)
	rig.chat.replay()
	rig.chat.screen.Footer([]string{footerPrompt}, 0, len(footerPrompt))
	rig.chat.screen.RefreshProgress()

	return rig.drawn()
}

func replayWhileRunning(t *testing.T, entries []replayEntry) string {
	t.Helper()

	var drawn string

	synctest.Test(t, func(t *testing.T) {
		rig := newReplayRig(t, replayColumns)
		rig.chat.currentTurn.Stream = testRunningTurnStream()
		rig.load(entriesUpToFirstCall(entries))
		rig.chat.replay()

		time.Sleep(revealAndSomeFrames)
		synctest.Wait()

		drawn = rig.drawn()

		rig.chat.currentTurn.painter.Close(dynamic.Cancelled)
	})

	return drawn
}

func replayAfterAQuestion(t *testing.T, entries []replayEntry) string {
	t.Helper()

	var drawn string

	synctest.Test(t, func(t *testing.T) {
		rig := newReplayRig(t, replayColumns)
		rig.chat.currentTurn.Stream = testRunningTurnStream()
		rig.load(entriesUpToFirstCall(entries))
		rig.chat.replay()

		time.Sleep(revealAndSomeFrames)
		synctest.Wait()

		broker := ask.New()
		closeBroker := broker.Open()
		defer closeBroker()

		result := make(chan error, 1)
		go func() {
			result <- ask.Confirm(t.Context(), broker, ask.Confirmation{
				Label:    "Run this command with host networking?",
				Detail:   "grep prompt *.go",
				Language: "bash",
			})
		}()
		<-broker.Changes()

		rig.chat.question.broker = broker
		rig.chat.onQuestionChange()
		rig.chat.currentTurn.painter.DrawEvent(agent.Event{
			Kind:              agent.ToolCallRequestEvent,
			ID:                "call-during-question",
			Name:              "read",
			FallbackRendering: agent.FallbackRendering{Subject: "main.go", ReadOnly: true},
		})

		time.Sleep(deliberation)
		synctest.Wait()

		rig.chat.answerQuestion(key.Key{Code: key.Rune, Value: 'y'})
		if err := <-result; err != nil {
			t.Fatal(err)
		}

		time.Sleep(carryingOn)
		synctest.Wait()

		drawn = rig.drawn()

		rig.chat.currentTurn.painter.Close(dynamic.Cancelled)
	})

	return drawn
}

const (
	revealAndSomeFrames = 7 * time.Second
	carryingOn          = 8 * time.Second
)

func entriesUpToFirstCall(entries []replayEntry) []replayEntry {
	for at, entry := range entries {
		if entry.Event != nil && entry.Event.Kind == agent.ToolCallRequestEvent {
			return entries[:at+1]
		}
	}

	return entries
}

func layOutWorkspace(t *testing.T) *work.Space {
	t.Helper()

	workspaceDir := filepath.Join(t.TempDir(), "workspace")

	t.Setenv("HOME", workspaceDir)

	if err := os.CopyFS(workspaceDir, os.DirFS(filepath.Join("testdata", "input", "workspace"))); err != nil {
		t.Fatal(err)
	}

	return openTestWorkspace(t, workspaceDir)
}

func TestALiveTurnLeavesTheSameScreenAsAReplayOfIt(t *testing.T) {
	for _, journal := range everyJournal(t) {
		t.Run(journal.name, func(t *testing.T) {
			entries := readJournal(t, journal.path)

			for _, columns := range []int{replayColumns, narrowColumns, tinyColumns} {
				replayed := replayAtWidth(t, entries, columns)

				for name, streamingMode := range everyStreamingMode() {
					requireSameVisibleScreenInColumns(
						t,
						"a live "+name+" turn and a replay of it left different screens",
						columns,
						replayed,
						streamIntoBufferAtWidth(t, entries, streamingMode, columns),
					)
				}
			}
		})
	}
}

func TestGoldenTheBannerDrawsWhatItDrewBefore(t *testing.T) {
	passes := map[string]func() string{}

	for _, flags := range []string{"", "r", "rw", "rx", "rxw", "rxwn", "rxwng", "rxwngl", "rn", "rg", "rl"} {
		grantedCaps, err := caps.Parse(flags)
		if err != nil {
			t.Fatal(err)
		}

		for _, isRunning := range []bool{false, true} {
			name := "caps " + flags + " while waiting"
			if isRunning {
				name = "caps " + flags + " while running"
			}

			passes[name] = func() string {
				return drawnOnAStoppedClock(t, func(t *testing.T) string {
					t.Helper()

					time.Sleep(spinnerSoFar)

					held := &App{mode: caps.NewMode(grantedCaps)}
					held.currentTurn.Stream = testTimedTurnStream(isRunning, time.Now().Add(-turnSoFar), time.Now().Add(-idleSoFar))

					built := goldenBarLayout(t, held)

					return renderBar(built, segment.BottomLeft)
				})
			}
		}
	}

	for name, inputTokens := range map[string]int{
		"context usage low":  5000,
		"context usage high": 182_000,
	} {
		passes[name] = func() string {
			return drawnOnAStoppedClock(t, func(t *testing.T) string {
				t.Helper()

				held := &App{
					mode:      caps.NewMode(caps.All()),
					metrics:   metrics.New(metrics.Settings{ContextWindowTokens: 200_000}),
					startedAt: time.Now().Add(-sessionSoFar),
					recordedEvents: []agent.Event{{
						Kind:  agent.ModelMessageEvent,
						Usage: &agent.Usage{InputTokens: inputTokens},
					}},
				}
				held.metrics.Restore(held.recordedEvents, nil)

				built := goldenBarLayout(t, held)

				return renderBar(built, segment.BottomLeft)
			})
		}
	}

	compareWithGolden(t, "banner", ".ansi", passes)
	compareWithGolden(t, "banner", ".screen", shownPasses(t, passes))
}

func TestGoldenTheBarConfiguredByDefaultDrawsWhatItDrewBefore(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	passes := map[string]func() string{}

	for _, isRunning := range []bool{false, true} {
		state := "waiting"
		if isRunning {
			state = "running"
		}

		for _, position := range segment.Positions {
			passes[position.String()+" while "+state] = func() string {
				return drawnOnAStoppedClock(t, func(t *testing.T) string {
					t.Helper()

					time.Sleep(spinnerSoFar)

					held := &App{
						mode:      caps.NewMode(caps.All()),
						metrics:   metrics.New(metrics.Settings{ContextWindowTokens: 200_000}),
						startedAt: time.Now().Add(-sessionSoFar),
						recordedEvents: []agent.Event{{
							Kind:  agent.ModelMessageEvent,
							Usage: &agent.Usage{InputTokens: 42_000},
						}},
					}
					held.metrics.Restore(held.recordedEvents, nil)
					held.currentTurn.Stream = testTimedTurnStream(
						isRunning,
						time.Now().Add(-turnSoFar),
						time.Now().Add(-idleSoFar),
					)

					layout, err := configFrom(t, "").BuildLayout(
						availableSegments(work.At(workspaceMarker), held),
					)
					if err != nil {
						t.Fatal(err)
					}

					return renderBar(layout, position)
				})
			}
		}
	}

	compareWithGolden(t, "default-bar", ".ansi", passes)
	compareWithGolden(t, "default-bar", ".screen", shownPasses(t, passes))
}

func TestGoldenTheStartupLineDrawsWhatItDrewBefore(t *testing.T) {
	bannerInfo := func(sessionName string, localConfig *startup.LocalConfig) startup.Info {
		return startup.Info{
			Session:       sessionName,
			PromptBytes:   740 + 3*1024,
			ProjectSkills: 3,
			GlobalSkills:  1,
			Snippets:      2,
			ToolBytes:     614,
			LocalConfig:   localConfig,
		}
	}

	line := func(sessionName string, columns int, isTextSizingSupported bool) string {
		event := startup.NewEvent(1500*time.Microsecond, bannerInfo(sessionName, nil))
		return startup.RenderEvent(event, columns, isTextSizingSupported)
	}

	compareWithGolden(t, "startup", ".ansi", map[string]func() string{
		"styled": func() string { return line("tame-impala", replayColumns, false) },
	})

	passes := map[string]func() string{}
	for name, columns := range map[string]int{
		"wide":       replayColumns,
		"narrow":     narrowColumns,
		"tiny":       tinyColumns,
		"one column": oneColumn,
	} {
		passes[name] = func() string { return shown(t, line("tame-impala", columns, false), columns) }
	}
	compareWithGolden(t, "startup", ".screen", passes)

	minimumColumns := 0
	for columns := 1; columns <= replayColumns; columns++ {
		if strings.Contains(line("tame-impala", columns, true), "\x1b]66;") {
			minimumColumns = columns
			break
		}
	}
	if minimumColumns == 0 {
		t.Fatal("sized startup banner never fits")
	}

	compareWithGolden(t, "startup-sized", ".ansi", map[string]func() string{
		"wide with emoji":          func() string { return line("tame-impala", replayColumns, true) },
		"minimum width with emoji": func() string { return line("tame-impala", minimumColumns, true) },
		"below minimum width":      func() string { return line("tame-impala", minimumColumns-1, true) },
		"unknown animal":           func() string { return line("brave-tester", replayColumns, true) },
		"retired animal":           func() string { return line(retiredAnimalSession, replayColumns, true) },
		"no session":               func() string { return line("", replayColumns, true) },
		"unsupported protocol":     func() string { return line("tame-impala", replayColumns, false) },
	})
	compareWithGolden(t, "startup-sized", ".screen", map[string]func() string{
		"wide with emoji": func() string {
			return shown(t, line("tame-impala", replayColumns, true), replayColumns)
		},
		"minimum width with emoji": func() string {
			return shown(t, line("tame-impala", minimumColumns, true), minimumColumns)
		},
		"below minimum width": func() string {
			return shown(t, line("tame-impala", minimumColumns-1, true), minimumColumns-1)
		},
	})

	terminalStream := func(columns int, localConfig *startup.LocalConfig) string {
		var stream strings.Builder
		screen := output.NewTerminalOfSize(&stream, columns, replayLines)
		event := startup.NewEvent(1500*time.Microsecond, bannerInfo("tame-impala", localConfig))
		screen.Line(startup.RenderEvent(event, columns, true))
		screen.Line("Following output.")
		screen.End()
		return stream.String()
	}

	wordyConfig := &startup.LocalConfig{Name: "oh.toml", Settings: []string{
		"caps.default",
		"editor.command",
		"model.round_robin",
		"ports.hostname",
		"skills.include",
		"ui.currency",
		"ui.streaming",
	}}

	streamPasses := map[string]func() string{
		"wide then following output":            func() string { return terminalStream(replayColumns, nil) },
		"wrapped details then following output": func() string { return terminalStream(minimumColumns, nil) },
		"fallback then following output":        func() string { return terminalStream(minimumColumns-1, nil) },
		"wrapped local config then following output": func() string {
			return terminalStream(replayColumns, wordyConfig)
		},
	}
	compareWithGolden(t, "startup-sized-output", ".ansi", streamPasses)

	streamColumns := map[string]int{
		"wide then following output":                 replayColumns,
		"wrapped details then following output":      minimumColumns,
		"fallback then following output":             minimumColumns - 1,
		"wrapped local config then following output": replayColumns,
	}
	shownStreamPasses := map[string]func() string{}
	for name, pass := range streamPasses {
		shownStreamPasses[name] = func() string { return shown(t, pass(), streamColumns[name]) }
	}
	compareWithGolden(t, "startup-sized-output", ".screen", shownStreamPasses)
}

func TestGoldenLocalConfigsDrawMegathoroughly(t *testing.T) {
	profiles := map[string]startup.Info{
		"no local config": {},
		"empty local config": {
			LocalConfig: &startup.LocalConfig{Name: "oh.toml"},
		},
		"one local setting": {
			LocalConfig: &startup.LocalConfig{Name: "oh.toml", Settings: []string{"caps.default"}},
		},
		"mixed global and local settings": {
			LocalConfig: &startup.LocalConfig{
				Name:     "oh.toml",
				Settings: []string{"sandbox.write", "snippets.review", "ui.currency"},
			},
		},
		"many local settings": {
			LocalConfig: &startup.LocalConfig{
				Name: "oh.toml",
				Settings: []string{
					"bar.bottom.left",
					"bar.top.center",
					"caps.default",
					"editor.command",
					"model.round_robin",
					"ports.hostname",
					"provider.ollama.host",
					"sandbox.read",
					"sandbox.write",
					"skills.exclude",
					"skills.include",
					"snippets.review",
					"tool.output",
					"ui.currency",
					"ui.grouping",
					"ui.reasoning",
					"ui.streaming",
				},
			},
		},
	}

	render := func(info startup.Info, columns int, isTextSizingSupported bool) string {
		info.Session = "tame-impala"
		info.PromptBytes = 740 + 3*1024
		info.ProjectSkills = 3
		info.GlobalSkills = 1
		info.Snippets = 2
		info.ToolBytes = 614
		event := startup.NewEvent(1500*time.Microsecond, info)
		return startup.RenderEvent(event, columns, isTextSizingSupported)
	}

	ansiPasses := map[string]func() string{}
	for profileName, info := range profiles {
		ansiPasses[profileName+" / ordinary"] = func() string {
			return render(info, replayColumns, false)
		}
		ansiPasses[profileName+" / sized"] = func() string {
			return render(info, replayColumns, true)
		}
	}
	compareWithGolden(t, "startup-local-config", ".ansi", ansiPasses)

	screenPasses := map[string]func() string{}
	for profileName, info := range profiles {
		for widthName, columns := range map[string]int{
			"wide":       replayColumns,
			"narrow":     narrowColumns,
			"tiny":       tinyColumns,
			"one column": oneColumn,
		} {
			screenPasses[profileName+" / ordinary / "+widthName] = func() string {
				return shown(t, render(info, columns, false), columns)
			}
		}
		for widthName, columns := range map[string]int{
			"wide":   replayColumns,
			"narrow": narrowColumns,
		} {
			screenPasses[profileName+" / sized / "+widthName] = func() string {
				return shown(t, render(info, columns, true), columns)
			}
		}
	}
	compareWithGolden(t, "startup-local-config", ".screen", screenPasses)
}

type inputBlockPass struct {
	frame  edit.Frame
	width  int
	mode   runMode
	status []string
}

func TestGoldenTheInputBlockDrawsWhatItDrewBefore(t *testing.T) {
	frames := map[string]edit.Frame{
		"one row": {
			Rows: []string{"> what is the weather"}, Row: 0, Column: 21,
		},
		"reverse history search": {
			Rows: []string{"git diff"}, Row: 0, Column: 8,
			IsSearching: true, SearchQuery: "git",
		},
		"scrolled both ways": {
			Rows: []string{"> the third row", "> the fourth row"}, Row: 1, Column: 16,
			HiddenLinesAbove: 2, HiddenLinesBelow: 7,
		},
	}

	passes := map[string]func() string{}
	shownPassesAtWidth := map[string]func() string{}

	addPass := func(passName string, pass inputBlockPass) {
		frame, width := pass.frame, pass.width
		passes[passName] = func() string {
			return drawnOnAStoppedClock(t, func(t *testing.T) string {
				t.Helper()

				time.Sleep(spinnerSoFar)

				held := &App{mode: caps.NewMode(caps.All()), runMode: pass.mode}
				held.currentTurn.Stream = testTimedTurnStream(true, time.Now().Add(-turnSoFar), time.Time{})

				built := goldenBarLayout(t, held)

				held.display.bar = bar.NewConfiguration(nil, built)

				block := input.Block{
					Top: input.Ruler{
						Left:   held.renderBar(segment.TopLeft, frame),
						Center: held.renderBar(segment.TopCenter, frame),
						Right:  held.renderBar(segment.TopRight, frame),
					},
					Input: frame,
					Bottom: input.Ruler{
						Left:   held.renderBar(segment.BottomLeft, frame),
						Center: held.renderBar(segment.BottomCenter, frame),
						Right:  held.renderBar(segment.BottomRight, frame),
					},
					Status:        pass.status,
					FrameFeedback: len(pass.status) > 0,
					Rule:          held.ruleStyle(),
				}

				rows, cursorRow, cursorColumn := block.Rows(width)

				return fmt.Sprintf(
					"%s\ncursor row %d column %d",
					strings.Join(rows, "\n"), cursorRow, cursorColumn,
				)
			})
		}

		shownPassesAtWidth[passName] = func() string {
			drawn := passes[passName]()

			return shown(t, drawn[:strings.LastIndex(drawn, "\ncursor row ")], width)
		}
	}

	for _, width := range []int{80, 40, 20} {
		for name, frame := range frames {
			addPass(
				fmt.Sprintf("%s at %d columns", name, width),
				inputBlockPass{frame: frame, width: width},
			)
		}
	}

	addPass("one row yolo at 80 columns", inputBlockPass{
		frame: frames["one row"], width: 80, mode: runMode{isYolo: true},
	})
	addPass("one row simulated yolo at 80 columns", inputBlockPass{
		frame: frames["one row"], width: 80, mode: runMode{isYolo: true, isSimulated: true},
	})

	for _, width := range []int{80, 60, 50, 40} {
		addPass(fmt.Sprintf("framed feedback at %d columns", width), inputBlockPass{
			frame:  frames["one row"],
			width:  width,
			status: []string{"Command not found: /unknown"},
		})
	}

	compareWithGolden(t, "inputblock", ".ansi", passes)
	compareWithGolden(t, "inputblock", ".screen", shownPassesAtWidth)
}

func TestGoldenVerticalInputMovementDrawsWhatItDrewBefore(t *testing.T) {
	up := key.Key{Code: key.Up}
	down := key.Key{Code: key.Down}

	passes := map[string]func() string{
		"1 wrapped draft": func() string {
			return verticalInputMovementStream(t)
		},
		"2 up within the draft": func() string {
			return verticalInputMovementStream(t, up)
		},
		"3 up into history": func() string {
			return verticalInputMovementStream(t, up, up)
		},
		"4 down within the draft": func() string {
			return verticalInputMovementStream(t, up, down)
		},
	}

	shownPasses := map[string]func() string{}
	for name, pass := range passes {
		shownPasses[name] = func() string {
			return shown(t, pass(), tinyColumns)
		}
	}

	compareWithGolden(t, "vertical-movement", ".ansi", passes)
	compareWithGolden(t, "vertical-movement", ".screen", shownPasses)
}

func verticalInputMovementStream(t *testing.T, keypresses ...key.Key) string {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, tinyColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	history.Add("earlier")
	inputLine := edit.NewInput(history)
	inputLine.SetText("one two three")
	self.show(inputLine)

	for _, keypress := range keypresses {
		self.handleKeypressAndShowInput(inputLine, history, keypress)
	}

	return screenOutput.String()
}

func TestGoldenReadlineInputBindingsDrawWhatTheyDrewBefore(t *testing.T) {
	control := func(value rune) key.Key {
		return key.Key{Code: key.Rune, Value: value, Mod: key.Ctrl}
	}
	character := func(value rune) key.Key {
		return key.Key{Code: key.Rune, Value: value}
	}

	historyLines := []string{"git status", "just test", "git diff"}
	passes := map[string]func() string{
		"01 ctrl+a": func() string {
			return readlineInputStream(t, nil, "one two", control('a'), character('!'))
		},
		"02 ctrl+e": func() string {
			return readlineInputStream(t, nil, "one two", key.Key{Code: key.Home}, control('e'), character('!'))
		},
		"03 ctrl+b": func() string {
			return readlineInputStream(t, nil, "one two", control('b'), character('!'))
		},
		"04 ctrl+f": func() string {
			return readlineInputStream(t, nil, "one two", key.Key{Code: key.Home}, control('f'), character('!'))
		},
		"05 ctrl+w": func() string {
			return readlineInputStream(t, nil, "one --two", control('w'))
		},
		"06 ctrl+k": func() string {
			return readlineInputStream(t, nil, "one two", key.Key{Code: key.Home}, control('f'), control('k'))
		},
		"07 ctrl+r empty history": func() string {
			return readlineInputStream(t, nil, "unfinished", control('r'))
		},
		"08 ctrl+r query": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'))
		},
		"09 ctrl+r again": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), control('r'))
		},
		"10 ctrl+r backspace": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), key.Key{Code: key.Backspace})
		},
		"11 ctrl+r failed query": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), character('z'))
		},
		"12 ctrl+r erased query": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), key.Key{Code: key.Backspace})
		},
		"13 ctrl+r escape": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), key.Key{Code: key.Escape})
		},
		"14 ctrl+r then ctrl+a": func() string {
			return readlineInputStream(t, historyLines, "unfinished", control('r'), character('g'), character('i'), character('t'), control('a'), character('!'))
		},
	}

	shownPasses := map[string]func() string{}
	for name, pass := range passes {
		shownPasses[name] = func() string {
			return shown(t, pass(), 40)
		}
	}

	compareWithGolden(t, "readline-bindings", ".ansi", passes)
	compareWithGolden(t, "readline-bindings", ".screen", shownPasses)
}

func readlineInputStream(t *testing.T, historyLines []string, text string, keypresses ...key.Key) string {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, 40, replayLines)

	history := edit.NewHistory("", historyLimit)
	for _, line := range historyLines {
		history.Add(line)
	}
	inputLine := edit.NewInput(history)
	inputLine.SetText(text)
	self.show(inputLine)

	for _, keypress := range keypresses {
		self.handleKeypressAndShowInput(inputLine, history, keypress)
	}

	return screenOutput.String()
}

const structuredThought = "## A heading\n\n- first\n- second\n"

func drawAThoughtThenACall(self *App, thought string) {
	picasso := self.newPainter(false)
	picasso.DrawEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: thought})
	picasso.DrawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                thought,
		Name:              "read",
		FallbackRendering: agent.FallbackRendering{Subject: "one.go"},
	})
	picasso.Close(dynamic.Done)
}

func TestReloadingConfigChangesTheReasoningRenderingForTheNextTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[ui]\nreasoning = \"plain\"\n")

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	prepareLiveConfig(t, self, path)
	if self.display.reasoningRendering != output.ReasoningPlain {
		t.Fatalf("initial reasoning rendering is %d, want plain", self.display.reasoningRendering)
	}
	drawAThoughtThenACall(self, structuredThought)
	if drawn := style.Plain(screenOutput.String()); !strings.Contains(drawn, "A heading • first • second") {
		t.Errorf("a flattened thought drew %q, want it on one line", drawn)
	}

	writeLiveConfig(t, path, "[ui]\nreasoning = \"markdown\"\n")
	settleLiveConfig(t, self)
	if self.display.reasoningRendering != output.ReasoningMarkdown {
		t.Errorf("reloaded reasoning rendering is %d, want markdown", self.display.reasoningRendering)
	}

	screenOutput.Reset()
	drawAThoughtThenACall(self, structuredThought)
	if drawn := style.Plain(screenOutput.String()); !strings.Contains(drawn, "A heading\n\n• first\n• second") {
		t.Errorf("the next turn drew %q, still flattening the thought", drawn)
	}
}

func TestReloadingConfigChangesTheGroupingStraightAway(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[ui]\ngrouping = [\"notice\", \"reasoning tool\", \"answer\"]\n")

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	prepareLiveConfig(t, self, path)

	drawAThoughtThenACall(self, "before")
	if drawn := style.Plain(screenOutput.String()); !strings.Contains(drawn, "before\nread one.go") {
		t.Errorf("a thought and its call drew %q, want them running together", drawn)
	}

	writeLiveConfig(t, path, "[ui]\ngrouping = [\"notice\", \"reasoning\", \"tool\", \"answer\"]\n")
	settleLiveConfig(t, self)

	screenOutput.Reset()
	drawAThoughtThenACall(self, "after")
	if drawn := style.Plain(screenOutput.String()); !strings.Contains(drawn, "after\n\nread one.go") {
		t.Errorf("a thought and its call drew %q, want a blank row between them", drawn)
	}
}

func writeLiveConfig(t *testing.T, path string, body string) {
	t.Helper()

	contents := fmt.Sprintf("version = %d\n%s", config.Format, undentConfig(body))
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeSnippetFile(t *testing.T, path string, prompt string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(prompt+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func prepareLiveConfig(t *testing.T, self *App, path string) {
	t.Helper()
	prepareLiveConfigSources(t, self, config.Source{Path: path})
}

func prepareLiveConfigSources(t *testing.T, self *App, sources ...config.Source) {
	t.Helper()

	settings, observer, err := config.ObserveSources(sources...)
	if err != nil {
		t.Fatal(err)
	}
	self.configObserver = observer
	t.Cleanup(observer.Close)
	registry := availableSegments(work.At(workspaceMarker), self)
	live, err := settings.BuildLive(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := self.slash.commands.ReplaceCommandSet(live.SnippetCommandSet); err != nil {
		t.Fatal(err)
	}
	if self.editorConfig == nil {
		self.editorConfig = editor.NewConfiguration(live.EditorCommand)
	} else {
		self.editorConfig.ReplaceCommand(live.EditorCommand)
	}
	if self.toolOutputLimit == nil {
		self.toolOutputLimit = truncate.NewLimit(live.ToolOutputBytes)
	} else {
		self.toolOutputLimit.Replace(live.ToolOutputBytes)
	}
	if self.experimental == nil {
		self.experimental = experimental.New(live.Experimental)
	} else {
		self.experimental.Replace(live.Experimental)
	}
	self.continueMessage = live.ContinueMessage
	self.display.streamingMode = live.StreamingMode
	self.display.reasoningRendering = live.ReasoningRendering
	self.display.theme = live.Theme
	style.ApplyTheme(live.Theme)
	self.screen.SetGrouping(live.Grouping)
	self.display.bar = bar.NewConfiguration(registry, live.SegmentLayout)
}

func settleLiveConfig(t *testing.T, self *App) {
	t.Helper()

	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case failure, isOpen := <-self.configObserver.Changes():
			if !isOpen {
				t.Fatal("config watch closed before reporting the change")
			}
			if self.reloadConfig(failure) {
				return
			}
		case <-timeout.C:
			t.Fatal("timed out waiting for a config change")
		}
	}
}

func TestTheReloadConfirmationNamesEveryFileAndWhatItChanged(t *testing.T) {
	cases := map[string]struct {
		changes []config.SourceChange
		want    string
	}{
		"one setting": {
			changes: []config.SourceChange{{Path: "oh.toml", Settings: []string{"editor.command"}}},
			want:    "Configuration reloaded automatically\noh.toml: editor.command",
		},
		"several files": {
			changes: []config.SourceChange{
				{Path: "config.toml", Settings: []string{"ui.theme.dim"}},
				{Path: "oh.toml", Settings: []string{"snippets.fix", "tool.output"}},
			},
			want: "Configuration reloaded automatically\n" +
				"config.toml: ui.theme.dim\n" +
				"oh.toml: snippets.fix, tool.output",
		},
		"file gone": {
			changes: []config.SourceChange{
				{Path: "oh.toml", Settings: []string{"ui.streaming"}, IsRemoved: true},
			},
			want: "Configuration reloaded automatically\noh.toml: gone, dropping ui.streaming",
		},
		"nothing of consequence": {
			changes: []config.SourceChange{{Path: "oh.toml"}},
			want:    "Configuration reloaded automatically\noh.toml: no setting changed",
		},
		"a grant this run will never read": {
			changes: []config.SourceChange{
				{Path: "config.toml", Settings: []string{"sandbox.read", "sandbox.write"}},
			},
			want: "Configuration reloaded automatically\n" +
				"config.toml: sandbox.read, sandbox.write\n" +
				"sandbox.read, sandbox.write land when oh next starts",
		},
		"a grant withdrawn is no more immediate": {
			changes: []config.SourceChange{
				{Path: "config.toml", Settings: []string{"sandbox.read"}, IsRemoved: true},
			},
			want: "Configuration reloaded automatically\n" +
				"config.toml: gone, dropping sandbox.read\n" +
				"sandbox.read lands when oh next starts",
		},
		"both waits beside a setting that landed": {
			changes: []config.SourceChange{
				{Path: "config.toml", Settings: []string{
					"caps.default",
					"model.round_robin",
					"sandbox.exec",
					"ui.streaming",
				}},
			},
			want: "Configuration reloaded automatically\n" +
				"config.toml: caps.default, model.round_robin, sandbox.exec, ui.streaming\n" +
				"sandbox.exec lands when oh next starts\n" +
				"caps.default, model.round_robin land in a new session",
		},
		"one setting named by two files is said once": {
			changes: []config.SourceChange{
				{Path: "config.toml", Settings: []string{"skills.include"}},
				{Path: "oh.toml", Settings: []string{"skills.include"}},
			},
			want: "Configuration reloaded automatically\n" +
				"config.toml: skills.include\n" +
				"oh.toml: skills.include\n" +
				"skills.include lands when oh next starts",
		},
	}

	for name, testCase := range cases {
		if got := reloadConfirmation(testCase.changes); got != testCase.want {
			t.Errorf("%s drew %q, want %q", name, got, testCase.want)
		}
	}
}

func TestReloadingAThemeClearsScrollbackAndReplaysTheWholeConversation(t *testing.T) {
	stream := themeReloadStream(t)

	if !strings.Contains(stream, "\x1b[H\x1b[2J\x1b[3J") {
		t.Errorf("theme reload did not clear the screen and scrollback: %q", stream)
	}
	plain := style.Plain(stream)
	for _, text := range []string{"read it", "model answer"} {
		if !strings.Contains(plain, text) {
			t.Errorf("replayed conversation does not contain %q: %q", text, plain)
		}
	}
	if !strings.Contains(stream, "\x1b[48;2;1;2;3m") {
		t.Errorf("replayed conversation did not use the reloaded user background: %q", stream)
	}
}

func TestGoldenReloadingAThemeReplaysTheWholeConversation(t *testing.T) {
	passes := map[string]func() string{
		"replayed conversation":        func() string { return themeReloadStream(t) },
		"live turn and input draft":    func() string { return activeThemeReloadStream(t) },
		"inherited theme restored":     func() string { return inheritedThemeReloadStream(t) },
		"invalid theme left untouched": func() string { return invalidThemeReloadStream(t) },
		"every palette role":           func() string { return themePaletteStream(t) },
		"decorated palette roles":      func() string { return decoratedThemeStream(t) },
	}
	compareWithGolden(t, "theme-reload", ".ansi", passes)
	compareWithGolden(t, "theme-reload", ".screen", shownPasses(t, passes))
}

func themeReloadStream(t *testing.T) string {
	t.Helper()
	restoreTheme := style.ApplyTheme(style.DefaultTheme())
	defer restoreTheme()

	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "")

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	prepareLiveConfig(t, self, path)
	self.recordedEvents = []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "read it"},
		{Kind: agent.ModelMessageEvent, Text: "model answer"},
	}
	self.redraw()
	screenOutput.Reset()

	writeLiveConfig(t, path, `
		[ui.theme]
		user = "#010203"
	`)
	settleLiveConfig(t, self)

	return screenOutput.String()
}

func activeThemeReloadStream(t *testing.T) string {
	t.Helper()
	restoreTheme := style.ApplyTheme(style.DefaultTheme())
	defer restoreTheme()

	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, `
		[ui]
		streaming = "asap"

		[bar.top]
		left = []
		center = []
		right = []

		[bar.bottom]
		left = []
		center = []
		right = []
	`)

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	self.now = func() time.Time { return time.Unix(0, 0) }
	prepareLiveConfig(t, self, path)
	self.recordedEvents = []agent.Event{{Kind: agent.UserMessageEvent, Text: "explain it"}}
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: "still thinking"})

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	inputLine.SetText("half written")
	inputLine.Apply(key.Key{Code: key.Left}, true)
	inputLine.Apply(key.Key{Code: key.Left}, true)
	beforeFrame := inputLine.Frame(replayColumns)
	screenOutput.Reset()

	writeLiveConfig(t, path, `
		[ui]
		streaming = "asap"

		[ui.theme]
		dim = "#010203"

		[bar.top]
		left = []
		center = []
		right = []

		[bar.bottom]
		left = []
		center = []
		right = []
	`)
	settleLiveConfig(t, self)
	self.show(inputLine)

	if inputLine.Text() != "half written" {
		t.Errorf("input draft became %q", inputLine.Text())
	}
	afterFrame := inputLine.Frame(replayColumns)
	if afterFrame.Row != beforeFrame.Row || afterFrame.Column != beforeFrame.Column {
		t.Errorf("input cursor moved from %d:%d to %d:%d", beforeFrame.Row, beforeFrame.Column, afterFrame.Row, afterFrame.Column)
	}
	if provisional := self.currentTurn.painter.ProvisionalDelta(); provisional.Text != "still thinking" {
		t.Errorf("provisional reasoning became %q", provisional.Text)
	}

	return screenOutput.String()
}

func inheritedThemeReloadStream(t *testing.T) string {
	t.Helper()
	restoreTheme := style.ApplyTheme(style.DefaultTheme())
	defer restoreTheme()

	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	overridePath := filepath.Join(directory, "oh.toml")
	writeLiveConfig(t, globalPath, "[ui.theme]\nuser = \"#010203\"\n")
	if err := os.WriteFile(overridePath, []byte("[ui.theme]\nuser = \"#040506\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	prepareLiveConfigSources(t, self,
		config.Source{Path: globalPath},
		config.Source{Path: overridePath, IsOverride: true},
	)
	if got := style.User("overridden"); !strings.Contains(got, "\x1b[48;2;4;5;6m") {
		t.Errorf("local theme override was not applied: %q", got)
	}
	self.recordedEvents = []agent.Event{{Kind: agent.UserMessageEvent, Text: "inherited"}}
	self.redraw()
	screenOutput.Reset()

	if err := os.Remove(overridePath); err != nil {
		t.Fatal(err)
	}
	settleLiveConfig(t, self)

	stream := screenOutput.String()
	if !strings.Contains(stream, "\x1b[48;2;1;2;3m") {
		t.Errorf("global theme was not restored: %q", stream)
	}
	return stream
}

func invalidThemeReloadStream(t *testing.T) string {
	t.Helper()
	restoreTheme := style.ApplyTheme(style.DefaultTheme())
	defer restoreTheme()

	path := stableGoldenPath(t, "config.toml", false)
	writeLiveConfig(t, path, "[ui.theme]\nuser = \"#010203\"\n")

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	prepareLiveConfig(t, self, path)
	self.recordedEvents = []agent.Event{{Kind: agent.UserMessageEvent, Text: "unchanged"}}
	self.redraw()
	beforeReload := screenOutput.Len()

	writeLiveConfig(t, path, "[ui.theme]\nuser = \"red\"\n")
	settleLiveConfig(t, self)
	inputLine := edit.NewInput(edit.NewHistory("", historyLimit))
	self.show(inputLine)

	added := screenOutput.String()[beforeReload:]
	if strings.Contains(added, "\x1b[H\x1b[2J\x1b[3J") {
		t.Errorf("invalid theme repainted the conversation: %q", added)
	}
	if got := style.User("unchanged"); !strings.Contains(got, "\x1b[48;2;1;2;3m") {
		t.Errorf("invalid theme replaced the active palette: %q", got)
	}

	return strings.ReplaceAll(screenOutput.String(), path, "config.toml")
}

func themePaletteStream(t *testing.T) string {
	t.Helper()
	theme := style.DefaultTheme()
	theme.Normal = "#010101"
	theme.Dim = "#020202"
	theme.Accent = "#030303"
	theme.StatusWarning = "#040404"
	theme.StatusSuccess = "#050505"
	theme.StatusInfo = "#060606"
	theme.StatusDanger = "#070707"
	theme.SyntaxType = "#080808"
	theme.SyntaxLiteral = "#090909"
	theme.SyntaxOperator = "#0a0a0a"
	theme.SyntaxKeyword = "#0b0b0b"
	theme.Skill = "#0e0e0e"
	theme.User = "#0c0c0c"
	theme.Harness = "#0d0d0d"
	restoreTheme := style.ApplyTheme(theme)
	defer restoreTheme()

	return strings.Join([]string{
		style.Normal("normal"),
		style.Dim("dim"),
		style.Subject("accent"),
		style.Change("status warning"),
		style.Success("status success"),
		style.Info("status info"),
		style.Failure("status danger"),
		style.Type("syntax type"),
		style.Literal("syntax literal"),
		style.Operator("syntax operator"),
		style.Keyword("syntax keyword"),
		style.Skill("skill"),
		style.User("user background"),
		style.Harness("harness background"),
	}, "\r\n") + "\r\n"
}

func decoratedThemeStream(t *testing.T) string {
	t.Helper()
	theme := style.DefaultTheme()
	theme.Accent = "#030303 italic"
	theme.Dim = "underline"
	theme.StatusInfo = "#060606 underline:curly underline:#0a0b0c"
	theme.StatusDanger = "#070707 bold strikethrough"
	theme.SyntaxType = "faint overline"
	theme.User = "#0c0c0c italic underline"
	restoreTheme := style.ApplyTheme(theme)
	defer restoreTheme()

	return strings.Join([]string{
		style.Subject("italic accent"),
		style.Dim("underlined terminal default"),
		style.Info("curly coloured underline"),
		style.Failure("bold strikethrough"),
		style.Type("faint overline"),
		style.User("decorated user background"),
	}, "\r\n") + "\r\n"
}

func TestReloadingConfigChangesTheContinueMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, `
		[input]
		continue = "first"
	`)

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	prepareLiveConfig(t, self, path)

	writeLiveConfig(t, path, `
		[input]
		continue = "second"
	`)
	settleLiveConfig(t, self)
	writeLiveConfig(t, path, `
		[input]
		continue = "carry on from the reloaded config"
	`)
	settleLiveConfig(t, self)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.apply(inputLine, history, key.Key{Code: key.Enter})
	self.apply(inputLine, history, key.Key{Code: key.Enter})

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	hasSentReloadedMessage := false
	for _, event := range self.recordedEvents {
		if event.Kind == agent.UserMessageEvent && event.Text == "carry on from the reloaded config" {
			hasSentReloadedMessage = true
		}
	}
	if self.feedback.Message().Status == agent.ErrorStatus {
		t.Errorf("successful reload complained: %+v", self.feedback.Message())
	}
	if !hasSentReloadedMessage {
		t.Error("the reloaded message was not sent")
	}
}

func TestReloadingConfigChangesTheStreamingModeForTheNextTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[ui]\nstreaming = \"asap\"\n")

	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)
	if self.display.streamingMode != output.StreamingModeASAP {
		t.Fatalf("initial streaming mode is %d, want asap", self.display.streamingMode)
	}

	writeLiveConfig(t, path, "[ui]\nstreaming = \"paced\"\n")
	settleLiveConfig(t, self)
	if self.display.streamingMode != output.StreamingModePaced {
		t.Errorf("reloaded streaming mode is %d, want paced", self.display.streamingMode)
	}
}

func TestReloadingConfigKeepsTheChangeAndWarnsAboutASettingNothingReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[ui]\nstreaming = \"asap\"\n")

	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)

	writeLiveConfig(t, path, "[ui]\nstreaming = \"paced\"\nmystery = \"loudly\"\n")
	settleLiveConfig(t, self)

	if self.display.streamingMode != output.StreamingModePaced {
		t.Errorf("reloaded streaming mode is %d, want paced", self.display.streamingMode)
	}
	message := self.feedback.Message()
	if message.Status != agent.WarningStatus {
		t.Errorf("feedback status is %q, want a warning", message.Status)
	}
	if !strings.Contains(message.Text, "mystery") {
		t.Errorf("feedback %q does not name the setting", message.Text)
	}
}

func TestDeletingALocalConfigLiveReloadsTheGlobalFallback(t *testing.T) {
	directory := t.TempDir()
	globalPath := filepath.Join(directory, "config.toml")
	writeLiveConfig(t, globalPath, "[ui]\nstreaming = \"asap\"\n")
	overridePath := filepath.Join(directory, "oh.toml")
	if err := os.WriteFile(overridePath, []byte("[ui]\nstreaming = \"paced\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfigSources(t, self,
		config.Source{Path: globalPath},
		config.Source{Path: overridePath, IsOverride: true},
	)
	if self.display.streamingMode != output.StreamingModePaced {
		t.Fatalf("initial streaming mode is %d, want paced", self.display.streamingMode)
	}
	if err := os.Remove(overridePath); err != nil {
		t.Fatal(err)
	}
	settleLiveConfig(t, self)
	if self.display.streamingMode != output.StreamingModeASAP {
		t.Errorf("reloaded streaming mode is %d, want global asap", self.display.streamingMode)
	}
}

func TestReloadingConfigChangesTheEditorAndToolOutputLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, `
		[editor]
		command = ["first-editor", "--wait"]

		[tool]
		output = "2K"
	`)

	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)

	writeLiveConfig(t, path, `
		[editor]
		command = "second-editor"

		[tool]
		output = "4K"
	`)
	settleLiveConfig(t, self)

	if got := self.editorConfig.GetCommand(); !slices.Equal(got, editor.Command{"second-editor"}) {
		t.Errorf("reloaded editor command is %v", got)
	}
	if got := self.toolOutputLimit.GetBytes(); got != 4*1024 {
		t.Errorf("reloaded tool output limit is %d", got)
	}
}

func TestReloadingConfigChangesExperimentalToggles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[experimental]\nquiet_rounds = true\n")

	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)

	if !self.experimental.IsEnabled("quiet_rounds") {
		t.Fatal("the toggle was not read from the configuration")
	}

	writeLiveConfig(t, path, "[experimental]\nquiet_rounds = false\n")
	settleLiveConfig(t, self)

	if self.experimental.IsEnabled("quiet_rounds") {
		t.Error("the reloaded toggle is still enabled")
	}
}

func TestReloadingConfigReplacesSnippetsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[snippets]\nold = \"Keep the old snippet.\"\n")

	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)

	writeLiveConfig(t, path, "[snippets]\nbroken = \"{{\"\n")
	settleLiveConfig(t, self)
	if _, found := self.slash.commands.Find("//old"); !found {
		t.Error("an invalid revision replaced the working snippets")
	}
	if _, found := self.slash.commands.Find("//broken"); found {
		t.Error("the invalid snippet was registered")
	}

	writeLiveConfig(t, path, "[snippets]\nnew = \"Use the new snippet.\"\n")
	settleLiveConfig(t, self)
	if _, found := self.slash.commands.Find("//old"); found {
		t.Error("the old snippet survived a valid replacement")
	}
	if _, found := self.slash.commands.Find("//new"); !found {
		t.Error("the reloaded snippet was not registered")
	}

	inputLine := edit.NewInput(nil)
	inputLine.SetText("//ne")
	self.apply(inputLine, nil, key.Key{Code: key.Rune, Value: '\t'})
	if inputLine.Text() != "//new" {
		t.Errorf("reloaded completion is %q", inputLine.Text())
	}
}

func TestGoldenWorkspacePathsInCallLabelsLoseTheirPrefix(t *testing.T) {
	passes := map[string]func() string{
		"paths named against the workspace": func() string { return drawWorkspacePathLabels(t) },
	}

	compareWithGolden(t, "workspace-paths", ".ansi", passes)
	compareWithGolden(t, "workspace-paths", ".screen", shownPasses(t, passes))
}

func drawWorkspacePathLabels(t *testing.T) string {
	t.Helper()

	rig := newWideRig(t)
	workspaceDir := rig.workspace.GetDir()

	calls := []struct {
		name      string
		arguments string
	}{
		{"read", fmt.Sprintf(`{"path":%q}`, filepath.Join(workspaceDir, "named-in-full.md"))},
		{"read", `{"path":"~/named-by-tilde.md"}`},
		{"read", fmt.Sprintf(`{"path":%q}`, workspaceDir)},
		{"read", `{"path":"~"}`},
		{"read", `{"path":"/etc/hosts"}`},
		{"grep", fmt.Sprintf(`{"pattern":"in-full","path":%q,"glob":"**/*.go"}`, workspaceDir)},
		{"grep", `{"pattern":"by-tilde","path":"~","glob":"**/*.md"}`},
	}

	for at, call := range calls {
		id := strconv.Itoa(at)
		rig.chat.recordedEvents = append(
			rig.chat.recordedEvents,
			agent.Event{
				Kind:      agent.ToolCallRequestEvent,
				ID:        id,
				Name:      call.name,
				Arguments: call.arguments,
			},
			agent.Event{
				Kind:   agent.ToolCallResultEvent,
				Status: agent.SuccessStatus,
				ID:     id,
				Name:   call.name,
			},
		)
	}

	rig.chat.replay()

	return rig.drawn()
}

func TestGoldenAnExistingPathDrawsAsAConversationMessage(t *testing.T) {
	passes := map[string]func() string{
		"existing path sent": func() string { return drawExistingPathMessage(t) },
	}

	compareWithGolden(t, "path-message", ".ansi", passes)
	compareWithGolden(t, "path-message", ".screen", shownPasses(t, passes))
}

func drawExistingPathMessage(t *testing.T) string {
	t.Helper()

	var screenOutput strings.Builder
	self := slashCommandFixture(t, caps.Read)
	self.agent = agent.New("", quietProvider{}, nil)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	self.slash.commands = fixtureCommandRegistry(t)
	self.workspace = work.At(t.TempDir())

	inputLine := edit.NewInput(nil)
	self.inputLine = inputLine
	inputLine.SetText("/etc/hosts")
	self.show(inputLine)
	self.acceptInput(inputLine, edit.NewHistory("", historyLimit))
	self.waitForCurrentTurn()
	self.show(inputLine)

	return screenOutput.String()
}

func TestGoldenUserMessagePathsAreLinkedAtConversationWidths(t *testing.T) {
	passes := map[string]func() string{}
	columnsByName := map[string]int{
		"wide":       replayColumns,
		"narrow":     narrowColumns,
		"tiny":       tinyColumns,
		"one column": oneColumn,
	}
	for name, columns := range columnsByName {
		passes[name] = func() string { return drawUserPathLinks(t, columns) }
	}

	compareWithGolden(t, "user-path-links", ".ansi", passes)

	screenPasses := map[string]func() string{}
	for name, pass := range passes {
		columns := columnsByName[name]
		screenPasses[name] = func() string { return shown(t, pass(), columns) }
	}
	compareWithGolden(t, "user-path-links", ".screen", screenPasses)
}

func drawUserPathLinks(t *testing.T, columns int) string {
	t.Helper()

	rig := newReplayRig(t, columns)
	if err := os.WriteFile(filepath.Join(rig.workspace.GetDir(), "spaced target.txt"), nil, 0o600); err != nil {
		t.Fatalf("prepare spaced path: %v", err)
	}
	rig.chat.recordedEvents = []agent.Event{{
		Kind: agent.UserMessageEvent,
		Text: "read target.txt and spaced target.txt and cmd/oh/line/render.go:12:3; also /etc/hosts and [the target](https://example.test).",
	}}
	rig.chat.replay()

	return rig.drawn()
}

type feedbackScenario int

const (
	feedbackCommandError feedbackScenario = iota
	feedbackHelp
	feedbackStartupInfo
	feedbackSuccess
	feedbackClearedByEditing
	feedbackClearedByEscape
	feedbackClearedByBackspace
	feedbackClearedByControlD
	feedbackSurvivingADismissKey
	feedbackClearedByTurnCompletion
	feedbackStorageWarnings
	feedbackUnknownSettings
	feedbackTallAnswer
	feedbackNetworkApproval
	feedbackConcurrentApproval
	feedbackChainedApproval
	feedbackHeredocApproval
	feedbackTallApproval
	feedbackApprovalDuringACall
	feedbackDeclaredToolApproval
)

func TestConfirmationFeedbackSchedulesItsOwnDismissal(t *testing.T) {
	self := &App{mode: caps.NewMode(caps.All())}
	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	self.now = func() time.Time { return base }
	self.showFeedback(feedback.Confirmation, feedback.Message{
		Text:         "done",
		Status:       agent.SuccessStatus,
		DismissAfter: configReloadConfirmationDuration,
	})

	dismissesAt := base.Add(configReloadConfirmationDuration)

	if got, want := self.nextRefresh(base), base.Add(time.Second); !got.Equal(want) {
		t.Errorf("next refresh = %s, want the next countdown tick %s", got, want)
	}

	if got := self.nextRefresh(dismissesAt.Add(-500 * time.Millisecond)); !got.Equal(dismissesAt) {
		t.Errorf("next refresh = %s, want the dismissal time once within the last second", got)
	}
}

func TestCommandFeedbackHasNoAutomaticDismissal(t *testing.T) {
	self := &App{mode: caps.NewMode(caps.All())}
	self.showFeedback(feedback.Command, feedback.Message{Text: "done", Status: agent.SuccessStatus})

	if got := self.nextRefresh(time.Now()); !got.IsZero() {
		t.Errorf("next refresh = %s, want none scheduled", got)
	}
}

func TestGoldenFeedbackDrawsEveryVisibleState(t *testing.T) {
	passes := streamPasses(t, feedbackStream, map[string]feedbackScenario{
		"command error":                     feedbackCommandError,
		"multiline help":                    feedbackHelp,
		"startup info":                      feedbackStartupInfo,
		"success confirmation":              feedbackSuccess,
		"editing clears feedback":           feedbackClearedByEditing,
		"escape clears feedback":            feedbackClearedByEscape,
		"backspace clears feedback":         feedbackClearedByBackspace,
		"ctrl+d clears feedback":            feedbackClearedByControlD,
		"a warning survives escape":         feedbackSurvivingADismissKey,
		"turn completion clears it":         feedbackClearedByTurnCompletion,
		"combined storage warnings":         feedbackStorageWarnings,
		"settings nothing reads":            feedbackUnknownSettings,
		"tall answer stays untouched":       feedbackTallAnswer,
		"network approval":                  feedbackNetworkApproval,
		"next concurrent approval":          feedbackConcurrentApproval,
		"chained command approval":          feedbackChainedApproval,
		"heredoc approval":                  feedbackHeredocApproval,
		"approval taller than the terminal": feedbackTallApproval,
		"approval during a call":            feedbackApprovalDuringACall,
		"declared tool approval":            feedbackDeclaredToolApproval,
	})

	compareWithGolden(t, "feedback", ".ansi", passes)
	compareWithGolden(t, "feedback", ".screen", shownPasses(t, passes))
	compareWithGolden(t, "feedback", ".txt", map[string]func() string{
		"plain help": func() string { return plainFeedback(t) },
	})
}

type feedbackFrameScenario struct {
	columns int
	lines   int
	text    string
}

func TestGoldenFeedbackFrameDrawsAtEverySize(t *testing.T) {
	scenarios := map[string]feedbackFrameScenario{
		"narrow wrapping": {
			columns: narrowColumns,
			lines:   replayLines,
			text:    "A feedback message long enough to wrap inside its frame.",
		},
		"minimum frame": {
			columns: input.MinimumFramedFeedbackWidth,
			lines:   replayLines,
			text:    "ab",
		},
		"below frame threshold": {
			columns: input.MinimumFramedFeedbackWidth - 1,
			lines:   replayLines,
			text:    "ab",
		},
		"one column": {
			columns: oneColumn,
			lines:   replayLines,
			text:    "x",
		},
		"taller than terminal": {
			columns: narrowColumns,
			lines:   8,
			text:    "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight",
		},
	}

	ansiPasses := make(map[string]func() string, len(scenarios))
	screenPasses := make(map[string]func() string, len(scenarios))
	for name, scenario := range scenarios {
		ansiPasses[name] = func() string { return feedbackFrameStream(t, scenario) }
		screenPasses[name] = func() string {
			return shown(t, feedbackFrameStream(t, scenario), scenario.columns)
		}
	}

	compareWithGolden(t, "feedback-frame", ".ansi", ansiPasses)
	compareWithGolden(t, "feedback-frame", ".screen", screenPasses)
}

func feedbackFrameStream(t *testing.T, scenario feedbackFrameScenario) string {
	t.Helper()

	var screenOutput strings.Builder
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(&screenOutput, scenario.columns, scenario.lines)
	self.showFeedback(feedback.Command, feedback.Message{Text: scenario.text, Status: agent.InfoStatus})
	self.show(edit.NewInput(nil))

	return screenOutput.String()
}

func feedbackStream(t *testing.T, scenario feedbackScenario) string {
	t.Helper()

	var screenOutput strings.Builder
	self := slashCommandFixture(t, caps.Read)
	self.agent = agent.New("", quietProvider{}, nil)
	terminalLines := replayLines
	if scenario == feedbackTallAnswer || scenario == feedbackTallApproval {
		terminalLines = 8
	}
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, terminalLines)
	self.slash.commands = fixtureCommandRegistry(
		t,
		slash.Command{
			Name: "help",
			Run: func(context slash.Context, _ slash.Arguments) error {
				context.Notice("Commands:\n  /conf\n  /copy")
				return nil
			},
		},
		slash.Command{
			Name: "copy",
			Run: func(context slash.Context, _ slash.Arguments) error {
				context.Success("Copied last message to clipboard")
				return nil
			},
		},
		slash.Command{
			Name: "info",
			Run: func(context slash.Context, _ slash.Arguments) error {
				context.PlainNotice(style.Info("active-model") + "  GPT Sol\n" +
					style.Info("mode-toggle") + "   " + style.Subtle("rxw ngl"))
				return nil
			},
		},
	)

	inputLine := edit.NewInput(nil)
	self.inputLine = inputLine

	if typed := typedForFeedback(scenario); typed != "" {
		inputLine.SetText(typed)
	}
	self.show(inputLine)

	switch scenario {
	case feedbackCommandError:
		self.handleCommand("/unknown")
		self.show(inputLine)
	case feedbackHelp:
		self.handleCommand("/help")
		self.show(inputLine)
	case feedbackStartupInfo:
		self.acceptInitialInput(inputLine, edit.NewHistory("", historyLimit), "/info")
	case feedbackSuccess:
		self.handleCommand("/copy")
		self.show(inputLine)
	case feedbackClearedByEditing:
		self.handleCommand("/unknown")
		self.show(inputLine)
		self.handleKeypressAndShowInput(inputLine, nil, key.Key{Code: key.Rune, Value: 'x'})
	case feedbackClearedByEscape, feedbackClearedByBackspace, feedbackClearedByControlD:
		self.handleCommand("/unknown")
		self.show(inputLine)
		self.handleKeypressAndShowInput(inputLine, nil, feedbackDismissKeys[scenario])
	case feedbackSurvivingADismissKey:
		self.notifyFailure("chat.md recording disabled: transcript append failed")
		self.show(inputLine)
		self.handleKeypressAndShowInput(inputLine, nil, key.Key{Code: key.Escape})
	case feedbackClearedByTurnCompletion:
		self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
		self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "still working"})
		self.handleCommand("/unknown")
		self.show(inputLine)
		completed := agent.Event{Kind: agent.ModelMessageEvent, Text: "still working"}
		self.takeTurn(TurnEvent{Update: agent.Update{Event: &completed}})
		self.finish()
		self.show(inputLine)
	case feedbackStorageWarnings:
		self.notifyFailure("chat.md recording disabled: transcript append failed\nwire.http recording disabled: wire append failed")
		self.show(inputLine)
	case feedbackUnknownSettings:
		self.notifyUnknownSettings([]string{
			"config.toml: unknown: ui.mystery",
			"oh.toml: unknown: bar.top.center.loudly",
		})
		self.show(inputLine)
	case feedbackChainedApproval, feedbackHeredocApproval:
		broker := ask.New()
		closeBroker := broker.Open()
		defer closeBroker()
		command := "cd /tmp && curl -sS https://example.com/a/long/path | jq -r .name && rm -rf build || echo failed"
		if scenario == feedbackHeredocApproval {
			command = "cat <<EOF > /tmp/note.txt\n  keep every word\nEOF"
		}
		go func() {
			_ = ask.Confirm(t.Context(), broker, ask.Confirmation{
				Label:    "Run this command with host networking?",
				Detail:   strings.Join(bash.Steps(command), "\n"),
				Language: "bash",
			})
		}()
		<-broker.Changes()
		self.question.broker = broker
		self.onQuestionChange()
		self.show(inputLine)
	case feedbackNetworkApproval:
		broker := ask.New()
		closeBroker := broker.Open()
		defer closeBroker()
		questionContext, cancelQuestion := context.WithDeadline(t.Context(), self.getNow().Add(time.Minute))
		defer cancelQuestion()
		go func() {
			_ = ask.Confirm(questionContext, broker, ask.Confirmation{
				Label:    "Run this command with host networking?",
				Detail:   "curl https://example.com/status",
				Language: "bash",
			})
		}()
		<-broker.Changes()
		self.question.broker = broker
		self.onQuestionChange()
		self.show(inputLine)
	case feedbackConcurrentApproval:
		broker := ask.New()
		closeBroker := broker.Open()
		defer closeBroker()

		firstResult := make(chan error, 1)
		go func() {
			firstResult <- ask.Confirm(t.Context(), broker, ask.Confirmation{
				Label:  "Approve the first operation?",
				Detail: "first operation",
			})
		}()
		<-broker.Changes()
		go func() {
			_ = ask.Confirm(t.Context(), broker, ask.Confirmation{
				Label:  "Approve the second operation?",
				Detail: "second operation",
			})
		}()
		<-broker.Changes()

		self.question.broker = broker
		self.onQuestionChange()
		self.show(inputLine)
		self.answerQuestion(key.Key{Code: key.Rune, Value: 'y'})
		if err := <-firstResult; err != nil {
			t.Fatal(err)
		}
		self.onQuestionChange()
		self.show(inputLine)
	case feedbackTallApproval:
		return drawApprovalTallerThanTheTerminal(t, self, inputLine, &screenOutput)
	case feedbackTallAnswer:
		const answer = "01 alpha\n\n02 bravo\n\n03 charlie\n\n04 delta\n\n05 echo\n\n06 foxtrot\n\n07 golf"
		self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
		self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: answer})
		self.handleCommand("/unknown")
		self.show(inputLine)
	case feedbackApprovalDuringACall:
		return drawApprovalDuringACall(t, self, inputLine, &screenOutput)
	case feedbackDeclaredToolApproval:
		showDeclaredToolApproval(t, self, inputLine)
	}

	return screenOutput.String()
}

func typedForFeedback(scenario feedbackScenario) string {
	return map[feedbackScenario]string{
		feedbackCommandError:            "/unknown",
		feedbackClearedByEditing:        "/unknown",
		feedbackClearedByEscape:         "/unknown",
		feedbackClearedByTurnCompletion: "/unknown",
		feedbackTallAnswer:              "/unknown",
		feedbackHelp:                    "/help",
		feedbackSuccess:                 "/copy",
		feedbackNetworkApproval:         "what is out there?",
		feedbackConcurrentApproval:      "waiting for approvals",
		feedbackChainedApproval:         "fetch and tidy up",
		feedbackTallApproval:            "tidy the tree",
		feedbackHeredocApproval:         "write the note",
		feedbackApprovalDuringACall:     "check what that endpoint says",
		feedbackDeclaredToolApproval:    "leave me a note about the host",
	}[scenario]
}

func showDeclaredToolApproval(t *testing.T, self *App, inputLine *edit.Input) {
	t.Helper()

	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	go func() {
		_ = ask.Confirm(t.Context(), broker, ask.Confirmation{
			Label:    "Run the note tool?",
			Detail:   "/opt/toolbox/notes.py --action add --text 'the toolbox works' --tag oh --tag test",
			Language: "bash",
		})
	}()
	<-broker.Changes()
	self.question.broker = broker
	self.onQuestionChange()
	self.show(inputLine)
}

func drawApprovalTallerThanTheTerminal(
	t *testing.T,
	self *App,
	inputLine *edit.Input,
	screenOutput *strings.Builder,
) string {
	t.Helper()

	steps := make([]string, 0, 12)
	for step := range 12 {
		steps = append(steps, fmt.Sprintf("rm -rf build/step-%02d && make step-%02d", step, step))
	}
	command := strings.Join(steps, " && ")

	self.screen.Line("the conversation this question stands over")

	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	go func() {
		_ = ask.Confirm(t.Context(), broker, ask.Confirmation{
			Label:    "Run this command with host networking?",
			Detail:   strings.Join(bash.Steps(command), "\n"),
			Language: "bash",
		})
	}()
	<-broker.Changes()

	self.question.broker = broker
	self.onQuestionChange()
	self.show(inputLine)

	return screenOutput.String()
}

const deliberation = 20 * time.Second

func drawApprovalDuringACall(
	t *testing.T,
	self *App,
	inputLine *edit.Input,
	screenOutput *strings.Builder,
) string {
	t.Helper()

	var drawn string

	synctest.Test(t, func(t *testing.T) {
		self.agent = agent.New("", quietProvider{}, []tool.Tool{
			bash.New(
				nil,
				func(context.Context) (sandbox.Policy, error) { return sandbox.Policy{}, nil },
				func(context.Context, string) error { return nil },
				sandbox.Direct(),
				true,
			),
		})
		self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
		self.currentTurn.painter.DrawEvent(agent.Event{
			Kind:      agent.ToolCallRequestEvent,
			ID:        "call-1",
			Name:      "bash",
			Arguments: `{"command":"curl https://example.com/status","network":"host"}`,
		})

		time.Sleep(revealAndSomeFrames)
		synctest.Wait()

		broker := ask.New()
		closeBroker := broker.Open()
		defer closeBroker()

		result := make(chan error, 1)
		go func() {
			result <- ask.Confirm(t.Context(), broker, ask.Confirmation{
				Label:    "Run this command with host networking?",
				Detail:   "curl https://example.com/status",
				Language: "bash",
			})
		}()
		<-broker.Changes()

		self.question.broker = broker
		self.onQuestionChange()
		self.show(inputLine)

		time.Sleep(deliberation)
		synctest.Wait()
		self.show(inputLine)

		drawn = screenOutput.String()

		self.answerQuestion(key.Key{Code: key.Rune, Value: 'y'})
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		self.currentTurn.painter.Close(dynamic.Cancelled)
	})

	return drawn
}

type queuedMessagesScenario int

const (
	queuedOne queuedMessagesScenario = iota
	queuedPath
	queuedTwo
	queuedSeveral
	queuedLongMessage
	queuedMultilineMessage
	queuedMarkdownEmphasis
	queuedSnippet
	queuedTakenBack
	queuedTakenBackWhileTyping
	queuedBehindFeedback
	queuedFlushed
	queuedDelivered
	queuedTallerThanTheTerminal
)

func TestGoldenQueuedMessagesDrawEveryVisibleState(t *testing.T) {
	passes := map[string]func() string{
		"one queued":                 func() string { return queuedMessagesStream(t, queuedOne) },
		"a path queued":              func() string { return queuedMessagesStream(t, queuedPath) },
		"two queued":                 func() string { return queuedMessagesStream(t, queuedTwo) },
		"several queued":             func() string { return queuedMessagesStream(t, queuedSeveral) },
		"a snippet queued":           func() string { return queuedMessagesStream(t, queuedSnippet) },
		"a long message elided":      func() string { return queuedMessagesStream(t, queuedLongMessage) },
		"a multiline message":        func() string { return queuedMessagesStream(t, queuedMultilineMessage) },
		"a markdown emphasis":        func() string { return queuedMessagesStream(t, queuedMarkdownEmphasis) },
		"escape takes the last back": func() string { return queuedMessagesStream(t, queuedTakenBack) },
		"taken back while typing":    func() string { return queuedMessagesStream(t, queuedTakenBackWhileTyping) },
		"feedback takes the footer":  func() string { return queuedMessagesStream(t, queuedBehindFeedback) },
		"flushed at a stopping turn": func() string { return queuedMessagesStream(t, queuedFlushed) },
		"delivered at the boundary":  func() string { return queuedMessagesStream(t, queuedDelivered) },
		"taller than the terminal":   func() string { return queuedMessagesStream(t, queuedTallerThanTheTerminal) },
	}

	compareWithGolden(t, "queued-messages", ".ansi", passes)
	compareWithGolden(t, "queued-messages", ".screen", shownPasses(t, passes))
}

func tallQueue() []string {
	messages := make([]string, 20)
	for i := range messages {
		messages[i] = fmt.Sprintf("queued message %d", i+1)
	}

	return messages
}

const longQueuedMessage = "and while you are there please also look at the second path, " +
	"which is the one that has been wrong all along"

func queuedMessagesStream(t *testing.T, scenario queuedMessagesScenario) string {
	t.Helper()

	var screenOutput strings.Builder
	self := slashCommandFixture(t, caps.Read)
	self.agent = agent.New("", quietProvider{}, nil)
	self.workspace = layOutWorkspace(t)
	terminalLines := replayLines
	if scenario == queuedTallerThanTheTerminal {
		terminalLines = 12
	}
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, terminalLines).LinkPathsUnder(
		link.Roots{Workspace: self.workspace.GetDir()},
	)
	self.slash.commands = fixtureCommandRegistryWithSnippets(
		t,
		map[string]snippets.Definition{
			"add": {Prompt: "Add the following:\n\n{{ .Arg }}", Arguments: snippets.ArgumentsRequired},
		},
		slash.Command{
			Name: "help",
			Run: func(context slash.Context, _ slash.Arguments) error {
				context.Notice("Commands:\n  /conf\n  /copy")
				return nil
			},
		},
	)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.currentTurn.painter.DrawEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "look at the first path"})
	self.currentTurn.painter.DrawEvent(agent.Event{
		Kind:              agent.ToolCallRequestEvent,
		ID:                "call-1",
		Name:              "work",
		FallbackRendering: agent.FallbackRendering{Subject: "the first path"},
	})
	self.show(inputLine)

	queued := map[queuedMessagesScenario][]string{
		queuedOne:                   {"check the other path too"},
		queuedPath:                  {"check screen.go too"},
		queuedTwo:                   {"check the other path too", "and mention what you find"},
		queuedSeveral:               {"the first", "the second", "the third", "the fourth", "the fifth"},
		queuedLongMessage:           {longQueuedMessage},
		queuedMultilineMessage:      {"check the other path too\n\n- the first thing\n- the second thing"},
		queuedMarkdownEmphasis:      {"check the *other* path too"},
		queuedTakenBack:             {"check the other path too", "and mention what you find"},
		queuedTakenBackWhileTyping:  {"check the other path too"},
		queuedBehindFeedback:        {"check the other path too"},
		queuedFlushed:               {"check the other path too", "and mention what you find"},
		queuedDelivered:             {"check the other path too"},
		queuedTallerThanTheTerminal: tallQueue(),
	}[scenario]

	for _, message := range queued {
		typeMessage(t, self, inputLine, history, message)
	}
	self.show(inputLine)

	switch scenario {
	case queuedOne, queuedPath, queuedTwo, queuedSeveral, queuedLongMessage, queuedMultilineMessage,
		queuedMarkdownEmphasis, queuedTallerThanTheTerminal:
	case queuedSnippet:
		self.handleCommand("//add review the second path")
		self.show(inputLine)
	case queuedTakenBack:
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Escape})
	case queuedTakenBackWhileTyping:
		for _, value := range "and one more" {
			self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Rune, Value: value})
		}
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Escape})
	case queuedBehindFeedback:
		self.handleCommand("/help")
		self.show(inputLine)
		self.feedback.Clear(feedback.Command)
		self.show(inputLine)
	case queuedFlushed:
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})
	case queuedDelivered:
		delivered, _ := self.currentTurn.TakeInterjections()
		event := agent.Event{Kind: agent.UserMessageEvent, Text: delivered}
		self.takeTurn(TurnEvent{Update: agent.Update{Event: &event}})
		self.show(inputLine)
	}

	return strings.ReplaceAll(screenOutput.String(), self.workspace.GetDir(), workspaceMarker)
}

func plainFeedback(t *testing.T) string {
	t.Helper()

	var screenOutput bytes.Buffer
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&screenOutput)
	self.runMode.isPlain = true
	self.slash.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "help",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Notice("Commands:\n  /help")
			return nil
		},
	})
	self.handleCommand("/help")
	self.screen.End()
	return strings.TrimSuffix(style.Plain(screenOutput.String()), "\n")
}

func TestAPrintedSessionAnswersOneCommandAndStops(t *testing.T) {
	var screenOutput bytes.Buffer
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines).AppendOnly()
	self.runMode.isPrinting = true
	self.slash.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "help",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Notice("Commands:\n  /help")
			return nil
		},
	})

	self.print(edit.NewHistory("", 0), "/help")

	requireNothingWasDrawnOver(t, screenOutput.String())
	if got := style.Plain(screenOutput.String()); got != "Commands:\r\n  /help\r\n" {
		t.Errorf("got %q", got)
	}
}

type configReloadScenario int

const (
	configReloadValid configReloadScenario = iota
	configReloadInvalidRecovery
	configReloadDeletion
	configReloadWatchFailure
	configReloadReplay
	configReloadSettingThatIsNotLive
	configReloadGrantThatWaitsForTheNextRun
	configReloadSnippets
	configReloadSnippetFile
	configReloadSharedSnippetFile
	configReloadSnippetFileTheConfigNames
	configReloadWithoutASettingChange
	configReloadDismissedConfirmation
	configReloadAutoDismissedConfirmation
	configReloadPreservedFailure
)

func TestGoldenReloadingConfigDrawsEveryVisibleState(t *testing.T) {
	scenarios := map[string]configReloadScenario{
		"valid revision":                                    configReloadValid,
		"invalid revision then recovery":                    configReloadInvalidRecovery,
		"deleted config restores defaults":                  configReloadDeletion,
		"filesystem watch failure":                          configReloadWatchFailure,
		"replayed failure and recovery":                     configReloadReplay,
		"reloaded snippets":                                 configReloadSnippets,
		"reloaded snippet file":                             configReloadSnippetFile,
		"a snippet file two snippets share":                 configReloadSharedSnippetFile,
		"a snippet file the revision itself names":          configReloadSnippetFileTheConfigNames,
		"a revision changing no setting":                    configReloadWithoutASettingChange,
		"confirmation dismissed by typing":                  configReloadDismissedConfirmation,
		"confirmation dismissed automatically":              configReloadAutoDismissedConfirmation,
		"failure showing when the reload lands":             configReloadPreservedFailure,
		"revision to a setting only a new session picks up": configReloadSettingThatIsNotLive,
		"a grant that waits for the next run":               configReloadGrantThatWaitsForTheNextRun,
	}

	passes := make(map[string]func() string, len(scenarios))
	for name, scenario := range scenarios {
		passes[name] = func() string { return configReloadStream(t, scenario) }
	}

	compareWithGolden(t, "config-reload", ".ansi", passes)
	compareWithGolden(t, "config-reload", ".screen", shownPasses(t, passes))
}

func TestEphemeralInterfaceFeedbackStaysOutOfConversationHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, `
		[ui]
		currency = "GBP"
	`)

	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	const terminalLines = 8
	const answer = "01 alpha\n\n02 bravo\n\n03 charlie\n\n04 delta\n\n05 echo\n\n06 foxtrot\n\n07 golf"

	var liveOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&liveOutput, replayColumns, terminalLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read),
		slash: slashState{commands: fixtureCommandRegistryWithSnippets(t, nil, slash.Command{
			Name: "help",
			Run: func(context slash.Context, _ slash.Arguments) error {
				context.Notice("Commands:\n  /help")
				return nil
			},
		})},
	}
	prepareLiveConfig(t, self, path)
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "show me the answer"})
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: answer})

	beforeFeedback := liveOutput.String()
	self.handleCommand("/foo")
	self.handleCommand("/help")
	writeLiveConfig(t, path, `
		[ui]
		currency = "EUR"
	`)
	settleLiveConfig(t, self)
	writeLiveConfig(t, path, `
		[ui]
		currency = "JPY"
	`)
	settleLiveConfig(t, self)

	if liveOutput.String() != beforeFeedback {
		t.Error("ephemeral interface feedback changed conversation scrollback before the answer was sealed")
	}
	if len(self.recordedEvents) != 1 || self.recordedEvents[0].Kind != agent.UserMessageEvent {
		t.Errorf("conversation events before the answer was sealed are %v, want [user_message]", getEventKinds(self.recordedEvents))
	}

	completed := agent.Event{Kind: agent.ModelMessageEvent, Text: answer}
	self.takeTurn(TurnEvent{Update: agent.Update{Event: &completed}})
	self.finish()

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSession.Events) != 2 ||
		storedSession.Events[0].Kind != agent.UserMessageEvent ||
		storedSession.Events[1].Kind != agent.ModelMessageEvent {
		t.Errorf("stored conversation events are %v, want [user_message model_message]", getEventKinds(storedSession.Events))
	}

	var replayOutput bytes.Buffer
	replayed := &App{
		agent:          agent.New("", quietProvider{}, nil),
		screen:         output.NewTerminalOfSize(&replayOutput, replayColumns, terminalLines),
		recordedEvents: storedSession.Events,
	}
	replayed.replay()

	requireSameVisibleScreen(
		t,
		"ephemeral interface feedback changed the stored replay",
		liveOutput.String(),
		replayOutput.String(),
	)
}

func TestRetryRemainsDurableConversationHistory(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	var liveOutput bytes.Buffer
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.NewTerminalOfSize(&liveOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read),
	}
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "try it"})
	self.recordEvent(agent.Event{
		Kind:    agent.RetryingEvent,
		Text:    "temporary fault",
		Attempt: 1,
		Took:    300 * time.Millisecond,
	})
	self.recordEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: "done"})
	self.finish()

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	storedSession, err := store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSession.Events) != 3 ||
		storedSession.Events[0].Kind != agent.UserMessageEvent ||
		storedSession.Events[1].Kind != agent.RetryingEvent ||
		storedSession.Events[2].Kind != agent.ModelMessageEvent {
		t.Errorf("stored conversation events are %v, want [user_message retrying model_message]", getEventKinds(storedSession.Events))
	}

	var replayOutput bytes.Buffer
	replayed := &App{
		agent:          agent.New("", quietProvider{}, nil),
		screen:         output.NewTerminalOfSize(&replayOutput, replayColumns, replayLines),
		recordedEvents: storedSession.Events,
	}
	replayed.replay()

	if !strings.Contains(style.Plain(replayOutput.String()), "[#1] Request failed; retrying in 0.3s: Temporary fault") {
		t.Errorf("stored replay omitted the retry:\n%s", style.Plain(replayOutput.String()))
	}
	requireSameVisibleScreen(t, "durable retry changed after replay", liveOutput.String(), replayOutput.String())
}

func getEventKinds(events []agent.Event) []agent.Kind {
	kinds := make([]agent.Kind, len(events))
	for i, event := range events {
		kinds[i] = event.Kind
	}
	return kinds
}

func configReloadStream(t *testing.T, scenario configReloadScenario) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, `
		[ui]
		currency = "GBP"

		[bar.top]
		left = [{ segment = "session-name" }]
		center = []
		right = []

		[bar.bottom]
		left = []
		center = []
		right = []
	`)

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	prepareLiveConfig(t, self, path)
	inputLine := edit.NewInput(nil)

	currentTime := time.Now()
	if scenario == configReloadAutoDismissedConfirmation {
		self.now = func() time.Time { return currentTime }
	}
	self.show(inputLine)

	switch scenario {
	case configReloadDeletion:
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		settleLiveConfig(t, self)
		self.show(inputLine)
		return screenOutput.String()
	case configReloadWatchFailure:
		self.reloadConfig(errors.New("inotify stopped"))
		self.show(inputLine)
		return screenOutput.String()
	case configReloadSnippets:
		writeLiveConfig(t, path, `
			[ui]
			currency = "GBP"

			[snippets]
			review = { prompt = "Review the changes.", description = "Review the working tree." }

			[bar.top]
			left = [{ segment = "session-name" }]
			center = []
			right = []

			[bar.bottom]
			left = []
			center = []
			right = []
		`)
		settleLiveConfig(t, self)
		self.handleCommand("//help")
		self.show(inputLine)
		return screenOutput.String()
	case configReloadSnippetFile:
		snippetPath := filepath.Join(filepath.Dir(path), "review.md")
		writeSnippetFile(t, snippetPath, "Review the first revision.")
		writeLiveConfig(t, path, `
			[ui]
			currency = "GBP"

			[snippets]
			review = { file = "review.md", description = "Review the working tree." }

			[bar.top]
			left = [{ segment = "session-name" }]
			center = []
			right = []

			[bar.bottom]
			left = []
			center = []
			right = []
		`)
		settleLiveConfig(t, self)
		writeSnippetFile(t, snippetPath, "Review the reloaded revision.")
		settleLiveConfig(t, self)
		self.show(inputLine)
		return screenOutput.String()
	case configReloadSharedSnippetFile:
		snippetPath := filepath.Join(filepath.Dir(path), "review.md")
		writeSnippetFile(t, snippetPath, "Review the first revision.")
		writeLiveConfig(t, path, `
			[ui]
			currency = "GBP"

			[snippets]
			check = { file = "review.md", description = "Check the working tree." }
			review = { file = "review.md", description = "Review the working tree." }

			[bar.top]
			left = [{ segment = "session-name" }]
			center = []
			right = []

			[bar.bottom]
			left = []
			center = []
			right = []
		`)
		settleLiveConfig(t, self)
		writeSnippetFile(t, snippetPath, "Review the reloaded revision.")
		settleLiveConfig(t, self)
		self.show(inputLine)
		return screenOutput.String()
	case configReloadSnippetFileTheConfigNames:
		writeSnippetFile(t, filepath.Join(filepath.Dir(path), "review.md"), "Review the changes.")
		writeLiveConfig(t, path, `
			[ui]
			currency = "GBP"

			[snippets]
			review = { file = "review.md", description = "Review the working tree." }

			[bar.top]
			left = [{ segment = "session-name" }]
			center = []
			right = []

			[bar.bottom]
			left = []
			center = []
			right = []
		`)
		settleLiveConfig(t, self)
		self.show(inputLine)
		return screenOutput.String()
	case configReloadWithoutASettingChange:
		writeLiveConfig(t, path, `
			# The currency below is the one it already had.
			[ui]
			currency = "GBP"

			[bar.top]
			left = [{ segment = "session-name" }]
			center = []
			right = []

			[bar.bottom]
			left = []
			center = []
			right = []
		`)
		settleLiveConfig(t, self)
		self.show(inputLine)
		return screenOutput.String()
	case configReloadSettingThatIsNotLive:
		writeLiveConfig(t, path, `
			[ui]
			currency = "GBP"

			[model]
			round_robin = ["ollama/example"]

			[bar.top]
			left = [{ segment = "session-name" }]
			center = []
			right = []

			[bar.bottom]
			left = []
			center = []
			right = []
		`)
		settleLiveConfig(t, self)
		self.show(inputLine)
		return screenOutput.String()
	case configReloadGrantThatWaitsForTheNextRun:
		writeLiveConfig(t, path, `
			[ui]
			currency = "GBP"

			[sandbox]
			read = ["~/Dropbox/iso"]

			[bar.top]
			left = [{ segment = "session-name" }]
			center = []
			right = []

			[bar.bottom]
			left = []
			center = []
			right = []
		`)
		settleLiveConfig(t, self)
		self.show(inputLine)
		return screenOutput.String()
	case configReloadInvalidRecovery, configReloadReplay:
		writeLiveConfig(t, path, `
			[bar.top]
			left = [{ segment = "missing" }]
		`)
		settleLiveConfig(t, self)
	case configReloadPreservedFailure:
		self.notifyFailure("The conversation could not be stored: no space left on device")
	case configReloadValid, configReloadDismissedConfirmation, configReloadAutoDismissedConfirmation:
	}

	writeLiveConfig(t, path, `
		[ui]
		currency = "EUR"

		[bar.top]
		left = []
		center = []
		right = []

		[bar.bottom]
		left = []
		center = [{ segment = "active-model" }]
		right = []
	`)
	settleLiveConfig(t, self)
	if scenario == configReloadDismissedConfirmation || scenario == configReloadReplay {
		self.apply(inputLine, nil, key.Key{Code: key.Rune, Value: 'a'})
		self.apply(inputLine, nil, key.Key{Code: key.Backspace})
	}
	if scenario == configReloadAutoDismissedConfirmation {
		currentTime = currentTime.Add(configReloadConfirmationDuration)
	}
	self.show(inputLine)
	if scenario != configReloadReplay {
		return screenOutput.String()
	}

	var replayOutput bytes.Buffer
	replayed := testConversation(t, &replayOutput)
	replayed.screen = output.NewTerminalOfSize(&replayOutput, replayColumns, replayLines)
	prepareLiveConfig(t, replayed, path)
	replayed.recordedEvents = append(replayed.recordedEvents, self.recordedEvents...)
	replayed.replay()
	replayed.show(edit.NewInput(nil))

	requireSameVisibleScreen(
		t,
		"reloaded config changed after replay",
		screenOutput.String(),
		replayOutput.String(),
	)

	return replayOutput.String()
}

type screen struct {
	t           *testing.T
	rows        [][]cell
	marks       map[int]bool
	row, column int
	columns     int
	isWrapping  bool
	style       graphicStyle
}

type cell struct {
	grapheme string
	styles   string
}

type graphicStyle struct {
	foreground  string
	background  string
	underline   string
	decorations []string
}

const picturePlaceholder = "\U0010EEEE"

var decorationsOff = map[string][]string{
	"21": {"1"},
	"22": {"1", "2"},
	"23": {"3"},
	"24": {"4"},
	"25": {"5", "6"},
	"27": {"7"},
	"28": {"8"},
	"29": {"9"},
	"55": {"53"},
}

func (self graphicStyle) String() string {
	parts := slices.Clone(self.decorations)
	slices.Sort(parts)

	for _, colour := range []string{self.foreground, self.background, self.underline} {
		if colour != "" {
			parts = append(parts, colour)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "\x1b[" + strings.Join(parts, ";") + "m"
}

func TestAScreenTellsTheSameWordsInDifferentColoursApart(t *testing.T) {
	red := playScreen(t, "\x1b[38;2;204;102;102m$\x1b[0m curl", replayColumns)
	blue := playScreen(t, "\x1b[38;2;129;162;190m$\x1b[0m curl", replayColumns)

	if !slices.Equal(red.text(), blue.text()) {
		t.Fatalf("the two screens read differently: %q and %q", red.text(), blue.text())
	}
	if slices.Equal(red.styled(), blue.styled()) {
		t.Errorf("the two screens were styled alike: %q", strutil.VisibleEscapes(strings.Join(red.styled(), "")))
	}
}

func TestAScreenReadsADefaultForegroundAsClearingTheColourBeforeIt(t *testing.T) {
	cleared := playScreen(t, "\x1b[38;2;1;2;3m\x1b[39mplain", replayColumns)
	never := playScreen(t, "plain", replayColumns)

	if !slices.Equal(cleared.styled(), never.styled()) {
		t.Errorf(
			"a cleared foreground drew %q, want %q",
			strutil.VisibleEscapes(strings.Join(cleared.styled(), "")),
			strutil.VisibleEscapes(strings.Join(never.styled(), "")),
		)
	}
}

func TestAScreenRemembersEveryRowAMarkArrivedOn(t *testing.T) {
	drawn := playScreen(t, "one\n"+escape.MessageMark+"two\nthree\n"+escape.MessageMark+"four", replayColumns)

	if got, want := drawn.marked(), []int{1, 3}; !slices.Equal(got, want) {
		t.Errorf("the screen marked rows %v, want %v", got, want)
	}
	if got, want := drawn.text(), []string{"one", "two", "three", "four"}; !slices.Equal(got, want) {
		t.Errorf("a mark was drawn as something: %q", got)
	}
}

func TestAScreenMarksTheRowAMarkLandsOnRatherThanTheOneItFollows(t *testing.T) {
	drawn := playScreen(t, "one"+escape.MessageMark+" two", replayColumns)

	if got, want := drawn.marked(), []int{0}; !slices.Equal(got, want) {
		t.Errorf("the screen marked rows %v, want %v", got, want)
	}
}

func TestAScreenForgetsTheMarksOnTheRowsItIsToldToErase(t *testing.T) {
	drawn := playScreen(t, "one\n"+escape.MessageMark+"two\n"+escape.MessageMark+"three\x1b[1;1H\x1b[0J", replayColumns)

	if got := drawn.marked(); len(got) != 0 {
		t.Errorf("the screen kept marks %v over an erased screen", got)
	}
}

func TestAScreenIgnoresThePictureIdentityCarriedAsAColour(t *testing.T) {
	first := playScreen(t, "\x1b[38;2;0;0;27m"+picturePlaceholder+"\x1b[39m", replayColumns)
	second := playScreen(t, "\x1b[38;2;0;0;28m"+picturePlaceholder+"\x1b[39m", replayColumns)

	if !slices.Equal(first.styled(), second.styled()) {
		t.Errorf(
			"two placements of the same picture were styled differently: %q and %q",
			strutil.VisibleEscapes(strings.Join(first.styled(), "")),
			strutil.VisibleEscapes(strings.Join(second.styled(), "")),
		)
	}
}

func TestAScreenKeepsADecorationApartFromItsColour(t *testing.T) {
	together := playScreen(t, "\x1b[3;38;2;1;2;3mword\x1b[0m", replayColumns)
	apart := playScreen(t, "\x1b[38;2;1;2;3m\x1b[3mword\x1b[0m", replayColumns)

	if !slices.Equal(together.styled(), apart.styled()) {
		t.Errorf(
			"one sequence drew %q and two drew %q",
			strutil.VisibleEscapes(strings.Join(together.styled(), "")),
			strutil.VisibleEscapes(strings.Join(apart.styled(), "")),
		)
	}
	if plain := playScreen(t, "word", replayColumns); slices.Equal(together.styled(), plain.styled()) {
		t.Error("an italic coloured word was styled like a plain one")
	}
}

func playScreen(t *testing.T, stream string, columns int) *screen {
	t.Helper()

	self := &screen{t: t, columns: columns, isWrapping: true}
	self.play(stream)

	return self
}

func visibleScreen(t *testing.T, stream string, columns int) []string {
	t.Helper()

	return playScreen(t, stream, columns).text()
}

func (self *screen) play(stream string) {
	for at := 0; at < len(stream); {
		switch stream[at] {
		case '\x1b':
			at = self.escape(stream, at)
		case '\r':
			self.column = 0
			at++
		case '\n':
			self.row++
			self.column = 0
			at++
		default:
			end := at + 1
			for end < len(stream) && stream[end] != '\x1b' && stream[end] != '\r' && stream[end] != '\n' {
				end++
			}

			for grapheme, cells := range width.Graphemes(stream[at:end]) {
				self.put(grapheme, cells)
			}
			at = end
		}
	}
}

func (self *screen) escape(stream string, at int) int {
	if at+1 >= len(stream) {
		return len(stream)
	}

	switch stream[at+1] {
	case '[':
		return self.control(stream, at)
	case ']':
		return self.operatingSystemCommand(stream, at)
	case '_':
		return self.applicationCommand(stream, at)
	default:
		self.t.Fatalf("the screen was sent an escape it does not know: %q", stream[at:min(at+8, len(stream))])
		return len(stream)
	}
}

func (self *screen) applicationCommand(stream string, at int) int {
	return skipUntilStringTerminator(stream, at)
}

func (self *screen) operatingSystemCommand(stream string, at int) int {
	end := skipUntilStringTerminator(stream, at)
	terminatorWidth := 1
	if end >= 2 && stream[end-2:end] == "\x1b\\" {
		terminatorWidth = 2
	}
	payload := stream[at+2 : end-terminatorWidth]
	if strings.HasPrefix(payload, "133;") {
		self.semanticPrompt(payload)
		return end
	}

	if !strings.HasPrefix(payload, "66;") {
		return end
	}

	parts := strings.SplitN(payload, ";", 3)
	if len(parts) != 3 {
		return end
	}

	scale := 1
	declaredWidth := width.Of(parts[2])
	for option := range strings.SplitSeq(parts[1], ":") {
		scale = prefixedNumber(option, "s=", scale)
		declaredWidth = prefixedNumber(option, "w=", declaredWidth)
	}

	self.putSized(parts[2], scale*declaredWidth)

	return end
}

const messageMarkPayload = "133;A"

func (self *screen) semanticPrompt(payload string) {
	if payload != messageMarkPayload {
		self.t.Fatalf("the screen was sent a semantic prompt it does not know: OSC %s", payload)
		return
	}

	if self.marks == nil {
		self.marks = map[int]bool{}
	}

	self.marks[self.row] = true
}

func (self *screen) putSized(grapheme string, cells int) {
	drawnCells := min(width.Of(grapheme), cells)
	self.put(grapheme, drawnCells)

	for range cells - drawnCells {
		self.put(" ", 1)
	}
}

func (self *screen) control(stream string, at int) int {
	end := at + 2
	for end < len(stream) && (stream[end] < 0x40 || stream[end] > 0x7e) {
		end++
	}

	if end >= len(stream) {
		return len(stream)
	}

	parameters := stream[at+2 : end]
	self.apply(stream[end], parameters)

	return end + 1
}

func (self *screen) apply(command byte, parameters string) {
	if strings.HasPrefix(parameters, "?") {
		self.privateMode(command, parameters)
		return
	}

	count := max(1, numberOr(parameters, 1))

	switch command {
	case 'm':
		self.restyle(parameters)
	case 'A':
		self.row = max(0, self.row-count)
	case 'B':
		self.row += count
	case 'C':
		self.column += count
	case 'D':
		self.column = max(0, self.column-count)
	case 'H':
		self.row, self.column = 0, 0
	case 'K':
		self.eraseInRow(numberOr(parameters, 0))
	case 'J':
		self.erase(numberOr(parameters, 0))
	default:
		self.t.Fatalf("the screen was sent a control it does not know: ESC [ %s%c", parameters, command)
	}
}

func (self *screen) privateMode(command byte, parameters string) {
	if command != 'h' && command != 'l' {
		self.t.Fatalf("the screen was sent a private mode it does not know: ESC [ %s%c", parameters, command)
	}

	switch parameters {
	case "?7":
		self.isWrapping = command == 'h'
	case "?25", "?2026":
	default:
		self.t.Fatalf("the screen was sent a private mode it does not know: ESC [ %s%c", parameters, command)
	}
}

func (self *screen) eraseInRow(mode int) {
	if self.row >= len(self.rows) {
		return
	}

	row := self.rows[self.row]

	switch mode {
	case 0:
		if self.column < len(row) {
			self.rows[self.row] = row[:self.column]
		}
	case 1:
		for at := range min(self.column+1, len(row)) {
			row[at] = cell{grapheme: " "}
		}
	case 2:
		self.rows[self.row] = nil
	}
}

func (self *screen) erase(mode int) {
	switch mode {
	case 0:
		self.eraseInRow(0)

		if self.row+1 < len(self.rows) {
			self.rows = self.rows[:self.row+1]
		}

		maps.DeleteFunc(self.marks, func(row int, _ bool) bool { return row > self.row })
	case 2, 3:
		self.rows = nil
		self.marks = nil
	default:
		self.t.Fatalf("the screen was asked to erase in a way it does not know: ESC [ %dJ", mode)
	}
}

func (self *screen) put(grapheme string, cells int) {
	if cells == 0 {
		for len(self.rows) <= self.row {
			self.rows = append(self.rows, nil)
		}
		if self.column > 0 {
			self.rows[self.row][self.column-1].grapheme += grapheme
		}
		return
	}

	if self.column+cells > self.columns && self.column > 0 {
		if !self.isWrapping {
			return
		}

		self.row++
		self.column = 0
	}

	for len(self.rows) <= self.row {
		self.rows = append(self.rows, nil)
	}

	drawnCells := min(cells, self.columns-self.column)
	row := self.rows[self.row]
	for len(row) < self.column+drawnCells {
		row = append(row, cell{grapheme: " "})
	}

	styles := self.style.String()
	if strings.HasPrefix(grapheme, picturePlaceholder) {
		styles = ""
	}

	row[self.column] = cell{grapheme: grapheme, styles: styles}
	for i := 1; i < drawnCells; i++ {
		row[self.column+i] = cell{styles: styles}
	}
	self.rows[self.row] = row
	self.column = min(self.column+cells, self.columns)
}

func (self *screen) restyle(parameters string) {
	tokens := strings.Split(parameters, ";")

	for at := 0; at < len(tokens); at++ {
		switch token := tokens[at]; {
		case token == "" || token == "0":
			self.style = graphicStyle{}
		case token == "38" || token == "48" || token == "58":
			colour, next := self.readColour(tokens, at)
			switch token {
			case "38":
				self.style.foreground = colour
			case "48":
				self.style.background = colour
			default:
				self.style.underline = colour
			}
			at = next
		case token == "39":
			self.style.foreground = ""
		case token == "49":
			self.style.background = ""
		case token == "59":
			self.style.underline = ""
		case decorationsOff[token] != nil:
			self.style.decorations = slices.DeleteFunc(self.style.decorations, func(one string) bool {
				name, _, _ := strings.Cut(one, ":")
				return slices.Contains(decorationsOff[token], name)
			})
		case isDecoration(token):
			if !slices.Contains(self.style.decorations, token) {
				self.style.decorations = append(self.style.decorations, token)
			}
		default:
			self.t.Fatalf("the screen was sent a graphic rendition it does not know: ESC [ %sm", parameters)
		}
	}
}

func (self *screen) readColour(tokens []string, at int) (string, int) {
	remaining := 0
	if at+1 < len(tokens) {
		switch tokens[at+1] {
		case "2":
			remaining = 4
		case "5":
			remaining = 2
		}
	}

	if remaining == 0 || at+remaining >= len(tokens) {
		self.t.Fatalf("the screen was sent a colour it does not know: %q", strings.Join(tokens[at:], ";"))
		return "", len(tokens)
	}

	return strings.Join(tokens[at:at+remaining+1], ";"), at + remaining
}

func isDecoration(token string) bool {
	name, _, _ := strings.Cut(token, ":")
	number, err := strconv.Atoi(name)

	return err == nil && (number >= 1 && number <= 9 || number == 53)
}

func (self *screen) text() []string {
	return self.lines(func(row []cell) string {
		var drawn strings.Builder
		for _, one := range row {
			drawn.WriteString(one.grapheme)
		}

		return drawn.String()
	})
}

func (self *screen) styled() []string {
	return self.lines(func(row []cell) string {
		var drawn strings.Builder
		styles := ""
		for _, one := range row {
			if one.styles != styles {
				if styles != "" {
					drawn.WriteString("\x1b[0m")
				}
				drawn.WriteString(one.styles)
				styles = one.styles
			}
			drawn.WriteString(one.grapheme)
		}
		if styles != "" {
			drawn.WriteString("\x1b[0m")
		}

		return drawn.String()
	})
}

func (self *screen) marked() []int {
	return slices.Sorted(maps.Keys(self.marks))
}

func (self *screen) backgroundAt(row int) string {
	if row >= len(self.rows) || len(self.rows[row]) == 0 {
		return ""
	}

	return self.rows[row][0].styles
}

func (self *screen) lines(draw func(row []cell) string) []string {
	lines := make([]string, 0, len(self.rows))

	for _, row := range self.rows {
		lines = append(lines, strings.TrimRight(draw(row), " "))
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

func skipUntilStringTerminator(stream string, at int) int {
	for end := at + 2; end < len(stream); end++ {
		switch {
		case stream[end] == '\a':
			return end + 1
		case stream[end] == '\x1b' && end+1 < len(stream) && stream[end+1] == '\\':
			return end + 2
		}
	}

	return len(stream)
}

func prefixedNumber(parameter string, prefix string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimPrefix(parameter, prefix))
	if err != nil || !strings.HasPrefix(parameter, prefix) {
		return fallback
	}

	return value
}

func numberOr(parameters string, fallback int) int {
	if parameters == "" {
		return fallback
	}

	value, err := strconv.Atoi(strings.Split(parameters, ";")[0])
	if err != nil {
		return fallback
	}

	return value
}

type goldenSegmentOptions string

func (self goldenSegmentOptions) Read(into any) error {
	_, err := toml.Decode(string(self), into)

	return err
}

func goldenSegmentPass(
	t *testing.T,
	factory segment.Factory,
	options string,
	context segment.Context,
) func() string {
	t.Helper()

	built, err := factory(goldenSegmentOptions(options))
	if err != nil {
		t.Fatal(err)
	}

	return func() string { return built.Render(context) }
}

func goldenFittedSegmentPass(
	t *testing.T,
	factory segment.Factory,
	options string,
	context segment.Context,
	cells int,
) func() string {
	t.Helper()

	built, err := factory(goldenSegmentOptions(options))
	if err != nil {
		t.Fatal(err)
	}
	fitter, ok := built.(segment.Fitter)
	if !ok {
		t.Fatal("segment does not support fitting")
	}
	return func() string { return fitter.RenderWithin(context, cells) }
}

func goldenRepository(t *testing.T, head string) string {
	t.Helper()

	workspaceDir := t.TempDir()
	gitDir := filepath.Join(workspaceDir, ".git")

	if err := os.MkdirAll(gitDir, 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	return workspaceDir
}

func availableSegments(workspace *work.Space, harness *App) segment.Registry {
	return bar.NewRegistry(bar.Options{
		Workspace: workspace,
		Session: cycle.Session{
			Name:      "tame-impala",
			Directory: filepath.Join("/state/sessions", "tame-impala"),
			Model:     "gpt-5.6-sol",
			Effort:    "high",
		},
		ModelEffortLevels: []string{"none", "minimal", "low", "medium", "high"},
		Sources:           harness.getBarSources(),
	})
}

func renderBar(layout segment.Layout, position segment.Position) string {
	return bar.Render(layout, position, segment.Context{})
}

func goldenBarLayout(t *testing.T, harness *App) segment.Layout {
	t.Helper()

	config := configFrom(t, `
		[bar.top]
		left = [{ segment = "workspace-dir" }]
		center = [{ segment = "active-model" }]
		right = [{ segment = "scroll-overflow", direction = "up" }]

		[bar.bottom]
		left = [
			{ segment = "activity-spinner", idle = "✧··", frames = ["✦··", "·✦·", "··✦", "··✧", "·✧·", "✧··"], rate = "125ms" },
			{ segment = "turn-timer" },
			{ segment = "mode-toggle" },
			{ segment = "workspace-dir" },
			{ segment = "git-branch" },
			{ segment = "active-model" },
			{ segment = "context-usage" },
		]
		center = []
		right = [
			{ segment = "turn-count" },
			{ segment = "session-name" },
			{ segment = "local-time", format = "15:04" },
			{ segment = "scroll-overflow", direction = "down" },
		]
	`)

	layout, err := config.BuildLayout(
		availableSegments(work.At(workspaceMarker), harness),
	)
	if err != nil {
		t.Fatal(err)
	}

	return layout
}

func clockAt(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func jobsOf(listing ...jobs.Snapshot) func() []jobs.Snapshot {
	return func() []jobs.Snapshot { return listing }
}

func goldenSchedulePass(t *testing.T, isRunning bool, workPerPass time.Duration, span time.Duration) func() string {
	t.Helper()

	return func() string {
		return drawnOnAStoppedClock(t, func(t *testing.T) string {
			t.Helper()

			startedAt := time.Now()

			held := &App{mode: caps.NewMode(caps.All())}
			held.currentTurn.Stream = testTimedTurnStream(isRunning, startedAt, startedAt)
			held.display.bar = bar.NewConfiguration(nil, goldenBarLayout(t, held))

			return filmstrip(startedAt, span, workPerPass, func() time.Time {
				return held.nextBarRefresh(time.Now())
			}, func() string {
				return held.display.bar.Render(segment.BottomLeft, segment.Context{})
			})
		})
	}
}

func goldenTimerSchedulePass(t *testing.T, workPerPass time.Duration, span time.Duration) func() string {
	t.Helper()

	return func() string {
		return drawnOnAStoppedClock(t, func(t *testing.T) string {
			t.Helper()

			startedAt := time.Now()

			held := &App{mode: caps.NewMode(caps.All())}
			held.currentTurn.Stream = testTimedTurnStream(true, startedAt, time.Time{})

			built, err := turnTimer.New(held.turnTiming, held.isTurnRunning)(goldenSegmentOptions(""))
			if err != nil {
				t.Fatal(err)
			}

			layout := segment.Layout{segment.BottomLeft: {built}}

			return filmstrip(startedAt, span, workPerPass, func() time.Time {
				return layout.NextRefresh(segment.Phase{At: time.Now(), IsRunning: true})
			}, func() string {
				return renderBar(layout, segment.BottomLeft)
			})
		})
	}
}

func goldenClockSchedulePass(t *testing.T, format string, span time.Duration) func() string {
	t.Helper()

	return func() string {
		return drawnOnAStoppedClock(t, func(t *testing.T) string {
			t.Helper()

			startedAt := time.Now()

			built, err := localTime.New(time.Now)(goldenSegmentOptions(fmt.Sprintf("format = %q", format)))
			if err != nil {
				t.Fatal(err)
			}

			layout := segment.Layout{segment.TopRight: {built}}

			return filmstrip(startedAt, span, 0, func() time.Time {
				return layout.NextRefresh(segment.Phase{At: time.Now()})
			}, func() string {
				return renderBar(layout, segment.TopRight)
			})
		})
	}
}

func goldenJobSchedulePass(t *testing.T, runsFor time.Duration, span time.Duration) func() string {
	t.Helper()

	return func() string {
		return drawnOnAStoppedClock(t, func(t *testing.T) string {
			t.Helper()

			startedAt := time.Now()
			endsAt := startedAt.Add(runsFor)

			getJobs := func() []jobs.Snapshot {
				if time.Now().Before(endsAt) {
					return []jobs.Snapshot{{Name: "check", State: jobs.StateRunning, StartedAt: startedAt}}
				}

				return []jobs.Snapshot{{
					Name:      "check",
					State:     jobs.StateComplete,
					StartedAt: startedAt,
					EndedAt:   endsAt,
				}}
			}

			built, err := jobNames.New(getJobs, noPortRoutes, time.Now)(goldenSegmentOptions(""))
			if err != nil {
				t.Fatal(err)
			}

			layout := segment.Layout{segment.TopRight: {built}}

			return filmstrip(startedAt, span, 0, func() time.Time {
				return layout.NextRefresh(segment.Phase{At: time.Now()})
			}, func() string {
				return renderBar(layout, segment.TopRight)
			})
		})
	}
}

func goldenFeedbackSchedulePass(t *testing.T, span time.Duration) func() string {
	t.Helper()

	return func() string {
		return drawnOnAStoppedClock(t, func(t *testing.T) string {
			t.Helper()

			startedAt := time.Now()

			held := &App{mode: caps.NewMode(caps.All()), screen: output.New(&bytes.Buffer{})}
			held.showFeedback(feedback.Confirmation, feedback.Message{
				Text:         "Configuration reloaded automatically",
				Status:       agent.SuccessStatus,
				DismissAfter: configReloadConfirmationDuration,
			})

			return filmstrip(startedAt, span, 0, func() time.Time {
				return held.nextRefresh(time.Now())
			}, func() string {
				held.feedback.ClearExpired(time.Now())
				return strings.Join(held.statusRows(replayColumns), "\n")
			})
		})
	}
}

func filmstrip(
	startedAt time.Time,
	span time.Duration,
	workPerPass time.Duration,
	getNextRefresh func() time.Time,
	draw func() string,
) string {
	var strip strings.Builder

	for {
		at := getNextRefresh()
		if at.IsZero() || at.Sub(startedAt) > span {
			return strip.String()
		}

		time.Sleep(time.Until(at))
		since := "+" + time.Since(startedAt).Truncate(time.Millisecond).String()
		fmt.Fprintf(&strip, "%9s  %s\n", since, draw())
		time.Sleep(workPerPass)
	}
}

func TestGoldenTheRedrawScheduleRunsWhenItRanBefore(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	passes := map[string]func() string{
		"running turn":                                      goldenSchedulePass(t, true, 0, 2*time.Second),
		"running turn with a slow pass":                     goldenSchedulePass(t, true, 40*time.Millisecond, 2*time.Second),
		"running turn with a pass past due":                 goldenSchedulePass(t, true, 200*time.Millisecond, 2*time.Second),
		"waiting between turns":                             goldenSchedulePass(t, false, 0, 3*time.Second),
		"turn timer alone on a slow loop":                   goldenTimerSchedulePass(t, 200*time.Millisecond, 3*time.Minute),
		"clock alone to the minute":                         goldenClockSchedulePass(t, "15:04", 3*time.Minute),
		"clock alone to the second":                         goldenClockSchedulePass(t, "15:04:05", 3*time.Second),
		"confirmation feedback ticks down to its dismissal": goldenFeedbackSchedulePass(t, configReloadConfirmationDuration+time.Second),
		"a finished job lingers on the bar and then goes":   goldenJobSchedulePass(t, 3*time.Second, 40*time.Second),
	}

	compareWithGolden(t, "schedule", ".ansi", passes)
	compareWithGolden(t, "schedule", ".screen", shownPasses(t, passes))
}

func TestGoldenEverySegmentDrawsItsRepresentativeStates(t *testing.T) {
	t.Setenv("HOME", "/user/kevin")

	at := time.Date(2026, time.August, 23, 14, 32, 9, 0, time.UTC)
	isPersisted := func() bool { return true }
	isNotPersisted := func() bool { return false }
	spinnerOptions := `
		idle = "✧·"
		frames = ["✦·", "·✦"]
		rate = "125ms"
	`

	passes := map[string]func() string{
		"activity-spinner / idle": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return false }, clockAt(at)),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running first frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return true }, clockAt(at)),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running second frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return true }, clockAt(at.Add(125*time.Millisecond))),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running frames coming round again": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return true }, clockAt(at.Add(250*time.Millisecond))),
			spinnerOptions,
			segment.Context{},
		),
		"activity-spinner / running part way through a frame": goldenSegmentPass(
			t,
			activitySpinner.New(func() bool { return true }, clockAt(at.Add(190*time.Millisecond))),
			spinnerOptions,
			segment.Context{},
		),
		"cache-usage / nothing asked yet": goldenSegmentPass(
			t,
			cacheUsage.New(func() (int, int) { return 0, 0 }),
			"",
			segment.Context{},
		),
		"cache-usage / every token read": goldenSegmentPass(
			t,
			cacheUsage.New(func() (int, int) { return 200_000, 200_000 }),
			"",
			segment.Context{},
		),
		"cache-usage / mostly read": goldenSegmentPass(
			t,
			cacheUsage.New(func() (int, int) { return 186_400, 200_000 }),
			"",
			segment.Context{},
		),
		"cache-usage / rebuilt from cold": goldenSegmentPass(
			t,
			cacheUsage.New(func() (int, int) { return 1_604, 281_033 }),
			"",
			segment.Context{},
		),
		"context-usage / known": goldenSegmentPass(
			t,
			contextUsage.New(func() (int, int) { return 182_000, 200_000 }),
			"",
			segment.Context{},
		),
		"context-usage / one million": goldenSegmentPass(
			t,
			contextUsage.New(func() (int, int) { return 500_000, 1_000_000 }),
			"",
			segment.Context{},
		),
		"context-usage / unknown": goldenSegmentPass(
			t,
			contextUsage.New(func() (int, int) { return 0, 0 }),
			"",
			segment.Context{},
		),
		"git-branch / outside a repository": goldenSegmentPass(
			t,
			gitBranch.New(workspaceMarker),
			"",
			segment.Context{},
		),
		"git-branch / on a branch": goldenSegmentPass(
			t,
			gitBranch.New(goldenRepository(t, "ref: refs/heads/feature/bars\n")),
			"",
			segment.Context{},
		),
		"git-branch / detached head": goldenSegmentPass(
			t,
			gitBranch.New(goldenRepository(t, "1fd19004e0f4a2c8b4c5d6e7f8a9b0c1d2e3f4a5\n")),
			"",
			segment.Context{},
		),
		"turn-count / nothing asked yet": goldenSegmentPass(
			t,
			turnCount.New(func() int { return 0 }),
			"",
			segment.Context{},
		),
		"turn-count / third turn": goldenSegmentPass(
			t,
			turnCount.New(func() int { return 3 }),
			"",
			segment.Context{},
		),
		"session-name": goldenSegmentPass(
			t,
			sessionName.New("tame-impala", "/state/sessions/tame-impala", isPersisted),
			"",
			segment.Context{},
		),
		"session-name / unpersisted": goldenSegmentPass(
			t,
			sessionName.New("tame-impala", "/state/sessions/tame-impala", isNotPersisted),
			"",
			segment.Context{},
		),
		"session-name / unpersisted / emoji": goldenSegmentPass(
			t,
			sessionName.New("tame-impala", "/state/sessions/tame-impala", isNotPersisted),
			"emoji = true",
			segment.Context{},
		),
		"session-name / emoji": goldenSegmentPass(
			t,
			sessionName.New("tame-impala", "/state/sessions/tame-impala", isPersisted),
			"emoji = true",
			segment.Context{},
		),
		"session-spend / unpriced model": goldenSegmentPass(
			t,
			sessionSpend.New(func() (float64, bool) { return 0, false }, money.Dollar()),
			"",
			segment.Context{},
		),
		"session-spend / nothing spent yet": goldenSegmentPass(
			t,
			sessionSpend.New(func() (float64, bool) { return 0, true }, money.Dollar()),
			"",
			segment.Context{},
		),
		"session-spend / a fraction of a cent": goldenSegmentPass(
			t,
			sessionSpend.New(func() (float64, bool) { return 0.000_42, true }, money.Dollar()),
			"",
			segment.Context{},
		),
		"session-spend / part way through a session": goldenSegmentPass(
			t,
			sessionSpend.New(func() (float64, bool) { return 1.284_5, true }, money.Dollar()),
			"",
			segment.Context{},
		),
		"session-spend / a long session": goldenSegmentPass(
			t,
			sessionSpend.New(func() (float64, bool) { return 42.5, true }, money.Dollar()),
			"",
			segment.Context{},
		),
		"session-spend / converted to pounds": goldenSegmentPass(
			t,
			sessionSpend.New(func() (float64, bool) { return 1.284_5, true }, money.In("GBP", 0.782)),
			"",
			segment.Context{},
		),
		"session-spend / a currency without a symbol": goldenSegmentPass(
			t,
			sessionSpend.New(func() (float64, bool) { return 10, true }, money.In("HUF", 356.4)),
			"",
			segment.Context{},
		),
		"session-emoji": goldenSegmentPass(
			t,
			sessionEmoji.New("tame-impala"),
			"",
			segment.Context{},
		),
		"session-emoji / unknown animal": goldenSegmentPass(
			t,
			sessionEmoji.New("brave-tester"),
			"",
			segment.Context{},
		),
		"session-emoji / retired animal": goldenSegmentPass(
			t,
			sessionEmoji.New(retiredAnimalSession),
			"",
			segment.Context{},
		),
		"session-name / retired animal": goldenSegmentPass(
			t,
			sessionName.New(retiredAnimalSession, "/state/sessions/"+retiredAnimalSession, isPersisted),
			"emoji = true",
			segment.Context{},
		),
		"local-time / custom format": goldenSegmentPass(
			t,
			localTime.New(func() time.Time { return at }),
			`format = "15:04:05"`,
			segment.Context{},
		),
		"local-time / default format": goldenSegmentPass(
			t,
			localTime.New(func() time.Time { return at }),
			"",
			segment.Context{},
		),
		"mode-toggle / all granted": goldenSegmentPass(
			t,
			modeToggle.New(caps.All, func() bool { return false }),
			"",
			segment.Context{},
		),
		"mode-toggle / pending prefix": goldenSegmentPass(
			t,
			modeToggle.New(func() caps.Set { return caps.Read }, func() bool { return true }),
			"",
			segment.Context{},
		),
		"mode-toggle / read only": goldenSegmentPass(
			t,
			modeToggle.New(func() caps.Set { return caps.Read }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"mode-toggle / network only": goldenSegmentPass(
			t,
			modeToggle.New(func() caps.Set { return caps.Read | caps.Network }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"mode-toggle / lookup only": goldenSegmentPass(
			t,
			modeToggle.New(func() caps.Set { return caps.Read | caps.Lookup }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"exposed-ports / empty": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenExposedPorts(),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / host to sandbox": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenExposedPorts(8000, 8080),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / associated starting job": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenAssociatedPort(jobs.StateStarting),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / associated running job": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenAssociatedPort(jobs.StateRunning),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / associated stopping job": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenAssociatedPort(jobs.StateStopping),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / associated failed job": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenAssociatedPort(jobs.StateFailed),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / associated complete job": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenAssociatedPort(jobs.StateComplete),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / associated stopped job": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenAssociatedPort(jobs.StateStopped),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / associated job ended with session": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenAssociatedPort(jobs.StateEnded),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / associated absent job": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenAssociatedPortWithoutJob(),
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / sandbox to host": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenExposedPorts(),
				goldenLocalPorts(3000, 6000),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / both directions": goldenSegmentPass(
			t,
			exposedPorts.New(
				goldenExposedPorts(8000),
				goldenLocalPorts(3000),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / configured hostname": goldenSegmentPass(
			t,
			exposedPorts.New(
				exposedPorts.Routes{
					GetRoutes: func() []portgrant.Route { return []portgrant.Route{{Port: 8000}} },
					Hostname:  "preview-" + goldenSessionName + ".test",
				},
				goldenLocalPorts(),
			),
			"",
			segment.Context{},
		),
		"exposed-ports / both directions constrained": goldenFittedSegmentPass(
			t,
			exposedPorts.New(
				goldenExposedPorts(8000, 8080),
				goldenLocalPorts(3000, 6000),
			),
			"",
			segment.Context{},
			16,
		),
		"path-grants / empty": goldenSegmentPass(
			t,
			pathGrants.New(func() []pathgrant.Grant { return nil }),
			"",
			segment.Context{},
		),
		"path-grants / read and write": goldenSegmentPass(
			t,
			pathGrants.New(func() []pathgrant.Grant {
				return []pathgrant.Grant{
					{Path: "/reference", Access: pathgrant.ReadAccess},
					{Path: "/output", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
				}
			}),
			"",
			segment.Context{},
		),
		"path-grants / every access": goldenSegmentPass(
			t,
			pathGrants.New(func() []pathgrant.Grant {
				return []pathgrant.Grant{
					{Path: "/reference", Access: pathgrant.ReadAccess},
					{Path: "/output", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
					{Path: "/tools", Access: pathgrant.ReadAccess | pathgrant.ExecAccess},
					{Path: "/build", Access: pathgrant.ReadAccess | pathgrant.WriteAccess | pathgrant.ExecAccess},
				}
			}),
			"",
			segment.Context{},
		),
		"path-grants / many constrained": goldenFittedSegmentPass(
			t,
			pathGrants.New(func() []pathgrant.Grant {
				grants := make([]pathgrant.Grant, 50)
				for i := range grants {
					grants[i] = pathgrant.Grant{
						Path:   fmt.Sprintf("/path-%02d", i+1),
						Access: pathgrant.ReadAccess,
					}
				}
				return grants
			}),
			"",
			segment.Context{},
			36,
		),
		"path-grants / duplicate basenames": goldenSegmentPass(
			t,
			pathGrants.New(func() []pathgrant.Grant {
				return []pathgrant.Grant{
					{Path: "/one/reference", Access: pathgrant.ReadAccess},
					{Path: "/two/reference", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
				}
			}),
			"",
			segment.Context{},
		),
		"scroll-overflow / down": goldenSegmentPass(
			t,
			scrollOverflow.New,
			`direction = "down"`,
			segment.Context{HiddenLinesBelow: 7},
		),
		"scroll-overflow / empty": goldenSegmentPass(
			t,
			scrollOverflow.New,
			`direction = "up"`,
			segment.Context{},
		),
		"scroll-overflow / up": goldenSegmentPass(
			t,
			scrollOverflow.New,
			`direction = "up"`,
			segment.Context{HiddenLinesAbove: 3},
		),
		"subscription-usage / not applicable": goldenSegmentPass(
			t,
			subUsage.New(subUsage.Settings{Now: func() time.Time { return at }}),
			"",
			segment.Context{},
		),
		"subscription-usage / even burn": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 40, ResetsAt: at.Add(150 * time.Minute)},
				{Duration: 7 * 24 * time.Hour, Percent: 12, ResetsAt: at.Add(6 * 24 * time.Hour)},
			},
		}),
		"subscription-usage / ahead of pace": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 28, ResetsAt: at.Add(4 * time.Hour)},
			},
		}),
		"subscription-usage / near the limit": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 95, ResetsAt: at.Add(4 * time.Hour)},
			},
		}),
		"subscription-usage / spent": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{
					Duration:  5 * time.Hour,
					Percent:   100,
					ResetsAt:  at.Add(time.Hour),
					IsLimited: true,
				},
			},
		}),
		"subscription-usage / window already elapsed": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 68, ResetsAt: at.Add(-time.Minute)},
			},
		}),
		"subscription-usage / no reset reported": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 7 * 24 * time.Hour, Percent: 6},
			},
		}),
		"subscription-usage / weekly plan window": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 7 * 24 * time.Hour, Percent: 6, ResetsAt: at.Add(6 * 24 * time.Hour)},
			},
		}),
		"subscription-usage / metered model in use": goldenUsagePass(
			t,
			at,
			"gpt-5.3-codex-spark",
			usageReport{windows: meteredWindows(at)},
		),
		"subscription-usage / metered model not in use": goldenUsagePass(
			t,
			at,
			"gpt-5.6-sol",
			usageReport{windows: meteredWindows(at)},
		),
		"subscription-usage / only other models metered": goldenUsagePass(
			t,
			at,
			"claude-sonnet-4-6",
			usageReport{windows: []agent.UsageWindow{
				{
					Duration: 7 * 24 * time.Hour,
					Percent:  3,
					ResetsAt: at.Add(6 * 24 * time.Hour),
					Scope:    "opus",
				},
			}},
		),
		"subscription-usage / three windows": goldenUsagePass(t, at, "kimi-k3", usageReport{
			windows: []agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 39, ResetsAt: at.Add(4 * time.Hour)},
				{Duration: 7 * 24 * time.Hour, Percent: 15, ResetsAt: at.Add(6 * 24 * time.Hour)},
				{Duration: 30 * 24 * time.Hour, Percent: 13, ResetsAt: at.Add(20 * 24 * time.Hour)},
			},
		}),
		"subscription-usage / refused": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			err: &req.StatusError{Status: 401, Message: "the key is not yours"},
		}),
		"subscription-usage / unreachable": goldenUsagePass(t, at, "gpt-5.6-sol", usageReport{
			err: errors.New("the endpoint is sulking"),
		}),
		"subscription-usage / nothing reported yet": goldenUsagePass(
			t,
			at,
			"gpt-5.6-sol",
			usageReport{},
		),
		"subscription-usage / another session's snapshot": goldenUsageFromCache(
			t,
			at,
			"gpt-5.6-sol",
			[]agent.UsageWindow{
				{Duration: 5 * time.Hour, Percent: 40, ResetsAt: at.Add(150 * time.Minute)},
				{Duration: 7 * 24 * time.Hour, Percent: 12, ResetsAt: at.Add(6 * 24 * time.Hour)},
			},
		),
		"subscription-usage / updated by another session": goldenUsageUpdatedFromCache(t, at),
		"subscription-usage / refreshing keeps the figures": goldenUsagePass(
			t,
			at,
			"gpt-5.6-sol",
			usageReport{
				windows: []agent.UsageWindow{
					{Duration: 5 * time.Hour, Percent: 40, ResetsAt: at.Add(150 * time.Minute)},
				},
				thenErr: &req.StatusError{Status: 429, Message: "slow down"},
			},
		),
		"turn-timer / user turn": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing {
				return turn.Timing{UserTurn: 12 * time.Minute, ModelTurn: 3 * time.Minute}
			}, func() bool {
				return false
			}),
			"",
			segment.Context{},
		),
		"turn-timer / model turn": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing {
				return turn.Timing{UserTurn: 3 * time.Minute, ModelTurn: time.Minute}
			}, func() bool {
				return true
			}),
			"",
			segment.Context{},
		),
		"turn-timer / nothing asked yet": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing { return turn.Timing{} }, func() bool { return false }),
			"",
			segment.Context{},
		),
		"turn-timer / part of a minute": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing {
				return turn.Timing{UserTurn: 40 * time.Second, ModelTurn: 20 * time.Second}
			}, func() bool {
				return true
			}),
			"",
			segment.Context{},
		),
		"turn-timer / turns past an hour": goldenSegmentPass(
			t,
			turnTimer.New(func() turn.Timing {
				return turn.Timing{UserTurn: 3*time.Hour + 5*time.Minute, ModelTurn: 95 * time.Minute}
			}, func() bool {
				return true
			}),
			"",
			segment.Context{},
		),
		"fast-mode / fast": goldenSegmentPass(
			t,
			fastMode.New(true),
			"",
			segment.Context{},
		),
		"fast-mode / standard": goldenSegmentPass(
			t,
			fastMode.New(false),
			"",
			segment.Context{},
		),
		"jobs / none": goldenSegmentPass(
			t,
			jobNames.New(jobsOf(), noPortRoutes, clockAt(at)),
			"",
			segment.Context{},
		),
		"jobs / running": goldenSegmentPass(
			t,
			jobNames.New(jobsOf(
				jobs.Snapshot{Name: "docs", State: jobs.StateRunning},
				jobs.Snapshot{Name: "watch", State: jobs.StateRunning},
			), noPortRoutes, clockAt(at)),
			"",
			segment.Context{},
		),
		"jobs / associated job named by port": goldenSegmentPass(
			t,
			jobNames.New(
				jobsOf(
					jobs.Snapshot{Name: "docs", State: jobs.StateRunning},
					jobs.Snapshot{Name: "watch", State: jobs.StateRunning},
				),
				func() []portgrant.Route {
					return []portgrant.Route{{Port: 8000, JobName: "docs"}}
				},
				clockAt(at),
			),
			"",
			segment.Context{},
		),
		"jobs / one being stopped among those running": goldenSegmentPass(
			t,
			jobNames.New(jobsOf(
				jobs.Snapshot{Name: "docs", State: jobs.StateRunning},
				jobs.Snapshot{Name: "watch", State: jobs.StateRunning},
				jobs.Snapshot{Name: "api", State: jobs.StateStopping},
			), noPortRoutes, clockAt(at)),
			"",
			segment.Context{},
		),
		"jobs / nothing is left of a closed session": goldenSegmentPass(
			t,
			jobNames.New(jobsOf(
				jobs.Snapshot{Name: "docs", State: jobs.StateEnded},
				jobs.Snapshot{Name: "bridge", State: jobs.StateEnded},
			), noPortRoutes, clockAt(at)),
			"",
			segment.Context{},
		),
		"jobs / a ghost beside a live job": goldenSegmentPass(
			t,
			jobNames.New(jobsOf(
				jobs.Snapshot{Name: "docs", State: jobs.StateRunning},
				jobs.Snapshot{Name: "builder", State: jobs.StateComplete, EndedAt: at.Add(-5 * time.Second)},
			), noPortRoutes, clockAt(at)),
			"",
			segment.Context{},
		),
		"jobs / a ghost whose linger is up": goldenSegmentPass(
			t,
			jobNames.New(jobsOf(
				jobs.Snapshot{Name: "docs", State: jobs.StateRunning},
				jobs.Snapshot{Name: "builder", State: jobs.StateComplete, EndedAt: at.Add(-time.Minute)},
				jobs.Snapshot{Name: "check", State: jobs.StateFailed, EndedAt: at.Add(-time.Hour)},
			), noPortRoutes, clockAt(at)),
			"",
			segment.Context{},
		),
		"jobs / failures alone": goldenSegmentPass(
			t,
			jobNames.New(jobsOf(
				jobs.Snapshot{Name: "builder", State: jobs.StateFailed, EndedAt: at.Add(-time.Second)},
				jobs.Snapshot{Name: "api", State: jobs.StateFailed, EndedAt: at.Add(-2 * time.Second)},
			), noPortRoutes, clockAt(at)),
			"",
			segment.Context{},
		),
		"jobs / failures beside those running": goldenSegmentPass(
			t,
			jobNames.New(jobsOf(
				jobs.Snapshot{Name: "docs", State: jobs.StateRunning},
				jobs.Snapshot{Name: "builder", State: jobs.StateFailed, EndedAt: at.Add(-time.Second)},
			), noPortRoutes, clockAt(at)),
			"",
			segment.Context{},
		),
		"workspace-dir": goldenSegmentPass(
			t,
			workspaceDir.New(work.At("/workspace/currents")),
			"",
			segment.Context{},
		),
		"workspace-dir / full": goldenSegmentPass(
			t,
			workspaceDir.New(work.At("/workspace/currents")),
			`type = "full"`,
			segment.Context{},
		),
		"workspace-dir / short below home": goldenSegmentPass(
			t,
			workspaceDir.New(work.At("/user/kevin/modular/currents")),
			`type = "short"`,
			segment.Context{},
		),
		"workspace-dir / short outside home": goldenSegmentPass(
			t,
			workspaceDir.New(work.At("/workspace/currents")),
			`type = "short"`,
			segment.Context{},
		),
	}

	modelEffortLevels := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
	for _, effort := range modelEffortLevels {
		passes["active-model / "+effort+" effort"] = goldenSegmentPass(
			t,
			activeModel.New(activeModel.Settings{Name: "gpt-5.6-sol", Effort: effort, EffortLevels: modelEffortLevels}),
			"",
			segment.Context{},
		)
	}
	passes["active-model / fast"] = goldenSegmentPass(
		t,
		activeModel.New(activeModel.Settings{Name: "gpt-5.6-sol", Effort: "high", EffortLevels: modelEffortLevels, IsFast: true}),
		"",
		segment.Context{},
	)
	passes["subscription-usage / boundless"] = goldenSegmentPass(
		t,
		subUsage.New(subUsage.Settings{IsSimulated: true}),
		"",
		segment.Context{},
	)
	passes["active-model / simulation"] = goldenSegmentPass(
		t,
		activeModel.New(activeModel.Settings{
			Name:         "simulation",
			Effort:       "high",
			EffortLevels: modelEffortLevels,
			IsSimulated:  true,
		}),
		"",
		segment.Context{},
	)
	effortLadders := []struct {
		name   string
		model  string
		effort string
		levels []string
	}{
		{name: "deepseek high", model: "deepseek-v4-pro", effort: "high", levels: []string{"high", "max"}},
		{name: "deepseek max", model: "deepseek-v4-pro", effort: "max", levels: []string{"high", "max"}},
		{name: "opus medium", model: "claude-opus-5", effort: "medium", levels: []string{"low", "medium", "high", "xhigh", "max"}},
		{name: "ollama thinking off", model: "qwen3.8:27b", effort: "none", levels: []string{"none", "low", "medium", "high"}},
		{name: "ollama without thinking", model: "llama3.3:70b", effort: "none", levels: []string{"none"}},
		{name: "sparse ladder", model: "gpt-5.6-sol", effort: "high", levels: []string{"none", "high"}},
	}
	for _, ladder := range effortLadders {
		passes["active-model / "+ladder.name] = goldenSegmentPass(
			t,
			activeModel.New(activeModel.Settings{Name: ladder.model, Effort: ladder.effort, EffortLevels: ladder.levels}),
			"",
			segment.Context{},
		)
	}

	compareWithGolden(t, "segments", ".ansi", passes)
	compareWithGolden(t, "segments", ".screen", shownPasses(t, passes))
}

func writeStoredJournal(t *testing.T, directory string, name string, started string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(`{"kind":"head","time":%q,"id":%q,"name":%q}`+"\n", started, name, name)
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeStoredSession(t *testing.T, directory string, workspaceDir string, name string, started string) {
	t.Helper()

	writeStoredJournal(t, directory, name, started)

	meta := fmt.Sprintf(
		`{"version":%d,"name":%q,"data":{"workspaceDir":%q},"started":%q,"touched":%q}`+"\n",
		session.MetaFormat, name, workspaceDir, started, started,
	)
	if err := os.WriteFile(filepath.Join(directory, name, "meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsComeFromJournalParsing(t *testing.T) {
	directory := t.TempDir()
	meta := json.RawMessage(`{"workspaceDir":"/home/alice/project","model":"gpt"}`)
	writer, err := session.Create(directory, meta, meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	sessions, err := sessions.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %d", len(sessions))
	}
	if sessions[0].Name != writer.Name() || sessions[0].WorkspaceDir != "/home/alice/project" {
		t.Errorf("unexpected session: %+v", sessions[0])
	}
	if sessions[0].Title != "hello" {
		t.Errorf("expected the provisional title, got %q", sessions[0].Title)
	}
	if sessions[0].StartedAt.IsZero() {
		t.Error("expected the session start time")
	}
}

func TestChoosingWithoutStoredSessionsFails(t *testing.T) {
	if _, err := sessions.Choose(t.TempDir(), work.At(t.TempDir()), nil, nil); err == nil {
		t.Error("expected an empty session list to fail")
	}
}

func TestChoosingOnlyOffersTheSessionsOfTheCurrentWorkspace(t *testing.T) {
	directory := t.TempDir()
	workspaceDir := t.TempDir()

	for _, sessionWorkspaceDir := range []string{workspaceDir, t.TempDir()} {
		meta := fmt.Appendf(nil, `{"workspaceDir":%q}`, sessionWorkspaceDir)
		writer, err := session.Create(directory, meta, meta)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "hello"}); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	loadedSessions, err := sessions.Load(directory)
	if err != nil {
		t.Fatal(err)
	}

	chosen := sessions.InWorkspace(loadedSessions, work.At(workspaceDir))
	if len(chosen) != 1 || chosen[0].WorkspaceDir != workspaceDir {
		t.Fatalf("expected only the session of this workspace, got %+v", chosen)
	}

	if _, err := sessions.Choose(directory, work.At(t.TempDir()), nil, nil); err == nil {
		t.Error("expected a workspace without sessions to fail")
	}
}

func TestChoosingASessionFromANewerOhAdvisesAnUpgrade(t *testing.T) {
	directory := t.TempDir()
	name := "able-dolphin"

	if err := os.MkdirAll(filepath.Join(directory, name), 0o700); err != nil {
		t.Fatal(err)
	}

	head := fmt.Sprintf(
		`{"kind":"head","time":"2026-08-01T00:00:00Z","version":%d,"id":%q,"name":%q}`+"\n",
		session.JournalFormat+1, name, name,
	)
	if err := os.WriteFile(filepath.Join(directory, name, "session.jsonl"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := sessions.Choose(directory, work.At(t.TempDir()), nil, nil)
	if err == nil {
		t.Fatal("expected the newer session to be refused")
	}
	if !strings.Contains(err.Error(), "upgrade oh") {
		t.Errorf("expected an upgrade to be advised, got %v", err)
	}
	if strings.Contains(err.Error(), "oh --ctl migrate") {
		t.Errorf("expected migration not to be advised for a newer format, got %v", err)
	}
}

func TestChoosingAnOutdatedSessionAdvisesMigration(t *testing.T) {
	directory := t.TempDir()
	writeStoredJournal(t, directory, "able-dolphin", "2026-08-01T00:00:00Z")

	_, err := sessions.Choose(directory, work.At(t.TempDir()), nil, nil)
	if err == nil {
		t.Fatal("expected the outdated session to be refused")
	}
	if !strings.Contains(err.Error(), "run `oh --ctl migrate`") {
		t.Errorf("expected migration advice, got %v", err)
	}
	if strings.Contains(err.Error(), "meta.json") {
		t.Errorf("expected the missing metadata implementation detail to be hidden, got %v", err)
	}
}

func TestSlashCommandRunsImmediately(t *testing.T) {
	didRun := false
	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(_ slash.Context, arguments slash.Arguments) error {
			if !slices.Equal(arguments.Fields, []string{"one", "two"}) {
				t.Errorf("got arguments %v", arguments.Fields)
			}
			didRun = true
			return nil
		},
	})

	self := slashCommandFixture(t, caps.Read|caps.Shell)
	self.slash.commands = fixtureCommands
	if got := self.handleCommand("/fixture one two"); got != dispatch.Handled {
		t.Fatalf("got slash input result %d", got)
	}
	if !didRun {
		t.Error("expected the command to run immediately")
	}
}

func TestPathGrantCommandBecomesPendingAccessAndUpdatesTheModel(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	grants := preparePathGrantCommands(t, self, openTestWorkspace(t, t.TempDir()))
	self.settleAccess()
	directory := t.TempDir()

	if got := self.handleCommand("/grant r " + directory); got != dispatch.Handled {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.pendingNotices.items) != 1 || self.pendingNotices.items[0].state.Kind != pathgrant.Change {
		t.Fatalf("got pending input %#v", self.pendingNotices.items)
	}
	if current := grants.GetCurrent(); len(current) != 1 || current[0].Path != directory {
		t.Errorf("got grants %#v", current)
	}

	self.start("continue")
	if message := grants.Inject(); message != "" {
		t.Errorf("model access was not advanced: %q", message)
	}
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	hasGrantEvent := false
	for _, event := range self.recordedEvents {
		hasGrantEvent = hasGrantEvent || event.Kind == pathgrant.Change
	}
	if !hasGrantEvent {
		t.Error("settled conversation did not record the grant")
	}
}

func TestSandboxToHostChangeBecomesPendingAccessAndUpdatesTheModel(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	var exposed []uint16
	grants := portgrant.NewSandboxToHost(portgrant.SandboxToHostExposer{
		Expose: func(port uint16) error {
			exposed = append(exposed, port)
			return nil
		},
		Hide: func(uint16) error { return nil },
	}, nil)
	self.sandboxToHost = grants
	self.settleAccess()

	event, err := grants.Expose(8080)
	if err != nil {
		t.Fatal(err)
	}
	self.emitCommandEvent(event)
	if len(self.pendingNotices.items) != 1 || self.pendingNotices.items[0].state.Kind != portgrant.SandboxToHostChange {
		t.Fatalf("got pending input %#v", self.pendingNotices.items)
	}
	for _, recordedEvent := range self.recordedEvents {
		if recordedEvent.Kind == portgrant.SandboxToHostChange {
			t.Fatal("the change was recorded before it could be sent to the model")
		}
	}

	self.settleAccess()
	if message := self.takeSettledNotes(); !strings.Contains(message, "8080") {
		t.Errorf("model access did not name the port: %q", message)
	}
	if message := grants.Inject(); message != "" {
		t.Errorf("model access was not advanced: %q", message)
	}
	if self.recordedEvents[len(self.recordedEvents)-1].Kind != portgrant.SandboxToHostChange {
		t.Errorf("got recorded events %#v", self.recordedEvents)
	}
}

func TestRestoredGrantCorrectionRemainsPendingUntilATurnCanTellTheModel(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	workspace := openTestWorkspace(t, t.TempDir())
	files := file.New(workspace.GetRoot(), caps.RefuseWrite(self.mode))
	pathAccess, err := shell.NewPathAccess(files, self.mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pathAccess.Close)

	missing := filepath.Join(t.TempDir(), "missing")
	grants, result := pathgrant.NewRestored(workspace, pathAccess, []pathgrant.Grant{{
		Path:   missing,
		Access: pathgrant.ReadAccess,
	}})
	if len(result.Failures) != 1 {
		t.Fatalf("got restoration failures %#v", result.Failures)
	}
	self.pathGrants = grants
	self.settledCaps = self.mode.Current()
	correction, err := pathgrant.ChangeEvent(missing, grants.GetCurrent())
	if err != nil {
		t.Fatal(err)
	}
	self.queuePathGrantChange(correction)
	self.initialiseAccess()

	for _, event := range self.recordedEvents {
		if event.Kind == pathgrant.Change {
			t.Fatal("correction was recorded before a turn could tell the model")
		}
	}
	self.start("continue")
	if message := grants.Inject(); message != "" {
		t.Errorf("model access was not advanced: %q", message)
	}
	isFound := false
	for _, event := range self.recordedEvents {
		isFound = isFound || event.Kind == pathgrant.Change
	}
	if !isFound {
		t.Error("correction was not recorded with the turn")
	}
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()
}

func TestPathGrantCommandInterruptsAnActiveTurn(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	preparePathGrantCommands(t, self, openTestWorkspace(t, t.TempDir()))
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	if got := self.handleCommand("/grant r " + t.TempDir()); got != dispatch.Handled {
		t.Fatalf("got slash input result %d", got)
	}
	if !self.currentTurn.Cancelled() {
		t.Error("active turn was not interrupted")
	}
	pending := self.queuedTurn.Peek()
	if !pending.AccessChange {
		t.Errorf("queued turn is %#v", pending)
	}
}

func TestARegrantedPathIsForgottenByEveryPendingMessage(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	grants := preparePathGrantCommands(t, self, openTestWorkspace(t, t.TempDir()))
	firstPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	self.handleCommand("/grant r " + firstPath)
	self.handleCommand("/grant r " + secondPath)
	self.handleCommand("/grant rw " + firstPath)
	self.handleCommand("/revoke " + firstPath)

	if len(self.pendingNotices.items) != 1 {
		t.Fatalf("got pending items %#v", self.pendingNotices.items)
	}
	recorded, found := pathgrant.LastRecorded([]agent.Event{self.pendingNotices.items[0].state})
	want := []pathgrant.Grant{{Path: secondPath, Access: pathgrant.ReadAccess}}
	if !found || !slices.Equal(recorded, want) {
		t.Errorf("got recorded grants %#v and %t", recorded, found)
	}
	if !grants.IsTold(firstPath) {
		t.Error("the re-granted path is still waiting to be told")
	}
}

func FuzzPendingPathGrantsSayWhatTheModelHasNotBeenTold(fuzzer *testing.F) {
	for _, commands := range []string{"", "\x00", "\x04", "\x00\x04", "\x00\x01\x04", "\x00\x05\x04", "\x00\x06\x0a\x04"} {
		fuzzer.Add(commands)
	}

	fuzzer.Fuzz(func(t *testing.T, commands string) {
		self := testConversation(t, &bytes.Buffer{})
		grants := preparePathGrantCommands(t, self, openTestWorkspace(t, t.TempDir()))
		self.settledCaps = self.mode.Current()

		paths := make([]string, 3)
		for i := range paths {
			path, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			paths[i] = path
		}

		for _, command := range []byte(commands) {
			path := paths[int(command/6)%len(paths)]
			switch command % 6 {
			case 0:
				self.handleCommand("/grant r " + path)
			case 1:
				self.handleCommand("/grant rw " + path)
			case 2:
				self.handleCommand("/grant rx " + path)
			case 3:
				self.handleCommand("/grant rxw " + path)
			case 4:
				self.handleCommand("/revoke " + path)
			case 5:
				self.settleAccess()
			}
			assertPendingPathGrants(t, self, grants, paths)
		}

		self.settleAccess()
		assertPendingPathGrants(t, self, grants, paths)
		if len(self.pendingNotices.items) != 0 {
			t.Fatalf("a settled turn left %d pending messages", len(self.pendingNotices.items))
		}
	})
}

func assertPendingPathGrants(t *testing.T, self *App, grants *pathgrant.Grants, paths []string) {
	t.Helper()

	pendingPaths := map[string]bool{}
	lastState := agent.Event{}

	for _, item := range self.pendingNotices.items {
		if item.state.Kind != pathgrant.Change {
			continue
		}
		if pendingPaths[item.state.Name] {
			t.Fatalf("%s waits in two pending messages", item.state.Name)
		}
		pendingPaths[item.state.Name] = true

		if _, isShown := pathgrant.Notice(item.state); !isShown {
			t.Fatalf("a pending path grant says nothing: %+v", item.state)
		}
		lastState = item.state
	}

	for _, path := range paths {
		if pendingPaths[path] == grants.IsTold(path) {
			t.Fatalf("%s waits=%t, but the model knows it=%t", path, pendingPaths[path], grants.IsTold(path))
		}
	}

	if lastState.Kind == "" {
		return
	}
	recorded, found := pathgrant.LastRecorded([]agent.Event{lastState})
	if !found {
		t.Fatal("the last pending message records no grants")
	}
	if want := grants.GetCurrent(); !slices.Equal(recorded, want) {
		t.Fatalf("the last pending message records %#v, and the session holds %#v", recorded, want)
	}
}

func TestAGrantTakenBackBeforeItIsSentLeavesNothingBehind(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	grants := preparePathGrantCommands(t, self, openTestWorkspace(t, t.TempDir()))
	firstPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	self.handleCommand("/grant r " + firstPath)
	self.handleCommand("/grant r " + secondPath)
	self.handleCommand("/revoke " + firstPath)

	if len(self.pendingNotices.items) != 1 {
		t.Fatalf("got pending items %#v", self.pendingNotices.items)
	}
	if name := self.pendingNotices.items[0].state.Name; name != secondPath {
		t.Errorf("got pending path %q", name)
	}

	recorded, found := pathgrant.LastRecorded([]agent.Event{self.pendingNotices.items[0].state})
	want := []pathgrant.Grant{{Path: secondPath, Access: pathgrant.ReadAccess}}
	if !found || !slices.Equal(recorded, want) {
		t.Errorf("got recorded grants %#v and %t", recorded, found)
	}
	if !grants.IsTold(firstPath) {
		t.Error("the taken-back path is still waiting to be told")
	}

	self.handleCommand("/revoke " + secondPath)
	if len(self.pendingNotices.items) != 0 {
		t.Errorf("got pending items %#v", self.pendingNotices.items)
	}
}

func TestAGrantChangedBeforeItIsSentReplacesItsNotice(t *testing.T) {
	self := testConversation(t, &bytes.Buffer{})
	preparePathGrantCommands(t, self, openTestWorkspace(t, t.TempDir()))
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	self.handleCommand("/grant r " + path)
	self.handleCommand("/grant rw " + path)

	if len(self.pendingNotices.items) != 1 {
		t.Fatalf("got pending items %#v", self.pendingNotices.items)
	}
	if text, _ := pathgrant.Notice(self.pendingNotices.items[0].state); !strings.Contains(text, "write access") {
		t.Errorf("got pending message %q", text)
	}
}

type pathGrantGoldenScenario int

const (
	pathGrantPending pathGrantGoldenScenario = iota
	pathGrantSettled
	pathGrantInterrupted
	pathGrantMissing
	pathGrantDuplicate
	pathGrantMany
	pathGrantUnknownAccess
	pathGrantRevokeMissing
	pathGrantRestorePending
	pathGrantRestoreSettled
	pathGrantHomePath
	pathGrantHomePathMissing
	pathGrantExec
	pathGrantTakenBack
	pathGrantReplaced
)

func TestGoldenPathGrantLifecycleDrawsEveryVisibleState(t *testing.T) {
	passes := map[string]func() string{
		"active turn interrupted":       func() string { return pathGrantGoldenStream(t, pathGrantInterrupted) },
		"duplicate grant rejected":      func() string { return pathGrantGoldenStream(t, pathGrantDuplicate) },
		"executable grant":              func() string { return pathGrantGoldenStream(t, pathGrantExec) },
		"grant replaced before sending": func() string { return pathGrantGoldenStream(t, pathGrantReplaced) },
		"grant taken back before sending": func() string {
			return pathGrantGoldenStream(t, pathGrantTakenBack)
		},
		"home path granted":              func() string { return pathGrantGoldenStream(t, pathGrantHomePath) },
		"missing home path rejected":     func() string { return pathGrantGoldenStream(t, pathGrantHomePathMissing) },
		"many grants truncated":          func() string { return pathGrantGoldenStream(t, pathGrantMany) },
		"missing path rejected":          func() string { return pathGrantGoldenStream(t, pathGrantMissing) },
		"pending grant":                  func() string { return pathGrantGoldenStream(t, pathGrantPending) },
		"restoration correction pending": func() string { return pathGrantGoldenStream(t, pathGrantRestorePending) },
		"restoration correction settled": func() string { return pathGrantGoldenStream(t, pathGrantRestoreSettled) },
		"revoke without grant rejected":  func() string { return pathGrantGoldenStream(t, pathGrantRevokeMissing) },
		"settled into next turn":         func() string { return pathGrantGoldenStream(t, pathGrantSettled) },
		"unknown access rejected":        func() string { return pathGrantGoldenStream(t, pathGrantUnknownAccess) },
	}
	compareWithGolden(t, "path-grant-lifecycle", ".ansi", passes)
	compareWithGolden(t, "path-grant-lifecycle", ".screen", shownPasses(t, passes))
}

func TestGoldenPortDirectionNoticesMatchGolden(t *testing.T) {
	passes := map[string]func() string{
		"pending sandbox to host": func() string {
			var screenOutput bytes.Buffer
			self := testConversation(t, &screenOutput)
			self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
			grants := portgrant.NewSandboxToHost(portgrant.SandboxToHostExposer{
				Expose: func(uint16) error { return nil },
				Hide:   func(uint16) error { return nil },
			}, nil)
			self.sandboxToHost = grants
			self.settleAccess()
			event, err := grants.Expose(3000)
			if err != nil {
				t.Fatal(err)
			}
			self.emitCommandEvent(event)
			self.settlePendingInput()
			self.screen.End()
			return screenOutput.String()
		},
		"recorded directions": func() string {
			var screenOutput bytes.Buffer
			self := testConversation(t, &screenOutput)
			self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

			hostToSandbox, err := portgrant.HostToSandboxChangeEvent("127.9.9.9", 8080, []portgrant.Route{{Port: 8080}})
			if err != nil {
				t.Fatal(err)
			}
			sandboxToHost, err := portgrant.SandboxToHostChangeEvent(3000, []uint16{3000})
			if err != nil {
				t.Fatal(err)
			}
			self.notify(hostToSandbox)
			self.notify(sandboxToHost)
			sandboxToHost, err = portgrant.SandboxToHostChangeEvent(3000, nil)
			if err != nil {
				t.Fatal(err)
			}
			hostToSandbox, err = portgrant.HostToSandboxChangeEvent("127.9.9.9", 8080, nil)
			if err != nil {
				t.Fatal(err)
			}
			self.notify(sandboxToHost)
			self.notify(hostToSandbox)
			self.screen.End()
			return screenOutput.String()
		},
		"recorded ports in a row": func() string { return recordedPortsInARow(t, replayLines) },
	}
	compareWithGolden(t, "port-directions", ".ansi", passes)
	compareWithGolden(t, "port-directions", ".screen", shownPasses(t, passes))
}

func recordedPortsInARow(t *testing.T, lines int) string {
	t.Helper()

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, lines)

	var ports []uint16
	for _, port := range []uint16{8001, 8002, 8003} {
		ports = append(ports, port)
		event, err := portgrant.HostToSandboxChangeEvent("127.9.9.9", port, portRoutes(ports...))
		if err != nil {
			t.Fatal(err)
		}
		self.notify(event)
	}
	self.screen.End()

	return screenOutput.String()
}

func TestNoticesOnAShortTerminalAreRepairedRatherThanFrozen(t *testing.T) {
	requireSameVisibleScreen(
		t,
		"notices drawn on a terminal too short to hold them differ from the same ones with room to draw",
		recordedPortsInARow(t, replayLines),
		recordedPortsInARow(t, shortLines),
	)
}

func pathGrantGoldenStream(t *testing.T, scenario pathGrantGoldenScenario) string {
	t.Helper()

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	workspace := openTestWorkspace(t, t.TempDir())
	preparePathGrantCommands(t, self, workspace)
	self.settleAccess()
	registry := availableSegments(workspace, self)
	live, err := configFrom(t, `
		[bar.top]
		left = [
			{ segment = "activity-spinner", idle = "·✦·", frames = ["✦··"], rate = "125ms" },
			{ segment = "path-grants" },
		]
		center = []
		right = [{ segment = "session-name" }]

		[bar.bottom]
		left = []
		center = []
		right = []
	`).BuildLive(registry)
	if err != nil {
		t.Fatal(err)
	}
	self.display.bar = bar.NewConfiguration(registry, live.SegmentLayout)
	referencePath := stableGoldenPath(t, "reference", true)
	missingPath := stableGoldenPath(t, "missing", false)
	homePath := stableGoldenPath(t, "user", true)
	t.Setenv("HOME", homePath)

	inputLine := edit.NewInput(nil)
	self.inputLine = inputLine
	self.show(inputLine)

	switch scenario {
	case pathGrantPending:
		self.handleCommand("/grant r " + referencePath)
	case pathGrantSettled:
		self.handleCommand("/grant r " + referencePath)
		self.start("continue")
		self.waitForCurrentTurn()
	case pathGrantInterrupted:
		turnEvents := make(chan TurnEvent)
		self.currentTurn = Turn{
			Stream: testTurnStream(
				turnEvents,
				func(error) { close(turnEvents) },
				turn.State{Running: true},
			),
			painter: self.newPainter(true),
		}
		self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "still working"})
		self.handleCommand("/grant r " + referencePath)
		self.waitForCurrentTurn()
		self.waitForCurrentTurn()
	case pathGrantMissing:
		self.handleCommand("/grant r " + missingPath)
	case pathGrantHomePath:
		if err := os.Mkdir(filepath.Join(homePath, "reference"), 0o700); err != nil {
			t.Fatal(err)
		}
		self.handleCommand("/grant r ~/reference")
	case pathGrantHomePathMissing:
		self.handleCommand("/grant r ~/missing")
	case pathGrantExec:
		self.handleCommand("/grant rx " + referencePath)
	case pathGrantTakenBack:
		self.handleCommand("/grant r " + referencePath)
		self.handleCommand("/revoke " + referencePath)
	case pathGrantReplaced:
		self.handleCommand("/grant r " + referencePath)
		self.handleCommand("/grant rw " + referencePath)
	case pathGrantDuplicate:
		self.handleCommand("/grant r " + referencePath)
		self.handleCommand("/grant r " + referencePath)
	case pathGrantMany:
		pathAccess := pathGrantAccess(t, self.mode, workspace)
		manyGrants := stableManyGrantGoldenPaths(t)
		grants, result := pathgrant.NewRestored(workspace, pathAccess, manyGrants)
		if len(result.Failures) != 0 {
			t.Fatalf("got restoration failures %#v", result.Failures)
		}
		self.pathGrants = grants
	case pathGrantUnknownAccess:
		self.handleCommand("/grant rwz " + referencePath)
	case pathGrantRevokeMissing:
		self.handleCommand("/revoke " + referencePath)
	case pathGrantRestorePending, pathGrantRestoreSettled:
		pathAccess := pathGrantAccess(t, self.mode, workspace)
		grants, result := pathgrant.NewRestored(workspace, pathAccess, []pathgrant.Grant{{
			Path:   missingPath,
			Access: pathgrant.ReadAccess,
		}})
		if len(result.Failures) != 1 {
			t.Fatalf("got restoration failures %#v", result.Failures)
		}
		self.pathGrants = grants
		correction, err := pathgrant.ChangeEvent(missingPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		self.queuePathGrantChange(correction)
		self.notifyFailure("Temporary access could not be restored: path does not exist")
		self.show(inputLine)
		self.refreshPendingMessages()
		if scenario == pathGrantRestoreSettled {
			self.start("continue")
			self.waitForCurrentTurn()
		}
	}
	self.show(inputLine)

	stream := screenOutput.String()
	stream = strings.ReplaceAll(stream, referencePath, "/reference")
	stream = strings.ReplaceAll(stream, missingPath, "/missing")
	stream = strings.ReplaceAll(stream, homePath, "/user")
	stream = strings.ReplaceAll(stream, manyGrantParent(), "/many")
	return stream
}

func stableGoldenPath(t *testing.T, label string, shouldExist bool) string {
	t.Helper()

	parent := fmt.Sprintf("/tmp/oh-golden-%s-%010d", label, os.Getpid())
	path := filepath.Join(parent, label)
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if shouldExist {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func manyGrantParent() string {
	return fmt.Sprintf("/tmp/oh-many-grants-%010d", os.Getpid())
}

func stableManyGrantGoldenPaths(t *testing.T) []pathgrant.Grant {
	t.Helper()

	parent := manyGrantParent()
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })

	grants := make([]pathgrant.Grant, 50)
	for i := range grants {
		path := filepath.Join(parent, fmt.Sprintf("path-%02d", i+1))
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		access := pathgrant.ReadAccess
		if i%2 == 1 {
			access = pathgrant.ReadAccess | pathgrant.WriteAccess
		}
		grants[i] = pathgrant.Grant{Path: path, Access: access}
	}
	return grants
}

func pathGrantAccess(t *testing.T, mode *caps.Mode, workspace *work.Space) *shell.PathAccess {
	t.Helper()

	files := file.New(workspace.GetRoot(), caps.RefuseWrite(mode))
	access, err := shell.NewPathAccess(files, mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(access.Close)
	return access
}

func TestSlashCommandCanAddFeedback(t *testing.T) {
	assertSlashCommandFeedback(t, func(context slash.Context) {
		context.Notice("fixture notice")
	}, feedback.Message{Text: "fixture notice", Status: agent.InfoStatus})
}

func TestSlashCommandCanAddSelfStyledFeedback(t *testing.T) {
	assertSlashCommandFeedback(t, func(context slash.Context) {
		context.PlainNotice("fixture notice")
	}, feedback.Message{Text: "fixture notice", HasOwnStyle: true})
}

func TestPlainCommandFeedbackIsPrintedWithoutEnteringConversationHistory(t *testing.T) {
	var screenOutput bytes.Buffer
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&screenOutput)
	self.runMode.isPlain = true
	self.slash.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "help",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Notice("Commands:\n  /help")
			return nil
		},
	})

	if got := self.handleCommand("/help"); got != dispatch.Handled {
		t.Fatalf("got slash input result %d", got)
	}
	self.screen.End()

	if len(self.recordedEvents) != 0 {
		t.Errorf("plain command feedback entered conversation events: %+v", self.recordedEvents)
	}
	if !strings.Contains(style.Plain(screenOutput.String()), "Commands:\n  /help") {
		t.Errorf("plain command feedback was not printed: %q", style.Plain(screenOutput.String()))
	}
}

const plainInputDiff = "diff --git a/agent/agent.go b/agent/agent.go\n" +
	"index d483cf5..a478bfa 100644\n" +
	"@@ -1,3 +1,3 @@\n" +
	"-was\n" +
	"+is\n"

type plainInputScenario int

const (
	plainInputPiped plainInputScenario = iota
	plainInputPipedAfterPrompt
	plainInputPipedAndPrinted
	plainInputTypedLines
	plainInputPipedNothing
	plainInputPipedUnsized
)

func TestGoldenPlainInputDrawsEveryVisibleState(t *testing.T) {
	passes := map[string]func() string{
		"a piped prompt asks once": func() string { return plainInputStream(t, plainInputPiped) },
		"a piped prompt follows a given one": func() string {
			return plainInputStream(t, plainInputPipedAfterPrompt)
		},
		"a piped prompt printed":   func() string { return plainInputStream(t, plainInputPipedAndPrinted) },
		"typed lines ask one each": func() string { return plainInputStream(t, plainInputTypedLines) },
		"an empty pipe asks nothing": func() string {
			return plainInputStream(t, plainInputPipedNothing)
		},
		"a piped prompt with nothing to measure against": func() string {
			return plainInputStream(t, plainInputPipedUnsized)
		},
	}

	compareWithGolden(t, "plain-input", ".ansi", passes)
	compareWithGolden(t, "plain-input", ".screen", shownPasses(t, passes))
}

func plainInputStream(t *testing.T, scenario plainInputScenario) string {
	t.Helper()

	var screenOutput strings.Builder
	self := slashCommandFixture(t, caps.Read)
	self.agent = agent.New("", &plainTurnProvider{}, nil)
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	self.slash.commands = fixtureSnippetRegistry(t, nil)

	history := edit.NewHistory("", historyLimit)

	givenPrompt := ""
	pipedSource := plainInputDiff
	switch scenario {
	case plainInputPipedAfterPrompt:
		givenPrompt = "review this"
	case plainInputPipedNothing:
		pipedSource = "\n\n"
	case plainInputPipedUnsized:
		self.screen = output.New(&screenOutput)
	case plainInputPiped, plainInputPipedAndPrinted, plainInputTypedLines:
	}

	if scenario == plainInputTypedLines {
		self.runMode.isPlain = true
		self.acceptTypedLines(history, "", strings.NewReader("first question\nsecond question\n"))
		self.screen.End()

		return screenOutput.String()
	}

	pipedPrompt, err := startup.ReadPipedPrompt(strings.NewReader(pipedSource))
	if err != nil {
		t.Fatal(err)
	}

	if scenario == plainInputPipedAndPrinted {
		self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines).AppendOnly()
		self.runMode.isPrinting = true
		self.print(history, startup.JoinPipedPrompt(givenPrompt, pipedPrompt))
		requireNothingWasDrawnOver(t, screenOutput.String())

		return screenOutput.String()
	}

	self.runMode.isPlain = true
	self.acceptPlainInput(history, startup.JoinPipedPrompt(givenPrompt, pipedPrompt))
	self.screen.End()

	return screenOutput.String()
}

func recordedUserMessages(events []agent.Event) []string {
	var messages []string
	for _, event := range events {
		if event.Kind == agent.UserMessageEvent {
			messages = append(messages, event.Text)
		}
	}
	return messages
}

func TestAPipedPromptAsksOneQuestionOfEverythingItWasGiven(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.runMode.isPlain = true

	piped := "diff --git a/agent/agent.go b/agent/agent.go\nindex d483cf5..a478bfa 100644\n"
	prompt, err := startup.ReadPipedPrompt(strings.NewReader(piped))
	if err != nil {
		t.Fatal(err)
	}

	self.acceptPlainInput(edit.NewHistory("", historyLimit), prompt)

	want := []string{strings.TrimSpace(piped)}
	if got := recordedUserMessages(self.recordedEvents); !slices.Equal(got, want) {
		t.Errorf("got user messages %q, want %q", got, want)
	}
}

func TestTypedPlainLinesEachAskAQuestionOfTheirOwn(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.runMode.isPlain = true

	self.acceptTypedLines(edit.NewHistory("", historyLimit), "", strings.NewReader("one\ntwo\n"))

	want := []string{"one", "two"}
	if got := recordedUserMessages(self.recordedEvents); !slices.Equal(got, want) {
		t.Errorf("got user messages %q, want %q", got, want)
	}
}

func TestUnknownSlashCommandShowsOneErrorWhileReturnRepeatsAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.slash.commands = fixtureCommandRegistry(t)
	inputLine := edit.NewInput(nil)
	for _, value := range "/unknown" {
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	for range 100 {
		self.apply(inputLine, nil, key.Key{Code: key.Enter})
	}

	if got := inputLine.Text(); got != "/unknown" {
		t.Errorf("got input %q", got)
	}
	if len(self.recordedEvents) != 0 {
		t.Fatalf("repeated command errors entered conversation events: %v", self.recordedEvents)
	}
	want := "Command not found: /unknown (alt+enter to send)"
	if self.feedback.Message().Text != want || self.feedback.Message().Status != agent.ErrorStatus {
		t.Errorf("got feedback %+v, want failed %q", self.feedback.Message(), want)
	}
}

func TestUnknownSlashCommandDoesNotInterruptARunningTurn(t *testing.T) {
	var screenOutput bytes.Buffer
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&screenOutput)
	self.slash.commands = fixtureCommandRegistry(t)
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: "still working"})
	inputLine := edit.NewInput(nil)
	for _, value := range "/unknown" {
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, true)
	}

	self.acceptInput(inputLine, nil)

	if self.currentTurn.Cancelled() {
		t.Error("expected the running turn not to be interrupted")
	}
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: " on it"})
	self.currentTurn.painter.DrawEvent(agent.Event{Kind: agent.ModelReasoningEvent, Text: "still working on it"})
	self.screen.End()
	plainOutput := style.Plain(screenOutput.String())
	if self.feedback.Message().Text != "Command not found: /unknown (alt+enter to send)" {
		t.Errorf("command error was not retained as interface feedback: %+v", self.feedback.Message())
	}
	if strings.Contains(plainOutput, "Command not found") {
		t.Errorf("command error entered conversation scrollback: %q", plainOutput)
	}
	if strings.Count(plainOutput, "still working") != 1 {
		t.Errorf("expected the completed reasoning once, got %q", plainOutput)
	}
}

func TestSnippetKeepsItsInvocationInHistoryAndQueuesItsRenderedPrompt(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.slash.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"add": {Prompt: "Add the following:\n\n{{ .Arg }}", Arguments: snippets.ArgumentsRequired},
	})
	self.currentTurn = Turn{Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true})}

	historyPath := filepath.Join(t.TempDir(), "history")
	history := edit.NewHistory(historyPath, historyLimit)
	inputLine := edit.NewInput(history)
	for _, value := range "//add review this" {
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, true)
	}
	self.acceptInput(inputLine, history)

	want := []string{"Add the following:\n\nreview this"}
	if queued := self.currentTurn.GetInterjections(); !slices.Equal(queued, want) {
		t.Errorf("queued %q, want %q", queued, want)
	}
	body, err := os.ReadFile(historyPath) //nolint:gosec // the path is the test's own history file
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "//add review this\n" {
		t.Errorf("got history %q", body)
	}
	if inputLine.Text() != "" {
		t.Errorf("got input text %q", inputLine.Text())
	}
}

func TestSnippetKeepsTheLayoutOfAPastedArgument(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.slash.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"add": {Prompt: "Add the following:\n\n{{ .Arg }}", Arguments: snippets.ArgumentsRequired},
	})
	self.currentTurn = Turn{Stream: testTurnStream(nil, func(error) {}, turn.State{Running: true})}

	historyPath := filepath.Join(t.TempDir(), "history")
	history := edit.NewHistory(historyPath, historyLimit)
	inputLine := edit.NewInput(history)
	for _, value := range "//add " {
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, true)
	}

	inputLine.Apply(key.Key{Code: key.PasteStart}, true)
	for _, value := range "review this\n\n- one\n- two" {
		if value == '\n' {
			inputLine.Apply(key.Key{Code: key.Enter}, true)
			continue
		}
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, true)
	}
	inputLine.Apply(key.Key{Code: key.PasteEnd}, true)
	self.acceptInput(inputLine, history)

	want := []string{"Add the following:\n\nreview this\n\n- one\n- two"}
	if queued := self.currentTurn.GetInterjections(); !slices.Equal(queued, want) {
		t.Errorf("queued %q, want %q", queued, want)
	}
	body, err := os.ReadFile(historyPath) //nolint:gosec // the path is the test's own history file
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `//add review this\n\n- one\n- two`+"\n" {
		t.Errorf("got history %q", body)
	}
}

func TestSnippetWithoutArgumentsShowsUsageAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.slash.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"add": {Prompt: "Add the following:\n\n{{ .Arg }}", Arguments: snippets.ArgumentsRequired},
	})
	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	for _, value := range "//add" {
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(inputLine, history)

	if inputLine.Text() != "//add" {
		t.Errorf("got input text %q", inputLine.Text())
	}
	if len(self.recordedEvents) != 0 {
		t.Errorf("snippet usage entered conversation events: %+v", self.recordedEvents)
	}
	if self.feedback.Message().Text != "Usage: //add <args> (alt+enter to send)" {
		t.Errorf("got feedback %+v", self.feedback.Message())
	}
}

func TestPlainSnippetInputWaitsForTheRenderedPrompt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.slash.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"ask": {Prompt: "Question: {{index .Args 0}} / {{.Arg}}", Arguments: snippets.ArgumentsRequired},
	})

	historyPath := filepath.Join(t.TempDir(), "history")
	history := edit.NewHistory(historyPath, historyLimit)
	self.acceptPlainInput(history, "//ask why now")

	var userMessages []string
	for _, event := range self.recordedEvents {
		if event.Kind == agent.UserMessageEvent {
			userMessages = append(userMessages, event.Text)
		}
	}
	if !slices.Equal(userMessages, []string{"Question: why / why now"}) {
		t.Errorf("got user messages %q", userMessages)
	}
	body, err := os.ReadFile(historyPath) //nolint:gosec // the path is the test's own history file
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "//ask why now\n" {
		t.Errorf("got history %q", body)
	}
}

type cutShortProvider struct {
	sent int
}

func (*cutShortProvider) Configure(string, []tool.Definition)   {}
func (*cutShortProvider) AddUserMessage(string)                 {}
func (*cutShortProvider) AddToolResults([]agent.ToolCallResult) {}
func (*cutShortProvider) Dump() []json.RawMessage               { return nil }
func (*cutShortProvider) Load([]json.RawMessage)                {}

func (self *cutShortProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	if self.sent++; self.sent > 1 {
		yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "Carrying on."})
		yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true})
		return agent.Reply{}, nil
	}

	yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: "Half a thought."})
	yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true})

	return agent.Reply{}, nil
}

func TestATurnCutShortIsPokedAndWaitedForBeforeTheNextInput(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.agent = agent.New("", &cutShortProvider{}, nil)

	self.ask(edit.NewHistory("", historyLimit), "get on with it")

	if self.currentTurn.Running() {
		t.Error("the poked turn was left running")
	}

	var answers []string
	for _, event := range self.recordedEvents {
		if event.Kind == agent.ModelMessageEvent {
			answers = append(answers, event.Text)
		}
	}
	if messages := submittedTexts(self.recordedEvents); !slices.Equal(messages, []string{"get on with it", turn.PokeMessage}) {
		t.Errorf("got submitted messages %q", messages)
	}
	if !slices.Equal(answers, []string{"Carrying on."}) {
		t.Errorf("got answers %q", answers)
	}
}

func TestSnippetTemplateErrorsAreReportedAndKeepTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.slash.commands = fixtureSnippetRegistry(t, map[string]snippets.Definition{
		"review": {Prompt: "{{index .Args 2}}", Arguments: snippets.ArgumentsRequired},
	})

	if got := self.handleCommand("//review only-one"); got != dispatch.Rejected {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.recordedEvents) != 0 {
		t.Errorf("snippet error entered conversation events: %+v", self.recordedEvents)
	}
	if !strings.HasPrefix(self.feedback.Message().Text, "//review: Could not render template:") {
		t.Errorf("got feedback %+v", self.feedback.Message())
	}
	if !strings.HasSuffix(self.feedback.Message().Text, " (alt+enter to send)") {
		t.Errorf("expected the way out to be offered, got %q", self.feedback.Message().Text)
	}
}

func TestUnknownSnippetShowsAnErrorAndKeepsTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.slash.commands = fixtureSnippetRegistry(t, nil)
	inputLine := edit.NewInput(nil)
	for _, value := range "//unknown" {
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(inputLine, nil)

	if got := inputLine.Text(); got != "//unknown" {
		t.Errorf("got input %q", got)
	}
	want := "Snippet not found: //unknown (alt+enter to send)"
	if len(self.recordedEvents) != 0 {
		t.Errorf("snippet error entered conversation events: %+v", self.recordedEvents)
	}
	if self.feedback.Message().Text != want || self.feedback.Message().Status != agent.ErrorStatus {
		t.Errorf("got feedback %+v, want failed %q", self.feedback.Message(), want)
	}
}

func TestTabCompletionKeepsCommandNamespacesSeparate(t *testing.T) {
	systemSet, err := slash.NewCommandSet(
		"/",
		slash.Command{Name: "conf", Run: slashTestHandler},
		slash.Command{Name: "copy", Run: slashTestHandler},
	)
	if err != nil {
		t.Fatal(err)
	}
	snippetSet, err := snippets.New(map[string]snippets.Definition{
		"test":   {Prompt: "Run tests.", Arguments: snippets.ArgumentsNone},
		"review": {Prompt: "Review changes.", Arguments: snippets.ArgumentsNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	self := slashCommandFixture(t, caps.Read)
	self.slash.commands = fixtureRegistry(t, systemSet, snippetSet)

	for input, want := range map[string]string{
		"/":  "/conf",
		"//": "//help",
	} {
		self.slash.completion.Reset()
		inputLine := edit.NewInput(nil)
		for _, value := range input {
			inputLine.Apply(key.Key{Code: key.Rune, Value: value}, false)
		}
		self.apply(inputLine, nil, key.Key{Code: key.Rune, Value: '\t'})
		if got := inputLine.Text(); got != want {
			t.Errorf("completion for %q got %q, want %q", input, got, want)
		}
	}
}

func TestTabCompletesAUniqueSlashCommand(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.slash.commands = fixtureCommandRegistry(
		t,
		slash.Command{Name: "conf", Run: slashTestHandler},
		slash.Command{Name: "copy", Run: slashTestHandler},
		slash.Command{Name: "open", Run: slashTestHandler},
	)
	inputLine := edit.NewInput(nil)
	for _, value := range "/op" {
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.apply(inputLine, nil, key.Key{Code: key.Rune, Value: '\t'})

	if got := inputLine.Text(); got != "/open" {
		t.Errorf("got completion %q", got)
	}
}

func slashTestHandler(slash.Context, slash.Arguments) error {
	return nil
}

func fixtureCommandRegistry(t *testing.T, commands ...slash.Command) slash.Registry {
	t.Helper()

	set, err := slash.NewCommandSet("/", commands...)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureRegistry(t, set)
}

func fixtureSnippetRegistry(t *testing.T, configured map[string]snippets.Definition) slash.Registry {
	t.Helper()
	return fixtureCommandRegistryWithSnippets(t, configured)
}

func fixtureCommandRegistryWithSnippets(
	t *testing.T,
	configured map[string]snippets.Definition,
	commands ...slash.Command,
) slash.Registry {
	t.Helper()

	systemSet, err := slash.NewCommandSet("/", commands...)
	if err != nil {
		t.Fatal(err)
	}
	snippetSet, err := snippets.New(configured)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureRegistry(t, systemSet, snippetSet)
}

func fixtureRegistry(t *testing.T, sets ...slash.CommandSet) slash.Registry {
	t.Helper()

	registry, err := slash.NewRegistry(sets...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func openTestWorkspace(t *testing.T, directory string) *work.Space {
	t.Helper()

	workspace := work.At(directory)
	if err := workspace.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })

	return workspace
}

func preparePathGrantCommands(t *testing.T, self *App, workspace *work.Space) *pathgrant.Grants {
	t.Helper()

	files := file.New(workspace.GetRoot(), caps.RefuseWrite(self.mode))
	pathAccess, err := shell.NewPathAccess(files, self.mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pathAccess.Close)

	grants := pathgrant.New(workspace, pathAccess)
	systemSet, err := commands.New(commands.Options{PathGrants: commands.PathGrants{
		Grant:      grants.Grant,
		Revoke:     grants.Revoke,
		GetCurrent: grants.GetCurrent,
	}})
	if err != nil {
		t.Fatal(err)
	}
	snippetSet, err := snippets.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	self.slash.commands = fixtureRegistry(t, systemSet, snippetSet)
	self.pathGrants = grants
	return grants
}

func slashCommandFixture(t *testing.T, currentCaps caps.Set) *App {
	t.Helper()

	log, err := store.Create(t.TempDir(), store.Meta{Model: "gpt"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	return &App{
		recorder: record.New(log),
		mode:     caps.NewMode(currentCaps),
	}
}

func TestConsecutiveTabsCycleCommandArguments(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.slash.commands = fixtureCommandRegistry(
		t,
		slash.Command{Name: "copy", Run: slashTestHandler}.WithArguments("session-name", "session-id", "session-dir"),
	)
	inputLine := edit.NewInput(nil)
	for _, value := range "/copy " {
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.apply(inputLine, nil, key.Key{Code: key.Rune, Value: '\t'})
	if got := inputLine.Text(); got != "/copy session-dir" {
		t.Errorf("got first completion %q", got)
	}

	self.apply(inputLine, nil, key.Key{Code: key.Rune, Value: '\t'})
	if got := inputLine.Text(); got != "/copy session-id" {
		t.Errorf("got second completion %q", got)
	}
}

func TestARequestedTransitionStopsTheApp(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	transition := cycle.Transition{Kind: cycle.NewSession, Arguments: []string{"-m", "codex/gpt@high"}}
	if err := self.requestTransition(transition); err != nil {
		t.Fatal(err)
	}
	if self.apply(edit.NewInput(nil), nil, key.Key{}) {
		t.Error("expected the app to stop")
	}
	if !slices.Equal(self.transition.Arguments, transition.Arguments) {
		t.Errorf("got transition %+v", self.transition)
	}
}

func TestSlashCommandCanAddSuccessFeedback(t *testing.T) {
	assertSlashCommandFeedback(t, func(context slash.Context) {
		context.Success("Copied to clipboard.")
	}, feedback.Message{Text: "Copied to clipboard.", Status: agent.SuccessStatus})
}

func assertSlashCommandFeedback(t *testing.T, run func(slash.Context), want feedback.Message) {
	t.Helper()

	fixtureCommands := fixtureCommandRegistry(t, slash.Command{
		Name: "fixture",
		Run: func(context slash.Context, _ slash.Arguments) error {
			run(context)
			return nil
		},
	})

	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.slash.commands = fixtureCommands
	if got := self.handleCommand("/fixture"); got != dispatch.Handled {
		t.Fatalf("got slash input result %d", got)
	}
	if len(self.recordedEvents) != 0 {
		t.Errorf("command feedback entered conversation events: %+v", self.recordedEvents)
	}
	if got := self.feedback.Message(); got != want {
		t.Errorf("got feedback %+v, want %+v", got, want)
	}
}

func TestUsageErrorIsNotPrefixedWithTheCommandName(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.slash.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "copy",
		Run: func(slash.Context, slash.Arguments) error {
			return slash.Usage()
		},
	}.WithArguments("session-name", "session-id", "session-dir"))

	if got := self.handleCommand("/copy"); got != dispatch.Rejected {
		t.Fatalf("got slash input result %d", got)
	}
	want := "Usage: /copy {session-dir|session-id|session-name} (alt+enter to send)"
	if len(self.recordedEvents) != 0 {
		t.Errorf("usage feedback entered conversation events: %+v", self.recordedEvents)
	}
	if self.feedback.Message().Text != want {
		t.Errorf("got feedback %+v, want %q", self.feedback.Message(), want)
	}
}

func TestARefusedCommandKeepsWhatWasTypedAndSaysWhy(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.slash.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "new",
		Run: func(slash.Context, slash.Arguments) error {
			return errors.New(`model "opus" is ambiguous`)
		},
	})

	inputLine := edit.NewInput(nil)
	for _, value := range "/new opus" {
		inputLine.Apply(key.Key{Code: key.Rune, Value: value}, false)
	}

	self.acceptInput(inputLine, edit.NewHistory("", historyLimit))

	if got := inputLine.Text(); got != "/new opus" {
		t.Errorf("expected the refused command to survive, got %q", got)
	}

	want := `/new: Model "opus" is ambiguous (alt+enter to send)`
	if len(self.recordedEvents) != 0 {
		t.Errorf("command feedback entered conversation events: %+v", self.recordedEvents)
	}
	if self.feedback.Message().Text != want {
		t.Errorf("got feedback %+v, want %q", self.feedback.Message(), want)
	}
}

const sessionGoldenSystemPrompt = "You are a test assistant."

type sessionGoldenResponse struct {
	Events               []string          `toml:"event"`
	Lines                []string          `toml:"line"`
	Body                 string            `toml:"body"`
	Headers              map[string]string `toml:"headers"`
	Status               int               `toml:"status"`
	Repeat               int               `toml:"repeat"`
	RefuseConnection     bool              `toml:"refuse-connection"`
	CancelAfterWireEvent int               `toml:"cancel-after-wire-event"`
	ResetAfterWireEvent  int               `toml:"reset-after-wire-event"`
	WaitForCancellation  bool              `toml:"wait-for-cancellation"`
}

type sessionGoldenTurn struct {
	Prompt                    string                  `toml:"prompt"`
	Responses                 []sessionGoldenResponse `toml:"response"`
	Timeout                   string                  `toml:"timeout"`
	IsCancelled               bool                    `toml:"is-cancelled"`
	CancelWithCtrlD           bool                    `toml:"cancel-with-ctrl-d"`
	CancelAfterReasoningDelta int                     `toml:"cancel-after-reasoning-delta"`
	CancelAfterReasoningEvent int                     `toml:"cancel-after-reasoning-event"`
	CancelAfterMessageDelta   int                     `toml:"cancel-after-message-delta"`
	CancelAfterToolRequest    int                     `toml:"cancel-after-tool-request"`
	CancelAfterRetryNotice    int                     `toml:"cancel-after-retry-notice"`
	ReplaceAfterToolRequest   string                  `toml:"replace-after-tool-request"`
	QueueAfterToolRequest     []string                `toml:"queue-after-tool-request"`
	QueueAfterEachToolRequest []string                `toml:"queue-after-each-tool-request"`
	QueueAfterMessageDelta    []string                `toml:"queue-after-message-delta"`
	FlushAfterToolRequest     bool                    `toml:"flush-after-tool-request"`
	CancelAfterQueueing       bool                    `toml:"cancel-after-queueing"`
	ToggleAfterMessageDelta   string                  `toml:"toggle-after-message-delta"`
	ToggleAfterToolRequest    string                  `toml:"toggle-after-tool-request"`
	ToggleDuringModeTurn      string                  `toml:"toggle-during-mode-turn"`
	CancelAfterToolToggle     bool                    `toml:"cancel-after-tool-toggle"`
	EndJobAfterToolRequest    string                  `toml:"end-job-after-tool-request"`
	EndJobAfterReasoningEvent string                  `toml:"end-job-after-reasoning-event"`
}

const exposeToolName = "expose"

const jobToolName = "job"

const notifyToolName = "notify"

const printedSessionIsImpossible = "this scenario drives the interface, which a printed session has none of\n"

func (self sessionGoldenScenario) usesTheInterface() bool {
	if slices.ContainsFunc(self.Tools, func(specification sessionGoldenTool) bool {
		return specification.Name == exposeToolName
	}) {
		return true
	}

	return self.ToggleBeforeFirst != "" || self.EndJobBeforeFirst != "" ||
		self.RunBeforeFirst != "" || self.JobHoldingWorkspace != "" ||
		self.FirstTurn.usesTheInterface()
}

func (self sessionGoldenTurn) usesTheInterface() bool {
	return self.ReplaceAfterToolRequest != "" ||
		self.ToggleAfterMessageDelta != "" ||
		self.ToggleAfterToolRequest != "" ||
		self.ToggleDuringModeTurn != "" ||
		self.EndJobAfterToolRequest != "" ||
		self.EndJobAfterReasoningEvent != "" ||
		self.FlushAfterToolRequest ||
		self.CancelAfterQueueing ||
		len(self.QueueAfterToolRequest) > 0 ||
		len(self.QueueAfterEachToolRequest) > 0 ||
		len(self.QueueAfterMessageDelta) > 0
}

type sessionGoldenTool struct {
	Name                  string   `toml:"name"`
	Outputs               []string `toml:"outputs"`
	Image                 string   `toml:"image"`
	ImageByteCount        int64    `toml:"image-bytes"`
	StateKey              string   `toml:"state-key"`
	ShellWithheld         bool     `toml:"shell-withheld"`
	ShouldWithholdNetwork bool     `toml:"network-withheld"`
	ShouldRefuseNetwork   bool     `toml:"network-refused"`
	LookupWithheld        bool     `toml:"lookup-withheld"`
	ShouldRefuseLookup    bool     `toml:"lookup-refused"`
	FetchWithheld         bool     `toml:"fetch-withheld"`
	LookupAnswer          string   `toml:"lookup-answer"`
	Blocks                bool     `toml:"blocks"`
	StoppedOutput         string   `toml:"stopped-output"`
	IsLargeRead           bool     `toml:"large-read"`
	Declared              string   `toml:"declared"`
}

type sessionGoldenScenario struct {
	Name                  string              `toml:"-"`
	Provider              string              `toml:"provider"`
	Model                 string              `toml:"model"`
	Effort                string              `toml:"effort"`
	IsFast                bool                `toml:"fast"`
	IdleAfter             string              `toml:"idle-after"`
	Asleep                string              `toml:"asleep"`
	Grouping              output.Grouping     `toml:"grouping"`
	Hostname              string              `toml:"hostname"`
	FirstTokenError       string              `toml:"first-token-error"`
	CredentialRefresh     string              `toml:"credential-refresh"`
	ToggleBeforeFirst     string              `toml:"toggle-before-first"`
	Conditions            string              `toml:"conditions"`
	ConditionsOnResume    string              `toml:"conditions-on-resume"`
	EndJobBeforeFirst     string              `toml:"end-job-before-first"`
	JobHoldingWorkspace   string              `toml:"job-holding-workspace"`
	JobsRunningIntoResume []string            `toml:"jobs-running-into-resume"`
	RunBeforeFirst        string              `toml:"run-before-first"`
	Tools                 []sessionGoldenTool `toml:"tool"`
	FirstTurn             sessionGoldenTurn   `toml:"first"`
	ResumeTurn            sessionGoldenTurn   `toml:"resume"`
	CredentialsPath       string              `toml:"-"`
	CredentialRecovery    func()              `toml:"-"`
	ScratchDirectory      string              `toml:"-"`
}

func TestGoldenScenariosProduceCanonicalOutputs(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "scenarios", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no session golden scenarios")
	}

	for _, path := range paths {
		scenario := readSessionGoldenScenario(t, path)
		t.Run(scenario.Name, func(t *testing.T) {
			for extension, got := range runSessionGoldenScenario(t, scenario) {
				compareScenarioGolden(t, scenario.Name+extension, got)
			}
		})
	}
}

func readSessionGoldenScenario(t *testing.T, path string) sessionGoldenScenario {
	t.Helper()

	var scenario sessionGoldenScenario
	metadata, err := toml.DecodeFile(path, &scenario)
	if err != nil {
		t.Fatal(err)
	}
	if undecodedKeys := metadata.Undecoded(); len(undecodedKeys) > 0 {
		t.Fatalf("%s: no such setting: %s", path, undecodedKeys[0])
	}

	scenario.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return scenario
}

func (self sessionGoldenScenario) screen(writer io.Writer) *output.Screen {
	screen := output.NewTerminalOfSize(writer, replayColumns, replayLines)
	screen.SetGrouping(self.Grouping)
	if self.ScratchDirectory != "" {
		screen.LinkPathsUnder(link.Roots{Scratch: self.ScratchDirectory})
	}

	return screen
}

func compareScenarioGolden(t *testing.T, name string, got string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", "output", name)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o700); err != nil {
			t.Fatal(err)
		}
		//nolint:gosec // the path is built from the fixed testdata directory
		if err := os.WriteFile(goldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, got, want)
	}
}

type failingAnthropicTokenSource struct {
	failure error
}

func (self failingAnthropicTokenSource) Token() (string, error) {
	return "", self.failure
}

type sessionGoldenAnthropicTokenSource struct {
	source          anthropic.TokenSource
	credentialsPath string
}

func (self sessionGoldenAnthropicTokenSource) Token() (string, error) {
	token, err := self.source.Token()
	return token, canonicalSessionGoldenCredentialError(err, self.credentialsPath)
}

type sessionGoldenCodexTokenSource struct {
	source          codex.TokenSource
	credentialsPath string
}

func (self sessionGoldenCodexTokenSource) Token() (codex.Token, error) {
	token, err := self.source.Token()
	return token, canonicalSessionGoldenCredentialError(err, self.credentialsPath)
}

func canonicalSessionGoldenCredentialError(err error, credentialsPath string) error {
	if err == nil {
		return nil
	}
	pendingCredentialsPattern := regexp.MustCompile(regexp.QuoteMeta(credentialsPath) + `\.[0-9]+`)
	message := pendingCredentialsPattern.ReplaceAllString(err.Error(), "auth.json.pending")
	message = strings.ReplaceAll(message, credentialsPath, "auth.json")
	if message == err.Error() {
		return err
	}
	return errors.New(message)
}

func newSessionGoldenAnthropicTokenSource(tokenError string, credentialsPath string) anthropic.TokenSource {
	if tokenError != "" {
		return failingAnthropicTokenSource{failure: errors.New(tokenError)}
	}
	if credentialsPath != "" {
		return sessionGoldenAnthropicTokenSource{
			source:          anthropic.StoredCredentialsAt(credentialsPath),
			credentialsPath: credentialsPath,
		}
	}
	return anthropic.Static("test-token")
}

func newSessionGoldenCodexTokenSource(credentialsPath string) codex.TokenSource {
	if credentialsPath != "" {
		return sessionGoldenCodexTokenSource{
			source:          codex.StoredCredentialsAt(credentialsPath),
			credentialsPath: credentialsPath,
		}
	}
	return codex.Static("test-token", "test-account")
}

func sessionGoldenProviderFor(
	t *testing.T,
	scenario sessionGoldenScenario,
	endpoint string,
	tokenError string,
	sessionName string,
) agent.Provider {
	t.Helper()

	provider := newSessionGoldenProvider(t, scenario, endpoint, tokenError)
	if scoped, isScoped := provider.(interface{ UseSession(name string) }); isScoped {
		scoped.UseSession(sessionName)
	}
	if scenario.IdleAfter != "" {
		after, err := time.ParseDuration(scenario.IdleAfter)
		if err != nil {
			t.Fatal(err)
		}
		quietable, canIdle := provider.(interface{ IdleAfter(after time.Duration) })
		if !canIdle {
			t.Fatalf("provider %q cannot be given an idle bound", scenario.Provider)
		}
		quietable.IdleAfter(after)
	}

	return provider
}

func newSessionGoldenClock(t *testing.T, scenario sessionGoldenScenario) func(*agent.Agent) {
	t.Helper()

	if scenario.Asleep == "" {
		return func(*agent.Agent) {}
	}

	sleepBetweenRequests, err := time.ParseDuration(scenario.Asleep)
	if err != nil {
		t.Fatal(err)
	}

	var mutex sync.Mutex
	var sleepSoFar time.Duration

	clock := func() time.Time {
		mutex.Lock()
		defer mutex.Unlock()

		at := time.Now().Add(sleepSoFar)
		sleepSoFar += sleepBetweenRequests

		return at
	}

	return func(assistant *agent.Agent) {
		assistant.TakeTimeFrom(clock)
	}
}

func newSessionGoldenProvider(
	t *testing.T,
	scenario sessionGoldenScenario,
	endpoint string,
	tokenError string,
) agent.Provider {
	t.Helper()

	switch scenario.Provider {
	case "anthropic":
		tokens := newSessionGoldenAnthropicTokenSource(tokenError, scenario.CredentialsPath)
		client, err := anthropic.New(tokens, scenario.Model, scenario.Effort, 128_000)
		if err != nil {
			t.Fatal(err)
		}
		client.URL = endpoint
		return client
	case "codex":
		client, err := codex.New(newSessionGoldenCodexTokenSource(scenario.CredentialsPath), scenario.Model, scenario.Effort)
		if err != nil {
			t.Fatal(err)
		}
		client.URL = endpoint
		client.IsFast = scenario.IsFast
		return client
	case "opencode-go":
		client, err := opencodego.New(endpoint, "test-token", scenario.Model, scenario.Effort, 128_000)
		if err != nil {
			t.Fatal(err)
		}
		return client
	case "chat":
		client, err := chatcompletions.New(
			endpoint,
			http.Header{"Authorization": {"Bearer test-token"}},
			scenario.Model,
			scenario.Effort,
			128_000,
		)
		if err != nil {
			t.Fatal(err)
		}
		return client
	case "ollama":
		client, err := ollama.New(endpoint, scenario.Model, scenario.Effort, 32_768)
		if err != nil {
			t.Fatal(err)
		}
		return client
	default:
		t.Fatalf("unknown provider %q", scenario.Provider)
		return nil
	}
}

const goldenSessionName = "tame-impala"

func noPortRoutes() []portgrant.Route {
	return nil
}

func portRoutes(ports ...uint16) []portgrant.Route {
	routes := make([]portgrant.Route, 0, len(ports))
	for _, port := range ports {
		routes = append(routes, portgrant.Route{Port: port})
	}

	return routes
}

func goldenAssociatedPort(state jobs.State) exposedPorts.Routes {
	return exposedPorts.Routes{
		GetRoutes: func() []portgrant.Route {
			return []portgrant.Route{{Port: 8000, JobName: "docs"}}
		},
		GetJobs: func() []jobs.Snapshot {
			return []jobs.Snapshot{{Name: "docs", State: state}}
		},
		Hostname: portgrant.AddressFor(goldenSessionName),
	}
}

func goldenAssociatedPortWithoutJob() exposedPorts.Routes {
	return exposedPorts.Routes{
		GetRoutes: func() []portgrant.Route {
			return []portgrant.Route{{Port: 8000, JobName: "docs"}}
		},
		Hostname: portgrant.AddressFor(goldenSessionName),
	}
}

func goldenExposedPorts(ports ...uint16) exposedPorts.Routes {
	return exposedPorts.Routes{
		GetRoutes: func() []portgrant.Route { return portRoutes(ports...) },
		Hostname:  portgrant.AddressFor(goldenSessionName),
	}
}

func goldenLocalPorts(ports ...uint16) exposedPorts.Routes {
	return exposedPorts.Routes{
		GetRoutes: func() []portgrant.Route { return portRoutes(ports...) },
		Hostname:  portgrant.LocalHost,
	}
}

func newSessionGoldenPorts(sessionName string, hostnameTemplate string) *portgrant.HostToSandbox {
	address := portgrant.AddressFor(sessionName)
	hostname := config.Ports{Hostname: hostnameTemplate}.GetHostname(sessionName, address)
	return portgrant.NewHostToSandbox(portgrant.HostToSandboxExposer{
		Expose: func(uint16) error { return nil },
		Hide:   func(uint16) error { return nil },
	}, hostname)
}

func newSessionGoldenTools(
	t *testing.T,
	specifications []sessionGoldenTool,
	ports *portgrant.HostToSandbox,
	scratchDirectory string,
) []tool.Tool {
	t.Helper()

	tools := make([]tool.Tool, 0, len(specifications))
	for _, specification := range specifications {
		if specification.Name == jobToolName {
			tools = append(tools, job.New(nil, nil, nil, true))
			continue
		}

		if specification.Name == exposeToolName {
			tools = append(tools, expose.New(ports.ForModel()))
			continue
		}

		if specification.IsLargeRead {
			tools = append(tools, newSessionGoldenLargeReadTool(t, scratchDirectory))
			continue
		}

		if specification.ShellWithheld {
			tools = append(tools, newSessionGoldenShell(t, caps.Read, false))
			continue
		}

		if specification.ShouldWithholdNetwork {
			tools = append(tools, newSessionGoldenShell(t, caps.Read|caps.Shell, false))
			continue
		}

		if specification.ShouldRefuseNetwork {
			tools = append(tools, newSessionGoldenRefusingShell(t))
			continue
		}

		if specification.Name == title.Name {
			tools = append(tools, title.New())
			continue
		}

		if specification.Name == notifyToolName {
			tools = append(tools, notify.New(func(string) bool { return true }, func() bool { return false }))
			continue
		}

		if specification.ShouldRefuseLookup {
			broker := newRefusingAskBroker(t)
			tools = append(tools, lookup.New(
				func() bool { return true },
				func(ctx context.Context, query string) error {
					return lookupApproval.ask(ctx, broker, permission.Ask, query)
				},
				sessionGoldenSearcher{answer: specification.LookupAnswer},
			))
			continue
		}

		if specification.LookupWithheld || specification.FetchWithheld || specification.LookupAnswer != "" {
			searcher := sessionGoldenSearcher{answer: specification.LookupAnswer}
			tools = append(tools,
				lookup.New(func() bool { return !specification.LookupWithheld }, allowApproval, searcher),
				fetch.New(func() bool { return !specification.FetchWithheld }, allowApproval, saveTestHTML),
			)
			continue
		}

		if specification.Declared != "" {
			tools = append(tools, newSessionGoldenDeclaredTool(t, specification))
			continue
		}

		if specification.Blocks {
			tools = append(tools, newSessionGoldenBlockingTool(specification))
			continue
		}

		callCount := 0
		builder := tool.Implement(
			tool.Definition{Name: specification.Name, Description: "A deterministic scenario tool."},
			func(struct{}) (string, string) { return specification.Name, "" },
		)
		if specification.StateKey != "" {
			builder = builder.State(specification.StateKey, func(state json.RawMessage) error {
				return json.Unmarshal(state, &callCount)
			})
		}
		attachment, attachmentMetrics := sessionGoldenImage(t, specification.Image, specification.ImageByteCount)
		tools = append(tools, builder.Run(func(context.Context, struct{}) (tool.ToolCallResult, error) {
			if callCount >= len(specification.Outputs) {
				return tool.ToolCallResult{}, fmt.Errorf("tool %s has no output for call %d", specification.Name, callCount+1)
			}

			output := specification.Outputs[callCount]
			callCount++
			state, err := json.Marshal(callCount)
			if err != nil {
				return tool.ToolCallResult{}, err
			}
			return tool.ToolCallResult{Output: output, Image: attachment, Metrics: attachmentMetrics, State: state}, nil
		}))
	}
	return tools
}

func newSessionGoldenLargeReadTool(t *testing.T, scratchDirectory string) tool.Tool {
	t.Helper()

	rootHandle, err := os.OpenRoot(scratchDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rootHandle.Close() })

	const name = "io/cmd/oh/output/region.go"
	if err := os.MkdirAll(filepath.Join(scratchDirectory, filepath.Dir(name)), 0o700); err != nil {
		t.Fatal(err)
	}
	openedFile, err := rootHandle.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	content := "before\nselected one\nselected two\n" + strings.Repeat("padding\n", 64)
	if _, err := openedFile.WriteString(content); err != nil {
		_ = openedFile.Close()
		t.Fatal(err)
	}
	if err := openedFile.Truncate(20*1024*1024 + 1); err != nil {
		_ = openedFile.Close()
		t.Fatal(err)
	}
	if err := openedFile.Close(); err != nil {
		t.Fatal(err)
	}

	root := file.New(rootHandle, func(string) error { return nil })
	root.Mount(sandbox.TmpDir, root)
	return read.New(root, file.NewSnapshots())
}

func sessionGoldenImage(t *testing.T, size string, byteCount int64) (tool.Image, tool.ToolCallMetrics) {
	t.Helper()

	if size == "" {
		return tool.Image{}, tool.ToolCallMetrics{}
	}
	if byteCount <= 0 {
		t.Fatalf("scenario image %q has no byte count", size)
	}

	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil {
		t.Fatalf("unreadable scenario image size %q: %v", size, err)
	}

	subject := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(subject, subject.Bounds(), image.NewUniform(color.NRGBA{R: 40, G: 80, B: 120, A: 255}), image.Point{}, draw.Src)

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, subject); err != nil {
		t.Fatalf("could not encode the scenario image: %v", err)
	}

	imageMetrics := tool.ToolCallMetrics{
		Kind:            tool.MetricImage,
		Bytes:           byteCount,
		EstimatedTokens: util.EstimateImageTokenCount(imageutil.Fit(width, height)),
	}

	return tool.Image{MediaType: "image/png", Data: encoded.Bytes()}, imageMetrics
}

var errSessionGoldenToolStopped = errors.New("the tool was stopped")

func newSessionGoldenDeclaredTool(t *testing.T, specification sessionGoldenTool) tool.Tool {
	t.Helper()

	script := filepath.Join(t.TempDir(), "declared")
	//nolint:gosec // a tool the scenario declares has to be runnable
	if err := os.WriteFile(script, []byte("#!/bin/bash\n"+specification.Declared+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	declared, err := command.New(command.Declaration{
		Name:        specification.Name,
		Description: "A deterministic scenario tool of the user's own.",
		Command:     []string{script},
		Subject:     "dish",
		Parameters: []command.Parameter{
			{
				Name:        "dish",
				Kind:        command.KindString,
				Description: "what the dish is called",
			},
			{
				Name:        "fact",
				Kind:        command.KindEnum,
				Values:      []string{"uptime", "kernel"},
				Description: "which fact to report",
				IsOptional:  true,
			},
			{
				Name:        "verbose",
				Kind:        command.KindBoolean,
				Description: "whether to say more about it",
				IsOptional:  true,
			},
		},
	}, command.Options{})
	if err != nil {
		t.Fatal(err)
	}

	return declared
}

func newSessionGoldenBlockingTool(specification sessionGoldenTool) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: specification.Name, Description: "A scenario tool that blocks."},
		func(struct{}) (string, string) { return specification.Name, "" },
	).Run(func(ctx context.Context, _ struct{}) (tool.ToolCallResult, error) {
		<-ctx.Done()
		return tool.ToolCallResult{Output: specification.StoppedOutput}, errSessionGoldenToolStopped
	})
}

type sessionGoldenSearcher struct {
	answer string
}

func (self sessionGoldenSearcher) Search(context.Context, string) (string, error) {
	return self.answer, nil
}

func newSessionGoldenShell(t *testing.T, grantedCaps caps.Set, isYolo bool) tool.Tool {
	t.Helper()

	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceRoot.Close() })

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(grantedCaps)
	pathAccess, err := shell.NewPathAccess(files, mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pathAccess.Close)

	return shell.New(
		workspace, t.TempDir(), t.TempDir(), pathAccess, mode, files, isYolo,
		func(context.Context, string) error { return nil }, sandbox.Direct(),
	)
}

func allowApproval(context.Context, string) error { return nil }

func saveTestHTML([]byte) (string, error) {
	return "/state/sessions/tame-impala/drops/fetch-test.html", nil
}

func newRefusingAskBroker(t *testing.T) *ask.Broker {
	t.Helper()

	broker := ask.New()
	t.Cleanup(broker.Open())

	go func() {
		for range broker.Changes() {
			if request := broker.Current(); request != nil {
				request.Choose(1)
			}
		}
	}()

	return broker
}

func newSessionGoldenRefusingShell(t *testing.T) tool.Tool {
	t.Helper()

	broker := newRefusingAskBroker(t)

	workspace := t.TempDir()
	workspaceRoot, err := os.OpenRoot(workspace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaceRoot.Close() })

	files := file.New(workspaceRoot, func(string) error { return file.ErrReadOnly })
	mode := caps.NewMode(caps.Read | caps.Shell | caps.Network)
	pathAccess, err := shell.NewPathAccess(files, mode, shell.Paths{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pathAccess.Close)

	return shell.New(
		workspace, t.TempDir(), t.TempDir(), pathAccess, mode, files, false,
		func(ctx context.Context, command string) error {
			return approveHostNetwork(ctx, broker, permission.Ask, command)
		},
		sandbox.Direct(),
	)
}

func serveSessionGoldenResponse(
	writer http.ResponseWriter,
	request *http.Request,
	response sessionGoldenResponse,
	cancelSignals chan<- struct{},
) {
	if response.RefuseConnection {
		resetSessionGoldenConnection(writer)
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	for name, value := range response.Headers {
		writer.Header().Set(name, value)
	}
	if response.Status != 0 && response.Status != http.StatusOK {
		writer.WriteHeader(response.Status)
		_, _ = fmt.Fprint(writer, response.Body)
		return
	}

	flusher, canFlush := writer.(http.Flusher)
	if !canFlush {
		panic("httptest response cannot flush")
	}

	for _, line := range response.Lines {
		_, _ = fmt.Fprintln(writer, line)
		flusher.Flush()
	}
	for i, payload := range response.Events {
		_, _ = fmt.Fprintf(writer, "data: %s\n\n", payload)
		flusher.Flush()
		switch i + 1 {
		case response.CancelAfterWireEvent:
			cancelSignals <- struct{}{}
			<-request.Context().Done()
			return
		case response.ResetAfterWireEvent:
			resetSessionGoldenConnection(writer)
			return
		}
	}
	if response.WaitForCancellation {
		<-request.Context().Done()
	}
}

func resetSessionGoldenConnection(writer http.ResponseWriter) {
	hijacker, canHijack := writer.(http.Hijacker)
	if !canHijack {
		panic("httptest response cannot be hijacked")
	}
	connection, _, err := hijacker.Hijack()
	if err != nil {
		panic(err)
	}
	_ = connection.Close()
}

func expandSessionGoldenResponses(responses []sessionGoldenResponse) []sessionGoldenResponse {
	expanded := make([]sessionGoldenResponse, 0, len(responses))

	for _, response := range responses {
		expanded = append(expanded, slices.Repeat([]sessionGoldenResponse{response}, max(response.Repeat, 1))...)
	}

	return expanded
}

const (
	sessionGoldenCredentialRefreshSuccess         = "success"
	sessionGoldenCredentialRefreshRotation        = "rotation"
	sessionGoldenCredentialRefreshRejection       = "rejection"
	sessionGoldenCredentialRefreshFailure         = "failure"
	sessionGoldenCredentialRefreshMissingToken    = "missing-token"
	sessionGoldenCredentialRefreshMissingProvider = "missing-provider"
	sessionGoldenCredentialRefreshStoreFailure    = "store-failure"
)

func newSessionGoldenStoredCredentials() *auth.Credentials {
	return &auth.Credentials{
		Version: auth.Version,
		Anthropic: &auth.AnthropicCredentials{
			Access:    "old-anthropic-access",
			Refresh:   "old-anthropic-refresh",
			ExpiresAt: time.Now().Add(-time.Minute).UnixMilli(),
		},
		Codex: &auth.CodexCredentials{
			Access:    "old-codex-access",
			Refresh:   "old-codex-refresh",
			ExpiresAt: time.Now().Add(-time.Minute).UnixMilli(),
			AccountID: "old-codex-account",
		},
		OpenCodeGo: &auth.OpenCodeGoCredentials{APIKey: "old-key"},
	}
}

func newSessionGoldenRotatedCredentials() *auth.Credentials {
	return &auth.Credentials{
		Version: auth.Version,
		Anthropic: &auth.AnthropicCredentials{
			Access:    "rotated-anthropic-access",
			Refresh:   "rotated-anthropic-refresh",
			ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
		},
		Codex: &auth.CodexCredentials{
			Access:    "rotated-codex-access",
			Refresh:   "rotated-codex-refresh",
			ExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
			AccountID: "rotated-codex-account",
		},
		OpenCodeGo: &auth.OpenCodeGoCredentials{APIKey: "rotated-key"},
	}
}

func serveSessionGoldenSuccessfulCredentialRefresh(writer http.ResponseWriter, provider string) {
	switch provider {
	case model.AnthropicProvider:
		_, _ = fmt.Fprint(writer, `{"access_token":"refreshed-anthropic-access","expires_in":3600}`)
	case model.CodexProvider:
		_, _ = fmt.Fprint(writer, `{"access_token":"not.a.jwt","expires_in":3600}`)
	}
}

func serveSessionGoldenCredentialRefresh(
	t *testing.T,
	writer http.ResponseWriter,
	scenario sessionGoldenScenario,
	credentialsPath string,
	isRecovery bool,
) {
	t.Helper()

	writer.Header().Set("Content-Type", "application/json")
	switch scenario.CredentialRefresh {
	case sessionGoldenCredentialRefreshSuccess:
		serveSessionGoldenSuccessfulCredentialRefresh(writer, scenario.Provider)
	case sessionGoldenCredentialRefreshRotation:
		if err := auth.Save(credentialsPath, newSessionGoldenRotatedCredentials()); err != nil {
			t.Errorf("rotate credentials: %v", err)
		}
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(writer, `{"error":"invalid_grant"}`)
	case sessionGoldenCredentialRefreshRejection:
		if isRecovery {
			serveSessionGoldenSuccessfulCredentialRefresh(writer, scenario.Provider)
			return
		}
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(writer, `{"error":"invalid_grant"}`)
	case sessionGoldenCredentialRefreshFailure:
		if isRecovery {
			serveSessionGoldenSuccessfulCredentialRefresh(writer, scenario.Provider)
			return
		}
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(writer, `{"error":"server_error"}`)
	case sessionGoldenCredentialRefreshStoreFailure:
		if !isRecovery {
			if err := syscall.Chmod(filepath.Dir(credentialsPath), 0o500); err != nil {
				t.Errorf("make credentials read-only: %v", err)
			}
		}
		serveSessionGoldenSuccessfulCredentialRefresh(writer, scenario.Provider)
	}
}

func checkSessionGoldenStoredCredentials(t *testing.T, scenario sessionGoldenScenario, credentialsPath string) {
	t.Helper()

	storedCredentials, err := auth.Load(credentialsPath)
	if err != nil {
		t.Error(err)
		return
	}

	anthropicAccess := "old-anthropic-access"
	anthropicRefresh := "old-anthropic-refresh"
	codexAccess := "old-codex-access"
	codexRefresh := "old-codex-refresh"
	codexAccount := "old-codex-account"
	openCodeGoKey := "old-key"
	hasRotatedCredentials := scenario.CredentialRefresh == sessionGoldenCredentialRefreshRotation ||
		scenario.CredentialRefresh == sessionGoldenCredentialRefreshMissingToken ||
		scenario.CredentialRefresh == sessionGoldenCredentialRefreshMissingProvider
	if hasRotatedCredentials {
		anthropicAccess = "rotated-anthropic-access"
		anthropicRefresh = "rotated-anthropic-refresh"
		codexAccess = "rotated-codex-access"
		codexRefresh = "rotated-codex-refresh"
		codexAccount = "rotated-codex-account"
		openCodeGoKey = "rotated-key"
	} else {
		switch scenario.Provider {
		case model.AnthropicProvider:
			anthropicAccess = "refreshed-anthropic-access"
		case model.CodexProvider:
			codexAccess = "not.a.jwt"
		}
	}

	if storedCredentials.Anthropic == nil || storedCredentials.Anthropic.Access != anthropicAccess || storedCredentials.Anthropic.Refresh != anthropicRefresh {
		t.Errorf("got Anthropic credentials %+v, want access %q and refresh %q", storedCredentials.Anthropic, anthropicAccess, anthropicRefresh)
	}
	if storedCredentials.Codex == nil || storedCredentials.Codex.Access != codexAccess || storedCredentials.Codex.Refresh != codexRefresh || storedCredentials.Codex.AccountID != codexAccount {
		t.Errorf("got Codex credentials %+v, want access %q, refresh %q, and account %q", storedCredentials.Codex, codexAccess, codexRefresh, codexAccount)
	}
	if storedCredentials.OpenCodeGo == nil || storedCredentials.OpenCodeGo.APIKey != openCodeGoKey {
		t.Errorf("got OpenCode Go credentials %+v, want key %q", storedCredentials.OpenCodeGo, openCodeGoKey)
	}
}

func prepareSessionGoldenCredentials(t *testing.T, scenario *sessionGoldenScenario) {
	t.Helper()

	if scenario.CredentialRefresh == "" {
		return
	}
	switch scenario.CredentialRefresh {
	case sessionGoldenCredentialRefreshSuccess,
		sessionGoldenCredentialRefreshRotation,
		sessionGoldenCredentialRefreshRejection,
		sessionGoldenCredentialRefreshFailure,
		sessionGoldenCredentialRefreshStoreFailure,
		sessionGoldenCredentialRefreshMissingToken,
		sessionGoldenCredentialRefreshMissingProvider:
	default:
		t.Fatalf("unknown credential refresh %q", scenario.CredentialRefresh)
	}
	if scenario.Provider != model.AnthropicProvider && scenario.Provider != model.CodexProvider {
		t.Fatalf("credential refresh is not supported for provider %q", scenario.Provider)
	}

	credentialsPath := filepath.Join(t.TempDir(), "auth.json")
	storedCredentials := newSessionGoldenStoredCredentials()
	if scenario.CredentialRefresh == sessionGoldenCredentialRefreshMissingToken {
		switch scenario.Provider {
		case model.AnthropicProvider:
			storedCredentials.Anthropic.Refresh = ""
		case model.CodexProvider:
			storedCredentials.Codex.Refresh = ""
		}
	}
	if scenario.CredentialRefresh == sessionGoldenCredentialRefreshMissingProvider {
		switch scenario.Provider {
		case model.AnthropicProvider:
			storedCredentials.Anthropic = nil
		case model.CodexProvider:
			storedCredentials.Codex = nil
		}
	}
	if err := auth.Save(credentialsPath, storedCredentials); err != nil {
		t.Fatal(err)
	}

	var refreshRequests atomic.Int32
	var requestsBeforeRecovery atomic.Int32
	var isRecovery atomic.Bool
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		refreshRequests.Add(1)
		serveSessionGoldenCredentialRefresh(t, writer, *scenario, credentialsPath, isRecovery.Load())
	}))
	t.Cleanup(tokenServer.Close)

	switch scenario.Provider {
	case model.AnthropicProvider:
		anthropic.TokenURL = tokenServer.URL
		t.Cleanup(func() { anthropic.TokenURL = "" })
	case model.CodexProvider:
		codex.TokenURL = tokenServer.URL
		t.Cleanup(func() { codex.TokenURL = "" })
	}
	scenario.CredentialsPath = credentialsPath
	switch scenario.CredentialRefresh {
	case sessionGoldenCredentialRefreshRejection, sessionGoldenCredentialRefreshFailure:
		scenario.CredentialRecovery = func() {
			requestsBeforeRecovery.Store(refreshRequests.Load())
			isRecovery.Store(true)
		}
	case sessionGoldenCredentialRefreshStoreFailure:
		scenario.CredentialRecovery = func() {
			requestsBeforeRecovery.Store(refreshRequests.Load())
			if err := syscall.Chmod(filepath.Dir(credentialsPath), 0o700); err != nil {
				t.Fatal(err)
			}
			isRecovery.Store(true)
		}
	case sessionGoldenCredentialRefreshMissingToken, sessionGoldenCredentialRefreshMissingProvider:
		scenario.CredentialRecovery = func() {
			if err := auth.Save(credentialsPath, newSessionGoldenRotatedCredentials()); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Cleanup(func() {
		switch scenario.CredentialRefresh {
		case sessionGoldenCredentialRefreshSuccess, sessionGoldenCredentialRefreshRotation:
			if refreshRequests.Load() != 1 {
				t.Errorf("got %d refresh requests, want one", refreshRequests.Load())
			}
		case sessionGoldenCredentialRefreshRejection, sessionGoldenCredentialRefreshFailure, sessionGoldenCredentialRefreshStoreFailure:
			if requestsBeforeRecovery.Load() == 0 || refreshRequests.Load() != requestsBeforeRecovery.Load()+1 {
				t.Errorf("got %d refresh requests before recovery and %d in total, want failures followed by one recovery", requestsBeforeRecovery.Load(), refreshRequests.Load())
			}
		case sessionGoldenCredentialRefreshMissingToken, sessionGoldenCredentialRefreshMissingProvider:
			if refreshRequests.Load() != 0 {
				t.Errorf("got %d refresh requests, want none", refreshRequests.Load())
			}
		}
		checkSessionGoldenStoredCredentials(t, *scenario, credentialsPath)
	})
}

func runSessionGoldenScenario(t *testing.T, scenario sessionGoldenScenario) map[string]string {
	t.Helper()

	if slices.ContainsFunc(scenario.Tools, func(specification sessionGoldenTool) bool {
		return specification.IsLargeRead
	}) {
		scenario.ScratchDirectory = t.TempDir()
	}
	prepareSessionGoldenCredentials(t, &scenario)

	responses := expandSessionGoldenResponses(
		append(slices.Clone(scenario.FirstTurn.Responses), scenario.ResumeTurn.Responses...),
	)
	cancelSignals := make(chan struct{}, len(responses))
	var requestCount atomic.Int32
	var requestMutex sync.Mutex
	var requestBodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestBody, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		requestMutex.Lock()
		requestBodies = append(requestBodies, requestBody)
		requestMutex.Unlock()

		requestIndex := int(requestCount.Add(1) - 1)
		if requestIndex >= len(responses) {
			http.Error(writer, "scenario has no response for this request", http.StatusConflict)
			return
		}
		serveSessionGoldenResponse(writer, request, responses[requestIndex], cancelSignals)
	}))
	defer server.Close()

	directory := t.TempDir()
	firstConditions := sessionGoldenConditions(t, scenario.Conditions)
	meta := store.Meta{
		Model:        scenario.Model,
		Provider:     scenario.Provider,
		Effort:       scenario.Effort,
		IsFast:       scenario.IsFast,
		SystemPrompt: sessionGoldenSystemPrompt,
	}
	if scenario.describesConditions() {
		meta.Conditions = &firstConditions
	}
	log, err := store.Create(directory, meta)
	if err != nil {
		t.Fatal(err)
	}
	goldenPorts := newSessionGoldenPorts(goldenSessionName, scenario.Hostname)
	firstAssistant := agent.New(
		sessionGoldenSystemPrompt,
		sessionGoldenProviderFor(
			t, scenario, server.URL, scenario.FirstTokenError, log.Name(),
		),
		newSessionGoldenTools(t, scenario.Tools, goldenPorts, scenario.ScratchDirectory),
	)
	firstAssistant.TakeRetryWaitsAtOnce()
	settleClock := newSessionGoldenClock(t, scenario)
	settleClock(firstAssistant)
	var firstScreenOutput bytes.Buffer
	firstHarness := &App{
		agent:         firstAssistant,
		screen:        scenario.screen(&firstScreenOutput),
		recorder:      record.New(log),
		hostToSandbox: goldenPorts,
		display:       displayState{modelName: scenario.Model},
	}
	if scenario.Provider == model.CodexProvider {
		firstHarness.openingEvents = []agent.Event{model.FastModeEvent(scenario.IsFast)}
	}
	firstHarness.conditions = conditions.NewRestored(firstConditions, firstConditions)
	settleSessionGoldenMode(firstHarness)
	if scenario.JobHoldingWorkspace != "" {
		startSessionGoldenJobHoldingTheWorkspace(t, firstHarness, scenario.JobHoldingWorkspace)
	}
	if len(scenario.JobsRunningIntoResume) > 0 {
		recordSessionGoldenRunningJobs(firstHarness, scenario.JobsRunningIntoResume)
	}
	if scenario.ToggleBeforeFirst != "" {
		toggleSessionGoldenCaps(t, firstHarness, scenario.ToggleBeforeFirst)
		firstHarness.settleAccess()
		firstAssistant.AddUserMessage(firstHarness.takeSettledNotes())
	}
	if scenario.JobHoldingWorkspace != "" {
		settleSessionGoldenJob(t, firstHarness, scenario.JobHoldingWorkspace)
	}
	if scenario.EndJobBeforeFirst != "" {
		firstHarness.jobEnded(endedSessionGoldenJob(scenario.EndJobBeforeFirst))
		firstHarness.settleAccess()
		firstAssistant.AddUserMessage(firstHarness.takeSettledNotes())
	}
	if scenario.RunBeforeFirst != "" {
		firstHarness.holdNotice(sessionGoldenHostCommand(scenario.RunBeforeFirst))
		firstHarness.settleAccess()
		firstAssistant.AddUserMessage(firstHarness.takeSettledNotes())
	}
	firstHarness.currentTurn = Turn{Stream: testRunningTurnStream(), painter: firstHarness.newPainter(true)}
	firstTurns := runSessionGoldenTurn(t, firstHarness, scenario.FirstTurn, cancelSignals)
	firstHarness.dropPendingInput()

	if !scenario.usesTheInterface() {
		printedOutput := drawPrintedSessionGoldenTurn(
			t, directory, scenario, firstHarness, nil, firstTurns,
		)
		requireNothingWasDrawnOver(t, printedOutput)
		requireSameVisibleScreen(
			t,
			"printed session differs from what the interface drew",
			firstScreenOutput.String(),
			printedOutput,
		)
	}
	if scenario.CredentialRecovery != nil {
		scenario.CredentialRecovery()
	}

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	storedSession, err := store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	resumedAssistant := agent.New(
		storedSession.Meta.SystemPrompt,
		sessionGoldenProviderFor(t, scenario, server.URL, "", sessionName),
		newSessionGoldenTools(t, scenario.Tools, goldenPorts, scenario.ScratchDirectory),
	)
	resumedAssistant.TakeRetryWaitsAtOnce()
	settleClock(resumedAssistant)
	if err := resumedAssistant.RestoreState(storedSession.Events); err != nil {
		t.Fatal(err)
	}
	if err := resumedAssistant.Load(storedSession.Items); err != nil {
		t.Fatal(err)
	}
	resumedAssistant.RestoreCache(storedSession.CacheReading)

	log, err = store.Open(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	var screenOutput bytes.Buffer
	resumedRecorder := record.New(log)
	resumedRecorder.Resume(len(storedSession.Items))
	resumedHarness := &App{
		agent:          resumedAssistant,
		screen:         scenario.screen(&screenOutput),
		recorder:       resumedRecorder,
		recordedEvents: slices.Clone(storedSession.Events),
		hostToSandbox:  goldenPorts,
		display:        displayState{modelName: scenario.Model},
	}
	settleResumedSessionGoldenMode(resumedHarness, storedSession.Events)
	restoredEvents := restoreSessionGoldenConditions(
		t, resumedHarness, storedSession, sessionGoldenConditions(t, resumeConditionsOf(scenario)),
	)
	if len(scenario.JobsRunningIntoResume) > 0 {
		restoredEvents = restoreSessionGoldenRunningJobs(t, resumedHarness, storedSession.Events, restoredEvents)
	}
	resumedHarness.currentTurn = Turn{Stream: testRunningTurnStream()}
	resumedHarness.replay()
	requireSameVisibleScreen(
		t,
		"first interactive turn differs after resume",
		firstScreenOutput.String(),
		screenOutput.String(),
	)
	resumedHarness.settleAccess()
	if note := resumedHarness.prelude(); note != "" {
		resumedAssistant.AddUserMessage(note)
	}
	resumeTurns := runSessionGoldenTurn(t, resumedHarness, scenario.ResumeTurn, cancelSignals)
	resumedHarness.dropPendingInput()

	printedScreen := printedSessionIsImpossible
	if !scenario.usesTheInterface() && !scenario.ResumeTurn.usesTheInterface() {
		printedOutput := drawPrintedSessionGoldenTurn(
			t, directory, scenario, resumedHarness, restoredEvents, resumeTurns,
		)
		requireNothingWasDrawnOver(t, printedOutput)
		requireSameVisibleScreen(
			t,
			"printed resumed session differs from what the interface drew",
			screenOutput.String(),
			printedOutput,
		)
		printedScreen = strings.Join(visibleScreen(t, printedOutput, replayColumns), "\n") + "\n"
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if got := int(requestCount.Load()); got != len(responses) {
		t.Errorf("provider received %d requests, want %d", got, len(responses))
	}

	transcriptPath := filepath.Join(directory, sessionName, "chat.md")
	transcript, err := os.ReadFile(transcriptPath) //nolint:gosec // the path is the test's own session directory
	if err != nil {
		t.Fatal(err)
	}

	storedSession, err = store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}
	var replayOutput bytes.Buffer
	replayHarness := &App{
		agent:          resumedAssistant,
		screen:         scenario.screen(&replayOutput),
		recordedEvents: storedSession.Events,
		display:        displayState{modelName: scenario.Model},
	}
	replayHarness.replay()

	requireSameVisibleScreen(
		t,
		"resumed live screen differs from its stored replay",
		screenOutput.String(),
		replayOutput.String(),
	)

	var printedReplayOutput bytes.Buffer
	printedReplayHarness := &App{
		agent:          resumedAssistant,
		screen:         scenario.screen(&printedReplayOutput).AppendOnly(),
		recordedEvents: storedSession.Events,
		runMode:        runMode{isPrinting: true},
		display:        displayState{modelName: scenario.Model},
	}
	printedReplayHarness.replay()

	requireNothingWasDrawnOver(t, printedReplayOutput.String())
	requireSameVisibleScreen(
		t,
		"printed replay differs from what the interface drew",
		replayOutput.String(),
		printedReplayOutput.String(),
	)
	liveScreen := visibleScreen(t, screenOutput.String(), replayColumns)

	ansi := strings.TrimRight(strutil.VisibleEscapes(screenOutput.String()), "\n") + "\n"
	settledScreen := strings.Join(liveScreen, "\n") + "\n"

	requestMutex.Lock()
	capturedRequestBodies := slices.Clone(requestBodies)
	requestMutex.Unlock()
	requests := canonicalProviderRequests(t, capturedRequestBodies)

	outputs := map[string]string{
		".jsonl":          canonicalSessionJournal(t, directory, sessionName),
		".meta.json":      canonicalSessionMeta(t, directory, sessionName),
		".ansi":           ansi,
		".screen":         settledScreen,
		".transcript":     canonicalSessionTranscript(string(transcript), sessionName),
		".requests.jsonl": requests,
		".print":          printedScreen,
	}

	for extension, drawn := range outputs {
		drawn = strings.ReplaceAll(drawn, sessionName, "tame-impala")
		if scenario.ScratchDirectory != "" {
			drawn = strings.ReplaceAll(drawn, scenario.ScratchDirectory, "/state/farm/tame-impala")
		}
		if scenario.CredentialsPath != "" {
			pendingCredentialsPattern := regexp.MustCompile(regexp.QuoteMeta(scenario.CredentialsPath) + `\.[0-9]+`)
			drawn = pendingCredentialsPattern.ReplaceAllString(drawn, "auth.json.pending")
			drawn = strings.ReplaceAll(drawn, scenario.CredentialsPath, "auth.json")
		}
		outputs[extension] = endpointAddressPattern.ReplaceAllString(drawn, "http://endpoint")
	}

	return outputs
}

func canonicalProviderRequests(t *testing.T, requestBodies [][]byte) string {
	t.Helper()

	cacheKeys := map[string]string{}

	var canonical bytes.Buffer
	for _, requestBody := range requestBodies {
		var request map[string]any
		if err := json.Unmarshal(requestBody, &request); err != nil {
			t.Fatal(err)
		}

		if key, isKeyed := request["prompt_cache_key"].(string); isKeyed {
			if _, isSeen := cacheKeys[key]; !isSeen {
				cacheKeys[key] = fmt.Sprintf("session-%d", len(cacheKeys)+1)
			}
			request["prompt_cache_key"] = cacheKeys[key]
		}
		canonicalRequest, _ := canonicalImages(request)
		encoded, err := json.Marshal(canonicalRequest)
		if err != nil {
			t.Fatal(err)
		}
		canonical.Write(encoded)
		canonical.WriteByte('\n')
	}
	return canonical.String()
}

func canonicalImages(value any) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		wasDescribed := false
		for key, item := range typed {
			described, itemWasDescribed := canonicalImages(item)
			typed[key] = described
			wasDescribed = wasDescribed || itemWasDescribed
		}
		return typed, wasDescribed
	case []any:
		wasDescribed := false
		for index, item := range typed {
			described, itemWasDescribed := canonicalImages(item)
			typed[index] = described
			wasDescribed = wasDescribed || itemWasDescribed
		}
		return typed, wasDescribed
	case string:
		if description, isImage := describeEncodedImage(typed); isImage {
			return description, true
		}
	}

	return value, false
}

func describeEncodedImage(value string) (string, bool) {
	encoded := value
	if _, payload, isDataURL := strings.Cut(value, ";base64,"); isDataURL {
		encoded = payload
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}

	mediaType := http.DetectContentType(data)
	if !imageutil.IsSupported(mediaType) {
		return "", false
	}

	width, height, isMeasured := imageutil.Dimensions(data)
	if !isMeasured {
		return "", false
	}

	return fmt.Sprintf("[%s %dx%d]", mediaType, width, height), true
}

func canonicalImagePayload(t *testing.T, payload json.RawMessage) json.RawMessage {
	t.Helper()

	if len(payload) == 0 {
		return payload
	}

	var value any
	if json.Unmarshal(payload, &value) != nil {
		return payload
	}

	described, wasDescribed := canonicalImages(value)
	if !wasDescribed {
		return payload
	}

	encoded, err := json.Marshal(described)
	if err != nil {
		t.Fatal(err)
	}

	return encoded
}

var repaintingSequence = regexp.MustCompile(`\x1b(\[[0-9?; ]*[ABCDHJKlhq]|\]9;4;)`)

func requireNothingWasDrawnOver(t *testing.T, printedOutput string) {
	t.Helper()

	if found := repaintingSequence.FindString(printedOutput); found != "" {
		t.Errorf(
			"printed session used %q\n%s",
			strutil.VisibleEscapes(found),
			strutil.VisibleEscapes(printedOutput),
		)
	}
}

func requireSameVisibleScreen(t *testing.T, description string, firstOutput string, secondOutput string) {
	t.Helper()

	requireSameVisibleScreenInColumns(t, description, replayColumns, firstOutput, secondOutput)
}

func requireSameVisibleScreenInColumns(
	t *testing.T,
	description string,
	columns int,
	firstOutput string,
	secondOutput string,
) {
	t.Helper()

	firstScreen := playScreen(t, firstOutput, columns)
	secondScreen := playScreen(t, secondOutput, columns)

	if first, second := firstScreen.text(), secondScreen.text(); !slices.Equal(first, second) {
		t.Errorf(
			"%s\nfirst:\n%s\nsecond:\n%s",
			description,
			strings.Join(first, "\n"),
			strings.Join(second, "\n"),
		)
		return
	}

	if first, second := firstScreen.styled(), secondScreen.styled(); !slices.Equal(first, second) {
		t.Errorf(
			"%s, in its styling\nfirst:\n%s\nsecond:\n%s",
			description,
			strutil.VisibleEscapes(strings.Join(first, "\n")),
			strutil.VisibleEscapes(strings.Join(second, "\n")),
		)
		return
	}

	if first, second := firstScreen.marked(), secondScreen.marked(); !slices.Equal(first, second) {
		t.Errorf("%s, in the rows it marked\nfirst: %v\nsecond: %v", description, first, second)
	}
}

func settleSessionGoldenMode(testHarness *App) {
	testHarness.mode = caps.NewMode(caps.All())
	testHarness.settleAccess()
}

func sessionGoldenConditions(t *testing.T, written string) conditions.Conditions {
	t.Helper()

	if written == "" {
		return conditions.Conditions{UnixSockets: true, IPv6: true, Interactive: true}
	}

	var current conditions.Conditions
	for name := range strings.FieldsSeq(written) {
		switch name {
		case "unix-sockets":
			current.UnixSockets = true
		case "ipv6":
			current.IPv6 = true
		case "interactive":
			current.Interactive = true
		case "none":
		default:
			t.Fatalf("unknown condition %q", name)
		}
	}

	return current
}

func restoreSessionGoldenConditions(
	t *testing.T,
	testHarness *App,
	storedSession *store.Session,
	current conditions.Conditions,
) []agent.Event {
	t.Helper()

	restored, err := conditions.Restore(storedSession.Meta.Conditions, storedSession.Events, current)
	if err != nil {
		t.Fatal(err)
	}

	testHarness.conditions = restored.State
	if !restored.IsChanged {
		return slices.Clone(storedSession.Events)
	}

	testHarness.pendingNotices.add(restored.Change)

	return append(slices.Clone(storedSession.Events), restored.Change)
}

func (self sessionGoldenScenario) describesConditions() bool {
	return self.Conditions != "" || self.ConditionsOnResume != ""
}

func resumeConditionsOf(scenario sessionGoldenScenario) string {
	if scenario.ConditionsOnResume != "" {
		return scenario.ConditionsOnResume
	}

	return scenario.Conditions
}

func settleResumedSessionGoldenMode(testHarness *App, events []agent.Event) {
	resumedCaps, found := caps.LastRecordedMode(events)
	if !found {
		resumedCaps = caps.All()
	}

	testHarness.mode = caps.NewMode(resumedCaps)
	testHarness.settledCaps = resumedCaps
}

const sessionGoldenWorkspace = "/workspace"

func startSessionGoldenJobHoldingTheWorkspace(t *testing.T, testHarness *App, name string) {
	t.Helper()

	manager := jobs.New(stoppableRunner{})
	t.Cleanup(func() { _ = manager.Close() })

	testHarness.jobs = jobState{manager: manager}
	testHarness.workspace = work.At(sessionGoldenWorkspace)

	policy := sandbox.Policy{Write: []string{sessionGoldenWorkspace}}
	if _, err := manager.Start(t.Context(), name, ".", "just "+name, policy); err != nil {
		t.Fatalf("the job did not start: %v", err)
	}
}

func recordSessionGoldenRunningJobs(testHarness *App, names []string) {
	startedAt := time.Date(2026, time.August, 23, 14, 32, 9, 0, time.UTC)

	listing := make([]jobs.Snapshot, 0, len(names))
	for _, name := range names {
		listing = append(listing, jobs.Snapshot{
			Name:      name,
			Command:   "just " + name,
			State:     jobs.StateRunning,
			StartedAt: startedAt,
		})
	}

	event := jobrecord.ListingEvent(listing)
	testHarness.recordedEvents = append(testHarness.recordedEvents, event)
	testHarness.storeEvent(event)
}

func restoreSessionGoldenRunningJobs(
	t *testing.T,
	testHarness *App,
	storedEvents []agent.Event,
	restoredEvents []agent.Event,
) []agent.Event {
	t.Helper()

	manager := jobs.New(stoppableRunner{})
	t.Cleanup(func() { _ = manager.Close() })

	testHarness.jobs = jobState{manager: manager}

	heldBefore := len(testHarness.pendingNotices.items)
	testHarness.restoreJobs(storedEvents)

	held := testHarness.pendingNotices.items[heldBefore:]
	if len(held) == 0 {
		t.Fatal("the restored jobs said nothing about the session having closed")
	}

	for _, item := range held {
		restoredEvents = append(restoredEvents, item.state)
	}

	return restoredEvents
}

func settleSessionGoldenJob(t *testing.T, testHarness *App, name string) {
	t.Helper()

	if _, err := testHarness.jobs.manager.Wait(t.Context(), []string{name}); err != nil {
		t.Fatalf("the stopped job never settled: %v", err)
	}

	testHarness.jobs.manager.PruneFinished()
}

func toggleSessionGoldenCaps(t *testing.T, testHarness *App, flags string) {
	t.Helper()

	for _, flag := range flags {
		toggledCaps, known := caps.Named(string(flag))
		if !known {
			t.Fatalf("unknown capability flag %q", string(flag))
		}
		testHarness.toggleCap(toggledCaps)
	}
}

type unaskedProvider struct{}

func (unaskedProvider) Configure(string, []tool.Definition)   {}
func (unaskedProvider) AddUserMessage(string)                 {}
func (unaskedProvider) AddToolResults([]agent.ToolCallResult) {}
func (unaskedProvider) Dump() []json.RawMessage               { return nil }
func (unaskedProvider) Load([]json.RawMessage)                {}

func (unaskedProvider) Send(ctx context.Context, _ agent.Yield) (agent.Reply, error) {
	<-ctx.Done()

	return agent.Reply{}, ctx.Err()
}

func drawPrintedSessionGoldenTurn(
	t *testing.T,
	directory string,
	scenario sessionGoldenScenario,
	liveHarness *App,
	restoredEvents []agent.Event,
	drawnTurns [][]TurnEvent,
) string {
	t.Helper()

	log, err := store.Create(directory, store.Meta{
		Model:        scenario.Model,
		Provider:     scenario.Provider,
		Effort:       scenario.Effort,
		IsFast:       scenario.IsFast,
		SystemPrompt: sessionGoldenSystemPrompt,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	var screenOutput bytes.Buffer
	printedHarness := &App{
		agent:    agent.New(sessionGoldenSystemPrompt, unaskedProvider{}, nil),
		screen:   scenario.screen(&screenOutput).AppendOnly(),
		recorder: record.New(log),
		runMode:  runMode{isPrinting: true},
		display:  displayState{modelName: scenario.Model},
	}
	if len(restoredEvents) > 0 {
		printedHarness.recordedEvents = slices.Clone(restoredEvents)
		settleResumedSessionGoldenMode(printedHarness, restoredEvents)
		printedHarness.currentTurn = Turn{Stream: testRunningTurnStream()}
		printedHarness.replay()
	} else {
		settleSessionGoldenMode(printedHarness)
	}
	printedHarness.currentTurn = Turn{
		Stream:  testRunningTurnStream(),
		painter: printedHarness.newPainter(true),
	}

	for index, drawnTurnEvents := range drawnTurns {
		for _, turnEvent := range drawnTurnEvents {
			printedHarness.takeTurn(turnEvent)
		}
		if index == len(drawnTurns)-1 && liveHarness.currentTurn.Cancelled() {
			printedHarness.interruptTurn(liveHarness.interruptionCause())
		}
		printedHarness.finish()
	}
	printedHarness.dropPendingInput()

	drawn := screenOutput.String()
	if printedHarness.currentTurn.Running() {
		printedHarness.interruptTurn(interrupt.SessionClose)
	}

	return drawn
}

func runSessionGoldenTurn(
	t *testing.T,
	testHarness *App,
	turn sessionGoldenTurn,
	cancelSignals <-chan struct{},
) [][]TurnEvent {
	t.Helper()

	var drawnTurnEvents []TurnEvent

	var streamContext context.Context
	var cancel context.CancelCauseFunc
	if turn.Timeout == "" {
		streamContext, cancel = context.WithCancelCause(t.Context())
	} else {
		timeout, err := time.ParseDuration(turn.Timeout)
		if err != nil {
			t.Fatal(err)
		}
		timed, stopTimer := context.WithTimeout(t.Context(), timeout)
		defer stopTimer()
		streamContext, cancel = context.WithCancelCause(timed)
	}
	defer cancel(nil)
	testHarness.currentTurn.Stream = testRunningTurnStreamWithCancel(cancel)
	inputLine := edit.NewInput(nil)
	stopKey := key.Key{Code: key.Escape}
	if turn.CancelWithCtrlD {
		stopKey = key.Key{Code: key.Rune, Value: 'd', Mod: key.Ctrl}
	}
	interruptWithStopKey := func() {
		if !testHarness.apply(inputLine, nil, stopKey) {
			t.Fatal("the stop key closed the harness")
		}
	}

	go func() {
		select {
		case <-cancelSignals:
			cancel(nil)
		case <-streamContext.Done():
		}
	}()

	takeToolRequest := func(toolRequests int) {
		if toolRequests == turn.CancelAfterToolRequest {
			interruptWithStopKey()
		}
		if toolRequests <= len(turn.QueueAfterEachToolRequest) {
			testHarness.currentTurn.Interject(turn.QueueAfterEachToolRequest[toolRequests-1])
		}
		if toolRequests == 1 {
			takeFirstSessionGoldenToolRequest(t, testHarness, turn, inputLine, interruptWithStopKey)
		}
	}

	reasoningDeltas := 0
	reasoningEvents := 0
	messageDeltas := 0
	toolRequests := 0
	retryNotices := 0
	for update, streamError := range testHarness.agent.Stream(streamContext, turn.Prompt, testHarness.currentTurn.Interjections()) {
		drawnTurnEvents = append(drawnTurnEvents, TurnEvent{Update: update, Err: streamError})
		testHarness.takeTurn(TurnEvent{Update: update, Err: streamError})
		if update.Delta != nil {
			switch update.Delta.Kind { //nolint:exhaustive // Only model prose event kinds can be deltas.
			case agent.ModelReasoningEvent:
				reasoningDeltas++
				if reasoningDeltas == turn.CancelAfterReasoningDelta {
					interruptWithStopKey()
				}
			case agent.ModelMessageEvent:
				messageDeltas++
				if messageDeltas == turn.CancelAfterMessageDelta {
					interruptWithStopKey()
				}
				if messageDeltas == 1 {
					takeFirstSessionGoldenMessageDelta(t, testHarness, turn)
				}
			}
		}
		if update.Event != nil && update.Event.Kind == agent.ModelReasoningEvent {
			reasoningEvents++
			if reasoningEvents == turn.CancelAfterReasoningEvent {
				interruptWithStopKey()
			}
			if reasoningEvents == 1 && turn.EndJobAfterReasoningEvent != "" {
				testHarness.jobEnded(endedSessionGoldenJob(turn.EndJobAfterReasoningEvent))
			}
		}
		if update.Event != nil && update.Event.Kind == agent.RetryingEvent {
			retryNotices++
			if retryNotices == turn.CancelAfterRetryNotice {
				interruptWithStopKey()
			}
		}
		if update.Event != nil && update.Event.Kind == agent.ToolCallRequestEvent {
			toolRequests++
			takeToolRequest(toolRequests)
		}
	}
	if turn.IsCancelled && !testHarness.currentTurn.Cancelled() {
		testHarness.interruptTurn(stopKeyCause(stopKey))
	}
	testHarness.finish()

	drawnTurns := [][]TurnEvent{drawnTurnEvents}

	return append(drawnTurns, runQueuedSessionGoldenTurns(t, testHarness, turn.ToggleDuringModeTurn)...)
}

func takeFirstSessionGoldenToolRequest(
	t *testing.T,
	testHarness *App,
	turn sessionGoldenTurn,
	inputLine *edit.Input,
	interruptWithStopKey func(),
) {
	t.Helper()

	if turn.ReplaceAfterToolRequest != "" {
		testHarness.replaceTurn(turn.ReplaceAfterToolRequest)
	}

	for _, queued := range turn.QueueAfterToolRequest {
		testHarness.currentTurn.Interject(queued)
	}

	if len(turn.QueueAfterToolRequest) > 0 && turn.CancelAfterQueueing {
		for range len(turn.QueueAfterToolRequest) + 1 {
			if testHarness.currentTurn.Cancelled() {
				break
			}
			interruptWithStopKey()
		}
	}

	if turn.FlushAfterToolRequest {
		testHarness.continueOrFlush(inputLine, edit.NewHistory("", historyLimit))
	}

	if turn.ToggleAfterToolRequest != "" {
		toggleSessionGoldenCaps(t, testHarness, turn.ToggleAfterToolRequest)
		if turn.CancelAfterToolToggle {
			interruptWithStopKey()
		}
	}

	if turn.EndJobAfterToolRequest != "" {
		testHarness.jobEnded(endedSessionGoldenJob(turn.EndJobAfterToolRequest))
	}
}

func sessionGoldenHostCommand(command string) agent.Event {
	return hostcommand.RanEvent(hostcommand.Result{
		Command: command,
		Output:  " M cmd/oh/draw.go\n?? notes.txt\n",
	})
}

func endedSessionGoldenJob(name string) jobs.Conclusion {
	startedAt := time.Date(2026, time.August, 23, 14, 32, 9, 0, time.UTC)

	return jobs.Conclusion{
		Snapshot: jobs.Snapshot{
			Name:      name,
			Command:   "just " + name,
			State:     jobs.StateFailed,
			StartedAt: startedAt,
			EndedAt:   startedAt.Add(12 * time.Second),
			ExitCode:  2,
		},
		Output: "go build ./...\ncmd/oh/draw.go:41:9: undefined: getWidth\nexit status 1",
	}
}

func takeFirstSessionGoldenMessageDelta(t *testing.T, testHarness *App, turn sessionGoldenTurn) {
	t.Helper()

	if turn.ToggleAfterMessageDelta != "" {
		toggleSessionGoldenCaps(t, testHarness, turn.ToggleAfterMessageDelta)
	}

	for _, queued := range turn.QueueAfterMessageDelta {
		testHarness.currentTurn.Interject(queued)
	}
}

func runQueuedSessionGoldenTurns(t *testing.T, testHarness *App, toggleDuringModeTurn string) [][]TurnEvent {
	t.Helper()

	var drawnTurns [][]TurnEvent

	for testHarness.currentTurn.Running() {
		var drawnTurnEvents []TurnEvent
		for event := range testHarness.currentTurn.Events() {
			drawnTurnEvents = append(drawnTurnEvents, event)
			testHarness.takeTurn(event)
			if toggleDuringModeTurn == "" || event.Update.Delta == nil {
				continue
			}
			if event.Update.Delta.Kind == agent.ModelMessageEvent {
				toggleSessionGoldenCaps(t, testHarness, toggleDuringModeTurn)
				toggleDuringModeTurn = ""
			}
		}
		testHarness.finish()
		drawnTurns = append(drawnTurns, drawnTurnEvents)
	}

	return drawnTurns
}

func canonicalSessionMeta(t *testing.T, directory string, name string) string {
	t.Helper()

	encoded, err := os.ReadFile(filepath.Join(directory, name, "meta.json")) //nolint:gosec // the path is the test's own session directory
	if err != nil {
		t.Fatal(err)
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &meta); err != nil {
		t.Fatal(err)
	}
	canonicalName, err := json.Marshal("tame-impala")
	if err != nil {
		t.Fatal(err)
	}
	canonicalTime, err := json.Marshal(transcriptTime)
	if err != nil {
		t.Fatal(err)
	}
	meta["name"] = canonicalName
	meta["started"] = canonicalTime
	meta["touched"] = canonicalTime

	canonical, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	return string(append(canonical, '\n'))
}

func canonicalSessionJournal(t *testing.T, directory string, name string) string {
	t.Helper()

	var canonical bytes.Buffer
	err := session.Records(directory, name, func(line session.Line) error {
		var event *agent.Event
		if line.Event != nil {
			canonicalEvent := *line.Event
			canonicalEvent.Took = 0
			event = &canonicalEvent
		}

		var turnSummary *session.TurnSummary
		if line.Turn != nil {
			canonicalSummary := *line.Turn
			canonicalSummary.Took = 0
			turnSummary = &canonicalSummary
		}

		record := struct {
			Kind    session.Kind         `json:"kind"`
			Version int                  `json:"version,omitempty"`
			Meta    json.RawMessage      `json:"meta,omitempty"`
			Turn    *session.TurnSummary `json:"turn,omitempty"`
			Event   *agent.Event         `json:"event,omitempty"`
			Payload json.RawMessage      `json:"payload,omitempty"`
		}{
			Kind:    line.Kind,
			Version: line.Version,
			Meta:    line.Meta,
			Turn:    turnSummary,
			Event:   event,
			Payload: canonicalImagePayload(t, line.Payload),
		}

		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		canonical.Write(encoded)
		canonical.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return canonical.String()
}

var (
	endpointAddressPattern    = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)
	transcriptStartedPattern  = regexp.MustCompile(`(?m)^- \*\*Started:\*\* ` + "`[^`]+`$")
	transcriptEventPattern    = regexp.MustCompile(`(?m)^> [^\n]+$`)
	transcriptDurationPattern = regexp.MustCompile(`(?m)^- \*\*Duration:\*\* ` + "`[^`]+`$")
	transcriptElapsedPattern  = regexp.MustCompile(`(?m)(^## .+ · )\+[^ \n]+$`)
)

func canonicalSessionTranscript(transcript string, sessionName string) string {
	canonical := strings.ReplaceAll(transcript, sessionName, "tame-impala")
	canonical = transcriptStartedPattern.ReplaceAllString(
		canonical,
		"- **Started:** `"+transcriptTime.Format(time.RFC3339Nano)+"`",
	)
	canonical = transcriptDurationPattern.ReplaceAllString(canonical, "- **Duration:** `0s`")
	canonical = transcriptElapsedPattern.ReplaceAllString(canonical, "${1}+0s")

	eventIndex := 0
	return transcriptEventPattern.ReplaceAllStringFunc(canonical, func(string) string {
		when := transcriptTime.Add(time.Duration(eventIndex) * time.Second)
		eventIndex++
		return "> " + when.Format(time.RFC3339Nano)
	})
}

func TestCanonicalTranscriptElapsedTimeDoesNotDependOnTestExecutionSpeed(t *testing.T) {
	transcript := "## Assistant · +0.1s\n"
	if got := canonicalSessionTranscript(transcript, "session"); got != "## Assistant · +0s\n" {
		t.Errorf("got %q", got)
	}
}

type faultingConversationLog struct {
	SessionLogger

	failedKind         agent.Kind
	itemFailure        error
	completionFailure  error
	warnings           []error
	eventAttempts      int
	failedAttempts     int
	completionAttempts int
}

func (self *faultingConversationLog) Event(event agent.Event) error {
	self.eventAttempts++
	if event.Kind == self.failedKind {
		self.failedAttempts++
		return errors.New("canonical write failed")
	}
	return self.SessionLogger.Event(event)
}

func (self *faultingConversationLog) Item(item json.RawMessage) error {
	if self.itemFailure != nil {
		return self.itemFailure
	}
	return self.SessionLogger.Item(item)
}

func (self *faultingConversationLog) CompleteTurn(summary session.TurnSummary) error {
	self.completionAttempts++
	if self.completionFailure != nil {
		return self.completionFailure
	}
	return self.SessionLogger.CompleteTurn(session.TurnSummary{})
}

func (self *faultingConversationLog) TakeWarnings() []error {
	warnings := self.warnings
	self.warnings = nil
	return warnings
}

func newStorageFaultHarness(log SessionLogger, assistant *agent.Agent) *App {
	testHarness := &App{
		agent:    assistant,
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.All()),
	}
	testHarness.currentTurn = Turn{
		painter: testHarness.newPainter(false),
		Stream:  testTimedTurnStream(false, time.Now(), time.Time{}),
	}
	return testHarness
}

func TestCanonicalEventWriteFailuresWarnWithoutRecursing(t *testing.T) {
	for _, failedKind := range []agent.Kind{
		agent.UserMessageEvent,
		agent.ModelReasoningEvent,
		agent.ModelMessageEvent,
		agent.FailureEvent,
	} {
		t.Run(string(failedKind), func(t *testing.T) {
			directory := t.TempDir()
			innerLog, err := store.Create(directory, store.Meta{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = innerLog.Close() }()

			log := &faultingConversationLog{SessionLogger: innerLog, failedKind: failedKind}
			testHarness := newStorageFaultHarness(log, agent.New("", quietProvider{}, nil))
			if failedKind != agent.UserMessageEvent {
				testHarness.recordEvent(agent.Event{Kind: agent.UserMessageEvent, Text: "start"})
			}
			testHarness.recordEvent(agent.Event{Kind: failedKind, Text: "failed event"})

			if log.failedAttempts != 1 {
				t.Errorf("failed %d writes, want 1", log.failedAttempts)
			}
			if log.eventAttempts > 3 {
				t.Errorf("made %d event write attempts", log.eventAttempts)
			}
			if failedKind != agent.UserMessageEvent {
				storedSession, err := store.Read(directory, innerLog.Name())
				if err != nil {
					t.Fatal(err)
				}
				if len(storedSession.Events) == 0 || storedSession.Events[0].Kind != agent.UserMessageEvent {
					t.Errorf("canonical journal lost its successful prefix: %+v", storedSession.Events)
				}
			}
		})
	}
}

func TestProviderItemWriteFailureWarnsAndLeavesCanonicalJournalReadable(t *testing.T) {
	directory := t.TempDir()
	innerLog, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = innerLog.Close() }()
	if err := innerLog.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
		t.Fatal(err)
	}

	provider := &fakeProvider{items: []json.RawMessage{json.RawMessage(`{"type":"reasoning"}`)}}
	log := &faultingConversationLog{
		SessionLogger: innerLog,
		itemFailure:   errors.New("item write failed"),
	}
	testHarness := newStorageFaultHarness(log, agent.New("", provider, nil))
	testHarness.finish()

	storedSession, err := store.Read(directory, innerLog.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSession.Items) != 0 {
		t.Errorf("stored failed provider items: %s", storedSession.Items)
	}
	if storedSession.TurnCompletions != 0 {
		t.Errorf("completed %d turns after the provider state failed", storedSession.TurnCompletions)
	}
	if log.completionAttempts != 0 {
		t.Errorf("attempted %d completion writes after the provider state failed", log.completionAttempts)
	}
	if len(storedSession.Events) != 1 || storedSession.Events[0].Kind != agent.UserMessageEvent {
		t.Errorf("unexpected canonical events: %+v", storedSession.Events)
	}
	if testHarness.feedback.Message().Status != agent.ErrorStatus || !strings.Contains(testHarness.feedback.Message().Text, "item write failed") {
		t.Errorf("provider item failure was not retained as interface feedback: %+v", testHarness.feedback.Message())
	}
}

func TestTurnCompletionIsStoredOnlyAfterProviderStateSucceeds(t *testing.T) {
	for name, completionFailure := range map[string]error{
		"success":                  nil,
		"completion write failure": errors.New("completion write failed"),
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			innerLog, err := store.Create(directory, store.Meta{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = innerLog.Close() }()
			if err := innerLog.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
				t.Fatal(err)
			}

			provider := &fakeProvider{items: []json.RawMessage{json.RawMessage(`{"type":"message"}`)}}
			log := &faultingConversationLog{SessionLogger: innerLog, completionFailure: completionFailure}
			testHarness := newStorageFaultHarness(log, agent.New("", provider, nil))
			testHarness.finish()

			storedSession, err := store.Read(directory, innerLog.Name())
			if err != nil {
				t.Fatal(err)
			}
			if len(storedSession.Items) != 1 {
				t.Fatalf("stored %d provider items, want 1", len(storedSession.Items))
			}
			wantTurnCompletions := 1
			if completionFailure != nil {
				wantTurnCompletions = 0
			}
			if storedSession.TurnCompletions != wantTurnCompletions {
				t.Errorf("completed %d turns, want %d", storedSession.TurnCompletions, wantTurnCompletions)
			}
			if log.completionAttempts != 1 {
				t.Errorf("attempted %d completion writes, want 1", log.completionAttempts)
			}
		})
	}
}

func TestAuxiliaryRecorderWarningsAreShownOnceAndRemainCanonical(t *testing.T) {
	directory := t.TempDir()
	innerLog, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = innerLog.Close() }()
	if err := innerLog.Event(agent.Event{Kind: agent.UserMessageEvent, Text: "start"}); err != nil {
		t.Fatal(err)
	}

	log := &faultingConversationLog{
		SessionLogger: innerLog,
		warnings: []error{
			errors.New("chat.md recording disabled: transcript append failed"),
			errors.New("wire.http recording disabled: wire append failed"),
		},
	}
	testHarness := newStorageFaultHarness(log, agent.New("", quietProvider{}, nil))
	testHarness.showStorageWarnings()
	testHarness.showStorageWarnings()

	storedSession, err := store.Read(directory, innerLog.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(storedSession.Events) != 1 || storedSession.Events[0].Kind != agent.UserMessageEvent {
		t.Errorf("unexpected canonical events: %+v", storedSession.Events)
	}
	for _, want := range []string{"transcript append failed", "wire append failed"} {
		if !strings.Contains(testHarness.feedback.Message().Text, want) {
			t.Errorf("interface feedback omitted %q: %+v", want, testHarness.feedback.Message())
		}
	}
}

func testRunningTurnStream() *turn.Stream {
	return testTurnStream(nil, nil, turn.State{Running: true})
}

func testTurnStreamForRunning(isRunning bool) *turn.Stream {
	if !isRunning {
		return nil
	}
	return testRunningTurnStream()
}

func testTimedTurnStream(isRunning bool, startedAt time.Time, finishedAt time.Time) *turn.Stream {
	if isRunning {
		finishedAt = time.Time{}
	}
	return testTurnStream(nil, nil, turn.State{Running: isRunning, StartedAt: startedAt, FinishedAt: finishedAt})
}

func testRunningTurnStreamWithCancel(cancel context.CancelCauseFunc) *turn.Stream {
	return testTurnStream(nil, cancel, turn.State{Running: true})
}

func testTurnStream(events chan TurnEvent, cancel context.CancelCauseFunc, state turn.State) *turn.Stream {
	if events == nil {
		events = make(chan TurnEvent)
	}
	if cancel == nil {
		cancel = func(error) {}
	}
	if state.StartedAt.IsZero() {
		state.StartedAt = time.Now()
	}
	return turn.Adopt(events, cancel, state)
}

func TestTheTimerRetainsTheModelTurnWhileTheUserTurnCountsUp(t *testing.T) {
	finishedAt := time.Now().Add(-69 * time.Second)
	harness := App{currentTurn: Turn{
		Stream: testTimedTurnStream(false, finishedAt.Add(-time.Minute), finishedAt),
	}}

	got := harness.turnTiming()
	want := turn.Timing{UserTurn: 69 * time.Second, ModelTurn: time.Minute}
	if got.UserTurn.Round(time.Second) != want.UserTurn || got.ModelTurn != want.ModelTurn {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTheTimerRetainsTheUserTurnWhileTheModelTurnCountsUp(t *testing.T) {
	startedAt := time.Now().Add(-30 * time.Second)
	harness := App{currentTurn: Turn{Stream: testTurnStream(nil, nil, turn.State{
		Running:   true,
		StartedAt: startedAt,
		Timing:    turn.Timing{UserTurn: 3 * time.Minute},
	})}}

	got := harness.turnTiming()
	want := turn.Timing{UserTurn: 3 * time.Minute, ModelTurn: 30 * time.Second}
	if got.UserTurn != want.UserTurn || got.ModelTurn.Round(time.Second) != want.ModelTurn {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestTheTimerCountsUpFromTheSessionBeforeTheFirstTurn(t *testing.T) {
	harness := App{startedAt: time.Now().Add(-30 * time.Second)}

	got := harness.turnTiming()
	if got.UserTurn.Round(time.Second) != 30*time.Second || got.ModelTurn != 0 {
		t.Errorf("got %+v, want a 30 second user turn", got)
	}
}

const (
	thinkingFor = 2 * time.Second
	wordEvery   = 120 * time.Millisecond
	toolTakes   = 3 * time.Second
)

type fakeProvider struct {
	turn  int
	items []json.RawMessage
}

func (self *fakeProvider) Configure(string, []tool.Definition)   {}
func (self *fakeProvider) AddUserMessage(string)                 {}
func (self *fakeProvider) AddToolResults([]agent.ToolCallResult) {}
func (self *fakeProvider) Dump() []json.RawMessage               { return self.items }
func (self *fakeProvider) Load(items []json.RawMessage)          { self.items = items }

func (self *fakeProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	self.turn++

	time.Sleep(thinkingFor)

	for _, thought := range []string{
		"**Reading the file**\nThe path they gave is the one to start with.",
		"**Searching for the spinner**\nIt is drawn somewhere near the output layer.",
	} {
		if !yield(agent.Output{Kind: agent.ModelReasoningEvent, Text: thought}) ||
			!yield(agent.Output{Kind: agent.ModelReasoningEvent, Done: true}) {
			return agent.Reply{}, nil
		}

		time.Sleep(wordEvery)
	}

	for word := range strings.FieldsSeq("let me have a look at that for you") {
		if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: word + " "}) {
			return agent.Reply{}, nil
		}

		time.Sleep(wordEvery)
	}
	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
		return agent.Reply{}, nil
	}

	if self.turn > 1 {
		return agent.Reply{}, nil
	}

	return agent.Reply{Calls: []agent.ToolCall{
		{ID: "1", Name: "read", Arguments: `{"path":"main.go"}`},
		{ID: "2", Name: "grep", Arguments: `{"path":"spinner"}`},
		{ID: "3", Name: "write", Arguments: `{"path":"notes.md"}`},
	}}, nil
}

type fakeArgs struct {
	Path string `json:"path"`
}

func slowToolBuilder(name string) tool.Builder[fakeArgs] {
	return tool.Implement(
		tool.Definition{
			Name:        name,
			Description: "",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args fakeArgs) (string, string) { return args.Path, "" },
	)
}

func buildSlowTool(builder tool.Builder[fakeArgs]) tool.Tool {
	return builder.Plain(func(context.Context, fakeArgs) (string, error) {
		time.Sleep(toolTakes)
		return "one\ntwo\nthree", nil
	})
}

func slowTool(name string) tool.Tool {
	return buildSlowTool(slowToolBuilder(name))
}

type frameRecordingWriter struct {
	stream strings.Builder
	frames []string
}

func (self *frameRecordingWriter) Write(value []byte) (int, error) {
	_, _ = self.stream.Write(value)
	self.frames = append(self.frames, self.stream.String())
	return len(value), nil
}

func TestGoldenOrdinaryTabDrawsWhatItDrewBefore(t *testing.T) {
	pass := func() string {
		self := slashCommandFixture(t, caps.Read)
		var screenOutput strings.Builder
		self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

		history := edit.NewHistory("", historyLimit)
		inputLine := edit.NewInput(history)
		inputLine.SetText("ordinary input")

		self.screen.Line("conversation remains in scrollback")
		self.show(inputLine)
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Rune, Value: '\t'})
		for _, value := range "after tab" {
			self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Rune, Value: value})
		}

		return screenOutput.String()
	}
	passes := map[string]func() string{"tab inserts four spaces": pass}

	compareWithGolden(t, "ordinary-tab", ".ansi", passes)
	compareWithGolden(t, "ordinary-tab", ".screen", shownPasses(t, passes))
}

func TestAPasteIsDrawnOnlyWhenItHasFinished(t *testing.T) {
	writer := &frameRecordingWriter{}
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(writer, replayColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.show(inputLine)
	framesBeforePaste := len(writer.frames)

	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.PasteStart})
	for _, value := range "pasted text" {
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Rune, Value: value})
	}

	if drawn := len(writer.frames) - framesBeforePaste; drawn != 0 {
		t.Errorf("the input was drawn %d times while the paste arrived, want 0", drawn)
	}

	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.PasteEnd})

	visible := strings.Join(visibleScreen(t, writer.stream.String(), replayColumns), "\n")
	if !strings.Contains(visible, "pasted text") {
		t.Errorf("the finished paste was not drawn:\n%s", visible)
	}
}

func pasteReport(status key.ClipboardStatus, mediaType string, payload []byte) key.Key {
	return key.Key{Code: key.Clipboard, Clipboard: &key.ClipboardReport{
		Status:    status,
		MediaType: mediaType,
		Payload:   payload,
	}}
}

func pasteEvent(mediaTypes string) []key.Key {
	opened := key.Key{Code: key.Clipboard, Clipboard: &key.ClipboardReport{
		Status:   key.ClipboardOpened,
		Password: "one-time-token",
	}}

	return []key.Key{
		opened,
		pasteReport(key.ClipboardChunk, key.MediaTypeList, []byte(mediaTypes)),
		pasteReport(key.ClipboardClosed, "", nil),
	}
}

func pasteContent(mediaType string, payload []byte) []key.Key {
	return []key.Key{
		pasteReport(key.ClipboardOpened, "", nil),
		pasteReport(key.ClipboardChunk, mediaType, payload),
		pasteReport(key.ClipboardClosed, "", nil),
	}
}

func TestAPastedImageIsSavedAndItsPathInsertedAtTheCursor(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})

	var savedType string
	var savedData []byte
	self.savePastedImage = func(mediaType string, data []byte) (string, error) {
		savedType, savedData = mediaType, data
		return "/state/sessions/tame-impala/drops/image-123.png", nil
	}

	inputLine := edit.NewInput(nil)
	inputLine.SetText("look  now")
	for range len(" now") {
		inputLine.Apply(key.Key{Code: key.Left}, false)
	}

	for _, keypress := range slices.Concat(
		pasteEvent("image/png"),
		pasteContent("image/png", []byte("\x89PNG")),
	) {
		self.handleKeypressAndShowInput(inputLine, nil, keypress)
	}

	want := "look /state/sessions/tame-impala/drops/image-123.png now"
	if inputLine.Text() != want {
		t.Errorf("pasted input is %q, want %q", inputLine.Text(), want)
	}
	if savedType != "image/png" || string(savedData) != "\x89PNG" {
		t.Errorf("saved %q of %q, want the pasted PNG", savedType, savedData)
	}
}

func TestAPastedImageIsAskedForWithTheOneTimeTokenTheTerminalGave(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

	inputLine := edit.NewInput(nil)
	for _, keypress := range pasteEvent("text/html image/png text/plain") {
		self.handleKeypressAndShowInput(inputLine, nil, keypress)
	}

	want := "\x1b]5522;type=read:pw=b25lLXRpbWUtdG9rZW4=:name=UGFzdGUgZXZlbnQ=;aW1hZ2UvcG5n\x1b\\"
	if got := screenOutput.String(); !strings.Contains(got, want) {
		t.Errorf("clipboard request is %q, want it to contain %q", got, want)
	}
}

func TestAPastedTextIsInsertedWithoutAskingToSaveAnything(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	self.savePastedImage = func(string, []byte) (string, error) {
		t.Error("a text paste asked to save an image")
		return "", nil
	}

	inputLine := edit.NewInput(nil)
	inputLine.SetText("say ")

	for _, keypress := range slices.Concat(
		pasteEvent("text/plain"),
		pasteContent("text/plain", []byte("    hello\n    world")),
	) {
		self.handleKeypressAndShowInput(inputLine, nil, keypress)
	}

	if got, want := inputLine.Text(), "say hello\nworld"; got != want {
		t.Errorf("pasted input is %q, want %q", got, want)
	}
}

func TestAPasteFailureIsShownWithoutChangingTheInput(t *testing.T) {
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.New(&bytes.Buffer{})
	inputLine := edit.NewInput(nil)
	inputLine.SetText("draft")

	self.handleKeypressAndShowInput(inputLine, nil, key.Key{Code: key.Clipboard, Clipboard: &key.ClipboardReport{
		Status:  key.ClipboardFailed,
		Failure: "EPERM",
	}})

	if inputLine.Text() != "draft" {
		t.Errorf("failed paste changed the input to %q", inputLine.Text())
	}
	want := "Could not paste: permission to read the clipboard was refused"
	if self.feedback.Message().Text != want || self.feedback.Message().Status != agent.ErrorStatus {
		t.Errorf("paste feedback is %+v, want error %q", self.feedback.Message(), want)
	}
}

type pasteStage int

const (
	pasteNotStarted pasteStage = iota
	pasteArriving
	pasteFinished
)

func TestGoldenAPasteDrawsWhatItDrewBefore(t *testing.T) {
	passes := map[string]func() string{
		"1 before the paste": func() string {
			return pasteStream(t, "pasted text", pasteNotStarted)
		},
		"2 while the paste arrives": func() string {
			return pasteStream(t, "pasted text", pasteArriving)
		},
		"3 after the paste": func() string {
			return pasteStream(t, "pasted text", pasteFinished)
		},
		"4 exactly five lines": func() string {
			return pasteStream(t, "first line\nsecond line\nthird line\nfourth line\nfifth line", pasteFinished)
		},
		"4a six prose lines": func() string {
			return pasteStream(t, "first line\nsecond line\nthird line\nfourth line\nfifth line\nsixth line", pasteFinished)
		},
		"4b ambiguous code-like text": func() string {
			return pasteStream(t, "const greet = (name) => {\n  const message = `Hello ${name}`\n  console.log(message)\n  return message\n}\ngreet(\"world\")", pasteFinished)
		},
		"4c a Go paste of many lines": func() string {
			return pasteStream(t, "package main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"hello\")\n}", pasteFinished)
		},
		"4d a paste containing a fence": func() string {
			return pasteStream(t, "one\n```\nthree\nfour\nfive\nsix", pasteFinished)
		},
		"5 an indented paste": func() string {
			return pasteStream(t, "    if isReady {\n        begin()\n    }", pasteFinished)
		},
		"6 a paste wider than the screen": func() string {
			return pasteStream(t, strings.Repeat("wide ", 40), pasteFinished)
		},
		"7 a pasted image": func() string {
			return pastedImageStream(t)
		},
		"8 an image arriving in chunks": func() string {
			return chunkedImagePasteStream(t)
		},
		"9 a long pasted text after typed prose": func() string {
			return pastedTextStream(t)
		},
		"a1 a paste from the primary selection": func() string {
			return primarySelectionPasteStream(t)
		},
		"a2 a paste holding nothing usable": func() string {
			return unusablePasteStream(t)
		},
		"a3 an image that could not be saved": func() string {
			return unsavedImagePasteStream(t)
		},
		"a4 a paste the terminal refused": func() string {
			return refusedPasteStream(t)
		},
	}

	compareWithGolden(t, "paste", ".ansi", passes)
	compareWithGolden(t, "paste", ".screen", shownPasses(t, passes))
}

func pasteEventStream(t *testing.T, typed string, save func(string, []byte) (string, error), keys []key.Key) string {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	self.savePastedImage = save

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	inputLine.SetText(typed)

	self.screen.Line("conversation remains in scrollback")
	self.show(inputLine)

	for _, keypress := range keys {
		self.handleKeypressAndShowInput(inputLine, history, keypress)
	}

	return screenOutput.String()
}

func savedAs(path string) func(string, []byte) (string, error) {
	return func(string, []byte) (string, error) { return path, nil }
}

func pastedImageStream(t *testing.T) string {
	t.Helper()

	return pasteEventStream(t, "review ", savedAs("/state/sessions/tame-impala/drops/image-123.png"),
		slices.Concat(
			pasteEvent("image/png"),
			pasteContent("image/png", []byte("\x89PNG")),
		))
}

func chunkedImagePasteStream(t *testing.T) string {
	t.Helper()

	chunks := []key.Key{pasteReport(key.ClipboardOpened, "", nil)}
	for range 4 {
		chunks = append(chunks, pasteReport(key.ClipboardChunk, "image/png", make([]byte, 4096)))
	}
	chunks = append(chunks, pasteReport(key.ClipboardClosed, "", nil))

	return pasteEventStream(t, "review ", savedAs("/state/sessions/tame-impala/drops/image-456.png"),
		slices.Concat(pasteEvent("image/png"), chunks))
}

func pastedTextStream(t *testing.T) string {
	t.Helper()

	return pasteEventStream(t, "explain ", nil, slices.Concat(
		pasteEvent("text/plain"),
		pasteContent("text/plain", []byte("    first line\n    second line\n    third line\n    fourth line\n    fifth line\n    sixth line")),
	))
}

func primarySelectionPasteStream(t *testing.T) string {
	t.Helper()

	opened := key.Key{Code: key.Clipboard, Clipboard: &key.ClipboardReport{
		Status:   key.ClipboardOpened,
		Password: "one-time-token",
		Location: "primary",
	}}

	return pasteEventStream(t, "note ", nil, slices.Concat(
		[]key.Key{
			opened,
			pasteReport(key.ClipboardChunk, key.MediaTypeList, []byte("text/plain")),
			pasteReport(key.ClipboardClosed, "", nil),
		},
		pasteContent("text/plain", []byte("from the selection")),
	))
}

func unusablePasteStream(t *testing.T) string {
	t.Helper()

	return pasteEventStream(t, "draft", nil, pasteEvent("application/x-nautilus-clipboard"))
}

func unsavedImagePasteStream(t *testing.T) string {
	t.Helper()

	save := func(string, []byte) (string, error) {
		return "", errors.New("the drops path is not a directory")
	}

	return pasteEventStream(t, "draft", save, slices.Concat(
		pasteEvent("image/png"),
		pasteContent("image/png", []byte("\x89PNG")),
	))
}

func refusedPasteStream(t *testing.T) string {
	t.Helper()

	return pasteEventStream(t, "draft", nil, []key.Key{{
		Code:      key.Clipboard,
		Clipboard: &key.ClipboardReport{Status: key.ClipboardFailed, Failure: "EPERM"},
	}})
}

func pasteStream(t *testing.T, text string, stage pasteStage) string {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.screen.Line("conversation remains in scrollback")
	self.show(inputLine)

	if stage == pasteNotStarted {
		return screenOutput.String()
	}

	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.PasteStart})
	for _, value := range text {
		if value == '\n' {
			self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})
			continue
		}
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Rune, Value: value})
	}

	if stage == pasteFinished {
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.PasteEnd})
	}

	return screenOutput.String()
}

func TestGoldenControlCDrawsWhatItDrewBefore(t *testing.T) {
	written := "a line worth keeping"
	controlC := key.Key{Code: key.Rune, Value: 'c', Mod: key.Ctrl}
	controlU := key.Key{Code: key.Rune, Value: 'u', Mod: key.Ctrl}
	bang := key.Key{Code: key.Rune, Value: '!'}

	passes := map[string]func() string{
		"1 a written line": func() string {
			return clearingStream(t, written)
		},
		"2 after one ctrl+c": func() string {
			return clearingStream(t, written, controlC)
		},
		"3 after two ctrl+cs": func() string {
			return clearingStream(t, written, controlC, controlC)
		},
		"4 a key between two ctrl+cs": func() string {
			return clearingStream(t, written, controlC, bang, controlC)
		},
		"5 after one ctrl+u": func() string {
			return clearingStream(t, written, controlU)
		},
	}

	compareWithGolden(t, "clearing", ".ansi", passes)
	compareWithGolden(t, "clearing", ".screen", shownPasses(t, passes))
}

func clearingStream(t *testing.T, written string, keypresses ...key.Key) string {
	t.Helper()

	self := slashCommandFixture(t, caps.Read)
	var screenOutput strings.Builder
	self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)

	self.screen.Line("conversation remains in scrollback")
	self.show(inputLine)

	for _, value := range written {
		self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Rune, Value: value})
	}

	for _, keypress := range keypresses {
		self.handleKeypressAndShowInput(inputLine, history, keypress)
	}

	return screenOutput.String()
}

func TestHelpDuringReasoningIsDrawnInOneFrame(t *testing.T) {
	frames := helpDuringReasoningFrames(t)
	if len(frames) != 1 {
		t.Fatalf("help was drawn across %d frames, want 1", len(frames))
	}

	visible := strings.Join(visibleScreen(t, frames[0], replayColumns), "\n")
	for _, want := range []string{"╭", "│ Commands:", "│   /conf", "│   /copy", "┴"} {
		if !strings.Contains(visible, want) {
			t.Errorf("help did not settle in its frame:\n%s", visible)
		}
	}
	if strings.Contains(visible, "/help") {
		t.Errorf("accepted input remained in the settled frame:\n%s", visible)
	}
}

func helpDuringReasoningFrames(t *testing.T) []string {
	t.Helper()

	writer := &frameRecordingWriter{}
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(writer, replayColumns, replayLines)
	self.slash.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "help",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Notice("Commands:\n  /conf\n  /copy")
			return nil
		},
	})
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine
	inputLine.SetText("/help")
	self.show(inputLine)
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: "thinking about it"})
	framesBeforeHelp := len(writer.frames)

	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})

	frames := slices.Clone(writer.frames[framesBeforeHelp:])
	self.currentTurn.painter.Close(dynamic.Cancelled)
	return frames
}

func shownHelpDuringReasoningFrames(t *testing.T) string {
	t.Helper()

	var shown strings.Builder
	for index, frame := range helpDuringReasoningFrames(t) {
		fmt.Fprintf(&shown, "--- frame %d ---\n%s\n", index+1, strings.Join(visibleScreen(t, frame, replayColumns), "\n"))
	}
	return strings.TrimSuffix(shown.String(), "\n")
}

func TestHelpStaysPutOnAShortTerminalDuringARunningTurn(t *testing.T) {
	frames := shortRunningHelpFrames(t)
	if len(frames) < 2 {
		t.Fatalf("help drew only %d frame, want repeated running-turn repaints", len(frames))
	}

	first := visibleScreen(t, frames[0], replayColumns)
	for i, frame := range frames[1:] {
		if got := visibleScreen(t, frame, replayColumns); !slices.Equal(got, first) {
			t.Errorf("frame %d moved the help feedback:\n%s", i+2, strings.Join(got, "\n"))
		}
	}
}

func shortRunningHelpFrames(t *testing.T) []string {
	t.Helper()

	writer := &frameRecordingWriter{}
	self := slashCommandFixture(t, caps.Read)
	self.screen = output.NewTerminalOfSize(writer, replayColumns, shortLines)
	self.slash.commands = fixtureCommandRegistry(t, slash.Command{
		Name: "help",
		Run: func(context slash.Context, _ slash.Arguments) error {
			context.Notice("Commands:\n  /conf\n  /copy")
			return nil
		},
	})
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine
	inputLine.SetText("/help")
	self.show(inputLine)
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelReasoningEvent, Text: "thinking about it"})
	framesBeforeHelp := len(writer.frames)

	self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "answer"})
	self.show(inputLine)
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: " grows"})
	self.show(inputLine)
	self.currentTurn.painter.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: "\nand another line"})
	self.show(inputLine)

	frames := slices.Clone(writer.frames[framesBeforeHelp:])
	self.currentTurn.painter.Close(dynamic.Cancelled)
	return frames
}

func shownShortRunningHelpFrames(t *testing.T) string {
	t.Helper()

	var shown strings.Builder
	for index, frame := range shortRunningHelpFrames(t) {
		fmt.Fprintf(&shown, "--- frame %d ---\n%s\n", index+1, strings.Join(visibleScreen(t, frame, replayColumns), "\n"))
	}
	return strings.TrimSuffix(shown.String(), "\n")
}

type usageReport struct {
	windows []agent.UsageWindow
	err     error
	thenErr error
}

type scriptedUsageReporter struct {
	report   usageReport
	answered atomic.Int64
}

func (self *scriptedUsageReporter) IsAvailable() bool {
	return true
}

func (self *scriptedUsageReporter) UsageWindows(context.Context) ([]agent.UsageWindow, error) {
	defer self.answered.Add(1)

	if self.answered.Load() > 0 && self.report.thenErr != nil {
		return nil, self.report.thenErr
	}

	return self.report.windows, self.report.err
}

func meteredWindows(at time.Time) []agent.UsageWindow {
	return []agent.UsageWindow{
		{Duration: 7 * 24 * time.Hour, Percent: 6, ResetsAt: at.Add(6 * 24 * time.Hour)},
		{
			Duration: 5 * time.Hour,
			Percent:  4,
			ResetsAt: at.Add(4 * time.Hour),
			Scope:    "gpt-5.3-codex-spark",
		},
		{
			Duration: 7 * 24 * time.Hour,
			Percent:  2,
			ResetsAt: at.Add(6 * 24 * time.Hour),
			Scope:    "gpt-5.3-codex-spark",
		},
	}
}

func goldenUsagePass(
	t *testing.T, at time.Time, modelName string, report usageReport,
) func() string {
	t.Helper()

	var now atomic.Int64
	now.Store(at.UnixNano())

	reporter := &scriptedUsageReporter{report: report}
	readClock := func() time.Time { return time.Unix(0, now.Load()).UTC() }

	built, err := subUsage.New(subUsage.Settings{
		Reporter:         reporter,
		ModelName:        modelName,
		IsSelfRefreshing: true,
		Now:              readClock,
	})(goldenSegmentOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return func() string {
		drawn := settleUsage(t, built, reporter, 1)

		if report.thenErr == nil {
			return drawn
		}

		now.Store(at.Add(time.Hour).UnixNano())

		return settleUsage(t, built, reporter, 2)
	}
}

func goldenUsageFromCache(
	t *testing.T, at time.Time, modelName string, windows []agent.UsageWindow,
) func() string {
	t.Helper()

	readClock := func() time.Time { return at }
	cachePath := filepath.Join(t.TempDir(), "usage", "codex.json")

	seeded := &scriptedUsageReporter{report: usageReport{windows: windows}}
	if _, err := usage.Shared(seeded, cachePath, time.Hour, readClock).UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	silent := &scriptedUsageReporter{}

	built, err := subUsage.New(subUsage.Settings{
		Reporter:         silent,
		CachePath:        cachePath,
		ModelName:        modelName,
		IsSelfRefreshing: true,
		Now:              readClock,
	})(goldenSegmentOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return func() string { return built.Render(segment.Context{}) }
}

func goldenUsageUpdatedFromCache(t *testing.T, at time.Time) func() string {
	t.Helper()

	var now atomic.Int64
	now.Store(at.UnixNano())
	readClock := func() time.Time { return time.Unix(0, now.Load()).UTC() }
	cachePath := filepath.Join(t.TempDir(), "usage", "codex.json")

	first := &scriptedUsageReporter{report: usageReport{windows: []agent.UsageWindow{{
		Duration: 5 * time.Hour,
		Percent:  40,
		ResetsAt: at.Add(150 * time.Minute),
	}}}}
	firstShared := usage.Shared(first, cachePath, usage.DefaultRefresh, readClock)
	if _, err := firstShared.UsageWindows(t.Context()); err != nil {
		t.Fatal(err)
	}

	built, err := subUsage.New(subUsage.Settings{
		Reporter:         &scriptedUsageReporter{},
		CachePath:        cachePath,
		ModelName:        "gpt-5.6-sol",
		IsSelfRefreshing: true,
		Now:              readClock,
	})(goldenSegmentOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	return func() string {
		now.Store(at.Add(usage.DefaultRefresh).UnixNano())
		first.report.windows[0].Percent = 60
		if _, err := firstShared.UsageWindows(t.Context()); err != nil {
			t.Fatal(err)
		}

		return built.Render(segment.Context{})
	}
}

func settleUsage(
	t *testing.T, built segment.Segment, reporter *scriptedUsageReporter, answers int64,
) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	drawn := built.Render(segment.Context{})

	for {
		if time.Now().After(deadline) {
			t.Fatalf("the usage segment never settled, last drew %q", drawn)
		}

		time.Sleep(time.Millisecond)

		redrawn := built.Render(segment.Context{})
		if reporter.answered.Load() >= answers && redrawn == drawn {
			return redrawn
		}

		drawn = redrawn
	}
}

func TestEveryWayOfStoppingATurnSaysWhy(t *testing.T) {
	tests := map[string]struct {
		stopTurn func(*App)
		want     string
	}{
		"escape": {
			stopTurn: func(self *App) { self.cancelTurn(interrupt.Escape) },
			want:     "the user pressed escape",
		},
		"another message": {
			stopTurn: func(self *App) { self.replaceTurn("do this instead") },
			want:     "the user sent another message",
		},
		"a capability change": {
			stopTurn: func(self *App) { self.toggleCap(caps.Git) },
			want:     "access changed",
		},
		"leaving the session": {
			stopTurn: func(self *App) {
				if err := self.requestTransition(cycle.Transition{Kind: cycle.NewSession}); err != nil {
					t.Fatal(err)
				}
			},
			want: "the session closed",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var cause error

			self := &App{
				screen:   output.New(&bytes.Buffer{}),
				mode:     caps.NewMode(caps.Read),
				recorder: record.New(testLog(t)),
			}
			self.currentTurn = Turn{
				Stream:  testTurnStream(nil, func(err error) { cause = err }, turn.State{Running: true}),
				painter: self.newPainter(true),
			}

			test.stopTurn(self)

			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(cause)

			if got := stop.Sentence(ctx); got != test.want {
				t.Errorf("got %q, want the turn stopped because %q", got, test.want)
			}
		})
	}
}

type notingProvider struct {
	quietProvider

	mutex sync.Mutex
	notes []string
}

func (self *notingProvider) AddUserMessage(text string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.notes = append(self.notes, text)
}

func (self *notingProvider) told() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return slices.Clone(self.notes)
}

func TestTheHarnessAsksForATitleOnlyOnceTheModelHasAnsweredWithoutGivingOne(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	backend := &notingProvider{}
	self.agent = agent.New("", backend, []tool.Tool{title.New()})

	if note := self.titleNote(); note != "" {
		t.Errorf("expected the opening turn to be left alone, got %q", note)
	}

	self.recordedEvents = append(self.recordedEvents, agent.Event{Kind: agent.ModelMessageEvent, Text: "done"})
	if note := self.titleNote(); !strings.Contains(note, title.Name) {
		t.Errorf("expected an unanswered session to be asked for a title, got %q", note)
	}

	completeTurn(self)
	if !slices.ContainsFunc(backend.told(), func(note string) bool {
		return strings.Contains(note, "Untitled session")
	}) {
		t.Errorf("the model was told %q", backend.told())
	}

	self.recordedEvents = append(self.recordedEvents, agent.Event{
		Kind:  agent.StateChangeEvent,
		Name:  agent.TitleStateKey,
		State: json.RawMessage(`{"title":"fix the picker clipping"}`),
	})
	if note := self.titleNote(); note != "" {
		t.Errorf("expected a titled session to be left alone, got %q", note)
	}
}

func TestASessionWithoutTheTitleToolIsNeverAskedForATitle(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.agent = agent.NewWithEnabledTools("", quietProvider{}, []tool.Tool{title.New()}, nil)
	self.recordedEvents = append(self.recordedEvents, agent.Event{Kind: agent.ModelMessageEvent, Text: "done"})

	if note := self.titleNote(); note != "" {
		t.Errorf("expected a session that cannot title itself to be left alone, got %q", note)
	}
}

func TestTheSessionTitleReachesTheTerminalLiveAndOnResume(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)

	self.restore(&store.Session{Events: []agent.Event{
		{Kind: agent.UserMessageEvent, Text: "have a look at the picker"},
		{
			Kind:  agent.StateChangeEvent,
			Name:  agent.TitleStateKey,
			State: json.RawMessage(`{"title":"fix the picker clipping"}`),
		},
	}})

	if got := self.terminal.GetSessionTitle(); got != "fix the picker clipping" {
		t.Errorf("a resumed session went by %q", got)
	}

	self.currentTurn = Turn{painter: self.newPainter(true)}
	self.recordEvent(agent.Event{
		Kind:  agent.StateChangeEvent,
		Name:  agent.TitleStateKey,
		State: json.RawMessage(`{"title":"give sessions a title"}`),
	})

	if got := self.terminal.GetSessionTitle(); got != "give sessions a title" {
		t.Errorf("a retitled session went by %q", got)
	}
}

type plainTurnProvider struct {
	items []json.RawMessage
}

func (self *plainTurnProvider) Configure(string, []tool.Definition)   {}
func (self *plainTurnProvider) AddToolResults([]agent.ToolCallResult) {}
func (self *plainTurnProvider) Dump() []json.RawMessage               { return self.items }
func (self *plainTurnProvider) Load(items []json.RawMessage)          { self.items = items }

func (self *plainTurnProvider) AddUserMessage(text string) {
	self.items = append(self.items, fmt.Appendf(nil, `{"role":"user","content":%q}`, text))
}

func (self *plainTurnProvider) Send(_ context.Context, yield agent.Yield) (agent.Reply, error) {
	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "Said it."}) {
		return agent.Reply{}, nil
	}
	if !yield(agent.Output{Kind: agent.ModelMessageEvent, Done: true}) {
		return agent.Reply{}, nil
	}

	self.items = append(self.items, json.RawMessage(`{"role":"assistant","content":"Said it."}`))

	return agent.Reply{}, nil
}

func TestGoldenTheAppWritesAndResumesASessionOnItsOwn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{
		Model:        "claude-opus-5",
		Provider:     "anthropic",
		Effort:       "medium",
		SystemPrompt: sessionGoldenSystemPrompt,
	})
	if err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New(sessionGoldenSystemPrompt, &plainTurnProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.All()),
	}

	self.begin("say something")

	sessionName := log.Name()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(filepath.Join(directory, sessionName, "chat.md")) //nolint:gosec // the path is the test's own session directory
	if err != nil {
		t.Fatal(err)
	}

	compareScenarioGolden(t, "app-plain-turn.jsonl", canonicalSessionJournal(t, directory, sessionName))
	compareScenarioGolden(
		t, "app-plain-turn.transcript", canonicalSessionTranscript(string(transcript), sessionName),
	)

	resumeAppPlainTurn(t, directory, sessionName)
}

func TestAPrintedAppAnswersThroughItsOwnStartingPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{
		Model:        "claude-opus-5",
		Provider:     "anthropic",
		Effort:       "medium",
		SystemPrompt: sessionGoldenSystemPrompt,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New(sessionGoldenSystemPrompt, &plainTurnProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines).AppendOnly(),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.All()),
		runMode:  runMode{isPrinting: true},
	}

	self.begin("say something")

	requireNothingWasDrawnOver(t, screenOutput.String())

	drawn := style.Plain(screenOutput.String())
	if !strings.Contains(drawn, "say something") || !strings.Contains(drawn, "Said it.") {
		t.Errorf("a printed session drew %q", drawn)
	}
	if got := recordedUserMessages(self.recordedEvents); !slices.Equal(got, []string{"say something"}) {
		t.Errorf("got user messages %q", got)
	}
}

func resumeAppPlainTurn(t *testing.T, directory string, sessionName string) {
	t.Helper()

	storedSession, err := store.Read(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}

	resumedCaps, err := sessions.OpeningCaps(caps.All(), false, storedSession)
	if err != nil {
		t.Fatal(err)
	}

	log, err := store.Open(directory, sessionName)
	if err != nil {
		t.Fatal(err)
	}

	var screenOutput bytes.Buffer
	self := &App{
		agent:    agent.New(storedSession.Meta.SystemPrompt, &plainTurnProvider{}, nil),
		screen:   output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines),
		recorder: record.New(log),
		mode:     caps.NewMode(resumedCaps),
	}
	self.restore(storedSession)

	self.begin("carry on")

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	transcript, err := os.ReadFile(filepath.Join(directory, sessionName, "chat.md")) //nolint:gosec // the path is the test's own session directory
	if err != nil {
		t.Fatal(err)
	}

	compareScenarioGolden(t, "app-plain-resume.jsonl", canonicalSessionJournal(t, directory, sessionName))
	compareScenarioGolden(
		t, "app-plain-resume.transcript", canonicalSessionTranscript(string(transcript), sessionName),
	)
}

func TestTheStartupUsageProbeLeavesNoSessionBehind(t *testing.T) {
	var probes atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"usage":{"rolling":{"status":"ok","percent":6}}}`)
	}))
	defer server.Close()

	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{Model: "model", Provider: "opencode-go"})
	if err != nil {
		t.Fatal(err)
	}

	client, err := opencodego.New(server.URL, "token", "model", "medium", 1024)
	if err != nil {
		t.Fatal(err)
	}
	client.UsageURL = server.URL
	client.ObserveHTTP(log.Observer())

	cachePath := filepath.Join(t.TempDir(), "usage", "opencode-go.json")
	built, err := subUsage.New(subUsage.Settings{
		Reporter:         client,
		CachePath:        cachePath,
		ModelName:        "model",
		IsSelfRefreshing: true,
		Now:              time.Now,
	})(goldenSegmentOptions(""))
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for probes.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the usage probe was never made")
		}
		built.Render(segment.Context{})
		time.Sleep(time.Millisecond)
	}

	if log.IsPersisted() {
		t.Error("the usage probe wrote the session down before anything was said in it")
	}
	if warnings := log.TakeWarnings(); len(warnings) != 0 {
		t.Errorf("expected no recorder warnings, got %v", warnings)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("the usage probe left %s behind", entry.Name())
	}
}

func TestNoRecordedRequestMarksAThoughtForCaching(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "output", "*.requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no recorded requests")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			requests, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
			if err != nil {
				t.Fatal(err)
			}

			for line := range strings.SplitSeq(strings.TrimSpace(string(requests)), "\n") {
				var request any
				if err := json.Unmarshal([]byte(line), &request); err != nil {
					t.Fatal(err)
				}

				for _, thought := range markedThoughts(request) {
					t.Errorf("expected no cache breakpoint on a thought, got %v", thought)
				}
			}
		})
	}
}

func TestNoRecordedRequestRewritesWhatTheModelHasThoughtOver(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "output", "*.requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no recorded requests")
	}

	var examined int

	for _, path := range paths {
		requests := boundRequests(t, path)
		if len(requests) < 2 {
			continue
		}

		examined++

		t.Run(filepath.Base(path), func(t *testing.T) {
			for at := 1; at < len(requests); at++ {
				earlier, later := requests[at-1], requests[at]

				if earlier.system != later.system {
					t.Errorf("request %d rewrote the system prompt:\n%s\n%s", at, earlier.system, later.system)
				}
				if earlier.tools != later.tools {
					t.Errorf("request %d rewrote the tools:\n%s\n%s", at, earlier.tools, later.tools)
				}
				if len(earlier.bound) > len(later.messages) {
					t.Fatalf("request %d dropped %d messages", at, len(earlier.bound)-len(later.messages))
				}

				for position, sent := range earlier.bound {
					if later.messages[position] != sent {
						t.Errorf(
							"request %d rewrote message %d:\n%s\n%s",
							at, position, sent, later.messages[position],
						)
					}
				}
			}
		})
	}

	if examined == 0 {
		t.Fatal("no conversation was recorded over more than one request")
	}
}

type boundRequest struct {
	system   string
	tools    string
	messages []string
	bound    []string
}

func boundRequests(t *testing.T, path string) []boundRequest {
	t.Helper()

	recorded, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	var requests []boundRequest

	for line := range strings.SplitSeq(strings.TrimSpace(string(recorded)), "\n") {
		var body map[string]any
		if err := json.Unmarshal([]byte(line), &body); err != nil {
			t.Fatal(err)
		}

		if _, isThinking := body["thinking"]; !isThinking {
			continue
		}

		sentMessages, _ := body["messages"].([]any)
		request := boundRequest{
			system:   canonicalJSON(t, body["system"]),
			tools:    canonicalJSON(t, body["tools"]),
			messages: make([]string, 0, len(sentMessages)),
		}

		for _, sent := range sentMessages {
			request.messages = append(request.messages, canonicalJSON(t, sent))
			if turn, isTurn := sent.(map[string]any); isTurn && turn["role"] == "assistant" {
				request.bound = slices.Clone(request.messages)
			}
		}

		requests = append(requests, request)
	}

	return requests
}

func canonicalJSON(t *testing.T, node any) string {
	t.Helper()

	written, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}

	return string(written)
}

func markedThoughts(node any) []map[string]any {
	var found []map[string]any

	switch node := node.(type) {
	case map[string]any:
		_, isMarked := node["cache_control"]
		if isMarked && (node["type"] == "thinking" || node["type"] == "redacted_thinking") {
			found = append(found, node)
		}
		for _, child := range node {
			found = append(found, markedThoughts(child)...)
		}
	case []any:
		for _, child := range node {
			found = append(found, markedThoughts(child)...)
		}
	}

	return found
}

func TestEverySessionGoldenOpensWithTheModeItIsIn(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "output", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no session journals")
	}

	for _, path := range paths {
		if strings.HasSuffix(path, ".requests.jsonl") {
			continue
		}

		t.Run(filepath.Base(path), func(t *testing.T) {
			journal, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
			if err != nil {
				t.Fatal(err)
			}

			for line := range strings.SplitSeq(strings.TrimSpace(string(journal)), "\n") {
				var record struct {
					Kind  session.Kind `json:"kind"`
					Event *agent.Event `json:"event"`
				}
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal(err)
				}
				if record.Event == nil {
					continue
				}

				if record.Event.Kind != caps.ModeChange {
					t.Fatalf("expected the mode to be recorded first, got %q", record.Event.Kind)
				}

				return
			}

			t.Fatal("expected the session to record the mode it opened in")
		})
	}
}

func TestAModeChangeIsNotLostWhenItsQueuedTurnIsDropped(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	takeTestTurn := func(message string) {
		self.start(message)
		for report := range self.currentTurn.Events() {
			self.takeTurn(report)
		}
		self.finish()
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.cancelTurn(interrupt.Escape)
	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	if self.currentTurn.Running() {
		t.Fatal("expected the dropped mode change to have started no turn of its own")
	}

	takeTestTurn("second")

	if said := countModeNotes(provider.messages, nowReadOnlyNote); said != 1 {
		t.Errorf("expected the dropped mode change to be said once, said %d in %q", said, provider.messages)
	}
}

func TestAModeChangeIsNotLostWhenAMessageReplacesItsTurn(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	provider := &refusingOnceProvider{}
	self := &App{
		agent:    agent.New("", provider, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
	}

	self.start("first")
	self.toggleCap(caps.Write)
	self.replaceTurn("second")

	for self.currentTurn.Running() {
		for report := range self.currentTurn.Events() {
			self.takeTurn(report)
		}
		self.finish()
	}

	if said := countModeNotes(provider.messages, nowReadOnlyNote); said != 1 {
		t.Errorf("expected the replaced mode change to be said once, said %d in %q", said, provider.messages)
	}
}

const shortestFence = 3

var transcriptParser = goldmark.New().Parser()

func TestEveryTranscriptIsWellFormedMarkdown(t *testing.T) {
	for _, path := range everyTranscriptGolden(t) {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".transcript"), func(t *testing.T) {
			source := readTranscriptGolden(t, path)

			reportListItemsSpanningSeveralLines(t, source)
			reportBlocksTouchingTheOneBefore(t, source)
		})
	}
}

func everyTranscriptGolden(t *testing.T) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join("testdata", "output", "*.transcript"))
	if err != nil {
		t.Fatal(err)
	}

	if len(paths) == 0 {
		t.Fatal("expected the transcript goldens to be found")
	}

	return paths
}

func readTranscriptGolden(t *testing.T, path string) []byte {
	t.Helper()

	golden, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	var transcript []string

	for line := range strings.SplitSeq(string(golden), "\n") {
		if !strings.HasPrefix(line, "=== ") {
			transcript = append(transcript, line)
		}
	}

	return []byte(strings.Join(transcript, "\n"))
}

func reportListItemsSpanningSeveralLines(t *testing.T, source []byte) {
	t.Helper()

	document := transcriptParser.Parse(text.NewReader(source))

	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node.Kind() != ast.KindListItem {
			return ast.WalkContinue, nil
		}

		if item := itemText(node, source); strings.Contains(strings.TrimSuffix(item, "\n"), "\n") {
			t.Errorf("a field swallowed the block below it:\n%s", item)
		}

		return ast.WalkContinue, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func itemText(node ast.Node, source []byte) string {
	var item strings.Builder

	writeLines(&item, node, source)

	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		writeLines(&item, child, source)
	}

	return item.String()
}

func writeLines(item *strings.Builder, node ast.Node, source []byte) {
	lines := node.Lines()

	for line := range lines.Len() {
		segment := lines.At(line)
		item.Write(segment.Value(source))
	}
}

func reportBlocksTouchingTheOneBefore(t *testing.T, source []byte) {
	t.Helper()

	previous := ""
	openFence := ""

	for line := range strings.SplitSeq(string(source), "\n") {
		switch {
		case openFence != "":
			if backtickRun(line) >= len(openFence) && strings.TrimRight(line, "`") == "" {
				openFence = ""
			}
		case backtickRun(line) >= shortestFence:
			openFence = line[:backtickRun(line)]

			if previous != "" {
				t.Errorf("a fence opened straight after %q", previous)
			}
		case previous != "" && startsABlock(line, previous):
			t.Errorf("%q started straight after %q", line, previous)
		}

		previous = line
	}
}

func backtickRun(line string) int {
	return len(line) - len(strings.TrimLeft(line, "`"))
}

func startsABlock(line string, previous string) bool {
	trimmedPrevious := strings.TrimLeft(previous, " ")

	switch {
	case strings.HasPrefix(line, "#"), strings.HasPrefix(line, "**"):
		return true
	case strings.HasPrefix(line, ">"):
		return !strings.HasPrefix(trimmedPrevious, ">")
	case strings.HasPrefix(line, "- "):
		return !strings.HasPrefix(trimmedPrevious, "- ")
	default:
		return false
	}
}

type stoppableCommand struct {
	outcome chan sandbox.Result
}

func (self stoppableCommand) Wait() (sandbox.Result, error) { return <-self.outcome, nil }
func (self stoppableCommand) Stop()                         { self.outcome <- sandbox.Result{ExitCode: 137} }

func (self stoppableCommand) Signal(signal syscall.Signal) error {
	if signal == syscall.SIGTERM {
		self.outcome <- sandbox.Result{ExitCode: 143}
	}

	return nil
}

type stoppableRunner struct{}

func (stoppableRunner) Run(
	context.Context,
	string,
	string,
	sandbox.Policy,
) (sandbox.Result, error) {
	return sandbox.Result{}, nil
}

func (stoppableRunner) Start(
	context.Context,
	string,
	string,
	sandbox.Policy,
	sandbox.Output,
) (sandbox.Command, error) {
	return stoppableCommand{outcome: make(chan sandbox.Result, 2)}, nil
}

func appWithOneWritableJob(t *testing.T) *App {
	t.Helper()

	manager := jobs.New(stoppableRunner{})
	t.Cleanup(func() { _ = manager.Close() })

	log, err := store.Create(t.TempDir(), store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	self := &App{
		agent:     agent.New("", quietProvider{}, nil),
		screen:    output.New(&bytes.Buffer{}),
		recorder:  record.New(log),
		mode:      caps.NewMode(caps.Read | caps.Write),
		jobs:      jobState{manager: manager},
		workspace: work.At("/workspace"),
	}

	policy := sandbox.Policy{Write: []string{"/workspace"}}
	if _, err := manager.Start(t.Context(), "web", ".", "webd", policy); err != nil {
		t.Fatalf("the job did not start: %v", err)
	}

	return self
}

func TestAnEndedJobTellsTheModelWhatItPrintedWithoutBeingAsked(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.mode = caps.NewMode(caps.Read)

	self.jobEnded(jobs.Conclusion{
		Snapshot: jobs.Snapshot{Name: "build", State: jobs.StateFailed, ExitCode: 2},
		Output:   "undefined: getWidth\nexit status 1\n",
	})

	notices := self.pendingNotices.notices()
	if len(notices) != 1 {
		t.Fatalf("got %d notices, want the one job that ended: %v", len(notices), notices)
	}
	if !strings.Contains(notices[0], "undefined: getWidth") {
		t.Errorf("got %q, want what the job printed rather than an errand to fetch it", notices[0])
	}
	self.settleAccess()

	if note := self.takeSettledNotes(); note != notices[0] {
		t.Errorf("told the model %q, want the notice it drew: %q", note, notices[0])
	}
	if note := self.takeSettledNotes(); note != "" {
		t.Errorf("told the model %q twice, want it taken once", note)
	}
}

func TestAnEndedJobWaitsForSomeoneToSpeakWhereNobodyIsListening(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	completeTurn(self)

	self.jobEnded(endedJobConclusion())

	if self.currentTurn.Running() {
		t.Error("a printed conversation woke itself, want it waiting until someone speaks")
	}

	self.settleAccess()

	if note := self.takeSettledNotes(); note == "" {
		t.Error("the ended job said nothing, want it kept for whenever the conversation next runs")
	}
}

func TestAPendingNoticeIsMarkedSubmittedThoughTheHarnessDrewBesideIt(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	completeTurn(self)

	self.jobEnded(endedJobConclusion())
	self.screen.Line("the harness said something")
	self.settleAccess()
	self.screen.End()

	drawn := strings.Join(visibleScreen(t, screenOutput.String(), replayColumns), "\n")
	if strings.Contains(drawn, "⏳") {
		t.Errorf("the submitted notice still reads as unsent:\n%s", drawn)
	}
	if !strings.Contains(drawn, "🤖") {
		t.Errorf("the submitted notice was not marked as the harness speaking:\n%s", drawn)
	}
}

func tallTurn(t *testing.T, screenOutput *bytes.Buffer, lines int) (*App, *edit.Input) {
	t.Helper()

	self := testConversation(t, screenOutput)
	self.screen = output.NewTerminalOfSize(screenOutput, replayColumns, lines)
	self.settleAccess()

	history := edit.NewHistory("", historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine
	self.currentTurn = Turn{Stream: testRunningTurnStream(), painter: self.newPainter(true)}

	drawWithoutRepairing := func(event agent.Event) {
		self.recordedEvents = append(self.recordedEvents, event)
		self.currentTurn.painter.DrawEvent(event)
	}

	for i := range 30 {
		drawWithoutRepairing(agent.Event{
			Kind:      agent.ToolCallRequestEvent,
			ID:        "call-" + strconv.Itoa(i),
			Name:      "read",
			Arguments: fmt.Sprintf(`{"path":"file-%d.txt"}`, i),
		})
	}
	self.show(inputLine)

	drawWithoutRepairing(agent.Event{
		Kind:   agent.ToolCallResultEvent,
		ID:     "call-0",
		Name:   "read",
		Status: agent.SuccessStatus,
		Text:   "one\n",
	})

	return self, inputLine
}

func frozenTallTurn(t *testing.T, screenOutput *bytes.Buffer) (*App, *edit.Input) {
	t.Helper()

	self, inputLine := tallTurn(t, screenOutput, shortLines)
	if !self.screen.WasRepaintRefused() {
		t.Fatal("the region drew a change above its top row, want the refusal this covers")
	}

	return self, inputLine
}

const roomyLines = 60

func roomyTallTurn(t *testing.T, screenOutput *bytes.Buffer) (*App, *edit.Input) {
	t.Helper()

	self, inputLine := tallTurn(t, screenOutput, roomyLines)
	if self.screen.WasRepaintRefused() {
		t.Fatal("the region refused a repaint it had the room to draw")
	}

	return self, inputLine
}

func noticesDrawnBesideAnOpenBlock(self *App) map[string]func() {
	hostToSandbox, _ := portgrant.HostToSandboxChangeEvent("127.9.9.9", 8080, []portgrant.Route{{Port: 8080}})

	return map[string]func(){
		"an ended job": func() { self.jobEnded(endedJobConclusion()) },
		"a host command": func() {
			self.hostCommandRan(hostcommand.RanEvent(hostcommand.Result{Command: "git status", Output: " M a\n"}))
		},
		"an exposed port": func() { self.notify(hostToSandbox) },
	}
}

func TestEveryHarnessNoticeRepairsARegionFrozenByARefusedRepaint(t *testing.T) {
	for name := range noticesDrawnBesideAnOpenBlock(nil) {
		t.Run(name, func(t *testing.T) {
			var screenOutput bytes.Buffer
			self, inputLine := frozenTallTurn(t, &screenOutput)

			noticesDrawnBesideAnOpenBlock(self)[name]()
			self.show(inputLine)

			if self.screen.WasRepaintRefused() {
				t.Error("the notice left the region frozen rather than redrawing it")
			}

			drawn := strings.Join(visibleScreen(t, screenOutput.String(), replayColumns), "\n")
			if !strings.Contains(drawn, "🤖") {
				t.Errorf("the notice never reached the screen:\n%s", drawn)
			}
		})
	}
}

func TestAnEndedJobWakesTheConversationWithATurnOfItsOwn(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.jobs.doesWake = true
	completeTurn(self)

	self.jobEnded(endedJobConclusion())

	if !self.currentTurn.Running() {
		t.Fatal("the ended job left the conversation asleep, want a turn of its own")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	messages := submittedTexts(self.recordedEvents)
	if len(messages) == 0 || !strings.Contains(messages[len(messages)-1], "build") {
		t.Errorf("got messages %q, want the ended job named in the turn it woke", messages)
	}
	if note := self.takeSettledNotes(); note != "" {
		t.Errorf("told the model %q afterwards, want the waking turn to have taken it", note)
	}
}

func endedJobConclusion() jobs.Conclusion {
	return jobs.Conclusion{
		Snapshot: jobs.Snapshot{Name: "build", State: jobs.StateFailed, ExitCode: 2},
		Output:   "undefined: getWidth\nexit status 1\n",
	}
}

func TestAHostCommandWakesTheConversationWithATurnOfItsOwn(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	completeTurn(self)

	self.emitCommandEvent(hostCommandRun())

	if !self.currentTurn.Running() {
		t.Fatal("the command left the conversation asleep, want a turn of its own")
	}

	for report := range self.currentTurn.Events() {
		self.takeTurn(report)
	}
	self.finish()

	messages := submittedTexts(self.recordedEvents)
	if len(messages) == 0 || !strings.Contains(messages[len(messages)-1], "git status --short") {
		t.Errorf("got messages %q, want the command named in the turn it woke", messages)
	}
	if note := self.takeSettledNotes(); note != "" {
		t.Errorf("told the model %q afterwards, want the waking turn to have taken it", note)
	}
}

func TestAHostCommandRunDuringATurnJoinsItRatherThanStartingAnother(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.currentTurn = Turn{
		painter: self.newPainter(true),
		Stream:  testTurnStream(nil, func(error) {}, turn.State{Running: true}),
	}

	self.emitCommandEvent(hostCommandRun())

	if len(self.pendingNotices.items) != 0 {
		t.Errorf("got pending notices %+v, want the running turn to have taken it", self.pendingNotices.items)
	}
	if queued := self.currentTurn.GetInterjections(); len(queued) != 0 {
		t.Errorf("got queued messages %q, want a harness note rather than a message of the person's", queued)
	}
	note, isNoted := self.currentTurn.TakeNotes()
	if !isNoted || !strings.Contains(note, "git status --short") {
		t.Errorf("got note %q and %t, want the command the person ran", note, isNoted)
	}
}

func TestAHostCommandsOutputIsCappedTheWayAToolCallIs(t *testing.T) {
	limitedOutput := truncate.Output(
		strings.Repeat("every line of a very talkative command\n", 200),
		truncate.NewLimit(1024),
	)

	notice, isSaid := hostcommand.Notice(hostcommand.RanEvent(hostcommand.Result{
		Command: "find /",
		Output:  limitedOutput,
	}))
	if !isSaid {
		t.Fatal("expected the notice to be said")
	}
	if !strings.Contains(notice, "truncated at") {
		t.Errorf("got %q, want a talkative command capped the way a tool call is", notice)
	}
}

func hostCommandRun() agent.Event {
	return hostcommand.RanEvent(hostcommand.Result{
		Command: "git status --short",
		Output:  " M README.md\n?? notes.txt\n",
	})
}

func TestGoldenHostCommandNoticesMatchGolden(t *testing.T) {
	drawnAt := func(build func(*App)) func() string {
		return func() string {
			var screenOutput bytes.Buffer
			self := testConversation(t, &screenOutput)
			self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
			self.settleAccess()
			build(self)
			self.screen.End()

			return screenOutput.String()
		}
	}

	passes := map[string]func() string{
		"pending before the turn it wakes": drawnAt(func(self *App) {
			self.pendingNotices.add(hostCommandRun())
			self.refreshPendingMessages()
		}),
		"settled into the turn": drawnAt(func(self *App) {
			self.pendingNotices.add(hostCommandRun())
			self.refreshPendingMessages()
			self.settlePendingInput()
		}),
		"stopped in the turn it woke": drawnAt(func(self *App) {
			self.pendingNotices.add(hostCommandRun())
			self.refreshPendingMessages()
			self.settlePendingInput()
			self.currentTurn = Turn{painter: self.newPainter(true)}
			self.currentTurn.painter.DrawEvent(interrupt.Event(interrupt.ControlD))
		}),
		"printed nothing": drawnAt(func(self *App) {
			self.notify(hostcommand.RanEvent(hostcommand.Result{Command: "touch notes.txt"}))
		}),
		"exited with a failure": drawnAt(func(self *App) {
			self.notify(hostcommand.RanEvent(hostcommand.Result{
				Command:  "git push",
				Output:   "error: failed to push some refs\n",
				ExitCode: 1,
			}))
		}),
		"stopped at its limit": drawnAt(func(self *App) {
			self.notify(hostcommand.RanEvent(hostcommand.Result{
				Command:      "sleep 600",
				StoppedAfter: 30 * time.Second,
			}))
		}),
	}

	compareWithGolden(t, "host-command", ".ansi", passes)
	compareWithGolden(t, "host-command", ".screen", shownPasses(t, passes))
}

func TestGoldenADoubleReturnFlushesPendingNoticesWithoutTheContinueMessage(t *testing.T) {
	drawnAt := func(build func(*App), returns int) func() string {
		return func() string {
			var screenOutput bytes.Buffer
			self := testConversation(t, &screenOutput)
			self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
			self.continueMessage = "."
			self.settleAccess()

			history := edit.NewHistory("", historyLimit)
			inputLine := edit.NewInput(history)
			self.inputLine = inputLine

			build(self)
			self.show(inputLine)

			for range returns {
				self.handleKeypressAndShowInput(inputLine, history, key.Key{Code: key.Enter})
			}

			if self.currentTurn.Running() {
				for report := range self.currentTurn.Events() {
					self.takeTurn(report)
				}
				self.finish()
			}

			self.show(inputLine)
			self.screen.End()

			return screenOutput.String()
		}
	}

	restoreOneJob := func(self *App) {
		self.jobs = jobState{manager: jobs.New(nil)}
		self.restoreJobs([]agent.Event{
			jobrecord.ListingEvent([]jobs.Snapshot{{Name: "docs", Command: "python3 -m http.server", State: jobs.StateRunning}}),
		})
		self.refreshPendingMessages()
	}
	endOneJob := func(self *App) {
		self.jobs = jobState{manager: jobs.New(nil)}
		self.jobEnded(jobs.Conclusion{
			Snapshot: jobs.Snapshot{Name: "build", Command: "just build", State: jobs.StateFailed},
		})
	}
	withdrawWrites := func(self *App) { self.toggleCap(caps.Write) }
	stopAJobAndGrantWritesBack := func(self *App) {
		manager := jobs.New(stoppableRunner{})
		t.Cleanup(func() { _ = manager.Close() })
		self.jobs = jobState{manager: manager}
		self.workspace = work.At("/workspace")
		policy := sandbox.Policy{Write: []string{"/workspace"}}
		if _, err := manager.Start(t.Context(), "web", ".", "webd", policy); err != nil {
			t.Fatal(err)
		}
		self.toggleCap(caps.Write)
		self.toggleCap(caps.Write)
	}

	passes := map[string]func() string{
		"1 a restored job standing":       drawnAt(restoreOneJob, 0),
		"2 a restored job flushed":        drawnAt(restoreOneJob, 2),
		"3 an ended job standing":         drawnAt(endOneJob, 0),
		"4 an ended job flushed":          drawnAt(endOneJob, 2),
		"5 a mode change flushed":         drawnAt(withdrawWrites, 2),
		"6 nothing standing carries on":   drawnAt(func(*App) {}, 2),
		"7 a restored job and one return": drawnAt(restoreOneJob, 1),
		"8 a stopped job standing":        drawnAt(stopAJobAndGrantWritesBack, 0),
		"9 a stopped job flushed":         drawnAt(stopAJobAndGrantWritesBack, 2),
	}

	compareWithGolden(t, "pending-notices", ".ansi", passes)
	compareWithGolden(t, "pending-notices", ".screen", shownPasses(t, passes))
}

func TestAnEndedJobsOutputIsCappedTheWayAToolCallIs(t *testing.T) {
	self := &App{
		screen:          output.New(&bytes.Buffer{}),
		mode:            caps.NewMode(caps.Read),
		toolOutputLimit: truncate.NewLimit(1024),
	}

	self.jobEnded(jobs.Conclusion{
		Snapshot: jobs.Snapshot{Name: "build", State: jobs.StateComplete},
		Output:   strings.Repeat("every line of a very talkative job\n", 200),
	})

	notices := self.pendingNotices.notices()
	if len(notices) != 1 {
		t.Fatalf("got %d notices, want the one job that ended: %v", len(notices), notices)
	}
	if !strings.Contains(notices[0], "truncated at") {
		t.Errorf("got %q, want a talkative job capped the way a tool call is", notices[0])
	}
	if len(notices[0]) > 2048 {
		t.Errorf("the notice is %d bytes, want it capped near the tool output limit", len(notices[0]))
	}
}

func TestAWithdrawalIsAnnouncedBeforeTheJobsItStopped(t *testing.T) {
	self := appWithOneWritableJob(t)

	self.toggleCap(caps.Write)

	notices := self.pendingNotices.notices()
	if len(notices) != 2 {
		t.Fatalf("got %d notices, want the change and the job it stopped: %v", len(notices), notices)
	}
	if !strings.Contains(notices[0], "Workspace is now read-only") {
		t.Errorf("got %q first, want the change that caused the stop", notices[0])
	}
	if !strings.Contains(notices[1], "Job `web` stopped") {
		t.Errorf("got %q second, want the job it stopped", notices[1])
	}
}

func TestTheModelIsToldAboutTheJobsAWithdrawalStopped(t *testing.T) {
	self := appWithOneWritableJob(t)

	self.toggleCap(caps.Write)
	self.settleAccess()

	note := self.prelude()
	change := strings.Index(note, "Workspace is now read-only")
	stop := strings.Index(note, "Job `web` stopped")
	if change < 0 || stop < 0 {
		t.Fatalf("got note %q, want the change and the job it stopped", note)
	}
	if change > stop {
		t.Errorf("got note %q, want the change announced before the job it stopped", note)
	}
}

func TestTheModelIsToldAboutAStoppedJobEvenWhenTheCapabilityComesBack(t *testing.T) {
	self := appWithOneWritableJob(t)

	self.toggleCap(caps.Write)
	self.toggleCap(caps.Write)
	self.settleAccess()

	if note := self.prelude(); !strings.Contains(note, "Job `web` stopped") {
		t.Errorf("got note %q, want the job the withdrawal stopped", note)
	}
	if note := self.prelude(); note != "" {
		t.Errorf("got note %q, want the stop told once", note)
	}
}

func TestTwoReturnsWithAStoppedJobNoticeSubmitItRatherThanTheContinueMessage(t *testing.T) {
	self := appWithOneWritableJob(t)
	self.continueMessage = "carry on"

	self.toggleCap(caps.Write)
	self.toggleCap(caps.Write)

	if !self.hasUntoldPendingNotices() {
		t.Error("the stopped job would have gone out beside the continue message")
	}
}

func TestTheModelIsToldExactlyWhatTheScreenDrew(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.settleAccess()
	self.jobs = jobState{manager: jobs.New(nil)}

	self.restoreJobs([]agent.Event{
		jobrecord.ListingEvent([]jobs.Snapshot{{Name: "docs", Command: "python3", State: jobs.StateRunning}}),
	})
	self.jobEnded(endedJobConclusion())
	self.toggleCap(caps.Write)

	drawn := self.pendingNotices.notices()
	if len(drawn) != 3 {
		t.Fatalf("got %d notices drawn, want the restored job, the ended job and the change: %q", len(drawn), drawn)
	}

	self.settleAccess()

	if told := self.prelude(); told != strings.Join(drawn, noticeSeparator) {
		t.Errorf("the model was told\n%q\nwhile the screen drew\n%q", told, drawn)
	}
}

func TestAHostCommandLeavesNothingStandingBecauseItWakesItsOwnTurn(t *testing.T) {
	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	self.settleAccess()

	self.hostCommandRan(hostcommand.RanEvent(hostcommand.Result{Command: "git status", Output: " M README.md\n"}))

	if !self.currentTurn.Running() {
		t.Fatal("the command left the conversation asleep, want a turn of its own")
	}
	if len(self.settledNotes) != 0 || len(self.pendingNotices.items) != 0 {
		t.Errorf(
			"got %d notes and %d notices standing, want the turn it woke to have taken both",
			len(self.settledNotes), len(self.pendingNotices.items),
		)
	}
}

func TestAStoppedJobIsToldWhenThePathItHeldIsRevoked(t *testing.T) {
	self := appWithOneWritableJob(t)

	self.stopJobsHoldingPath("/workspace")
	self.settleAccess()

	if note := self.prelude(); !strings.Contains(note, "Job `web` stopped") {
		t.Errorf("got note %q, want the job the revoked path stopped", note)
	}
}

func TestGrantingACapabilityBackStopsNothing(t *testing.T) {
	self := appWithOneWritableJob(t)

	self.toggleCap(caps.Write)
	self.toggleCap(caps.Write)

	for _, notice := range self.pendingNotices.notices() {
		if strings.Contains(notice, "read-write") && strings.Contains(notice, "stopped") {
			t.Errorf("granting a capability stopped a job: %q", notice)
		}
	}
}

func TestAModeChangeThatStoppedAJobIsNotTakenBack(t *testing.T) {
	self := appWithOneWritableJob(t)

	self.toggleCap(caps.Write)
	self.toggleCap(caps.Write)

	notices := self.pendingNotices.notices()
	if len(notices) != 3 {
		t.Fatalf("got %d notices, want both changes and the stop: %v", len(notices), notices)
	}
	if !strings.Contains(notices[2], "Workspace is now read-write") {
		t.Errorf("got %q last, want the capability being granted again", notices[2])
	}
}

func TestAJobStoppedByAnotherCapabilityHoldsNothingBack(t *testing.T) {
	self := &App{
		screen: output.New(&bytes.Buffer{}),
		mode:   caps.NewMode(caps.Read | caps.Write | caps.Shell),
	}

	self.toggleCap(caps.Write)
	self.pendingNotices.add(caps.JobStopEvent("web", caps.Shell))
	self.toggleCap(caps.Write)

	notices := self.pendingNotices.notices()
	if len(notices) != 1 {
		t.Fatalf(
			"got %d notices, want only the job the shell withdrawal stopped: %v",
			len(notices), notices,
		)
	}
	if !strings.Contains(notices[0], "Job `web` stopped") {
		t.Errorf("got %q, want the stopped job left standing on its own", notices[0])
	}
}

func TestTogglingOnPastAStoppedJobStillSubtracts(t *testing.T) {
	self := appWithOneWritableJob(t)

	self.toggleCap(caps.Write)

	for step, wantCount := range []int{3, 2, 3, 2, 3, 2} {
		self.toggleCap(caps.Write)

		notices := self.pendingNotices.notices()
		if len(notices) != wantCount {
			t.Fatalf(
				"after %d further toggles got %d notices, want %d: %v",
				step+1, len(notices), wantCount, notices,
			)
		}
	}

	notices := self.pendingNotices.notices()
	if !strings.Contains(notices[0], "Workspace is now read-only") {
		t.Errorf("got %q first, want the change that stopped the job", notices[0])
	}
	if !strings.Contains(notices[1], "Job `web` stopped") {
		t.Errorf("got %q second, want the job it stopped", notices[1])
	}
}

func TestAModeChangeThatStoppedNothingIsStillTakenBack(t *testing.T) {
	self := &App{screen: output.New(&bytes.Buffer{}), mode: caps.NewMode(caps.Read | caps.Write)}

	self.toggleCap(caps.Write)
	self.toggleCap(caps.Write)

	if len(self.pendingNotices.items) != 0 {
		t.Errorf("a mode change with no consequence was not taken back: %v", self.pendingNotices.items)
	}
}

func TestRemovingTheLastJobIsRecordedSoAResumeDoesNotBringItBack(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	manager := jobs.New(nil)
	manager.Restore([]jobs.Snapshot{{Name: "docs", Command: "python3", State: jobs.StateFailed}})

	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
		jobs:     jobState{manager: manager},
	}

	self.recordJobListing()
	manager.PruneFinished()
	self.recordJobListing()

	restored, wasRecorded := jobrecord.LastRecorded(self.recordedEvents)
	if !wasRecorded {
		t.Fatal("nothing was recorded at all")
	}
	if len(restored) != 0 {
		t.Errorf("got %#v, want the emptied listing recorded so a resume restores nothing", restored)
	}
}

func TestPruningRightAfterAResumeIsStillRecorded(t *testing.T) {
	directory := t.TempDir()
	log, err := store.Create(directory, store.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()

	manager := jobs.New(nil)
	self := &App{
		agent:    agent.New("", quietProvider{}, nil),
		screen:   output.New(&bytes.Buffer{}),
		recorder: record.New(log),
		mode:     caps.NewMode(caps.Read | caps.Write),
		jobs:     jobState{manager: manager},
	}

	self.restoreJobs([]agent.Event{
		jobrecord.ListingEvent([]jobs.Snapshot{{Name: "docs", Command: "python3", State: jobs.StateFailed}}),
	})
	manager.PruneFinished()
	self.recordJobListing()

	restored, wasRecorded := jobrecord.LastRecorded(self.recordedEvents)
	if !wasRecorded || len(restored) != 0 {
		t.Errorf("got %#v (recorded %v), want the emptied listing recorded", restored, wasRecorded)
	}
}

func drawnPNGFor(t *testing.T, width int, height int) []byte {
	t.Helper()

	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}

	return encoded.Bytes()
}

func storedPictureFor(t *testing.T, width int, height int) (string, *agent.Picture) {
	t.Helper()

	sessionDirectory := filepath.Join(t.TempDir(), "tame-impala")

	reference, err := pictures.Store(
		sessionDirectory,
		func() error { return os.MkdirAll(sessionDirectory, 0o700) },
		tool.Image{MediaType: "image/png", Data: drawnPNGFor(t, width, height)},
	)
	if err != nil {
		t.Fatal(err)
	}

	return sessionDirectory, reference
}

func pictureCallEvents(reference *agent.Picture) []agent.Event {
	return pictureCallEventsFor("call-1", "screenshot.png", reference)
}

func pictureCallEventsFor(id string, path string, reference *agent.Picture) []agent.Event {
	return []agent.Event{
		{
			Kind:      agent.ToolCallRequestEvent,
			ID:        id,
			Name:      "read",
			Arguments: fmt.Sprintf(`{"path":%q}`, path),
		},
		{
			Kind:    agent.ToolCallResultEvent,
			ID:      id,
			Name:    "read",
			Text:    "image/png image (4096 bytes)",
			Status:  agent.SuccessStatus,
			Picture: reference,
		},
	}
}

func paintPictureEvents(t *testing.T, sessionDirectory string, columns int, events []agent.Event) string {
	t.Helper()

	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, columns, replayLines)
	picasso := newTestPainter(screen, false)
	picasso.DrawPicturesFrom(goldenPictureDisplay(sessionDirectory, false))

	for _, event := range events {
		picasso.DrawEvent(event)
	}

	picasso.Close(dynamic.Done)
	screen.Seal()

	return screenOutput.String()
}

func pictureAmongCallsStream(t *testing.T) string {
	t.Helper()

	sessionDirectory, reference := storedPictureFor(t, 400, 200)

	events := []agent.Event{
		{Kind: agent.ToolCallRequestEvent, ID: "before", Name: "ls", Arguments: `{"path":"."}`},
		{Kind: agent.ToolCallRequestEvent, ID: "call-1", Name: "read", Arguments: `{"path":"screenshot.png"}`},
		{Kind: agent.ToolCallRequestEvent, ID: "after", Name: "grep", Arguments: `{"pattern":"x"}`},
		{Kind: agent.ToolCallResultEvent, ID: "before", Name: "ls", Text: "3 entries", Status: agent.SuccessStatus},
		{
			Kind: agent.ToolCallResultEvent, ID: "call-1", Name: "read",
			Text: "image/png image (4096 bytes)", Status: agent.SuccessStatus, Picture: reference,
		},
		{Kind: agent.ToolCallResultEvent, ID: "after", Name: "grep", Text: "1 match", Status: agent.SuccessStatus},
	}

	return paintPictureEvents(t, sessionDirectory, replayColumns, events)
}

func twoPicturesStream(t *testing.T) string {
	t.Helper()

	sessionDirectory, first := storedPictureFor(t, 400, 200)
	second := storedPictureIn(t, sessionDirectory, 200, 200)

	events := slices.Concat(
		pictureCallEventsFor("call-1", "wide.png", first),
		pictureCallEventsFor("call-2", "square.png", second),
	)

	return paintPictureEvents(t, sessionDirectory, replayColumns, events)
}

func storedPictureIn(t *testing.T, sessionDirectory string, width int, height int) *agent.Picture {
	t.Helper()

	reference, err := pictures.Store(
		sessionDirectory,
		func() error { return os.MkdirAll(sessionDirectory, 0o700) },
		tool.Image{MediaType: "image/png", Data: drawnPNGFor(t, width, height)},
	)
	if err != nil {
		t.Fatal(err)
	}

	return reference
}

func unorderedPicturesStream(t *testing.T) string {
	t.Helper()

	sessionDirectory, first := storedPictureFor(t, 800, 600)
	second := storedPictureIn(t, sessionDirectory, 700, 500)
	third := storedPictureIn(t, sessionDirectory, 600, 450)

	requests, results := pictureRoundEvents(map[string]*agent.Picture{
		"call-1": first,
		"call-2": second,
		"call-3": third,
	})

	return paintPictureEvents(t, sessionDirectory, replayColumns, slices.Concat(requests, results))
}

func pictureRoundEvents(references map[string]*agent.Picture) ([]agent.Event, []agent.Event) {
	var requests []agent.Event
	var results []agent.Event

	for _, id := range []string{"call-1", "call-2", "call-3"} {
		requests = append(requests, pictureCallEventsFor(id, id+".png", references[id])[0])
	}

	for _, id := range []string{"call-3", "call-1", "call-2"} {
		results = append(results, pictureCallEventsFor(id, id+".png", references[id])[1])
	}

	return requests, results
}

func narrowPictureStream(t *testing.T) string {
	t.Helper()

	sessionDirectory, reference := storedPictureFor(t, 400, 200)

	return paintPictureEvents(t, sessionDirectory, 20, pictureCallEvents(reference))
}

func tallPictureStream(t *testing.T) string {
	t.Helper()

	sessionDirectory, reference := storedPictureFor(t, 100, 4000)

	return paintPictureEvents(t, sessionDirectory, replayColumns, pictureCallEvents(reference))
}

func pictureStream(t *testing.T, isLocal bool, repaints int, isPictureMissing bool) string {
	t.Helper()

	sessionDirectory, reference := storedPictureFor(t, 400, 200)
	if isPictureMissing {
		if err := os.RemoveAll(pictures.GetDirectory(sessionDirectory)); err != nil {
			t.Fatal(err)
		}
	}

	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	picasso := newTestPainter(screen, false)
	picasso.DrawPicturesFrom(goldenPictureDisplay(sessionDirectory, isLocal))

	for _, event := range pictureCallEvents(reference) {
		picasso.DrawEvent(event)
	}

	for range repaints {
		screen.Refresh()
	}

	picasso.Close(dynamic.Done)
	screen.Seal()

	return screenOutput.String()
}

func goldenPictureDisplay(directory string, isLocal bool) pictures.Display {
	return pictures.Display{
		SessionDirectory: directory,
		CellWidth:        10,
		CellHeight:       20,
		IsLocal:          isLocal,
	}
}

func scratchedAnswerPictureStream(t *testing.T) string {
	t.Helper()

	directory := answerPictureDirectory(t)

	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	picasso := painter.New(screen, false, nil, work.At(t.TempDir()), defaultStreamingMode)

	display := goldenPictureDisplay(directory, false)
	display.ScratchDirectory = directory
	picasso.DrawPicturesFrom(display)

	picasso.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answerNaming("/tmp/chart.png")})
	screen.Seal()

	return screenOutput.String()
}

func answerPictureDirectory(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	writeAnswerPicture(t, directory, "chart.png", 400, 200)

	return directory
}

func writeAnswerPicture(t *testing.T, directory string, name string, pictureWidth int, pictureHeight int) {
	t.Helper()

	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, drawnPNGFor(t, pictureWidth, pictureHeight), 0o600); err != nil {
		t.Fatal(err)
	}
}

func twoAnswerPicturesStream(t *testing.T) string {
	t.Helper()

	directory := answerPictureDirectory(t)
	writeAnswerPicture(t, directory, "square.png", 200, 200)

	var screenOutput strings.Builder
	picasso, screen := newAnswerPicturePainter(t, &screenOutput, directory, false)

	picasso.DrawEvent(agent.Event{
		Kind: agent.ModelMessageEvent,
		Text: "**Zoomed**\n\n![](chart.png)\n\n**Not zoomed**\n\n![](square.png)\n\nSame settings either way.",
	})
	screen.Seal()

	return screenOutput.String()
}

func answerNaming(path string) string {
	return "Here is the shape of it:\n\n![](" + path + ")\n\nThat is all the evidence there is."
}

func newAnswerPicturePainter(
	t *testing.T,
	screenOutput *strings.Builder,
	directory string,
	isLocal bool,
) (*Painter, *output.Screen) {
	t.Helper()

	screen := output.NewTerminalOfSize(screenOutput, replayColumns, replayLines)

	return newAnswerPicturePainterOn(t, screen, directory, isLocal), screen
}

func newAnswerPicturePainterOn(
	t *testing.T,
	screen *output.Screen,
	directory string,
	isLocal bool,
) *Painter {
	t.Helper()

	picasso := painter.New(screen, false, nil, work.At(directory), defaultStreamingMode)
	picasso.DrawPicturesFrom(goldenPictureDisplay(directory, isLocal))

	return picasso
}

func quotedAnswerPictureStream(t *testing.T) string {
	t.Helper()

	var screenOutput strings.Builder
	picasso, screen := newAnswerPicturePainter(t, &screenOutput, answerPictureDirectory(t), false)

	picasso.DrawEvent(agent.Event{
		Kind: agent.ModelMessageEvent,
		Text: "It said:\n\n> ![](chart.png)\n>\n> and nothing else",
	})
	screen.Seal()

	return screenOutput.String()
}

func appendedAnswerPictureStream(t *testing.T) string {
	t.Helper()

	directory := answerPictureDirectory(t)

	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines).AppendOnly()
	picasso := newAnswerPicturePainterOn(t, screen, directory, false)

	picasso.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answerNaming("chart.png")})
	screen.Seal()

	return screenOutput.String()
}

func answerPictureStream(t *testing.T, isLocal bool) string {
	t.Helper()

	var screenOutput strings.Builder
	picasso, screen := newAnswerPicturePainter(t, &screenOutput, answerPictureDirectory(t), isLocal)

	picasso.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answerNaming("chart.png")})
	screen.Seal()

	return screenOutput.String()
}

func streamedAnswerPictureStream(t *testing.T) string {
	t.Helper()

	answer := answerNaming("chart.png")

	var screenOutput strings.Builder
	picasso, screen := newAnswerPicturePainter(t, &screenOutput, answerPictureDirectory(t), false)

	for _, delta := range deltas(answer, 10) {
		picasso.DrawDelta(agent.Delta{Kind: agent.ModelMessageEvent, Text: delta})
	}

	picasso.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answer})
	screen.Seal()

	return screenOutput.String()
}

func listedAnswerPictureStream(t *testing.T) string {
	t.Helper()

	var screenOutput strings.Builder
	picasso, screen := newAnswerPicturePainter(t, &screenOutput, answerPictureDirectory(t), false)

	picasso.DrawEvent(agent.Event{
		Kind: agent.ModelMessageEvent,
		Text: "Both of them:\n\n- ![](chart.png)\n- and a word about it",
	})
	screen.Seal()

	return screenOutput.String()
}

func writtenAnswerPictureStream(t *testing.T, answer string, isDrawingPictures bool) string {
	t.Helper()

	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	picasso := newTestPainter(screen, false)

	if isDrawingPictures {
		picasso.DrawPicturesFrom(goldenPictureDisplay(t.TempDir(), false))
	}

	picasso.DrawEvent(agent.Event{Kind: agent.ModelMessageEvent, Text: answer})
	screen.Seal()

	return screenOutput.String()
}

func pictureWithoutGraphicsStream(t *testing.T) string {
	t.Helper()

	_, reference := storedPictureFor(t, 400, 200)

	var screenOutput strings.Builder
	screen := output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
	picasso := newTestPainter(screen, false)

	for _, event := range pictureCallEvents(reference) {
		picasso.DrawEvent(event)
	}

	picasso.Close(dynamic.Done)
	screen.Seal()

	return screenOutput.String()
}

var (
	pictureIdentifier   = regexp.MustCompile(`i=\d+`)
	pictureColour       = regexp.MustCompile(`\x1b\[38;2;\d+;\d+;\d+m\x{10EEEE}`)
	pictureData         = regexp.MustCompile(`(\x1b_G[^;]*t=d,[^;]*,m=)[01];[A-Za-z0-9+/=]+\x1b\\(?:\x1b_Gm=[01];[A-Za-z0-9+/=]+\x1b\\)*`)
	picturePath         = regexp.MustCompile(`(\x1b_G[^;]*t=f,[^;]*,m=)[01];[A-Za-z0-9+/=]+\x1b\\(?:\x1b_Gm=[01];[A-Za-z0-9+/=]+\x1b\\)*`)
	pictureCommandClose = "\x1b\\"
)

func anonymisePictures(stream string) string {
	stream = pictureIdentifier.ReplaceAllString(stream, "i=<identifier>")
	stream = pictureData.ReplaceAllString(stream, "${1}0;<data>"+pictureCommandClose)
	stream = picturePath.ReplaceAllString(stream, "${1}0;<path>"+pictureCommandClose)

	return pictureColour.ReplaceAllString(stream, "<picture>\U0010EEEE")
}

func TestPictureAnonymisationIgnoresPayloadEncodingAndChunking(t *testing.T) {
	chunked := "\x1b_Ga=T,t=d,i=123,m=1;AAAA\x1b\\" +
		"\x1b_Gm=1;BBBB\x1b\\" +
		"\x1b_Gm=0;CCCC\x1b\\" +
		"\x1b_Ga=T,t=f,i=456,m=0;DDDD\x1b\\"
	unchunked := "\x1b_Ga=T,t=d,i=789,m=0;EEEE\x1b\\" +
		"\x1b_Ga=T,t=f,i=987,m=0;FFFF\x1b\\"
	want := "\x1b_Ga=T,t=d,i=<identifier>,m=0;<data>\x1b\\" +
		"\x1b_Ga=T,t=f,i=<identifier>,m=0;<path>\x1b\\"

	for name, stream := range map[string]string{"chunked": chunked, "unchunked": unchunked} {
		t.Run(name, func(t *testing.T) {
			if got := anonymisePictures(stream); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func rebuiltPictureStream(t *testing.T) string {
	t.Helper()

	sessionDirectory, reference := storedPictureFor(t, 400, 200)

	drawing, isStored := pictures.Prepare(sessionDirectory, reference)
	if !isStored {
		t.Fatal("the drawn copy was never made")
	}
	if err := os.Remove(drawing.Path); err != nil {
		t.Fatal(err)
	}

	return paintPictureEvents(t, sessionDirectory, replayColumns, pictureCallEvents(reference))
}

func resizedPictureRows(t *testing.T) string {
	t.Helper()

	block := dynamic.NewBlock(func() {})
	t.Cleanup(block.Stop)

	index := block.Add(call.LabelFor(agent.Event{
		Kind:      agent.ToolCallRequestEvent,
		ID:        "call-1",
		Name:      "read",
		Arguments: `{"path":"screenshot.png"}`,
	}, nil, nil), 0)

	block.AttachPicture(index, dynamic.Picture{
		Data:       drawnPNGFor(t, 400, 200),
		Width:      400,
		Height:     200,
		CellWidth:  10,
		CellHeight: 20,
	})

	var drawn strings.Builder
	for _, columns := range []int{60, 30, 60} {
		fmt.Fprintf(&drawn, "--- at %d columns ---\n", columns)
		for _, row := range block.Rows(columns) {
			drawn.WriteString(row)
			drawn.WriteString("\n")
		}
	}

	return drawn.String()
}

func TestAPictureRedrawnAtTheSameWidthIsNeverSentTwice(t *testing.T) {
	sessionDirectory, reference := storedPictureFor(t, 400, 200)

	live := anonymisePictures(paintPictureEvents(t, sessionDirectory, replayColumns, pictureCallEvents(reference)))
	replayed := anonymisePictures(paintPictureEvents(t, sessionDirectory, replayColumns, pictureCallEvents(reference)))

	if live != replayed {
		t.Error("a replayed picture drew something other than the live one")
	}
}

func TestAPictureNamedInAnAnswerIsDrawnTheSameStreamedAsReplayed(t *testing.T) {
	requireSameVisibleScreen(
		t,
		"a streamed answer drew its picture differently from the replayed one",
		anonymisePictures(streamedAnswerPictureStream(t)),
		anonymisePictures(answerPictureStream(t, false)),
	)
}

func TestAPictureNamedInAnAnswerIsSentOnlyOnceWhileTheAnswerArrives(t *testing.T) {
	if sent := strings.Count(streamedAnswerPictureStream(t), "f=100"); sent != 1 {
		t.Errorf("the picture was transmitted %d times while the answer arrived, want once", sent)
	}
}

func TestGoldenAPictureIsDrawnUnderTheCallThatReadIt(t *testing.T) {
	passes := map[string]func() string{
		"1 a picture sent to the terminal": func() string {
			return pictureStream(t, false, 0, false)
		},
		"2 a picture painted a second time": func() string {
			return pictureStream(t, false, 1, false)
		},
		"3 a picture sent by path": func() string {
			return pictureStream(t, true, 0, false)
		},
		"4 a picture no longer stored": func() string {
			return pictureStream(t, false, 0, true)
		},
		"5 a terminal that draws no pictures": func() string {
			return pictureWithoutGraphicsStream(t)
		},
		"6 a picture among other calls": func() string {
			return pictureAmongCallsStream(t)
		},
		"7 two pictures in one block": func() string {
			return twoPicturesStream(t)
		},
		"8 a picture wider than the screen": func() string {
			return narrowPictureStream(t)
		},
		"9 a picture taller than a placement allows": func() string {
			return tallPictureStream(t)
		},
		"a1 a picture laid out again for a new width": func() string {
			return resizedPictureRows(t)
		},
		"a2 a drawn copy rebuilt from the original": func() string {
			return rebuiltPictureStream(t)
		},
		"a3 pictures whose calls finish out of order": func() string {
			return unorderedPicturesStream(t)
		},
		"a4 a picture named in an answer": func() string {
			return answerPictureStream(t, false)
		},
		"a5 a picture named in an answer sent by path": func() string {
			return answerPictureStream(t, true)
		},
		"a6 an answer naming a picture that is not there": func() string {
			return writtenAnswerPictureStream(t, answerNaming("/pictures/absent.png"), true)
		},
		"a7 a picture named in an answer as it arrives": func() string {
			return streamedAnswerPictureStream(t)
		},
		"a8 a picture named in an answer in a list": func() string {
			return listedAnswerPictureStream(t)
		},
		"a9 a picture named in an answer beside prose": func() string {
			return writtenAnswerPictureStream(t, "Look at ![](/pictures/chart.png) closely.", true)
		},
		"b1 a picture named for a terminal that draws no pictures": func() string {
			return writtenAnswerPictureStream(t, answerNaming("/pictures/chart.png"), false)
		},
		"b2 a picture named in an answer in a quote": func() string {
			return quotedAnswerPictureStream(t)
		},
		"b3 a picture named in an answer only appended": func() string {
			return appendedAnswerPictureStream(t)
		},
		"b4 two pictures named in one answer": func() string {
			return twoAnswerPicturesStream(t)
		},
		"b5 a picture named under the scratch the model sees as /tmp": func() string {
			return scratchedAnswerPictureStream(t)
		},
		"b6 a picture named under /tmp that the scratch does not hold": func() string {
			return writtenAnswerPictureStream(t, answerNaming("/tmp/absent.png"), true)
		},
	}

	compareWithGolden(t, "pictures", ".ansi", passes)
	compareWithGolden(t, "pictures", ".screen", shownPasses(t, passes))
}

func TestAnAllowedPermissionAsksNobody(t *testing.T) {
	broker := ask.New()
	closeBroker := broker.Open()
	defer closeBroker()

	go func() {
		<-broker.Changes()
		t.Error("an allowed permission asked anyway")
	}()

	for name, ask := range map[string]func() error{
		"network": func() error {
			return approveHostNetwork(t.Context(), broker, permission.Allow, "curl example.com")
		},
		"lookup": func() error {
			return lookupApproval.ask(t.Context(), broker, permission.Allow, "weather")
		},
		"fetch": func() error {
			return fetchApproval.ask(t.Context(), broker, permission.Allow, "https://example.com")
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ask(); err != nil {
				t.Errorf("got %v, want an allowed permission to pass straight through", err)
			}
		})
	}
}

func TestEveryPermissionRefusesInWordsOfItsOwn(t *testing.T) {
	for name, test := range map[string]struct {
		ask  func(broker *ask.Broker) error
		want string
	}{
		"network": {
			ask: func(broker *ask.Broker) error {
				return approveHostNetwork(t.Context(), broker, permission.Ask, "curl example.com")
			},
			want: "command did not run",
		},
		"lookup": {
			ask: func(broker *ask.Broker) error {
				return lookupApproval.ask(t.Context(), broker, permission.Ask, "weather")
			},
			want: "lookup did not run",
		},
		"fetch": {
			ask: func(broker *ask.Broker) error {
				return fetchApproval.ask(t.Context(), broker, permission.Ask, "https://example.com")
			},
			want: "fetch did not run",
		},
	} {
		t.Run(name, func(t *testing.T) {
			broker := ask.New()
			closeBroker := broker.Open()
			defer closeBroker()

			go func() {
				<-broker.Changes()
				broker.Current().Choose(1)
			}()

			err := test.ask(broker)
			if err == nil {
				t.Fatal("a refused permission was allowed")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("got %q, want it to say %q", err, test.want)
			}
		})
	}
}

func TestYoloWarnsOnceWhenSandboxDenyRulesCanBeBypassed(t *testing.T) {
	for _, test := range []struct {
		tools []string
		want  string
	}{
		{want: "warning: sandbox.deny cannot be enforced for the bash and job tools under --yolo\n"},
		{tools: []string{"read", "bash"}, want: "warning: sandbox.deny cannot be enforced for the bash tool under --yolo\n"},
		{tools: []string{"job"}, want: "warning: sandbox.deny cannot be enforced for the job tool under --yolo\n"},
	} {
		var warnings strings.Builder
		warnAboutDenyEnforcement(true, test.tools, []string{"foo.txt"}, &warnings)
		if style.Plain(warnings.String()) != test.want {
			t.Errorf("tools %v: got %q, want %q", test.tools, warnings.String(), test.want)
		}
	}
}

func TestSandboxDenyRulesDoNotWarnWhenTheyStillApply(t *testing.T) {
	for _, test := range []struct {
		isYolo bool
		tools  []string
		deny   []string
	}{
		{isYolo: false, deny: []string{"foo.txt"}},
		{isYolo: true},
		{isYolo: true, tools: []string{"read", "ls", "grep"}, deny: []string{"foo.txt"}},
		{isYolo: true, tools: []string{"write"}, deny: []string{"foo.txt"}},
		{isYolo: true, tools: demo.Tools(), deny: []string{"foo.txt"}},
	} {
		var warnings strings.Builder
		warnAboutDenyEnforcement(test.isYolo, test.tools, test.deny, &warnings)
		if warnings.Len() != 0 {
			t.Errorf("got an inapplicable warning for %+v: %q", test, warnings.String())
		}
	}
}

func TestASimulationRunsWithNoSandboxAndNothingThatCouldUseOne(t *testing.T) {
	options := cli.Options{Caps: caps.All()}
	applySimulationOptions(&options)

	if !options.Yolo {
		t.Error("a simulation asked for a sandbox it does not need")
	}
	if want := []string{"read", "ls", "grep"}; !slices.Equal(options.Tools, want) {
		t.Errorf("a simulation offered %v, want %v", options.Tools, want)
	}

	for _, refused := range []string{"bash", "job", "write", "edit", "expose"} {
		if slices.Contains(options.Tools, refused) {
			t.Errorf("a simulation offered the %s tool with no sandbox around it", refused)
		}
	}
}

func TestASimulationDrawsNoHazard(t *testing.T) {
	simulated := &App{runMode: runMode{isYolo: true, isSimulated: true}}
	if got := simulated.ruleStyle()("│"); got != style.Rule("│") {
		t.Errorf("a simulation drew its rule as %q, want the ordinary rule", got)
	}

	unconfined := &App{runMode: runMode{isYolo: true}}
	if got := unconfined.ruleStyle()("│"); got != style.Hazard("│") {
		t.Errorf("an unconfined conversation drew its rule as %q, want the hazard rule", got)
	}
}

func writeDeclaredToolConfig(t *testing.T, environment []string, body string) {
	t.Helper()

	var configHome string
	for _, entry := range environment {
		if value, isConfigHome := strings.CutPrefix(entry, "XDG_CONFIG_HOME="); isConfigHome {
			configHome = value
		}
	}
	if configHome == "" {
		t.Fatal("the test environment names no config home")
	}

	directory := filepath.Join(configHome, "org.crdx", "oh")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf("version = %d\n\n%s", config.Format, body)
	if err := os.WriteFile(filepath.Join(directory, "config.toml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func declaredToolScript(t *testing.T) string {
	t.Helper()

	path := filepath.Join(reachableWorkspaceDir(t), "forecast")
	//nolint:gosec // a tool the test declares has to be runnable
	if err := os.WriteFile(path, []byte("#!/bin/bash\necho \"forecast for $*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestADeclaredToolRunsItsOwnCommandAndReportsWhatItSaid(t *testing.T) {
	binary := buildTestBinary(t)
	script := declaredToolScript(t)
	endpoint := sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"London","days":2}`}}},
			{Say: "Rain, then."},
		},
	})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]
	environment := append(testBinaryEnvironment(t, t.TempDir()), backend.EndpointVariable+"="+address)
	writeDeclaredToolConfig(t, environment, `[tools.weather]
description = "report the weather for a city"
command = ["`+script+`"]
permission = "allow"
parameters = [
    { name = "city", kind = "string", description = "the city to report on" },
    { name = "days", kind = "integer", description = "how many days ahead to look", optional = true },
]
`)

	output := runTestBinary(
		t, binary, reachableWorkspaceDir(t), environment,
		"-p", "--yolo", "-m", "anthropic/fake", "what is the weather",
	)

	if !strings.Contains(output, "weather London 2") {
		t.Errorf("the declared call was not drawn: %q", output)
	}
	if !strings.Contains(output, "Rain, then.") {
		t.Errorf("the answer did not follow the tool: %q", output)
	}

	requests := endpoint.Requests()
	if len(requests) < 2 {
		t.Fatalf("the endpoint saw %d requests", len(requests))
	}
	if !slices.Contains(requests[0].Tools, "weather") {
		t.Errorf("the declared tool was not offered: %q", requests[0].Tools)
	}

	var reported string
	for _, entry := range requests[1].Input {
		if entry.Type == sim.CallOutput {
			reported = entry.Output
		}
	}
	if reported != "forecast for --city London --days 2" {
		t.Errorf("the model was told %q", reported)
	}
}

func TestADeclaredToolNobodyCanBeAskedAboutDoesNotRun(t *testing.T) {
	binary := buildTestBinary(t)
	script := declaredToolScript(t)
	endpoint := sim.New(&sim.Scenario{
		Model: "fake",
		Turns: []sim.Turn{
			{Calls: []sim.Call{{Name: "weather", Arguments: `{"city":"London"}`}}},
			{Say: "No matter."},
		},
	})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]
	environment := append(testBinaryEnvironment(t, t.TempDir()), backend.EndpointVariable+"="+address)
	writeDeclaredToolConfig(t, environment, `[tools.weather]
description = "report the weather for a city"
command = ["`+script+`"]
parameters = [
    { name = "city", kind = "string", description = "the city to report on" },
]
`)

	output := runTestBinary(
		t, binary, reachableWorkspaceDir(t), environment,
		"-p", "--yolo", "-m", "anthropic/fake", "what is the weather",
	)

	if strings.Contains(output, "forecast for") {
		t.Errorf("the declared tool ran with nobody to ask: %q", output)
	}
}

func TestADeclaredToolIsOfferedAloneWhenItIsNamed(t *testing.T) {
	binary := buildTestBinary(t)
	script := declaredToolScript(t)
	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Nothing to do."}}})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)

	address := endpoint.Addresses(server.URL)[sim.Messages]
	environment := append(testBinaryEnvironment(t, t.TempDir()), backend.EndpointVariable+"="+address)
	writeDeclaredToolConfig(t, environment, `[tools.weather]
description = "report the weather for a city"
command = ["`+script+`"]
parameters = [
    { name = "city", kind = "string", description = "the city to report on" },
]
`)

	runTestBinary(
		t, binary, reachableWorkspaceDir(t), environment,
		"-p", "--yolo", "-m", "anthropic/fake", "-t", "weather", "what is the weather",
	)

	requests := endpoint.Requests()
	if len(requests) == 0 {
		t.Fatal("the endpoint saw no request")
	}
	if !slices.Equal(requests[0].Tools, []string{"weather"}) {
		t.Errorf("got the tools %q, want the declared tool alone", requests[0].Tools)
	}
}

func TestAWorkspaceCannotDeclareAToolOfItsOwn(t *testing.T) {
	binary := buildTestBinary(t)
	workspaceDir := reachableWorkspaceDir(t)
	overridePath := filepath.Join(workspaceDir, "oh.toml")
	declaration := "[tools.weather]\ndescription = \"x\"\ncommand = [\"echo\"]\n"
	if err := os.WriteFile(overridePath, []byte(declaration), 0o600); err != nil {
		t.Fatal(err)
	}

	//nolint:gosec // running the binary under test
	process := exec.CommandContext(t.Context(), binary, "-p", "hello")
	process.Env = testBinaryEnvironment(t, t.TempDir())
	process.Dir = workspaceDir
	output, err := process.CombinedOutput()
	if err == nil {
		t.Fatalf("the workspace declared a tool: %q", output)
	}
	if !strings.Contains(string(output), "cannot be overridden in oh.toml") {
		t.Errorf("got %q", output)
	}
}
