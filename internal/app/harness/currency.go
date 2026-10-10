package harness

import (
	"context"
	"time"

	"crdx.org/oh/internal/app/feedback"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/agent"
)

const (
	currencyRefreshLimit   = 30 * time.Second
	currencyRefreshBacklog = 8
)

type currencyState struct {
	code        string
	current     money.Currency
	isSimulated bool
	refreshes   chan money.Refresh
	refresh     func(code string, isSimulated bool) money.Refresh
}

func newCurrencyState(configuredCode string, current money.Currency, isSimulated bool) currencyState {
	return currencyState{
		code:        currencyCode(configuredCode),
		current:     current,
		isSimulated: isSimulated,
		refreshes:   make(chan money.Refresh, currencyRefreshBacklog),
		refresh:     refreshCurrency,
	}
}

func (self *App) currencyRefreshes() <-chan money.Refresh {
	return self.currency.refreshes
}

func (self *App) getCurrency() money.Currency {
	return self.currency.current
}

func (self *App) followCurrency(configuredCode string) {
	code := currencyCode(configuredCode)
	if code == self.currency.code || self.currency.refreshes == nil {
		return
	}
	self.currency.code = code
	refreshes, refresh, isSimulated := self.currency.refreshes, self.currency.refresh, self.currency.isSimulated
	go func() {
		select {
		case refreshes <- refresh(code, isSimulated):
		default:
		}
	}()
}

func refreshCurrency(code string, isSimulated bool) money.Refresh {
	if code == "" || code == money.DollarCode || isSimulated {
		return money.Refresh{Code: code, Currency: money.Dollar()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), currencyRefreshLimit)
	defer cancel()
	currency, err := money.Ensure(ctx, "", location.GetExchangeRateCachePath(), code)
	return money.Refresh{Code: code, Currency: currency, Failure: err}
}

func (self *App) currencyRefreshed(refresh money.Refresh) {
	if refresh.Code != self.currency.code {
		return
	}
	self.currency.current = refresh.Currency
	self.display.tariff.Currency = refresh.Currency
	if refresh.Failure != nil {
		self.showFeedback(feedback.Config, feedback.Message{
			Text:   "The exchange rate for " + refresh.Code + " could not be refreshed: " + refresh.Failure.Error(),
			Status: agent.WarningStatus,
		})
	}
}
