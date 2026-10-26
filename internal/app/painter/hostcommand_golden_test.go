package painter

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"crdx.org/oh/internal/app/hostcommand"
	"crdx.org/oh/internal/app/style"
)

func TestGoldenHostCommandCountdownUsesItsOwnTimeLimit(t *testing.T) {
	var drawn strings.Builder
	for _, timeLimit := range []time.Duration{hostcommand.DefaultTimeLimit, 15 * time.Second} {
		for _, columns := range []int{120, 45, 16} {
			fmt.Fprintf(&drawn, "limit %s, width %d\n", timeLimit, columns)
			rows := RenderHostCommand("sleep 60", "begun", 12*time.Second, timeLimit, columns)
			for _, row := range rows {
				if got := style.Width(row); got > columns {
					t.Errorf("row width %d exceeds %d", got, columns)
				}
				drawn.WriteString(style.Plain(row))
				drawn.WriteByte('\n')
			}
			drawn.WriteString("\n")
		}
	}
	compareCallRowGolden(t, "host-command-countdown.screen", strings.TrimRight(drawn.String(), "\n")+"\n")
}
