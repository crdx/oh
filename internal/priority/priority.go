package priority

import (
	"os/exec"
	"runtime"
	"sync"

	"golang.org/x/sys/unix"
)

const (
	Niceness       = 10
	currentProcess = 0
)

type request struct {
	command *exec.Cmd
	result  chan error
}

var (
	launch   sync.Once
	requests = make(chan request)
)

func Start(command *exec.Cmd) error {
	launch.Do(func() {
		ready := make(chan struct{})
		go serve(ready)
		<-ready
	})
	result := make(chan error, 1)
	requests <- request{command: command, result: result}
	return <-result
}

func serve(ready chan<- struct{}) {
	runtime.LockOSThread()
	_ = lower()
	close(ready)
	for next := range requests {
		next.result <- next.command.Start()
	}
}

func lower() error {
	return unix.Setpriority(unix.PRIO_PROCESS, currentProcess, Niceness)
}
