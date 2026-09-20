package shell

import (
	"os"
	"strings"

	"crdx.org/oh/internal/util/pathutil"
)

type Paths struct {
	Deny  []string `toml:"deny"`
	Read  []string `toml:"read"`
	Write []string `toml:"write"`
	Exec  []string `toml:"exec"`
	Path  []string `toml:"path"`
	Home  []string `toml:"home"`
}

func ShellPath(pathDirectories []string) string {
	return strings.Join(append([]string{os.Getenv("PATH")}, pathDirectories...), string(os.PathListSeparator))
}

func HomeRelativePath(path string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	return pathutil.RelativeTo(home, path)
}
