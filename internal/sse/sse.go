package sse

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"time"

	"crdx.org/oh/internal/transient"
)

var ErrTruncated error = truncation{}

type truncation struct{}

func (truncation) Error() string { return "the stream ended before the response did" }

func (truncation) Retriable() bool { return true }

func (truncation) RetryAfter() time.Duration { return 0 }

type Step func(payload string) (bool, error)

func Read(body io.Reader, step Step) error {
	reader := bufio.NewReader(body)

	var data strings.Builder

	for {
		line, err := reader.ReadString('\n')
		eof := errors.Is(err, io.EOF)

		if err != nil && !eof {
			return transient.Wrap(err)
		}

		text := strings.TrimRight(line, "\r\n")

		if field, hasData := strings.CutPrefix(text, "data:"); hasData {
			data.WriteString(strings.TrimSpace(field))
		}

		if text == "" || eof {
			payload := data.String()
			data.Reset()

			if payload != "" {
				switch done, err := step(payload); {
				case err != nil:
					return err
				case done:
					return nil
				}
			}
		}

		if eof {
			return ErrTruncated
		}
	}
}
