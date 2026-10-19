package model

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"crdx.org/oh/internal/app/model/picker"
	"crdx.org/oh/internal/app/tty"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/agent"
)

var ErrNotLoggedIn = errors.New("not logged in to any provider: run oh -L to sign in")

func List(
	output io.Writer,
	path string,
	ignoredChoices []Choice,
	currency money.Currency,
	isLoggedIn func(providerName string) bool,
	defaults Defaults,
	columns int,
) error {
	choices, err := selectableChoices(path, isLoggedIn)
	if err != nil {
		return err
	}

	models := offered(choices, defaults)
	for _, model := range offered(signedInto(ignoredChoices, isLoggedIn), defaults) {
		model.IsIgnored = true
		models = append(models, model)
	}

	return picker.Print(output, models, currency, columns)
}

func ListNames(
	output io.Writer,
	path string,
	ignoredChoices []Choice,
	isLoggedIn func(providerName string) bool,
) error {
	choices, err := selectableChoices(path, isLoggedIn)
	if err != nil {
		return err
	}

	for _, choice := range append(choices, signedInto(ignoredChoices, isLoggedIn)...) {
		if _, err := fmt.Fprintf(output, "%s/%s\n", choice.Provider, choice.ID); err != nil {
			return err
		}
	}
	return nil
}

func IgnoredChoices(path string, seenPath string) ([]Choice, error) {
	seen, err := seenChoices(seenPath)
	if err != nil {
		return nil, fmt.Errorf("read the models seen before: %w", err)
	}

	choices := Choices(path)

	return slices.DeleteFunc(seen, func(seenChoice Choice) bool {
		_, isListed := chosenAmong(choices, seenChoice.Provider, seenChoice.ID)
		return isListed
	}), nil
}

func selectableChoices(path string, isLoggedIn func(providerName string) bool) ([]Choice, error) {
	choices := Choices(path)
	if len(choices) == 0 {
		return nil, errors.New("no models are known: run with -u to fetch the model list")
	}

	if choices = signedInto(choices, isLoggedIn); len(choices) == 0 {
		return nil, ErrNotLoggedIn
	}

	return choices, nil
}

func Choose(
	path string,
	currency money.Currency,
	isLoggedIn func(providerName string) bool,
	terminal *os.File,
	screen io.Writer,
	defaults Defaults,
) (Selection, error) {
	choices, err := selectableChoices(path, isLoggedIn)
	if err != nil {
		return Selection{}, err
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
		if effort == "" && len(efforts) > 0 {
			effort = efforts[0]
		}

		prices := choice.Prices
		if prices == nil && choice.IsGoQuotaExempt() {
			prices = &agent.TokenPrices{}
		}

		models = append(models, &picker.Model{
			Provider:            ProviderName(choice.Provider),
			ProviderID:          choice.Provider,
			Name:                strings.Join(DisplayName(choice.ID), " "),
			ID:                  choice.ID,
			EffortLevels:        effortLadder(efforts, SupportsFastMode(choice.Provider)),
			Effort:              picker.Effort{Level: effort, IsFast: defaults.IsFastFor(choice.Provider)},
			ContextWindowTokens: choice.ContextWindowTokens,
			Prices:              prices,
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
