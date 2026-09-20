package tty

import (
	"os"
	"os/signal"
	"syscall"
)

var fatalSignals = []os.Signal{
	syscall.SIGHUP,
	syscall.SIGINT,
	syscall.SIGQUIT,
	syscall.SIGTERM,
}

func RestoreOnSignal(restore func()) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, fatalSignals...)

	stoppedChannel := make(chan struct{})

	go watch(signals, stoppedChannel, restore, reraise)

	return func() {
		signal.Stop(signals)
		close(stoppedChannel)
	}
}

func watch(signals <-chan os.Signal, stoppedChannel <-chan struct{}, restore func(), raise func(os.Signal)) {
	select {
	case receivedSignal := <-signals:
		restore()
		raise(receivedSignal)
	case <-stoppedChannel:
	}
}

func reraise(receivedSignal os.Signal) {
	signal.Reset(receivedSignal)

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		os.Exit(1)
	}

	_ = process.Signal(receivedSignal)
}
