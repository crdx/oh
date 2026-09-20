package toolresult

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/toolresult"
)

const (
	requestFlag = "--tool-result"
	pagerFlag   = "--pager"
)

type Request struct {
	URL        string
	ShouldPage bool
}

type pager func(string) error

func ParseRequest(arguments []string) (Request, bool, error) {
	if len(arguments) == 0 || arguments[0] != requestFlag {
		return Request{}, false, nil
	}
	if len(arguments) < 2 || strings.TrimSpace(arguments[1]) == "" || strings.HasPrefix(arguments[1], "--") {
		return Request{}, true, fmt.Errorf("%s requires a URL", requestFlag)
	}

	request := Request{URL: arguments[1]}
	if len(arguments) == 2 {
		return request, true, nil
	}
	if len(arguments) == 3 && arguments[2] == pagerFlag {
		request.ShouldPage = true
		return request, true, nil
	}

	return Request{}, true, fmt.Errorf("%s accepts only %s beside its URL", requestFlag, pagerFlag)
}

func Show(url string, shouldPage bool) error {
	return show(url, shouldPage, location.GetSessionsDir(), os.Stdout, page)
}

func show(url string, shouldPage bool, directory string, output io.Writer, openPager pager) error {
	exchange, err := toolresult.Read(directory, url)
	if err != nil {
		return err
	}

	result := render(exchange, getColumns(output))
	if shouldPage {
		return openPager(result)
	}

	_, err = fmt.Fprint(output, result)
	return err
}

func getColumns(output io.Writer) int {
	file, isFile := output.(*os.File)
	if !isFile {
		return 0
	}
	columns, _, err := term.GetSize(int(file.Fd()))
	if err != nil {
		return 0
	}
	return columns
}

func page(text string) error {
	command := exec.CommandContext(context.Background(), "less", "-R", "-+F")
	command.Stdin = strings.NewReader(text)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("pager failed: %w", err)
	}
	return nil
}
