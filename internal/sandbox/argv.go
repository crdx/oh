package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

type ArgvRunner interface {
	RunArgv(ctx context.Context, directory string, arguments []string, policy Policy) (Result, error)
	StartArgv(
		ctx context.Context,
		directory string,
		arguments []string,
		policy Policy,
		output Output,
	) (Command, error)
}

func DirectArgv() ArgvRunner {
	return runner{spawn: spawnAlone}
}

func UnconfinedArgv() ArgvRunner {
	return runner{isUnconfined: true}
}

func (self runner) RunArgv(
	ctx context.Context,
	directory string,
	arguments []string,
	policy Policy,
) (Result, error) {
	runningCommand, err := self.StartArgv(ctx, directory, arguments, policy, &boundedBuffer{})
	if err != nil {
		return Result{}, err
	}
	return runningCommand.Wait()
}

func (self runner) StartArgv(
	ctx context.Context,
	directory string,
	arguments []string,
	policy Policy,
	output Output,
) (Command, error) {
	if policy.Yolo && !self.isUnconfined {
		return nil, errors.New("argv commands cannot run unconfined")
	}
	if len(arguments) == 0 || !filepath.IsAbs(arguments[0]) {
		return nil, errors.New("argv commands need an absolute executable path")
	}
	if err := ensureSane(directory, arguments[0]); err != nil {
		return nil, err
	}
	for _, argument := range arguments[1:] {
		if err := ensureSane(directory, argument); err != nil {
			return nil, err
		}
	}

	if self.isUnconfined {
		policy.Yolo = true
		return startOnHost(ctx, directory, arguments, policy, output)
	}

	var err error
	policy, err = preparePolicy(ctx, policy, false)
	if err != nil {
		return nil, err
	}

	encodedPolicy, err := json.Marshal(policy)
	if err != nil {
		return nil, fmt.Errorf("could not write the policy: %w", err)
	}
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		return nil, fmt.Errorf("could not write the command arguments: %w", err)
	}
	environment := commandEnvironment(policy, string(encodedPolicy), envArguments, string(encodedArguments))
	return self.start(ctx, directory, environment, policy, output)
}

func preparePolicy(ctx context.Context, policy Policy, needsShell bool) (Policy, error) {
	if needsShell {
		if err := validate(ctx, policy); err != nil {
			return Policy{}, err
		}
	} else if err := validatePolicy(ctx, policy); err != nil {
		return Policy{}, err
	}
	if len(policy.Deny) > 0 && policy.DenyPaths == nil {
		denyPaths, err := policy.DiscoverDenyPaths()
		if err != nil {
			return Policy{}, err
		}
		policy.DenyPaths = append([]string{}, denyPaths...)
	}
	return policy, nil
}
