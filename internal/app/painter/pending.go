package painter

import (
	"slices"
	"strings"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/jobrecord"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
)

const (
	unsentMark              = "⏳"
	harnessMark             = "🤖"
	unwrappedPreviewColumns = 1 << 16
	sendHint                = "double-enter to send now"
	stoppingHint            = "sending" + width.Ellipsis
)

type submissionKind uint8

const (
	userSubmission submissionKind = iota
	pendingHarnessSubmission
	sentHarnessSubmission
)

type submittedMessage struct {
	text string
	kind submissionKind
}

func (self submittedMessage) marker() string {
	switch self.kind {
	case pendingHarnessSubmission:
		return unsentMark + " "
	case sentHarnessSubmission:
		return harnessMark + " "
	case userSubmission:
		return ""
	}

	return ""
}

func (self submittedMessage) background() style.Style {
	if self.kind == userSubmission {
		return style.User
	}

	return style.Harness
}

func RenderQueuedMessages(
	messages []string,
	canBeSentNow bool,
	columns int,
	shouldRenderHyperlinks bool,
	roots link.Roots,
) []string {
	if len(messages) == 0 {
		return nil
	}

	hint := sendHint
	if !canBeSentNow {
		hint = stoppingHint
	}

	rows := make([]string, 0, len(messages)+2)
	rows = append(rows, frameQueuedRow(renderHintRow(hint, columns), columns))

	for _, message := range messages {
		summary := summariseQueuedMessage(message, shouldRenderHyperlinks, roots)
		rows = append(rows, renderQueuedRow(unsentMark+" "+summary, columns))
	}

	return append(rows, renderQueuedRow("", columns))
}

func summariseQueuedMessage(message string, shouldRenderHyperlinks bool, roots link.Roots) string {
	firstLine := strutil.FirstLine(message)

	var renderedLines []string
	if shouldRenderHyperlinks {
		renderedLines = markdown.RenderWithHyperlinksUnder(strutil.StripControl(firstLine), unwrappedPreviewColumns, roots)
	} else {
		renderedLines = markdown.Render(strutil.StripControl(firstLine), unwrappedPreviewColumns)
	}

	summary := firstLine
	if len(renderedLines) > 0 {
		summary = renderedLines[0]
	}

	if strings.Contains(strings.TrimSpace(message), "\n") {
		summary += width.Ellipsis
	}

	return summary
}

func renderQueuedRow(text string, columns int) string {
	row := ""
	if text != "" {
		row = width.Elide(" "+text, columns)
	}

	return frameQueuedRow(row, columns)
}

func frameQueuedRow(row string, columns int) string {
	if room := columns - style.Width(row); room > 0 {
		row += strings.Repeat(" ", room)
	}

	return style.User(row)
}

func renderHintRow(hint string, columns int) string {
	room := columns - width.Of(hint) - 1
	if room < 1 {
		return ""
	}

	return strings.Repeat(" ", room) + style.Subtle(hint)
}

type PendingMessages struct {
	messages               []string
	pathRoots              link.Roots
	kind                   submissionKind
	shouldRenderHyperlinks bool
}

func NewPendingMessages(messages []string, shouldRenderHyperlinks bool, pathRoots link.Roots) *PendingMessages {
	return &PendingMessages{
		messages:               slices.Clone(messages),
		pathRoots:              pathRoots,
		kind:                   pendingHarnessSubmission,
		shouldRenderHyperlinks: shouldRenderHyperlinks,
	}
}

func (self *PendingMessages) Replace(messages []string) {
	self.messages = slices.Clone(messages)
}

func (self *PendingMessages) MarkSent() {
	self.kind = sentHarnessSubmission
}

func (self *PendingMessages) Rows(columns int) []string {
	if len(self.messages) == 0 {
		return nil
	}

	var content []string
	for _, message := range self.messages {
		content = append(content, submittedContentRows(
			submittedMessage{text: message, kind: self.kind},
			columns,
			self.shouldRenderHyperlinks,
			self.pathRoots,
		)...)
	}

	return frameSubmitted(self.sendHintRow(columns), content, columns, style.Harness)
}

func (self *PendingMessages) sendHintRow(columns int) string {
	if self.kind != pendingHarnessSubmission {
		return ""
	}

	return renderHintRow(sendHint, columns)
}

func HarnessNotices(event agent.Event) ([]string, bool) {
	switch event.Kind {
	case caps.ModeChange:
		return caps.ModeNotice(event)
	case caps.JobStop:
		return oneNotice(caps.JobStopNotice(event))
	case jobrecord.Ended:
		return oneNotice(jobrecord.EndedNotice(event))
	case jobrecord.EndedWithSession:
		return oneNotice(jobrecord.EndedWithSessionNotice(event))
	case hostcommand.Ran:
		return oneNotice(hostcommand.Notice(event))
	case conditions.Change:
		return conditions.Notice(event)
	case pathgrant.Change:
		return oneNotice(pathgrant.Notice(event))
	case portgrant.SandboxToHostChange:
		return oneNotice(portgrant.SandboxToHostNotice(event))
	case portgrant.HostToSandboxChange:
		return oneNotice(portgrant.HostToSandboxNotice(event))
	case turn.HarnessPoke:
		return oneNotice(turn.PokeNotice(event))
	case agent.StartupEvent, agent.UserMessageEvent, agent.SilentTurnEvent, agent.CacheRebuildEvent, agent.PrefixRewriteEvent,
		agent.ModelReasoningEvent, agent.ModelMessageEvent, agent.ToolCallRequestEvent,
		agent.ToolCallResultEvent, agent.StateChangeEvent, agent.InterruptionEvent,
		agent.RetryingEvent, agent.FailureEvent:
		return nil, false
	}
	return nil, false
}

func oneNotice(notice string, isSaid bool) ([]string, bool) {
	if !isSaid {
		return nil, false
	}

	return []string{notice}, true
}
