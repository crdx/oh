package sandbox

import (
	"context"
	"errors"

	"crdx.org/oh/internal/sandbox"
)

type Policy = sandbox.Policy

type Runner = sandbox.Runner

type Command = sandbox.Command

type Output = sandbox.Output

type Result = sandbox.Result

const TmpDir = sandbox.TmpDir

var errUnconfined = errors.New("sandbox.Direct refuses an unconfined policy")

func Init() {
	sandbox.Init()
}

func Supported(ctx context.Context) error {
	return sandbox.Supported(ctx)
}

func Direct() Runner {
	return directRunner{inner: sandbox.Direct()}
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
