package harness

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/bar"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/edit"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/internal/app/painter"
	"crdx.org/oh/internal/app/startup"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/sim"
	"crdx.org/oh/pkg/agent"
)

func TestGoldenASessionCountsItsSpendInTheConfiguredCurrencyInProcess(t *testing.T) {
	compareWithGolden(t, "spend-currency", ".screen", map[string]func() string{
		"dollars by default": func() string { return inProcessSpendScreen(t, "", "") },
		"pounds from a current rate": func() string {
			return inProcessSpendScreen(t, "[ui]\ncurrency = \"gbp\"\n", `{"GBP":0.5}`)
		},
	})
}

func inProcessSpendScreen(t *testing.T, configuration string, rates string) string {
	t.Helper()

	t.Setenv(location.StateDirVariable, t.TempDir())
	configurationPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configurationPath, fmt.Appendf(nil, "version = %d\n%s%s", config.Format, configuration, spendBar), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.LoadSources(config.Source{Path: configurationPath})
	if err != nil {
		t.Fatal(err)
	}
	if rates != "" {
		path := location.GetExchangeRateCachePath()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{"version":1,"fetched":%q,"base":"USD","rates":%s}`, time.Now().Format(time.RFC3339), rates)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	endpoint := sim.New(&sim.Scenario{Model: "fake", Turns: []sim.Turn{{Say: "Priced answer."}}})
	server := httptest.NewServer(endpoint)
	t.Cleanup(server.Close)
	endpoints := backend.EndpointSettings{OverrideURL: endpoint.Addresses(server.URL)[sim.Completions]}
	var drawn bytes.Buffer
	choices, err := model.Ensure(&drawn, endpoints.OverrideURL, location.GetModelCachePath(), location.GetSeenModelsPath(),
		func(ctx context.Context, providerName string) ([]agent.Model, error) {
			return backend.ListModels(ctx, providerName, endpoints)
		},
		func(string) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	choice, err := model.Chosen(choices, location.GetSeenModelsPath(), model.OpencodeGoProvider, "fake")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := backend.Connect(choice, model.Selection{Provider: choice.Provider, Model: choice.ID, Effort: "high"}, endpoints)
	if err != nil {
		t.Fatal(err)
	}

	self := testConversation(t, &drawn)
	self.workspace = work.At(t.TempDir())
	self.screen = output.NewTerminalOfSize(&drawn, interactiveColumns, interactiveRows)
	self.mode = caps.NewMode(caps.Unconfined())
	self.agent = agent.New("Reply briefly.", connection.Client, nil)
	self.metrics = metrics.New(metrics.Settings{ContextWindowTokens: choice.ContextWindowTokens, Prices: choice.Prices})
	currency := ensureCurrency(t.Context(), &drawn, settings.Ui.Currency, false)
	self.currency = newCurrencyState(settings.Ui.Currency, currency, false)
	registry := bar.NewRegistry(bar.Options{
		Workspace:    self.workspace,
		IsUnconfined: true,
		GetCurrency:  self.getCurrency,
		Sources:      self.getBarSources(),
	})
	live, err := settings.BuildLive(registry)
	if err != nil {
		t.Fatal(err)
	}
	self.display.bar = bar.NewConfiguration(registry, live.SegmentLayout)
	self.display.tariff = painter.Tariff{Prices: choice.Prices, Currency: currency}
	self.display.streamingMode = live.StreamingMode
	self.display.reasoningRendering = live.ReasoningRendering
	self.display.theme = live.Theme
	self.inputLine = edit.NewInput(edit.NewHistory("", historyLimit))
	self.screen.Line(startup.RenderBanner(time.Millisecond, false, startup.Info{Session: "tame-impala", ReadyAt: time.Now()}, interactiveColumns, false))
	self.show(self.inputLine)
	self.start("a question")
	for event := range self.currentTurn.Events() {
		self.takeTurn(event)
	}
	self.finish()
	self.show(self.inputLine)

	if requests := endpoint.Requests(); len(requests) != 1 {
		t.Fatalf("got %d provider requests, want one real priced turn", len(requests))
	}
	spend, isKnown := self.sessionSpend()
	if !isKnown || spend <= 0 {
		t.Fatalf("the provider turn recorded no known spend: %v, known %t", spend, isKnown)
	}
	shown := playScreenOfSize(t, drawn.String(), interactiveColumns, interactiveRows).text()
	return wrappedBanner.ReplaceAllString(strings.Join(shown, "\n"), "Agent <banner>")
}
