package picker

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crdx.org/io/internal/app/menu"
	"crdx.org/io/internal/app/width"
	"crdx.org/io/internal/money"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/agent"
)

var updateGoldens = flag.Bool("update", false, "write what was drawn back to the golden files")

func compareWithGolden(t *testing.T, name string, drawn string) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name)

	if *updateGoldens {
		if err := os.WriteFile(goldenPath, []byte(drawn), 0o600); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(goldenPath) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}

	if drawn != string(want) {
		t.Errorf("rows differ from %s\n--- got ---\n%s--- want ---\n%s", goldenPath, drawn, want)
	}
}

func levels(names ...string) []Effort {
	ladder := make([]Effort, 0, len(names))
	for _, name := range names {
		ladder = append(ladder, Effort{Level: name})
	}

	return ladder
}

func fastLadder(names ...string) []Effort {
	ladder := make([]Effort, 0, len(names)*2)
	for _, name := range names {
		ladder = append(ladder, Effort{Level: name}, Effort{Level: name, IsFast: true})
	}

	return ladder
}

func availableModels() []*Model {
	return []*Model{
		{
			Provider:            "Anthropic",
			ProviderID:          "anthropic",
			Name:                "Sonnet 5",
			ID:                  "claude-sonnet-5",
			EffortLevels:        levels("none", "low", "medium", "high"),
			Effort:              Effort{Level: "medium"},
			ContextWindowTokens: 200000,
			Prices:              &agent.TokenPrices{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75},
		},
		{
			Provider:            "Codex",
			ProviderID:          "codex",
			Name:                "Codex 5.3",
			ID:                  "gpt-5.3-codex",
			EffortLevels:        fastLadder("low", "medium", "high", "xhigh"),
			Effort:              Effort{Level: "medium", IsFast: true},
			ContextWindowTokens: 272000,
			Prices:              &agent.TokenPrices{Input: 1.25, Output: 10, CacheRead: 0.125},
		},
		{
			Provider:            "OpenCode Go",
			ProviderID:          "opencode-go",
			Name:                "DeepSeek Flash Vision Exp 4",
			ID:                  "deepseek-v4-flash-vision-exp",
			EffortLevels:        levels("low", "medium", "high"),
			Effort:              Effort{Level: "high"},
			ContextWindowTokens: 1000000,
			Prices:              &agent.TokenPrices{Input: 0.075, Output: 0.3},
		},
		{
			Provider:            "Anthropic",
			ProviderID:          "anthropic",
			Name:                "Opus 5",
			ID:                  "claude-opus-5",
			EffortLevels:        levels("low", "medium", "high"),
			Effort:              Effort{Level: "high"},
			ContextWindowTokens: 200000,
			Prices:              &agent.TokenPrices{Input: 15, Output: 75, CacheRead: 1.5, CacheWrite: 18.75},
		},
		{
			Provider:            "Codex",
			ProviderID:          "codex",
			Name:                "Sol Pro",
			ID:                  "gpt-5.3-sol-pro",
			EffortLevels:        levels("medium", "high"),
			Effort:              Effort{Level: "medium"},
			ContextWindowTokens: 272000,
			Prices:              &agent.TokenPrices{Input: 150, Output: 600},
		},
		{
			Provider:            "Ollama",
			ProviderID:          "ollama",
			Name:                "Qwen Coder 3 30B",
			ID:                  "qwen3-coder:30b",
			EffortLevels:        levels("medium"),
			Effort:              Effort{Level: "medium"},
			ContextWindowTokens: 0,
		},
	}
}

func TestEveryModelCanBeChosen(t *testing.T) {
	models := &modelList{models: availableModels()}

	for index := range models.Len() {
		if !models.IsChoosable(index) {
			t.Errorf("expected model %d to be available, got otherwise", index)
		}
	}
}

func TestTheEffortOfAModelIsSetOneStepAtATime(t *testing.T) {
	models := availableModels()
	list := &modelList{models: models}

	list.Adjust(1, 1)
	if models[1].Effort != (Effort{Level: "high"}) {
		t.Errorf("expected the step above a fast effort to be the next level, got %s", models[1].Effort)
	}

	list.Adjust(1, 1)
	if models[1].Effort != (Effort{Level: "high", IsFast: true}) {
		t.Errorf("expected fast mode to sit beside each level, got %s", models[1].Effort)
	}

	for range 9 {
		list.Adjust(1, 1)
	}
	if models[1].Effort != (Effort{Level: "xhigh", IsFast: true}) {
		t.Errorf("expected the effort to stop at the highest, got %s", models[1].Effort)
	}

	for range 9 {
		list.Adjust(1, -1)
	}
	if models[1].Effort != (Effort{Level: "low"}) {
		t.Errorf("expected the effort to stop at the lowest, got %s", models[1].Effort)
	}

	if models[0].Effort != (Effort{Level: "medium"}) {
		t.Errorf("expected the other models to keep their effort, got %s", models[0].Effort)
	}
}

func TestAnEffortTheModelDoesNotOfferIsLeftAlone(t *testing.T) {
	models := []*Model{{EffortLevels: levels("low", "high"), Effort: Effort{Level: "medium"}}}
	(&modelList{models: models}).Adjust(0, 1)

	if models[0].Effort != (Effort{Level: "medium"}) {
		t.Errorf("expected the effort to be left alone, got %s", models[0].Effort)
	}
}

func TestAFastEffortIsWrittenWithTheFastMark(t *testing.T) {
	if got := (Effort{Level: "high"}).String(); got != "high" {
		t.Errorf("got %q", got)
	}

	if got := (Effort{Level: "none", IsFast: true}).String(); got != "none ⚡" {
		t.Errorf("got %q", got)
	}
}

func TestAContextWindowIsWrittenTheWayEveryTokenCountIs(t *testing.T) {
	cases := map[int]string{
		0:       "—",
		512:     "1K",
		64000:   "64K",
		272000:  "272K",
		1048576: "1M",
	}

	for count, want := range cases {
		if got := contextWindow(count); got != want {
			t.Errorf("contextWindow(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestGoldenTheRowsOfTheModelPickerMatchTheGolden(t *testing.T) {
	models := &modelList{models: availableModels()}

	var output strings.Builder

	for i, room := range []int{150, 100, 80, 46} {
		if i > 0 {
			_, _ = fmt.Fprintln(&output)
		}
		_, _ = fmt.Fprintf(&output, "--- %d columns ---\n", room)
		_, _ = fmt.Fprintln(&output, models.ColumnHeader(room))
		for index, model := range models.models {
			_, _ = fmt.Fprintln(&output, modelRow(model, models.currency, index == 1, room))
		}
	}

	_, _ = fmt.Fprintf(&output, "\n--- %d columns in pounds ---\n", 150)
	pounds := money.In("GBP", 0.8)
	for index, model := range models.models {
		_, _ = fmt.Fprintln(&output, modelRow(model, pounds, index == 1, 150))
	}

	compareWithGolden(t, "rows.golden", output.String())
}

func TestGoldenWhatTheModelPickerPaintsMatchesTheGolden(t *testing.T) {
	frames := []struct {
		name   string
		room   int
		height int
		cursor int
		query  string
	}{
		{name: "a wide terminal, with the columns kept to the left", room: 150, height: 24, cursor: 0},
		{name: "a terminal the columns fill exactly", room: 80, height: 24, cursor: 1},
		{name: "no room for every row, so the list is scrolled to the cursor", room: 80, height: 3, cursor: 2},
		{name: "a narrow terminal, with the columns clipped", room: 46, height: 24, cursor: 0},
		{name: "a filter narrowing the list to one provider", room: 80, height: 24, cursor: 0, query: "opencode"},
		{name: "a filter no model answers to", room: 80, height: 24, cursor: 0, query: "gemini"},
	}

	var output strings.Builder

	for _, frame := range frames {
		fmt.Fprintf(&output, "=== %s ===\n%s\n", frame.name, strutil.VisibleEscapes(
			menu.Paint(
				&modelList{models: availableModels()},
				frame.room,
				frame.height,
				frame.cursor,
				frame.query,
			),
		))
	}

	compareWithGolden(t, "painted.ansi", output.String())
}

func TestTheCostColumnFitsTheLongestTierItCanDraw(t *testing.T) {
	for _, priceTier := range agent.PriceTiers() {
		if got := width.Of(priceTier.String()); got > costColumn {
			t.Errorf("%q needs %d cells, but the column holds %d", priceTier, got, costColumn)
		}
	}
}
