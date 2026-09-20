package dispatch

import (
	"fmt"
	"strings"

	"crdx.org/io/internal/app/slash"
	"crdx.org/io/pkg/agent"
)

type Result int

const (
	commandNotFoundMessage = "Command not found"
	sendAsMessageHint      = " (alt+enter to send)"
	snippetNotFoundMessage = "Snippet not found"
	snippetPrefix          = "//"
)

const (
	Proceed Result = iota
	Handled
	Rejected
)

type Actions struct {
	EmitEvent         func(agent.Event)
	SendPrompt        func(string)
	ShowFeedback      func(string, agent.Status)
	ShowPlainFeedback func(string)
}

func Handle(registry slash.Registry, actions Actions, message string) (Result, string) {
	invocation, found := registry.Find(message)
	if found {
		if err := invocation.Command.Run(actions, invocation.Arguments); err != nil {
			return Rejected, slash.FormatError(invocation, err) + sendAsMessageHint
		}
		return Handled, ""
	}

	name, isCommand := registry.CommandName(message)
	if !isCommand {
		return Proceed, ""
	}

	notFoundMessage := commandNotFoundMessage
	if strings.HasPrefix(name, snippetPrefix) {
		notFoundMessage = snippetNotFoundMessage
	}
	return Rejected, fmt.Sprintf("%s: %s%s", notFoundMessage, name, sendAsMessageHint)
}

func (self Actions) Emit(event agent.Event) {
	self.EmitEvent(event)
}

func (self Actions) Send(message string) {
	self.SendPrompt(message)
}

func (self Actions) Notice(message string) {
	self.ShowFeedback(message, agent.InfoStatus)
}

func (self Actions) PlainNotice(message string) {
	self.ShowPlainFeedback(message)
}

func (self Actions) Success(message string) {
	self.ShowFeedback(message, agent.SuccessStatus)
}
