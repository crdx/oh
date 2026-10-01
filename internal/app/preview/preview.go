package preview

import (
	"strings"

	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/agent"
)

const minimumRoom = 20

func Read(directory string, name string, workspace *work.Space, currency money.Currency, room int) ([]string, error) {
	storedSession, err := store.Read(directory, name)
	if err != nil {
		return nil, err
	}

	tariff := painter.Tariff{Prices: storedSession.Meta.Prices(), Currency: currency}

	return Draw(storedSession.Events, tariff, workspace, room), nil
}

func Draw(events []agent.Event, tariff painter.Tariff, workspace *work.Space, room int) []string {
	var conversation strings.Builder

	screen := output.NewTerminalOfSize(&conversation, max(room, minimumRoom), 0).
		AppendOnly().
		WithoutMessageMarks()
	picasso := painter.New(screen, false, nil, workspace, output.StreamingModeLine)
	picasso.PriceCacheRebuildsAt(tariff)

	for _, event := range events {
		picasso.DrawEvent(event)
	}
	picasso.Close(dynamic.Cancelled)
	screen.End()

	return rows(conversation.String())
}

func rows(conversation string) []string {
	conversation = strings.ReplaceAll(conversation, "\r\n", "\n")
	conversation = strings.TrimSuffix(conversation, "\n")
	if conversation == "" {
		return nil
	}

	return strings.Split(conversation, "\n")
}
