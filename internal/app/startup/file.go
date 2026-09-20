package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const messageIntroduction = "The following files have been made available in this session's scratch directory:"

type InitialFile struct {
	SourcePath  string
	DisplayName string
}

func PrepareInitialFiles(files []InitialFile, scratchDirectory string) (string, error) {
	destinationPaths := make([]string, 0, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file.SourcePath)
		if err != nil {
			return "", fmt.Errorf("could not read the initial file: %w", err)
		}

		displayName := file.DisplayName
		if displayName == "" {
			displayName = filepath.Base(file.SourcePath)
		}

		destinationPath := filepath.Join(scratchDirectory, displayName)
		if err := os.WriteFile(destinationPath, content, 0o600); err != nil { //nolint:gosec // a generated scratch directory receives the display name
			return "", fmt.Errorf("could not copy the initial file: %w", err)
		}
		destinationPaths = append(destinationPaths, destinationPath)
	}

	listedPaths := make([]string, len(destinationPaths))
	for i, destinationPath := range destinationPaths {
		listedPaths[i] = "- " + destinationPath
	}
	return messageIntroduction + "\n\n" + strings.Join(listedPaths, "\n"), nil
}
