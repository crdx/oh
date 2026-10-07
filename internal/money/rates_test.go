package money

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func quotedRate(t *testing.T, rate float64) (string, *int) {
	t.Helper()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Query().Get("base") != DollarCode {
			t.Errorf("asked for base %q", request.URL.Query().Get("base"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"base":"USD","rates":{"GBP":` + formatFloat(rate) + `}}`))
	}))
	t.Cleanup(server.Close)

	return server.URL, &requests
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func TestAFetchedRateIsCachedAndReused(t *testing.T) {
	address, requests := quotedRate(t, 0.78)
	path := filepath.Join(t.TempDir(), "rates.json")

	if _, err := Ensure(t.Context(), address, path, "GBP"); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(t.Context(), address, path, "GBP"); err != nil {
		t.Fatal(err)
	}

	if *requests != 1 {
		t.Errorf("expected one request, got %d", *requests)
	}

	pounds := Load(path, "GBP")
	if pounds.Rate != 0.78 || pounds.Symbol != "£" {
		t.Errorf("got %+v", pounds)
	}
}

func TestAStaleRateIsFetchedAgain(t *testing.T) {
	address, requests := quotedRate(t, 0.78)
	path := filepath.Join(t.TempDir(), "rates.json")

	if _, err := Ensure(t.Context(), address, path, "GBP"); err != nil {
		t.Fatal(err)
	}

	stale := loadRateCache(path)
	stale.FetchedAt = time.Now().Add(-maximumCacheAge - time.Minute)
	if err := saveRateCache(path, stale); err != nil {
		t.Fatal(err)
	}

	if _, err := Ensure(t.Context(), address, path, "GBP"); err != nil {
		t.Fatal(err)
	}
	if *requests != 2 {
		t.Errorf("expected the stale rate to be asked for again, got %d requests", *requests)
	}
}

func TestDollarsAreNeverFetched(t *testing.T) {
	address, requests := quotedRate(t, 0.78)
	path := filepath.Join(t.TempDir(), "rates.json")

	for _, code := range []string{"", DollarCode} {
		if _, err := Ensure(t.Context(), address, path, code); err != nil {
			t.Fatal(err)
		}
	}

	if *requests != 0 {
		t.Errorf("expected no request, got %d", *requests)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("expected no cache to be written")
	}
}

func TestAnUnquotedCurrencyIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"base":"USD","rates":{}}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "rates.json")

	if _, err := Ensure(t.Context(), server.URL, path, "GBP"); err == nil {
		t.Fatal("expected an error when nothing was quoted")
	}

	if !Load(path, "GBP").IsDollar() {
		t.Error("expected an unquoted currency to stay in dollars")
	}
}

func ensuredCurrency(t *testing.T, address string, path string, code string) (Currency, error) {
	t.Helper()

	currency, err := Ensure(t.Context(), address, path, code)
	if loaded := Load(path, code); currency != loaded {
		t.Errorf("for %q ensuring handed back %+v while the cache now holds %+v", code, currency, loaded)
	}

	return currency, err
}

func TestTheCurrencyInUseFollowsWhatTheCacheHoldsAfterEnsuring(t *testing.T) {
	const unreachable = "http://127.0.0.1:1"
	path := filepath.Join(t.TempDir(), "rates.json")

	if pounds, err := ensuredCurrency(t, unreachable, path, "GBP"); err == nil || !pounds.IsDollar() {
		t.Errorf("with nothing cached and nothing reachable got %+v and %v, want dollars and an error", pounds, err)
	}

	address, _ := quotedRate(t, 0.78)
	if pounds, err := ensuredCurrency(t, address, path, "GBP"); err != nil || pounds.Rate != 0.78 {
		t.Errorf("after fetching got %+v and %v, want 0.78", pounds, err)
	}

	if pounds, err := ensuredCurrency(t, unreachable, path, "GBP"); err != nil || pounds.Rate != 0.78 {
		t.Errorf("with a current cache got %+v and %v, want the cached 0.78 without asking", pounds, err)
	}

	stale := loadRateCache(path)
	stale.FetchedAt = time.Now().Add(-maximumCacheAge - time.Minute)
	if err := saveRateCache(path, stale); err != nil {
		t.Fatal(err)
	}
	if pounds, err := ensuredCurrency(t, unreachable, path, "GBP"); err == nil || pounds.Rate != 0.78 {
		t.Errorf("with a stale cache and nothing reachable got %+v and %v, want the old 0.78 and an error", pounds, err)
	}

	refreshed, _ := quotedRate(t, 0.81)
	if pounds, err := ensuredCurrency(t, refreshed, path, "GBP"); err != nil || pounds.Rate != 0.81 {
		t.Errorf("after refreshing a stale cache got %+v and %v, want 0.81", pounds, err)
	}

	for _, code := range []string{"", DollarCode} {
		if currency, err := ensuredCurrency(t, unreachable, path, code); err != nil || !currency.IsDollar() {
			t.Errorf("for %q got %+v and %v, want dollars", code, currency, err)
		}
	}
}
