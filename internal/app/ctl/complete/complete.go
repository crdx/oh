package complete

import (
	"fmt"
	"io"
	"strings"

	"crdx.org/oh/internal/app/ctl/analyse"
	"crdx.org/oh/internal/app/ctl/check"
	"crdx.org/oh/internal/app/ctl/gc"
	"crdx.org/oh/internal/app/ctl/migrate"
	"crdx.org/oh/internal/app/ctl/models"
	"crdx.org/oh/internal/app/ctl/regenerate"
	"crdx.org/oh/internal/app/ctl/render"
	"crdx.org/oh/internal/app/ctl/sessions"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/session"
)

const Flag = "--complete"

const (
	kindCommand   = "command"
	kindDeletable = "deletable"
	kindOption    = "option"
	kindSession   = "session"
)

var commands = []string{"analyse", "check", "gc", "migrate", "models", "regenerate", "render", "sessions"}

var optionsByCommand = map[string][]string{
	"analyse":    strutil.OptionNames(analyse.Usage),
	"check":      strutil.OptionNames(check.Usage),
	"gc":         strutil.OptionNames(gc.Usage),
	"migrate":    strutil.OptionNames(migrate.Usage),
	"models":     strutil.OptionNames(models.Usage),
	"regenerate": strutil.OptionNames(regenerate.Usage),
	"render":     strutil.OptionNames(render.Usage),
	"sessions":   strutil.OptionNames(sessions.Usage),
}

func Write(out io.Writer, args []string) bool {
	kind, word, command, isWanted := request(args)
	if !isWanted {
		return false
	}

	for _, completion := range completions(kind, word, command) {
		_, _ = fmt.Fprintln(out, completion)
	}

	return true
}

func request(args []string) (string, string, string, bool) {
	if len(args) < 2 || args[0] != Flag {
		return "", "", "", false
	}

	var word string
	if len(args) > 2 {
		word = args[2]
	}

	var command string
	if len(args) > 3 {
		command = args[3]
	}

	return args[1], word, command, true
}

func completions(kind string, word string, command string) []string {
	switch kind {
	case kindCommand:
		return withPrefix(word, commands)
	case kindDeletable:
		return withPrefix(word, deletableNames())
	case kindOption:
		return withPrefix(word, optionsByCommand[command])
	case kindSession:
		return withPrefix(word, storedNames())
	default:
		return nil
	}
}

func withPrefix(word string, candidates []string) []string {
	var matches []string

	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, word) {
			matches = append(matches, candidate)
		}
	}

	return matches
}

func deletableNames() []string {
	names, err := session.AllNames(location.GetSessionsDir())
	if err != nil {
		return nil
	}

	return names
}

func storedNames() []string {
	names, err := session.StoredNames(location.GetSessionsDir())
	if err != nil {
		return nil
	}

	return names
}
