package tool

import "errors"

var ErrCommandFailed = errors.New("the command failed")

type commandError string

func (self commandError) Error() string { return string(self) }

func (self commandError) Is(target error) bool { return target == ErrCommandFailed }

func CommandFailed(reason string) error {
	return commandError(reason)
}
