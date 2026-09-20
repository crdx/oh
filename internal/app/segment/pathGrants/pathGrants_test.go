package pathGrants

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"crdx.org/io/internal/app/link"
	"crdx.org/io/internal/app/pathgrant"
	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/width"
)

type testOptions struct {
	pathType string
}

func (self testOptions) Read(into any) error {
	args, ok := into.(*struct {
		Type string `toml:"type"`
	})
	if !ok {
		return errors.New("unexpected options type")
	}
	args.Type = self.pathType
	return nil
}

func buildSegment(t *testing.T, grants *[]pathgrant.Grant, pathType string) segment.Segment {
	t.Helper()

	built, err := New(func() []pathgrant.Grant { return *grants })(testOptions{pathType: pathType})
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func TestAnEmptyGrantListDrawsNothing(t *testing.T) {
	grants := []pathgrant.Grant{}
	if got := buildSegment(t, &grants, "").Render(segment.Context{}); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestTheSegmentReadsTheCurrentGrantListEveryTime(t *testing.T) {
	grants := []pathgrant.Grant{{Path: "/reference", Access: pathgrant.ReadAccess}}
	built := buildSegment(t, &grants, "")
	if got := style.Plain(built.Render(segment.Context{})); got != "r:reference" {
		t.Errorf("got %q", got)
	}

	grants = []pathgrant.Grant{{Path: "/output", Access: pathgrant.ReadAccess | pathgrant.WriteAccess}}
	if got := style.Plain(built.Render(segment.Context{})); got != "rw:output" {
		t.Errorf("got %q", got)
	}

	grants = []pathgrant.Grant{{Path: "/tools", Access: pathgrant.ReadAccess | pathgrant.ExecAccess}}
	if got := style.Plain(built.Render(segment.Context{})); got != "rx:tools" {
		t.Errorf("got %q", got)
	}
}

func TestTheShellLetterFollowsWhatEachGrantMayChange(t *testing.T) {
	for _, test := range []struct {
		access pathgrant.Access
		paint  style.Style
	}{
		{pathgrant.ReadAccess | pathgrant.ExecAccess, style.Read},
		{pathgrant.ReadAccess | pathgrant.ExecAccess | pathgrant.WriteAccess, style.Write},
	} {
		grants := []pathgrant.Grant{{Path: "/tools", Access: test.access}}
		got := buildSegment(t, &grants, "").Render(segment.Context{})
		if !strings.Contains(got, test.paint("x")) {
			t.Errorf(
				"access %q drew %q, want the shell letter painted %q",
				test.access.Flags(),
				got,
				test.paint("x"),
			)
		}
	}
}

func TestEachGrantLinksToTheWholePathItNames(t *testing.T) {
	grants := []pathgrant.Grant{{Path: "/one/reference", Access: pathgrant.ReadAccess}}
	got := buildSegment(t, &grants, "").Render(segment.Context{})
	if want := link.RenderPath(style.Normal("reference"), "/one/reference"); !strings.Contains(got, want) {
		t.Errorf("got %q, want the shortened name linked as %q", got, want)
	}
}

func TestDuplicateBasenamesExpandToDistinguishingPaths(t *testing.T) {
	grants := []pathgrant.Grant{
		{Path: "/one/reference", Access: pathgrant.ReadAccess},
		{Path: "/two/reference", Access: pathgrant.ReadAccess | pathgrant.WriteAccess},
	}
	got := style.Plain(buildSegment(t, &grants, "").Render(segment.Context{}))
	for _, want := range []string{"r:/one/reference", "w:/two/reference"} {
		if !strings.Contains(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestTheConfiguredPathTypeIsApplied(t *testing.T) {
	grants := []pathgrant.Grant{{Path: "/one/reference", Access: pathgrant.ReadAccess}}
	for pathType, want := range map[string]string{
		"base":  "r:reference",
		"short": "r:/one/reference",
		"full":  "r:/one/reference",
	} {
		if got := style.Plain(buildSegment(t, &grants, pathType).Render(segment.Context{})); got != want {
			t.Errorf("%s got %q, want %q", pathType, got, want)
		}
	}
}

func TestManyGrantsShowAsManyAsFitAndThenTheirHiddenCount(t *testing.T) {
	grants := make([]pathgrant.Grant, 50)
	for i := range grants {
		grants[i] = pathgrant.Grant{
			Path:   fmt.Sprintf("/path-%02d", i+1),
			Access: pathgrant.ReadAccess,
		}
	}
	built := buildSegment(t, &grants, "")
	fitter, ok := built.(segment.Fitter)
	if !ok {
		t.Fatal("path grants segment does not fit itself to available room")
	}

	got := style.Plain(fitter.RenderWithin(segment.Context{}, 36))
	if width.Of(got) > 36 || !strings.Contains(got, "r:path-01") || !strings.Contains(got, "+47") {
		t.Errorf("got %q at width %d", got, width.Of(got))
	}
	if strings.Contains(got, "path-50") {
		t.Errorf("hidden path leaked into %q", got)
	}
	if got := style.Plain(fitter.RenderWithin(segment.Context{}, 3)); got != "+50" {
		t.Errorf("got count-only rendering %q", got)
	}
}

func TestAnUnknownPathTypeIsRefused(t *testing.T) {
	grants := []pathgrant.Grant{}
	if _, err := New(func() []pathgrant.Grant { return grants })(testOptions{pathType: "unknown"}); err == nil {
		t.Error("expected unknown path type to fail")
	}
}
