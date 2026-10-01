package ask

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"
)

var ErrDenied = errors.New("the request was denied")

const (
	yesIndex = 0
	noIndex  = 1
)

type Option struct {
	Key   rune
	Label string
}

type Field struct {
	Name  string
	Value string
}

type Question struct {
	Label    string
	Intent   string
	Lapse    string
	Detail   string
	Language string
	Fields   []Field
	Options  []Option
	Default  int
}

func (self Question) DefaultIndex() int {
	if self.Default < 0 || self.Default >= len(self.Options) {
		return 0
	}

	return self.Default
}

func (self Question) IndexForKey(key rune) int {
	for index, option := range self.Options {
		if option.Key == key {
			return index
		}
	}

	return noChoice
}

type Confirmation struct {
	Label    string
	Intent   string
	Detail   string
	Language string
	Fields   []Field
}

const confirmationLapse = "auto-denies"

var confirmationOptions = []Option{
	{Key: 'y', Label: "Yes"},
	{Key: 'n', Label: "No"},
}

func (self Confirmation) Question() Question {
	return Question{
		Label:    self.Label,
		Intent:   self.Intent,
		Lapse:    confirmationLapse,
		Detail:   self.Detail,
		Language: self.Language,
		Fields:   slices.Clone(self.Fields),
		Options:  slices.Clone(confirmationOptions),
		Default:  yesIndex,
	}
}

func Confirm(ctx context.Context, broker *Broker, confirmation Confirmation) error {
	index, err := broker.Ask(ctx, confirmation.Question())
	return confirmationResult(index, err)
}

func ConfirmWithin(
	ctx context.Context,
	broker *Broker,
	confirmation Confirmation,
	lapseAfter time.Duration,
) error {
	index, err := broker.AskWithin(ctx, confirmation.Question(), lapseAfter)
	return confirmationResult(index, err)
}

func confirmationResult(index int, err error) error {
	switch {
	case errors.Is(err, ErrCancelled):
		return ErrDenied
	case err != nil:
		return err
	case index != yesIndex:
		return ErrDenied
	}

	return nil
}

type Choice struct {
	Label  string
	Detail string
	Labels []string
}

func (self Choice) Question() Question {
	options := make([]Option, 0, len(self.Labels))

	for index, label := range self.Labels {
		option := Option{Label: label}
		if index < 9 {
			option.Key = []rune(strconv.Itoa(index + 1))[0]
		}
		options = append(options, option)
	}

	return Question{
		Label:   self.Label,
		Detail:  self.Detail,
		Options: options,
	}
}
