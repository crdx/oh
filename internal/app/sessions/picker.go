package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"crdx.org/io/internal/app/location"
	"crdx.org/io/internal/app/menu"
	"crdx.org/io/internal/app/model"
	"crdx.org/io/internal/app/preview"
	"crdx.org/io/internal/app/sessions/picker"
	"crdx.org/io/internal/app/store"
	"crdx.org/io/internal/app/style"
	"crdx.org/io/internal/app/work"
	"crdx.org/io/pkg/session"
)

func Choose(directory string, workspace *work.Space, terminal *os.File, screen io.Writer) (string, error) {
	if err := RefreshListings(directory, screen); err != nil {
		return "", err
	}

	sessions, err := Load(directory)
	if err != nil {
		if migrationError := ValidateFormats(directory); migrationError != nil {
			return "", migrationError
		}
		return "", err
	}
	sessions = InWorkspace(sessions, workspace)

	archivedSessions, err := LoadArchived(directory)
	if err != nil {
		return "", err
	}
	archivedSessions = InWorkspace(archivedSessions, workspace)

	if len(sessions) == 0 && len(archivedSessions) == 0 {
		return "", errors.New("there are no stored conversations for this workspace")
	}

	store := picker.Store{
		Sessions:         sessions,
		ArchivedSessions: archivedSessions,
		Archive: func(storedSession *picker.Session) error {
			return session.Archive(directory, storedSession.Name)
		},
		Restore: func(storedSession *picker.Session) error {
			return session.Restore(directory, storedSession.Name)
		},
		Delete: func(storedSession *picker.Session) error {
			if err := session.Delete(directory, storedSession.Name); err != nil {
				return err
			}

			return os.RemoveAll(location.GetTmpDir(storedSession.Name))
		},
		Read: func(storedSession *picker.Session, room int) ([]string, error) {
			return preview.Read(directory, storedSession.Name, workspace, room)
		},
	}

	chosenSession, err := picker.Choose(store, terminal, screen)
	if errors.Is(err, menu.ErrCancelled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return chosenSession.Name, nil
}

func InWorkspace(sessions []*picker.Session, workspace *work.Space) []*picker.Session {
	chosenSessions := make([]*picker.Session, 0, len(sessions))
	for _, storedSession := range sessions {
		if workspace.IsAt(storedSession.WorkspaceDir) {
			chosenSessions = append(chosenSessions, storedSession)
		}
	}

	return chosenSessions
}

func NamesInWorkspace(directory string, workspace *work.Space) ([]string, error) {
	storedNames, err := session.StoredNames(directory)
	if err != nil {
		return nil, err
	}

	sessions := make([]*picker.Session, 0, len(storedNames))
	for _, name := range storedNames {
		storedMeta, metaError := session.ReadMeta(directory, name)
		if metaError != nil {
			continue
		}

		listing, isDescribed := describe(storedMeta)
		if !isDescribed {
			continue
		}

		sessions = append(sessions, listing)
	}

	newestFirst(sessions)

	chosenSessions := InWorkspace(sessions, workspace)
	names := make([]string, 0, len(chosenSessions))
	for _, chosenSession := range chosenSessions {
		names = append(names, chosenSession.Name)
	}

	return names, nil
}

func RefreshListings(directory string, screen io.Writer) error {
	names, err := session.AllNames(directory)
	if err != nil {
		return err
	}

	return refreshListings(directory, screen, names, ValidateFormats)
}

func RefreshListing(directory string, screen io.Writer, name string) error {
	if name == "" || !session.Exists(directory, name) {
		return nil
	}

	return refreshListings(directory, screen, []string{name}, ValidateStoredFormats)
}

func refreshListings(
	directory string,
	screen io.Writer,
	names []string,
	validate func(directory string) error,
) error {
	stale := store.StaleMetaOf(directory, names)
	if len(stale) == 0 {
		return nil
	}

	if formatError := validate(directory); formatError != nil {
		return formatError
	}

	rebuilt := 0
	for _, name := range stale {
		wasRebuilt, err := store.RebuildMetaIfIdle(directory, name)
		if err != nil {
			return err
		}
		if wasRebuilt {
			rebuilt++
		}
	}
	if rebuilt > 0 {
		_, _ = fmt.Fprintln(screen, style.Subtle("writing the session listing again from the journals"))
	}
	return nil
}

func Load(directory string) ([]*picker.Session, error) {
	metadata, err := loadMetadata(directory)
	if err != nil {
		return nil, err
	}
	if err := inspect(directory, metadata); err != nil {
		return nil, err
	}

	return listings(metadata), nil
}

func LoadNewest(directory string, limit int) ([]*picker.Session, int, error) {
	names, err := session.StoredNames(directory)
	if err != nil {
		return nil, 0, err
	}

	metadata, err := loadNamedMetadata(directory, names)
	if err != nil {
		return nil, 0, err
	}
	total := len(metadata)
	if limit >= 0 && len(metadata) > limit {
		metadata = metadata[:limit]
	}
	if err := inspect(directory, metadata); err != nil {
		return nil, 0, err
	}

	return listings(metadata), total, nil
}

type sessionMetadata struct {
	listing        *picker.Session
	isRunningKnown bool
}

func loadMetadata(directory string) ([]sessionMetadata, error) {
	names, err := session.StoredNames(directory)
	if err != nil {
		return nil, err
	}

	return loadNamedMetadata(directory, names)
}

func loadNamedMetadata(directory string, names []string) ([]sessionMetadata, error) {
	metadata := make([]sessionMetadata, 0, len(names))

	for _, name := range names {
		storedMeta, metaError := session.ReadMeta(directory, name)
		isRunning := false
		isRunningKnown := false
		if metaError != nil {
			var err error
			isRunning, err = session.IsInUse(directory, name)
			if err != nil {
				return nil, err
			}
			isRunningKnown = true
			if isRunning {
				storedMeta, metaError = store.GetListingMeta(directory, name)
			}
		}
		if metaError != nil {
			return nil, fmt.Errorf("could not read session %s metadata: %w", name, metaError)
		}

		listing, isDescribed := describe(storedMeta)
		if !isDescribed {
			continue
		}
		listing.IsRunning = isRunning

		metadata = append(metadata, sessionMetadata{
			listing:        listing,
			isRunningKnown: isRunningKnown,
		})
	}

	newestMetadataFirst(metadata)
	return metadata, nil
}

func inspect(directory string, metadata []sessionMetadata) error {
	for _, storedMetadata := range metadata {
		if !storedMetadata.isRunningKnown {
			isRunning, err := session.IsInUse(directory, storedMetadata.listing.Name)
			if err != nil {
				return err
			}
			storedMetadata.listing.IsRunning = isRunning
		}
	}

	return nil
}

func listings(metadata []sessionMetadata) []*picker.Session {
	sessions := make([]*picker.Session, 0, len(metadata))
	for _, storedMetadata := range metadata {
		sessions = append(sessions, storedMetadata.listing)
	}
	return sessions
}

func LoadArchived(directory string) ([]*picker.Session, error) {
	names, err := session.ArchivedNames(directory)
	if err != nil {
		return nil, err
	}

	sessions := make([]*picker.Session, 0, len(names))
	for _, name := range names {
		storedMeta, err := session.ArchivedMeta(directory, name)
		if err != nil {
			return nil, fmt.Errorf("could not read archived session %s metadata: %w", name, err)
		}

		listing, isDescribed := describe(storedMeta)
		if !isDescribed {
			continue
		}
		listing.IsArchived = true

		sessions = append(sessions, listing)
	}

	newestFirst(sessions)
	return sessions, nil
}

type listingData struct {
	WorkspaceDir string `json:"workspaceDir"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Effort       string `json:"effort"`
	IsFast       bool   `json:"fast"`
}

func describe(storedMeta *session.Meta) (*picker.Session, bool) {
	var data listingData
	if len(storedMeta.Data) > 0 && json.Unmarshal(storedMeta.Data, &data) != nil {
		return nil, false
	}

	return &picker.Session{
		Name:         storedMeta.Name,
		WorkspaceDir: data.WorkspaceDir,
		StartedAt:    storedMeta.StartedAt,
		TouchedAt:    storedMeta.TouchedAt,
		Title:        storedMeta.Title,
		Model:        strings.Join(model.DisplayName(data.Model), " "),
		ModelID:      data.Model,
		Effort:       data.Effort,
		IsFast:       data.IsFast,
		MessageCount: storedMeta.Messages,
	}, true
}

func newestFirst(sessions []*picker.Session) {
	slices.SortFunc(sessions, newestOrder)
}

func newestMetadataFirst(metadata []sessionMetadata) {
	slices.SortFunc(metadata, func(first, second sessionMetadata) int {
		return newestOrder(first.listing, second.listing)
	})
}

func newestOrder(first *picker.Session, second *picker.Session) int {
	if order := second.TouchedAt.Compare(first.TouchedAt); order != 0 {
		return order
	}
	return strings.Compare(second.Name, first.Name)
}
