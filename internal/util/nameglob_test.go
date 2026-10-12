package util

import "testing"

func TestDenyEntriesDistinguishAbsolutePathsFromNamePatterns(t *testing.T) {
	for _, entry := range []string{"auth.json", "*.env", "/home/example/auth.json"} {
		if err := ValidateDenyEntry(entry); err != nil {
			t.Errorf("valid entry %q: %v", entry, err)
		}
	}
	for _, entry := range []string{"", "relative/auth.json", "broken["} {
		if err := ValidateDenyEntry(entry); err == nil {
			t.Errorf("invalid entry %q was accepted", entry)
		}
	}
	if patterns := DenyNamePatterns([]string{"auth.json", "/home/example/auth.json", "*.env"}); len(patterns) != 2 || patterns[0] != "auth.json" || patterns[1] != "*.env" {
		t.Errorf("got name patterns %v", patterns)
	}
}

func TestNameGlobsMatchEveryPathSegment(t *testing.T) {
	for _, name := range []string{
		"foo.txt",
		"/workspace/foo.txt",
		"/workspace/foo.txt/inside",
	} {
		if !MatchNameGlobs([]string{"foo.*"}, name) {
			t.Errorf("%q did not match", name)
		}
	}
	if MatchNameGlobs([]string{"foo.*"}, "/workspace/food.txt") {
		t.Error("a different name matched")
	}
}

func TestNameGlobsRejectPathsAndMalformedPatterns(t *testing.T) {
	for _, pattern := range []string{"", "directory/name", `directory\\name`, "broken["} {
		if err := ValidateNameGlob(pattern); err == nil {
			t.Errorf("%q was accepted", pattern)
		}
	}
}
