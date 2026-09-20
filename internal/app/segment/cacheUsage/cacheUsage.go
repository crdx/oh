package cacheUsage

import (
	"strconv"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
)

const (
	fullPercentage = 100
)

type state struct {
	usage func() (readTokens int, askedTokens int)
}

func New(usage func() (readTokens int, askedTokens int)) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}

		return state{usage: usage}, nil
	}
}

func (self state) Render(segment.Context) string {
	readTokens, askedTokens := self.usage()
	share, isKnown := readShare(readTokens, askedTokens)
	if !isKnown {
		return ""
	}

	return style.Quantity(strconv.Itoa(share) + "%")
}

func readShare(readTokens int, askedTokens int) (int, bool) {
	if askedTokens <= 0 {
		return 0, false
	}

	return min(fullPercentage, (readTokens*fullPercentage+askedTokens/2)/askedTokens), true
}
