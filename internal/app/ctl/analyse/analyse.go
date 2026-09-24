package analyse

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"crdx.org/duckopt/v2"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/metrics"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/session"
)

const usage = `oh --ctl analyse — analyse stored sessions

Usage:
    $0 --ctl analyse [options] [<session>...]

Options:
    -j, --json    Write the analysis as JSON
    -h, --help    Show this help
`

const journalTranscriptName = "session.jsonl"

type inputOpts struct {
	IsControlling bool     `docopt:"--ctl"`
	Analyse       bool     `docopt:"analyse"`
	JSON          bool     `docopt:"--json"`
	Sessions      []string `docopt:"<session>"`
}

type Analysis struct {
	PromptCache     PromptCacheAnalysis `json:"promptCache"`
	Models          ModelAnalysis       `json:"models"`
	Activity        ActivityAnalysis    `json:"activity"`
	Faults          FaultAnalysis       `json:"faults"`
	Tools           ToolAnalysis        `json:"tools"`
	Sessions        []SessionStatistics `json:"sessions,omitempty"`
	SkippedSessions int                 `json:"skippedSessions"`
}

type PromptCacheAnalysis struct {
	Providers []CacheStatistics `json:"providers"`
	Total     CacheStatistics   `json:"total"`
}

type ModelAnalysis struct {
	Models         []ModelStatistics `json:"models"`
	Total          ModelStatistics   `json:"total"`
	UnpricedModels int               `json:"unpricedModels"`
}

type ActivityAnalysis struct {
	Providers []ActivityStatistics `json:"providers"`
	Total     ActivityStatistics   `json:"total"`
}

type FaultAnalysis struct {
	Providers []FaultStatistics `json:"providers"`
	Total     FaultStatistics   `json:"total"`
}

type ToolAnalysis struct {
	Tools []ToolStatistics `json:"tools"`
	Total ToolStatistics   `json:"total"`
}

const analysisCacheFormat = 5

type analysisCache struct {
	Format   int                      `json:"format"`
	Sessions map[string]cachedSession `json:"sessions"`
}

type cachedSession struct {
	JournalSize       int64             `json:"journalSize"`
	JournalModifiedAt int64             `json:"journalModifiedAt"`
	IsWhole           bool              `json:"isWhole"`
	Statistics        SessionStatistics `json:"statistics"`
}

type Settings struct {
	SessionsDir    string
	CachePath      string
	ModelCachePath string
	Currency       money.Currency
	Names          []string
	IsJSON         bool
}

func Run() error {
	options := duckopt.MustBind[inputOpts](usage, "$0")

	return run(Settings{
		SessionsDir:    location.GetSessionsDir(),
		CachePath:      location.GetAnalysisCachePath(),
		ModelCachePath: location.GetModelCachePath(),
		Currency:       readCurrency(),
		Names:          options.Sessions,
		IsJSON:         options.JSON,
	}, console.Standard())
}

func readCurrency() money.Currency {
	settings, err := config.Load(location.GetConfigFile())
	if err != nil {
		return money.Dollar()
	}

	code := strings.ToUpper(strings.TrimSpace(settings.Ui.Currency))
	if code == "" || code == money.DollarCode {
		return money.Dollar()
	}

	return money.Load(location.GetExchangeRateCachePath(), code)
}

func run(settings Settings, output console.Output) error {
	selectedNames, err := selectNames(settings.SessionsDir, settings.Names)
	if err != nil {
		return err
	}

	sessions, skippedCount, err := readSessions(settings.SessionsDir, settings.CachePath, selectedNames)
	if err != nil {
		return err
	}

	analysis := aggregate(sessions, readPricebook(settings.ModelCachePath))
	analysis.SkippedSessions = skippedCount

	if settings.IsJSON {
		return writeJSON(analysis, output.Screen)
	}

	return writeText(analysis, presentation{
		currency:     settings.Currency,
		isPerSession: len(settings.Names) > 0,
	}, output.Screen)
}

func selectNames(directory string, requestedNames []string) ([]string, error) {
	entries, err := session.Entries(directory)
	if err != nil {
		return nil, err
	}

	isArchived := make(map[string]bool, len(entries))
	for _, entry := range entries {
		isArchived[entry.Name] = entry.IsArchived
	}

	if len(requestedNames) == 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsArchived {
				continue
			}
			names = append(names, entry.Name)
		}
		return names, nil
	}

	for _, name := range requestedNames {
		if _, exists := isArchived[name]; !exists {
			return nil, fmt.Errorf("there is no stored session named %q", name)
		}
		if isArchived[name] {
			return nil, fmt.Errorf("the session %q is archived", name)
		}
	}

	return requestedNames, nil
}

type analysisJob struct {
	name             string
	cachedStatistics cachedSession
	hasCached        bool
}

type sessionAnalysis struct {
	name             string
	statistics       SessionStatistics
	cachedStatistics cachedSession
	err              error
	hasCached        bool
	isWhole          bool
}

func readSessions(directory string, cachePath string, names []string) ([]SessionStatistics, int, error) {
	cache := readAnalysisCache(cachePath)
	jobs := make(chan analysisJob, len(names))
	results := make(chan sessionAnalysis)
	var workers sync.WaitGroup

	for _, name := range names {
		cachedStatistics, hasCached := cache.Sessions[name]
		jobs <- analysisJob{name: name, cachedStatistics: cachedStatistics, hasCached: hasCached}
	}
	close(jobs)

	for range min(runtime.GOMAXPROCS(0), len(names)) {
		workers.Go(func() {
			for job := range jobs {
				result := analyseSessionWithCache(directory, job.name, job.cachedStatistics, job.hasCached)
				result.name = job.name
				results <- result
			}
		})
	}
	go func() {
		workers.Wait()
		close(results)
	}()

	sessions := make([]SessionStatistics, 0, len(names))
	skippedCount := 0
	var firstError error
	for result := range results {
		if result.err != nil {
			if firstError == nil {
				firstError = result.err
			}
			continue
		}
		if result.hasCached {
			cache.Sessions[result.name] = result.cachedStatistics
		}
		if !result.isWhole {
			skippedCount++
			continue
		}
		statistics := result.statistics
		statistics.Name = result.name
		sessions = append(sessions, statistics)
	}
	if firstError != nil {
		return nil, 0, firstError
	}
	_ = writeAnalysisCache(cachePath, cache)

	slices.SortFunc(sessions, func(first SessionStatistics, second SessionStatistics) int {
		return strings.Compare(first.Name, second.Name)
	})

	return sessions, skippedCount, nil
}

func aggregate(sessions []SessionStatistics, prices pricebook) Analysis {
	cacheByProvider := map[string]*CacheStatistics{}
	activityByProvider := map[string]*ActivityStatistics{}
	faultsByProvider := map[string]*FaultStatistics{}
	modelsByName := map[string]*ModelStatistics{}
	toolsByName := map[string]*ToolStatistics{}

	analysis := Analysis{Sessions: sessions}

	for index := range sessions {
		statistics := &sessions[index]
		statistics.Spend, statistics.IsPriced = prices.charge(*statistics)

		if statistics.Cache.Requests > 0 {
			gather(cacheByProvider, statistics.Provider, statistics.Cache, func(into *CacheStatistics) {
				into.Provider = statistics.Provider
			})
		}
		gather(activityByProvider, statistics.Provider, statistics.Activity, func(into *ActivityStatistics) {
			into.Provider = statistics.Provider
		})
		gather(faultsByProvider, statistics.Provider, statistics.Faults, func(into *FaultStatistics) {
			into.Provider = statistics.Provider
		})

		modelStatistics := modelledSession(*statistics)
		gather(modelsByName, priceKey(statistics.Provider, statistics.Model), modelStatistics,
			func(into *ModelStatistics) {
				into.Provider = modelStatistics.Provider
				into.Model = modelStatistics.Model
			})

		for _, toolStatistics := range statistics.Tools {
			gather(toolsByName, toolStatistics.Name, toolStatistics, func(into *ToolStatistics) {
				into.Name = toolStatistics.Name
			})
		}
	}

	analysis.PromptCache.Providers = collect(cacheByProvider, func(first CacheStatistics, second CacheStatistics) int {
		return strings.Compare(first.Provider, second.Provider)
	})
	analysis.Activity.Providers = collect(
		activityByProvider,
		func(first ActivityStatistics, second ActivityStatistics) int {
			return strings.Compare(first.Provider, second.Provider)
		},
	)
	analysis.Faults.Providers = collect(faultsByProvider, func(first FaultStatistics, second FaultStatistics) int {
		return strings.Compare(first.Provider, second.Provider)
	})
	analysis.Models.Models = collect(modelsByName, func(first ModelStatistics, second ModelStatistics) int {
		return strings.Compare(priceKey(first.Provider, first.Model), priceKey(second.Provider, second.Model))
	})
	analysis.Tools.Tools = collect(toolsByName, func(first ToolStatistics, second ToolStatistics) int {
		if first.Calls != second.Calls {
			return second.Calls - first.Calls
		}
		return strings.Compare(first.Name, second.Name)
	})

	for _, statistics := range analysis.PromptCache.Providers {
		analysis.PromptCache.Total.add(statistics)
	}
	for _, statistics := range analysis.Activity.Providers {
		analysis.Activity.Total.add(statistics)
	}
	for _, statistics := range analysis.Faults.Providers {
		analysis.Faults.Total.add(statistics)
	}
	for _, statistics := range analysis.Models.Models {
		analysis.Models.Total.add(statistics)
		if !statistics.IsPriced {
			analysis.Models.UnpricedModels++
		}
	}
	for _, statistics := range analysis.Tools.Tools {
		analysis.Tools.Total.add(statistics)
	}

	return analysis
}

type summable[Statistics any] interface {
	*Statistics
	add(statisticsToAdd Statistics)
}

func gather[Statistics any, Sum summable[Statistics]](
	groups map[string]Sum,
	key string,
	statistics Statistics,
	name func(Sum),
) {
	sum, isKnown := groups[key]
	if !isKnown {
		sum = Sum(new(Statistics))
		name(sum)
		groups[key] = sum
	}

	sum.add(statistics)
}

func collect[Statistics any](
	groups map[string]*Statistics,
	compare func(Statistics, Statistics) int,
) []Statistics {
	collection := make([]Statistics, 0, len(groups))
	for _, statistics := range groups {
		collection = append(collection, *statistics)
	}
	slices.SortFunc(collection, compare)

	return collection
}

func modelledSession(statistics SessionStatistics) ModelStatistics {
	return ModelStatistics{
		Provider:      statistics.Provider,
		Model:         statistics.Model,
		Sessions:      1,
		Requests:      statistics.Cache.Requests,
		InputTokens:   statistics.Cache.InputTokens,
		CachedTokens:  statistics.Cache.CachedTokens,
		WrittenTokens: statistics.Cache.WrittenTokens,
		OutputTokens:  statistics.Cache.OutputTokens,
		Spend:         statistics.Spend,
		IsPriced:      statistics.IsPriced,
	}
}

type pricebook map[string]agent.TokenPrices

func priceKey(providerName string, modelName string) string {
	return providerName + "/" + modelName
}

func readPricebook(path string) pricebook {
	prices := pricebook{}
	if path == "" {
		return prices
	}

	for _, pricedModel := range model.ListedPrices(path) {
		prices[priceKey(pricedModel.Provider, pricedModel.ID)] = pricedModel.Prices
	}

	return prices
}

func (self pricebook) charge(statistics SessionStatistics) (float64, bool) {
	prices, isPriced := self[priceKey(statistics.Provider, statistics.Model)]
	if !isPriced {
		return 0, false
	}

	return metrics.Spend(prices, agent.Usage{
		InputTokens:  int(statistics.Cache.InputTokens),
		OutputTokens: int(statistics.Cache.OutputTokens),
		Cache: &agent.CacheUsage{
			ReadTokens:  int(statistics.Cache.CachedTokens),
			WriteTokens: int(statistics.Cache.WrittenTokens),
		},
	}), true
}

type sessionFiles struct {
	journalSize       int64
	journalModifiedAt int64
}

func openSession(directory string, name string) (sessionFiles, error) {
	sessionRoot, err := os.OpenRoot(session.Dir(directory, name))
	if err != nil {
		return sessionFiles{}, err
	}
	defer func() { _ = sessionRoot.Close() }()

	journalInfo, err := sessionRoot.Stat(journalTranscriptName)
	if err != nil {
		return sessionFiles{}, err
	}

	return sessionFiles{
		journalSize:       journalInfo.Size(),
		journalModifiedAt: journalInfo.ModTime().UnixNano(),
	}, nil
}

func analyseSessionWithCache(
	directory string,
	name string,
	cachedStatistics cachedSession,
	hasCached bool,
) sessionAnalysis {
	files, err := openSession(directory, name)
	if err != nil {
		return sessionAnalysis{err: fmt.Errorf("could not read %s: %w", name, err)}
	}

	if hasCached && cachedStatistics.matches(files) {
		return sessionAnalysis{statistics: cachedStatistics.Statistics, isWhole: cachedStatistics.IsWhole}
	}

	statistics, isWhole, err := analyseJournal(directory, name)
	if err != nil {
		return sessionAnalysis{err: err}
	}

	return cachedAnalysis(statistics, isWhole, files)
}

func (self cachedSession) matches(files sessionFiles) bool {
	return self.JournalSize == files.journalSize &&
		self.JournalModifiedAt == files.journalModifiedAt
}

func cachedAnalysis(statistics SessionStatistics, isWhole bool, files sessionFiles) sessionAnalysis {
	return sessionAnalysis{
		statistics: statistics,
		isWhole:    isWhole,
		cachedStatistics: cachedSession{
			JournalSize:       files.journalSize,
			JournalModifiedAt: files.journalModifiedAt,
			IsWhole:           isWhole,
			Statistics:        statistics,
		},
		hasCached: true,
	}
}

func readAnalysisCache(path string) analysisCache {
	freshCache := analysisCache{Format: analysisCacheFormat, Sessions: map[string]cachedSession{}}
	if path == "" {
		return freshCache
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return freshCache
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return freshCache
	}

	var readCache analysisCache
	if json.Unmarshal(data, &readCache) != nil || readCache.Format != analysisCacheFormat || readCache.Sessions == nil {
		return freshCache
	}
	return readCache
}

func writeAnalysisCache(path string, cache analysisCache) error {
	if path == "" {
		return nil
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	name := filepath.Base(path)
	temporaryName := fmt.Sprintf("%s.%d.tmp", name, os.Getpid())
	if err := root.WriteFile(temporaryName, data, 0o600); err != nil {
		return err
	}
	defer func() { _ = root.Remove(temporaryName) }()
	return root.Rename(temporaryName, name)
}
