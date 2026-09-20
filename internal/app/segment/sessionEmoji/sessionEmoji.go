package sessionEmoji

import (
	"crdx.org/io/internal/app/segment"
	"crdx.org/io/pkg/session"
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
