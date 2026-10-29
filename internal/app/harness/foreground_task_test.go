package harness

import (
	"bytes"
	"context"
	"testing"

	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/output"
	"crdx.org/oh/pkg/tasks"
	"crdx.org/oh/pkg/tool"
)

func TestGoldenForegroundCallStopNoticesUseShortLabels(t *testing.T) {
	passes := map[string]func() string{
		"foreground calls stopped": func() string {
			var screenOutput bytes.Buffer
			self := testConversation(t, &screenOutput)
			self.screen = output.NewTerminalOfSize(&screenOutput, replayColumns, replayLines)
			manager := tasks.New(t.Context())
			defer manager.Close()
			for range 2 {
				_, err := manager.Start(t.Context(), "", "bash", false, false,
					func(ctx context.Context) (tool.ToolCallResult, error) {
						<-ctx.Done()
						return tool.ToolCallResult{}, ctx.Err()
					}, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range manager.StopWhere(func(string, any) bool { return true }, "write access was withdrawn") {
				self.notify(caps.TaskStopEvent(name, caps.Write))
			}
			self.screen.End()
			return screenOutput.String()
		},
	}
	compareWithGolden(t, "foreground-call-labels", ".ansi", passes)
	compareWithGolden(t, "foreground-call-labels", ".screen", shownPasses(t, passes))
}
