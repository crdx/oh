package render

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"crdx.org/duckopt/v2"
	"crdx.org/oh/internal/app/scene"
	"crdx.org/oh/internal/app/vt"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/agent"
)

const Usage = `oh --ctl render — draw a journal, scene, or stream

Usage:
    $0 --ctl render [options] <file>

Options:
    -w, --width <columns>    Draw this many columns wide
    -H, --height <rows>      Draw a scene this many rows high
    -P, --pass <label>       Take this pass of a stream that holds several
    -h, --help               Show this help

The file decides what is drawn. A .jsonl file is a journal, a stored session's or a replay's, and
is drawn as a resumed session draws it, leaving out any pictures. A .toml file is a scene: oh runs
in a terminal of its own, the scene's keys are typed into it, and the screen is taken once it
settles. Any other file is a stream of terminal output, raw or with its escapes written out as \e,
and is played and settled.

The drawing goes to stdout. A journal is 100 columns wide and a scene 100 by 24 unless told
otherwise. A stream is drawn at the width given, which it needs.
`

const (
	defaultColumns = 100
	defaultRows    = 24
	passMarker     = "=== "
)

type inputOpts struct {
	IsControlling bool   `docopt:"--ctl"`
	Render        bool   `docopt:"render"`
	Width         string `docopt:"--width"`
	Height        string `docopt:"--height"`
	Pass          string `docopt:"--pass"`
	File          string `docopt:"<file>"`
}

type DrawJournal func(events []agent.Event, columns int) (string, error)

func Run(drawJournal DrawJournal) error {
	options := duckopt.MustBind[inputOpts](Usage, "$0")

	columns, err := positive(options.Width, "--width", defaultColumns)
	if err != nil {
		return err
	}
	rows, err := positive(options.Height, "--height", defaultRows)
	if err != nil {
		return err
	}

	drawing, err := draw(options.File, options.Pass, options.Width != "", columns, rows, drawJournal)
	if err != nil {
		return err
	}

	_, err = io.WriteString(os.Stdout, drawing)
	return err
}

func draw(
	path string,
	pass string,
	isWidthGiven bool,
	columns int,
	rows int,
	drawJournal DrawJournal,
) (string, error) {
	switch filepath.Ext(path) {
	case ".jsonl":
		if pass != "" {
			return "", errors.New("--pass takes a stream, not a journal")
		}
		events, err := ReadJournal(path)
		if err != nil {
			return "", err
		}
		return drawJournal(events, columns)
	case ".toml":
		if pass != "" {
			return "", errors.New("--pass takes a stream, not a scene")
		}
		return scene.Render(context.Background(), path, columns, rows)
	default:
		if !isWidthGiven {
			return "", errors.New("a stream needs --width, the width it was drawn at")
		}
		return Settle(path, pass, columns)
	}
}

func positive(value string, name string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}

	number, err := strconv.Atoi(value)
	if err != nil || number < 1 {
		return 0, fmt.Errorf("%s takes a whole number above zero, not %q", name, value)
	}

	return number, nil
}

func ReadJournal(path string) ([]agent.Event, error) {
	file, err := os.Open(path) //nolint:gosec // the person names the journal to draw
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var events []agent.Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 64<<20)
	for number := 1; scanner.Scan(); number++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var record struct {
			Event *agent.Event `json:"event"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("%s: line %d: %w", path, number, err)
		}
		if record.Event != nil {
			events = append(events, *record.Event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("%s holds no events", path)
	}

	return events, nil
}

func Settle(path string, pass string, columns int) (string, error) {
	contents, err := os.ReadFile(path) //nolint:gosec // the person names the stream to draw
	if err != nil {
		return "", err
	}

	stream, err := choosePass(string(contents), pass)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if !strings.Contains(stream, "\x1b") && strings.Contains(stream, `\e`) {
		stream = strutil.RestoreEscapes(stream)
	}

	screen := vt.New(columns, 0)
	if err := screen.Play(stream); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}

	return strings.Join(screen.Styled(), "\n") + "\n", nil
}

func choosePass(contents string, pass string) (string, error) {
	labels := []string{}
	bodies := map[string]string{}
	current := ""
	isInPass := false
	var body strings.Builder

	finish := func() {
		if isInPass {
			bodies[current] = strings.TrimRight(body.String(), "\n")
		}
		body.Reset()
	}

	for line := range strings.SplitSeq(contents, "\n") {
		if strings.HasPrefix(line, passMarker) && strings.HasSuffix(line, " ===") {
			finish()
			current = strings.TrimSuffix(strings.TrimPrefix(line, passMarker), " ===")
			labels = append(labels, current)
			isInPass = true
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	finish()

	if len(labels) == 0 {
		if pass != "" {
			return "", fmt.Errorf("it holds no passes, so there is no %q", pass)
		}
		return contents, nil
	}
	if pass == "" {
		if len(labels) == 1 {
			return bodies[labels[0]], nil
		}
		return "", fmt.Errorf("it holds %d passes, so name one with --pass: %s", len(labels), strings.Join(labels, ", "))
	}

	passBody, isFound := bodies[pass]
	if !isFound {
		return "", fmt.Errorf("it holds no pass %q, only: %s", pass, strings.Join(labels, ", "))
	}

	return passBody, nil
}
