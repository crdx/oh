package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"time"

	"crdx.org/oh/internal/stop"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/internal/waiting"
	"crdx.org/oh/pkg/tool"
)

func New(systemPrompt string, provider Provider, tools []tool.Tool) *Agent {
	return NewWithEnabledTools(systemPrompt, provider, tools, tools)
}

func NewWithEnabledTools(systemPrompt string, provider Provider, tools []tool.Tool, enabledTools []tool.Tool) *Agent {
	definitions := make([]tool.Definition, len(enabledTools))
	enabledNames := make(map[string]struct{}, len(enabledTools))
	for i, enabledTool := range enabledTools {
		definitions[i] = tool.Describe(enabledTool)
		enabledNames[enabledTool.Name()] = struct{}{}
	}

	availableTools := make(map[string]tool.Tool, len(tools))
	stateOwners := map[string]tool.Tool{}
	for _, availableTool := range tools {
		availableTools[availableTool.Name()] = availableTool
		if stateKey := availableTool.StateKey(); stateKey != "" {
			stateOwners[stateKey] = availableTool
		}
	}

	provider.Configure(systemPrompt, definitions)

	return &Agent{
		provider:         provider,
		registeredTools:  availableTools,
		enabledToolNames: enabledNames,
		owners:           stateOwners,
		cacheLifetime:    reportedCacheLifetime(provider),
		now:              time.Now,
	}
}

func (self *Agent) Dump() ([]json.RawMessage, error) {
	state, ok := self.provider.(State)
	if !ok {
		return nil, ErrNoState
	}

	items := state.Dump()
	if len(items) < len(self.state) {
		return nil, ErrStateReplaced
	}
	for i := range self.state {
		if !bytes.Equal(items[i], self.state[i]) {
			return nil, ErrStateReplaced
		}
	}

	self.state = cloneState(items)
	return cloneState(items), nil
}

func (self *Agent) Load(items []json.RawMessage) error {
	state, ok := self.provider.(State)
	if !ok {
		return ErrNoState
	}
	state.Load(cloneState(items))
	self.state = cloneState(items)
	return nil
}

func cloneState(items []json.RawMessage) []json.RawMessage {
	clonedItems := make([]json.RawMessage, len(items))
	for i, item := range items {
		clonedItems[i] = bytes.Clone(item)
	}
	return clonedItems
}

func (self *Agent) AddUserMessage(text string) {
	self.provider.AddUserMessage(text)
}

type proseStream struct {
	kind              Kind
	text              strings.Builder
	pendingEvent      *Event
	cacheUsage        *Usage
	hasReportedUsage  bool
	hasReportedOutput bool
	hasAnswered       bool
}

func (self *proseStream) startAttempt() {
	self.cacheUsage = nil
}

func (self *proseStream) takeAttemptUsage() (Usage, bool) {
	if self.cacheUsage == nil {
		return Usage{}, false
	}

	usage := *self.cacheUsage
	self.cacheUsage = nil

	return usage, true
}

func (self *proseStream) add(output Output) []Update {
	output.Text = strutil.StripControl(output.Text)

	if output.Usage != nil && output.Usage.Cache != nil && self.cacheUsage == nil {
		usage := *output.Usage
		self.cacheUsage = &usage
	}

	if output.Done {
		if self.kind != output.Kind || self.text.Len() == 0 {
			self.resetText()
			return nil
		}

		event := Event{Kind: self.kind, Text: self.text.String()}
		if self.kind == ModelMessageEvent {
			self.hasAnswered = true
		}
		if output.Usage != nil && !self.hasReportedUsage {
			event.Usage = output.Usage
			self.hasReportedUsage = true
			self.hasReportedOutput = output.Usage.OutputTokens > 0
		}
		self.resetText()
		if !output.AwaitUsage {
			return append(self.takePending(), Update{Event: &event})
		}

		self.pendingEvent = &event
		return nil
	}

	if output.Text == "" {
		return nil
	}

	updates := self.takePending()

	if self.kind != "" && self.kind != output.Kind {
		self.resetText()
	}

	self.kind = output.Kind
	self.text.WriteString(output.Text)
	delta := Delta{Kind: output.Kind, Text: output.Text}

	return append(updates, Update{Delta: &delta})
}

func (self *proseStream) finish(usage Usage) []Update {
	if self.pendingEvent != nil {
		if remainder, isLeft := self.unreported(usage); isLeft {
			self.pendingEvent.Usage = &remainder
		}
	}

	return self.takePending()
}

func (self *proseStream) unreported(usage Usage) (Usage, bool) {
	switch {
	case usage.InputTokens > 0 && !self.hasReportedUsage:
		self.hasReportedUsage = true
		self.hasReportedOutput = usage.OutputTokens > 0
		return usage, true
	case usage.OutputTokens > 0 && !self.hasReportedOutput:
		self.hasReportedOutput = true
		return Usage{OutputTokens: usage.OutputTokens}, true
	default:
		return Usage{}, false
	}
}

func (self *proseStream) interrupted() []Update {
	updates := self.takePending()
	if self.kind == ModelMessageEvent && self.text.Len() > 0 {
		event := Event{Kind: self.kind, Text: self.text.String()}
		self.hasAnswered = true
		updates = append(updates, Update{Event: &event})
	}
	self.resetText()

	return updates
}

func (self *proseStream) takePending() []Update {
	if self.pendingEvent == nil {
		return nil
	}

	update := Update{Event: self.pendingEvent}
	self.pendingEvent = nil
	return []Update{update}
}

func (self *proseStream) resetText() {
	self.kind = ""
	self.text.Reset()
}

const (
	SilentTurnNotice     = "Model returned no reply."
	PrefixRewriteNotice  = "Request prefix changed: "
	defaultCacheLifetime = 5 * time.Minute
)

type CacheCause string

const (
	CacheReopened CacheCause = "reopened"
	CacheExpired  CacheCause = "expired"
	CacheRebuilt  CacheCause = "rebuilt"
	CacheSettling CacheCause = "settling"
)

const cacheSettlingGap = 30 * time.Second

type CacheReading struct {
	ReadTokens int
	At         time.Time
}

func (self CacheReading) exists() bool {
	return !self.At.IsZero()
}

func CacheRebuildNotice(event Event) string {
	var rewrittenTokens int
	if event.Usage != nil && event.Usage.Cache != nil {
		rewrittenTokens = event.Usage.Cache.WriteTokens
	}

	tokens := util.FormatTokens(rewrittenTokens)
	gap := util.CoarseDuration(event.Took)

	switch CacheCause(event.Name) {
	case CacheReopened:
		return fmt.Sprintf("Cache gone: %s sent.", tokens)
	case CacheExpired:
		return fmt.Sprintf("Cache expired: %s sent after %s.", tokens, gap)
	case CacheSettling:
		return fmt.Sprintf("Cache unsettled: %s sent.", tokens)
	case CacheRebuilt:
		return fmt.Sprintf("Cache rebuilt: %s sent %s later.", tokens, gap)
	}

	return fmt.Sprintf("Cache rebuilt: %s sent.", tokens)
}

func cacheCause(gap time.Duration, lifetime time.Duration, previousRead int, rewrittenTokens int) CacheCause {
	switch {
	case gap >= lifetime:
		return CacheExpired
	case gap <= cacheSettlingGap && rewrittenTokens*100 < previousRead:
		return CacheSettling
	}

	return CacheRebuilt
}

func reportedCacheLifetime(provider Provider) time.Duration {
	if reporter, isReported := provider.(CacheLifetimeReporter); isReported {
		if lifetime := reporter.CacheLifetime(); lifetime > 0 {
			return lifetime
		}
	}

	return defaultCacheLifetime
}

func (self *Agent) CacheLifetime() time.Duration {
	return self.cacheLifetime
}

func (self *Agent) RestoreCache(cacheReading CacheReading) {
	if !cacheReading.exists() {
		return
	}

	self.cache = CacheReading{ReadTokens: cacheReading.ReadTokens, At: util.WallClock(cacheReading.At)}
}

func (self *Agent) readCache(usage Usage, at time.Time) (Event, bool) {
	if usage.Cache == nil {
		return Event{}, false
	}

	previous := self.cache
	self.cache = CacheReading{ReadTokens: usage.Cache.ReadTokens, At: util.WallClock(at)}

	if usage.Cache.WriteTokens == 0 || !previous.exists() {
		return Event{}, false
	}

	if usage.Cache.ReadTokens >= previous.ReadTokens {
		return Event{}, false
	}

	gap := util.WallClock(at).Sub(previous.At)

	return cacheRebuild(cacheCause(gap, self.cacheLifetime, previous.ReadTokens, usage.Cache.WriteTokens), gap, usage), true
}

func cacheRebuild(cause CacheCause, gap time.Duration, usage Usage) Event {
	return Event{
		Kind: CacheRebuildEvent,
		Name: string(cause),
		Took: gap,
		Usage: &Usage{
			Cache: &CacheUsage{ReadTokens: usage.Cache.ReadTokens, WriteTokens: usage.Cache.WriteTokens},
		},
	}
}

func (self *Agent) readAbandonedCache(prose *proseStream, askedAt time.Time, yieldEvent func(Event, error) bool) bool {
	usage, isReported := prose.takeAttemptUsage()
	if !isReported {
		return true
	}

	notice, wasRebuilt := self.readCache(usage, askedAt)
	if !wasRebuilt {
		return true
	}

	return yieldEvent(notice, nil)
}

func (self *Agent) Stream(ctx context.Context, message string, interjections *Interjections) iter.Seq2[Update, error] {
	return func(yield func(Update, error) bool) {
		yieldEvent := func(event Event, err error) bool {
			update := Update{}
			if err == nil {
				update.Event = &event
			}

			return yield(update, err)
		}
		yieldUpdates := func(updates []Update) bool {
			for _, update := range updates {
				if !yield(update, nil) {
					return false
				}
			}

			return true
		}

		if message != "" {
			self.provider.AddUserMessage(message)

			if !yieldEvent(Event{Kind: UserMessageEvent, Text: message}, nil) {
				return
			}
		}

		for {
			var prose proseStream

			reply, askedAt, isListening, err := self.send(ctx, &prose, yieldUpdates, yieldEvent)

			switch {
			case !isListening:
				self.answer(cancelledResults(ctx, reply.Calls))
				return
			case err != nil:
				if !yieldUpdates(prose.interrupted()) {
					return
				}
				if !self.readAbandonedCache(&prose, askedAt, yieldEvent) {
					return
				}
				yield(Update{}, err)
				return
			case len(reply.Calls) == 0:
				if !yieldUpdates(prose.finish(reply.Usage)) {
					return
				}
				if reply.PrefixRewrite != "" {
					yieldEvent(Event{Kind: PrefixRewriteEvent, Text: reply.PrefixRewrite}, nil)
				}
				if notice, wasRebuilt := self.readCache(reply.Usage, askedAt); wasRebuilt {
					yieldEvent(notice, nil)
				}
				if self.interjectNotes(ctx, interjections) {
					continue
				}
				if !prose.hasAnswered {
					yieldEvent(Event{Kind: SilentTurnEvent}, nil)
				}
				return
			}

			if !yieldUpdates(prose.finish(Usage{})) {
				self.answer(cancelledResults(ctx, reply.Calls))
				return
			}

			if reply.PrefixRewrite != "" {
				if !yieldEvent(Event{Kind: PrefixRewriteEvent, Text: reply.PrefixRewrite}, nil) {
					self.answer(cancelledResults(ctx, reply.Calls))
					return
				}
			}

			if notice, wasRebuilt := self.readCache(reply.Usage, askedAt); wasRebuilt {
				if !yieldEvent(notice, nil) {
					self.answer(cancelledResults(ctx, reply.Calls))
					return
				}
			}

			usage, isLeft := prose.unreported(reply.Usage)
			if !isLeft {
				usage = Usage{}
			}
			if !self.runCalls(ctx, reply.Calls, usage, yieldEvent) {
				return
			}

			if !self.interject(ctx, interjections, yieldEvent) {
				return
			}
		}
	}
}

func (self *Agent) interjectNotes(ctx context.Context, interjections *Interjections) bool {
	if ctx.Err() != nil {
		return false
	}

	note, isNoted := interjections.TakeNotes()
	if isNoted {
		self.provider.AddUserMessage(note)
	}

	return isNoted
}

func (self *Agent) interject(
	ctx context.Context,
	interjections *Interjections,
	yieldEvent func(Event, error) bool,
) bool {
	if ctx.Err() != nil {
		return false
	}

	self.interjectNotes(ctx, interjections)

	text, isQueued := interjections.Take()
	if !isQueued {
		return true
	}

	self.provider.AddUserMessage(text)

	return yieldEvent(Event{Kind: UserMessageEvent, Text: text}, nil)
}

func (self *Agent) send(
	ctx context.Context,
	prose *proseStream,
	yieldUpdates func([]Update) bool,
	yieldEvent func(Event, error) bool,
) (Reply, time.Time, bool, error) {
	isListening := true
	askedRewind := rewindOf(self.provider)

	var spentTime time.Duration

	for attempt := 1; ; attempt++ {
		askedAt := self.now()
		prose.startAttempt()

		reply, err := self.provider.Send(ctx, func(output Output) bool {
			isListening = yieldUpdates(prose.add(output))
			return isListening
		})

		if !isListening || err == nil {
			return reply, askedAt, isListening, err
		}

		wait, worthIt := self.retryWait(err, attempt, spentTime)
		if !worthIt {
			return reply, askedAt, isListening, err
		}

		spentTime += wait

		if !isResumable(err) {
			askedRewind.restore()
		}

		if !yieldUpdates(prose.interrupted()) {
			return reply, askedAt, false, err
		}

		if !self.readAbandonedCache(prose, askedAt, yieldEvent) {
			return reply, askedAt, false, err
		}

		notice := Event{
			Kind:    RetryingEvent,
			Failure: FailureFrom(err),
			Attempt: attempt,
			Took:    wait,
		}

		if call, faultedCall := faultedCall(err); faultedCall {
			notice.ID, notice.Name, notice.Arguments = call.ID, call.Name, call.Arguments
		}

		if !yieldEvent(notice, nil) {
			return reply, askedAt, false, err
		}

		if !self.waitBeforeRetry(ctx, wait) {
			return reply, askedAt, isListening, err
		}
	}
}

func (self *Agent) Send(ctx context.Context, message string) (string, error) {
	var answer strings.Builder
	var failure error

	for update, err := range self.Stream(ctx, message, nil) {
		if err != nil {
			failure = err
			break
		}

		if update.Event != nil && update.Event.Kind == ModelMessageEvent {
			answer.WriteString(update.Event.Text)
		}
	}

	return answer.String(), failure
}

const CancelledOutput = "the call was cancelled"

func resultStatus(ctx context.Context, ok bool) Status {
	switch {
	case ok:
		return SuccessStatus
	case ctx.Err() != nil:
		return CancelledStatus
	default:
		return ErrorStatus
	}
}

func cancelledResults(ctx context.Context, calls []ToolCall) []ToolCallResult {
	results := make([]ToolCallResult, len(calls))
	output := CancelledOutput + stop.Phrase(ctx)

	for i, call := range calls {
		results[i] = ToolCallResult{ID: call.ID, Output: output, IsError: true}
	}

	return results
}

func (self *Agent) answer(results []ToolCallResult) {
	if len(results) > 0 {
		self.provider.AddToolResults(results)
	}
}

type pendingCall struct {
	rawToolCall    ToolCall
	parsedToolCall tool.ToolCall

	err string
}

func (self *Agent) runCalls(
	ctx context.Context,
	calls []ToolCall,
	usage Usage,
	yield func(Event, error) bool,
) bool {
	results := cancelledResults(ctx, calls)

	defer func() { self.answer(results) }()

	queuedCalls := make([]pendingCall, len(calls))

	for i, rawCall := range calls {
		parsedToolCall, err := self.parseCall(rawCall)
		queuedCalls[i] = pendingCall{
			rawToolCall:    rawCall,
			parsedToolCall: parsedToolCall,
			err:            err,
		}

		event := Event{
			Kind:      ToolCallRequestEvent,
			ID:        rawCall.ID,
			Arguments: rawCall.Arguments,
			Name:      rawCall.Name,
			FallbackRendering: FallbackRendering{
				ReadOnly: self.readOnly(rawCall),
			},
		}
		if i == len(calls)-1 && (usage.InputTokens > 0 || usage.OutputTokens > 0) {
			event.Usage = &usage
		}

		if parsedToolCall != nil {
			event.Describe(parsedToolCall)
		} else {
			event.Subject = self.describeUnparsedToolCall(rawCall)
		}

		if !yield(event, nil) {
			return false
		}
	}

	isListening := true

	for start := 0; start < len(queuedCalls) && isListening; {
		end := self.batchEnd(queuedCalls, start)
		isListening = self.runBatch(ctx, queuedCalls[start:end], results[start:end], yield)
		start = end
	}

	return isListening
}

func (self *Agent) batchEnd(queuedCalls []pendingCall, start int) int {
	if !self.concurrent(queuedCalls[start].rawToolCall) {
		return start + 1
	}

	end := start + 1
	for end < len(queuedCalls) && self.concurrent(queuedCalls[end].rawToolCall) {
		end++
	}

	return end
}

func (self *Agent) concurrent(call ToolCall) bool {
	calledTool, isFound := self.registeredTools[call.Name]

	return isFound && calledTool.Concurrent()
}

func (self *Agent) describeUnparsedToolCall(call ToolCall) string {
	calledTool, isFound := self.registeredTools[call.Name]
	if !isFound {
		return strutil.FirstLine(call.Arguments)
	}

	return tool.DescribeUnparsedArguments(calledTool, call.Arguments)
}

func (self *Agent) readOnly(call ToolCall) bool {
	calledTool, isFound := self.registeredTools[call.Name]

	return isFound && calledTool.ReadOnly()
}

type completedToolCall struct {
	result Event
	state  Event
}

const maxConcurrentToolCalls = 16

func (self *Agent) runBatch(
	ctx context.Context,
	batch []pendingCall, results []ToolCallResult,
	yield func(Event, error) bool,
) bool {
	done := make(chan completedToolCall, len(batch))
	availableSlots := make(chan struct{}, maxConcurrentToolCalls)

	for i, item := range batch {
		availableSlots <- struct{}{}
		go func() {
			defer func() { <-availableSlots }()
			startedAt := time.Now()

			callContext, waitedTime := waiting.Track(ctx)

			executionResult := tool.ToolCallResult{Output: item.err}
			ok := false
			if item.parsedToolCall != nil {
				executionResult, ok = exec(callContext, item.parsedToolCall)
			}

			results[i] = ToolCallResult{
				ID:      item.rawToolCall.ID,
				Output:  executionResult.Output,
				Image:   executionResult.Image,
				IsError: !ok,
			}

			if item.parsedToolCall != nil && executionResult.Metrics.Kind == "" {
				executionResult.Metrics = tool.GetMetrics(executionResult.Output)
			}

			var metrics *tool.ToolCallMetrics
			if executionResult.Metrics.Kind != "" {
				if executionResult.Metrics.TotalBytes == executionResult.Metrics.Bytes {
					executionResult.Metrics.TotalBytes = 0
				}
				metrics = &executionResult.Metrics
			}

			status := resultStatus(ctx, ok)

			completion := completedToolCall{result: Event{
				Kind:    ToolCallResultEvent,
				ID:      item.rawToolCall.ID,
				Name:    item.rawToolCall.Name,
				Text:    executionResult.Output,
				Status:  status,
				Took:    max(time.Since(startedAt)-waitedTime(), 0),
				Metrics: metrics,
			}}
			if self.storePicture != nil && len(executionResult.Image.Data) > 0 {
				completion.result.Picture = self.storePicture(executionResult.Image)
			}
			if ok && len(executionResult.State) > 0 {
				completion.state = Event{
					Kind:  StateChangeEvent,
					ID:    item.rawToolCall.ID,
					Name:  self.registeredTools[item.rawToolCall.Name].StateKey(),
					State: executionResult.State,
				}
			}

			done <- completion
		}()
	}

	isListening := true

	for range batch {
		completion := <-done

		if completion.state.Kind != "" {
			if err := self.restoreState(completion.state); err != nil {
				if isListening {
					yield(Event{}, err)
				}
				return false
			}
		}
		if isListening && completion.state.Kind != "" {
			isListening = yield(completion.state, nil)
		}
		if isListening {
			isListening = yield(completion.result, nil)
		}
	}

	return isListening
}

func (self *Agent) RestoreState(events []Event) error {
	for _, event := range events {
		if event.Kind != StateChangeEvent {
			continue
		}
		if err := self.restoreState(event); err != nil {
			return err
		}
	}

	return nil
}

func (self *Agent) restoreState(event Event) error {
	calledTool, isKnown := self.owners[event.Name]
	if !isKnown {
		return nil
	}
	if err := calledTool.Restore(event.State); err != nil {
		return fmt.Errorf("could not restore %s state: %w", event.Name, err)
	}

	return nil
}

func (self *Agent) Tool(name string) (tool.Tool, bool) {
	found, isKnown := self.registeredTools[name]
	return found, isKnown
}

func (self *Agent) IsToolEnabled(name string) bool {
	_, isEnabled := self.enabledToolNames[name]
	return isEnabled
}

func (self *Agent) parseCall(call ToolCall) (tool.ToolCall, string) {
	calledTool, isRegistered := self.registeredTools[call.Name]
	if !isRegistered || !self.IsToolEnabled(call.Name) {
		return nil, fmt.Sprintf("there is no tool called %q", call.Name)
	}

	parsedToolCall, err := calledTool.Parse(call.Arguments)
	if err != nil {
		return nil, err.Error()
	}

	return parsedToolCall, ""
}

func exec(ctx context.Context, call tool.ToolCall) (tool.ToolCallResult, bool) {
	result, err := call.Exec(ctx)

	switch {
	case err == nil:
		return result, true
	case result.Output != "":
		return result, false
	}

	result.Output = err.Error()
	return result, false
}
