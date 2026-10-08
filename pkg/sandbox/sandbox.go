package sandbox

import (
	"context"
	"errors"

	internal "crdx.org/oh/internal/sandbox"
)

type Policy = internal.Policy

type Runner = internal.Runner

type Command = internal.Command

type Output = internal.Output

type Result = internal.Result

const TmpDir = internal.TmpDir

var errUnconfined = errors.New("sandbox.Direct refuses an unconfined policy")

func Init() {
	internal.Init()
}

func Supported(ctx context.Context) error {
	return internal.Supported(ctx)
}

func Direct() Runner {
	return directRunner{inner: internal.Direct()}
}

type directRunner struct {
	inner Runner
}

func (self directRunner) Run(ctx context.Context, directory string, command string, policy Policy) (Result, error) {
	if policy.Yolo {
		return Result{}, errUnconfined
	}
	return self.inner.Run(ctx, directory, command, policy.Clone())
}

func (self directRunner) Start(
	ctx context.Context,
	directory string,
	command string,
	policy Policy,
	output Output,
) (Command, error) {
	if policy.Yolo {
		return nil, errUnconfined
	}
	return self.inner.Start(ctx, directory, command, policy.Clone(), output)
}
