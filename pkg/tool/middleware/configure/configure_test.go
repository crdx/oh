package configure_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/configure"
)

type arguments struct {
	Path string `json:"path"`
}

func makeTool() tool.Tool {
	return tool.Implement(
		tool.Definition{
			Name:        "read",
			Description: "read a file",
			Schema:      tool.Schema{tool.String("path", "file")},
		},
		func(args arguments) tool.CallRendering { return tool.CallRendering{Subject: args.Path} },
	).Revision("2").CompatibleWith("1").MarksSuccess().Plain(func(_ context.Context, args arguments) (string, error) {
		return args.Path, nil
	})
}

func TestAnOverrideKeepsTheFrozenToolContract(t *testing.T) {
	original := makeTool()
	configured := configure.Tool(original, configure.Options{Description: "read this file"})
	if configured.Description() != "read this file" || !tool.MarksSuccess(configured) {
		t.Errorf("configured tool lost its description or success marker")
	}
	if !tool.AcceptsRevision(configured, "1") {
		t.Error("configuration lost the original compatibility")
	}

	oldSchema := tool.Schema{tool.String("path", "file"), tool.String("hint", "optional hint").Optional()}
	call, err := tool.ParseUnder(configured, oldSchema, `{"path":"notes.md","hint":"older call"}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if err != nil || result.Output != "notes.md" {
		t.Errorf("frozen call returned %q, %v", result.Output, err)
	}
}

func TestAnAccessRefusalPrecedesAnApproval(t *testing.T) {
	var wasAsked bool
	configured := configure.Tool(makeTool(), configure.Options{
		RequireAccess: func() error { return errors.New("withheld") },
		Approve: func(context.Context, string) error {
			wasAsked = true
			return nil
		},
	})
	call, err := configured.Parse(`{"path":"notes.md"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = call.Exec(t.Context())
	if err == nil || err.Error() != "withheld" || wasAsked {
		t.Errorf("got refusal %v, asked %t", err, wasAsked)
	}
}

func TestATimeLimitIsVisibleAndStartsAfterApproval(t *testing.T) {
	wasApproved := false
	configured := configure.Tool(makeTool(), configure.Options{
		TimeLimit: time.Second,
		Approve: func(context.Context, string) error {
			wasApproved = true
			return nil
		},
	})
	call, err := configured.Parse(`{"path":"notes.md"}`)
	if err != nil {
		t.Fatal(err)
	}
	if call.TimeLimit() != time.Second {
		t.Errorf("got limit %s", call.TimeLimit())
	}
	result, err := call.Exec(t.Context())
	if err != nil || !wasApproved || result.Output != "notes.md" {
		t.Errorf("got %q, %v; approved %t", result.Output, err, wasApproved)
	}
}

func TestAConfiguredTimeoutNamesItsLimitAndPartialOutput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		original := tool.Implement(
			tool.Definition{Name: "hold", Description: "hold until stopped"},
			func(struct{}) tool.CallRendering { return tool.CallRendering{} },
		).Run(func(ctx context.Context, _ struct{}) (tool.ToolCallResult, error) {
			<-ctx.Done()
			return tool.ToolCallResult{Output: "partial output"}, ctx.Err()
		})
		configured := configure.Tool(original, configure.Options{TimeLimit: time.Second})
		call, err := configured.Parse(`{}`)
		if err != nil {
			t.Fatal(err)
		}
		result, err := call.Exec(t.Context())
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(result.Output, "tool timeout of 1s") || !strings.Contains(result.Output, "partial output") {
			t.Errorf("got result %q and failure %v", result.Output, err)
		}
	})
}

func TestAccessIsCheckedAgainAfterApproval(t *testing.T) {
	withheld := errors.New("group access was withdrawn")
	isAllowed := true
	configured := configure.Tool(makeTool(), configure.Options{
		RequireAccess: func() error {
			if !isAllowed {
				return withheld
			}
			return nil
		},
		Approve: func(context.Context, string) error {
			isAllowed = false
			return nil
		},
	})
	call, err := configured.Parse(`{"path":"notes.md"}`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := call.Exec(t.Context())
	if !errors.Is(err, withheld) || result.Output != "" {
		t.Errorf("withdrawn call ran: %+v, %v", result, err)
	}
}

func TestAccessIsCheckedAgainAfterAConcurrencyWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		withheld := errors.New("group access was withdrawn")
		isAllowed := true
		runs := 0
		release := make(chan struct{})
		original := tool.Implement(
			tool.Definition{Name: "hold"},
			func(struct{}) tool.CallRendering { return tool.CallRendering{} },
		).Plain(func(context.Context, struct{}) (string, error) {
			runs++
			<-release
			return "done", nil
		})
		configured := configure.Tool(original, configure.Options{
			Concurrency: 1,
			RequireAccess: func() error {
				if !isAllowed {
					return withheld
				}
				return nil
			},
		})
		call, err := configured.Parse(`{}`)
		if err != nil {
			t.Fatal(err)
		}
		results := make(chan error, 2)
		go func() { _, failure := call.Exec(t.Context()); results <- failure }()
		synctest.Wait()
		go func() { _, failure := call.Exec(t.Context()); results <- failure }()
		synctest.Wait()
		isAllowed = false
		close(release)
		synctest.Wait()
		failures := []error{<-results, <-results}
		if runs != 1 || !errors.Is(failures[0], withheld) && !errors.Is(failures[1], withheld) {
			t.Errorf("queued call ran after withdrawal: %d runs, %v", runs, failures)
		}
	})
}
