package skill

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

const BuiltinName = "oh"

//go:embed skills/oh/SKILL.md
var builtinSkill []byte

func Materialise(directory string) error {
	skillDirectory := filepath.Join(directory, BuiltinName)
	path := filepath.Join(skillDirectory, filename)

	if isCurrent(path, builtinSkill) {
		return nil
	}

	if err := os.MkdirAll(skillDirectory, 0o700); err != nil {
		return fmt.Errorf("could not write the built-in skill: %w", err)
	}
	if err := os.WriteFile(path, builtinSkill, 0o600); err != nil {
		return fmt.Errorf("could not write the built-in skill: %w", err)
	}

	return nil
}

func isCurrent(path string, skillBody []byte) bool {
	body, err := os.ReadFile(path) //nolint:gosec // the path is oh's own state directory
	if err != nil {
		return false
	}

	return sha256.Sum256(body) == sha256.Sum256(skillBody)
}
