package tty

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
)

func ReadLine(ctx context.Context, input *os.File) (string, error) {
	reader := NewReader(input)
	defer reader.Close()

	forgetContext := context.AfterFunc(ctx, reader.Stop)
	defer forgetContext()

	var line strings.Builder
	var character [1]byte

	for {
		if cancellation := ctx.Err(); cancellation != nil {
			return "", cancellation
		}

		read, err := reader.Read(character[:])

		if read == 1 {
			if character[0] == '\n' {
				return strings.TrimSuffix(line.String(), "\r"), nil
			}

			line.WriteByte(character[0])
		}

		if errors.Is(err, io.EOF) {
			if cancellation := ctx.Err(); cancellation != nil {
				return "", cancellation
			}

			return line.String(), err
		}

		if err != nil {
			return "", err
		}
	}
}
