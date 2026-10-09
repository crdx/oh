package gitStatus

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"crdx.org/oh/internal/app/gitrepo"
	"crdx.org/oh/internal/app/segment"
	"crdx.org/oh/internal/app/segment/fit"
	"crdx.org/oh/internal/app/style"
)

var _ segment.Refresher = &state{}

const (
	defaultRate     = 5 * time.Second
	readingTimeout  = 10 * time.Second
	readingInterval = 100 * time.Millisecond
	treeMark        = "●"
	aheadMark       = "↑"
	behindMark      = "↓"
	conflictedLabel = "conflicted"
	failureLabel    = "status failed"
	branchHeader    = "# branch.ab "
)

var operations = []struct {
	marker string
	label  string
}{
	{marker: filepath.Join("rebase-apply", "applying"), label: "applying"},
	{marker: "rebase-merge", label: "rebasing"},
	{marker: "rebase-apply", label: "rebasing"},
	{marker: "MERGE_HEAD", label: "merging"},
	{marker: "CHERRY_PICK_HEAD", label: "cherry-picking"},
	{marker: "REVERT_HEAD", label: "reverting"},
}

type tree struct {
	isDirty      bool
	isConflicted bool
	operation    string
	ahead        int
	behind       int
}

type state struct {
	workspaceDir string
	rate         time.Duration

	mutex        sync.Mutex
	tree         tree
	isFailed     bool
	readAt       time.Time
	isRead       bool
	isReading    bool
	shouldRedraw bool
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
	self.mutex.Lock()
	defer self.mutex.Unlock()

	switch {
	case self.shouldRedraw || (!self.isReading && self.readAt.IsZero()):
		return phase.At
	case self.isReading:
		return phase.At.Add(readingInterval)
	default:
		return self.readAt.Add(self.rate)
	}
}

func (self *state) Render(context segment.Context) string {
	return self.Ladder(context)[0]
}

func (self *state) Ladder(segment.Context) []string {
	gitDir := gitrepo.Dir(self.workspaceDir)
	if gitDir == "" {
		self.mutex.Lock()
		self.tree, self.isFailed, self.isRead = tree{}, false, false
		self.readAt = time.Now()
		self.mutex.Unlock()

		return []string{""}
	}

	self.mutex.Lock()
	shouldRead := !self.isReading && (self.readAt.IsZero() || time.Since(self.readAt) >= self.rate)
	if shouldRead {
		self.isReading = true
	}
	current, isFailed, isRead := self.tree, self.isFailed, self.isRead
	self.shouldRedraw = false
	self.mutex.Unlock()

	if shouldRead {
		go self.read(gitDir)
	}

	switch {
	case isFailed:
		return fit.Ladder(style.Dim(failureLabel), "")
	case !isRead:
		return []string{""}
	default:
		return fit.Ladder(current.draw(), current.drawMark(), "")
	}
}

func (self *state) read(gitDir string) {
	current, err := readTree(self.workspaceDir, gitDir)

	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.tree = current
	self.isFailed = err != nil
	self.readAt = time.Now()
	self.isRead = true
	self.isReading = false
	self.shouldRedraw = true
}

func readTree(workspaceDir string, gitDir string) (tree, error) {
	readingContext, stop := context.WithTimeout(context.Background(), readingTimeout)
	defer stop()

	command := exec.CommandContext(
		readingContext,
		"git",
		"--no-optional-locks",
		"status",
		"--porcelain=v2",
		"--branch",
	)
	command.Dir = workspaceDir

	output, err := command.Output()
	if err != nil {
		return tree{}, fmt.Errorf("git status failed: %w", err)
	}

	current := parseStatus(output)
	current.operation = operationIn(gitDir)

	return current, nil
}

func parseStatus(output []byte) tree {
	var current tree

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		if counts, ok := strings.CutPrefix(line, branchHeader); ok {
			current.ahead, current.behind = parseDivergence(counts)

			continue
		}

		switch {
		case strings.HasPrefix(line, "u "):
			current.isConflicted = true
			current.isDirty = true
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "), strings.HasPrefix(line, "? "):
			current.isDirty = true
		}
	}

	return current
}

func parseDivergence(counts string) (int, int) {
	fields := strings.Fields(counts)
	if len(fields) != 2 {
		return 0, 0
	}

	ahead, _ := strconv.Atoi(strings.TrimPrefix(fields[0], "+"))
	behind, _ := strconv.Atoi(strings.TrimPrefix(fields[1], "-"))

	return ahead, behind
}

func operationIn(gitDir string) string {
	for _, operation := range operations {
		if _, err := os.Stat(filepath.Join(gitDir, operation.marker)); err == nil {
			return operation.label
		}
	}

	return ""
}

func (self tree) isTroubled() bool {
	return self.isConflicted || self.operation != ""
}

func (self tree) drawMark() string {
	switch {
	case self.isTroubled():
		return style.TroubledTree(treeMark)
	case self.isDirty:
		return style.DirtyTree(treeMark)
	default:
		return style.CleanTree(treeMark)
	}
}

func (self tree) draw() string {
	parts := []string{self.drawMark()}

	switch {
	case self.operation != "":
		parts = append(parts, style.TroubledTree(self.operation))
	case self.isConflicted:
		parts = append(parts, style.TroubledTree(conflictedLabel))
	}

	if self.ahead > 0 {
		parts = append(parts, style.Divergence(aheadMark+strconv.Itoa(self.ahead)))
	}

	if self.behind > 0 {
		parts = append(parts, style.Divergence(behindMark+strconv.Itoa(self.behind)))
	}

	return strings.Join(parts, " ")
}
