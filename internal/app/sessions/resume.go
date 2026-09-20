package sessions

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"crdx.org/io/pkg/session"

	"crdx.org/io/internal/app/caps"
	"crdx.org/io/internal/app/drops"
	"crdx.org/io/internal/app/model"
	"crdx.org/io/internal/app/store"
	"crdx.org/io/internal/app/work"
)

const sessionTranscriptName = "chat.md"

type ForkSource struct {
	SourceChatPath  string
	DroppedChatName string
	sourceName      string
	userMessage     string
}

func forkedTranscriptName(sourceName string) string {
	return sourceName + "." + sessionTranscriptName
}

func forkSourcePrompt(sourceName string, transcriptPath string) string {
	return fmt.Sprintf(
		"This session was forked from %s.\n"+
			"Check %s's size first, then read its head and tail before continuing.\n"+
			"Its own opening message will say whether %s was forked from an earlier session.\n"+
			"If so, follow the chain to get the full context.",
		sourceName, transcriptPath, sourceName,
	)
}

func (self *ForkSource) GetInitialUserMessage(transcriptPath string) string {
	initialUserMessage := forkSourcePrompt(self.sourceName, transcriptPath)
	if self.userMessage != "" {
		initialUserMessage += "\n\n" + self.userMessage
	}
	return initialUserMessage
}

func (self *ForkSource) GetMessageWithChatAt(message string, transcriptPath string) string {
	placeholderPrompt := forkSourcePrompt(self.sourceName, self.DroppedChatName)
	resolvedPrompt := forkSourcePrompt(self.sourceName, transcriptPath)
	return strings.Replace(message, placeholderPrompt, resolvedPrompt, 1)
}

func (self *ForkSource) CopyChat(keeper *drops.Keeper) (string, error) {
	return keeper.CopyFile(self.SourceChatPath, self.DroppedChatName)
}

func GetForkSource(directory string, workspace *work.Space, name string, userMessage string) (*ForkSource, error) {
	if name == "" {
		return nil, nil //nolint:nilnil // no name means no fork was asked for
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		return nil, err
	}
	if err := requireWorkspace(storedSession, workspace); err != nil {
		return nil, err
	}

	return &ForkSource{
		SourceChatPath:  filepath.Join(directory, storedSession.Name, sessionTranscriptName),
		DroppedChatName: forkedTranscriptName(storedSession.Name),
		sourceName:      storedSession.Name,
		userMessage:     userMessage,
	}, nil
}

func LoadForResume(directory string, workspace *work.Space, name string) (*store.Session, error) {
	if name == "" {
		return nil, nil //nolint:nilnil // no name means no fork was asked for
	}

	isRunning, err := session.IsInUse(directory, name)
	if err != nil {
		return nil, err
	}
	if isRunning {
		return nil, fmt.Errorf("session %s is running", name)
	}

	storedSession, err := store.Read(directory, name)
	if err != nil {
		return nil, err
	}
	if err := requireWorkspace(storedSession, workspace); err != nil {
		return nil, err
	}
	if !storedSession.CanResume() {
		return nil, fmt.Errorf("session %s did not finish every turn and cannot be resumed safely (yet)", name)
	}

	return storedSession, nil
}

func requireWorkspace(storedSession *store.Session, workspace *work.Space) error {
	if workspace.IsAt(storedSession.Meta.WorkspaceDir) {
		return nil
	}

	return fmt.Errorf(
		"session %s belongs to %s and can only be opened there",
		storedSession.Name,
		storedSession.Meta.WorkspaceDir,
	)
}

func OpenWriter(directory string, resumedSession *store.Session, meta store.Meta) (*store.Writer, error) {
	if resumedSession == nil {
		return store.Create(directory, meta)
	}

	log, err := store.Open(directory, resumedSession.Name)
	if errors.Is(err, session.ErrInUse) {
		return nil, fmt.Errorf("session %s is running", resumedSession.Name)
	}

	return log, err
}

func ModelSelection(resumedSession *store.Session) model.Selection {
	if resumedSession == nil {
		return model.Selection{}
	}

	isFast, _ := model.LastRecordedFastMode(resumedSession.Events)

	return model.Selection{
		Provider: resumedSession.Meta.Provider,
		Model:    resumedSession.Meta.Model,
		Effort:   resumedSession.Meta.Effort,
		IsFast:   isFast,
	}
}

func OpeningCaps(requestedCaps caps.Set, wereCapsChosen bool, resumedSession *store.Session) (caps.Set, error) {
	if resumedSession == nil {
		return requestedCaps, nil
	}

	lastCaps, found := caps.LastRecordedMode(resumedSession.Events)
	if !found {
		return requestedCaps, nil
	}

	if wereCapsChosen && requestedCaps != lastCaps {
		return 0, fmt.Errorf(
			"a resumed conversation opens in the mode it was left in, which was %s rather than %s",
			lastCaps.Flags(),
			requestedCaps.Flags(),
		)
	}

	return lastCaps, nil
}

func OpeningConfinement(wasYoloChosen bool, resumedSession *store.Session) (bool, error) {
	if resumedSession == nil {
		return wasYoloChosen, nil
	}

	if wasYoloChosen && !resumedSession.Meta.Yolo {
		return false, fmt.Errorf(
			"a resumed conversation opens in the confinement it was left in, which was %s rather than %s",
			confinement(resumedSession.Meta.Yolo),
			confinement(wasYoloChosen),
		)
	}

	return resumedSession.Meta.Yolo, nil
}

func confinement(isYolo bool) string {
	if isYolo {
		return "no sandbox at all"
	}

	return "a sandbox"
}

func ResumeCommand(programPath string, name string) string {
	return fmt.Sprintf("%s -r %s", filepath.Base(programPath), name)
}
