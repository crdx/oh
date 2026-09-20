package experimental

import (
	"fmt"
	"maps"
	"slices"
	"sync/atomic"
)

type Name string

type Kind int

const (
	BooleanKind Kind = iota
	TextKind
	WholeNumberKind
	DecimalNumberKind
)

func (self Kind) String() string {
	switch self {
	case BooleanKind:
		return "true or false"
	case TextKind:
		return "a string"
	case WholeNumberKind:
		return "a whole number"
	case DecimalNumberKind:
		return "a number"
	}
	return "a value"
}

var toggleKinds = map[Name]Kind{}

type Complaint struct {
	Name   string
	Reason string
}

func Check(values map[string]any) []Complaint {
	return check(toggleKinds, values)
}

func check(toggles map[Name]Kind, values map[string]any) []Complaint {
	var complaints []Complaint

	for _, name := range slices.Sorted(maps.Keys(values)) {
		kind, hasKind := toggles[Name(name)]
		if !hasKind {
			complaints = append(complaints, Complaint{
				Name:   name,
				Reason: "no toggle by this name, so it does nothing",
			})
			continue
		}
		if !kind.accepts(values[name]) {
			complaints = append(complaints, Complaint{
				Name:   name,
				Reason: fmt.Sprintf("this toggle wants %s, so it does nothing", kind),
			})
		}
	}

	return complaints
}

func (self Kind) accepts(value any) bool {
	switch self {
	case BooleanKind:
		_, isBoolean := value.(bool)
		return isBoolean
	case TextKind:
		_, isText := value.(string)
		return isText
	case WholeNumberKind:
		_, isWholeNumber := value.(int64)
		return isWholeNumber
	case DecimalNumberKind:
		_, isDecimalNumber := value.(float64)
		return isDecimalNumber
	}
	return false
}

type Toggles struct {
	values atomic.Pointer[map[string]any]
}

func New(values map[string]any) *Toggles {
	toggles := &Toggles{}
	toggles.Replace(values)
	return toggles
}

func (self *Toggles) Replace(values map[string]any) {
	replacement := maps.Clone(values)
	self.values.Store(&replacement)
}

func (self *Toggles) IsEnabled(name Name) bool {
	value, _ := read[bool](self, name)
	return value
}

func (self *Toggles) GetText(name Name, fallback string) string {
	if value, exists := read[string](self, name); exists {
		return value
	}
	return fallback
}

func (self *Toggles) GetWholeNumber(name Name, fallback int) int {
	if value, exists := read[int64](self, name); exists {
		return int(value)
	}
	return fallback
}

func (self *Toggles) GetDecimalNumber(name Name, fallback float64) float64 {
	if value, exists := read[float64](self, name); exists {
		return value
	}
	return fallback
}

func read[T any](toggles *Toggles, name Name) (T, bool) {
	var absent T
	if toggles == nil {
		return absent, false
	}
	values := toggles.values.Load()
	if values == nil {
		return absent, false
	}
	value, exists := (*values)[string(name)]
	if !exists {
		return absent, false
	}
	match, isMatch := value.(T)
	if !isMatch {
		return absent, false
	}
	return match, true
}
