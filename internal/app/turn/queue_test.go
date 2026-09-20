package turn

import "testing"

func TestLatestReplacementWinsAndKeepsAccessChange(t *testing.T) {
	var queue Queue
	queue.Replace("first")
	queue.MarkAccessChange()
	queue.Replace("second")

	pending := queue.Peek()
	if !pending.Replacement || !pending.AccessChange || pending.Message != "second" {
		t.Errorf("got %+v", pending)
	}
}

func TestReplacementTakesPriorityAndConsumesTheQueue(t *testing.T) {
	var queue Queue
	queue.MarkAccessChange()
	queue.Replace("continue")

	kind, message := queue.Take()
	if kind != Replacement || message != "continue" {
		t.Errorf("got %v %q", kind, message)
	}
	if !queue.Empty() {
		t.Errorf("queue was not consumed: %+v", queue.Peek())
	}
}

func TestAccessChangeIsTakenWithoutAReplacement(t *testing.T) {
	var queue Queue
	queue.MarkAccessChange()

	kind, message := queue.Take()
	if kind != AccessChange || message != "" {
		t.Errorf("got %v %q", kind, message)
	}
}

func TestASilentTurnIsPokedWithTheMessageSayingWhy(t *testing.T) {
	var queue Queue
	queue.MarkSilentTurn()

	kind, message := queue.Take()
	if kind != Poke || message != PokeMessage {
		t.Errorf("got %v %q", kind, message)
	}
	if !queue.Empty() {
		t.Errorf("queue was not consumed: %+v", queue.Peek())
	}
}

func TestAMessageOfTheUsersOwnIsTakenAheadOfAPoke(t *testing.T) {
	var queue Queue
	queue.MarkSilentTurn()
	queue.Replace("never mind that")

	kind, message := queue.Take()
	if kind != Replacement || message != "never mind that" {
		t.Errorf("got %v %q", kind, message)
	}
}

func TestAStoppedTurnDropsThePokeItAskedFor(t *testing.T) {
	var queue Queue
	queue.MarkSilentTurn()
	queue.Drop()

	kind, message := queue.Take()
	if kind != None || message != "" {
		t.Errorf("got %v %q", kind, message)
	}
}

func TestClearDropsEveryQueuedAction(t *testing.T) {
	var queue Queue
	queue.Replace("later")
	queue.MarkAccessChange()
	queue.Clear()

	kind, message := queue.Take()
	if kind != None || message != "" || !queue.Empty() {
		t.Errorf("got %v %q %+v", kind, message, queue.Peek())
	}
}
