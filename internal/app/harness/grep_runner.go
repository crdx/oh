//go:build !oh_grep_test

package harness

import "crdx.org/oh/internal/sandbox"

func grepRunner(isUnconfined bool) sandbox.ArgvRunner {
	return sandbox.ArgvFor(isUnconfined)
}
