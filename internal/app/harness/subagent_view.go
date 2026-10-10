package harness

import (
	"errors"
	"os"
	"strings"

	"crdx.org/oh/internal/app/commands"
	"crdx.org/oh/internal/app/editor"
	"crdx.org/oh/internal/app/preview"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagentrecord"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/session"
)

var errConversationGone = errors.New("that subagent's conversation is gone")

var conversationPager = editor.Launch{Command: editor.Command{"less", "-R", "-+F"}, IsTerminal: true}

func (self *App) listSubagents() []commands.SubagentListing {
	var listing []commands.SubagentListing
	for _, event := range self.recordedEvents {
		if event.Kind != subagentrecord.Started {
			continue
		}
		listing = append(listing, commands.SubagentListing{
			Name:      event.Subagent,
			IsMissing: !session.Exists(self.children.directory, event.Subagent),
		})
	}
	return listing
}

func (self *App) showSubagent(name string) error {
	columns, _ := self.screen.Size()
	rows, err := self.drawSubagent(name, columns)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "oh-"+name+"-*.txt")
	if err != nil {
		return err
	}
	path := file.Name()
	_, writeError := file.WriteString(strings.Join(rows, "\n") + "\n")
	if err := errors.Join(writeError, file.Close()); err != nil {
		_ = os.Remove(path)
		return err
	}
	self.forgetViewedConversation()
	self.children.viewedConversation = path
	if err := self.handOverTerminal(conversationPager, []string{path}, ""); err != nil {
		self.forgetViewedConversation()
		return err
	}
	return nil
}

func (self *App) drawSubagent(name string, columns int) ([]string, error) {
	if !session.Exists(self.children.directory, name) {
		return nil, errConversationGone
	}
	storedSession, err := store.Read(self.children.directory, name)
	if err != nil {
		return nil, err
	}
	return preview.Read(
		self.children.directory, name, work.At(storedSession.Meta.WorkspaceDir), self.display.tariff.Currency, columns,
	)
}

func (self *App) forgetViewedConversation() {
	if self.children.viewedConversation != "" {
		_ = os.Remove(self.children.viewedConversation)
		self.children.viewedConversation = ""
	}
}
