package contextfile

import "testing"

func TestAContextFileEstimatesItsTokensFromBytes(t *testing.T) {
	file := FromBytes("/config/SYSTEM.md", 2_800)

	if file.Path != "/config/SYSTEM.md" || file.EstimatedTokens != 1_001 {
		t.Errorf("got %+v", file)
	}
}
