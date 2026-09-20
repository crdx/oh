package contextUsage

import (
	"fmt"
	"strconv"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/util"
)

const (
	fullPercentage = 100

	unknown = "?"
)

type state struct {
	usage func() (usedTokens int, totalTokens int)
}

func New(usage func() (usedTokens int, totalTokens int)) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}

		return state{usage: usage}, nil
	}
}

func (self state) Render(segment.Context) string {
	usedTokens, totalTokens := self.usage()

	return style.Quantity(fmt.Sprintf(
		"%s %s/%s",
		formatPercentage(usedTokens, totalTokens),
		util.FormatWholeThousands(usedTokens),
		formatTotalTokens(totalTokens),
	))
}

func formatPercentage(usedTokens int, totalTokens int) string {
	if totalTokens <= 0 {
		return unknown + "%"
	}

	if usedTokens <= 0 {
		return "0%"
	}

	usedPercentage := (usedTokens*fullPercentage + totalTokens/2) / totalTokens

	return strconv.Itoa(min(fullPercentage, usedPercentage)) + "%"
}

func formatTotalTokens(tokens int) string {
	if tokens <= 0 {
		return unknown
	}

	return util.FormatWholeThousands(tokens)
}
