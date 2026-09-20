package transcript

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/conditions"
	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/interrupt"
	"crdx.org/oh/internal/app/jobrecord"
	"crdx.org/oh/internal/app/pathgrant"
	"crdx.org/oh/internal/app/portgrant"
	"crdx.org/oh/internal/app/turn"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

const (
	formattedBytePrecision = 3
	maximumLogSubject      = 80

	partSeparator = " · "
	logSeparator  = ", "

	patience    = 5 * time.Second
	noTimeAtAll = "0s"

	containmentCanary = "2e49acc8-627d-4ba6-b7b6-50f24a4aeb2b"
)

var markdownSafetyParser = goldmark.New().Parser()

type Meta struct {
	Name, Model, Effort, Provider, Workspace string
	StartedAt                                time.Time
}

type Recorder struct {
	file          *os.File
	startedAt     time.Time
	pendingCalls  []*toolCallEntry
	pendingCallAt time.Time
	callByID      map[string]*toolCallEntry
}

type toolCallEntry struct {
	id           string
	name         string
	subject      string
	hasResult    bool
	outcome      string
	measurements string
}

func Open(path string, meta Meta) (*Recorder, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the parent store supplies the fixed bundle path
	if err != nil {
		return nil, err
	}
	recorder := &Recorder{file: file, startedAt: meta.StartedAt}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Size() == 0 {
		_, err = fmt.Fprintf(
			file,
			"# Conversation\n\n- **Session:** `%s`\n- **Started:** `%s`\n- **Model:** `%s`\n- **Effort:** `%s`\n- **Provider:** `%s`\n- **Workspace:** `%s`\n- **Tool detail:** `jq 'select(.event.id == \"<id>\")' session.jsonl`, for the `[id]` of any call\n",
			meta.Name, meta.StartedAt.UTC().Format(time.RFC3339Nano), meta.Model, meta.Effort, meta.Provider, meta.Workspace,
		)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return recorder, nil
}

func (self *Recorder) Event(at time.Time, event agent.Event) error {
	if self.file == nil {
		return os.ErrClosed
	}

	switch event.Kind {
	case agent.StateChangeEvent, agent.ModelReasoningEvent:
		return nil
	case agent.ToolCallRequestEvent:
		self.bufferToolCall(at, event)
		return nil
	case agent.ToolCallResultEvent:
		self.bufferToolResult(at, event)
		return nil
	case agent.StartupEvent, agent.UserMessageEvent, agent.ModelMessageEvent,
		agent.InterruptionEvent, agent.RetryingEvent, agent.FailureEvent, agent.SilentTurnEvent,
		agent.CacheRebuildEvent, agent.PrefixRewriteEvent:
	}

	if err := self.flushToolCalls(); err != nil {
		return err
	}

	var output document
	output.paragraph("## " + joinParts(append(heading(event), self.offset(at))...))

	switch event.Kind {
	case agent.UserMessageEvent, agent.ModelMessageEvent:
		if event.Text != "" {
			output.markdown(event.Text)
		}
	case agent.FailureEvent:
		output.fence(agent.FailureText(event))
	case agent.SilentTurnEvent:
		output.fence(agent.SilentTurnNotice)
	case agent.CacheRebuildEvent:
		output.fence(agent.CacheRebuildNotice(event))
	case agent.PrefixRewriteEvent:
		output.fence(agent.PrefixRewriteNotice + event.Text)
	case turn.HarnessPoke, jobrecord.Ended, jobrecord.EndedWithSession, caps.JobStop, caps.ModeChange,
		conditions.Change, pathgrant.Change, portgrant.SandboxToHostChange, portgrant.HostToSandboxChange,
		hostcommand.Ran:
		if notice, isSaid := harnessNotice(event); isSaid {
			output.fence(notice)
		}
	case agent.InterruptionEvent:
		output.paragraph(interrupt.Notice(event))
	case agent.RetryingEvent:
		output.fence(agent.FailureText(event))
		if event.Arguments != "" {
			output.fence(event.Arguments)
		}
	case agent.StartupEvent, agent.ModelReasoningEvent, agent.ToolCallRequestEvent,
		agent.ToolCallResultEvent, agent.StateChangeEvent:
	}

	_, err := self.file.WriteString(output.String())
	return err
}

func harnessNotice(event agent.Event) (string, bool) {
	switch event.Kind {
	case turn.HarnessPoke:
		return turn.PokeNotice(event)
	case jobrecord.Ended:
		return jobrecord.EndedNotice(event)
	case jobrecord.EndedWithSession:
		return jobrecord.EndedWithSessionNotice(event)
	case caps.JobStop:
		return caps.JobStopNotice(event)
	case hostcommand.Ran:
		return hostcommand.Notice(event)
	case caps.ModeChange:
		return joined(caps.ModeNotice(event))
	case conditions.Change:
		return joined(conditions.Notice(event))
	case pathgrant.Change:
		return pathgrant.Notice(event)
	case portgrant.SandboxToHostChange:
		return portgrant.SandboxToHostNotice(event)
	case portgrant.HostToSandboxChange:
		return portgrant.HostToSandboxNotice(event)
	case agent.StartupEvent, agent.UserMessageEvent, agent.SilentTurnEvent, agent.PrefixRewriteEvent,
		agent.CacheRebuildEvent, agent.ModelReasoningEvent, agent.ModelMessageEvent,
		agent.ToolCallRequestEvent, agent.ToolCallResultEvent, agent.StateChangeEvent,
		agent.InterruptionEvent, agent.RetryingEvent, agent.FailureEvent:
		return "", false
	}

	return "", false
}

func joined(notices []string, areSaid bool) (string, bool) {
	if !areSaid {
		return "", false
	}

	return strings.Join(notices, "\n"), true
}

func (self *toolCallEntry) line() string {
	line := self.name
	if self.subject != "" {
		line += ": " + self.subject
	}
	brackets := joinWith(logSeparator, self.outcome, self.measurements, self.id)
	if brackets != "" {
		line += " [" + brackets + "]"
	}

	return line
}

func heading(event agent.Event) []string {
	name := title(event.Kind)
	if event.Name != "" && event.Kind == agent.RetryingEvent {
		name += " — " + event.Name
	}

	switch event.Kind {
	case caps.ModeChange:
		return []string{name, modeFlags(event), prefixed("toggled ", event.Name)}
	case conditions.Change:
		summary, _ := conditions.Summary(event)
		return []string{name, summary}
	case pathgrant.Change:
		summary, _ := pathgrant.Summary(event)
		return []string{name, summary, prefixed("changed ", event.Name)}
	case portgrant.SandboxToHostChange:
		summary, _ := portgrant.SandboxToHostSummary(event)
		return []string{name, summary, prefixed("changed host port ", event.Name)}
	case portgrant.HostToSandboxChange:
		summary, _ := portgrant.HostToSandboxSummary(event)
		return []string{name, summary, prefixed("changed port ", event.Name)}
	case agent.RetryingEvent:
		return []string{name, "attempt " + strconv.Itoa(event.Attempt), prefixed("waited ", util.CompactDuration(event.Took))}
	case agent.StartupEvent, agent.UserMessageEvent, agent.ModelMessageEvent, agent.InterruptionEvent, agent.FailureEvent,
		agent.ModelReasoningEvent, agent.ToolCallRequestEvent, agent.ToolCallResultEvent, agent.StateChangeEvent,
		agent.SilentTurnEvent, agent.CacheRebuildEvent, agent.PrefixRewriteEvent:
	}

	return []string{name}
}

func logSubject(subject string) string {
	subject = strings.Join(strings.Fields(subject), " ")
	runes := []rune(subject)
	if len(runes) > maximumLogSubject {
		return string(runes[:maximumLogSubject]) + "…"
	}

	return subject
}

func compactStatus(event agent.Event) string {
	status := string(event.Status)
	if event.Status == agent.SuccessStatus {
		status = "ok"
	}
	if event.Took >= patience {
		status = strings.TrimSpace(status + " in " + util.CompactDuration(event.Took))
	}

	return status
}

func measurements(metrics *tool.ToolCallMetrics) string {
	if metrics == nil {
		return ""
	}

	var parts []string
	if metrics.Lines > 0 {
		parts = append(parts, plural(metrics.Lines, "line"))
	}
	if metrics.Bytes > 0 {
		size := util.FormatBytes(metrics.Bytes, formattedBytePrecision)
		if metrics.TotalBytes > metrics.Bytes {
			size += " of " + util.FormatBytes(metrics.TotalBytes, formattedBytePrecision)
		}
		parts = append(parts, size)
	}
	if metrics.AddedLines > 0 || metrics.RemovedLines > 0 {
		parts = append(parts, fmt.Sprintf("+%d −%d", metrics.AddedLines, metrics.RemovedLines))
	}
	if metrics.EstimatedTokens > 0 {
		parts = append(parts, util.FormatEstimatedTokens(metrics.EstimatedTokens))
	}
	if cpuTime := util.CompactDuration(metrics.CPUTime); metrics.CPUTime > 0 && cpuTime != noTimeAtAll {
		parts = append(parts, cpuTime+" CPU")
	}
	if metrics.PeakMemory > 0 {
		parts = append(parts, util.FormatBytes(metrics.PeakMemory, formattedBytePrecision)+" peak")
	}
	if metrics.IsTruncated {
		parts = append(parts, "truncated")
	}

	return strings.Join(parts, logSeparator)
}

func joinParts(parts ...string) string {
	return joinWith(partSeparator, parts...)
}

func joinWith(separator string, parts ...string) string {
	var present []string
	for _, part := range parts {
		if part != "" {
			present = append(present, part)
		}
	}

	return strings.Join(present, separator)
}

func prefixed(prefix string, value string) string {
	if value == "" {
		return ""
	}

	return prefix + value
}

func plural(count int64, noun string) string {
	if count == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", count, noun)
}

func (self *Recorder) Close() error {
	if self.file == nil {
		return nil
	}
	flushError := self.flushToolCalls()
	closeError := self.file.Close()
	self.file = nil
	return errors.Join(flushError, closeError)
}

func (self *Recorder) bufferToolCall(at time.Time, event agent.Event) {
	self.markPendingCallRun(at)
	entry := &toolCallEntry{id: event.ID, name: event.Name, subject: logSubject(event.Subject)}
	self.pendingCalls = append(self.pendingCalls, entry)
	if event.ID == "" {
		return
	}
	if self.callByID == nil {
		self.callByID = map[string]*toolCallEntry{}
	}
	self.callByID[event.ID] = entry
}

func (self *Recorder) bufferToolResult(at time.Time, event agent.Event) {
	entry := self.callByID[event.ID]
	if entry == nil {
		self.markPendingCallRun(at)
		entry = &toolCallEntry{id: event.ID, name: event.Name}
		self.pendingCalls = append(self.pendingCalls, entry)
	}
	entry.hasResult = true
	entry.outcome = compactStatus(event)
	entry.measurements = measurements(event.Metrics)
	delete(self.callByID, event.ID)
}

func (self *Recorder) markPendingCallRun(at time.Time) {
	if len(self.pendingCalls) == 0 {
		self.pendingCallAt = at
	}
}

func (self *Recorder) flushToolCalls() error {
	entries := self.pendingCalls
	at := self.pendingCallAt
	self.pendingCalls = nil
	self.callByID = nil
	if len(entries) == 0 {
		return nil
	}

	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.line())
	}

	var output document
	output.paragraph("## " + joinParts("Tool calls", self.offset(at)))
	output.fence(strings.Join(lines, "\n"))
	_, err := self.file.WriteString(output.String())
	return err
}

func (self *Recorder) offset(at time.Time) string {
	if self.startedAt.IsZero() {
		return ""
	}

	return "+" + util.CompactDuration(at.Sub(self.startedAt))
}

func modeFlags(event agent.Event) string {
	grantedCaps, err := caps.GrantedBy(event)
	if err != nil {
		return ""
	}

	return grantedCaps.Flags()
}

func title(kind agent.Kind) string {
	switch kind {
	case agent.StartupEvent:
		return "Startup"
	case agent.UserMessageEvent:
		return "User"
	case agent.ModelReasoningEvent:
		return "Reasoning"
	case agent.ModelMessageEvent:
		return "Assistant"
	case agent.ToolCallRequestEvent:
		return "Tool call"
	case agent.ToolCallResultEvent:
		return "Tool result"
	case caps.ModeChange:
		return "Mode"
	case conditions.Change:
		return "Conditions"
	case pathgrant.Change:
		return "Path grant"
	case portgrant.SandboxToHostChange:
		return "Sandbox → host"
	case portgrant.HostToSandboxChange:
		return "Host → sandbox"
	case turn.HarnessPoke:
		return "Poke"
	case hostcommand.Ran:
		return "Host command"
	case agent.SilentTurnEvent:
		return "Silent turn"
	case agent.CacheRebuildEvent:
		return "Cache rebuild"
	case agent.PrefixRewriteEvent:
		return "Prefix rewrite"
	case agent.InterruptionEvent:
		return "Interrupted"
	case agent.RetryingEvent:
		return "Retrying"
	case agent.FailureEvent:
		return "Failure"
	case agent.StateChangeEvent:
		return string(kind)
	default:
		return string(kind)
	}
}

type document struct {
	text strings.Builder
}

func (self *document) String() string {
	return "\n" + self.text.String()
}

func (self *document) paragraph(text string) {
	self.openBlock()
	self.text.WriteString(strings.TrimRight(text, "\n"))
	self.text.WriteString("\n")
}

func (self *document) openBlock() {
	if self.text.Len() > 0 {
		self.text.WriteString("\n")
	}
}

func (self *document) fence(value string) {
	longest := 0
	for _, run := range strings.FieldsFunc(value, func(character rune) bool { return character != '`' }) {
		if len(run) > longest {
			longest = len(run)
		}
	}
	length := max(3, longest+1)
	fence := strings.Repeat("`", length)
	self.openBlock()
	fmt.Fprintf(&self.text, "%s\n%s\n%s\n", fence, strings.TrimRight(value, "\n"), fence)
}

func (self *document) markdown(value string) {
	if couldSwallowWhatFollows(value) {
		self.fence(value)
		return
	}
	self.paragraph(value)
}

func couldSwallowWhatFollows(value string) bool {
	probe := []byte(value + "\n\n" + containmentCanary + "\n")

	var isSurvived bool
	_ = ast.Walk(markdownSafetyParser.Parse(text.NewReader(probe)), func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if textNode, isText := node.(*ast.Text); entering && isText && string(textNode.Segment.Value(probe)) == containmentCanary {
			isSurvived = true
		}
		return ast.WalkContinue, nil
	})

	return !isSurvived
}
