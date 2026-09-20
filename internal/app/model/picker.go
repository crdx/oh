package model

import (
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/model/picker"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/money"
)

var ErrNotLoggedIn = errors.New("not logged in to any provider: run oh -L to sign in")

func Choose(
	path string,
	currency money.Currency,
	isLoggedIn func(providerName string) bool,
	terminal *os.File,
	screen io.Writer,
	defaults Defaults,
) (Selection, error) {
	choices := Choices(path)
	if len(choices) == 0 {
		return Selection{}, errors.New("no models are known: run with -u to fetch the model list")
	}

	if choices = signedInto(choices, isLoggedIn); len(choices) == 0 {
		return Selection{}, ErrNotLoggedIn
	}

	chosenModel, err := picker.Choose(offered(choices, defaults), currency, terminal, screen)
	if err != nil {
		return Selection{}, err
	}

	return Selection{
		Provider: chosenModel.ProviderID,
		Model:    chosenModel.ID,
		Effort:   chosenModel.Effort.Level,
		IsFast:   chosenModel.Effort.IsFast,
	}, nil
}

func ChooseWhenNoneSelected(
	reason error,
	path string,
	currency money.Currency,
	isLoggedIn func(providerName string) bool,
	terminal *os.File,
	screen io.Writer,
	defaults Defaults,
) (Selection, error) {
	if !errors.Is(reason, ErrNoSelection) || !tty.Is(terminal) || !tty.Is(screen) {
		return Selection{}, reason
	}

	return Choose(path, currency, isLoggedIn, terminal, screen, defaults)
}

func signedInto(choices []Choice, isLoggedIn func(providerName string) bool) []Choice {
	available := make([]Choice, 0, len(choices))

	for _, choice := range choices {
		if isLoggedIn(choice.Provider) {
			available = append(available, choice)
		}
	}

	return available
}

func offered(choices []Choice, defaults Defaults) []*picker.Model {
	models := make([]*picker.Model, 0, len(choices))

	for _, choice := range choices {
		efforts := orderedEfforts(choice.EffortLevels)
		effort := defaults.EffortFor(efforts)
		if effort == "" {
			effort = efforts[0]
		}

		models = append(models, &picker.Model{
			Provider:            ProviderName(choice.Provider),
			ProviderID:          choice.Provider,
			Name:                strings.Join(DisplayName(choice.ID), " "),
			ID:                  choice.ID,
			EffortLevels:        effortLadder(efforts, SupportsFastMode(choice.Provider)),
			Effort:              picker.Effort{Level: effort, IsFast: defaults.IsFastFor(choice.Provider)},
			ContextWindowTokens: choice.ContextWindowTokens,
			Prices:              choice.Prices,
		})
	}

	return models
}

func effortLadder(efforts []string, isFastOffered bool) []picker.Effort {
	ladder := make([]picker.Effort, 0, len(efforts)*2)

	for _, effort := range efforts {
		ladder = append(ladder, picker.Effort{Level: effort})

		if isFastOffered {
			ladder = append(ladder, picker.Effort{Level: effort, IsFast: true})
		}
	}

	return ladder
}

func orderedEfforts(efforts []string) []string {
	orderedEfforts := slices.Clone(efforts)

	slices.SortStableFunc(orderedEfforts, func(first string, second string) int {
		return slices.Index(EffortOrder, first) - slices.Index(EffortOrder, second)
	})

	return orderedEfforts
}
