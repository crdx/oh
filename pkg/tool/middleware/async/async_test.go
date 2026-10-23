package async_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/async"
	"crdx.org/oh/pkg/tool/middleware/truncate"
)

type arguments struct {
	Command string `json:"command"`
}

func heldTool(release <-chan struct{}, runs *int) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: "bash", Description: "run a command", Schema: tool.Schema{tool.String("command", "shell command")}},
		func(args arguments) tool.CallRendering { return tool.CallRendering{Subject: args.Command} },
	).Plain(func(ctx context.Context, _ arguments) (string, error) {
		*runs++
		if err := tool.Launched(ctx); err != nil {
			return "", err
		}
		<-release
		return "done", nil
	})
}

func TestBackgroundCallReturnsAReceiptAndKeepsExecuting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		release := make(chan struct{})
		runs := 0
		subject := async.Tool(heldTool(release, &runs), manager, async.Options{IsRestartable: true})
		call, err := subject.Parse(`{"command":"sleep","background":true}`)
		if err != nil {
			t.Fatal(err)
		}
		acknowledgement, err := call.Exec(t.Context())
		if err != nil || !acknowledgement.IsPending {
			t.Errorf("got acknowledgement %+v, %v", acknowledgement, err)
		}
		close(release)
		synctest.Wait()
		conclusions := manager.Drain()
		if len(conclusions) != 1 || conclusions[0].Result.Output != "done" || runs != 1 {
			t.Errorf("got %v conclusions after %d executions", conclusions, runs)
		}
	})
}

func TestFrozenOldSchemaKeepsItsForegroundCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		release := make(chan struct{})
		close(release)
		runs := 0
		subject := async.Tool(heldTool(release, &runs), manager, async.Options{})
		oldSchema := tool.Schema{tool.String("command", "shell command")}
		call, err := tool.ParseUnder(subject, oldSchema, `{"command":"true"}`)
		if err != nil {
			t.Fatal(err)
		}
		result, err := call.Exec(t.Context())
		if err != nil || result.IsPending || result.Output != "done" {
			t.Errorf("old call returned %+v, %v", result, err)
		}
		if len(manager.List()) != 0 {
			t.Error("frozen old call unexpectedly became a task")
		}
	})
}

func TestRespawnRequiresBackgroundAtParseTime(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	var runs int
	subject := async.Tool(heldTool(nil, &runs), manager, async.Options{IsRestartable: true})
	if _, err := subject.Parse(`{"command":"true","respawn":true}`); err == nil {
		t.Error("respawned a foreground call")
	}
}

func TestAResumedAsyncToolParsesAgainstItsFrozenSchema(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		completed := make(chan struct{})
		subject := tool.Implement(
			tool.Definition{Name: "weather", Description: "get weather", Schema: tool.Schema{
				tool.String("city", "city name"), tool.String("country", "country name"),
			}},
			func(args struct{ City, Country string }) tool.CallRendering {
				return tool.CallRendering{Subject: args.City}
			},
		).Plain(func(context.Context, struct{ City, Country string }) (string, error) {
			close(completed)
			return "clear", nil
		})
		wrapped := async.Tool(subject, manager, async.Options{})
		frozen := tool.Schema{tool.String("city", "city name"), tool.Boolean("background", "start in background").Optional()}
		call, err := tool.ParseUnder(wrapped, frozen, `{"city":"Bath","background":true}`)
		if err != nil {
			t.Fatal(err)
		}
		result, err := call.Exec(t.Context())
		if err != nil || !result.IsPending {
			t.Errorf("frozen tool call failed: %+v, %v", result, err)
		}
		<-completed
	})
}

func TestAnAsyncToolResumedHeadlessStillRunsAnOldForegroundCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		close(release)
		runs := 0
		wrapped := async.Tool(heldTool(release, &runs), nil, async.Options{BackgroundUnavailable: "background calls are unavailable in print mode"})
		frozen := tool.Schema{tool.String("command", "shell command"), tool.Boolean("background", "start in background").Optional()}
		for index, arguments := range []string{`{"command":"true"}`, `{"command":"true","background":false}`} {
			call, err := tool.ParseUnder(wrapped, frozen, arguments)
			if err != nil {
				t.Fatalf("case %d: %v", index, err)
			}
			result, err := call.Exec(t.Context())
			if err != nil || result.Output != "done" || runs != index+1 {
				t.Errorf("case %d: headless foreground returned %+v, %v after %d runs", index, result, err, runs)
			}
		}
	})
}

func TestTheSupervisorStoresOnlyTruncatedToolOutput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		output := strings.Repeat("x", 2048)
		subject := tool.Implement(
			tool.Definition{Name: "weather", Schema: tool.Schema{}},
			func(struct{}) tool.CallRendering { return tool.CallRendering{} },
		).Plain(func(context.Context, struct{}) (string, error) {
			return output, nil
		})
		bounded := truncate.Tool(subject, truncate.NewLimit(512), nil)
		call, err := async.Tool(bounded, manager, async.Options{}).Parse(`{"background":true}`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := call.Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		over, release, err := manager.Hold("task-1")
		if err != nil {
			t.Fatal(err)
		}
		<-over
		release()
		snapshot, err := manager.Output("task-1")
		if err != nil || len(snapshot.Output) >= len(output) || !strings.Contains(snapshot.Output, "truncated") {
			t.Errorf("the saved output was not bounded: %d bytes, %v", len(snapshot.Output), err)
		}
	})
}

func TestAQueuedBackgroundReceiptDoesNotClaimTheTaskIsRunning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		started := make(chan struct{}, tasks.MaxRunning)
		for index := range tasks.MaxRunning {
			_, err := manager.Start(t.Context(), fmt.Sprintf("hold-%d", index), "bash", true, false,
				func(ctx context.Context) (tool.ToolCallResult, error) {
					started <- struct{}{}
					<-ctx.Done()
					return tool.ToolCallResult{}, ctx.Err()
				}, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		for range tasks.MaxRunning {
			<-started
		}
		call, err := async.Tool(heldTool(nil, new(int)), manager, async.Options{}).Parse(`{"command":"later","background":true}`)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := call.Exec(t.Context())
		if err != nil || !strings.Contains(receipt.Output, "scheduled") || strings.Contains(receipt.Output, "is running") {
			t.Errorf("queued call claimed to run: %+v, %v", receipt, err)
		}
		if snapshot, err := manager.Status("task-1"); err != nil || snapshot.State != tasks.Queued {
			t.Errorf("task was not queued: %+v, %v", snapshot, err)
		}
	})
}

func TestFrozenBackgroundRequestNamesWhyTaskControlsAreUnavailable(t *testing.T) {
	wrapped := async.Tool(heldTool(nil, new(int)), nil, async.Options{
		BackgroundUnavailable: "background tasks require the task tool, which is disabled",
	})
	frozen := tool.Schema{tool.String("command", "shell command"), tool.Boolean("background", "start in background").Optional()}
	_, err := tool.ParseUnder(wrapped, frozen, `{"command":"true","background":true}`)
	if err == nil || !strings.Contains(err.Error(), "task tool") || strings.Contains(err.Error(), "print mode") {
		t.Errorf("unavailable task controls reported %v", err)
	}
}

func TestAForegroundCallHandsOffAfterThirtySecondsWithoutRestarting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		release := make(chan struct{})
		runs := 0
		subject := async.Tool(heldTool(release, &runs), manager, async.Options{})
		call, err := subject.Parse(`{"command":"slow command"}`)
		if err != nil {
			t.Fatal(err)
		}
		startedAt := time.Now()
		receipt, err := call.Exec(t.Context())
		if elapsed := time.Since(startedAt); elapsed != 30*time.Second {
			t.Errorf("handoff took %s, want 30s", elapsed)
		}
		if err != nil || !receipt.IsPending || !strings.Contains(receipt.Output, "moved to the background as task-1") {
			t.Errorf("automatic handoff returned %+v, %v", receipt, err)
		}
		close(release)
		synctest.Wait()
		conclusions := manager.Drain()
		if runs != 1 || len(conclusions) != 1 || conclusions[0].Result.Output != "done" {
			t.Errorf("handoff restarted or lost the call: %d runs, %+v", runs, conclusions)
		}
	})
}

func TestNamedBackgroundLaunchesPublishTheirPortOnceStarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		runs := 0
		release := make(chan struct{})
		close(release)
		publications := 0
		settings := async.Options{PublishPort: func(_ context.Context, port uint16, name string) (string, error) {
			if port != 8000 || name != "docs" || runs != publications+1 {
				t.Errorf("publication did not follow the start: port=%d name=%q runs=%d publications=%d", port, name, runs, publications)
			}
			publications++
			return "Use http://127.1.2.3:8000 for the user", nil
		}}
		subject := async.Tool(heldTool(release, &runs), manager, settings)
		async.Bind(subject, subject.Schema())
		call, err := subject.Parse(`{"command":"serve docs","background":true,"task_name":"docs","forward_port":8000}`)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := call.Exec(t.Context())
		if err != nil || !strings.Contains(receipt.Output, "scheduled as docs") || !strings.Contains(receipt.Output, "http://127.1.2.3:8000") {
			t.Errorf("named server receipt is %+v, %v", receipt, err)
		}
		synctest.Wait()
		if _, err := manager.Restart(t.Context(), "docs"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if runs != 2 || publications != 2 {
			t.Errorf("restart lost its command or port: %d runs, %d publications", runs, publications)
		}
	})
}

func TestAPortPublicationFailureEndsTheRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		runs := 0
		subject := async.Tool(heldTool(nil, &runs), manager, async.Options{
			PublishPort: func(context.Context, uint16, string) (string, error) { return "", errors.New("port unavailable") },
		})
		call, err := subject.Parse(`{"command":"serve docs","background":true,"forward_port":8000}`)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := call.Exec(t.Context())
		if err != nil || !strings.Contains(receipt.Output, "port 8000 was not forwarded because the task ended") {
			t.Errorf("port refusal returned %+v, %v", receipt, err)
		}
		synctest.Wait()
		conclusions := manager.Drain()
		if runs != 1 || len(conclusions) != 1 || conclusions[0].Snapshot.State != tasks.Failed ||
			!strings.Contains(conclusions[0].Snapshot.Failure, "port 8000 could not be forwarded: port unavailable") {
			t.Errorf("failed publication left %d executions and %+v", runs, conclusions)
		}
	})
}

func TestACallRefusedBeforeItStartsPublishesNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		refused := tool.Implement(
			tool.Definition{Name: "bash", Description: "run a command", Schema: tool.Schema{tool.String("command", "shell command")}},
			func(args arguments) tool.CallRendering { return tool.CallRendering{Subject: args.Command} },
		).Plain(func(context.Context, arguments) (string, error) {
			return "", errors.New("access refused")
		})
		publications := 0
		subject := async.Tool(refused, manager, async.Options{
			PublishPort: func(context.Context, uint16, string) (string, error) {
				publications++
				return "published", nil
			},
		})
		call, err := subject.Parse(`{"command":"serve docs","background":true,"forward_port":8000}`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := call.Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if publications != 0 {
			t.Errorf("a refused call published its port %d times", publications)
		}
	})
}

func TestARevokedPortIsNotPublishedAgainOnRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		runs := 0
		release := make(chan struct{})
		close(release)
		publications := 0
		subject := async.Tool(heldTool(release, &runs), manager, async.Options{
			PublishPort: func(context.Context, uint16, string) (string, error) {
				publications++
				return "published", nil
			},
		})
		async.Bind(subject, subject.Schema())
		call, err := subject.Parse(`{"command":"serve docs","background":true,"task_name":"docs","forward_port":8000}`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := call.Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if !manager.ForgetPort("docs", 8000) {
			t.Fatal("the task did not hold the revoked port")
		}
		if _, err := manager.Restart(t.Context(), "docs"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		snapshot, err := manager.Status("docs")
		if err != nil {
			t.Fatal(err)
		}
		if runs != 2 || publications != 1 || snapshot.Invocation.ForwardPort != 0 {
			t.Errorf("restart after revocation: %d runs, %d publications, %+v", runs, publications, snapshot.Invocation)
		}
	})
}

func TestTaskLaunchOptionsRequireBackgroundAndSandboxNetworking(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	subject := async.Tool(heldTool(nil, new(int)), manager, async.Options{
		PublishPort: func(context.Context, uint16, string) (string, error) { return "forwarded", nil },
	})
	nameParameter := subject.Schema().Find("task_name")
	if nameParameter == nil || !strings.Contains(nameParameter.Description, tasks.NamePattern()) {
		t.Errorf("task_name description hides its pattern: %+v", nameParameter)
	}
	for _, arguments := range []string{
		`{"command":"true","task_name":"build"}`,
		`{"command":"true","forward_port":8000}`,
		`{"command":"true","background":true,"task_name":"two words"}`,
		`{"command":"true","background":true,"forward_port":65536}`,
		`{"command":"true","background":true,"forward_port":-1}`,
	} {
		if _, err := subject.Parse(arguments); err == nil {
			t.Errorf("invalid launch options were accepted: %s", arguments)
		}
	}
}

func TestAnInvalidAsyncOptionDoesNotHideTheCallRendering(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	subject := async.Tool(heldTool(nil, new(int)), manager, async.Options{
		IsRestartable: true,
		PublishPort:   func(context.Context, uint16, string) (string, error) { return "forwarded", nil },
	})
	for _, arguments := range []string{
		`{"command":"serve docs","background":true,"task_name":"http-server","forward_port":8000}`,
		`{"command":"serve docs","background":"yes"}`,
		`{"command":"serve docs","background":true,"task_name":7}`,
		`{"command":"serve docs","background":true,"forward_port":"8000"}`,
		`{"command":"serve docs","background":true,"respawn":"yes"}`,
		`{"command":"serve docs","task_name":"docs"}`,
	} {
		if _, err := subject.Parse(arguments); err == nil {
			t.Errorf("accepted invalid async options: %s", arguments)
		}
		rendering, isRendered := subject.Render(arguments)
		if !isRendered || rendering.Subject != "serve docs" || rendering.Introduces != "" {
			t.Errorf("%s got rendered=%t and rendering %+v", arguments, isRendered, rendering)
		}
	}
}

func TestTheDescriptionNamesTheForegroundLimit(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit time.Duration
		want  string
	}{
		{"default", 0, "after 30 active seconds continues in the background"},
		{"one second", time.Second, "after 1 active second continues in the background"},
		{"whole minutes", 2 * time.Minute, "after 2 active minutes continues in the background"},
		{"odd seconds", 90 * time.Second, "after 90 active seconds continues in the background"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := tasks.New(t.Context())
			defer manager.Close()
			manager.SetForegroundLimit(test.limit)
			description := async.Tool(heldTool(nil, new(int)), manager, async.Options{}).Description()
			if !strings.Contains(description, test.want) || !strings.Contains(description, "A task stops after 30 active minutes") {
				t.Errorf("got description %q, want it to say %q", description, test.want)
			}
		})
	}
}

func TestALongerToolLimitIsTheTaskLimitTheModelIsTold(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit time.Duration
		want  string
	}{
		{"none", 0, "A task stops after 30 active minutes"},
		{"shorter", 10 * time.Minute, "A task stops after 30 active minutes"},
		{"longer", 45 * time.Minute, "A task stops after 45 active minutes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := tasks.New(t.Context())
			defer manager.Close()
			description := async.Tool(heldTool(nil, new(int)), manager, async.Options{RunLimit: test.limit}).Description()
			if !strings.Contains(description, test.want) {
				t.Errorf("got description %q, want it to say %q", description, test.want)
			}
		})
	}
}

func TestABackgroundCallRunsUnderTheLongerToolLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := tasks.New(t.Context())
		defer manager.Close()
		release := make(chan struct{})
		subject := async.Tool(heldTool(release, new(int)), manager, async.Options{RunLimit: 45 * time.Minute})
		call, err := subject.Parse(`{"command":"sleep","background":true,"task_name":"long"}`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := call.Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(40 * time.Minute)
		synctest.Wait()
		for _, snapshot := range manager.List() {
			if snapshot.ID == "long" && snapshot.State != tasks.Running {
				t.Errorf("the call was %s after 40 minutes, want it running under its 45-minute limit", snapshot.State)
			}
		}
		close(release)
		synctest.Wait()
	})
}
