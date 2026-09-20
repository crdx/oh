package gitBranch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"crdx.org/io/internal/app/segment"
	"crdx.org/io/internal/app/style"
)

var _ segment.Refresher = &state{}

const (
	defaultRate    = 5 * time.Second
	shortHashWidth = 7
)

type state struct {
	workspaceDir string
	rate         time.Duration
	readAt       time.Time
	name         string
}

func New(workspaceDir string) segment.Factory {
	return func(options segment.Options) (segment.Segment, error) {
		var args struct {
			Rate time.Duration `toml:"rate"`
		}

		if err := options.Read(&args); err != nil {
			return nil, err
		}

		if args.Rate < 0 {
			return nil, fmt.Errorf("rate must not be negative (got %s)", args.Rate)
		}

		if args.Rate == 0 {
			args.Rate = defaultRate
		}

		return &state{workspaceDir: workspaceDir, rate: args.Rate}, nil
	}
}

func (self *state) NextRefresh(phase segment.Phase) time.Time {
	if self.readAt.IsZero() {
		return phase.At
	}

	return self.readAt.Add(self.rate)
}

func (self *state) Render(segment.Context) string {
	if self.readAt.IsZero() || time.Since(self.readAt) >= self.rate {
		self.name = branchOf(self.workspaceDir)
		self.readAt = time.Now()
	}

	return style.Subtle(self.name)
}

func branchOf(workspaceDir string) string {
	gitDir := gitDirOf(workspaceDir)
	if gitDir == "" {
		return ""
	}

	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD")) //nolint:gosec // HEAD of the workspace
	if err != nil {
		return ""
	}

	text := strings.TrimSpace(string(head))

	if reference, ok := strings.CutPrefix(text, "ref: "); ok {
		return strings.TrimPrefix(reference, "refs/heads/")
	}

	if len(text) >= shortHashWidth {
		return text[:shortHashWidth]
	}

	return ""
}

func gitDirOf(workspaceDir string) string {
	gitPath := filepath.Join(workspaceDir, ".git")

	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}

	if info.IsDir() {
		return gitPath
	}

	pointer, err := os.ReadFile(gitPath) //nolint:gosec // the .git of the workspace
	if err != nil {
		return ""
	}

	elsewhere, ok := strings.CutPrefix(strings.TrimSpace(string(pointer)), "gitdir: ")
	if !ok {
		return ""
	}

	if filepath.IsAbs(elsewhere) {
		return elsewhere
	}

	return filepath.Join(workspaceDir, elsewhere)
}
