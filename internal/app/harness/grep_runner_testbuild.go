//go:build oh_grep_test

package harness

import (
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/sandbox/testexec"
)

func grepRunner(isUnconfined bool) sandbox.ArgvRunner {
	if isUnconfined {
		return sandbox.UnconfinedArgv()
	}
	return testexec.New()
}
