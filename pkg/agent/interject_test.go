package agent_test

import (
	"context"
	"slices"
	"testing"

	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/tool"
)

type interjectionProvider struct {
	sent          int
	rounds        int
	history       []string
	interjections *agent.Interjections
	queueAfter    []string
	noteAfter     []string
}

func (self *interjectionProvider) Configure(string, []tool.Definition) {}

func (self *interjectionProvider) AddUserMessage(text string) {
	self.history = append(self.history, "user:"+text)
}

func (self *interjectionProvider) AddToolResults(results []agent.ToolCallResult) {
	for _, result := range results {
		self.history = append(self.history, "result:"+result.ID)
	}
}

func (self *interjectionProvider) Send(_ context.Context, _ agent.Yield) (agent.Reply, error) {
	self.sent++
	self.history = append(self.history, "send")

	if self.sent == 1 {
		for _, text := range self.noteAfter {
			self.interjections.Note(text)
		}
	}

	if self.sent > self.rounds {
		return agent.Reply{}, nil
	}

	if self.sent == 1 {
		for _, text := range self.queueAfter {
			self.interjections.Add(text)
		}
	}

	return agent.Reply{Calls: []agent.ToolCall{
		{ID: "a", Name: "noop", Arguments: `{}`},
		{ID: "b", Name: "noop", Arguments: `{}`},
	}}, nil
}

func runInterjecting(t *testing.T, rounds int, queued ...string) (*interjectionProvider, []string) {
	t.Helper()

	interjections := &agent.Interjections{}
	provider := &interjectionProvider{rounds: rounds, interjections: interjections, queueAfter: queued}
	assistant := agent.New("", provider, []tool.Tool{noop()})

	var messages []string
	for update, err := range assistant.Stream(t.Context(), "go", interjections) {
		if err != nil {
			t.Fatal(err)
		}
		if update.Event != nil && update.Event.Kind == agent.UserMessageEvent {
			messages = append(messages, update.Event.Text)
		}
	}

	return provider, messages
}

func TestAQueuedMessageReachesTheProviderOnlyOnceEveryCallIsAnswered(t *testing.T) {
	provider, messages := runInterjecting(t, 2, "look at the other path too")

	want := []string{
		"user:go",
		"send",
		"result:a",
		"result:b",
		"user:look at the other path too",
		"send",
		"result:a",
		"result:b",
		"send",
	}
	if !slices.Equal(provider.history, want) {
		t.Errorf("history %q, want %q", provider.history, want)
	}

	wantMessages := []string{"go", "look at the other path too"}
	if !slices.Equal(messages, wantMessages) {
		t.Errorf("drew %q, want %q", messages, wantMessages)
	}
}

func TestSeveralQueuedMessagesArriveAsOneSeparatedByABlankLine(t *testing.T) {
	provider, messages := runInterjecting(t, 2, "first", "second")

	if !slices.Contains(provider.history, "user:first\n\nsecond") {
		t.Errorf("history %q, want the queue joined into one message", provider.history)
	}

	wantMessages := []string{"go", "first\n\nsecond"}
	if !slices.Equal(messages, wantMessages) {
		t.Errorf("drew %q, want %q", messages, wantMessages)
	}
}

func TestAnEmptyQueueDeliversNothing(t *testing.T) {
	provider, messages := runInterjecting(t, 1)

	want := []string{"user:go", "send", "result:a", "result:b", "send"}
	if !slices.Equal(provider.history, want) {
		t.Errorf("history %q, want %q", provider.history, want)
	}
	if !slices.Equal(messages, []string{"go"}) {
		t.Errorf("drew %q, want only the prompt", messages)
	}
}

func TestAQueueOutlivesATurnThatEndsBeforeTheNextRound(t *testing.T) {
	interjections := &agent.Interjections{}
	provider := &interjectionProvider{rounds: 0, interjections: interjections}
	assistant := agent.New("", provider, []tool.Tool{noop()})

	interjections.Add("still waiting")

	for _, err := range assistant.Stream(t.Context(), "go", interjections) {
		if err != nil {
			t.Fatal(err)
		}
	}

	if queued := interjections.Peek(); !slices.Equal(queued, []string{"still waiting"}) {
		t.Errorf("left %q queued, want the message kept for the caller", queued)
	}
}

func TestAStoppedTurnKeepsItsQueueRatherThanSwallowingIt(t *testing.T) {
	interjections := &agent.Interjections{}
	provider := &interjectionProvider{rounds: 2, interjections: interjections}
	assistant := agent.New("", provider, []tool.Tool{noop()})

	streamContext, cancel := context.WithCancel(t.Context())
	defer cancel()

	isStopped := false
	for update, err := range assistant.Stream(streamContext, "go", interjections) {
		if err != nil {
			break
		}
		if update.Event != nil && update.Event.Kind == agent.ToolCallRequestEvent && !isStopped {
			isStopped = true
			interjections.Add("do this instead")
			cancel()
		}
	}

	if queued := interjections.Peek(); !slices.Equal(queued, []string{"do this instead"}) {
		t.Errorf("left %q queued, want the message kept for the next turn", queued)
	}
	if slices.Contains(provider.history, "user:do this instead") {
		t.Errorf("history %q, want nothing added to a stopped turn", provider.history)
	}
}

func TestTakingTheQueueEmptiesItAndTakingTheLastLeavesTheRest(t *testing.T) {
	interjections := &agent.Interjections{}
	interjections.Add("first")
	interjections.Add("second")

	last, isTaken := interjections.TakeLast()
	if !isTaken || last != "second" {
		t.Errorf("took back %q, want the second", last)
	}
	if queued := interjections.Peek(); !slices.Equal(queued, []string{"first"}) {
		t.Errorf("left %q queued, want only the first", queued)
	}

	joined, isQueued := interjections.Take()
	if !isQueued || joined != "first" {
		t.Errorf("took %q, want the first", joined)
	}
	if queued := interjections.Peek(); len(queued) != 0 {
		t.Errorf("left %q queued, want nothing", queued)
	}
	if _, isQueued := interjections.Take(); isQueued {
		t.Error("expected an emptied queue to report nothing")
	}
	if _, isTaken := interjections.TakeLast(); isTaken {
		t.Error("expected an emptied queue to give nothing back")
	}
}

func TestAnEmptyMessageIsNotQueued(t *testing.T) {
	interjections := &agent.Interjections{}

	if interjections.Add("") {
		t.Error("expected an empty message to be refused")
	}
	if queued := interjections.Peek(); len(queued) != 0 {
		t.Errorf("queued %q, want nothing", queued)
	}
	if _, isQueued := interjections.Take(); isQueued {
		t.Error("expected nothing to deliver")
	}
}

func TestANilQueueIsSafeToUse(t *testing.T) {
	var interjections *agent.Interjections

	if interjections.Add("ignored") {
		t.Error("expected a nil queue to refuse a message")
	}
	if queued := interjections.Peek(); queued != nil {
		t.Errorf("peeked %q, want nothing", queued)
	}
	if _, isQueued := interjections.Take(); isQueued {
		t.Error("expected a nil queue to hold nothing")
	}
	if _, isTaken := interjections.TakeLast(); isTaken {
		t.Error("expected a nil queue to give nothing back")
	}
}

func TestAHarnessNoteReachesTheModelWithoutBeingDrawnAsAUserMessage(t *testing.T) {
	interjections := &agent.Interjections{}
	provider := &interjectionProvider{rounds: 1, interjections: interjections}
	assistant := agent.New("", provider, []tool.Tool{noop()})

	var messages []string
	isNoted := false
	for update, err := range assistant.Stream(t.Context(), "go", interjections) {
		if err != nil {
			t.Fatal(err)
		}
		if update.Event == nil {
			continue
		}
		if update.Event.Kind == agent.ToolCallRequestEvent && !isNoted {
			isNoted = true
			interjections.Note("the job build has finished")
		}
		if update.Event.Kind == agent.UserMessageEvent {
			messages = append(messages, update.Event.Text)
		}
	}

	want := []string{"user:go", "send", "result:a", "result:b", "user:the job build has finished", "send"}
	if !slices.Equal(provider.history, want) {
		t.Errorf("history %q, want %q", provider.history, want)
	}
	if !slices.Equal(messages, []string{"go"}) {
		t.Errorf("drew %q, want the note to be drawn by whoever recorded it", messages)
	}
}

func TestAHarnessNoteArrivingDuringTheFinalAnswerGetsAnotherRound(t *testing.T) {
	interjections := &agent.Interjections{}
	provider := &interjectionProvider{
		rounds:        0,
		interjections: interjections,
		noteAfter:     []string{"the job build has finished"},
	}
	assistant := agent.New("", provider, []tool.Tool{noop()})

	for _, err := range assistant.Stream(t.Context(), "go", interjections) {
		if err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"user:go", "send", "user:the job build has finished", "send"}
	if !slices.Equal(provider.history, want) {
		t.Errorf("history %q, want %q", provider.history, want)
	}
	if note, isNoted := interjections.TakeNotes(); isNoted {
		t.Errorf("left %q queued, want the model to receive it in the same turn", note)
	}
}

func TestAnEmptyNoteIsNotQueued(t *testing.T) {
	interjections := &agent.Interjections{}

	if interjections.Note("") {
		t.Error("expected an empty note to be refused")
	}
	if _, isNoted := interjections.TakeNotes(); isNoted {
		t.Error("expected no note to be queued")
	}
}
