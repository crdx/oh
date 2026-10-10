package painter

import (
	"strings"

	"crdx.org/oh/internal/app/call"
)

const asideSeparator = "; "

type Introductions struct {
	intents map[string]string
}

func NewIntroductions() *Introductions {
	return &Introductions{intents: map[string]string{}}
}

func (self *Introductions) Forget() {
	clear(self.intents)
}

func (self *Introductions) Introduce(name string, intent string) {
	if name != "" && intent != "" {
		self.intents[name] = intent
	}
}

func (self *Introductions) recall(names []string) string {
	var intents []string
	for _, name := range names {
		if intent := self.intents[name]; intent != "" {
			intents = append(intents, intent)
		}
	}

	return strings.Join(intents, asideSeparator)
}

func (self *Introductions) note(label call.Label) {
	self.Introduce(label.Introduces, label.Intent)
}
