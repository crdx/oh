package turn

type Kind int

const (
	None Kind = iota
	Replacement
	AccessChange
	AccessNotice
	Poke
)

type PendingEntry struct {
	Message      string
	Replacement  bool
	AccessChange bool
	AccessNotice bool
	Poke         bool
}

type Queue struct {
	pendingEntry PendingEntry
}

func (self *Queue) Replace(message string) {
	self.pendingEntry.Message = message
	self.pendingEntry.Replacement = true
}

func (self *Queue) MarkAccessChange() {
	self.pendingEntry.AccessChange = true
}

func (self *Queue) MarkSilentTurn() {
	self.pendingEntry.Poke = true
}

func (self *Queue) Clear() {
	self.pendingEntry = PendingEntry{}
}

func (self *Queue) Drop() {
	self.pendingEntry = PendingEntry{AccessNotice: self.pendingEntry.AccessChange || self.pendingEntry.AccessNotice}
}

func (self *Queue) Empty() bool {
	return !self.pendingEntry.Replacement && !self.pendingEntry.AccessChange &&
		!self.pendingEntry.AccessNotice && !self.pendingEntry.Poke
}

func (self *Queue) Peek() PendingEntry {
	return self.pendingEntry
}

func (self *Queue) Take() (Kind, string) {
	pendingEntry := self.pendingEntry
	self.Clear()

	switch {
	case pendingEntry.Replacement:
		return Replacement, pendingEntry.Message
	case pendingEntry.AccessChange:
		return AccessChange, ""
	case pendingEntry.AccessNotice:
		return AccessNotice, ""
	case pendingEntry.Poke:
		return Poke, PokeMessage
	default:
		return None, ""
	}
}
