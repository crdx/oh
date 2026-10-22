package sandbox

import (
	"syscall"
	"testing"
)

func TestAFailureNamesTheExitStatusOrTheSignalThatEndedIt(t *testing.T) {
	for _, test := range []struct {
		result Result
		want   string
	}{
		{result: Result{ExitCode: 1}, want: "the command exited with status 1"},
		{result: Result{ExitCode: 137}, want: "the command exited with status 137"},
		{result: Result{ExitCode: 137, Signal: syscall.SIGKILL}, want: "SIGKILL stopped the command"},
	} {
		if got := FailureReason(test.result); got != test.want {
			t.Errorf("got %q, want %q", got, test.want)
		}
	}
}
