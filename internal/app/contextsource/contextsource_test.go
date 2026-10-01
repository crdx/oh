package contextsource

import "testing"

func TestContextSourcesEstimateTokensFromBytes(t *testing.T) {
	file := FileFromBytes("/config/SYSTEM.md", 2_800)
	if file.DisplayName() != "/config/SYSTEM.md" || file.EstimatedTokens != 1_001 {
		t.Errorf("got file source %+v", file)
	}

	harness := KindFromBytes(HarnessInstructions, 1_400)
	if harness.DisplayName() != "harness instructions" || harness.EstimatedTokens != 501 {
		t.Errorf("got harness source %+v", harness)
	}
}

func TestCountedSourcesAreNamedWhenDrawn(t *testing.T) {
	for _, test := range []struct {
		source Source
		want   string
	}{
		{source: Source{Kind: SkillDefinitions, Count: 1}, want: "1 skill definition"},
		{source: Source{Kind: SkillDefinitions, Count: 26}, want: "26 skill definitions"},
		{source: Source{Kind: ToolDefinitions, Count: 1}, want: "1 tool definition"},
		{source: Source{Kind: ToolDefinitions, Count: 14}, want: "14 tool definitions"},
	} {
		if got := test.source.DisplayName(); got != test.want {
			t.Errorf("got %q, want %q", got, test.want)
		}
	}
}
