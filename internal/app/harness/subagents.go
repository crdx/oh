package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"

	"crdx.org/oh/internal/app/backend"
	"crdx.org/oh/internal/app/caps"
	"crdx.org/oh/internal/app/config"
	"crdx.org/oh/internal/app/location"
	"crdx.org/oh/internal/app/model"
	"crdx.org/oh/internal/app/permission"
	"crdx.org/oh/internal/app/prompt"
	"crdx.org/oh/internal/app/shell"
	"crdx.org/oh/internal/app/skill"
	"crdx.org/oh/internal/app/store"
	"crdx.org/oh/internal/app/subagents"
	"crdx.org/oh/internal/app/work"
	"crdx.org/oh/internal/file"
	"crdx.org/oh/internal/sandbox"
	"crdx.org/oh/internal/util/pathutil"
	"crdx.org/oh/pkg/agent"
	"crdx.org/oh/pkg/ask"
	"crdx.org/oh/pkg/session"
	"crdx.org/oh/pkg/tool"
	"crdx.org/oh/pkg/tool/middleware/truncate"
	"crdx.org/oh/pkg/toolbox/messageparent"
)

type childOptions struct {
	parentChoice       model.Choice
	endpoints          backend.EndpointSettings
	workspace          *work.Space
	settings           config.Config
	sessionName        string
	sessionsDir        string
	scratchParent      string
	runner             sandbox.Runner
	ensurePersisted    func() error
	isParentUnconfined bool
	recordsWire        bool
	parentFiles        *file.Root
	parentWritable     func() []string
	parentCaps         func() caps.Set
	userHome           string
	toolOutputLimit    *truncate.Limit
	skillDirectories   []string
}

type childBrief struct {
	skills         []skill.Skill
	dropsDirectory string
	notes          agent.NoteFormat
}

func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

type preparedChildManager struct {
	manager *subagents.Manager
	rotate  func(config.Subagent, model.Defaults) (subagents.Rotation, error)
}

func prepareChildManager(options childOptions, choices []model.Choice, isPrinting bool) (preparedChildManager, error) {
	if isPrinting {
		return preparedChildManager{}, nil
	}
	rotate := func(settings config.Subagent, defaults model.Defaults) (subagents.Rotation, error) {
		return childRotationOf(options.parentChoice, settings, defaults, choices)
	}
	rotation, err := rotate(options.settings.Subagent, options.settings.Defaults.ForSelections())
	if err != nil {
		return preparedChildManager{}, err
	}
	manager, err := subagents.New(subagents.Options{
		Directory:   session.ChildrenDir(options.sessionsDir, options.sessionName),
		Scratch:     options.scratchParent,
		Parent:      options.sessionName,
		Models:      rotation.Models,
		Concurrency: rotation.Concurrency,
		Meta: store.Meta{
			WorkspaceDir:   options.workspace.GetDir(),
			ShouldSkipWire: !options.recordsWire,
		},
		Factory:      newChildFactory(options),
		PickName:     rand.IntN,
		EnsureParent: options.ensurePersisted,
		Workspace:    childWorkspace(options),
		ScratchNote:  childScratchNote(options),
		Caps: func() caps.Set {
			return options.parentCaps() & (caps.Read | caps.Shell)
		},
	})
	return preparedChildManager{manager: manager, rotate: rotate}, err
}

func canStartChildren(
	parentChoice model.Choice,
	settings config.Config,
	choices []model.Choice,
) bool {
	rotation, err := childRotationOf(
		parentChoice, settings.Subagent, settings.Defaults.ForSelections(), choices,
	)
	return err == nil && len(rotation.Models) > 0
}

func childRotationOf(
	parentChoice model.Choice,
	settings config.Subagent,
	defaults model.Defaults,
	choices []model.Choice,
) (subagents.Rotation, error) {
	defaults.IsFast = false
	models := make([]subagents.ChildModel, 0, len(choices))
	for _, choice := range choices {
		if !model.CanDelegateTo(parentChoice, choice) {
			continue
		}
		selection, err := model.ParseSelection([]model.Choice{choice}, choice.Provider+"/"+choice.ID, defaults)
		if err != nil {
			return subagents.Rotation{}, err
		}
		models = append(models, subagents.ChildModel{Choice: choice, Selection: selection})
	}
	return subagents.Rotation{Models: models, Concurrency: settings.Concurrency}, nil
}

func newChildFactory(options childOptions) subagents.Factory {
	return func(ctx context.Context, child subagents.Child) (subagents.Worker, error) {
		workspace := work.At(child.Workspace)
		if err := workspace.Open(); err != nil {
			return subagents.Worker{}, err
		}
		skills, err := discoverSkills(workspace.GetDir(), options.skillDirectories, options.settings.Skills.Exclude, io.Discard)
		if err != nil {
			_ = workspace.Close()
			return subagents.Worker{}, err
		}
		state, err := buildChildState(options, child, workspace, skills)
		if err != nil {
			_ = workspace.Close()
			return subagents.Worker{}, err
		}
		closeChild := func() {
			state.Close()
			_ = workspace.Close()
		}

		tools, err := state.childTools(child, options.runner)
		if err != nil {
			closeChild()
			return subagents.Worker{}, err
		}
		tools, err = options.settings.ConfigureBuiltins(tools, config.BuiltinToolOptions{
			IsGroupAllowed: state.options.mode.Allows,
			Approve: func(context.Context, string, string, permission.Setting) error {
				return ask.ErrUnavailable
			},
		})
		if err != nil {
			closeChild()
			return subagents.Worker{}, err
		}
		connection, err := backend.Connect(child.Choice, child.Selection, options.endpoints)
		if err != nil {
			closeChild()
			return subagents.Worker{}, err
		}
		systemPrompt := child.SystemPrompt
		if systemPrompt == "" {
			systemPrompt = childPrompt(options, workspace, child, state.options.roots.HomeDirectory, childBrief{
				skills:         skills,
				dropsDirectory: state.drops.GetDirectory(),
				notes:          agent.NoteFormatOf(connection.Client),
			})
		}
		connection.UseSession(options.sessionName + "-" + child.Name)
		connection.ObserveHTTP(child.Observer)
		provider := guardUsage(
			ctx,
			connection.Client,
			child.Choice,
			child.Selection,
			location.GetUsageCachePath(child.Selection.Provider, options.endpoints.OverrideURL != ""),
		)
		worker, changes, err := childAgent(systemPrompt, provider, tools, child)
		if err != nil {
			closeChild()
			return subagents.Worker{}, err
		}
		state.storePictures(worker)
		return subagents.Worker{Agent: worker, Tools: tools, SystemPrompt: systemPrompt, Changes: changes, Close: closeChild}, nil
	}
}

func buildChildState(
	options childOptions,
	child subagents.Child,
	workspace *work.Space,
	skills []skill.Skill,
) (*agentState, error) {
	if err := child.ScratchRoot.Mkdir(subagents.HomeName, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	home, err := child.ScratchRoot.OpenRoot(subagents.HomeName)
	if err != nil {
		return nil, err
	}
	return buildAgentState(agentOptions{
		workspace: workspace,
		mode:      caps.NewMode(child.Caps),
		paths:     childPaths(options.settings.Grants),
		roots: shell.Roots{
			HomeDirectory:    filepath.Join(child.Scratch, subagents.HomeName),
			Home:             home,
			ScratchDirectory: child.Scratch,
			Scratch:          child.ScratchRoot,
		},
		releaseRoots:     func() { _ = home.Close() },
		refuseHomeWrite:  allowWrite,
		parent:           &shell.Parent{Scratch: options.scratchParent, Writable: options.parentWritable},
		skills:           skills,
		sessionDirectory: child.Directory,
		ensurePersisted:  child.EnsurePersisted,
		outputLimit:      options.toolOutputLimit,
	})
}

func (self *agentState) childTools(child subagents.Child, runner sandbox.Runner) ([]tool.Tool, error) {
	return self.tools(self.shell(nil, runner), agentFeatures{
		afterShell: []tool.Tool{messageparent.New(child.MessageParent)},
	})
}

func childAgent(prompt string, provider agent.Provider, tools []tool.Tool, child subagents.Child) (*agent.Agent, []agent.Event, error) {
	if len(child.FrozenTools) == 0 {
		return agent.New(prompt, provider, tools), nil, nil
	}
	frozenToolbox, err := prepareFrozenTools(child.FrozenTools, child.History, tools, nil)
	if err != nil {
		return nil, nil, err
	}
	var changes []agent.Event
	if frozenToolbox.availabilityRestoration.IsChanged {
		changes = append(changes, frozenToolbox.availabilityRestoration.Change)
	}
	return agent.NewWithEnabledTools(prompt, provider, frozenToolbox.registeredTools, frozenToolbox.offeredTools), changes, nil
}

func childPrompt(
	options childOptions,
	workspace *work.Space,
	child subagents.Child,
	home string,
	brief childBrief,
) string {
	paths := childPaths(options.settings.Grants)
	return prompt.Subagent(prompt.SubagentConfig{
		SessionName:        options.sessionName,
		Name:               child.Name,
		WorkspaceDir:       workspace.GetDir(),
		HomeDir:            home,
		ScratchDir:         child.Scratch,
		ParentScratchDir:   parentScratchPath(options, child.Name),
		ParentScratchHost:  options.scratchParent,
		SiblingScratchHost: filepath.Join(options.scratchParent, subagents.ScratchName),
		IsParentUnconfined: options.isParentUnconfined,
		IsScratchReadable:  isReadableThrough(paths, options.scratchParent),
		HasShell:           child.Caps.Has(caps.Shell),
		UserHome:           options.userHome,
		Paths:              paths,
		Skills:             brief.skills,
		DropsDirectory:     brief.dropsDirectory,
		SharedPrompt:       child.SharedPrompt,
		Notes:              brief.notes,
	})
}

func childPaths(parentPaths shell.Paths) shell.Paths {
	return shell.Paths{
		Deny: parentPaths.Deny,
		Read: outsideHostTmp(parentPaths.Read),
		Exec: outsideHostTmp(parentPaths.Exec),
		Path: outsideHostTmp(parentPaths.Path),
		Home: outsideHostTmp(parentPaths.Home),
	}
}

func outsideHostTmp(paths []string) []string {
	return slices.DeleteFunc(slices.Clone(paths), func(path string) bool {
		return shell.RefuseHostTmpPath(path) != nil
	})
}

func isReadableThrough(paths shell.Paths, directory string) bool {
	if directory == "" {
		return false
	}
	candidates := []string{filepath.Clean(directory), pathutil.Canonicalise(directory)}
	for _, root := range slices.Concat(paths.Read, paths.Exec, paths.Path, paths.Home) {
		for _, candidate := range candidates {
			if _, isBelow := pathutil.RelativeTo(root, candidate); isBelow {
				return true
			}
		}
	}
	return false
}

func parentScratchPath(options childOptions, name string) string {
	if options.isParentUnconfined {
		return filepath.Join(options.scratchParent, subagents.ScratchName, name)
	}
	return filepath.Join(sandbox.TmpDir, subagents.ScratchName, name)
}

const unverifiedNote = "unverified: check what matters before relying on it; "

func childScratchNote(options childOptions) func(string) string {
	return func(name string) string {
		owner := "your "
		if options.isParentUnconfined {
			owner = ""
		}
		return unverifiedNote + "its /tmp is " + owner + parentScratchPath(options, name) + ", so read any /tmp path it reports as beneath that"
	}
}

func childWorkspace(options childOptions) func(string) (string, error) {
	parentWorkspace := options.workspace.GetDir()
	return func(directory string) (string, error) {
		if directory == "" {
			return parentWorkspace, nil
		}
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(parentWorkspace, directory)
		}
		resolvedPath, err := resolveChildWorkspace(options, directory)
		if err != nil {
			return "", err
		}
		if work.IsShadowed(resolvedPath) {
			return "", fmt.Errorf("%s leads into the host's /tmp, which no subagent can read", directory)
		}
		if options.scratchParent != "" {
			scratchRoot := filepath.Clean(options.scratchParent)
			if _, isBelow := pathutil.RelativeTo(scratchRoot, resolvedPath); isBelow {
				return "", fmt.Errorf("%s lies inside the parent's scratch space, which no subagent can read", directory)
			}
		}
		return resolvedPath, nil
	}
}

func resolveChildWorkspace(options childOptions, directory string) (string, error) {
	if options.isParentUnconfined {
		return hostDirectory(directory)
	}
	hostPath, err := readableDirectory(options.parentFiles, directory)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(hostPath)
	if err != nil {
		return "", fmt.Errorf("%s cannot be resolved: %w", directory, err)
	}
	if resolvedPath != hostPath {
		if _, err := readableDirectory(options.parentFiles, resolvedPath); err != nil {
			return "", err
		}
	}
	return resolvedPath, nil
}

func hostDirectory(directory string) (string, error) {
	resolvedPath, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", fmt.Errorf("%s cannot be resolved: %w", directory, err)
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", directory)
	}
	if work.IsShadowed(resolvedPath) {
		return "", fmt.Errorf("%s leads into the host's /tmp, which no subagent can read", directory)
	}
	return resolvedPath, nil
}

func readableDirectory(files *file.Root, directory string) (string, error) {
	root, name, err := files.Resolve(directory)
	if err != nil {
		return "", fmt.Errorf("%s is not a directory you can read", directory)
	}
	info, err := root.Stat(name)
	if err != nil {
		return "", fmt.Errorf("%s is not a directory you can read: %w", directory, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", directory)
	}
	return filepath.Join(root.Name(), name), nil
}
