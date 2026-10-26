package harness

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

func TestReloadingTheBackgroundDelayChangesTheExistingManager(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[experimental]\nbackground_after_seconds = 5\n")
	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)
	self.tasks = tasks.New(t.Context())
	t.Cleanup(self.tasks.Close)
	configureTaskManager(self.tasks, self.experimental)
	if got := self.tasks.ForegroundLimit(); got != 5*time.Second {
		t.Fatalf("initial delay is %s", got)
	}
	writeLiveConfig(t, path, "[experimental]\nbackground_after_seconds = 2\n")
	settleLiveConfig(t, self)
	if got := self.tasks.ForegroundLimit(); got != 2*time.Second {
		t.Errorf("reloaded delay is %s", got)
	}
	writeLiveConfig(t, path, "[experimental]\nbackground_after_seconds = 0\n")
	settleLiveConfig(t, self)
	if got := self.tasks.ForegroundLimit(); got != tasks.ForegroundLimit {
		t.Errorf("fallback delay is %s", got)
	}
	writeLiveConfig(t, path, "")
	settleLiveConfig(t, self)
	if got := self.tasks.ForegroundLimit(); got != 10*time.Second {
		t.Errorf("default delay is %s", got)
	}
}

func TestReloadingTheTaskLimitReconfiguresExistingAdmissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	writeLiveConfig(t, path, "[experimental]\nmaximum_running_tasks = 1\n")
	self := testConversation(t, &bytes.Buffer{})
	prepareLiveConfig(t, self, path)
	self.tasks = tasks.New(t.Context())
	t.Cleanup(self.tasks.Close)
	configureTaskManager(self.tasks, self.experimental)
	started := make(chan string, 3)
	for _, name := range []string{"one", "two"} {
		if _, err := self.tasks.Start(t.Context(), name, "bash", true, false, func(ctx context.Context) (tool.ToolCallResult, error) {
			started <- name
			<-ctx.Done()
			return tool.ToolCallResult{}, ctx.Err()
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first task never started")
	}
	writeLiveConfig(t, path, "[experimental]\nmaximum_running_tasks = 2\n")
	settleLiveConfig(t, self)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the queued task did not start after reload")
	}
	writeLiveConfig(t, path, "[experimental]\nmaximum_running_tasks = 1\n")
	settleLiveConfig(t, self)
	for _, name := range []string{"one", "two"} {
		snapshot, err := self.tasks.Status(name)
		if err != nil || snapshot.State != tasks.Running {
			t.Errorf("task %s is %+v, %v", name, snapshot, err)
		}
	}
	if _, err := self.tasks.Start(t.Context(), "three", "bash", true, false, func(ctx context.Context) (tool.ToolCallResult, error) {
		started <- "three"
		<-ctx.Done()
		return tool.ToolCallResult{}, ctx.Err()
	}, nil); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := self.tasks.Status("three"); err != nil || snapshot.State != tasks.Queued {
		t.Fatalf("third task is %+v, %v", snapshot, err)
	}
	writeLiveConfig(t, path, "")
	settleLiveConfig(t, self)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the queued task did not start after restoring defaults")
	}
}

func TestGoldenTaskAndTimeoutReloadConfirmationIsLive(t *testing.T) {
	passes := map[string]func() string{
		"task settings": func() string {
			return reloadConfirmation([]config.SourceChange{{Path: "config.toml", Settings: []string{"experimental.background_after_seconds", "experimental.maximum_running_tasks"}}}, config.ReachOf)
		},
		"request settings": func() string {
			return reloadConfirmation([]config.SourceChange{{Path: "config.toml", Settings: []string{"experimental.response_timeout", "experimental.stream_idle_timeout"}}}, config.ReachOf)
		},
	}
	compareWithGolden(t, "live-task-settings", ".screen", passes)
}
