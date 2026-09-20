package turn

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/io/internal/stop"
	"crdx.org/io/pkg/agent"
	"crdx.org/io/pkg/tool"
)

type streamProvider struct {
	send func(context.Context, agent.Yield) (agent.Reply, error)
}

func (self streamProvider) Configure(string, []tool.Definition)   {}
func (self streamProvider) AddUserMessage(string)                 {}
func (self streamProvider) AddToolResults([]agent.ToolCallResult) {}
func (self streamProvider) Send(ctx context.Context, yield agent.Yield) (agent.Reply, error) {
	return self.send(ctx, yield)
}

func TestStreamLifecycle(t *testing.T) {
	provider := streamProvider{send: func(_ context.Context, yield agent.Yield) (agent.Reply, error) {
		yield(agent.Output{Kind: agent.ModelMessageEvent, Text: "hello"})
		return agent.Reply{}, nil
	}}
	stream := Start(agent.New("", provider, nil), "begin", Timing{})
	var events []Event
	for event := range stream.Events() {
		events = append(events, event)
	}
	if len(events) == 0 || !stream.Running() {
		t.Fatal("stream did not deliver while running")
	}
	finishedAt := time.Now()
	stream.MarkFinished(finishedAt)
	if _, known := stream.Timing(); !known {
		t.Error("timing was lost when the turn finished")
	}
	stream.Finish()
	if stream.Running() || stream.Events() != nil {
		t.Error("stream remained active")
	}
}

func TestInterruptReachesProvider(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := streamProvider{send: func(ctx context.Context, _ agent.Yield) (agent.Reply, error) {
			<-ctx.Done()
			return agent.Reply{}, ctx.Err()
		}}
		stream := Start(agent.New("", provider, nil), "begin", Timing{})
		synctest.Wait()
		if !stream.Interrupt(stop.Because("the user pressed escape")) || !stream.Cancelled() {
			t.Fatal("stream was not interrupted")
		}
		if stream.Reason() == nil || stream.Reason().Error() != "the user pressed escape" {
			t.Errorf("got reason %v, want the one it was interrupted with", stream.Reason())
		}
		for event := range stream.Events() {
			stream.Observe(event)
		}
		if stream.Error() == nil {
			t.Error("terminal cancellation error was lost")
		}
	})
}

func TestObserveAndExternalState(t *testing.T) {
	terminalError := errors.New("failed")
	startedAt := time.Now().Add(-time.Second)
	stream := Adopt(nil, func(error) {}, State{Running: true, StartedAt: startedAt})
	if stream.Observe(Event{Err: terminalError}) || !errors.Is(stream.Error(), terminalError) {
		t.Error("error was not retained")
	}
	if !stream.Observe(Event{Update: agent.Update{}}) {
		t.Error("update was refused")
	}
	stream.SetCancelled(true)
	if !stream.Cancelled() {
		t.Error("external cancellation was lost")
	}
}

func TestTimingRetainsTheInactiveTurnAndCountsUpTheActiveTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		startedAt := time.Now()
		stream := Adopt(make(chan Event), func(error) {}, State{
			Running:   true,
			StartedAt: startedAt,
			Timing:    Timing{UserTurn: 12 * time.Minute},
		})

		time.Sleep(30 * time.Second)
		if timing, known := stream.Timing(); !known || timing != (Timing{UserTurn: 12 * time.Minute, ModelTurn: 30 * time.Second}) {
			t.Errorf("got timing %+v, known=%v while model turn was active", timing, known)
		}

		stream.MarkFinished(time.Now())
		stream.Finish()

		time.Sleep(5 * time.Second)
		if timing, known := stream.Timing(); !known || timing != (Timing{UserTurn: 5 * time.Second, ModelTurn: 30 * time.Second}) {
			t.Errorf("got timing %+v, known=%v while user turn was active", timing, known)
		}
	})
}

func TestAbsentStreamIsIdle(t *testing.T) {
	var stream *Stream
	if stream.Running() || stream.Cancelled() || stream.Error() != nil || stream.Events() != nil {
		t.Error("absent stream is active")
	}
	if _, known := stream.Timing(); known {
		t.Error("absent stream has timing")
	}
}

func TestATurnIsTimedOnTheWallClockAcrossASuspendedMachine(t *testing.T) {
	const monotonicReading = " m=+"

	assistant := agent.New("", streamProvider{
		send: func(context.Context, agent.Yield) (agent.Reply, error) { return agent.Reply{}, nil },
	}, nil)

	stream := Start(assistant, "go on", Timing{})
	stream.MarkFinished(time.Now())

	for name, at := range map[string]time.Time{
		"the moment the turn started":  stream.state.StartedAt,
		"the moment the turn finished": stream.state.FinishedAt,
	} {
		if got := at.String(); strings.Contains(got, monotonicReading) {
			t.Errorf("%s: got %q, want no monotonic reading", name, got)
		}
	}
}
