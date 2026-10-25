package toolresult

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/taskrecord"
	"crdx.org/oh/internal/toolresult"
)

const (
	requestFlag = "--view"
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
	result, err := view(directory, url)
	if err != nil {
		return err
	}

	if shouldPage {
		return openPager(result)
	}

	_, err = fmt.Fprint(output, result)
	return err
}

func view(directory string, url string) (string, error) {
	if toolresult.IsTask(url) {
		reference, err := toolresult.ParseTask(url)
		if err != nil {
			return "", err
		}
		snapshot, err := taskrecord.Read(directory, reference)
		if err != nil {
			return "", err
		}
		return renderTask(snapshot), nil
	}

	exchange, err := toolresult.Read(directory, url)
	if err != nil {
		return "", err
	}
	return render(exchange), nil
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
