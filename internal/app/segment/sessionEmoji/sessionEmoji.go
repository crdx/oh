package sessionEmoji

import (
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/pkg/session"
)

type state struct {
	emoji string
}

func New(name string) segment.Factory {
	return func(segment.Options) (segment.Segment, error) {
		return state{emoji: session.Emoji(name)}, nil
	}
}

func (self state) Render(segment.Context) string {
	return self.emoji
}
