package console

import (
	"io"
	"os"
)

type Output struct {
	Screen  io.Writer
	Failure io.Writer
}

func Standard() Output {
	return Output{Screen: os.Stdout, Failure: os.Stderr}
}
