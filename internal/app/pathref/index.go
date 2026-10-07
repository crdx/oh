package pathref

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	MaxPaths       = 200_000
	staleAfter     = 5 * time.Second
	listingTimeout = 10 * time.Second
)

type Listing struct {
	Paths       []string
	Err         error
	IsTruncated bool
	foldedPaths []string
	depths      []int
	every       []int
}

func NewListing(files []string) *Listing {
	directories := map[string]bool{}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if file == "" {
			continue
		}
		paths = append(paths, file)
		if IsDirectory(file) {
			directories[strings.TrimSuffix(file, "/")] = true
		}
		for directory := path.Dir(strings.TrimSuffix(file, "/")); directory != "." && directory != "/" && !directories[directory]; directory = path.Dir(directory) {
			directories[directory] = true
			paths = append(paths, directory+"/")
		}
	}

	listing := &Listing{
		Paths:       paths,
		foldedPaths: make([]string, len(paths)),
		depths:      make([]int, len(paths)),
		every:       make([]int, len(paths)),
	}
	for i, entry := range paths {
		listing.foldedPaths[i] = fold(entry)
		listing.depths[i] = strings.Count(strings.TrimSuffix(entry, "/"), "/")
		listing.every[i] = i
	}

	return listing
}

type Lister func(ctx context.Context, directory string, excludedNames []string) ([]string, bool, error)

type Index struct {
	directory        string
	getExcludedNames func() []string
	list             Lister
	now              func() time.Time

	mutex     sync.Mutex
	listing   *Listing
	listedAt  time.Time
	isListing bool
}

func NewIndex(directory string, getExcludedNames func() []string) *Index {
	return NewIndexWith(directory, getExcludedNames, ListFiles, time.Now)
}

func NewIndexWith(directory string, getExcludedNames func() []string, list Lister, now func() time.Time) *Index {
	return &Index{
		directory:        directory,
		getExcludedNames: getExcludedNames,
		list:             list,
		now:              now,
	}
}

func (self *Index) Listing() *Listing {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.listing
}

func (self *Index) Refresh(announceChange func()) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if self.isListing || self.listing != nil && self.now().Sub(self.listedAt) < staleAfter {
		return
	}

	self.isListing = true
	var excludedNames []string
	if self.getExcludedNames != nil {
		excludedNames = self.getExcludedNames()
	}

	go self.refresh(excludedNames, announceChange)
}

func (self *Index) refresh(excludedNames []string, announceChange func()) {
	ctx, cancel := context.WithTimeout(context.Background(), listingTimeout)
	defer cancel()

	files, isTruncated, err := self.list(ctx, self.directory, excludedNames)
	listing := NewListing(files)
	listing.IsTruncated = isTruncated
	listing.Err = err

	self.mutex.Lock()
	self.listing = listing
	self.listedAt = self.now()
	self.isListing = false
	self.mutex.Unlock()

	announceChange()
}

func ListFiles(ctx context.Context, directory string, excludedNames []string) ([]string, bool, error) {
	return listFiles(ctx, directory, excludedNames, false)
}

func ListIgnoredFiles(ctx context.Context, directory string, excludedNames []string) ([]string, bool, error) {
	ordinaryFiles, isTruncated, err := ListFiles(ctx, directory, excludedNames)
	if err != nil {
		return nil, isTruncated, err
	}
	if isTruncated {
		return nil, true, errors.New("too many ordinary paths to identify ignored paths")
	}

	allFiles, allTruncated, err := listFiles(ctx, directory, excludedNames, true)
	if err != nil {
		return nil, allTruncated, err
	}
	ordinaryPaths := make(map[string]bool, len(ordinaryFiles))
	for _, file := range ordinaryFiles {
		ordinaryPaths[file] = true
	}
	ignoredFiles := make([]string, 0, len(allFiles))
	for _, file := range allFiles {
		if !ordinaryPaths[file] {
			ignoredFiles = append(ignoredFiles, file)
		}
	}
	return ignoredFiles, allTruncated, nil
}

func listFiles(ctx context.Context, directory string, excludedNames []string, isIncludingIgnored bool) ([]string, bool, error) {
	arguments := []string{
		"--no-config",
		"--files",
		"--hidden",
		"--no-require-git",
		"--glob=!.git/**",
		"--no-messages",
		"--null",
	}
	if isIncludingIgnored {
		arguments = append(arguments, "--no-ignore")
	}
	for _, pattern := range excludedNames {
		arguments = append(arguments, "--glob=!"+pattern)
	}

	listContext, stopListing := context.WithCancel(ctx)
	defer stopListing()

	command := exec.CommandContext(listContext, "rg", arguments...)
	command.Dir = directory

	var stderr bytes.Buffer
	command.Stderr = &stderr

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, false, fmt.Errorf("failed to read ripgrep output: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, false, fmt.Errorf("failed to start ripgrep: %w", err)
	}

	var files []string
	isTruncated := false
	scanner := bufio.NewScanner(stdout)
	scanner.Split(splitAtNull)
	for scanner.Scan() {
		if len(files) == MaxPaths {
			isTruncated = true
			stopListing()
			break
		}
		files = append(files, strings.TrimPrefix(scanner.Text(), "./"))
	}
	scanErr := scanner.Err()

	waitErr := command.Wait()
	switch {
	case isTruncated:
		return files, true, nil
	case scanErr != nil:
		return files, true, fmt.Errorf("failed to read ripgrep output: %w", scanErr)
	case ctx.Err() != nil:
		return files, true, fmt.Errorf("listing the workspace took longer than %s", listingTimeout)
	case waitErr != nil:
		var exitError *exec.ExitError
		message := strings.TrimSpace(stderr.String())
		if errors.As(waitErr, &exitError) && exitError.ExitCode() == 1 && message == "" {
			return files, false, nil
		}
		if message != "" {
			return files, false, errors.New(message)
		}
		return files, false, fmt.Errorf("ripgrep failed: %w", waitErr)
	}

	return files, false, nil
}

func splitAtNull(data []byte, isAtEOF bool) (int, []byte, error) {
	if at := bytes.IndexByte(data, 0); at >= 0 {
		return at + 1, data[:at], nil
	}
	if isAtEOF && len(data) > 0 {
		return len(data), data, nil
	}

	return 0, nil, nil
}
