package slash

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"crdx.org/oh/internal/app/dropdown"
	"crdx.org/oh/internal/app/pathref"
	"crdx.org/oh/internal/app/trigger"
	"crdx.org/oh/internal/util/pathutil"
)

const (
	pathSourceSymbol    = rune(-1)
	pathListingText     = "listing paths…"
	noMatchingPathsText = "no matching paths"
	pathListingFailure  = "could not list paths: "
)

type PathSource struct {
	directory   string
	getRegistry func() Registry
	matcher     pathref.Matcher
	indexes     map[string]*pathref.Index
	announce    func()
}

func NewPathSource(directory string, getRegistry func() Registry) *PathSource {
	return &PathSource{
		directory:   directory,
		getRegistry: getRegistry,
	}
}

func (self *PathSource) Symbol() rune {
	return pathSourceSymbol
}

func (self *PathSource) Elision() dropdown.Elision {
	return dropdown.ElideStart
}

func (self *PathSource) Find(runes []rune, cursor int) (trigger.Word, bool) {
	return self.getRegistry().pathArgumentWord(runes, cursor)
}

func (self *PathSource) Open(announceChange func()) {
	self.indexes = make(map[string]*pathref.Index)
	self.announce = announceChange
}

func (self *PathSource) Results(word trigger.Word, limit int) trigger.Results {
	directory, prefix, partial, err := pathTarget(self.directory, word.Query)
	if err != nil {
		return trigger.Results{Placeholder: pathListingFailure + err.Error()}
	}

	listing := self.listing(directory)
	if listing == nil {
		return trigger.Results{Placeholder: pathListingText}
	}
	if listing.Err != nil {
		return trigger.Results{Placeholder: pathListingFailure + listing.Err.Error()}
	}

	paths, total := self.matcher.Match(listing, partial, limit)
	items := make([]trigger.Result, len(paths))
	for i, path := range paths {
		path = prefix + path
		isOpenEnded := pathref.IsDirectory(path)
		items[i] = trigger.Result{
			Label:       path,
			Text:        trigger.QuotedText(path, word.IsQuoted, isOpenEnded),
			IsOpenEnded: isOpenEnded,
		}
	}

	return trigger.Results{Items: items, Total: total, Placeholder: noMatchingPathsText}
}

func (self *PathSource) listing(directory string) *pathref.Listing {
	index, isFound := self.indexes[directory]
	if !isFound {
		index = pathref.NewIndexWith(directory, nil, listPathDirectory, time.Now)
		self.indexes[directory] = index
	}
	index.Refresh(self.announce)
	return index.Listing()
}

func listPathDirectory(ctx context.Context, directory string, _ []string) ([]string, bool, error) {
	directoryEntries, err := os.ReadDir(directory)
	if err != nil {
		return nil, false, err
	}

	paths := make([]string, 0, min(len(directoryEntries), pathref.MaxPaths))
	for _, entry := range directoryEntries {
		if err := ctx.Err(); err != nil {
			return paths, true, err
		}
		if len(paths) == pathref.MaxPaths {
			return paths, true, nil
		}

		name := entry.Name()
		isDirectory := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, statErr := os.Stat(filepath.Join(directory, name)); statErr == nil {
				isDirectory = info.IsDir()
			}
		}
		if isDirectory {
			name += string(os.PathSeparator)
		}
		paths = append(paths, name)
	}

	return paths, false, nil
}

func pathTarget(baseDirectory string, query string) (string, string, string, error) {
	prefix, partial := filepath.Split(query)
	expandedPrefix, err := pathutil.Expand(prefix)
	if err != nil {
		return "", "", "", err
	}

	directory := expandedPrefix
	if directory == "" {
		directory = baseDirectory
	} else if !filepath.IsAbs(directory) {
		directory = filepath.Join(baseDirectory, directory)
	}

	return filepath.Clean(directory), prefix, partial, nil
}

func (self Registry) pathArgumentWord(runes []rune, cursor int) (trigger.Word, bool) {
	if len(runes) == 0 || cursor < 0 || cursor > len(runes) || slices.ContainsFunc(runes, isPathCompletionControl) {
		return trigger.Word{}, false
	}

	nameEnd := indexSpace(runes, 0)
	if nameEnd == len(runes) {
		return trigger.Word{}, false
	}
	name := string(runes[:nameEnd])
	set, isFound := self.getSet(name)
	if !isFound {
		return trigger.Word{}, false
	}
	command, isFound := set.commands[strings.TrimPrefix(name, set.prefix)]
	if !isFound || command.isPathArgumentAfter == nil {
		return trigger.Word{}, false
	}

	argumentStart := skipSpaces(runes, nameEnd)
	argumentEnd := indexSpace(runes, argumentStart)
	if argumentStart == argumentEnd || !command.isPathArgumentAfter(string(runes[argumentStart:argumentEnd])) {
		return trigger.Word{}, false
	}
	if argumentEnd == len(runes) {
		return trigger.Word{}, false
	}

	pathStart := skipSpaces(runes, argumentEnd)
	if cursor < pathStart {
		return trigger.Word{}, false
	}

	return quotedWordAt(runes, pathStart, cursor), true
}

func isPathCompletionControl(value rune) bool {
	return value == '\t' || value == '\r' || value == '\n'
}

func indexSpace(runes []rune, start int) int {
	for i := start; i < len(runes); i++ {
		if unicode.IsSpace(runes[i]) {
			return i
		}
	}
	return len(runes)
}

func skipSpaces(runes []rune, start int) int {
	for start < len(runes) && unicode.IsSpace(runes[start]) {
		start++
	}
	return start
}
