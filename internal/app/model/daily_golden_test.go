package model

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"slices"
	"testing"
	"time"

	"crdx.org/oh/internal/app/style"
)

func TestGoldenADailyRefreshKeepsModelsFromAnUnreachableProvider(t *testing.T) {
	for _, test := range []struct {
		name    string
		isPlain bool
	}{
		{name: "daily-partial-refresh.ansi"},
		{name: "daily-partial-refresh-plain.txt", isPlain: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			var output bytes.Buffer
			if test.isPlain {
				restoreStyle := style.Init(&output)
				defer restoreStyle()
			}

			endpoint := serveRegistry(t, oneCodexModel)
			if err := Update(io.Discard, endpoint, modelCachePath(), seenModelsPath(), listingModels(firstListings()), false); err != nil {
				t.Fatal(err)
			}
			ageModelCache(t, time.Now().AddDate(0, 0, -1))
			previous := loadModelCache(modelCachePath()).Providers[OllamaProvider]
			listings := secondListings()
			delete(listings, OllamaProvider)
			lister := listingModels(listings)
			isLoggedIn := func(providerName string) bool { return providerName != OpencodeGoProvider }

			fmt.Fprintln(&output, "=== daily refresh ===")
			choices, err := Ensure(&output, endpoint, modelCachePath(), seenModelsPath(), lister, isLoggedIn)
			if err != nil {
				t.Fatal(err)
			}
			retained := loadModelCache(modelCachePath()).Providers[OllamaProvider]
			if !reflect.DeepEqual(retained, previous) {
				t.Fatalf("the unreachable provider's listing changed: got %+v, want %+v", retained, previous)
			}
			var selections []string
			for _, choice := range choices {
				selections = append(selections, choice.Provider+"/"+choice.ID)
			}
			want := []string{
				"codex/gpt-5.6-sol",
				"anthropic/claude-sonnet-4-6",
				"anthropic/claude-fable-5-1",
				"ollama/llama-4",
			}
			if !slices.Equal(selections, want) {
				t.Fatalf("got choices %v, want %v", selections, want)
			}
			fmt.Fprintln(&output, "\n=== choices returned ===")
			for _, selection := range selections {
				fmt.Fprintln(&output, selection)
			}

			var nextOutput bytes.Buffer
			nextChoices, err := Ensure(&nextOutput, deadAddress, modelCachePath(), seenModelsPath(), unreachableProviders, isLoggedIn)
			if err != nil {
				t.Fatal(err)
			}
			if nextOutput.Len() != 0 || !reflect.DeepEqual(nextChoices, choices) {
				t.Fatalf("the same-day launch refreshed or changed its choices: output %q, choices %+v", nextOutput.String(), nextChoices)
			}
			assertGolden(t, test.name, report(t, output.String()))
		})
	}
}
