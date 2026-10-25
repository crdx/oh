package subagentview

import (
	"errors"

	"crdx.org/oh/internal/app/preview"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/session"
)

var ErrConversationGone = errors.New("that subagent's conversation is gone")

func Read(directory string, name string, currency money.Currency, columns int) ([]string, error) {
	if !session.Exists(directory, name) {
		return nil, ErrConversationGone
	}
	storedSession, err := store.Read(directory, name)
	if err != nil {
		return nil, err
	}
	return preview.Read(directory, name, work.At(storedSession.Meta.WorkspaceDir), currency, columns)
}
