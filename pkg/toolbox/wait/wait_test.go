package wait_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/wait"
)

type fakeSource struct {
	kind     string
	overs    map[string]chan struct{}
	holds    map[string]int
	released map[string]int
}

func newSource(kind string, names ...string) *fakeSource {
	source := &fakeSource{
		kind:     kind,
		overs:    map[string]chan struct{}{},
		holds:    map[string]int{},
		released: map[string]int{},
	}
	for _, name := range names {
		source.overs[name] = make(chan struct{})
	}
	return source
}

func (self *fakeSource) Kind() string { return self.kind }

func (self *fakeSource) Mention(name string) string { return self.kind + "/" + name }

func (self *fakeSource) Knows(name string) bool {
	_, isKnown := self.overs[name]
	return isKnown
}

func (self *fakeSource) Hold(name string) (<-chan struct{}, func(), error) {
	over, isKnown := self.overs[name]
	if !isKnown {
		return nil, nil, errors.New("gone")
	}
	self.holds[name]++
	return over, func() { self.released[name]++ }, nil
}

func (self *fakeSource) Ended(name string) string { return self.kind + " " + name + ": ended" }

func (self *fakeSource) Running(name string) string { return self.kind + " " + name + ": running" }

func (self *fakeSource) end(name string) { close(self.overs[name]) }

func build(arrival <-chan struct{}, sources ...wait.Source) tool.Tool {
	return wait.New(sources, func(context.Context) <-chan struct{} { return arrival })
}

func parse(t *testing.T, built tool.Tool, args wait.Args) tool.ToolCall {
	t.Helper()

	arguments, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := built.Parse(string(arguments))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func run(t *testing.T, built tool.Tool, args wait.Args) (string, error) {
	t.Helper()

	result, err := parse(t, built, args).Exec(t.Context())
	return result.Output, err
}

func TestWaitingForAnyReturnsOnceOneHasEnded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		jobs := newSource("job", "build", "lint")
		go jobs.end("lint")

		output, err := run(t, build(nil, jobs), wait.Args{Names: []string{"build", "lint"}})
		if err != nil {
			t.Fatal(err)
		}
		if output != "job lint: ended" {
			t.Errorf("got %q, want only the job that ended", output)
		}
		if jobs.holds["build"] != 1 || jobs.released["build"] != 1 || jobs.released["lint"] != 1 {
			t.Errorf("held %v and released %v, want every hold released", jobs.holds, jobs.released)
		}
	})
}

func TestWaitingForAllReturnsEveryReportAcrossKinds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		jobs := newSource("job", "build")
		children := newSource("subagent", "tame-adder")
		go func() {
			time.Sleep(time.Second)
			children.end("tame-adder")
			time.Sleep(time.Second)
			jobs.end("build")
		}()

		output, err := run(t, build(nil, jobs, children), wait.Args{Names: []string{"build", "tame-adder"}, Until: wait.All})
		if err != nil {
			t.Fatal(err)
		}
		if want := "job build: ended\n\nsubagent tame-adder: ended"; output != want {
			t.Errorf("got %q, want %q", output, want)
		}
	})
}

func TestAWaitThatGivesUpSaysWhatIsStillRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		jobs := newSource("job", "build", "lint")
		jobs.end("lint")

		output, err := run(t, build(nil, jobs), wait.Args{Names: []string{"build", "lint"}, Until: wait.All, Seconds: 20})
		if err != nil {
			t.Fatal(err)
		}
		want := "job build: running\n\njob lint: ended\n\nnote: the wait gave up after 20s, and build is still running."
		if output != want {
			t.Errorf("got %q, want %q", output, want)
		}
	})
}

func TestAMessageFromTheUserEndsTheWaitEarly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		jobs := newSource("job", "build")
		arrival := make(chan struct{})
		go func() {
			time.Sleep(time.Second)
			close(arrival)
		}()

		output, err := run(t, build(arrival, jobs), wait.Args{Names: []string{"build"}})
		if err != nil {
			t.Fatal(err)
		}
		want := "job build: running\n\nnote: the user sent a message, so the wait ended early, and build is still running."
		if output != want {
			t.Errorf("got %q, want %q", output, want)
		}
	})
}

func TestAWaitEndsWithItsTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		jobs := newSource("job", "build")
		ctx, cancel := context.WithCancel(t.Context())
		go func() {
			time.Sleep(time.Second)
			cancel()
		}()

		if _, err := parse(t, build(nil, jobs), wait.Args{Names: []string{"build"}}).Exec(ctx); err == nil {
			t.Error("a wait outlived its turn")
		}
		if jobs.released["build"] != 1 {
			t.Error("a stopped wait kept its hold")
		}
	})
}

func TestANameIsFoundByKindOrRefusedWhenItIsAmbiguousOrUnknown(t *testing.T) {
	jobs := newSource("job", "docs")
	children := newSource("subagent", "docs", "tame-adder")
	jobs.end("docs")
	children.end("docs")
	children.end("tame-adder")
	built := build(nil, jobs, children)

	for names, want := range map[string]string{
		"tame-adder":    "subagent tame-adder: ended",
		"job:docs":      "job docs: ended",
		"subagent:docs": "subagent docs: ended",
	} {
		if output, err := run(t, built, wait.Args{Names: []string{names}}); err != nil || output != want {
			t.Errorf("%s gave %q, %v, want %q", names, output, err, want)
		}
	}
	for name, want := range map[string]string{
		"docs":      "docs names more than one thing; name it as job:docs or subagent:docs",
		"ghost":     "nothing is named ghost",
		"job:ghost": "there is no job named ghost",
	} {
		if _, err := run(t, built, wait.Args{Names: []string{name}}); err == nil || err.Error() != want {
			t.Errorf("%s gave %v, want %q", name, err, want)
		}
	}
}

func TestAWaitIsRefusedWhenItsArgumentsMakeNoSense(t *testing.T) {
	built := build(nil, newSource("job", "build"))
	for _, arguments := range []string{
		`{"names":[]}`,
		`{"names":[" "]}`,
		`{"names":["build","build"]}`,
		`{"names":["build"],"until":"most"}`,
		`{"names":["build"],"seconds":-1}`,
	} {
		if _, err := built.Parse(arguments); err == nil {
			t.Errorf("accepted %s", arguments)
		}
	}
}

func TestAWaitDrawsWhatItWaitsForAndHowLong(t *testing.T) {
	jobs := newSource("job")
	children := newSource("subagent")
	built := build(nil, jobs, children)
	for _, shape := range []struct {
		args      wait.Args
		subject   string
		qualifier string
		limit     time.Duration
		mentions  []string
	}{
		{wait.Args{Names: []string{"build", "lint"}}, "build || lint", "", wait.Limit, []string{"job/build", "subagent/build", "job/lint", "subagent/lint"}},
		{wait.Args{Names: []string{"job:build", "subagent:tame-adder"}, Until: wait.All, Seconds: 20}, "job:build && subagent:tame-adder", "for up to 20s", 20 * time.Second, []string{"job/build", "subagent/tame-adder"}},
		{wait.Args{Names: []string{"build"}, Seconds: 100000}, "build", "for up to 10m", wait.Limit, []string{"job/build", "subagent/build"}},
	} {
		parsed := parse(t, built, shape.args)
		rendering := parsed.Rendering()
		if rendering.Subject != shape.subject || rendering.Qualifier != shape.qualifier {
			t.Errorf("%+v drew %+v", shape.args, rendering)
		}
		if !slices.Equal(rendering.Mentions, shape.mentions) {
			t.Errorf("%+v mentioned %v, want %v", shape.args, rendering.Mentions, shape.mentions)
		}
		if parsed.TimeLimit() != shape.limit {
			t.Errorf("%+v was limited to %s, want %s", shape.args, parsed.TimeLimit(), shape.limit)
		}
	}
}

func TestTheDescriptionNamesEveryKindAndOnlyOffersAPrefixWhenNamesCanClash(t *testing.T) {
	alone := build(nil, newSource("job")).Description()
	if !strings.Contains(alone, "named jobs") || strings.Contains(alone, "prefix") {
		t.Errorf("a wait over jobs alone described itself as %q", alone)
	}
	both := build(nil, newSource("job"), newSource("subagent")).Description()
	if !strings.Contains(both, "named jobs and subagents") || !strings.Contains(both, "job:<name>") {
		t.Errorf("a wait over both described itself as %q", both)
	}
}
