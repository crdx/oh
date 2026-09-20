package sessionSpend

import (
	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/money"
)

type state struct {
	spend    func() (float64, bool)
	currency money.Currency
}

func New(spend func() (float64, bool), currency money.Currency) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}

		return state{spend: spend, currency: currency}, nil
	}
}

func (self state) Render(segment.Context) string {
	dollars, isKnown := self.spend()
	if !isKnown {
		return ""
	}

	return style.Quantity(self.currency.Format(dollars))
}
