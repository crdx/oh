package xdg

import (
	"os"
	"path/filepath"
)

func StatePath(parts ...string) string {
	return resolve("XDG_STATE_HOME", filepath.Join(".local", "state"), parts)
}

func ConfigPath(parts ...string) string {
	return resolve("XDG_CONFIG_HOME", ".config", parts)
}

func resolve(variable string, fallback string, parts []string) string {
	root := os.Getenv(variable)

	if !filepath.IsAbs(root) {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}

		root = filepath.Join(home, fallback)
	}

	return filepath.Join(append([]string{root}, parts...)...)
}
