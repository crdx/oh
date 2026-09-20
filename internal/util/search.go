package util

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"crdx.org/io/pkg/tool"
)

const MaxSearchBytes = 16 * 1024

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
}

func DescribeSearch(pattern string, path string, globPattern string) (string, string) {
	var qualifier string

	if path != "" && path != "." {
		qualifier = path
	}

	if globPattern != "" {
		if qualifier != "" {
			qualifier += " "
		}

		qualifier += globPattern
	}

	return pattern, qualifier
}

func SearchPath(call tool.ToolCall) string {
	qualifier := strings.TrimSpace(call.Qualifier())
	if qualifier == "" {
		return ""
	}

	return filepath.Base(qualifier)
}

func Walk(filesystem fs.FS, root string, visit func(path string, entry fs.DirEntry) error) error {
	return fs.WalkDir(filesystem, root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}

			return nil
		}

		if entry.IsDir() && skipDirs[entry.Name()] && path != root {
			return fs.SkipDir
		}

		if entry.Type()&fs.ModeSymlink != 0 && path != root {
			return nil
		}

		return visit(path, entry)
	})
}

func AppendSearchResult(results []string, returnedBytes int64, result string) ([]string, int64, bool) {
	separatorBytes := 0
	if len(results) > 0 {
		separatorBytes = 1
	}
	resultBytes := int64(separatorBytes + len(result))
	if returnedBytes+resultBytes > MaxSearchBytes {
		return results, returnedBytes, true
	}

	return append(results, result), returnedBytes + resultBytes, false
}

func ReportSearchResults(results []string, isTruncated bool) string {
	joinedResults := strings.Join(results, "\n")
	if isTruncated {
		if joinedResults != "" {
			joinedResults += "\n\n"
		}

		return fmt.Sprintf(
			"%s[stopped before matching output exceeded %s; narrow the search to see the rest]",
			joinedResults, FormatBytes(MaxSearchBytes, 3),
		)
	}
	if joinedResults == "" {
		return "(no matches)"
	}

	return joinedResults
}
