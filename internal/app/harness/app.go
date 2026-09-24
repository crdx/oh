package harness

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"

	"crdx.org/oh/internal/app/access"
	"crdx.org/oh/internal/app/bar"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/cycle"
	"crdx.org/oh/internal/app/dispatch"
	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/input"
	"crdx.org/oh/internal/app/interaction"
	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/jobrecord"
	"crdx.org/oh/internal/app/key"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/paste"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/internal/app/pictures"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/record"
	"crdx.org/oh/internal/app/schedule"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/terminal"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/jobs"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/ask"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool/middleware/truncate"
	"crdx.org/oh/pkg/toolbox/title"
)

const configReloadConfirmationDuration = 10 * time.Second

type SessionLogger = record.Session

type pendingMessage struct {
	state agent.Event
}

type pendingNotices struct {
	items    []pendingMessage
	renderer *painter.PendingMessages
	block    *output.BlockHandle
}

func (self *pendingNotices) add(state agent.Event) {
	self.items = append(self.items, pendingMessage{state: state})
}

func (self *pendingNotices) takeBack(index int) {
	self.items = slices.Delete(self.items, index, index+1)
}

func (self *pendingNotices) hasStoppedJobsAfter(index int, whichCaps caps.Set) bool {
	for _, item := range self.items[index+1:] {
		if item.state.Kind != caps.JobStop {
			continue
		}

		if withdrawnCaps, err := caps.GrantedBy(item.state); err == nil && withdrawnCaps.Has(whichCaps) {
			return true
		}
	}

	return false
}

func (self *pendingNotices) notices() []string {
	var notices []string
	for _, item := range self.items {
		if itemNotices, areSaid := painter.HarnessNotices(item.state); areSaid {
			notices = append(notices, itemNotices...)
		}
	}
	return notices
}

type jobState struct {
	manager         *jobs.Manager
	recordedListing string
	hasRecorded     bool
	doesWake        bool
}

type displayState struct {
	bar                bar.Config
	streamingMode      output.StreamingMode
	reasoningRendering output.ReasoningRendering
	theme              style.Theme
	pictures           pictures.Display
	modelName          string
}

type runMode struct {
	isPrinting  bool
	isPlain     bool
	isYolo      bool
	isSimulated bool
}

type slashState struct {
	commands   slash.Registry
	completion slash.Completion
}

type questionState struct {
	broker  *ask.Broker
	request *ask.Request
	cursor  int
}

type App struct {
	agent           *agent.Agent
	recordedEvents  []agent.Event
	openingEvents   []agent.Event
	screen          *output.Screen
	recorder        *record.Recorder
	configObserver  *config.Observer
	inputLine       *edit.Input
	editorConfig    *editor.Config
	mode            *caps.Mode
	conditions      *conditions.State
	pathGrants      *pathgrant.Grants
	hostToSandbox   *portgrant.HostToSandbox
	sandboxToHost   *portgrant.SandboxToHost
	jobs            jobState
	settledNotes    []string
	settledCaps     caps.Set
	pendingNotices  pendingNotices
	feedback        feedback.State
	terminal        terminal.Terminal
	metrics         metrics.Tracker
	toolOutputLimit *truncate.Limit
	experimental    *experimental.Toggles
	permissions     *permission.Live
	onFailure       func(failure error)
	onQuestion      func(question ask.Question)
	savePastedImage func(mediaType string, data []byte) (string, error)
	pasteExchange   paste.Exchange
	workspace       *work.Space
	continueMessage string
	display         displayState
	runMode         runMode
	slash           slashState
	question        questionState
	transition      cycle.Transition
	queuedTurn      turn.Queue
	currentTurn     Turn
	startedAt       time.Time
	keyboard        *os.File
	now             func() time.Time
}

type Turn struct {
	*turn.Stream

	painter *painter.Picasso
}

type TurnEvent = turn.Event

const historyLimit = 1000

func (self *App) begin(message string) cycle.Transition {
	self.initialiseAccess()

	history := edit.NewHistory(location.GetHistoryPath(), historyLimit)
	inputLine := edit.NewInput(history)
	self.inputLine = inputLine

	if self.runMode.isPrinting {
		self.print(history, message)
		return self.transition
	}

	restoreTTY, err := tty.Raw(self.getKeyboard(), os.Stdout)
	if err != nil {
		self.plainly(history, message)
		return self.transition
	}

	restoreTitle := self.terminal.Begin(self.mode.Current())
	restoreCursor := self.screen.BeginEditing()
	closeQuestions := func() {}
	if self.question.broker != nil {
		closeQuestions = self.question.broker.Open()
	}
	defer closeQuestions()

	restoreTerminal := func() {
		restoreTerminalState(
			self.screen,
			self.recorder.IsPersisted(),
			restoreCursor,
			restoreTitle,
			restoreTTY,
		)
	}

	defer restoreTerminal()
	stopListening := tty.RestoreOnSignal(restoreTerminal)
	defer stopListening()
	defer self.dropPendingInput()

	self.show(inputLine)
	if len(self.pendingNotices.items) > 0 {
		self.refreshPendingMessages()
	}

	self.acceptInitialInput(inputLine, history, message)
	if self.isTransitionRequested() {
		return self.transition
	}

	interaction.Run(self.getKeyboard(), self.nextRefresh, interaction.Handler{
		GetTurnEvents: func() <-chan turn.Event { return self.currentTurn.Events() },
		OnKey:         func(keypress key.Key) bool { return self.handleKeypressAndShowInput(inputLine, history, keypress) },
		OnTurn:        self.takeTurn,
		OnTurnFinished: func() bool {
			self.finish()
			return !self.isTransitionRequested()
		},
		OnResize:              self.redraw,
		OnBeat:                self.screen.RefreshProgress,
		Changes:               self.configObserver.Changes(),
		OnChange:              self.reloadConfig,
		Conclusions:           self.jobConclusions(),
		OnJobEnded:            self.jobEnded,
		HostToSandboxChanges:  self.hostToSandboxChanges(),
		OnHostToSandboxChange: self.holdHostToSandboxChange,
		QuestionChanges:       self.questionChanges(),
		OnQuestionChange:      self.onQuestionChange,
		OnDraw:                func() { self.show(inputLine) },
	})

	return self.transition
}

func (self *App) acceptInitialInput(inputLine *edit.Input, history *edit.History, message string) {
	if message == "" {
		return
	}

	history.Add(message)
	if self.handleCommand(message) == dispatch.Proceed {
		self.start(message)
		return
	}

	self.show(inputLine)
}

func restoreTerminalState(screen *output.Screen, isPersisted bool, restorers ...func()) {
	screen.Release(isPersisted)
	for _, restore := range restorers {
		restore()
	}
}

func (self *App) handleKeypressAndShowInput(inputLine *edit.Input, history *edit.History, keypress key.Key) bool {
	if self.terminal.ObserveFocus(keypress.Code) {
		return true
	}

	if self.isAwaitingAnswer() {
		self.screen.Sync(func() {
			self.answerQuestion(keypress)
			self.show(inputLine)
		})
		return true
	}

	if keypress.Code == key.Clipboard {
		if self.receivePaste(inputLine, keypress.Clipboard) {
			self.screen.Sync(func() { self.show(inputLine) })
		}

		return true
	}

	shouldContinue := true
	self.screen.Sync(func() {
		shouldContinue = self.apply(inputLine, history, keypress)
		if shouldContinue {
			self.show(inputLine)
		}
	})
	return shouldContinue
}

func (self *App) answerQuestion(keypress key.Key) {
	request := self.question.request
	options := len(request.Question.Options)

	switch {
	case keypress.Code == key.Escape:
		request.Cancel()
	case keypress.Code == key.Enter:
		request.Choose(self.question.cursor)
	case keypress.Code == key.Up || keypress.Code == key.Left:
		self.question.cursor = (self.question.cursor + options - 1) % max(options, 1)
		return
	case keypress.Code == key.Down || keypress.Code == key.Right:
		self.question.cursor = (self.question.cursor + 1) % max(options, 1)
		return
	case keypress.Code == key.Rune && keypress.Value == '\t':
		self.question.cursor = (self.question.cursor + 1) % max(options, 1)
		return
	case keypress.Code == key.Rune && !keypress.Mod.Has(key.Ctrl) && !keypress.Mod.Has(key.Alt):
		index := request.Question.IndexForKey(unicode.ToLower(keypress.Value))
		if index < 0 {
			return
		}
		request.Choose(index)
	default:
		return
	}

	self.finishQuestion()
}

func (self *App) apply(inputLine *edit.Input, history *edit.History, keypress key.Key) bool {
	if keypress.Code == key.FocusIn || keypress.Code == key.FocusOut {
		return true
	}

	previousText := inputLine.Text()
	action := inputLine.Apply(keypress, self.currentTurn.Running())
	if inputLine.Text() != previousText {
		self.feedback.Dismiss()
	} else if dismissesFeedback(keypress) && self.feedback.Dismiss() {
		action = edit.DrawInput
	}
	if action != edit.CompleteCommand {
		self.slash.completion.Reset()
	}

	switch action {
	case edit.AcceptInput:
		self.acceptInput(inputLine, history)

	case edit.ForceAcceptInput:
		self.submitInput(inputLine, history, strings.TrimSpace(inputLine.Text()))

	case edit.ContinueTurn:
		self.continueOrFlush(inputLine, history)

	case edit.CancelTurn:
		if !self.takeBackInterjection(inputLine) {
			self.cancelTurn(stopKeyCause(keypress))
		}

	case edit.QuitSession:
		return false

	case edit.CompleteCommand:
		if completion, found := self.slash.completion.Next(self.slash.commands, inputLine.Text()); found {
			inputLine.SetText(completion)
		}

	case edit.ToggleWrite:
		self.toggleCap(caps.Write)

	case edit.ToggleShell:
		self.toggleCap(caps.Shell)

	case edit.ToggleNetwork:
		self.toggleCap(caps.Network)

	case edit.ToggleGit:
		self.toggleCap(caps.Git)

	case edit.ToggleLookup:
		self.toggleCap(caps.Lookup)

	case edit.DrawInput:
	}

	return !self.isTransitionRequested() || self.currentTurn.Running()
}

func (self *App) receivePaste(inputLine *edit.Input, report *key.ClipboardReport) bool {
	result := self.pasteExchange.Receive(report)

	switch result.Kind {
	case paste.Requested:
		self.screen.WriteEscape(result.Sequence)

	case paste.Text:
		self.feedback.Dismiss()
		inputLine.InsertPasted(result.Text)

		return true

	case paste.Image:
		return self.pasteImage(inputLine, result)

	case paste.Failed:
		self.showPasteFailure(result.Message)

		return true

	case paste.Ignored:
	}

	return false
}

func (self *App) pasteImage(inputLine *edit.Input, result paste.Result) bool {
	if self.savePastedImage == nil {
		return false
	}

	path, err := self.savePastedImage(result.MediaType, result.Data)
	if err != nil {
		self.showPasteFailure(err.Error())

		return true
	}

	self.feedback.Dismiss()
	inputLine.Insert(path)

	return true
}

func (self *App) showPasteFailure(message string) {
	self.showFeedback(feedback.Command, feedback.Message{
		Text:   "Could not paste: " + message,
		Status: agent.ErrorStatus,
	})
}

func (self *App) isTransitionRequested() bool {
	return self.transition.Kind != cycle.Quit
}

func (self *App) requestTransition(transition cycle.Transition) error {
	self.transition = transition
	if self.currentTurn.Running() {
		self.interruptTurn(interrupt.SessionClose)
	}
	return nil
}

func (self *App) handleCommand(message string) dispatch.Result {
	self.feedback.Clear(feedback.Command)
	result, failure := dispatch.Handle(self.slash.commands, dispatch.Actions{
		EmitEvent:  self.emitCommandEvent,
		SendPrompt: self.sendCommandPrompt,
		ShowFeedback: func(text string, status agent.Status) {
			self.showFeedback(feedback.Command, feedback.Message{Text: text, Status: status})
		},
		ShowPlainFeedback: func(text string) {
			self.showFeedback(feedback.Command, feedback.Message{Text: text, HasOwnStyle: true})
		},
	}, message)
	if failure != "" {
		self.showFeedback(feedback.Command, feedback.Message{Text: failure, Status: agent.ErrorStatus})
	}
	return result
}

func (self *App) emitCommandEvent(event agent.Event) {
	if event.Kind == hostcommand.Ran {
		self.hostCommandRan(event)
		return
	}
	if event.Kind == portgrant.SandboxToHostChange {
		self.pendingNotices.add(event)
		if self.currentTurn.Running() {
			self.queuedTurn.MarkAccessChange()
			self.interruptTurn(interrupt.AccessChange)
			return
		}
		self.refreshPendingMessages()
		return
	}
	if event.Kind != pathgrant.Change {
		self.notify(event)
		return
	}

	self.queuePathGrantChange(event)
	if self.currentTurn.Running() {
		self.queuedTurn.MarkAccessChange()
		self.interruptTurn(interrupt.AccessChange)
		return
	}
	self.refreshPendingMessages()
}

func (self *App) hostCommandRan(event agent.Event) {
	if self.holdNotice(event) {
		self.startTurn()
	}
}

func (self *App) holdNotice(event agent.Event) bool {
	notices, areSaid := painter.HarnessNotices(event)
	if !areSaid {
		return false
	}

	if self.currentTurn.Note(strings.Join(notices, noticeSeparator)) {
		self.notify(event)
		return false
	}

	self.pendingNotices.add(event)
	self.refreshPendingMessages()

	return true
}

func (self *App) queuePathGrantChange(event agent.Event) {
	if index, isPending := self.pendingPathGrantChange(event.Name); isPending {
		self.takeBackPathGrantChange(index, event.Name)

		if self.pathGrants.IsTold(event.Name) {
			return
		}
	}

	if _, isShown := pathgrant.Notice(event); !isShown {
		return
	}
	self.pendingNotices.add(event)
}

func (self *App) pendingPathGrantChange(path string) (int, bool) {
	for index, item := range self.pendingNotices.items {
		if item.state.Kind == pathgrant.Change && item.state.Name == path {
			return index, true
		}
	}

	return 0, false
}

func (self *App) takeBackPathGrantChange(index int, path string) {
	self.pendingNotices.takeBack(index)

	grants := self.pathGrants.GetCurrent()
	for other := range self.pendingNotices.items {
		item := &self.pendingNotices.items[other]
		if item.state.Kind != pathgrant.Change {
			continue
		}
		item.state = pathgrant.WithGrantOf(item.state, path, grants)
	}
}

func (self *App) sendCommandPrompt(message string) {
	if self.currentTurn.Interject(message) {
		return
	}
	self.start(message)
}

func (self *App) acceptInput(inputLine *edit.Input, history *edit.History) {
	message := strings.TrimSpace(inputLine.Text())
	switch self.handleCommand(message) {
	case dispatch.Handled:
		history.Add(message)
		inputLine.Reset()
	case dispatch.Proceed:
		self.submitInput(inputLine, history, message)
	case dispatch.Rejected:
	}
}

func (self *App) submitInput(inputLine *edit.Input, history *edit.History, message string) {
	history.Add(message)
	inputLine.Reset()

	if message == "" {
		return
	}

	if !self.currentTurn.Interject(message) {
		self.start(message)
	}
}

func (self *App) sendInput(inputLine *edit.Input, history *edit.History, message string) {
	history.Add(message)
	inputLine.Reset()

	if message == "" {
		return
	}

	if self.currentTurn.Running() {
		self.replaceTurn(message)
	} else {
		self.start(message)
	}
}

func (self *App) continueOrFlush(inputLine *edit.Input, history *edit.History) {
	if self.currentTurn.HasInterjections() {
		self.interruptTurn(interrupt.Replacement)
		return
	}

	if !self.currentTurn.Running() && self.hasUntoldPendingNotices() {
		self.startTurn()
		return
	}

	self.sendInput(inputLine, history, self.continueMessage)
}

func (self *App) hasUntoldPendingNotices() bool {
	return len(self.pendingNotices.items) > 0
}

func (self *App) takeBackInterjection(inputLine *edit.Input) bool {
	if inputLine == nil {
		return false
	}

	message, isQueued := self.currentTurn.TakeLastInterjection()
	if !isQueued {
		return false
	}

	if typedText := inputLine.Text(); typedText != "" {
		message += agent.InterjectionSeparator + typedText
	}

	inputLine.SetText(message)

	return true
}

func (self *App) toggleCap(whichCaps caps.Set) {
	self.mode.Toggle(whichCaps)
	self.terminal.SetMode(self.mode.Current())

	isWithdrawn := !self.mode.Current().Has(whichCaps)

	if i, isPending := self.pendingModeChange(whichCaps); isPending {
		self.takeBackModeChange(i, whichCaps)
	} else {
		self.showModeChange(whichCaps)
	}

	if isWithdrawn {
		self.stopJobsLosingAccess(whichCaps)
	}

	if self.currentTurn.Running() {
		self.queuedTurn.MarkAccessChange()
		self.interruptTurn(interrupt.AccessChange)
	}
}

func (self *App) pendingModeChange(whichCaps caps.Set) (int, bool) {
	for index, item := range slices.Backward(self.pendingNotices.items) {
		if item.state.Kind != caps.ModeChange || item.state.Name != whichCaps.Flag() {
			continue
		}

		return index, !self.pendingNotices.hasStoppedJobsAfter(index, whichCaps)
	}

	return 0, false
}

func (self *App) showModeChange(whichCaps caps.Set) {
	self.pendingNotices.add(caps.ModeToggleEvent(whichCaps, self.mode.Current()))

	if !self.currentTurn.Running() {
		self.refreshPendingMessages()
	}
}

func (self *App) takeBackModeChange(index int, whichCaps caps.Set) {
	self.pendingNotices.takeBack(index)

	for other := index; other < len(self.pendingNotices.items); other++ {
		item := &self.pendingNotices.items[other]
		if item.state.Kind != caps.ModeChange {
			continue
		}
		item.state = caps.ModeWithout(item.state, whichCaps)
	}

	if self.currentTurn.Running() {
		self.redraw()
		return
	}

	self.refreshPendingMessages()
}

func (self *App) refreshPendingMessages() {
	if len(self.pendingNotices.items) == 0 {
		handle := self.pendingNotices.block
		self.pendingNotices.renderer = nil
		self.pendingNotices.block = nil

		if handle != nil && !self.screen.DiscardBlock(handle) {
			self.redraw()
		}
		return
	}

	messages := self.pendingNotices.notices()
	if self.pendingNotices.renderer == nil {
		self.pendingNotices.renderer = painter.NewPendingMessages(
			messages,
			self.screen.IsTerminal(),
			self.screen.LinkRoots().WithoutScratch(),
		)
		self.pendingNotices.block = self.screen.OpenPanel(self.pendingNotices.renderer)
		return
	}

	self.pendingNotices.renderer.Replace(messages)
	if !self.screen.RefreshBlock(self.pendingNotices.block) {
		self.pendingNotices.renderer = nil
		self.pendingNotices.block = nil
		self.redraw()
	}
}

func (self *App) initialiseAccess() {
	if self.settledCaps != 0 {
		return
	}
	self.settledCaps = self.mode.Current()
	self.recordModeEvent(caps.ModeEvent(self.settledCaps))
	for _, event := range self.openingEvents {
		self.storeEvent(event)
	}
	self.openingEvents = nil
}

func (self *App) settleAccess() {
	if self.settledCaps == 0 {
		self.initialiseAccess()
	}

	self.settlePendingInput()
	self.settledCaps = self.mode.Current()
}

func (self *App) settlePendingInput() {
	self.settledNotes = append(self.settledNotes, self.pendingNotices.notices()...)
	self.markAccessTold()

	wasShown := self.pendingNotices.block != nil
	for _, item := range self.pendingNotices.items {
		if item.state.Kind == "" {
			continue
		}
		self.metrics.Record(item.state)
		self.recordedEvents = append(self.recordedEvents, item.state)
		self.storeEvent(item.state)
		if !wasShown {
			self.noticePainter().DrawEvent(item.state)
		}
	}

	if self.pendingNotices.block != nil {
		self.pendingNotices.renderer.MarkSent()
		self.screen.RefreshBlock(self.pendingNotices.block)
		self.screen.SealBlock(self.pendingNotices.block)
	}
	self.pendingNotices = pendingNotices{}
}

func (self *App) dropPendingInput() {
	if self.pendingNotices.block != nil {
		self.screen.DiscardBlock(self.pendingNotices.block)
	}
	self.pendingNotices = pendingNotices{}
}

func (self *App) recordModeEvent(event agent.Event) {
	self.storeEvent(event)
}

func dismissesFeedback(keypress key.Key) bool {
	if keypress.Code == key.Backspace || keypress.Code == key.Escape {
		return true
	}

	return keypress.Code == key.Rune && keypress.Value == 'd' && keypress.Mod.Has(key.Ctrl)
}

func stopKeyCause(keypress key.Key) interrupt.Cause {
	if keypress.Code == key.Escape {
		return interrupt.Escape
	}

	return interrupt.ControlD
}

func (self *App) cancelTurn(cause interrupt.Cause) {
	if self.currentTurn.Cancelled() {
		self.queuedTurn.Drop()
	}

	self.interruptTurn(cause)
}

func (self *App) replaceTurn(message string) {
	self.queuedTurn.Replace(message)
	self.interruptTurn(interrupt.Replacement)
}

func (self *App) interruptTurn(cause interrupt.Cause) {
	self.currentTurn.Interrupt(interrupt.Because(cause))
}

func (self *App) show(inputLine *edit.Input) {
	if inputLine.IsPasting() {
		return
	}

	self.feedback.ClearExpired(self.getNow())

	columns := self.screen.Columns()
	frame := inputLine.Frame(columns)
	isFeedbackFramed := !self.feedback.IsEmpty()
	statusWidth := columns
	topWidth := columns
	if isFeedbackFramed {
		statusWidth = input.FeedbackContentWidth(columns)
		topWidth = input.FeedbackRuleWidth(columns)
	}

	topRight := self.renderBar(segment.TopRight, frame)
	bottomRight := self.renderBar(segment.BottomRight, frame)
	block := input.Block{
		Top: input.Ruler{
			Left:   self.renderBarWithin(segment.TopLeft, frame, input.LeftContentWidth(topWidth, topRight)),
			Center: self.renderBar(segment.TopCenter, frame),
			Right:  topRight,
		},
		Input: frame,
		Bottom: input.Ruler{
			Left:   self.renderBarWithin(segment.BottomLeft, frame, input.LeftContentWidth(columns, bottomRight)),
			Center: self.renderBar(segment.BottomCenter, frame),
			Right:  bottomRight,
		},
		Status:        self.statusRows(statusWidth),
		FrameFeedback: isFeedbackFramed,
		Question:      self.questionRows(columns),
		Rule:          self.ruleStyle(),
	}

	if self.isAwaitingAnswer() {
		block.Top.Center = painter.QuestionHead(
			self.question.request.Question,
			self.remainingAnswerTime(self.getNow()),
		)
	}

	rows, cursorRow, cursorColumn := block.Rows(columns)

	if self.isAwaitingAnswer() {
		self.screen.InertFooter(rows, cursorRow)
		return
	}

	self.screen.Footer(rows, cursorRow, cursorColumn)
}

func (self *App) isAwaitingAnswer() bool {
	return self.question.request != nil
}

func (self *App) questionRows(columns int) []string {
	request := self.question.request
	if request == nil {
		return nil
	}

	return painter.RenderQuestion(request.Question, self.question.cursor, columns)
}

func (self *App) remainingAnswerTime(at time.Time) time.Duration {
	request := self.question.request
	if request == nil {
		return 0
	}

	deadline, hasDeadline := request.Deadline()
	if !hasDeadline {
		return 0
	}

	return deadline.Sub(at)
}

func (self *App) ruleStyle() style.Style {
	switch {
	case self.isAwaitingAnswer():
		return style.Change
	case self.runMode.isYolo && !self.runMode.isSimulated:
		return style.Hazard
	default:
		return style.Rule
	}
}

func (self *App) statusRows(columns int) []string {
	if self.feedback.IsEmpty() {
		return painter.RenderQueuedMessages(
			self.currentTurn.GetInterjections(),
			!self.currentTurn.Cancelled(),
			columns,
			self.screen.IsTerminal(),
			self.screen.LinkRoots().WithoutScratch(),
		)
	}

	return self.feedback.Render(columns, self.getNow())
}

func (self *App) showFeedback(source feedback.Source, message feedback.Message) {
	if self.runMode.isPlain {
		text := message.Text
		if !message.HasOwnStyle {
			text = painter.NoticeStyle(message.Status)(text)
		}
		self.screen.Line(text)
		return
	}

	self.feedback.Show(source, message, self.getNow())
}

func (self *App) renderBar(position segment.Position, frame edit.Frame) string {
	return self.display.bar.Render(position, getBarContext(frame))
}

func (self *App) renderBarWithin(position segment.Position, frame edit.Frame, cells int) string {
	return self.display.bar.RenderWithin(position, getBarContext(frame), cells)
}

func getBarContext(frame edit.Frame) segment.Context {
	return segment.Context{
		HiddenLinesAbove: frame.HiddenLinesAbove,
		HiddenLinesBelow: frame.HiddenLinesBelow,
	}
}

func (self *App) getBarSources() bar.Sources {
	return bar.Sources{
		IsTurnRunning:          self.isTurnRunning,
		IsSessionPersisted:     self.isSessionPersisted,
		GetContextUsage:        self.contextUsage,
		GetCacheUsage:          self.cacheUsage,
		GetSessionSpend:        self.sessionSpend,
		GetGrantedCaps:         self.grantedCaps,
		GetPathGrants:          self.getPathGrants,
		GetHostToSandboxRoutes: self.getHostToSandboxRoutes,
		GetSandboxToHostPorts:  self.getSandboxToHostPorts,
		IsPrefixPending:        self.isPrefixPending,
		GetTurnTiming:          self.turnTiming,
		GetTurnCount:           self.turnCount,
		GetJobs:                self.getJobs,
	}
}

func (self *App) isSessionPersisted() bool {
	return self.recorder != nil && self.recorder.IsPersisted()
}

func (self *App) getJobs() []jobs.Snapshot {
	if self.jobs.manager == nil {
		return nil
	}

	return self.jobs.manager.List()
}

func (self *App) questionChanges() <-chan struct{} {
	if self.question.broker == nil {
		return nil
	}

	return self.question.broker.Changes()
}

func (self *App) onQuestionChange() {
	request := self.question.broker.Current()
	if request == self.question.request {
		return
	}

	self.finishQuestion()

	self.question.request = request
	if request == nil {
		return
	}

	self.question.cursor = request.Question.DefaultIndex()

	if self.currentTurn.painter != nil {
		self.currentTurn.painter.HoldTiming()
	}

	if self.onQuestion != nil {
		self.onQuestion(request.Question)
	}
}

func (self *App) finishQuestion() {
	if self.question.request == nil {
		return
	}

	self.question.request = nil

	if self.currentTurn.painter != nil {
		self.currentTurn.painter.ResumeTiming()
	}
}

func (self *App) hostToSandboxChanges() <-chan agent.Event {
	if self.hostToSandbox == nil {
		return nil
	}

	return self.hostToSandbox.Changes()
}

func (self *App) holdHostToSandboxChange(event agent.Event) {
	self.holdNotice(event)
}

func (self *App) drainHostToSandboxChanges() {
	if self.hostToSandbox == nil {
		return
	}

	for {
		select {
		case event := <-self.hostToSandbox.Changes():
			self.holdNotice(event)
		default:
			return
		}
	}
}

func (self *App) jobConclusions() <-chan jobs.Conclusion {
	if self.jobs.manager == nil {
		return nil
	}

	return self.jobs.manager.Conclusions()
}

func (self *App) jobEnded(conclusion jobs.Conclusion) {
	conclusion.Output = self.withinToolOutputLimit(conclusion.Output)

	if self.holdNotice(jobrecord.EndedEvent(conclusion)) && self.jobs.doesWake {
		self.startTurn()
	}
}

func (self *App) withinToolOutputLimit(output string) string {
	if self.toolOutputLimit == nil {
		return output
	}

	return truncate.Output(output, self.toolOutputLimit)
}

func (self *App) stopJobsHoldingPath(path string) {
	if self.jobs.manager == nil {
		return
	}

	for _, name := range self.jobs.manager.StopHolding(shell.StoppedByPath(path)) {
		self.showStoppedJob(caps.JobStoppedForPathEvent(name, path))
	}
}

func (self *App) stopJobsLosingAccess(withdrawnCaps caps.Set) {
	if self.jobs.manager == nil {
		return
	}

	holds, doesStop := shell.StoppedBy(withdrawnCaps, self.workspace.GetDir())
	if !doesStop {
		return
	}

	for _, name := range self.jobs.manager.StopHolding(holds) {
		self.showStoppedJob(caps.JobStopEvent(name, withdrawnCaps))
	}
}

func (self *App) showStoppedJob(event agent.Event) {
	self.pendingNotices.add(event)

	if !self.currentTurn.Running() {
		self.refreshPendingMessages()
	}
}

func (self *App) isTurnRunning() bool {
	return self.currentTurn.Running()
}

func (self *App) nextBarRefresh(at time.Time) time.Time {
	return self.display.bar.NextRefresh(segment.Phase{At: at, IsRunning: self.isTurnRunning()})
}

func (self *App) nextRefresh(at time.Time) time.Time {
	return schedule.Soonest(
		self.nextBarRefresh(at),
		self.feedback.NextRefresh(at),
		self.nextAnswerRefresh(at),
	)
}

func (self *App) nextAnswerRefresh(at time.Time) time.Time {
	if self.remainingAnswerTime(at) <= 0 {
		return time.Time{}
	}

	return schedule.NextTick(at, time.Second)
}

func (self *App) reloadConfig(watchFailure error) bool {
	result := self.configObserver.Reload(watchFailure, self.display.bar.GetRegistry())
	switch result.Status {
	case config.ReloadUnchanged:
		return false
	case config.ReloadFailed:
		self.showFeedback(feedback.Config, feedback.Message{
			Text:   "The configuration could not be reloaded: " + result.Failure.Error(),
			Status: agent.ErrorStatus,
		})
		return true
	case config.ReloadApplied:
		if err := self.slash.commands.ReplaceCommandSet(result.LiveConfig.SnippetCommandSet); err != nil {
			self.showFeedback(feedback.Config, feedback.Message{
				Text:   "The configuration could not be reloaded: could not replace snippets: " + err.Error(),
				Status: agent.ErrorStatus,
			})
			return true
		}
		isThemeChanged := self.display.theme != result.LiveConfig.Theme
		if isThemeChanged {
			style.ApplyTheme(result.LiveConfig.Theme)
			self.display.theme = result.LiveConfig.Theme
			defer self.redraw()
		}
		self.slash.completion.Reset()
		self.continueMessage = result.LiveConfig.ContinueMessage
		self.editorConfig.ReplaceCommand(result.LiveConfig.EditorCommand)
		self.display.streamingMode = result.LiveConfig.StreamingMode
		self.display.reasoningRendering = result.LiveConfig.ReasoningRendering
		self.screen.SetGrouping(result.LiveConfig.Grouping)
		self.toolOutputLimit.Replace(result.LiveConfig.ToolOutputBytes)
		self.experimental.Replace(result.LiveConfig.Experimental)
		if self.permissions != nil {
			self.permissions.Replace(result.LiveConfig.Permissions)
		}
		self.display.bar.ReplaceLayout(result.LiveConfig.SegmentLayout)
		self.feedback.Clear(feedback.Config)
		if len(result.LiveConfig.UnknownSettings) > 0 {
			self.notifyUnknownSettings(result.LiveConfig.UnknownSettings)
			return true
		}
		if self.feedback.Message().Status != agent.ErrorStatus {
			self.showFeedback(feedback.Confirmation, feedback.Message{
				Text:         reloadConfirmation(result.Changes),
				Status:       agent.SuccessStatus,
				DismissAfter: configReloadConfirmationDuration,
			})
		}
	}
	return true
}

func reloadConfirmation(changes []config.SourceChange) string {
	rows := make([]string, 0, len(changes)+1)
	rows = append(rows, "Configuration reloaded automatically")

	for _, change := range changes {
		switch {
		case change.IsRemoved && len(change.Settings) > 0:
			rows = append(rows, change.Path+": gone, dropping "+strings.Join(change.Settings, ", "))
		case change.IsRemoved:
			rows = append(rows, change.Path+": gone")
		case len(change.Settings) == 0:
			rows = append(rows, change.Path+": no setting changed")
		default:
			rows = append(rows, change.Path+": "+strings.Join(change.Settings, ", "))
		}
	}

	return strings.Join(append(rows, reachRows(changes)...), "\n")
}

var reachDescriptions = map[config.Reach]string{
	config.ReachNextRun:     "when oh next starts",
	config.ReachNextSession: "in a new session",
}

func reachRows(changes []config.SourceChange) []string {
	settingsByReach := map[config.Reach][]string{}
	isRecorded := map[string]bool{}

	for _, change := range changes {
		for _, setting := range change.Settings {
			reach := config.ReachOf(setting)
			if reach == config.ReachLive || isRecorded[setting] {
				continue
			}
			isRecorded[setting] = true
			settingsByReach[reach] = append(settingsByReach[reach], setting)
		}
	}

	rows := make([]string, 0, len(settingsByReach))

	for _, reach := range []config.Reach{config.ReachNextRun, config.ReachNextSession} {
		settings := settingsByReach[reach]
		if len(settings) == 0 {
			continue
		}

		verb := "land"
		if len(settings) == 1 {
			verb = "lands"
		}

		rows = append(rows, strings.Join(settings, ", ")+" "+verb+" "+reachDescriptions[reach])
	}

	return rows
}

func (self *App) notifyUnknownSettings(reports []string) {
	if len(reports) == 0 {
		return
	}

	self.showFeedback(feedback.Config, feedback.Message{
		Text:   "Unknown settings were ignored.\n" + strings.Join(reports, "\n"),
		Status: agent.WarningStatus,
	})
}

func (self *App) turnSummary() session.TurnSummary {
	inputTokens, _ := self.metrics.ContextUsage()
	timing, _ := self.currentTurn.Timing()

	return session.TurnSummary{Took: timing.ModelTurn, InputTokens: inputTokens}
}

func (self *App) turnTiming() turn.Timing {
	if timing, isKnown := self.currentTurn.Timing(); isKnown {
		return timing
	}

	if self.startedAt.IsZero() {
		return turn.Timing{}
	}
	return turn.Timing{UserTurn: time.Since(self.startedAt)}
}

func (self *App) turnCount() int {
	return self.metrics.TurnCount()
}

func (self *App) cacheUsage() (int, int) {
	return self.metrics.CacheUsage()
}

func (self *App) contextUsage() (int, int) {
	return self.metrics.ContextUsage()
}

func (self *App) sessionSpend() (float64, bool) {
	return self.metrics.Spend()
}

func (self *App) grantedCaps() caps.Set {
	return self.mode.Current()
}

func (self *App) getPathGrants() []pathgrant.Grant {
	if self.pathGrants == nil {
		return nil
	}
	return self.pathGrants.GetCurrent()
}

func (self *App) getHostToSandboxRoutes() []portgrant.Route {
	if self.hostToSandbox == nil {
		return nil
	}
	return self.hostToSandbox.GetRoutes()
}

func (self *App) getSandboxToHostPorts() []uint16 {
	if self.sandboxToHost == nil {
		return nil
	}
	return self.sandboxToHost.GetCurrent()
}

func (self *App) isPrefixPending() bool {
	return self.inputLine != nil && self.inputLine.IsPrefixPending()
}

func (self *App) getKeyboard() *os.File {
	if self.keyboard == nil {
		return os.Stdin
	}

	return self.keyboard
}

func (self *App) getNow() time.Time {
	if self.now == nil {
		return time.Now()
	}

	return self.now()
}

func (self *App) print(history *edit.History, message string) {
	self.runMode.isPlain = true
	defer func() { self.runMode.isPlain = false }()
	defer self.screen.End()

	self.acceptPlainInput(history, message)
}

func (self *App) plainly(history *edit.History, initialMessage string) {
	self.runMode.isPlain = true
	defer func() { self.runMode.isPlain = false }()

	if keyboard := self.getKeyboard(); tty.Is(keyboard) {
		self.acceptTypedLines(history, initialMessage, keyboard)
		return
	}

	self.acceptPlainInput(history, initialMessage)
}

func (self *App) acceptTypedLines(history *edit.History, initialMessage string, source io.Reader) {
	self.acceptPlainInput(history, initialMessage)
	if self.isTransitionRequested() {
		return
	}

	reader := bufio.NewScanner(source)

	for reader.Scan() {
		self.acceptPlainInput(history, strings.TrimSpace(reader.Text()))
		if self.isTransitionRequested() {
			return
		}
	}

	if err := reader.Err(); err != nil {
		fmt.Fprintln(os.Stderr, style.Error(fmt.Errorf("could not read input: %w", err)))
	}
}

func (self *App) acceptPlainInput(history *edit.History, message string) {
	if message == "" {
		return
	}
	if self.handleCommand(message) == dispatch.Proceed {
		self.ask(history, message)
		return
	}

	history.Add(message)
	if self.currentTurn.Running() {
		self.waitForCurrentTurn()
	}
}

func (self *App) ask(history *edit.History, message string) {
	history.Add(message)
	self.start(message)
	self.waitForCurrentTurn()
}

func (self *App) waitForCurrentTurn() {
	for self.currentTurn.Running() {
		for event := range self.currentTurn.Events() {
			self.takeTurn(event)
		}
		self.finish()
	}
}

func (self *App) getLastMessage() (string, bool) {
	for _, event := range slices.Backward(self.recordedEvents) {
		if event.Kind == agent.ModelMessageEvent {
			return event.Text, true
		}
	}
	return "", false
}

func (self *App) restore(storedSession *store.Session) {
	self.settledCaps, _ = caps.LastRecordedMode(storedSession.Events)

	if err := self.agent.RestoreState(storedSession.Events); err != nil {
		self.notifyFailure("The state could not be restored: " + err.Error())
		return
	}
	if err := self.agent.Load(storedSession.Items); err != nil {
		self.notifyFailure("The conversation could not be restored: " + err.Error())
		return
	}

	self.recorder.Resume(len(storedSession.Items))
	self.recordedEvents = append(self.recordedEvents, storedSession.Events...)

	for _, event := range storedSession.Events {
		self.takeSessionTitle(event)
	}

	self.agent.RestoreCache(storedSession.CacheReading)
	self.metrics.Restore(storedSession.Events, storedSession.Turns)
	self.restoreJobs(storedSession.Events)

	self.screen.Reset()
	self.replay()
}

func (self *App) newPainter(isRunning bool) *painter.Picasso {
	picasso := painter.New(self.screen, isRunning, self.agent.Tool, self.workspace, self.display.streamingMode)
	picasso.RenderReasoningAs(self.display.reasoningRendering)
	if self.screen.IsTerminal() && self.recorder != nil {
		picasso.LinkToolResults(self.recorder.Name())
	}
	if self.display.pictures.SessionDirectory != "" {
		picasso.DrawPicturesFrom(self.display.pictures)
	}
	picasso.SuggestForkingWith(self.display.modelName)
	return picasso
}

func (self *App) replay() {
	self.screen.Sync(func() {
		painter := self.newPainter(self.currentTurn.Running())

		for _, event := range self.recordedEvents {
			painter.DrawEvent(event)
		}

		if self.currentTurn.Running() {
			self.currentTurn.painter = painter
			return
		}

		painter.Close(dynamic.Cancelled)

		self.screen.End()
		self.refreshPendingMessages()
	})
}

func (self *App) redraw() {
	var provisionalPainter agent.Delta
	var previousPainter *painter.Picasso
	if self.currentTurn.Running() {
		previousPainter = self.currentTurn.painter
		provisionalPainter = previousPainter.ProvisionalDelta()
		previousPainter.Stop()
	}

	self.screen.Sync(func() {
		self.pendingNotices.renderer = nil
		self.pendingNotices.block = nil
		self.screen.Reset()
		self.replay()
		if provisionalPainter.Text != "" {
			self.currentTurn.painter.DrawRestoredDelta(provisionalPainter, previousPainter)
		}
		if self.inputLine != nil {
			self.show(self.inputLine)
		}
	})
}

func (self *App) startTurn() {
	self.start("")
}

func (self *App) start(message string) {
	userTurnElapsed := self.turnTiming().UserTurn
	self.settleAccess()
	self.metrics.BeginTurn()

	if note := self.prelude(); note != "" {
		self.agent.AddUserMessage(note)
	}

	self.currentTurn = Turn{
		painter: self.newPainter(true),
		Stream: turn.Start(self.agent, message, turn.Timing{
			UserTurn: userTurnElapsed,
		}),
	}

	self.screen.ReportProgress(true)
}

func (self *App) takeSessionTitle(event agent.Event) {
	if sessionTitle, isTitle := agent.TitleFromEvent(event); isTitle {
		self.terminal.SetSessionTitle(sessionTitle)
	}
}

func (self *App) prelude() string {
	notes := slices.DeleteFunc(
		[]string{
			self.interruptionNote(),
			self.takeSettledNotes(),
			self.titleNote(),
		},
		func(note string) bool { return note == "" },
	)

	return strings.Join(notes, " ")
}

func (self *App) accessTellers() access.Group {
	tellers := []access.Teller{self.mode}
	if self.conditions != nil {
		tellers = append(tellers, self.conditions)
	}
	if self.pathGrants != nil {
		tellers = append(tellers, self.pathGrants)
	}
	if self.hostToSandbox != nil {
		tellers = append(tellers, self.hostToSandbox)
	}
	if self.sandboxToHost != nil {
		tellers = append(tellers, self.sandboxToHost)
	}
	return access.NewGroup(tellers...)
}

func (self *App) markAccessTold() {
	self.accessTellers().Inject()
}

func (self *App) titleNote() string {
	if !self.agent.IsToolEnabled(title.Name) {
		return ""
	}

	hasAnswered := false
	for _, event := range self.recordedEvents {
		if _, isTitled := agent.TitleFromEvent(event); isTitled {
			return ""
		}
		if event.Kind == agent.ModelMessageEvent {
			hasAnswered = true
		}
	}
	if !hasAnswered {
		return ""
	}

	return "Untitled session: use the " + title.Name + " tool."
}

const noticeSeparator = "\n\n"

func (self *App) takeSettledNotes() string {
	notes := self.settledNotes
	self.settledNotes = nil

	return strings.Join(notes, noticeSeparator)
}

func (self *App) recordJobListing() {
	if self.jobs.manager == nil {
		return
	}

	listing := self.jobs.manager.List()
	if len(listing) == 0 && !self.jobs.hasRecorded {
		return
	}

	event := jobrecord.ListingEvent(listing)
	if string(event.State) == self.jobs.recordedListing {
		return
	}

	self.jobs.recordedListing = string(event.State)
	self.jobs.hasRecorded = true
	self.recordedEvents = append(self.recordedEvents, event)
	self.storeEvent(event)
}

func (self *App) restoreJobs(events []agent.Event) {
	if self.jobs.manager == nil {
		return
	}

	rememberedJobs, wasRecorded := jobrecord.LastRecorded(events)
	if !wasRecorded {
		return
	}

	self.jobs.manager.Restore(rememberedJobs)
	self.jobs.hasRecorded = true

	var live []string
	for _, snapshot := range rememberedJobs {
		if snapshot.IsLive() {
			live = append(live, snapshot.Name)
		}
	}

	if len(live) == 0 {
		return
	}

	self.pendingNotices.add(jobrecord.EndedWithSessionEvent(live))
}

func (self *App) interruptionNote() string {
	if !self.currentTurn.Cancelled() {
		return ""
	}

	note := "Turn stopped"
	if reason := self.interruptionReason(); reason != "" {
		note += " because " + reason
	}

	return note + "."
}

func (self *App) interruptionReason() string {
	if reason := self.currentTurn.Reason(); reason != nil {
		return reason.Error()
	}

	return ""
}

func (self *App) interruptionCause() interrupt.Cause {
	cause, _ := interrupt.CauseOf(self.currentTurn.Reason())

	return cause
}

func (self *App) takeTurn(turnEvent TurnEvent) {
	self.drainHostToSandboxChanges()

	if !self.currentTurn.Observe(turnEvent) {
		return
	}

	if turnEvent.Update.Delta != nil {
		self.currentTurn.painter.DrawDelta(*turnEvent.Update.Delta)
		if self.currentTurn.painter.Stale() {
			self.redraw()
		}
		return
	}

	if turnEvent.Update.Event != nil {
		self.recordEvent(*turnEvent.Update.Event)
	}
}

func (self *App) recordEvent(event agent.Event) {
	self.metrics.Record(event)
	self.takeSessionTitle(event)

	if event.Kind == agent.SilentTurnEvent && self.wasCutShort() && !self.wasPoked() {
		self.queuedTurn.MarkSilentTurn()
	}

	self.recordedEvents = append(self.recordedEvents, event)
	self.currentTurn.painter.DrawEvent(event)

	if self.currentTurn.painter.Stale() {
		self.redraw()
	}

	self.storeEvent(event)
}

func (self *App) storeEvent(event agent.Event) {
	if err := self.recorder.Event(event); err != nil {
		self.notifyFailure("The conversation could not be stored: " + err.Error())
	}
	self.showStorageWarnings()
}

func (self *App) notifyFailure(text string) {
	self.showFeedback(feedback.System, feedback.Message{Text: text, Status: agent.ErrorStatus})
}

func (self *App) notify(event agent.Event) {
	self.recordedEvents = append(self.recordedEvents, event)

	picasso := self.noticePainter()
	picasso.DrawEvent(event)
	if picasso.Stale() {
		self.redraw()
	}

	_ = self.recorder.Event(event)
}

func (self *App) noticePainter() *painter.Picasso {
	if self.currentTurn.Running() {
		return self.currentTurn.painter
	}

	return self.newPainter(false)
}

func (self *App) wasCutShort() bool {
	if len(self.recordedEvents) == 0 {
		return false
	}

	return self.recordedEvents[len(self.recordedEvents)-1].Kind == agent.ModelReasoningEvent
}

func (self *App) wasPoked() bool {
	for _, event := range slices.Backward(self.recordedEvents) {
		if event.Kind == turn.HarnessPoke {
			return true
		}
		if event.Kind == agent.UserMessageEvent {
			return false
		}
	}

	return false
}

func (self *App) finish() {
	self.drainHostToSandboxChanges()
	self.currentTurn.MarkFinished(time.Now())
	self.screen.ReportProgress(false)

	var turnError error
	if self.currentTurn.Cancelled() {
		self.recordEvent(interrupt.Event(self.interruptionCause()))
	} else if turnError = self.currentTurn.Error(); turnError != nil {
		self.recordEvent(agent.Event{Kind: agent.FailureEvent, Failure: agent.FailureFrom(turnError)})
	}

	if note, isNoted := self.currentTurn.TakeNotes(); isNoted {
		self.settledNotes = append(self.settledNotes, note)
	}

	self.recordJobListing()

	if self.storeProviderState() {
		if err := self.recorder.CompleteTurn(self.turnSummary()); err != nil {
			self.notifyFailure("The turn completion could not be stored: " + err.Error())
		}
	}
	self.showStorageWarnings()
	self.currentTurn.painter.Close(dynamic.Cancelled)
	if self.currentTurn.painter.Stale() {
		self.redraw()
	}
	self.screen.End()
	self.feedback.Clear(feedback.Command)

	self.currentTurn.Finish()

	if turnError != nil && self.onFailure != nil {
		self.onFailure(turnError)
	}

	if self.isTransitionRequested() {
		return
	}

	if message, isQueued := self.currentTurn.TakeInterjections(); isQueued {
		self.queuedTurn.Replace(message)
	}

	queuedKind, message := self.queuedTurn.Take()
	switch queuedKind {
	case turn.Replacement:
		self.refreshPendingMessages()
		self.start(message)
	case turn.AccessChange:
		if self.hasUntoldPendingNotices() {
			self.startTurn()
		}
	case turn.AccessNotice:
		self.refreshPendingMessages()
	case turn.Poke:
		self.refreshPendingMessages()
		self.notify(turn.PokeEvent())
		self.agent.AddUserMessage(message)
		self.startTurn()
	case turn.None:
	}
}

func (self *App) storeProviderState() bool {
	items, err := self.agent.Dump()
	if err != nil {
		self.notifyFailure("The conversation state could not be stored: " + err.Error())
		return false
	}

	if err := self.recorder.StoreItems(items); err != nil {
		self.notifyFailure(err.Error())
		return false
	}

	return true
}

func (self *App) showStorageWarnings() {
	warnings := self.recorder.TakeWarnings()
	if len(warnings) == 0 {
		return
	}

	messages := make([]string, len(warnings))
	for i, warning := range warnings {
		messages[i] = warning.Error()
	}
	self.notifyFailure(strings.Join(messages, "\n"))
}
