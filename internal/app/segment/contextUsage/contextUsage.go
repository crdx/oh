package contextUsage

import (
	"fmt"
	"strconv"

	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/fit"
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

func (self state) Render(context segment.Context) string {
	return self.Ladder(context)[0]
}

func (self state) Ladder(segment.Context) []string {
	usedTokens, totalTokens := self.usage()

	percentage := formatPercentage(usedTokens, totalTokens)
	usedCount := util.FormatWholeThousands(usedTokens)
	totalCount := formatTotalTokens(totalTokens)

	return fit.Ladder(
		style.Quantity(fmt.Sprintf("%s %s/%s", percentage, usedCount, totalCount)),
		style.Quantity(percentage+" "+usedCount),
		style.Quantity(percentage),
		"",
	)
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
