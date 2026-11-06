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
	"crdx.org/oh/internal/app/tr"
	"crdx.org/oh/internal/app/width"
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

const providerTitle = "  " + tr.ProviderColumn

const (
	costRequestCount = 100
	costColumn       = 10
	costRoom         = markWidth + providerColumn + nameColumn + effortColumn +
		contextColumn + costColumn + shortenedIdentifier + costGaps*table.Gap
	rateRoom = markWidth + providerColumn + nameColumn + effortColumn + contextColumn +
		costColumn + inputColumn + outputColumn + identifierColumn + rateGaps*table.Gap
)

const (
	mediumCostFrom  = 1.0
	highCostFrom    = 5.0
	extremeCostFrom = 10.0
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
	IsIgnored           bool
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
		isFirstKnown := first != nil
		isSecondKnown := second != nil
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
			{Key: "s", Description: fmt.Sprintf(tr.ModelSortAction, self.sortLabel())},
			{Key: "enter", Description: tr.MenuChooseAction},
			{Key: "q", Description: tr.MenuQuitAction},
		}
	}

	return []menu.Shortcut{
		{Key: "↑↓", Description: tr.MenuMoveAction},
		{Key: "← →", Description: tr.ModelEffortAction},
		{Key: "s", Description: fmt.Sprintf(tr.ModelSortAction, self.sortLabel())},
		{Key: "enter", Description: tr.MenuChooseAction},
		{Key: "q", Description: tr.MenuQuitAction},
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
	model := self.models[index]
	if model.IsIgnored {
		return style.Subtle.Over(modelRow(model, self.currency, isChosen, room))
	}

	return style.Answer.Over(modelRow(model, self.currency, isChosen, room))
}

func (self *modelList) sortLabel() string {
	switch self.order {
	case cheapestFirst:
		return tr.CheapestFirstOrder
	case dearestFirst:
		return tr.DearestFirstOrder
	case providerOrder:
		return tr.ProviderOrder
	}
	return tr.ProviderOrder
}

func modelTable(order sortOrder) *table.Table {
	title := tr.CostColumn
	switch order {
	case cheapestFirst:
		title += " ↑"
	case dearestFirst:
		title += " ↓"
	case providerOrder:
	}

	return table.New(
		table.Column{Title: providerTitle, Width: titled(providerTitle, markWidth+providerColumn)},
		table.Column{Title: tr.ModelColumn, Width: titled(tr.ModelColumn, nameColumn)},
		table.Column{Title: tr.EffortColumn, Width: titled(tr.EffortColumn, effortColumn)},
		table.Column{Title: tr.ContextColumn, Width: titled(tr.ContextColumn, contextColumn), Align: table.Right},
		table.Column{Title: title, Width: titled(title, costColumn), Align: table.Right, MinRoom: costRoom},
		table.Column{Title: tr.InputColumn, Width: titled(tr.InputColumn, inputColumn), Align: table.Right, MinRoom: rateRoom},
		table.Column{Title: tr.OutputColumn, Width: titled(tr.OutputColumn, outputColumn), Align: table.Right, MinRoom: rateRoom},
		table.Column{Title: tr.IdentifierColumn, Width: titled(tr.IdentifierColumn, identifierColumn), Style: style.Subtle},
	)
}

func titled(title string, minimum int) int {
	return max(minimum, width.Of(title))
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
	if prices == nil {
		return style.Subtle(unknownQuantity)
	}

	dollars := prices.EstimatedRequestCost() * costRequestCount
	if dollars == 0 {
		return style.LowPrice(unknownQuantity)
	}

	return costStyle(dollars)(currency.FormatPrice(dollars))
}

func costStyle(dollars float64) style.Style {
	switch {
	case dollars >= extremeCostFrom:
		return style.WtfPrice
	case dollars >= highCostFrom:
		return style.HighPrice
	case dollars >= mediumCostFrom:
		return style.MediumPrice
	default:
		return style.LowPrice
	}
}

func inputRate(prices agent.TokenPrices) float64 {
	return prices.Input
}

func outputRate(prices agent.TokenPrices) float64 {
	return prices.Output
}

func price(prices *agent.TokenPrices, currency money.Currency, rate func(agent.TokenPrices) float64) string {
	if prices == nil {
		return unknownQuantity
	}

	return currency.FormatPrice(rate(*prices))
}
