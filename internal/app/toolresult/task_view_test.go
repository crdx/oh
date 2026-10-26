package toolresult

import (
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/internal/toolresult"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tasks"
)

func TestInProcessStoredTaskRunIncludesItsInvocationAndOutput(t *testing.T) {
	stateDirectory := t.TempDir()
	t.Setenv(location.StateDirVariable, stateDirectory)
	writer, err := session.Create(location.GetSessionsDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	completion := taskrecord.Completion{Snapshot: tasks.Snapshot{
		Invocation: &tasks.Invocation{Arguments: `{"command":"seq 1 3"}`},
		ID:         "count",
		ToolName:   "bash",
		Run:        1,
		State:      tasks.Done,
		EndedAt:    time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
		Output:     "1\n2\n3\n",
	}, IsFinal: true}
	if _, err := writer.Event(taskrecord.FinishedEvent(tasks.Conclusion{Snapshot: completion.Snapshot, IsFinal: true})); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	if err := show(toolresult.TaskURL(completion.Reference(writer.Name())), false, location.GetSessionsDir(), &output, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := style.Plain(output.String()), "bash\n\n{\n    \"command\": \"seq 1 3\"\n}\n\n1\n2\n3\n"; got != want {
		t.Errorf("task result is %q, want %q", got, want)
	}
}
