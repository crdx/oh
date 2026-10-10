package harness

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/agent"
)

func TestReloadingConfigChangesTheCurrencyOnceItsRateArrives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[ui]\nstreaming = \"line\"\n")

	var screenOutput bytes.Buffer
	self := testConversation(t, &screenOutput)
	prepareLiveConfig(t, self, path)
	self.currency = newCurrencyState("", money.Dollar(), false)
	self.currency.refresh = func(code string, _ bool) money.Refresh {
		if code == "XAU" {
			return money.Refresh{Code: code, Currency: money.Dollar(), Failure: errors.New("nobody quotes gold")}
		}
		return money.Refresh{Code: code, Currency: money.In(code, 0.5)}
	}
	takeRefresh := func() money.Refresh {
		t.Helper()
		select {
		case refresh := <-self.currencyRefreshes():
			return refresh
		case <-time.After(2 * time.Second):
			t.Fatal("no exchange rate arrived")
			return money.Refresh{}
		}
	}

	writeLiveConfig(t, path, "[ui]\nstreaming = \"line\"\ncurrency = \"gbp\"\n")
	settleLiveConfig(t, self)
	if got := self.getCurrency().Format(1); strings.Contains(got, "£") {
		t.Fatalf("the currency changed to %q before its rate arrived", got)
	}
	self.currencyRefreshed(takeRefresh())
	if got := self.getCurrency().Format(1); !strings.Contains(got, "£") {
		t.Errorf("a dollar drew as %q after the reload, want pounds", got)
	}
	if got := self.display.tariff.Currency.Format(1); !strings.Contains(got, "£") {
		t.Errorf("the painter's tariff drew a dollar as %q", got)
	}

	writeLiveConfig(t, path, "[ui]\nstreaming = \"line\"\ncurrency = \"XAU\"\n")
	settleLiveConfig(t, self)
	stale := money.Refresh{Code: "GBP", Currency: money.In("GBP", 0.9)}
	self.currencyRefreshed(stale)
	if got := self.getCurrency().Format(1); got == stale.Currency.Format(1) {
		t.Errorf("a rate for a currency nobody asks for any more was applied: %q", got)
	}
	self.currencyRefreshed(takeRefresh())
	if message := self.feedback.Message(); message.Status != agent.WarningStatus || !strings.Contains(message.Text, "XAU") {
		t.Errorf("a rate that could not be refreshed said %+v", message)
	}
}
