package painter

import (
	"strings"

	"crdx.org/oh/internal/app/call"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/pkg/toolbox/subagent"
)

type Introductions struct {
	intents map[string]string
	aliases map[string]string
}

func NewIntroductions() *Introductions {
	return &Introductions{intents: map[string]string{}, aliases: map[string]string{}}
}

func (self *Introductions) Forget() {
	clear(self.intents)
	clear(self.aliases)
}

func (self *Introductions) Introduce(name string, intent string) {
	if name != "" && intent != "" {
		self.intents[name] = intent
	}
}

func (self *Introductions) SubagentAlias(name string) string {
	if self == nil {
		return ""
	}
	return self.aliases[subagent.Mention(name)]
}

func (self *Introductions) SubagentTitle(name string) string {
	return subagentrecord.Title(name, self.SubagentAlias(name))
}

func (self *Introductions) introduceAlias(name string, alias string) {
	if name != "" && alias != "" {
		self.aliases[name] = alias
	}
}

func (self *Introductions) recall(names []string) string {
	var intents []string
	for _, name := range names {
		intent := self.intents[name]
		if alias := self.aliases[name]; alias != "" && intent != "" {
			intent = alias + ": " + intent
		}
		if intent != "" {
			intents = append(intents, intent)
		}
	}

	return strings.Join(intents, call.AsideSeparator)
}

func (self *Introductions) note(label call.Label) {
	self.Introduce(label.Introduces, label.Intent)
}
