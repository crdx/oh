package model

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/modelsdev"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/provider/anthropic"
	"crdx.org/oh/pkg/provider/codex"
	"crdx.org/oh/pkg/provider/opencodego"
)

const refreshMessage = "Refreshing the model list..."

const (
	cacheVersion    = 5
	updateTimeout   = 90 * time.Second
	refreshTimeout  = 20 * time.Second
	maximumCacheAge = 7 * 24 * time.Hour
)

const (
	CodexProvider      = "codex"
	OpencodeGoProvider = "opencode-go"
	AnthropicProvider  = "anthropic"
	OllamaProvider     = "ollama"
)

var registryNames = map[string]string{
	CodexProvider:      "openai",
	OpencodeGoProvider: "opencode-go",
	AnthropicProvider:  "anthropic",
}

func ProviderNames() []string {
	return []string{CodexProvider, OpencodeGoProvider, AnthropicProvider, OllamaProvider}
}

func LoginProviderNames() []string {
	return []string{CodexProvider, OpencodeGoProvider, AnthropicProvider}
}

type modelCache struct {
	Version   int                     `json:"version"`
	CheckedAt time.Time               `json:"checked"`
	Providers map[string]cachedModels `json:"providers"`
}

type cachedModels struct {
	FetchedAt time.Time     `json:"fetched"`
	Source    string        `json:"source"`
	Models    []agent.Model `json:"models"`
}

const (
	sourceEndpoint   = "endpoint"
	sourceRegistry   = "models.dev"
	sourceBoth       = "endpoint+models.dev"
	sourceSimulation = "simulation"
)

func StoreSimulated(path string, providerName string, models []agent.Model) error {
	now := time.Now()

	return saveModelCache(path, modelCache{
		CheckedAt: now,
		Providers: map[string]cachedModels{
			providerName: {FetchedAt: now, Source: sourceSimulation, Models: models},
		},
	})
}

func registryAddress(endpoint string) string {
	if endpoint == "" {
		return ""
	}

	address, err := url.Parse(endpoint)
	if err != nil || address.Scheme == "" || address.Host == "" {
		return ""
	}

	wantedNames := make([]string, 0, len(registryNames))
	for _, name := range registryNames {
		wantedNames = append(wantedNames, name)
	}

	slices.Sort(wantedNames)

	query := url.Values{simulatedRegistryQuery: {strings.Join(wantedNames, ",")}}

	return address.Scheme + "://" + address.Host + simulatedRegistryPath + "?" + query.Encode()
}

const (
	simulatedRegistryPath  = "/models.dev/api.json"
	simulatedRegistryQuery = "providers"
)

func loadModelCache(path string) modelCache {
	empty := modelCache{Version: cacheVersion, Providers: map[string]cachedModels{}}

	data, err := os.ReadFile(path) //nolint:gosec // the path is ours
	if err != nil {
		return empty
	}

	var cache modelCache
	if json.Unmarshal(data, &cache) != nil || cache.Version != cacheVersion {
		return empty
	}

	if cache.Providers == nil {
		cache.Providers = map[string]cachedModels{}
	}

	for providerName, listing := range cache.Providers {
		listing.Models = plainModels(listing.Models)
		cache.Providers[providerName] = listing
	}

	return cache
}

func plainModels(models []agent.Model) []agent.Model {
	for i := range models {
		models[i].ID = plainly(models[i].ID)
		models[i].Name = plainly(models[i].Name)
	}

	return models
}

func saveModelCache(path string, cache modelCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	cache.Version = cacheVersion

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func supplement(
	providerName string,
	listedModels []agent.Model,
	registeredModels map[string]agent.Model,
) []agent.Model {
	supplementedModels := make([]agent.Model, 0, len(listedModels))

	for _, model := range listedModels {
		if knownModel, isFound := registeredModels[model.ID]; isFound {
			model = supplemented(providerName, model, knownModel)
		}

		supplementedModels = append(supplementedModels, model)
	}

	return supplementedModels
}

func supplemented(providerName string, model agent.Model, knownModel agent.Model) agent.Model {
	if providerName == CodexProvider && knownModel.ContextWindowTokens > 0 {
		model.ContextWindowTokens = knownModel.ContextWindowTokens
	}

	return filledFrom(model, knownModel)
}

func filledFrom(model agent.Model, knownModel agent.Model) agent.Model {
	if model.Name == "" {
		model.Name = knownModel.Name
	}
	if len(model.EffortLevels) == 0 {
		model.EffortLevels = slices.Clone(knownModel.EffortLevels)
	}
	if model.ContextWindowTokens == 0 {
		model.ContextWindowTokens = knownModel.ContextWindowTokens
	}
	if model.MaxOutputTokens == 0 {
		model.MaxOutputTokens = knownModel.MaxOutputTokens
	}
	if model.Prices == nil && knownModel.Prices != nil {
		prices := *knownModel.Prices
		model.Prices = &prices
	}

	return model
}

func fromRegistry(registeredModels map[string]agent.Model) []agent.Model {
	models := make([]agent.Model, 0, len(registeredModels))
	for _, model := range registeredModels {
		models = append(models, model)
	}

	slices.SortFunc(models, func(first agent.Model, second agent.Model) int {
		return cmp.Compare(first.ID, second.ID)
	})

	return models
}

var modelIterationPattern = regexp.MustCompile(`[0-9]+(?:[.-][0-9]+)*`)

type modelFamily struct {
	Prefix string
	Suffix string
}

type modelIteration struct {
	Numbers  []int
	Snapshot int
}

func (self modelIteration) precedes(other modelIteration) bool {
	if order := slices.Compare(self.Numbers, other.Numbers); order != 0 {
		return order < 0
	}

	return self.Snapshot < other.Snapshot
}

func (self modelIteration) matches(other modelIteration) bool {
	return self.Snapshot == other.Snapshot && slices.Equal(self.Numbers, other.Numbers)
}

func getModelIteration(id string) (modelFamily, modelIteration, bool) {
	location := modelIterationPattern.FindStringIndex(id)
	if location == nil {
		return modelFamily{}, modelIteration{}, false
	}

	family := modelFamily{
		Prefix: strings.TrimRight(id[:location[0]], "-."),
		Suffix: strings.TrimLeft(id[location[1]:], "-."),
	}

	var iteration modelIteration

	for part := range strings.FieldsFuncSeq(id[location[0]:location[1]], func(character rune) bool {
		return character == '.' || character == '-'
	}) {
		number, _ := strconv.Atoi(part)
		if util.IsDatedSnapshot(part) {
			iteration.Snapshot = number
			continue
		}

		iteration.Numbers = append(iteration.Numbers, number)
	}

	return family, iteration, true
}

type latestIteration struct {
	ID        string
	Iteration modelIteration
}

func latestModelIterations(models []agent.Model) map[modelFamily]latestIteration {
	latest := make(map[modelFamily]latestIteration)

	for _, model := range models {
		family, iteration, hasIteration := getModelIteration(model.ID)
		if !hasIteration {
			continue
		}

		knownLatest, hasKnown := latest[family]
		if !hasKnown || knownLatest.Iteration.precedes(iteration) {
			latest[family] = latestIteration{ID: model.ID, Iteration: iteration}
		}
	}

	return latest
}

func supersededBy(latest map[modelFamily]latestIteration, id string) (string, bool) {
	family, iteration, hasIteration := getModelIteration(id)
	if !hasIteration || iteration.matches(latest[family].Iteration) {
		return "", false
	}

	return latest[family].ID, true
}

func isDrivable(providerName string, id string) bool {
	switch providerName {
	case AnthropicProvider:
		return anthropic.SupportsAdaptiveThinking(id)
	case CodexProvider:
		return codex.SupportsResponses(id)
	case OpencodeGoProvider:
		return opencodego.SupportsCompletions(id)
	default:
		return true
	}
}

const undrivableReason = "unsupported request shape"

type ignoredModel struct {
	Name   string
	Reason string
}

func recordableModels(providerName string, models []agent.Model) ([]agent.Model, []ignoredModel) {
	latest := latestModelIterations(models)

	recordable := make([]agent.Model, 0, len(models))

	var ignoredModels []ignoredModel

	for _, model := range models {
		supersedingID, isSuperseded := supersededBy(latest, model.ID)

		switch {
		case isSuperseded:
			ignoredModels = append(ignoredModels, ignoredFor(model, "superseded by "+supersedingID))
		case !isDrivable(providerName, model.ID):
			ignoredModels = append(ignoredModels, ignoredFor(model, undrivableReason))
		default:
			recordable = append(recordable, model)
		}
	}

	return recordable, ignoredModels
}

func unselectableReason(model agent.Model) string {
	switch {
	case model.ID == "":
		return "unknown id"
	case len(model.EffortLevels) == 0:
		return "unknown effort level"
	case model.MaxOutputTokens <= 0:
		return "unknown output limit"
	default:
		return ""
	}
}

const unnamedModelName = "(unnamed)"

func modelName(model agent.Model) string {
	switch {
	case model.ID != "":
		return model.ID
	case model.Name != "":
		return model.Name
	default:
		return unnamedModelName
	}
}

func ignoredFor(model agent.Model, reason string) ignoredModel {
	return ignoredModel{Name: modelName(model), Reason: reason}
}

const (
	addedChange   = "added"
	removedChange = "removed"
)

type modelChange struct {
	Name   string
	Change string
}

func modelChanges(storedModels []agent.Model, models []agent.Model) []modelChange {
	var changes []modelChange

	for _, model := range models {
		if !holdsModel(storedModels, model.ID) {
			changes = append(changes, modelChange{Name: modelName(model), Change: addedChange})
		}
	}

	for _, model := range storedModels {
		if !holdsModel(models, model.ID) {
			changes = append(changes, modelChange{Name: modelName(model), Change: removedChange})
		}
	}

	return changes
}

func holdsModel(models []agent.Model, id string) bool {
	return slices.ContainsFunc(models, func(model agent.Model) bool {
		return model.ID == id
	})
}

func unselectableModels(models []agent.Model) []ignoredModel {
	var ignoredModels []ignoredModel

	for _, model := range models {
		if reason := unselectableReason(model); reason != "" {
			ignoredModels = append(ignoredModels, ignoredFor(model, reason))
		}
	}

	return ignoredModels
}

func choicesFor(providerName string, models []agent.Model) []Choice {
	choices := make([]Choice, 0, len(models))

	for _, model := range models {
		if unselectableReason(model) != "" || !isDrivable(providerName, model.ID) {
			continue
		}

		choices = append(choices, Choice{
			Provider:            providerName,
			ID:                  model.ID,
			Name:                model.Name,
			EffortLevels:        model.EffortLevels,
			ContextWindowTokens: model.ContextWindowTokens,
			MaxOutputTokens:     model.MaxOutputTokens,
			Prices:              model.Prices,
		})
	}

	return choices
}

func availableModelChoices(cache modelCache) []Choice {
	var available []Choice

	for _, providerName := range ProviderNames() {
		if listing, isFound := cache.Providers[providerName]; isFound {
			available = append(available, choicesFor(providerName, listing.Models)...)
		}
	}

	return available
}

func List(output io.Writer, path string, isAvailable func(providerName string) bool) error {
	choices := availableModelChoices(loadModelCache(path))
	if len(choices) == 0 {
		return errors.New("no models are known: run with -u to fetch the model list")
	}

	if choices = signedInto(choices, isAvailable); len(choices) == 0 {
		return ErrNotLoggedIn
	}

	for _, choice := range choices {
		if _, err := fmt.Fprintf(output, "%s/%s\n", choice.Provider, choice.ID); err != nil {
			return err
		}
	}

	return nil
}

type ProviderLister func(context.Context, string) ([]agent.Model, error)

func Ensure(output io.Writer, endpoint string, path string, seenPath string, listProviderModels ProviderLister) error {
	cache := loadModelCache(path)
	if isCacheCurrent(cache, time.Now()) {
		return nil
	}

	_, _ = fmt.Fprintln(output, style.Subtle(refreshMessage))

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	var reportedText bytes.Buffer

	reports, err := updateModels(ctx, &reportedText, endpoint, path, seenPath, listProviderModels, false)
	if err == nil {
		writeChangedModels(output, reports)
		return nil
	}

	_, _ = io.Copy(output, &reportedText)

	if len(cache.Providers) == 0 {
		return err
	}

	_, _ = fmt.Fprintln(output, style.Change("model list not refreshed: %s", err))

	cache.CheckedAt = time.Now()

	return saveModelCache(path, cache)
}

func isCacheCurrent(cache modelCache, now time.Time) bool {
	if len(cache.Providers) == 0 || cache.CheckedAt.IsZero() {
		return false
	}

	return now.Sub(cache.CheckedAt) < maximumCacheAge
}

func Update(
	output io.Writer,
	endpoint string,
	path string,
	seenPath string,
	listProviderModels ProviderLister,
	isShowingIgnored bool,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	_, err := updateModels(ctx, output, endpoint, path, seenPath, listProviderModels, isShowingIgnored)

	return err
}

func updateModels(
	ctx context.Context,
	output io.Writer,
	endpoint string,
	path string,
	seenPath string,
	listProviderModels ProviderLister,
	isShowingIgnored bool,
) ([]providerReport, error) {
	registry, err := modelsdev.Fetch(ctx, registryAddress(endpoint), nil)
	if err != nil {
		_, _ = fmt.Fprintln(output, style.Failure("models.dev: %s", err))
		registry = modelsdev.Registry{}
	}

	cache := loadModelCache(path)

	writeHeadings(output)

	var describedCount int

	reports := make([]providerReport, 0, len(ProviderNames()))
	listedByProvider := make(map[string][]agent.Model, len(ProviderNames()))

	for _, providerName := range ProviderNames() {
		registeredModels := registry.Provider(registryNames[providerName])
		storedListing, isStored := cache.Providers[providerName]

		listedModels, source, why := describeProviderModels(ctx, providerName, registeredModels, listProviderModels)
		listedModels = plainModels(listedModels)
		listedByProvider[providerName] = listedModels

		models, ignoredModels := recordableModels(providerName, listedModels)
		ignoredModels = append(ignoredModels, unselectableModels(models)...)

		report := providerReport{
			Provider:        providerName,
			Source:          source,
			ListedCount:     len(listedModels),
			SelectableCount: pickable(models),
			IgnoredModels:   ignoredModels,
		}

		if len(models) == 0 {
			report.Why = nothingRecordedReason(why, ignoredModels)
		} else {
			report.IsRecorded = true
			report.Why = why
			if isStored {
				report.ChangedModels = modelChanges(storedListing.Models, models)
			}
			cache.Providers[providerName] = cachedModels{
				FetchedAt: time.Now(),
				Source:    source,
				Models:    models,
			}
			describedCount++
		}

		reports = append(reports, report)
		writeProviderReport(output, report)
	}

	if err := recordSeenModels(seenPath, listedByProvider); err != nil {
		return reports, fmt.Errorf("record the models seen: %w", err)
	}

	writeChangedModels(output, reports)

	if isShowingIgnored {
		writeIgnoredModels(output, reports)
	}

	if describedCount == 0 {
		return reports, errors.New("no provider could be described")
	}

	cache.CheckedAt = time.Now()

	if err := saveModelCache(path, cache); err != nil {
		return reports, err
	}

	_, _ = fmt.Fprintln(output)
	_, _ = fmt.Fprintln(output, style.Subtle("Stored model list in ")+style.Normal(path))

	if !isShowingIgnored {
		writeIgnoredHint(output, reports)
	}

	return reports, nil
}

func pickable(models []agent.Model) int {
	var count int
	for _, model := range models {
		if unselectableReason(model) == "" {
			count++
		}
	}

	return count
}

func nothingRecordedReason(why string, ignoredModels []ignoredModel) string {
	if why != "" {
		return why
	}

	if len(ignoredModels) == 1 {
		return "the one model it offers is ignored"
	}

	return fmt.Sprintf("all %d models it offers are ignored", len(ignoredModels))
}

func describeProviderModels(
	ctx context.Context,
	providerName string,
	registeredModels map[string]agent.Model,
	listProviderModels ProviderLister,
) ([]agent.Model, string, string) {
	listedModels, err := listProviderModels(ctx, providerName)

	why := "the endpoint lists no models"
	if err != nil {
		why = err.Error()
	}

	if len(listedModels) > 0 {
		if len(registeredModels) == 0 {
			return listedModels, sourceEndpoint, ""
		}

		return supplement(providerName, listedModels, registeredModels), sourceBoth, ""
	}

	if len(registeredModels) > 0 {
		return fromRegistry(registeredModels), sourceRegistry, why
	}

	return nil, "", why
}
