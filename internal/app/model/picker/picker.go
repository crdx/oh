package picker

import (
	"cmp"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/menu"
	"crdx.org/oh/internal/app/segment/fastMode"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/table"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
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

const (
	costTitle        = "Cost"
	costRequestCount = 100
	costColumn       = 10
	costRoom         = markWidth + providerColumn + nameColumn + effortColumn +
		contextColumn + costColumn + shortenedIdentifier + costGaps*table.Gap
	rateRoom = markWidth + providerColumn + nameColumn + effortColumn + contextColumn +
		costColumn + inputColumn + outputColumn + identifierColumn + rateGaps*table.Gap
)

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

func Print(output io.Writer, models []*Model, currency money.Currency, columns int) error {
	rows := modelList{models: models, currency: currency, order: cheapestFirst}
	if _, err := fmt.Fprintln(output, style.Column(rows.ColumnHeader(columns))); err != nil {
		return err
	}
	indexes := make([]int, len(models))
	for index := range models {
		indexes[index] = index
	}
	rows.Sort(indexes)
	for _, index := range indexes {
		if _, err := fmt.Fprintln(output, rows.Row(index, false, columns)); err != nil {
			return err
		}
	}
	return nil
}

type sortOrder int

const (
	providerOrder sortOrder = iota
	cheapestFirst
	dearestFirst
)

const (
	sortOrderCount = 3
	sortHelpRoom   = 100
)

type modelList struct {
	models   []*Model
	currency money.Currency
	order    sortOrder
}

func (self *modelList) CycleSort() {
	self.order = (self.order + 1) % sortOrderCount
}

func (self *modelList) Sort(indexes []int) {
	if self.order == providerOrder {
		return
	}

	slices.SortStableFunc(indexes, func(firstIndex, secondIndex int) int {
		first := self.models[firstIndex].Prices
		second := self.models[secondIndex].Prices
		isFirstKnown := first != nil && first.IsKnown()
		isSecondKnown := second != nil && second.IsKnown()
		switch {
		case isFirstKnown && !isSecondKnown:
			return -1
		case !isFirstKnown && isSecondKnown:
			return 1
		case !isFirstKnown:
			return 0
		}

		comparison := cmp.Compare(first.EstimatedRequestCost(), second.EstimatedRequestCost())
		if self.order == dearestFirst {
			return -comparison
		}

		return comparison
	})
}

func (self *modelList) Len() int { return len(self.models) }

func (self *modelList) IsChoosable(int) bool { return true }

func (self *modelList) KeyboardHelp(room int) []menu.Shortcut {
	if room < sortHelpRoom {
		return []menu.Shortcut{
			{Key: "ctrl+s", Description: "sort: " + self.sortLabel()},
			{Key: "enter", Description: "choose"},
			{Key: "esc", Description: "close"},
		}
	}

	return []menu.Shortcut{
		{Key: "↑↓", Description: "move"},
		{Key: "← →", Description: "effort"},
		{Key: "ctrl+s", Description: "sort: " + self.sortLabel()},
		{Key: "enter", Description: "choose"},
		{Key: "esc", Description: "close"},
	}
}

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
	return modelTable(self.order).Header(room)
}

func (self *modelList) Row(index int, isChosen bool, room int) string {
	paint := style.Answer
	if isChosen {
		paint = style.ChosenRow
	}

	return paint.Over(modelRow(self.models[index], self.currency, isChosen, room))
}

func (self *modelList) sortLabel() string {
	switch self.order {
	case cheapestFirst:
		return "cost ↑"
	case dearestFirst:
		return "cost ↓"
	case providerOrder:
		return "provider"
	}
	return "provider"
}

func modelTable(order sortOrder) *table.Table {
	title := costTitle
	switch order {
	case cheapestFirst:
		title += " ↑"
	case dearestFirst:
		title += " ↓"
	case providerOrder:
	}

	return table.New(
		table.Column{Title: "  Provider", Width: markWidth + providerColumn},
		table.Column{Title: "Model", Width: nameColumn},
		table.Column{Title: "Effort", Width: effortColumn},
		table.Column{Title: "Context", Width: contextColumn, Align: table.Right},
		table.Column{Title: title, Width: costColumn, Align: table.Right, MinRoom: costRoom},
		table.Column{Title: "Input", Width: inputColumn, Align: table.Right, MinRoom: rateRoom},
		table.Column{Title: "Output", Width: outputColumn, Align: table.Right, MinRoom: rateRoom},
		table.Column{Title: "Identifier", Width: identifierColumn, Style: style.Subtle},
	)
}

func modelRow(model *Model, currency money.Currency, isChosen bool, room int) string {
	return modelTable(providerOrder).Row([]string{
		menu.Mark(isChosen) + " " + model.Provider,
		model.Name,
		strutil.OrDash(model.Effort.String()),
		contextWindow(model.ContextWindowTokens),
		estimatedCost(model.Prices, currency),
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

func estimatedCost(prices *agent.TokenPrices, currency money.Currency) string {
	if prices == nil || !prices.IsKnown() {
		return unknownQuantity
	}

	return currency.FormatPrice(prices.EstimatedRequestCost() * costRequestCount)
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

	return currency.FormatPrice(rate(*prices))
}
