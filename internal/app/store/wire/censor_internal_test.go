package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

func FuzzTheBearerCheckCensorsWhatThePatternAloneWould(f *testing.F) {
	for _, seed := range []string{
		"",
		"Bearer secret",
		"bEaReR\tsecret",
		"no token here",
		"bearer",
		"xbearer  y",
		"BEARE",
		"Ｂearer secret",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, value string) {
		expected := bearerPattern.ReplaceAllString(value, "Bearer "+redacted)
		if censored := censorBearer(value); censored != expected {
			t.Errorf("censorBearer(%q) = %q, want %q", value, censored, expected)
		}
	})
}

func TestTheBearerCheckFindsTheWordInAnyCase(t *testing.T) {
	for _, value := range []string{"Bearer x", "bearer x", "BEARER x", "say bEaReR"} {
		if !hasBearerWord(value) {
			t.Errorf("expected %q to hold the word", value)
		}
	}

	for _, value := range []string{"", "beare", "b e a r e r", "Ｂearer"} {
		if hasBearerWord(value) {
			t.Errorf("expected %q not to hold the word", value)
		}
	}
}

func BenchmarkCensorALongConversation(b *testing.B) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	messages := make([]message, 0, 400)
	for i := range 400 {
		messages = append(messages, message{
			Role:    []string{"user", "assistant"}[i%2],
			Content: strings.Repeat("the quick brown fox jumps over the lazy dog ", 40),
		})
	}

	body, err := json.Marshal(map[string]any{"model": "model", "messages": messages})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		censorJSON(body)
	}
}
