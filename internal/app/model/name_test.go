package model

import (
	"strings"
	"testing"
	"unicode"
)

func TestEveryModelIsWrittenForAPerson(t *testing.T) {
	cases := map[string]string{
		"gpt-5.3-codex":                "Codex 5.3",
		"gpt-5.3-codex-spark":          "Codex Spark 5.3",
		"gpt-5.4-mini":                 "GPT Mini 5.4",
		"gpt-5.4-nano":                 "GPT Nano 5.4",
		"gpt-5.5-pro":                  "GPT Pro 5.5",
		"gpt-5.6":                      "GPT 5.6",
		"gpt-5.6-luna":                 "GPT Luna 5.6",
		"gpt-5.6-sol":                  "GPT Sol 5.6",
		"gpt-5.6-terra":                "GPT Terra 5.6",
		"o3":                           "o3",
		"o3-pro":                       "o3 Pro",
		"o4-mini":                      "o4 Mini",
		"kimi-k3":                      "Kimi K3",
		"kimi-k2.7-code":               "Kimi Code K2.7",
		"longcat-2.0":                  "LongCat 2.0",
		"mimo-v2-omni":                 "Mimo Omni 2",
		"mimo-v2.5-pro":                "Mimo Pro 2.5",
		"mimo-v2.5":                    "Mimo 2.5",
		"glm-5.3-flash":                "GLM Flash 5.3",
		"glm-5.3":                      "GLM 5.3",
		"deepseek-v4-pro":              "DeepSeek Pro 4",
		"deepseek-v4-flash":            "DeepSeek Flash 4",
		"deepseek-v4-flash-vision-exp": "DeepSeek Flash Vision Exp 4",
		"hy3":                          "HY3",
		"claude-opus-5":                "Opus 5",
		"claude-sonnet-5":              "Sonnet 5",
		"claude-fable-5":               "Fable 5",
		"claude-fable-5-1":             "Fable 5.1",
		"claude-mythos-5-1":            "Claude Mythos 5.1",
		"claude-opus-4-6":              "Opus 4.6",
		"claude-sonnet-4-5-20250929":   "Sonnet 4.5",
		"claude-3-7-sonnet-20250219":   "Sonnet 3.7",

		"gemma4:12b":                 "Gemma 4 12B",
		"gemma4:31b":                 "Gemma 4 31B",
		"glm-4.7-flash:latest":       "GLM Flash 4.7",
		"glm-ocr:latest":             "GLM OCR",
		"gpt-oss:120b":               "GPT OSS 120B",
		"granite4.1:30b":             "Granite 4.1 30B",
		"laguna-xs-2.1:q8_0":         "Laguna XS 2.1",
		"muse-glimmer-128k:latest":   "Muse Glimmer 128K",
		"muse-glimmer:latest":        "Muse Glimmer",
		"nemotron-cascade-2:30b":     "Nemotron Cascade 2 30B",
		"qwen2.5-coder:32b":          "Qwen Coder 2.5 32B",
		"qwen3.6:35b-a3b-q8_0":       "Qwen 3.6 35B",
		"qwen3.6:latest":             "Qwen 3.6",
		"qwen3.8:27b-mtp-q8_0":       "Qwen 3.8 27B",
		"qwen3.8:27b-mtp-q8_0-256k":  "Qwen 3.8 27B 256K",
		"qwen3:30b-a3b":              "Qwen 3 30B",
		"qwen3:32b":                  "Qwen 3 32B",
		"qwen3-coder:30b-a3b-q4_K_M": "Qwen Coder 3 30B",
	}

	for id, want := range cases {
		if got := strings.Join(DisplayName(id), " "); got != want {
			t.Errorf("%s is written %q, want %q", id, got, want)
		}
	}
}

func TestAModelKnownByItsOwnNameLosesTheFamilyInFrontOfIt(t *testing.T) {
	for id, want := range map[string]string{
		"gpt-5.7-codex":   "Codex 5.7",
		"claude-opus-6":   "Opus 6",
		"claude-sonnet-6": "Sonnet 6",
		"gpt-5.7-luna":    "GPT Luna 5.7",
		"glm-6-flash":     "GLM Flash 6",
	} {
		if got := strings.Join(DisplayName(id), " "); got != want {
			t.Errorf("%s is written %q, want %q", id, got, want)
		}
	}
}

func TestTheProviderInFrontOfAModelIsNotPartOfItsName(t *testing.T) {
	if got := strings.Join(DisplayName("ollama/qwen3:32b"), " "); got != "Qwen 3 32B" {
		t.Errorf("expected the provider to be left out, got %q", got)
	}
}

func TestANameNothingCanBeReadOutOfIsLeftAsItIs(t *testing.T) {
	for _, id := range []string{"7", "1.5", "v2"} {
		if got := strings.Join(DisplayName(id), " "); got != id {
			t.Errorf("%q is written %q, want it left alone", id, got)
		}
	}
}

func FuzzDisplayNameReadsAnyIdentifier(f *testing.F) {
	for _, id := range []string{
		"gpt-5.3-codex",
		"ollama/qwen3.8:27b-mtp-q8_0-256k",
		"muse-glimmer-128k:latest",
		"o3",
		"",
		":",
		"/",
		"-",
		"1.2.3.4-5",
		"日本語-2:70b",
	} {
		f.Add(id)
	}

	f.Fuzz(func(t *testing.T, id string) {
		name := DisplayName(id)

		if len(name) == 0 || len(name) > 2 {
			t.Fatalf("%q is written in %d parts, want one or two", id, len(name))
		}

		for _, part := range name {
			if strings.ContainsFunc(part, unicode.IsControl) {
				t.Errorf("%q is written %q, which carries a control character", id, part)
			}
			if strings.TrimSpace(part) != part {
				t.Errorf("%q is written %q, which is padded with space", id, part)
			}
		}

		if len(name) == 2 && name[1] == "" {
			t.Errorf("%q is written with an empty second part", id)
		}

		if plainly(id) != "" && name[0] == "" {
			t.Errorf("%q is written with no name at all", id)
		}
	})
}

func TestEveryProviderIsWrittenForAPerson(t *testing.T) {
	cases := map[string]string{
		AnthropicProvider:  "Anthropic",
		CodexProvider:      "Codex",
		OllamaProvider:     "Ollama",
		OpencodeGoProvider: "OpenCode Go",
		"whoever":          "Whoever",
	}

	for id, want := range cases {
		if got := ProviderName(id); got != want {
			t.Errorf("%s is written %q, want %q", id, got, want)
		}
	}
}

func TestEveryProviderThereIsHasAName(t *testing.T) {
	for _, id := range ProviderNames() {
		if _, isFound := providerNames[id]; !isFound {
			t.Errorf("%s is not written for a person anywhere", id)
		}
	}
}
