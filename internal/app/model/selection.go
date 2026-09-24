package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"crdx.org/oh/pkg/agent"
)

type Choice struct {
	Provider            string             `json:"provider"`
	ID                  string             `json:"id"`
	Name                string             `json:"name,omitempty"`
	EffortLevels        []string           `json:"efforts,omitempty"`
	ContextWindowTokens int                `json:"context,omitempty"`
	MaxOutputTokens     int                `json:"output,omitempty"`
	Prices              *agent.TokenPrices `json:"prices,omitempty"`
}

func Chosen(path string, seenPath string, providerName string, model string) (Choice, error) {
	for _, choice := range slices.Concat(Choices(path), seenChoices(seenPath)) {
		if choice.Provider == providerName && choice.ID == model {
			return choice, nil
		}
	}

	if !isDrivable(providerName, model) {
		return Choice{}, fmt.Errorf("%s/%s: %s", providerName, model, undrivableReason)
	}

	return Choice{}, fmt.Errorf(
		"nothing is known about %s/%s: run with -u to update the model list", providerName, model,
	)
}

func Choices(path string) []Choice {
	return availableModelChoices(loadModelCache(path))
}

type PricedModel struct {
	Provider string
	ID       string
	Prices   agent.TokenPrices
}

func ListedPrices(path string) []PricedModel {
	cache := loadModelCache(path)

	var pricedModels []PricedModel

	for _, providerName := range ProviderNames() {
		listing, isFound := cache.Providers[providerName]
		if !isFound {
			continue
		}

		for _, listedModel := range listing.Models {
			if listedModel.ID == "" || listedModel.Prices == nil || !listedModel.Prices.IsKnown() {
				continue
			}

			pricedModels = append(pricedModels, PricedModel{
				Provider: providerName,
				ID:       listedModel.ID,
				Prices:   *listedModel.Prices,
			})
		}
	}

	return pricedModels
}

func ParseSelection(path string, writtenSelection string, defaults Defaults) (Selection, error) {
	selectionQuery, isFast, err := splitFastMode(writtenSelection)
	if err != nil {
		return Selection{}, err
	}

	modelQuery, effortQuery, hasEffort := strings.Cut(selectionQuery, "@")
	if modelQuery == "" || (hasEffort && effortQuery == "") {
		return Selection{}, fmt.Errorf(
			"model must be written as provider/model or provider/model@effort[+fast], got %q", writtenSelection,
		)
	}

	choice, err := matchModel(modelQuery, Choices(path))
	if err != nil {
		return Selection{}, err
	}
	if isFast && !SupportsFastMode(choice.Provider) {
		return Selection{}, fmt.Errorf("%s does not support fast mode", choice.Provider)
	}

	effort := defaults.EffortFor(choice.EffortLevels)

	switch {
	case hasEffort:
		if effort, err = matchEffort(effortQuery, choice); err != nil {
			return Selection{}, err
		}
	case effort == "":
		return Selection{}, fmt.Errorf("model %s has no recognised effort levels", choice.ID)
	default:
		isFast = isFast || defaults.IsFastFor(choice.Provider)
	}

	return Selection{Provider: choice.Provider, Model: choice.ID, Effort: effort, IsFast: isFast}, nil
}

func ResolveQuery(query string, choices []Choice, defaults Defaults) (Selection, error) {
	selectionQuery, isFast, err := splitFastMode(query)
	if err != nil {
		return Selection{}, err
	}

	modelQuery, effortQuery, hasEffort := strings.Cut(selectionQuery, "@")
	if modelQuery == "" || (hasEffort && effortQuery == "") {
		return Selection{}, fmt.Errorf("model must be written as model or model@effort[+fast], got %q", query)
	}

	choice, err := onlyMatch(modelQuery, RankedChoicesWithoutGuessing(modelQuery, choices))
	if err != nil {
		return Selection{}, err
	}
	if isFast && !SupportsFastMode(choice.Provider) {
		return Selection{}, fmt.Errorf("%s does not support fast mode", choice.Provider)
	}

	effort := defaults.EffortFor(choice.EffortLevels)

	switch {
	case hasEffort:
		if effort, err = matchEffort(effortQuery, choice); err != nil {
			return Selection{}, err
		}
	case effort == "":
		return Selection{}, fmt.Errorf("model %s has no recognised effort levels", choice.ID)
	default:
		isFast = isFast || defaults.IsFastFor(choice.Provider)
	}

	return Selection{Provider: choice.Provider, Model: choice.ID, Effort: effort, IsFast: isFast}, nil
}

func splitFastMode(writtenSelection string) (string, bool, error) {
	selectionQuery, mode, hasMode := strings.Cut(writtenSelection, "+")
	if !hasMode {
		return selectionQuery, false, nil
	}
	if mode != "fast" {
		return "", false, fmt.Errorf("model mode must be fast, got %q", mode)
	}

	return selectionQuery, true, nil
}

func SupportsFastMode(providerName string) bool {
	return providerName == CodexProvider
}

var EffortOrder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

var (
	highestEffort = EffortOrder[len(EffortOrder)-1]
	effortFloor   = "high"
)

type Effort string

func (self *Effort) UnmarshalText(text []byte) error {
	level := ResolveEffort(strings.TrimSpace(string(text)))
	if !slices.Contains(EffortOrder, level) {
		return fmt.Errorf("effort must be one of: %s", strings.Join(EffortOrder, ", "))
	}

	*self = Effort(level)
	return nil
}

type Defaults struct {
	Effort Effort
	IsFast bool
}

func (self Defaults) EffortFor(available []string) string {
	wantedEffort := string(self.Effort)
	if wantedEffort == "" {
		wantedEffort = highestEffort
	}

	if allowedEfforts := effortsAtOrAbove(effortFloor, available); len(allowedEfforts) > 0 {
		if slices.Index(EffortOrder, wantedEffort) < slices.Index(EffortOrder, effortFloor) {
			wantedEffort = effortFloor
		}

		available = allowedEfforts
	}

	return NearestEffort(wantedEffort, available)
}

func effortsAtOrAbove(level string, available []string) []string {
	var levels []string

	for _, candidate := range available {
		if slices.Index(EffortOrder, candidate) >= slices.Index(EffortOrder, level) {
			levels = append(levels, candidate)
		}
	}

	return levels
}

func (self Defaults) IsFastFor(providerName string) bool {
	return self.IsFast && SupportsFastMode(providerName)
}

func NearestEffort(current string, available []string) string {
	currentIndex := slices.Index(EffortOrder, current)

	for distance := range EffortOrder {
		higherIndex := currentIndex + distance
		if higherIndex >= 0 && higherIndex < len(EffortOrder) && slices.Contains(available, EffortOrder[higherIndex]) {
			return EffortOrder[higherIndex]
		}

		lowerIndex := currentIndex - distance
		if distance > 0 && lowerIndex >= 0 && slices.Contains(available, EffortOrder[lowerIndex]) {
			return EffortOrder[lowerIndex]
		}
	}

	return ""
}

func matchEffort(query string, choice Choice) (string, error) {
	levels := EffortsMatching(query, choice.EffortLevels)

	switch len(levels) {
	case 0:
		return "", fmt.Errorf("effort %q does not match any of: %s", query, strings.Join(choice.EffortLevels, ", "))
	case 1:
		return levels[0], nil
	default:
		return "", fmt.Errorf("effort %q is ambiguous; matches: %s", query, strings.Join(levels, ", "))
	}
}

var effortAliases = []struct {
	name  string
	level string
}{
	{name: "off", level: "none"},
}

func EffortsMatching(query string, efforts []string) []string {
	names := slices.Clone(efforts)

	for _, alias := range effortAliases {
		if slices.Contains(efforts, alias.level) {
			names = append(names, alias.name)
		}
	}

	var levels []string

	for _, name := range matchPrefixes(query, names) {
		if level := ResolveEffort(name); !slices.Contains(levels, level) {
			levels = append(levels, level)
		}
	}

	return levels
}

func ResolveEffort(name string) string {
	for _, alias := range effortAliases {
		if alias.name == strings.ToLower(name) {
			return alias.level
		}
	}

	return name
}

type matcher func(candidate string, query string) bool

var certainMatchTiers = []matcher{
	func(candidate string, query string) bool { return candidate == query },
	strings.HasPrefix,
	strings.Contains,
}

var matchTiers = slices.Concat(certainMatchTiers, []matcher{holdsInOrder})

func matchModel(query string, choices []Choice) (Choice, error) {
	if len(choices) == 0 {
		return Choice{}, errors.New("no models are known: run with -u to fetch the model list")
	}

	return onlyMatch(query, RankedChoices(query, choices))
}

func onlyMatch(query string, matches []Choice) (Choice, error) {
	switch len(matches) {
	case 0:
		return Choice{}, fmt.Errorf("model %q does not match any known model", query)
	case 1:
		return matches[0], nil
	default:
		names := make([]string, len(matches))
		for i, choice := range matches {
			names[i] = choice.Provider + "/" + choice.ID
		}
		return Choice{}, fmt.Errorf("model %q is ambiguous; matches: %s", query, strings.Join(names, ", "))
	}
}

func RankedChoices(query string, choices []Choice) []Choice {
	return rankedChoices(query, choices, matchTiers)
}

func RankedChoicesWithoutGuessing(query string, choices []Choice) []Choice {
	return rankedChoices(query, choices, certainMatchTiers)
}

func rankedChoices(query string, choices []Choice, tiers []matcher) []Choice {
	for _, matchTier := range tiers {
		if matches := matchingModels(query, choices, matchTier); len(matches) > 0 {
			return matches
		}
	}

	return nil
}

func matchingModels(query string, choices []Choice, matchTier matcher) []Choice {
	query = strings.ToLower(query)
	namesProvider := strings.Contains(query, "/")

	var matches []Choice

	for _, choice := range choices {
		candidate := strings.ToLower(choice.ID)
		if namesProvider {
			candidate = strings.ToLower(choice.Provider + "/" + choice.ID)
		}

		if matchTier(candidate, query) {
			matches = append(matches, choice)
		}
	}

	return matches
}

func matchPrefixes(query string, candidates []string) []string {
	query = strings.ToLower(query)
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(strings.ToLower(candidate), query) {
			matches = append(matches, candidate)
		}
	}
	return matches
}

func holdsInOrder(candidate string, query string) bool {
	wantedRunes := []rune(query)

	var matchedCount int

	for _, letter := range candidate {
		if matchedCount < len(wantedRunes) && letter == wantedRunes[matchedCount] {
			matchedCount++
		}
	}

	return matchedCount == len(wantedRunes)
}
