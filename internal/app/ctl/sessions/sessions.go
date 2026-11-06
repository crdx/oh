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

	"crdx.org/oh/internal/app/ctl/console"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/segment/fastMode"
	"crdx.org/oh/internal/app/sessions/picker"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/table"
	"crdx.org/oh/internal/app/tr"
	"crdx.org/oh/internal/app/width"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/util"
	"crdx.org/oh/internal/util/strutil"
	"crdx.org/oh/pkg/session"

	"crdx.org/oh/internal/app/sessions"
)

const Usage = `oh --ctl sessions — list or delete stored sessions

Usage:
    $0 --ctl sessions [-j] [-w <dir>] [-r] [-a] [<filter>]
    $0 --ctl sessions (-d | --delete) <label>

Options:
    -d, --delete             Delete one stored or archived session by its exact label
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
	Delete        bool   `docopt:"--delete"`
	Label         string `docopt:"<label>"`
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
	Bytes        int64     `json:"bytes"`
	StartedAt    time.Time `json:"started"`
	TouchedAt    time.Time `json:"touched"`
}

func Run() error {
	return run(duckopt.MustBind[inputOpts](Usage, "$0"), console.Standard())
}

func run(options *inputOpts, output console.Output) error {
	directory := location.GetSessionsDir()
	if options.Delete {
		if !session.Exists(directory, options.Label) {
			return fmt.Errorf(tr.NoSessionToDelete, options.Label)
		}
		if err := session.Delete(directory, options.Label); err != nil {
			return fmt.Errorf("%s: %w", fmt.Sprintf(tr.SessionDeleteFailure, options.Label), err)
		}
		_, err := fmt.Fprintf(output.Screen, tr.SessionDeleted+"\n", options.Label)
		return err
	}

	storedSessions, unlistedCount, err := loadRequestedSessions(directory, options, output.Failure)
	if err != nil {
		if migrationError := sessions.ValidateFormats(directory); migrationError != nil {
			return migrationError
		}
		return err
	}

	if options.Workspace != "" {
		workspaceDir, err := filepath.Abs(options.Workspace)
		if err != nil {
			return fmt.Errorf("%s: %w", tr.WorkspacePathFailure, err)
		}
		storedSessions = sessions.InWorkspace(storedSessions, work.At(workspaceDir))
	}

	storedSessions = matching(storedSessions, options.Filter)

	listings := describe(directory, storedSessions, options.Running)
	listingTotal := len(listings) + unlistedCount
	if len(listings) == 0 {
		_, _ = fmt.Fprintln(output.Failure, style.Subtle(nothingToList(options)))
	}

	listings = withinLimit(listings, listingTotal, options.Filter, output.Failure)
	measure(listings)

	if options.JSON {
		return writeJSON(listings, output.Screen)
	}

	return writeTable(listings, output.Screen)
}

func nothingToList(options *inputOpts) string {
	switch {
	case options.Archived && options.Filter != "":
		return fmt.Sprintf(tr.NoArchivedSessionMatches, options.Filter)
	case options.Filter != "":
		return fmt.Sprintf(tr.NoStoredSessionMatches, options.Filter)
	case options.Archived:
		return tr.NoArchivedSessions
	default:
		return tr.NoStoredSessions
	}
}

func loadRequestedSessions(directory string, options *inputOpts, failure io.Writer) ([]*picker.Session, int, error) {
	if canLoadNewest(options) {
		return loadNewestSessions(directory, failure)
	}

	if err := sessions.RefreshListings(directory, failure); err != nil {
		return nil, 0, err
	}
	storedSessions, err := loadSessions(directory, options.Archived)
	return storedSessions, 0, err
}

func loadNewestSessions(directory string, failure io.Writer) ([]*picker.Session, int, error) {
	storedSessions, total, err := sessions.LoadNewest(directory, listLimit)
	if err == nil {
		return storedSessions, total - len(storedSessions), nil
	}
	if refreshError := sessions.RefreshListings(directory, failure); refreshError != nil {
		return nil, 0, refreshError
	}

	storedSessions, total, err = sessions.LoadNewest(directory, listLimit)
	return storedSessions, total - len(storedSessions), err
}

func canLoadNewest(options *inputOpts) bool {
	return !options.Archived && !options.Running && options.Workspace == "" && options.Filter == ""
}

func loadSessions(directory string, isArchivedWanted bool) ([]*picker.Session, error) {
	if isArchivedWanted {
		return sessions.LoadArchived(directory)
	}

	return sessions.Load(directory)
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

	_, _ = fmt.Fprintln(failure, style.Subtle(fmt.Sprintf(tr.ListingLimit, listLimit, total, shortFilter)))

	return listings[:min(listLimit, len(listings))]
}

func measure(listings []Listing) {
	for index := range listings {
		listings[index].Bytes = sessions.Occupied(listings[index].SessionDir)
	}
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
			SessionDir:   sessions.Path(directory, storedSession),
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
			picker.FormatSize(listing.Bytes),
			util.CoarseDuration(listing.TouchedAt.Sub(listing.StartedAt)),
			util.Ago(listing.TouchedAt),
			modelName(listing),
			strutil.OrDash(listing.Effort),
			listing.WorkspaceDir,
		})
	}

	listingTable := table.New(
		table.Column{Title: tr.StatusColumn},
		table.Column{Title: tr.AgentColumn},
		table.Column{Title: tr.TitleColumn},
		table.Column{Title: tr.MessagesColumn},
		table.Column{Title: tr.SizeColumn, Align: table.Right},
		table.Column{Title: tr.LengthColumn},
		table.Column{Title: tr.LastMessageColumn},
		table.Column{Title: tr.ModelColumn},
		table.Column{Title: tr.EffortColumn},
		table.Column{Title: tr.WorkspaceColumn},
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
