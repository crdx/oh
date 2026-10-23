package harness

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/toolset"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/toolbox/forward"
)

func TestSelectingBackgroundableBashAlsoOffersTaskControls(t *testing.T) {
	tools := []tool.Tool{
		tool.Implement(tool.Definition{Name: "bash", Schema: tool.Schema{tool.Boolean("background", "allow background").Optional()}},
			func(struct{}) tool.CallRendering { return tool.CallRendering{} }).Plain(func(_ context.Context, _ struct{}) (string, error) { return "", nil }),
		tool.Implement(tool.Definition{Name: "task"}, func(struct{}) tool.CallRendering { return tool.CallRendering{} }).Plain(func(_ context.Context, _ struct{}) (string, error) { return "", nil }),
	}
	prepared, err := prepareLegacyTools(nil, tools, []string{"bash"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if names := toolset.Names(prepared.offeredTools); !slices.Equal(names, []string{"bash", "task"}) {
		t.Errorf("backgroundable bash offered %v, want bash and task", names)
	}
	prepared, err = prepareLegacyTools(&store.Session{Meta: store.Meta{Tools: []string{"bash"}}}, tools, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if names := toolset.Names(prepared.offeredTools); !slices.Equal(names, []string{"bash"}) {
		t.Errorf("a name-only resume offered %v, want bash alone", names)
	}
}

type recordedCommand struct {
	Command string `json:"command"`
}

func plainTestTool(name string, runs *[]string) tool.Tool {
	return tool.Implement(
		tool.Definition{Name: name, Schema: tool.Schema{tool.String("command", "command")}},
		func(recordedCommand) tool.CallRendering { return tool.CallRendering{} },
	).Plain(func(ctx context.Context, args recordedCommand) (string, error) {
		*runs = append(*runs, args.Command)
		return "ran " + args.Command, nil
	})
}

func taskTestTool() tool.Tool {
	return tool.Implement(tool.Definition{Name: "task"}, func(struct{}) tool.CallRendering { return tool.CallRendering{} }).
		Plain(func(_ context.Context, _ struct{}) (string, error) { return "", nil })
}

func testBackgroundOptions(manager *tasks.Manager) backgroundOptions {
	return backgroundOptions{
		manager:   manager,
		policyFor: func(string) func(context.Context, string) (any, error) { return nil },
	}
}

func TestANameOnlyResumeKeepsTheHistoricalToolbox(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	var runs []string
	options := testBackgroundOptions(manager)
	options.resumedSession = &store.Session{Meta: store.Meta{Tools: []string{"bash", "read"}}}
	tools, err := backgroundTools([]tool.Tool{plainTestTool("bash", &runs), plainTestTool("read", &runs), taskTestTool()}, options)
	if err != nil {
		t.Fatal(err)
	}
	if names := toolset.Names(tools); !slices.Equal(names, []string{"bash", "read"}) {
		t.Errorf("a name-only resume kept %v, want bash and read", names)
	}
	if tools[0].Schema().Find("background") != nil {
		t.Error("a name-only resume gained background parameters")
	}
}

func TestBackgroundCallsNeedATaskSupervisor(t *testing.T) {
	var runs []string
	tools, err := backgroundTools([]tool.Tool{plainTestTool("bash", &runs), taskTestTool()}, testBackgroundOptions(nil))
	if err != nil {
		t.Fatal(err)
	}
	if tools[0].Schema().Find("background") != nil {
		t.Error("a session without a task supervisor offered background calls")
	}
	call, err := tools[0].Parse(`{"command":"true"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := call.Exec(t.Context()); err != nil || result.Output != "ran true" {
		t.Errorf("a foreground call returned %+v, %v", result, err)
	}
}

func TestAFrozenBackgroundSchemaRefusesBackgroundOnceItIsDisabled(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	isBackgroundable := false
	var runs []string
	options := testBackgroundOptions(manager)
	options.settings = config.Config{Tools: map[string]config.ToolConfig{"offload": {IsBackgroundable: &isBackgroundable}}}
	frozenSchema := tool.Schema{
		tool.String("command", "command"),
		tool.Boolean("background", "start in the background").Optional(),
		tool.String("task_name", "name").Optional(),
	}
	options.resumedSession = &store.Session{Meta: store.Meta{ToolDefinitions: store.FreezeTools([]tool.Tool{
		tool.Implement(tool.Definition{Name: "offload", Schema: frozenSchema}, func(struct{}) tool.CallRendering { return tool.CallRendering{} }).
			Plain(func(_ context.Context, _ struct{}) (string, error) { return "", nil }),
	})}}
	tools, err := backgroundTools([]tool.Tool{plainTestTool("offload", &runs), taskTestTool()}, options)
	if err != nil {
		t.Fatal(err)
	}
	parser, isParser := tools[0].(tool.SchemaParser)
	if !isParser {
		t.Fatal("the wrapped offload cannot parse a frozen schema")
	}
	if _, err := parser.ParseUnder(frozenSchema, `{"command":"serve","background":true}`); err == nil ||
		!strings.Contains(err.Error(), "background calls are disabled for the offload tool") {
		t.Errorf("a disabled background call parsed with %v", err)
	}
	call, err := parser.ParseUnder(frozenSchema, `{"command":"build","background":false}`)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := call.Exec(t.Context()); err != nil || result.Output != "ran build" || len(manager.List()) != 0 {
		t.Errorf("a foreground call returned %+v, %v with tasks %+v", result, err, manager.List())
	}
	if !slices.Equal(runs, []string{"build"}) {
		t.Errorf("commands ran %v, want build alone", runs)
	}
}

func TestOnlyBashForwardsAPortAndOnlyThroughAnOfferedForwardTool(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	var runs []string
	var forwarded []string
	forwardTool := tool.Implement(
		tool.Definition{Name: "forward", Schema: tool.Schema{
			tool.String("action", "action"), tool.Integer("port", "port"), tool.String("task_name", "task").Optional(),
		}},
		func(forward.Args) tool.CallRendering { return tool.CallRendering{} },
	).Plain(func(_ context.Context, args forward.Args) (string, error) {
		forwarded = append(forwarded, fmt.Sprintf("%s %d %s", args.Action, args.Port, args.TaskName))
		return "Use http://forwarded for the user", nil
	})
	toolbox := []tool.Tool{plainTestTool("bash", &runs), plainTestTool("grep", &runs), forwardTool, taskTestTool()}

	options := testBackgroundOptions(manager)
	tools, err := backgroundTools(toolbox, options)
	if err != nil {
		t.Fatal(err)
	}
	if tools[0].Schema().Find("forward_port") != nil {
		t.Error("bash offered forward_port without an offered forward tool")
	}

	options.isForwardOffered = true
	tools, err = backgroundTools(toolbox, options)
	if err != nil {
		t.Fatal(err)
	}
	if tools[0].Schema().Find("forward_port") == nil || tools[1].Schema().Find("forward_port") != nil {
		t.Errorf("forward_port offered to bash=%t grep=%t", tools[0].Schema().Find("forward_port") != nil, tools[1].Schema().Find("forward_port") != nil)
	}
	publish := portPublisher(forwardTool, false)
	if receipt, err := publish(t.Context(), 8000, "docs"); err != nil || receipt != "Use http://forwarded for the user" {
		t.Errorf("publication returned %q, %v", receipt, err)
	}
	if !slices.Equal(forwarded, []string{"add 8000 docs"}) {
		t.Errorf("the forward tool received %v", forwarded)
	}
}

func TestCapabilityWithdrawalMatchesEveryBackgroundTool(t *testing.T) {
	cases := []struct {
		name      string
		access    taskAccess
		withdrawn caps.Set
		want      bool
	}{
		{"host network", taskAccess{ToolName: "bash", IsHostNetwork: true}, caps.Network, true},
		{"sandbox network", taskAccess{ToolName: "bash"}, caps.Network, false},
		{"shell access", taskAccess{ToolName: "bash"}, caps.Shell, true},
		{"grep read", taskAccess{ToolName: "grep"}, caps.Read, true},
		{"lookup access", taskAccess{ToolName: "lookup"}, caps.Lookup, true},
		{"custom tool", taskAccess{ToolName: "weather"}, caps.Lookup, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			holds, doesStop := shell.StoppedBy(test.withdrawn, "/workspace")
			if got := taskAccessLost(test.access, test.withdrawn, holds, doesStop); got != test.want {
				t.Errorf("%+v with %s stopped=%t, want %t", test.access, test.withdrawn.Flags(), got, test.want)
			}
		})
	}
}

func TestRevokingAPathStopsOnlyTheSearchesThatCouldReachIt(t *testing.T) {
	root := t.TempDir()
	search := filepath.Join(root, "docs")
	secret := filepath.Join(root, "secret")
	for _, path := range []string{search, secret} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name        string
		searchPath  string
		revokedPath string
		want        bool
	}{
		{"unrelated directory", search, secret, false},
		{"search root", search, search, true},
		{"search ancestor", search, root, true},
		{"unknown search path", "", secret, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := taskSearchAffected(test.searchPath, test.revokedPath); got != test.want {
				t.Errorf("%q affected by %q: %t, want %t", test.searchPath, test.revokedPath, got, test.want)
			}
		})
	}
}

func TestAToolWithItsOwnBackgroundArgumentIsLeftAloneOnResume(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	note := tool.Implement(
		tool.Definition{Name: "note", Schema: tool.Schema{tool.Boolean("background", "the tool's own flag").Optional()}},
		func(struct{}) tool.CallRendering { return tool.CallRendering{} },
	).Plain(func(_ context.Context, _ struct{}) (string, error) { return "noted", nil })
	options := testBackgroundOptions(manager)
	options.resumedSession = &store.Session{Meta: store.Meta{ToolDefinitions: store.FreezeTools([]tool.Tool{note})}}
	tools, err := backgroundTools([]tool.Tool{note, taskTestTool()}, options)
	if err != nil {
		t.Fatalf("resume refused a tool that owns its background argument: %v", err)
	}
	call, err := tools[0].Parse(`{"background":true}`)
	if err != nil {
		t.Fatalf("the tool's own background argument was refused: %v", err)
	}
	if result, err := call.Exec(t.Context()); err != nil || result.Output != "noted" {
		t.Errorf("the tool's own background argument was taken: %+v, %v", result, err)
	}
}

func TestAToolsConfiguredTimeoutBecomesItsTaskLimit(t *testing.T) {
	manager := tasks.New(t.Context())
	defer manager.Close()
	var runs []string
	isBackgroundable := true
	options := testBackgroundOptions(manager)
	options.settings = config.Config{Tools: map[string]config.ToolConfig{
		"offload": {IsBackgroundable: &isBackgroundable, Timeout: 45 * time.Minute},
		"bash":    {IsBackgroundable: &isBackgroundable},
	}}
	tools, err := backgroundTools([]tool.Tool{plainTestTool("offload", &runs), plainTestTool("bash", &runs), taskTestTool()}, options)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"A task stops after 45 active minutes", "A task runs until it exits or you stop it"} {
		if description := tools[index].Description(); !strings.Contains(description, want) {
			t.Errorf("%s described itself as %q, want %q", tools[index].Name(), description, want)
		}
	}
}
