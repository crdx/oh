package frontmatter

import (
	"bytes"
	"errors"
)

const (
	byteOrderMark = "\xef\xbb\xbf"
	delimiter     = "---"
)

var ErrUnterminated = errors.New("unterminated YAML frontmatter")

func Split(data []byte) ([]byte, []byte, bool, error) {
	data = bytes.TrimPrefix(data, []byte(byteOrderMark))
	lines := bytes.Split(data, []byte("\n"))
	if string(bytes.TrimSpace(lines[0])) != delimiter {
		return nil, data, false, nil
	}

	for i := 1; i < len(lines); i++ {
		if string(bytes.TrimSpace(lines[i])) == delimiter {
			header := bytes.Join(lines[1:i], []byte("\n"))
			body := bytes.Join(lines[i+1:], []byte("\n"))
			return header, body, true, nil
		}
	}

	return nil, data, false, ErrUnterminated
}
