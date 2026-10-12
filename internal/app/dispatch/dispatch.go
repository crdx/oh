package dispatch

import (
	"fmt"
	"strings"

	"crdx.org/oh/internal/app/slash"
	"crdx.org/oh/pkg/agent"
)

type Result int

const (
	commandNotFoundMessage = "Command not found"
	sendAsMessageHint      = " (prepend a space to send)"
	snippetNotFoundMessage = "Snippet not found"
	snippetPrefix          = "//"
)

const (
	Proceed Result = iota
	Handled
	PromptSent
	Rejected
)

type Actions struct {
	EmitEvent                 func(agent.Event)
	SendPrompt                func(string)
	ShowFeedback              func(string, agent.Status)
	ShowIndentedFeedback      func(string, agent.Status, int)
	ShowListingFeedback       func(string, agent.Status)
	ShowPlainFeedback         func(string)
	ShowPlainIndentedFeedback func(string, int)
	ShowPlainListingFeedback  func(string)
}

func LiteralSlash(message string) bool {
	return strings.HasPrefix(message, " ") && strings.HasPrefix(strings.TrimLeft(message, " "), "/")
}

func Handle(registry slash.Registry, actions Actions, message string) (Result, string) {
	if LiteralSlash(message) {
		return Proceed, ""
	}

	invocation, found := registry.Find(message)
	if found {
		isPromptSent := false
		sendPrompt := actions.SendPrompt
		actions.SendPrompt = func(prompt string) {
			isPromptSent = true
			sendPrompt(prompt)
		}
		if err := invocation.Command.Run(actions, invocation.Arguments); err != nil {
			return Rejected, slash.FormatError(invocation, err) + sendAsMessageHint
		}
		if isPromptSent {
			return PromptSent, ""
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

func (self Actions) NoticeIndented(message string, continuationIndent int) {
	self.ShowIndentedFeedback(message, agent.InfoStatus, continuationIndent)
}

func (self Actions) NoticeListing(message string) {
	self.ShowListingFeedback(message, agent.InfoStatus)
}

func (self Actions) PlainNotice(message string) {
	self.ShowPlainFeedback(message)
}

func (self Actions) PlainNoticeIndented(message string, continuationIndent int) {
	self.ShowPlainIndentedFeedback(message, continuationIndent)
}

func (self Actions) PlainNoticeListing(message string) {
	self.ShowPlainListingFeedback(message)
}

func (self Actions) Success(message string) {
	self.ShowFeedback(message, agent.SuccessStatus)
}
