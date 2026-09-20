package picker

import (
	"io"
	"os"
	"slices"
	"strings"

	"crdx.org/io/internal/app/menu"
	"crdx.org/io/internal/app/segment/fastMode"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/table"
	"crdx.org/io/internal/app/width"
	"crdx.org/io/internal/money"
	"crdx.org/io/internal/util"
	"crdx.org/io/pkg/agent"
)

const (
	markWidth        = 2
	providerColumn   = 12
	nameColumn       = 28
	effortColumn     = 9
	contextColumn    = 7
	inputColumn      = 9
	outputColumn     = 9
	identifierColumn = 28
)

const (
	costGaps            = 6
	rateGaps            = 7
	shortenedIdentifier = 18
)

const costTitle = "Cost"

var costColumn = widestPriceTier()

func widestPriceTier() int {
	widest := width.Of(costTitle)
	for _, priceTier := range agent.PriceTiers() {
		widest = max(widest, width.Of(priceTier.String()))
	}

	return widest
}

var costRoom = markWidth + providerColumn + nameColumn + effortColumn +
	contextColumn + costColumn + shortenedIdentifier + costGaps*table.DefaultGap

var rateRoom = markWidth + providerColumn + nameColumn + effortColumn + contextColumn +
	costColumn + inputColumn + outputColumn + identifierColumn + rateGaps*table.DefaultGap

type Effort struct {
	Level  string
	IsFast bool
}

func (self Effort) String() string {
	if self.IsFast {
		return self.Level + " " + fastMode.FastMark
	}

	return self.Level
}

type Model struct {
	Provider            string
	ProviderID          string
	Name                string
	ID                  string
	EffortLevels        []Effort
	Effort              Effort
	ContextWindowTokens int
	Prices              *agent.TokenPrices
}

func Choose(
	models []*Model,
	currency money.Currency,
	terminal *os.File,
	screen io.Writer,
) (*Model, error) {
	chosenIndex, err := menu.Choose(&modelList{models: models, currency: currency}, terminal, screen)
	if err != nil {
		return nil, err
	}

	return models[chosenIndex], nil
}

type modelList struct {
	models   []*Model
	currency money.Currency
}

func (self *modelList) Len() int { return len(self.models) }

func (self *modelList) IsChoosable(int) bool { return true }

func (self *modelList) Text(index int) string {
	model := self.models[index]

	return strings.Join([]string{model.Provider, model.ProviderID, model.Name, model.ID}, " ")
}

func (self *modelList) Adjust(index int, direction int) {
	model := self.models[index]

	at := slices.Index(model.EffortLevels, model.Effort)
	if at < 0 {
		return
	}

	if wantedIndex := at + direction; wantedIndex >= 0 && wantedIndex < len(model.EffortLevels) {
		model.Effort = model.EffortLevels[wantedIndex]
	}
}

func (self *modelList) ColumnHeader(room int) string {
	return modelTable().Header(room)
}

func (self *modelList) Row(index int, isChosen bool, room int) string {
	paint := style.Answer
	if isChosen {
		paint = style.ChosenRow
	}

	return paint.Over(modelRow(self.models[index], self.currency, isChosen, room))
}

func modelTable() *table.Table {
	return table.New(
		table.Column{Title: "  Provider", Width: markWidth + providerColumn},
		table.Column{Title: "Model", Width: nameColumn},
		table.Column{Title: "Effort", Width: effortColumn},
		table.Column{Title: "Context", Width: contextColumn, Align: table.Right},
		table.Column{Title: costTitle, Width: costColumn, MinRoom: costRoom},
		table.Column{Title: "Input", Width: inputColumn, Align: table.Right, MinRoom: rateRoom},
		table.Column{Title: "Output", Width: outputColumn, Align: table.Right, MinRoom: rateRoom},
		table.Column{Title: "Identifier", Width: identifierColumn, Style: style.Subtle},
	)
}

func modelRow(model *Model, currency money.Currency, isChosen bool, room int) string {
	return modelTable().Row([]string{
		menu.Mark(isChosen) + " " + model.Provider,
		model.Name,
		model.Effort.String(),
		contextWindow(model.ContextWindowTokens),
		tier(model.Prices),
		price(model.Prices, currency, inputRate),
		price(model.Prices, currency, outputRate),
		model.ID,
	}, room)
}

const unknownQuantity = "—"

func contextWindow(tokens int) string {
	if tokens <= 0 {
		return unknownQuantity
	}

	return util.FormatWholeThousands(tokens)
}

func tier(prices *agent.TokenPrices) string {
	if prices == nil {
		return unknownQuantity
	}

	priceTier := prices.Tier()
	if priceTier == agent.PriceUnknown {
		return unknownQuantity
	}

	return priceTierStyles[priceTier](priceTier.String())
}

var priceTierStyles = map[agent.PriceTier]style.Style{
	agent.PriceLow:     style.LowPrice,
	agent.PriceMedium:  style.MediumPrice,
	agent.PriceHigh:    style.HighPrice,
	agent.PriceExtreme: style.WtfPrice,
}

func inputRate(prices agent.TokenPrices) float64 {
	return prices.Input
}

func outputRate(prices agent.TokenPrices) float64 {
	return prices.Output
}

func price(prices *agent.TokenPrices, currency money.Currency, rate func(agent.TokenPrices) float64) string {
	if prices == nil || !prices.IsKnown() {
		return unknownQuantity
	}

	return currency.FormatRate(rate(*prices))
}
