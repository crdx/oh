package preview

import (
	"strings"

	"crdx.org/oh/internal/app/dynamic"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/pkg/agent"
)

const minimumRoom = 20

func Read(directory string, name string, workspace *work.Space, room int) ([]string, error) {
	storedSession, err := store.Read(directory, name)
	if err != nil {
		return nil, err
	}

	return Draw(storedSession.Events, workspace, room), nil
}

func Draw(events []agent.Event, workspace *work.Space, room int) []string {
	var conversation strings.Builder

	screen := output.NewTerminalOfSize(&conversation, max(room, minimumRoom), 0).
		AppendOnly().
		WithoutMessageMarks()
	picasso := painter.New(screen, false, nil, workspace, output.StreamingModeLine)

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
