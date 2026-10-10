package sessionSpend

import (
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/money"
)

type state struct {
	spend       func() (float64, bool)
	getCurrency func() money.Currency
}

func New(spend func() (float64, bool), getCurrency func() money.Currency) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		if err := options.Read(&struct{}{}); err != nil {
			return nil, err
		}

		return state{spend: spend, getCurrency: getCurrency}, nil
	}
}

func (self state) Render(segment.Context) string {
	dollars, isKnown := self.spend()
	if !isKnown {
		return ""
	}

	currency := money.Dollar()
	if self.getCurrency != nil {
		currency = self.getCurrency()
	}
	return style.Quantity(currency.Format(dollars))
}
