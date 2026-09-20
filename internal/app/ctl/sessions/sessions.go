package sessions

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"crdx.org/duckopt/v2"

	"crdx.org/io/internal/app/ctl/console"
	"crdx.org/io/internal/app/location"
	"crdx.org/io/internal/app/segment/fastMode"
	"crdx.org/io/internal/app/sessions/picker"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/table"
	"crdx.org/io/internal/app/width"
	"crdx.org/io/internal/app/work"
	"crdx.org/io/internal/util"
	"crdx.org/io/internal/util/strutil"
	"crdx.org/io/pkg/session"

	ohSessions "crdx.org/io/internal/app/sessions"
)

const usage = `oh --ctl sessions — list the stored sessions

Usage:
    $0 --ctl sessions [options] [<filter>]

Options:
    -j, --json               Write the listing as JSON
    -w, --workspace <dir>    List only the sessions of one workspace
    -r, --running            List only the sessions that are running
    -a, --archived           List the archived sessions rather than the stored ones
    -h, --help               Show this help

The newest 50 sessions are listed, unless a filter of more than three characters is given, which
lists every session it matches.
`

const (
	runningStatus  = "running"
	endedStatus    = "ended"
	archivedStatus = "archived"
	titleColumn    = 40
	listLimit      = 50
	shortFilter    = 3
)

type inputOpts struct {
	IsControlling bool   `docopt:"--ctl"`
	Sessions      bool   `docopt:"sessions"`
	JSON          bool   `docopt:"--json"`
	Workspace     string `docopt:"--workspace"`
	Running       bool   `docopt:"--running"`
	Archived      bool   `docopt:"--archived"`
	Filter        string `docopt:"<filter>"`
}

type Listing struct {
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	IsRunning    bool      `json:"isRunning"`
	IsArchived   bool      `json:"isArchived"`
	IsFast       bool      `json:"isFast"`
	Title        string    `json:"title"`
	WorkspaceDir string    `json:"workspaceDir"`
	ScratchDir   string    `json:"scratchDir"`
	SessionDir   string    `json:"sessionDir"`
	Model        string    `json:"model"`
	Effort       string    `json:"effort"`
	Messages     int       `json:"messages"`
	StartedAt    time.Time `json:"started"`
	TouchedAt    time.Time `json:"touched"`
}

func Run() error {
	return run(duckopt.MustBind[inputOpts](usage, "$0"), console.Standard())
}

func run(options *inputOpts, output console.Output) error {
	directory := location.GetSessionsDir()
	storedSessions, unlistedCount, err := loadRequestedSessions(directory, options, output.Failure)
	if err != nil {
		if migrationError := ohSessions.ValidateFormats(directory); migrationError != nil {
			return migrationError
		}
		return err
	}

	if options.Workspace != "" {
		workspaceDir, err := filepath.Abs(options.Workspace)
		if err != nil {
			return fmt.Errorf("could not resolve the workspace path: %w", err)
		}
		storedSessions = ohSessions.InWorkspace(storedSessions, work.At(workspaceDir))
	}

	storedSessions = matching(storedSessions, options.Filter)

	listings := describe(directory, storedSessions, options.Running)
	listingTotal := len(listings) + unlistedCount
	if len(listings) == 0 {
		_, _ = fmt.Fprintln(output.Failure, style.Subtle(nothingToList(options)))
	}

	listings = withinLimit(listings, listingTotal, options.Filter, output.Failure)

	if options.JSON {
		return writeJSON(listings, output.Screen)
	}

	return writeTable(listings, output.Screen)
}

func nothingToList(options *inputOpts) string {
	kind := "stored"
	if options.Archived {
		kind = "archived"
	}
	if options.Filter != "" {
		return fmt.Sprintf("no %s session matches %q", kind, options.Filter)
	}

	return "there are no " + kind + " sessions to list"
}

func loadRequestedSessions(directory string, options *inputOpts, failure io.Writer) ([]*picker.Session, int, error) {
	if canLoadNewest(options) {
		return loadNewestSessions(directory, failure)
	}

	if err := ohSessions.RefreshListings(directory, failure); err != nil {
		return nil, 0, err
	}
	storedSessions, err := loadSessions(directory, options.Archived)
	return storedSessions, 0, err
}

func loadNewestSessions(directory string, failure io.Writer) ([]*picker.Session, int, error) {
	storedSessions, total, err := ohSessions.LoadNewest(directory, listLimit)
	if err == nil {
		return storedSessions, total - len(storedSessions), nil
	}
	if refreshError := ohSessions.RefreshListings(directory, failure); refreshError != nil {
		return nil, 0, refreshError
	}

	storedSessions, total, err = ohSessions.LoadNewest(directory, listLimit)
	return storedSessions, total - len(storedSessions), err
}

func canLoadNewest(options *inputOpts) bool {
	return !options.Archived && !options.Running && options.Workspace == "" && options.Filter == ""
}

func loadSessions(directory string, isArchivedWanted bool) ([]*picker.Session, error) {
	if isArchivedWanted {
		return ohSessions.LoadArchived(directory)
	}

	return ohSessions.Load(directory)
}

func matching(storedSessions []*picker.Session, filter string) []*picker.Session {
	if filter == "" {
		return storedSessions
	}

	matches := make([]*picker.Session, 0, len(storedSessions))
	for _, storedSession := range storedSessions {
		if strutil.MatchesQuery(storedSession.Text(), filter) {
			matches = append(matches, storedSession)
		}
	}

	return matches
}

func withinLimit(listings []Listing, total int, filter string, failure io.Writer) []Listing {
	if len(filter) > shortFilter || total <= listLimit {
		return listings
	}

	_, _ = fmt.Fprintln(failure, style.Subtle(fmt.Sprintf(
		"listing the newest %d of %d sessions, which a filter of more than %d characters lists in full",
		listLimit, total, shortFilter,
	)))

	return listings[:min(listLimit, len(listings))]
}

func describe(directory string, storedSessions []*picker.Session, isRunningOnly bool) []Listing {
	listings := make([]Listing, 0, len(storedSessions))
	for _, storedSession := range storedSessions {
		if isRunningOnly && !storedSession.IsRunning {
			continue
		}

		listings = append(listings, Listing{
			Name:         storedSession.Name,
			Status:       status(storedSession),
			IsRunning:    storedSession.IsRunning,
			IsArchived:   storedSession.IsArchived,
			IsFast:       storedSession.IsFast,
			Title:        oneLine(storedSession.Title),
			WorkspaceDir: storedSession.WorkspaceDir,
			ScratchDir:   location.GetTmpDir(storedSession.Name),
			SessionDir:   sessionPath(directory, storedSession),
			Model:        storedSession.ModelID,
			Effort:       storedSession.Effort,
			Messages:     storedSession.MessageCount,
			StartedAt:    storedSession.StartedAt,
			TouchedAt:    storedSession.TouchedAt,
		})
	}

	return listings
}

func status(storedSession *picker.Session) string {
	switch {
	case storedSession.IsArchived:
		return archivedStatus
	case storedSession.IsRunning:
		return runningStatus
	default:
		return endedStatus
	}
}

func sessionPath(directory string, storedSession *picker.Session) string {
	if storedSession.IsArchived {
		return session.ArchivePath(directory, storedSession.Name)
	}

	return session.Dir(directory, storedSession.Name)
}

func writeJSON(listings []Listing, writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "    ")
	return encoder.Encode(listings)
}

func writeTable(listings []Listing, writer io.Writer) error {
	if len(listings) == 0 {
		return nil
	}

	rows := make([][]string, 0, len(listings))
	for _, listing := range listings {
		rows = append(rows, []string{
			listing.Status,
			listing.Name,
			width.Elide(strutil.OrDash(listing.Title), titleColumn),
			strconv.Itoa(listing.Messages),
			util.CoarseDuration(listing.TouchedAt.Sub(listing.StartedAt)),
			util.Ago(listing.TouchedAt),
			modelName(listing),
			strutil.OrDash(listing.Effort),
			listing.WorkspaceDir,
		})
	}

	listingTable := table.New(
		table.Column{Title: "Status"},
		table.Column{Title: "Agent"},
		table.Column{Title: "Title"},
		table.Column{Title: "Messages"},
		table.Column{Title: "Length"},
		table.Column{Title: "Last Message"},
		table.Column{Title: "Model"},
		table.Column{Title: "Effort"},
		table.Column{Title: "Workspace"},
	).Fit(rows)

	if _, err := fmt.Fprintln(writer, style.Subtle(listingTable.Header(0))); err != nil {
		return err
	}

	for index, listing := range listings {
		line := listingTable.Row(rows[index], 0)
		if listing.IsRunning {
			line = style.RunningSession(line)
		}

		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}

	return nil
}

func modelName(listing Listing) string {
	name := strutil.OrDash(listing.Model)
	if listing.IsFast {
		return fastMode.GetMark(true) + " " + name
	}

	return name
}

func oneLine(text string) string {
	return strings.ReplaceAll(text, "\n", " ")
}
