package money

import (
	"fmt"
	"math"
	"strings"
)

const (
	DollarCode   = "USD"
	dollarSymbol = "$"

	belowMark       = "<"
	smallAmount     = 0.01
	smallestAmount  = 0.0001
	moneyPlaces     = 2
	ratePlaces      = 1
	shownRatePlaces = 2
	rateFigures     = 2
	fractionPlaces  = 4
	wholeUnit       = 1.0
)

var symbols = map[string]string{
	"AUD": "A$",
	"BRL": "R$",
	"CAD": "C$",
	"CHF": "CHF ",
	"CNY": "CN¥",
	"CZK": "Kč",
	"DKK": "kr",
	"EUR": "€",
	"GBP": "£",
	"HKD": "HK$",
	"ILS": "₪",
	"INR": "₹",
	"JPY": "¥",
	"KRW": "₩",
	"MXN": "Mex$",
	"NOK": "kr",
	"NZD": "NZ$",
	"PLN": "zł",
	"SEK": "kr",
	"SGD": "S$",
	"TRY": "₺",
	"USD": dollarSymbol,
	"ZAR": "R",
}

type Currency struct {
	Code   string
	Symbol string
	Rate   float64
}

func Dollar() Currency {
	return Currency{Code: DollarCode, Symbol: dollarSymbol, Rate: 1}
}

func In(code string, rate float64) Currency {
	if code == "" || code == DollarCode || rate <= 0 {
		return Dollar()
	}

	symbol, isNamed := symbols[code]
	if !isNamed {
		symbol = code + " "
	}

	return Currency{Code: code, Symbol: symbol, Rate: rate}
}

func (self Currency) IsDollar() bool {
	return self.Code == "" || self.Code == DollarCode
}

func (self Currency) Format(dollars float64) string {
	symbol, amount := self.convert(dollars)

	switch {
	case amount <= 0:
		return symbol + fmt.Sprintf("%.*f", moneyPlaces, 0.0)
	case amount < smallestAmount:
		return belowMark + symbol + fmt.Sprintf("%.*f", fractionPlaces, smallestAmount)
	case amount < smallAmount:
		return symbol + formatPlaces(amount, fractionPlaces)
	default:
		return symbol + formatPlaces(amount, moneyPlaces)
	}
}

func (self Currency) FormatRate(dollars float64) string {
	symbol, amount := self.convert(dollars)

	switch {
	case amount <= 0:
		return symbol + fmt.Sprintf("%.*f", shownRatePlaces, 0.0)
	case amount < smallestAmount:
		return belowMark + symbol + fmt.Sprintf("%.*f", fractionPlaces, smallestAmount)
	}

	if amount >= wholeUnit {
		return symbol + fmt.Sprintf("%.*f", shownRatePlaces, roundedTo(amount, ratePlaces))
	}

	return symbol + trimmedTo(formatPlaces(amount, significantPlaces(amount)), shownRatePlaces)
}

func (self Currency) FormatPrice(dollars float64) string {
	symbol, amount := self.convert(dollars)

	if amount > 0 && amount < smallAmount {
		return belowMark + symbol + formatPlaces(smallAmount, moneyPlaces)
	}

	return symbol + formatPlaces(max(amount, 0), moneyPlaces)
}

func trimmedTo(formattedAmount string, minimumPlaces int) string {
	point := strings.IndexByte(formattedAmount, '.')
	if point < 0 {
		return formattedAmount
	}

	for len(formattedAmount)-point-1 > minimumPlaces && strings.HasSuffix(formattedAmount, "0") {
		formattedAmount = formattedAmount[:len(formattedAmount)-1]
	}

	return formattedAmount
}

func significantPlaces(amount float64) int {
	leadingPlace := int(math.Floor(math.Log10(amount)))

	return min(rateFigures-leadingPlace-1, fractionPlaces)
}

func formatPlaces(amount float64, places int) string {
	return fmt.Sprintf("%.*f", places, roundedTo(amount, places))
}

func roundedTo(amount float64, places int) float64 {
	scale := math.Pow(10, float64(places))

	return math.Floor(amount*scale+0.5) / scale
}

func (self Currency) convert(dollars float64) (string, float64) {
	symbol := self.Symbol
	if symbol == "" {
		symbol = dollarSymbol
	}

	rate := self.Rate
	if rate <= 0 {
		rate = 1
	}

	return symbol, dollars * rate
}
