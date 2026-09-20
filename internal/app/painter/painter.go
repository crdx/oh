package painter

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"

	"crdx.org/oh/internal/app/call"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/jobrecord"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/pictures"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/startup"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/app/work"
)

const retryArgumentsCells = 120

type Picasso struct {
	screen         *output.Screen
	toolBlock      *dynamic.Block
	rows           map[string]int
	labels         map[string]call.Label
	answer         liveText
	answerRenderer markdown.IncrementalRenderer
	reasoning      liveText
	previousKind   agent.Kind

	isStale               bool
	isRunning             bool
	streamingMode         output.StreamingMode
	reasoningRendering    output.ReasoningRendering
	resultLinkSessionName string
	forkModelName         string

	getTool       func(string) (tool.Tool, bool)
	workspace     *work.Space
	pictures      pictures.Display
	pictureDrawer markdown.PictureDrawer

	heldPictures []heldPicture
}

type heldPicture struct {
	index   int
	picture dynamic.Picture
}

func New(
	screen *output.Screen,
	isRunning bool,
	getTool func(string) (tool.Tool, bool),
	workspace *work.Space,
	streamingMode output.StreamingMode,
) *Picasso {
	self := &Picasso{
		screen:        screen,
		isRunning:     isRunning,
		getTool:       getTool,
		workspace:     workspace,
		streamingMode: streamingMode,
	}

	self.answer.streamingMode = streamingMode
	self.reasoning.streamingMode = streamingMode

	return self
}

func (self *Picasso) RenderReasoningAs(rendering output.ReasoningRendering) {
	self.reasoningRendering = rendering
}

func (self *Picasso) DrawPicturesFrom(display pictures.Display) {
	self.pictures = display
	self.pictureDrawer = pictures.NewDrawer(display, self.workspace.GetDir())
}

func (self *Picasso) LinkToolResults(sessionName string) {
	self.resultLinkSessionName = sessionName
}

func (self *Picasso) SuggestForkingWith(modelName string) {
	self.forkModelName = modelName
}

func (self *Picasso) DrawDelta(delta agent.Delta) {
	self.drawDeltaWithAnswerRendererReset(delta, true)
}

func (self *Picasso) DrawRestoredDelta(delta agent.Delta, previous *Picasso) {
	self.answerRenderer = previous.answerRenderer
	self.drawDeltaWithAnswerRendererReset(delta, false)
}

func (self *Picasso) DrawEvent(event agent.Event) {
	if !isJoinableNotice(event) {
		self.screen.SealOpenPanel()
	}

	switch {
	case event.Kind == agent.ModelReasoningEvent && self.previousKind == agent.ModelReasoningEvent && self.reasoning.Len() == 0:
		self.screen.End()
	case event.Kind == agent.ModelMessageEvent && self.previousKind == agent.ModelMessageEvent && self.answer.Len() == 0:
		self.screen.Blank()
	}
	self.previousKind = event.Kind

	if event.Kind != agent.ModelReasoningEvent && event.Kind != agent.ModelMessageEvent {
		self.discardProvisionalReasoning()
		self.settleAnswer()
		self.answer.Reset()
	}

	switch event.Kind {
	case agent.UserMessageEvent:
		self.discardProvisionalReasoning()
		self.answer.Reset()
		self.drawSubmitted(submittedMessage{text: event.Text, kind: userSubmission})

	case agent.ModelReasoningEvent:
		self.answer.Reset()
		self.reasoning.Reset()
		self.reasoning.Write(event.Text)
		self.drawReasoning(true)
		self.screen.Seal()
		self.reasoning.Reset()

	case agent.ModelMessageEvent:
		self.discardProvisionalReasoning()
		self.answer.Reset()
		self.answer.Write(event.Text)
		renderedAnswer := markdown.RenderWith(self.answer.Text(), self.answerOptions())
		if !self.screen.DrawAnswer(renderedAnswer) {
			self.isStale = true
		}
		self.screen.Seal()
		self.answer.Reset()

	case agent.ToolCallRequestEvent:
		if self.toolBlock == nil {
			self.toolBlock = dynamic.NewBlock(self.screen.Refresh)
			self.screen.OpenTool(self.toolBlock)
			self.rows = map[string]int{}
			self.labels = map[string]call.Label{}
		}

		roots := self.linkRoots()
		label := call.LabelFor(event, self.getTool, self.workspace).WithHostPathAliases(roots)
		if self.screen.IsTerminal() {
			label.PathRoots = roots
		}
		self.rows[event.ID] = self.toolBlock.Add(label, label.TimeLimit)
		self.labels[event.ID] = label

	case agent.ToolCallResultEvent:
		self.mark(event)

	case agent.StartupEvent:
		self.screen.Line(self.render(event))

	case agent.SilentTurnEvent:
		self.screen.Line(style.StoppedTurn(agent.SilentTurnNotice))

	case agent.PrefixRewriteEvent:
		self.screen.Line(style.Failure(agent.PrefixRewriteNotice + event.Text))

	case agent.CacheRebuildEvent:
		self.screen.Line(style.Change(agent.CacheRebuildNotice(event)))

	case portgrant.HostToSandboxChange, hostcommand.Ran, jobrecord.Ended:
		self.drawNotices(event, self.drawSubmittedBeforeResult)

	case caps.ModeChange, caps.JobStop, portgrant.SandboxToHostChange, jobrecord.EndedWithSession,
		conditions.Change, pathgrant.Change, turn.HarnessPoke:
		self.drawNotices(event, self.drawSubmitted)

	case agent.RetryingEvent:
		self.Close(dynamic.Cancelled)
		self.screen.Line(style.StoppedTurn(RenderRetry(event)))

	case agent.FailureEvent:
		self.Close(dynamic.Cancelled)
		self.screen.Line(style.Failure(RenderFailure(event)))
		if notice, isSaid := RenderContextExceeded(event, self.forkModelName); isSaid {
			self.screen.Line(style.StoppedTurn(notice))
		}

	case agent.InterruptionEvent:
		if interrupt.IsAnnounced(event) {
			self.Close(dynamic.Cancelled)
			self.screen.Line(style.StoppedTurn(interrupt.Notice(event)))
		}

	case agent.StateChangeEvent:
	}
}

func (self *Picasso) ProvisionalDelta() agent.Delta {
	if self.reasoning.Len() > 0 {
		return agent.Delta{Kind: agent.ModelReasoningEvent, Text: self.reasoning.String()}
	}

	return agent.Delta{Kind: agent.ModelMessageEvent, Text: self.answer.String()}
}

func RenderRetry(event agent.Event) string {
	notice := "[#" + strconv.Itoa(event.Attempt) + "] Request failed"

	if event.Took > 0 {
		notice += "; retrying in " + util.CompactDuration(event.Took)
	} else {
		notice += "; retrying"
	}

	if failure := agent.FailureText(event); failure != "" {
		notice += ": " + strutil.Capitalise(strutil.Flatten(strutil.FirstLine(failure)))
	}

	if event.Arguments != "" {
		notice += ": " + width.Elide(strutil.Flatten(event.Arguments), retryArgumentsCells)
	}

	return notice
}

func RenderSubmittedMessage(text string, columns int) string {
	return renderSubmittedMessage(submittedMessage{text: text}, columns, false, link.Roots{})
}

func RenderSubmittedMessageWithHyperlinks(text string, columns int) string {
	return renderSubmittedMessage(submittedMessage{text: text}, columns, true, link.Roots{})
}

func renderSubmittedMessage(
	message submittedMessage,
	columns int,
	shouldRenderHyperlinks bool,
	roots link.Roots,
) string {
	content := submittedContentRows(message, columns, shouldRenderHyperlinks, roots)

	return strings.Join(frameSubmitted("", content, columns, message.background()), "\n")
}

func submittedContentRows(
	message submittedMessage,
	columns int,
	shouldRenderHyperlinks bool,
	roots link.Roots,
) []string {
	marker := message.marker()
	contentColumns := columns
	if contentColumns > 1 {
		contentColumns--
	}

	markerWidth := width.Of(marker)
	if markerWidth > 0 && contentColumns > markerWidth {
		contentColumns -= markerWidth
	}

	var content []string
	if shouldRenderHyperlinks {
		content = markdown.RenderWithHyperlinksUnder(strutil.StripControl(message.text), contentColumns, roots)
	} else {
		content = markdown.Render(strutil.StripControl(message.text), contentColumns)
	}
	for i, row := range content {
		if shouldRenderHyperlinks && !roots.IsEmpty() {
			row = link.Render(row, roots)
		}

		prefix := " "
		switch {
		case marker == "":
		case i == 0:
			prefix += marker
		default:
			prefix += strings.Repeat(" ", markerWidth)
		}
		content[i] = prefix + row
	}

	return content
}

func frameSubmitted(head string, content []string, columns int, background style.Style) []string {
	rows := append([]string{head}, content...)
	rows = append(rows, "")

	for i, row := range rows {
		if room := columns - style.Width(row); room > 0 {
			row += strings.Repeat(" ", room)
		}

		rows[i] = background(row)
	}

	return rows
}

func NoticeStyle(severity agent.Status) style.Style {
	switch severity {
	case agent.InfoStatus:
		return style.Info
	case agent.SuccessStatus:
		return style.Success
	case agent.ErrorStatus:
		return style.Failure
	case agent.CancelledStatus:
		return style.CancelledCall
	case agent.WarningStatus, "":
		return style.StoppedTurn
	default:
		return style.Normal
	}
}

func getState(status agent.Status) dynamic.RowState {
	switch status {
	case agent.ErrorStatus:
		return dynamic.Failed
	case agent.CancelledStatus:
		return dynamic.Cancelled
	case agent.InfoStatus, agent.SuccessStatus, agent.WarningStatus:
		return dynamic.Done
	default:
		return dynamic.Done
	}
}

func RenderReasoning(thought string, columns int, rendering output.ReasoningRendering) []string {
	renderedRows := markdown.Render(thought, columns)

	if rendering == output.ReasoningMarkdown {
		for i, row := range renderedRows {
			renderedRows[i] = style.Reasoning.Over(row)
		}

		return renderedRows
	}

	plain := style.Plain(strings.Join(renderedRows, "\n"))
	strippedText := strings.Join(strings.Fields(plain), " ")

	return width.Wrap(style.Reasoning(strippedText), columns)
}

func (self *Picasso) Stale() bool { return self.isStale || self.screen.WasRepaintRefused() }

func (self *Picasso) HoldTiming() {
	if self.toolBlock != nil {
		self.toolBlock.HoldTiming()
	}
}

func (self *Picasso) ResumeTiming() {
	if self.toolBlock != nil {
		self.toolBlock.ResumeTiming()
	}
}

func (self *Picasso) Close(state dynamic.RowState) {
	self.discardProvisionalReasoning()
	self.settleAnswer()

	if self.toolBlock != nil {
		self.attachPicturesAbove(math.MaxInt)
		self.toolBlock.Close(state)
		self.toolBlock = nil
		self.rows = nil
		self.labels = nil

		self.screen.Seal()
	}
}

func (self *Picasso) End() {
	self.screen.End()
}

func (self *Picasso) Stop() {
	if self.toolBlock != nil {
		self.toolBlock.Stop()
		self.toolBlock = nil
		self.rows = nil
		self.labels = nil

		self.screen.Seal()
	}
}

func (self *Picasso) drawNotices(event agent.Event, draw func(submittedMessage)) {
	notices, areSaid := HarnessNotices(event)
	if !areSaid {
		return
	}

	for _, notice := range notices {
		draw(submittedMessage{text: notice, kind: sentHarnessSubmission})
	}
}

func isJoinableNotice(event agent.Event) bool {
	switch event.Kind {
	case caps.ModeChange, caps.JobStop, portgrant.SandboxToHostChange, portgrant.HostToSandboxChange,
		jobrecord.Ended, jobrecord.EndedWithSession, conditions.Change, pathgrant.Change, turn.HarnessPoke,
		hostcommand.Ran:
		return true
	case agent.StartupEvent, agent.UserMessageEvent, agent.SilentTurnEvent, agent.PrefixRewriteEvent,
		agent.CacheRebuildEvent, agent.ModelReasoningEvent, agent.ModelMessageEvent, agent.ToolCallRequestEvent,
		agent.ToolCallResultEvent, agent.StateChangeEvent, agent.InterruptionEvent,
		agent.RetryingEvent, agent.FailureEvent:
		return false
	}

	return false
}

func (self *Picasso) drawSubmitted(message submittedMessage) {
	self.Close(dynamic.Cancelled)

	if message.kind == sentHarnessSubmission {
		self.screen.Blank()
		self.drawStandalonePanel(message)
		return
	}

	self.drawSubmittedLine(message)
	self.screen.End()
	self.screen.Blank()
}

func (self *Picasso) drawSubmittedBeforeResult(message submittedMessage) {
	block, frame := self.submittedPanel(message)
	self.screen.Panel(block, frame)
}

func (self *Picasso) drawStandalonePanel(message submittedMessage) {
	block, frame := self.submittedPanel(message)
	self.screen.StandalonePanel(block, frame)
}

func (self *Picasso) submittedPanel(message submittedMessage) (output.Block, output.Frame) {
	return submittedRows{
			render: func(columns int) []string { return self.submittedContent(message, columns) },
		},
		func(content []string, columns int) []string {
			return frameSubmitted("", content, columns, message.background())
		}
}

type submittedRows struct {
	render func(columns int) []string
}

func (self submittedRows) Rows(columns int) []string {
	if columns <= 0 {
		return self.render(columns)
	}

	var rows []string
	for _, row := range self.render(columns) {
		rows = append(rows, width.Wrap(row, columns)...)
	}

	return rows
}

func (self *Picasso) submittedContent(message submittedMessage, columns int) []string {
	if !self.screen.IsTerminal() {
		return submittedContentRows(message, columns, false, link.Roots{})
	}

	return submittedContentRows(message, columns, true, self.linkRoots().WithoutScratch())
}

func (self *Picasso) drawSubmittedLine(message submittedMessage) {
	self.screen.Blank()

	if message.kind == userSubmission {
		self.screen.MarkedLine(self.renderSubmitted(message))
		return
	}

	self.screen.Line(self.renderSubmitted(message))
}

func (self *Picasso) renderSubmitted(message submittedMessage) string {
	if !self.screen.IsTerminal() {
		return renderSubmittedMessage(message, self.screen.Columns(), false, link.Roots{})
	}

	return renderSubmittedMessage(message, self.screen.Columns(), true, self.linkRoots().WithoutScratch())
}

func (self *Picasso) drawDeltaWithAnswerRendererReset(delta agent.Delta, shouldResetAnswerRenderer bool) {
	switch delta.Kind { //nolint:exhaustive // Only model prose event kinds can be deltas.
	case agent.ModelReasoningEvent:
		if self.reasoning.Len() == 0 && self.previousKind == agent.ModelReasoningEvent {
			self.screen.End()
		}
		self.reasoning.Write(delta.Text)
		if self.reasoning.IsDue() {
			self.drawReasoning(false)
		}

	case agent.ModelMessageEvent:
		self.discardProvisionalReasoning()
		if self.answer.Len() == 0 {
			if shouldResetAnswerRenderer {
				self.answerRenderer.Reset()
			}
			if self.previousKind == agent.ModelMessageEvent {
				self.screen.Blank()
			}
		}
		self.answer.Write(delta.Text)
		if self.answer.IsDue() {
			self.drawAnswer(false)
		}
	}
}

func (self *Picasso) settleAnswer() {
	if self.answer.IsOwed() {
		self.drawAnswer(true)
	}
}

func (self *Picasso) drawReasoning(isSettled bool) {
	thought, isRowArriving := self.withoutArrivingTableRow(self.reasoning.Text(), isSettled)
	rows := RenderReasoning(thought, self.screen.Columns(), self.reasoningRendering)
	if self.screen.IsTerminal() {
		roots := self.linkRoots()
		for i := range rows {
			rows[i] = link.Render(rows[i], roots)
		}
	}

	isTailHidden := !isSettled && self.streamingMode == output.StreamingModeLine
	if isTailHidden {
		rows = self.reasoning.WithoutLastRow(rows)
	}

	if !self.screen.DrawReasoning(self.reasoning.Take(rows, isTailHidden || isRowArriving)) {
		self.isStale = true
	}
}

func (self *Picasso) drawAnswer(isSettled bool) {
	answerText, isRowArriving := self.withoutArrivingTableRow(self.answer.Text(), isSettled)

	rows := self.answerRenderer.RenderWith(answerText, self.answerOptions())

	isTailHeldBack := !isRowArriving && self.isTailHeldBack(isSettled)
	if isTailHeldBack {
		rows = self.answer.WithoutLastRow(rows)
	}

	if !self.screen.DrawAnswer(self.answer.Take(rows, isTailHeldBack || isRowArriving)) {
		self.isStale = true
	}
}

func (self *Picasso) linkRoots() link.Roots {
	roots := self.screen.LinkRoots()
	if roots.Workspace == "" {
		roots.Workspace = self.workspace.GetDir()
	}

	return roots
}

func (self *Picasso) answerOptions() markdown.Options {
	options := markdown.Options{
		Columns:  self.screen.Columns(),
		Pictures: self.pictureDrawer,
	}

	if self.screen.IsTerminal() {
		options.ShouldRenderHyperlinks = true
		options.LinkRoot = self.linkRoots()
	}

	return options
}

func (self *Picasso) isTailHeldBack(isSettled bool) bool {
	if isSettled || self.streamingMode != output.StreamingModeLine {
		return false
	}

	return !strings.HasSuffix(self.answer.String(), "\n") && !self.answerRenderer.IsTailMermaid()
}

func (self *Picasso) withoutArrivingTableRow(text string, isSettled bool) (string, bool) {
	if isSettled || self.streamingMode != output.StreamingModeLine {
		return text, false
	}

	settledEnd := strings.LastIndex(text, "\n") + 1
	arrivingRow := text[settledEnd:]

	if !strings.Contains(arrivingRow, "|") || !markdown.EndsWithTable(text[:settledEnd]) {
		return text, false
	}

	return text[:settledEnd], true
}

func (self *Picasso) discardProvisionalReasoning() {
	if self.reasoning.Len() == 0 {
		return
	}

	if !self.screen.DiscardLive() {
		self.isStale = true
	}
	self.reasoning.Reset()
}

func (self *Picasso) mark(event agent.Event) {
	if self.toolBlock == nil {
		return
	}

	index, isKnown := self.rows[event.ID]
	if !isKnown {
		return
	}

	delete(self.rows, event.ID)
	label := self.labels[event.ID]
	delete(self.labels, event.ID)
	if self.resultLinkSessionName != "" {
		label.ResultURI = toolresult.URL(self.resultLinkSessionName, event.ID)
	}

	self.toolBlock.FinaliseRowWithLabel(
		index,
		label,
		getState(event.Status),
		event.Took,
		call.Summary(event),
		call.Measurements(event.Metrics),
	)

	self.holdPicture(index, event)
	self.attachSettledPictures()

	if len(self.rows) == 0 {
		self.Close(dynamic.Done)
	}
}

func (self *Picasso) attachSettledPictures() {
	self.attachPicturesAbove(self.firstRunningRow())
}

func (self *Picasso) firstRunningRow() int {
	firstRow := math.MaxInt

	for _, index := range self.rows {
		firstRow = min(firstRow, index)
	}

	return firstRow
}

func (self *Picasso) attachPicturesAbove(firstRunningRow int) {
	if self.toolBlock == nil {
		return
	}

	var picturesAbove []heldPicture
	var picturesBelow []heldPicture

	for _, entry := range self.heldPictures {
		if entry.index < firstRunningRow {
			picturesAbove = append(picturesAbove, entry)
		} else {
			picturesBelow = append(picturesBelow, entry)
		}
	}

	if len(picturesAbove) == 0 {
		return
	}

	self.heldPictures = picturesBelow

	slices.SortFunc(picturesAbove, func(one heldPicture, other heldPicture) int {
		return one.index - other.index
	})

	self.screen.Sync(func() {
		for _, entry := range picturesAbove {
			self.toolBlock.AttachPicture(entry.index, entry.picture)
		}
	})
}

func (self *Picasso) holdPicture(index int, event agent.Event) {
	if event.Picture == nil || self.pictures.SessionDirectory == "" || !self.screen.IsTerminal() {
		return
	}

	drawing, isStored := pictures.Prepare(self.pictures.SessionDirectory, event.Picture)
	if !isStored {
		return
	}

	picture := dynamic.Picture{
		Path:       drawing.Path,
		Width:      drawing.Width,
		Height:     drawing.Height,
		CellWidth:  self.pictures.CellWidth,
		CellHeight: self.pictures.CellHeight,
		IsLocal:    self.pictures.IsLocal,
	}

	if !self.pictures.IsLocal {
		data, isRead := pictures.Read(drawing.Path)
		if !isRead {
			return
		}
		picture.Data = data
	}

	self.heldPictures = append(self.heldPictures, heldPicture{index: index, picture: picture})
}

func (self *Picasso) render(event agent.Event) string {
	if event.Kind == agent.StartupEvent {
		return startup.RenderEvent(event, self.screen.Columns(), self.screen.IsTextSizingSupported())
	}

	return NoticeStyle(event.Status)(strutil.CapitaliseSentence(strutil.PrintableLines(event.Text)))
}

func RenderFailure(event agent.Event) string {
	return strutil.CapitaliseSentence(strutil.PrintableLines(agent.FailureText(event)))
}

const (
	contextExceededNotice = "Context window full."
	forkCommand           = "/fork"
)

func RenderContextExceeded(event agent.Event, modelName string) (string, bool) {
	if event.Failure == nil || !event.Failure.IsContextExceeded() {
		return "", false
	}

	notice := contextExceededNotice
	if modelName != "" {
		notice += " Run " + forkCommand + " " + modelName + " to continue in a new session."
	}

	return notice, true
}
