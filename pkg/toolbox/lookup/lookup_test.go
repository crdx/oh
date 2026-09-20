package lookup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type searchStub struct {
	query   string
	queries int
	output  string
	err     error
}

func (self *searchStub) Search(_ context.Context, query string) (string, error) {
	self.query = query
	self.queries++

	return self.output, self.err
}

func allowLookup(context.Context, string) error { return nil }

func TestLookupDelegatesToTheConfiguredSearcher(t *testing.T) {
	searcher := &searchStub{output: "cited answer"}
	call, err := New(func() bool { return true }, allowLookup, searcher).Parse(`{"query":"current weather"}`)
	if err != nil {
		t.Fatal(err)
	}

	result, err := call.Exec(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if searcher.query != "current weather" || result.Output != "cited answer" {
		t.Errorf("got query %q and output %q", searcher.query, result.Output)
	}
}

func TestLookupReportsTheSearchersFailure(t *testing.T) {
	failure := errors.New("search failed")
	call, err := New(func() bool { return true }, allowLookup, &searchStub{err: failure}).Parse(`{"query":"weather"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, failure) {
		t.Errorf("got %v", err)
	}
}

func TestLookupRejectsAnEmptyQuery(t *testing.T) {
	if _, err := New(func() bool { return true }, allowLookup, &searchStub{}).Parse(`{"query":"  "}`); err == nil {
		t.Error("expected an empty query to be rejected")
	}
}

func TestLookupIsAReadOnlyConcurrentTool(t *testing.T) {
	offeredTool := New(func() bool { return true }, allowLookup, &searchStub{})

	if offeredTool.Name() != "lookup" {
		t.Errorf("got name %q", offeredTool.Name())
	}
	if !offeredTool.Concurrent() {
		t.Error("expected the tool to be concurrent")
	}
	if !offeredTool.ReadOnly() {
		t.Error("expected the tool to be read-only")
	}
}

func TestLookupIsRefusedWithoutLookupAccess(t *testing.T) {
	searcher := &searchStub{output: "cited answer"}

	call, err := New(func() bool { return false }, allowLookup, searcher).Parse(`{"query":"weather"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, ErrWithheld) {
		t.Errorf("got %v, want %v", err, ErrWithheld)
	}
	if searcher.query != "" {
		t.Errorf("the searcher was reached with %q", searcher.query)
	}
}

func TestLookupReportsAnAnswerWithNoContent(t *testing.T) {
	call, err := New(func() bool { return true }, allowLookup, &searchStub{}).Parse(`{"query":"weather"}`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := call.Exec(t.Context()); err == nil || !strings.Contains(err.Error(), "no content") {
		t.Errorf("got %v", err)
	}
}

func TestARefusedLookupSearchesForNothing(t *testing.T) {
	searcher := &searchStub{output: "cited answer"}
	refusal := errors.New("the user refused this lookup")

	call, err := New(
		func() bool { return true },
		func(context.Context, string) error { return refusal },
		searcher,
	).Parse(`{"query":"weather"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := call.Exec(t.Context()); !errors.Is(err, refusal) {
		t.Errorf("got %v, want the refusal", err)
	}
	if searcher.queries != 0 {
		t.Errorf("the searcher was asked %d times", searcher.queries)
	}
}

func TestALookupIsAskedAboutTheQueryItWasGiven(t *testing.T) {
	asked := ""

	call, err := New(
		func() bool { return true },
		func(_ context.Context, query string) error {
			asked = query
			return nil
		},
		&searchStub{output: "cited answer"},
	).Parse(`{"query":"current weather"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := call.Exec(t.Context()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if asked != "current weather" {
		t.Errorf("got approval for %q, want the query", asked)
	}
}
