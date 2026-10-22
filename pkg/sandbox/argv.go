package sandbox

import (
	"context"

	"crdx.org/oh/internal/sandbox"
)

type ArgvRunner = sandbox.ArgvRunner

func DirectArgv() ArgvRunner {
	return directArgvRunner{inner: sandbox.DirectArgv()}
}

type directArgvRunner struct {
	inner ArgvRunner
}

func (self directArgvRunner) RunArgv(
	ctx context.Context,
	directory string,
	arguments []string,
	policy Policy,
) (Result, error) {
	if policy.Yolo {
		return Result{}, errUnconfined
	}
	return self.inner.RunArgv(ctx, directory, arguments, policy.Clone())
}

func (self directArgvRunner) StartArgv(
	ctx context.Context,
	directory string,
	arguments []string,
	policy Policy,
	output Output,
) (Command, error) {
	if policy.Yolo {
		return nil, errUnconfined
	}
	return self.inner.StartArgv(ctx, directory, arguments, policy.Clone(), output)
}
