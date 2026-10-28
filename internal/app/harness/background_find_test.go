package harness

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/task"
)

func TestFindRunsInTheForegroundOrBackground(t *testing.T) {
	for _, test := range []struct {
		name         string
		arguments    string
		delay        time.Duration
		wantWait     time.Duration
		isBackground bool
	}{
		{"foreground", `{"pattern":"*.go"}`, 0, 0, false},
		{"explicit background", `{"pattern":"*.go","background":true}`, time.Second, 0, true},
		{"automatic background", `{"pattern":"*.go"}`, 2 * time.Second, time.Second, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				settings, err := config.LoadSources()
				if err != nil {
					t.Fatal(err)
				}
				manager := tasks.New(t.Context())
				defer manager.Close()
				configureTaskManager(manager, experimental.New(settings.Experimental))
				searched := newSessionGoldenSearchedFiles(t)
				finder := delayedFindTool{
					Tool:  searched.tool(t, sessionGoldenTool{Name: findToolName}),
					delay: test.delay,
				}
				tools, err := backgroundTools([]tool.Tool{finder, task.New(manager)}, backgroundOptions{
					settings: settings,
					manager:  manager,
					policyFor: func(string) func(context.Context, string) (any, error) {
						return nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				call, err := tools[0].Parse(test.arguments)
				if err != nil {
					t.Fatal(err)
				}
				started := time.Now()
				result, err := call.Exec(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if waited := time.Since(started); waited != test.wantWait {
					t.Errorf("find returned after %s, want %s", waited, test.wantWait)
				}
				if result.IsPending != test.isBackground {
					t.Fatalf("find pending=%t, want %t: %s", result.IsPending, test.isBackground, result.Output)
				}
				if test.isBackground {
					if !strings.Contains(result.Output, `backgrounded (named "find-1")`) {
						t.Fatalf("find returned no task receipt: %s", result.Output)
					}
					time.Sleep(test.delay)
					synctest.Wait()
					listing := manager.List()
					if len(listing) != 1 || listing[0].State != tasks.Done {
						t.Fatalf("find did not complete: %+v", listing)
					}
					result.Output = listing[0].Output
				}
				if !strings.Contains(result.Output, "main.go") || strings.Contains(result.Output, "server.log") {
					t.Errorf("find returned the wrong matches: %s", result.Output)
				}
			})
		})
	}
}

type delayedFindTool struct {
	tool.Tool

	delay time.Duration
}

func (self delayedFindTool) Parse(arguments string) (tool.ToolCall, error) {
	call, err := self.Tool.Parse(arguments)
	if err != nil {
		return nil, err
	}
	return delayedFindCall{ToolCall: call, delay: self.delay}, nil
}

type delayedFindCall struct {
	tool.ToolCall

	delay time.Duration
}

func (self delayedFindCall) Exec(ctx context.Context) (tool.ToolCallResult, error) {
	select {
	case <-time.After(self.delay):
		return self.ToolCall.Exec(ctx)
	case <-ctx.Done():
		return tool.ToolCallResult{}, ctx.Err()
	}
}
