package money

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"crdx.org/oh/internal/req"
)

const Endpoint = "https://api.frankfurter.dev/v1/latest"

const (
	cacheVersion    = 1
	fetchTimeout    = 20 * time.Second
	maximumCacheAge = 24 * time.Hour
)

type rateCache struct {
	Version   int                `json:"version"`
	FetchedAt time.Time          `json:"fetched"`
	Base      string             `json:"base"`
	Rates     map[string]float64 `json:"rates"`
}

func loadRateCache(path string) rateCache {
	empty := rateCache{Version: cacheVersion, Base: DollarCode, Rates: map[string]float64{}}

	data, err := os.ReadFile(path) //nolint:gosec // the path is ours
	if err != nil {
		return empty
	}

	var cache rateCache
	if json.Unmarshal(data, &cache) != nil || cache.Version != cacheVersion || cache.Base != DollarCode {
		return empty
	}

	if cache.Rates == nil {
		cache.Rates = map[string]float64{}
	}

	return cache
}

func saveRateCache(path string, cache rateCache) error {
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

func Load(path string, code string) Currency {
	if code == "" || code == DollarCode {
		return Dollar()
	}

	return In(code, loadRateCache(path).Rates[code])
}

func Ensure(ctx context.Context, address string, path string, code string) error {
	if code == "" || code == DollarCode {
		return nil
	}

	cache := loadRateCache(path)
	if isRateCurrent(cache, code, time.Now()) {
		return nil
	}

	rate, err := fetchRate(ctx, address, code)
	if err != nil {
		return err
	}

	cache.Base = DollarCode
	cache.FetchedAt = time.Now()
	cache.Rates[code] = rate

	return saveRateCache(path, cache)
}

func isRateCurrent(cache rateCache, code string, now time.Time) bool {
	if cache.Rates[code] <= 0 || cache.FetchedAt.IsZero() {
		return false
	}

	return now.Sub(cache.FetchedAt) < maximumCacheAge
}

func fetchRate(ctx context.Context, address string, code string) (float64, error) {
	if address == "" {
		address = Endpoint
	}

	query := url.Values{"base": {DollarCode}, "symbols": {code}}

	var payload struct {
		Base  string             `json:"base"`
		Rates map[string]float64 `json:"rates"`
	}

	if err := req.New(fetchTimeout).Get(ctx, address+"?"+query.Encode(), nil, &payload); err != nil {
		return 0, err
	}

	rate := payload.Rates[code]
	if rate <= 0 {
		return 0, fmt.Errorf("no %s rate was quoted against %s", code, DollarCode)
	}

	return rate, nil
}
