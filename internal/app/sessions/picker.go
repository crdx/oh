package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"

	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/menu"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/preview"
	"crdx.org/oh/internal/app/sessions/picker"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/style"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/money"
	"crdx.org/oh/internal/util/diskutil"
	"crdx.org/oh/pkg/session"
)

func Choose(directory string, workspace *work.Space, currency money.Currency, terminal *os.File, screen io.Writer) (string, error) {
	sessions, archivedSessions, err := loadPickerSessions(directory, screen)
	if err != nil {
		return "", err
	}
	markOtherWorkspaces(sessions, workspace)
	markOtherWorkspaces(archivedSessions, workspace)

	if len(sessions) == 0 && len(archivedSessions) == 0 {
		return "", errors.New("there are no stored conversations")
	}

	store := picker.Store{
		Sessions:         sessions,
		ArchivedSessions: archivedSessions,
		Archive: func(storedSession *picker.Session) (int64, error) {
			if err := session.Archive(directory, storedSession.Name); err != nil {
				return 0, err
			}

			return Occupied(session.ArchivePath(directory, storedSession.Name)), nil
		},
		Restore: func(storedSession *picker.Session) (int64, error) {
			if err := session.Restore(directory, storedSession.Name); err != nil {
				return 0, err
			}

			return Occupied(session.Dir(directory, storedSession.Name)), nil
		},
		Delete: func(storedSession *picker.Session) error {
			if err := session.Delete(directory, storedSession.Name); err != nil {
				return err
			}

			return os.RemoveAll(location.GetTmpDir(storedSession.Name))
		},
		Read: func(storedSession *picker.Session, room int) ([]string, error) {
			return preview.Read(directory, storedSession.Name, work.At(storedSession.WorkspaceDir), currency, room)
		},
		Measure: func(storedSession *picker.Session) int64 {
			return Occupied(Path(directory, storedSession))
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

func markOtherWorkspaces(sessions []*picker.Session, workspace *work.Space) {
	for _, storedSession := range sessions {
		storedSession.IsOtherWorkspace = !workspace.IsAt(storedSession.WorkspaceDir)
	}
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

func Measure(directory string, sessions []*picker.Session) {
	for _, storedSession := range sessions {
		storedSession.Bytes = Occupied(Path(directory, storedSession))
	}
}

func Occupied(path string) int64 {
	occupiedBytes, err := diskutil.Occupied(path)
	if err != nil {
		return 0
	}

	return occupiedBytes
}

func Path(directory string, storedSession *picker.Session) string {
	if storedSession.IsArchived {
		return session.ArchivePath(directory, storedSession.Name)
	}

	return session.Dir(directory, storedSession.Name)
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

type metadataKey struct {
	name       string
	isArchived bool
}

type metadataResult struct {
	metadata *session.Meta
	err      error
}

func RefreshListings(directory string, screen io.Writer) error {
	storedNames, archivedNames, err := session.ListNames(directory)
	if err != nil {
		return err
	}

	keys := append(metadataKeys(storedNames, false), metadataKeys(archivedNames, true)...)
	_, err = refreshMetadata(directory, screen, keys, ValidateFormats)
	return err
}

func RefreshListing(directory string, screen io.Writer, name string) error {
	if name == "" || !session.Exists(directory, name) {
		return nil
	}

	_, err := refreshMetadata(directory, screen, []metadataKey{{name: name}}, nil)
	return explainFormat(name, err)
}

func refreshMetadata(
	directory string,
	screen io.Writer,
	keys []metadataKey,
	validate func(directory string) error,
) (map[metadataKey]metadataResult, error) {
	metadata := readMetadata(directory, keys)
	stale := make([]metadataKey, 0)
	for _, key := range keys {
		if metadata[key].err != nil {
			stale = append(stale, key)
		}
	}
	if len(stale) == 0 {
		return metadata, nil
	}

	if validate != nil {
		if formatError := validate(directory); formatError != nil {
			return nil, formatError
		}
	}

	rebuilt := 0
	for _, key := range stale {
		wasRebuilt, err := store.RebuildMetaIfIdle(directory, key.name)
		if err != nil {
			return nil, err
		}
		if wasRebuilt {
			rebuilt++
		}
		metadata[key] = readOneMetadata(directory, key)
	}
	if rebuilt > 0 {
		_, _ = fmt.Fprintln(screen, style.Subtle("writing the session listing again from the journals"))
	}
	return metadata, nil
}

func metadataKeys(names []string, isArchived bool) []metadataKey {
	keys := make([]metadataKey, 0, len(names))
	for _, name := range names {
		keys = append(keys, metadataKey{name: name, isArchived: isArchived})
	}
	return keys
}

func readMetadata(directory string, keys []metadataKey) map[metadataKey]metadataResult {
	results := make([]metadataResult, len(keys))
	indexes := make(chan int)
	readerCount := min(len(keys), runtime.GOMAXPROCS(0))
	var readers sync.WaitGroup
	for range readerCount {
		readers.Go(func() {
			for index := range indexes {
				results[index] = readOneMetadata(directory, keys[index])
			}
		})
	}
	for index := range keys {
		indexes <- index
	}
	close(indexes)
	readers.Wait()

	metadata := make(map[metadataKey]metadataResult, len(keys))
	for index, key := range keys {
		metadata[key] = results[index]
	}
	return metadata
}

func readOneMetadata(directory string, key metadataKey) metadataResult {
	var storedMeta *session.Meta
	var err error
	if key.isArchived {
		storedMeta, err = session.ArchivedMeta(directory, key.name)
	} else {
		storedMeta, err = session.ReadMeta(directory, key.name)
	}
	return metadataResult{metadata: storedMeta, err: err}
}

func loadPickerSessions(
	directory string,
	screen io.Writer,
) ([]*picker.Session, []*picker.Session, error) {
	storedNames, archivedNames, err := session.ListNames(directory)
	if err != nil {
		return nil, nil, err
	}

	keys := append(metadataKeys(storedNames, false), metadataKeys(archivedNames, true)...)
	readResults, err := refreshMetadata(directory, screen, keys, ValidateFormats)
	if err != nil {
		return nil, nil, err
	}

	storedMetadata, err := loadNamedMetadataFrom(directory, storedNames, readResults)
	if err != nil {
		return nil, nil, err
	}
	if err := inspect(directory, storedMetadata); err != nil {
		return nil, nil, err
	}
	archivedSessions, err := loadArchivedMetadataFrom(archivedNames, readResults)
	if err != nil {
		return nil, nil, err
	}

	return listings(storedMetadata), archivedSessions, nil
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
	return loadNamedMetadataFrom(directory, names, readMetadata(directory, metadataKeys(names, false)))
}

func loadNamedMetadataFrom(
	directory string,
	names []string,
	readResults map[metadataKey]metadataResult,
) ([]sessionMetadata, error) {
	metadata := make([]sessionMetadata, 0, len(names))

	for _, name := range names {
		result := readResults[metadataKey{name: name}]
		storedMeta := result.metadata
		metaError := result.err
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

	return loadArchivedMetadataFrom(names, readMetadata(directory, metadataKeys(names, true)))
}

func loadArchivedMetadataFrom(
	names []string,
	readResults map[metadataKey]metadataResult,
) ([]*picker.Session, error) {
	sessions := make([]*picker.Session, 0, len(names))
	for _, name := range names {
		result := readResults[metadataKey{name: name, isArchived: true}]
		if result.err != nil {
			return nil, fmt.Errorf("could not read archived session %s metadata: %w", name, result.err)
		}

		listing, isDescribed := describe(result.metadata)
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
