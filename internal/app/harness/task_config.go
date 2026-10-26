package harness

import (
	"time"

	"crdx.org/oh/internal/app/experimental"
	"crdx.org/oh/pkg/tasks"
)

func configureTaskManager(manager *tasks.Manager, toggles *experimental.Toggles) {
	manager.SetMaximumRunning(toggles.GetWholeNumber(experimental.MaximumRunningTasks, tasks.DefaultMaximumRunning))
	foregroundLimit := time.Duration(toggles.GetWholeNumber(experimental.BackgroundAfterSeconds, 0)) * time.Second
	if foregroundLimit <= 0 {
		foregroundLimit = tasks.ForegroundLimit
	}
	manager.SetForegroundLimit(foregroundLimit)
}
