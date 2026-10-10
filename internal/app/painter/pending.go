package painter

import (
	"slices"
	"strings"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/environment"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/jobrecord"
	"crdx.org/oh/internal/app/link"
	"crdx.org/oh/internal/app/markdown"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
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
	mark string
}

func (self submittedMessage) marker() string {
	switch self.kind {
	case pendingHarnessSubmission:
		return unsentMark + " "
	case sentHarnessSubmission:
		if self.mark != "" {
			return self.mark + " "
		}
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

type Queue struct {
	Rows []string
	Hint string
}

func RenderQueue(
	messages []string,
	notices []string,
	canBeSentNow bool,
	columns int,
	shouldRenderHyperlinks bool,
	roots link.Roots,
) Queue {
	if len(messages)+len(notices) == 0 {
		return Queue{}
	}

	rows := make([]string, 0, len(notices)+len(messages))
	for _, notice := range notices {
		summary := summariseQueuedMessage(notice, shouldRenderHyperlinks, roots)
		rows = append(rows, style.Subtle.Over(width.Elide(unsentMark+" "+summary, columns)))
	}
	for _, message := range messages {
		summary := summariseQueuedMessage(message, shouldRenderHyperlinks, roots)
		rows = append(rows, width.Elide(unsentMark+" "+summary, columns))
	}

	hint := sendHint
	if !canBeSentNow {
		hint = stoppingHint
	}

	return Queue{Rows: rows, Hint: hint}
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

func renderHintRow(hint string, columns int) string {
	room := columns - width.Of(hint) - 1
	if room < 1 {
		return ""
	}

	return strings.Repeat(" ", room) + style.Subtle(hint)
}

type PendingMessages struct {
	messages               []Notice
	pathRoots              link.Roots
	kind                   submissionKind
	shouldRenderHyperlinks bool
}

func NewPendingMessages(messages []Notice, shouldRenderHyperlinks bool, pathRoots link.Roots) *PendingMessages {
	return &PendingMessages{
		messages:               slices.Clone(messages),
		pathRoots:              pathRoots,
		kind:                   pendingHarnessSubmission,
		shouldRenderHyperlinks: shouldRenderHyperlinks,
	}
}

func (self *PendingMessages) Replace(messages []Notice) {
	self.messages = slices.Clone(messages)
}

func (self *PendingMessages) MarkSent() {
	self.kind = sentHarnessSubmission
}

func (self *PendingMessages) Rows(columns int) []string {
	if len(self.messages) == 0 {
		return nil
	}

	parts := make([]output.StackPart, 0, len(self.messages))
	for _, message := range self.messages {
		parts = append(parts, output.StackPart{
			Rows: submittedContentRows(
				submittedMessage{text: message.Text, kind: self.kind, mark: message.Mark},
				columns,
				self.shouldRenderHyperlinks,
				self.pathRoots,
			),
			IsLoose: isLooseNotice(message.Text),
		})
	}

	content := output.Stack(parts)

	return frameSubmitted("", content, self.sendHintRow(columns), columns, style.Harness)
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
	case subagentrecord.ReportsDelivered:
		return oneNotice(event.Text, true)
	case subagentrecord.AccessWithdrawnStop:
		return oneNotice(subagentrecord.AccessWithdrawnStopNotice(event))
	case jobrecord.EndedWithSession:
		return oneNotice(jobrecord.EndedWithSessionNotice(event))
	case hostcommand.Ran:
		return oneNotice(hostcommand.Notice(event))
	case conditions.Change:
		return conditions.Notice(event)
	case environment.Change:
		return environment.Notice(event)
	case toolset.AvailabilityChange:
		return toolset.AvailabilityNotice(event)
	case pathgrant.Change:
		return oneNotice(pathgrant.Notice(event))
	case portgrant.ForwardChange:
		return oneNotice(portgrant.ForwardNotice(event))
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

type Notice struct {
	Text string
	Mark string
}

func DrawnNotices(event agent.Event) ([]Notice, bool) {
	if reports, isReported := subagentrecord.ReportsOf(event); isReported {
		notices := make([]Notice, len(reports))
		for index, report := range reports {
			notices[index] = Notice{Text: report.Notice(), Mark: session.Emoji(report.Name)}
		}
		return notices, true
	}
	texts, areSaid := HarnessNotices(event)
	notices := make([]Notice, len(texts))
	for index, text := range texts {
		notices[index] = Notice{Text: text}
	}
	return notices, areSaid
}

func HarnessNoteKind(kind agent.Kind) agent.NoteKind {
	switch kind {
	case caps.JobStop, jobrecord.Ended, jobrecord.EndedWithSession:
		return agent.JobNote
	case hostcommand.Ran:
		return agent.HostCommandNote
	case turn.HarnessPoke:
		return agent.PokeNote
	case subagentrecord.ReportsDelivered, subagentrecord.AccessWithdrawnStop:
		return agent.SubagentNote
	case caps.ModeChange, conditions.Change, environment.Change, toolset.AvailabilityChange, pathgrant.Change,
		portgrant.ForwardChange:
		return agent.EnvironmentNote
	case agent.StartupEvent, agent.UserMessageEvent, agent.SilentTurnEvent, agent.CacheRebuildEvent, agent.PrefixRewriteEvent,
		agent.ModelReasoningEvent, agent.ModelMessageEvent, agent.ToolCallRequestEvent,
		agent.ToolCallResultEvent, agent.StateChangeEvent, agent.InterruptionEvent,
		agent.RetryingEvent, agent.FailureEvent:
		return ""
	}
	return ""
}

func oneNotice(notice string, isSaid bool) ([]string, bool) {
	if !isSaid {
		return nil, false
	}

	return []string{notice}, true
}
