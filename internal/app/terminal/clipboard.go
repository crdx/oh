package terminal

import (
	"encoding/base64"
	"fmt"
	"io"
)

func Copy(writer io.Writer, text string) error {
	encodedText := base64.StdEncoding.EncodeToString([]byte(text))
	_, err := fmt.Fprintf(writer, "\x1b]52;c;%s\x07", encodedText)
	return err
}
