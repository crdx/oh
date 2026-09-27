package contextsource

import "testing"

func TestContextSourcesEstimateTokensFromBytes(t *testing.T) {
	file := FileFromBytes("/config/SYSTEM.md", 2_800)
	if file.DisplayName() != "/config/SYSTEM.md" || file.EstimatedTokens != 1_001 {
		t.Errorf("got file source %+v", file)
	}

	named := NamedFromBytes("harness", 1_400)
	if named.DisplayName() != "harness" || named.EstimatedTokens != 501 {
		t.Errorf("got named source %+v", named)
	}
}
